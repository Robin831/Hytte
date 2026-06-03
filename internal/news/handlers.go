package news

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

const (
	maxHidden     = 120            // cap audit-drawer payload size
	maxArticleAge = 48 * time.Hour // drop anything older than this
	maxArticles   = 100            // cap the returned feed size

	voteUpScore   = 100 // 👍 pins an article to the top
	voteDownScore = 0   // 👎 sinks it to the bottom (below any threshold)
)

// ArticlesHandler fetches all enabled sources, applies the user's hard filters
// and de-duplication, caps the result by age and count, applies any cached
// relevance scores, and returns the feed immediately. Computing missing scores
// happens in the background (see applyRanking) so the page never blocks on the
// Claude CLI.
func (s *Service) ArticlesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		settings, err := GetSettings(db, user.ID)
		if err != nil {
			log.Printf("news: load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		raw := s.FetchAll(r.Context(), settings.Sources)
		visible, hidden := applyFilters(raw, settings)
		visible = dedupe(visible)
		visible = withinAge(visible, maxArticleAge)

		// Canonical order is newest-first; the client re-sorts by relevance when
		// the user picks that mode. Cap after sorting so we keep the newest.
		sort.SliceStable(visible, func(i, j int) bool {
			return visible[i].PublishedAt.After(visible[j].PublishedAt)
		})
		if len(visible) > maxArticles {
			visible = visible[:maxArticles]
		}

		// Per-user overlays.
		readSet, _ := ReadSet(db, user.ID)
		savedSet, _ := SavedSet(db, user.ID)
		fbMap, _ := FeedbackMap(db, user.ID)
		for i := range visible {
			a := &visible[i]
			a.Read = readSet[a.ID]
			a.Saved = savedSet[a.ID]
			a.Feedback = fbMap[a.ID]
		}

		rankingEnabled, scoringPending := false, false
		if settings.LLMScoring {
			rankingEnabled, scoringPending = s.applyRanking(db, user.ID, visible)
		}

		if len(hidden) > maxHidden {
			hidden = hidden[:maxHidden]
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"articles":        visible,
			"hidden":          hidden,
			"ranking_enabled": rankingEnabled,
			"scoring_pending": scoringPending,
			"scored":          rankingEnabled, // legacy alias
			"score_threshold": settings.ScoreThreshold,
			"layout":          settings.Layout,
			"generated_at":    time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// applyRanking fills in any cached scores synchronously and, if some articles
// are still unscored, kicks off a background scoring job. It returns whether
// ranking is enabled (Claude available) and whether a background job was
// started this request (so the client knows to refetch shortly).
func (s *Service) applyRanking(db *sql.DB, userID int64, articles []Article) (enabled, pending bool) {
	cfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil || !cfg.Enabled {
		return false, false
	}

	profile, err := GetProfile(db, userID)
	if err != nil {
		log.Printf("news: load profile: %v", err)
		return true, false
	}

	cached, _ := GetScores(db, userID)
	applyScores(articles, cached)

	var toScore []Article
	for _, a := range articles {
		if _, ok := cached[a.ID]; !ok {
			toScore = append(toScore, a)
		}
	}
	if len(toScore) == 0 {
		return true, false
	}
	if !s.tryStartScoring(userID) {
		return true, false // a job is already running for this user
	}

	snap := make([]Article, len(toScore))
	copy(snap, toScore)
	go s.scoreInBackground(db, userID, cfg, profile, snap)
	return true, true
}

// scoreInBackground scores articles off the request path and persists the
// results, which the next feed fetch will pick up from the cache.
func (s *Service) scoreInBackground(db *sql.DB, userID int64, cfg *training.ClaudeConfig, profile Profile, toScore []Article) {
	defer s.finishScoring(userID)

	ctx, cancel := context.WithTimeout(context.Background(), scoreBackgroundTimeout)
	defer cancel()

	fresh, err := scoreArticles(ctx, cfg, profile, toScore)
	if err != nil {
		log.Printf("news: background scoring failed: %v", err)
		return
	}
	if err := SaveScores(db, userID, fresh); err != nil {
		log.Printf("news: save scores: %v", err)
		return
	}
	log.Printf("news: scored %d articles for user %d", len(fresh), userID)
}

func applyScores(articles []Article, scores map[string]ScoreEntry) {
	for i := range articles {
		if e, ok := scores[articles[i].ID]; ok {
			articles[i].Score = e.Score
			articles[i].ScoreReason = e.Reason
		}
	}
}

// withinAge drops articles older than maxAge. Articles with an unknown
// (zero) publish time are kept rather than silently discarded.
func withinAge(articles []Article, maxAge time.Duration) []Article {
	cutoff := time.Now().Add(-maxAge)
	out := articles[:0]
	for _, a := range articles {
		if a.PublishedAt.IsZero() || a.PublishedAt.After(cutoff) {
			out = append(out, a)
		}
	}
	return out
}

// MarkReadHandler records that an article was opened.
func (s *Service) MarkReadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			ArticleID string `json:"article_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ArticleID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article_id required"})
			return
		}
		if err := MarkRead(db, user.ID, body.ArticleID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark read"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// FeedbackHandler records a more/less-like-this vote (signal 1/-1, or 0 to clear).
func (s *Service) FeedbackHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			ArticleID string `json:"article_id"`
			Signal    int    `json:"signal"`
			Title     string `json:"title"`
			Summary   string `json:"summary"`
			Source    string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ArticleID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article_id required"})
			return
		}
		if body.Signal < -1 || body.Signal > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signal must be -1, 0 or 1"})
			return
		}
		if err := SetFeedback(db, user.ID, body.ArticleID, body.Signal, body.Title, body.Summary, body.Source); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save feedback"})
			return
		}

		// Apply the vote directly to this article's score so it immediately
		// moves to the top (👍) or bottom (👎) — no full rescore. The updated
		// taste profile still shapes scoring of future articles. Clearing a vote
		// drops the override so the model re-scores the article on the next pass.
		switch {
		case body.Signal > 0:
			_ = SetScore(db, user.ID, body.ArticleID, voteUpScore, "you liked this")
		case body.Signal < 0:
			_ = SetScore(db, user.ID, body.ArticleID, voteDownScore, "you hid this")
		default:
			_ = DeleteScore(db, user.ID, body.ArticleID)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// SavedListHandler returns the user's bookmarked articles.
func (s *Service) SavedListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		articles, err := ListSaved(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load saved"})
			return
		}
		if articles == nil {
			articles = []Article{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"articles": articles})
	}
}

// SavedToggleHandler bookmarks or removes an article.
func (s *Service) SavedToggleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			Saved   bool    `json:"saved"`
			Article Article `json:"article"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Article.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article required"})
			return
		}
		if err := SetSaved(db, user.ID, body.Article, body.Saved); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SettingsGetHandler returns the user's news settings.
func SettingsGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		settings, err := GetSettings(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// SettingsPutHandler saves the user's news settings.
func SettingsPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid settings"})
			return
		}
		s = sanitizeSettings(s)
		if err := SaveSettings(db, user.ID, s); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// sanitizeSettings clamps and normalises incoming settings.
func sanitizeSettings(s Settings) Settings {
	if s.ScoreThreshold < 0 {
		s.ScoreThreshold = 0
	}
	if s.ScoreThreshold > 100 {
		s.ScoreThreshold = 100
	}
	if s.Layout != "columns" {
		s.Layout = "timeline"
	}
	if s.Sources == nil {
		s.Sources = append([]Source(nil), DefaultSources...)
	}
	if s.BlockKeywords == nil {
		s.BlockKeywords = []string{}
	}
	if s.BlockCategories == nil {
		s.BlockCategories = []string{}
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("news: write json: %v", err)
	}
}
