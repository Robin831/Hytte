// Package news aggregates articles from Norwegian RSS feeds, applies per-user
// topic/paywall filtering and cross-source de-duplication, and optionally ranks
// surviving articles by relevance using the Claude CLI. It mirrors the
// fetch-cache-serve shape of the weather and transit packages.
package news

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// Source describes a single RSS feed.
type Source struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	FeedURL string `json:"feed_url"`
	Color   string `json:"color"`
	Enabled bool   `json:"enabled"`
}

// Article is a single news item after parsing.
type Article struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	SourceName  string    `json:"source_name"`
	SourceColor string    `json:"source_color"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Summary     string    `json:"summary"`
	ImageURL    string    `json:"image_url"`
	PublishedAt time.Time `json:"published_at"`
	Categories  []string  `json:"categories"`

	// Derived/per-user fields, populated by the handler.
	Read        bool        `json:"read"`
	Saved       bool        `json:"saved"`
	Feedback    int         `json:"feedback"`     // 1, -1, or 0
	Score       int         `json:"score"`        // -1 when not scored
	ScoreReason string      `json:"score_reason"` // short explanation from the ranker
	AlsoIn      []SourceRef `json:"also_in"`      // other sources running the same story
}

// SourceRef is a lightweight pointer to a duplicate of an article in another source.
type SourceRef struct {
	Source     string `json:"source"`
	SourceName string `json:"source_name"`
	URL        string `json:"url"`
}

// HiddenArticle is an article removed by a hard filter, kept for the
// "why filtered" audit drawer.
type HiddenArticle struct {
	Article
	Reason string `json:"reason"` // e.g. "keyword:trump", "category:Sport", "paywall"
}

// Settings holds a user's news configuration.
type Settings struct {
	Sources         []Source `json:"sources"`
	BlockKeywords   []string `json:"block_keywords"`
	BlockCategories []string `json:"block_categories"`
	HidePaywalled   bool     `json:"hide_paywalled"`
	LLMScoring      bool     `json:"llm_scoring"`
	ScoreThreshold  int      `json:"score_threshold"`
	Layout          string   `json:"layout"`
}

// DefaultSources is the seed feed list. All confirmed to serve clean RSS.
var DefaultSources = []Source{
	{Key: "vg", Name: "VG", FeedURL: "https://www.vg.no/rss/feed/", Color: "#e4002b", Enabled: true},
	{Key: "nrk", Name: "NRK", FeedURL: "https://www.nrk.no/toppsaker.rss", Color: "#0a51a1", Enabled: true},
	{Key: "tv2", Name: "TV 2", FeedURL: "https://www.tv2.no/rss/nyheter", Color: "#e30613", Enabled: true},
	{Key: "tek", Name: "tek.no", FeedURL: "https://www.tek.no/api/rss/rss2/medium/collections", Color: "#1f9c4d", Enabled: true},
	{Key: "gamer", Name: "gamer.no", FeedURL: "https://www.gamer.no/rss", Color: "#7b2ff7", Enabled: true},
}

// DefaultBlockKeywords seeds a sensible "don't show me this" list. Matched
// case-insensitively against title, summary and categories. Editable in the UI.
var DefaultBlockKeywords = []string{
	"trump", "ukraina", "ukraine", "russland", "russia", "putin",
	"krig", "iran", "israel", "gaza", "hamas",
	"influenser", "influencer", "realityprofil", "paradise hotel", "love island",
	"kardashian", "stjernekamp",
}

// DefaultSettings returns the configuration applied to a user who has never
// saved news settings.
func DefaultSettings() Settings {
	return Settings{
		Sources:         append([]Source(nil), DefaultSources...),
		BlockKeywords:   append([]string(nil), DefaultBlockKeywords...),
		BlockCategories: []string{},
		HidePaywalled:   true,
		LLMScoring:      true,
		ScoreThreshold:  25,
		Layout:          "timeline",
	}
}

// ArticleID derives a stable identifier for an article from its canonical URL
// (falling back to the GUID). Stable across refreshes so read/saved/score state
// keys correctly.
func ArticleID(urlOrGUID string) string {
	sum := sha1.Sum([]byte(urlOrGUID))
	return hex.EncodeToString(sum[:])[:16]
}
