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

const maxHidden = 120 // cap audit-drawer payload size

// ArticlesHandler fetches all enabled sources, applies the user's hard filters
// and de-duplication, optionally ranks the survivors with the LLM, and returns
// the assembled feed plus the audit list of what was hidden.
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

		scored := false
		if settings.LLMScoring {
			scored = s.rankArticles(r.Context(), db, user.ID, visible)
		}

		sortFeed(visible, scored)

		if len(hidden) > maxHidden {
			hidden = hidden[:maxHidden]
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"articles":        visible,
			"hidden":          hidden,
			"scored":          scored,
			"score_threshold": settings.ScoreThreshold,
			"layout":          settings.Layout,
			"generated_at":    time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// rankArticles fills in Score/ScoreReason on the articles in place, using cached
// scores where valid and asking the LLM for the rest. Returns whether scoring
// actually ran (Claude enabled and reachable).
func (s *Service) rankArticles(ctx context.Context, db *sql.DB, userID int64, articles []Article) bool {
	cfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil || !cfg.Enabled {
		return false
	}

	profile, err := GetProfile(db, userID)
	if err != nil {
		log.Printf("news: load profile: %v", err)
		return false
	}

	cached, _ := GetScores(db, userID, profile.Version)

	var toScore []Article
	for _, a := range articles {
		if _, ok := cached[a.ID]; !ok {
			toScore = append(toScore, a)
		}
	}

	fresh, err := scoreArticles(ctx, cfg, profile, toScore)
	if err != nil {
		log.Printf("news: scoring failed: %v", err)
		// Still apply whatever was cached.
		applyScores(articles, cached)
		return len(cached) > 0
	}
	if err := SaveScores(db, userID, profile.Version, fresh); err != nil {
		log.Printf("news: save scores: %v", err)
	}

	merged := cached
	if merged == nil {
		merged = map[string]ScoreEntry{}
	}
	for id, e := range fresh {
		merged[id] = e
	}
	applyScores(articles, merged)
	return true
}

func applyScores(articles []Article, scores map[string]ScoreEntry) {
	for i := range articles {
		if e, ok := scores[articles[i].ID]; ok {
			articles[i].Score = e.Score
			articles[i].ScoreReason = e.Reason
		}
	}
}

// sortFeed orders the feed. When scored, relevance comes first (unscored items,
// Score == -1, sort to the bottom of the scored block but above nothing else);
// ties and the unscored fall back to newest-first.
func sortFeed(articles []Article, scored bool) {
	sort.SliceStable(articles, func(i, j int) bool {
		if scored && articles[i].Score != articles[j].Score {
			return articles[i].Score > articles[j].Score
		}
		return articles[i].PublishedAt.After(articles[j].PublishedAt)
	})
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
