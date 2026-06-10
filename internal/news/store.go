package news

import (
	"database/sql"
	"encoding/json"
	"log"
	"sort"

	"github.com/Robin831/Hytte/internal/encryption"
)

// GetSettings loads a user's news settings, falling back to defaults for any
// missing row. Encrypted fields are decrypted; an empty/legacy source list
// falls back to DefaultSources so new sources we ship reach existing users only
// when they haven't customised the list.
func GetSettings(db *sql.DB, userID int64) (Settings, error) {
	def := DefaultSettings()
	var (
		sourcesJSON, kwEnc, catEnc, layout string
		hidePaywall, llm, threshold        int
	)
	err := db.QueryRow(`SELECT sources, block_keywords, block_categories,
		hide_paywalled, llm_scoring, score_threshold, layout
		FROM news_settings WHERE user_id = ?`, userID).
		Scan(&sourcesJSON, &kwEnc, &catEnc, &hidePaywall, &llm, &threshold, &layout)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, err
	}

	s := def
	if sourcesJSON != "" {
		var srcs []Source
		if jsonErr := json.Unmarshal([]byte(sourcesJSON), &srcs); jsonErr == nil && len(srcs) > 0 {
			s.Sources = srcs
		}
	}
	s.BlockKeywords = decryptList(kwEnc, def.BlockKeywords)
	s.BlockCategories = decryptList(catEnc, []string{})
	s.HidePaywalled = hidePaywall == 1
	s.LLMScoring = llm == 1
	s.ScoreThreshold = threshold
	if layout != "" {
		s.Layout = layout
	}
	return s, nil
}

// SaveSettings upserts a user's news settings, encrypting the keyword and
// category lists (they reveal personal interests).
func SaveSettings(db *sql.DB, userID int64, s Settings) error {
	sourcesJSON, err := json.Marshal(s.Sources)
	if err != nil {
		return err
	}
	kwEnc, err := encryptList(s.BlockKeywords)
	if err != nil {
		return err
	}
	catEnc, err := encryptList(s.BlockCategories)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO news_settings
		(user_id, sources, block_keywords, block_categories, hide_paywalled, llm_scoring, score_threshold, layout, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(user_id) DO UPDATE SET
			sources=excluded.sources, block_keywords=excluded.block_keywords,
			block_categories=excluded.block_categories, hide_paywalled=excluded.hide_paywalled,
			llm_scoring=excluded.llm_scoring, score_threshold=excluded.score_threshold,
			layout=excluded.layout, updated_at=excluded.updated_at`,
		userID, string(sourcesJSON), kwEnc, catEnc,
		boolToInt(s.HidePaywalled), boolToInt(s.LLMScoring), s.ScoreThreshold, s.Layout)
	return err
}

// SetFeedback records (or clears, when signal == 0) a more/less-like-this vote.
func SetFeedback(db *sql.DB, userID int64, articleID string, signal int, title, summary, source string) error {
	if signal == 0 {
		_, err := db.Exec(`DELETE FROM news_feedback WHERE user_id=? AND article_id=?`, userID, articleID)
		return err
	}
	titleEnc, err := encryption.EncryptField(title)
	if err != nil {
		return err
	}
	summaryEnc, err := encryption.EncryptField(summary)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO news_feedback (user_id, article_id, signal, title, summary, source)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, article_id) DO UPDATE SET
			signal=excluded.signal, title=excluded.title, summary=excluded.summary, source=excluded.source`,
		userID, articleID, signal, titleEnc, summaryEnc, source)
	return err
}

// FeedbackMap returns articleID -> signal for marking the feed.
func FeedbackMap(db *sql.DB, userID int64) (map[string]int, error) {
	rows, err := db.Query(`SELECT article_id, signal FROM news_feedback WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var sig int
		if err := rows.Scan(&id, &sig); err != nil {
			return nil, err
		}
		out[id] = sig
	}
	return out, rows.Err()
}

// Profile holds the liked/disliked headlines used to build the ranking prompt
// for scoring new articles. Votes are applied directly to the voted article's
// score (see SetScore), so the profile only shapes future scoring — it never
// invalidates already-computed scores.
type Profile struct {
	Likes    []string
	Dislikes []string
}

// GetProfile reconstructs the taste profile from stored feedback.
func GetProfile(db *sql.DB, userID int64) (Profile, error) {
	rows, err := db.Query(`SELECT signal, title FROM news_feedback WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return Profile{}, err
	}
	defer rows.Close()

	var p Profile
	for rows.Next() {
		var titleEnc string
		var sig int
		if err := rows.Scan(&sig, &titleEnc); err != nil {
			return Profile{}, err
		}
		title, decErr := encryption.DecryptField(titleEnc)
		if decErr != nil {
			log.Printf("news: decrypt feedback title: %v", decErr)
			title = ""
		}
		if title == "" {
			continue
		}
		if sig > 0 && len(p.Likes) < 40 {
			p.Likes = append(p.Likes, title)
		} else if sig < 0 && len(p.Dislikes) < 40 {
			p.Dislikes = append(p.Dislikes, title)
		}
	}
	return p, rows.Err()
}

// MarkRead records that the user opened an article.
func MarkRead(db *sql.DB, userID int64, articleID string) error {
	_, err := db.Exec(`INSERT INTO news_read (user_id, article_id) VALUES (?, ?)
		ON CONFLICT(user_id, article_id) DO NOTHING`, userID, articleID)
	return err
}

// ReadSet returns the set of article IDs the user has already opened.
func ReadSet(db *sql.DB, userID int64) (map[string]bool, error) {
	rows, err := db.Query(`SELECT article_id FROM news_read WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetSaved bookmarks or removes an article from the saved list.
func SetSaved(db *sql.DB, userID int64, a Article, saved bool) error {
	if !saved {
		_, err := db.Exec(`DELETE FROM news_saved WHERE user_id=? AND article_id=?`, userID, a.ID)
		return err
	}
	titleEnc, err := encryption.EncryptField(a.Title)
	if err != nil {
		return err
	}
	urlEnc, err := encryption.EncryptField(a.URL)
	if err != nil {
		return err
	}
	summaryEnc, err := encryption.EncryptField(a.Summary)
	if err != nil {
		return err
	}
	pub := ""
	if !a.PublishedAt.IsZero() {
		pub = a.PublishedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	_, err = db.Exec(`INSERT INTO news_saved
		(user_id, article_id, source, source_name, title, url, summary, image_url, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, article_id) DO UPDATE SET
			source=excluded.source, source_name=excluded.source_name,
			title=excluded.title, url=excluded.url, summary=excluded.summary,
			image_url=excluded.image_url, published_at=excluded.published_at`,
		userID, a.ID, a.Source, a.SourceName, titleEnc, urlEnc, summaryEnc, a.ImageURL, pub)
	return err
}

// SavedSet returns the IDs of saved articles, for marking the feed.
func SavedSet(db *sql.DB, userID int64) (map[string]bool, error) {
	rows, err := db.Query(`SELECT article_id FROM news_saved WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListSaved returns the user's saved articles, newest first.
func ListSaved(db *sql.DB, userID int64) ([]Article, error) {
	rows, err := db.Query(`SELECT article_id, source, source_name, title, url, summary, image_url, published_at
		FROM news_saved WHERE user_id=? ORDER BY saved_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		var a Article
		var titleEnc, urlEnc, summaryEnc, pub string
		if err := rows.Scan(&a.ID, &a.Source, &a.SourceName, &titleEnc, &urlEnc, &summaryEnc, &a.ImageURL, &pub); err != nil {
			return nil, err
		}
		a.Title, _ = encryption.DecryptField(titleEnc)
		a.URL, _ = encryption.DecryptField(urlEnc)
		a.Summary, _ = encryption.DecryptField(summaryEnc)
		a.PublishedAt = parseDate(pub)
		a.Saved = true
		a.Score = -1
		out = append(out, a)
	}
	return out, rows.Err()
}

// ScoreEntry is a cached relevance score.
type ScoreEntry struct {
	Score  int
	Reason string
}

// GetScores returns all cached scores for the user. Scores persist across
// votes — a vote updates only the voted article (see SetScore), so existing
// scores are never invalidated and never need a full recompute.
func GetScores(db *sql.DB, userID int64) (map[string]ScoreEntry, error) {
	rows, err := db.Query(`SELECT article_id, score, reason FROM news_scores WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ScoreEntry{}
	for rows.Next() {
		var id string
		var e ScoreEntry
		if err := rows.Scan(&id, &e.Score, &e.Reason); err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, rows.Err()
}

// SaveScores writes freshly computed model scores.
func SaveScores(db *sql.DB, userID int64, scores map[string]ScoreEntry) error {
	if len(scores) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO news_scores (user_id, article_id, score, reason)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, article_id) DO UPDATE SET
			score=excluded.score, reason=excluded.reason`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	// Deterministic order keeps tests stable.
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := scores[id]
		if _, err := stmt.Exec(userID, id, e.Score, e.Reason); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetScore upserts a single article's score. Used to apply a vote directly so
// the voted article immediately moves to the top (👍) or bottom (👎) without
// rescoring the whole feed.
func SetScore(db *sql.DB, userID int64, articleID string, score int, reason string) error {
	_, err := db.Exec(`INSERT INTO news_scores (user_id, article_id, score, reason)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, article_id) DO UPDATE SET
			score=excluded.score, reason=excluded.reason`,
		userID, articleID, score, reason)
	return err
}

// DeleteScore removes an article's cached score so it gets re-scored by the
// model on the next refresh (used when a vote is cleared).
func DeleteScore(db *sql.DB, userID int64, articleID string) error {
	_, err := db.Exec(`DELETE FROM news_scores WHERE user_id=? AND article_id=?`, userID, articleID)
	return err
}

// --- helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func encryptList(list []string) (string, error) {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return encryption.EncryptField(string(b))
}

func decryptList(enc string, fallback []string) []string {
	if enc == "" {
		return fallback
	}
	plain, err := encryption.DecryptField(enc)
	if err != nil {
		log.Printf("news: decrypt list: %v", err)
		return fallback
	}
	var out []string
	if jsonErr := json.Unmarshal([]byte(plain), &out); jsonErr != nil {
		return fallback
	}
	return out
}
