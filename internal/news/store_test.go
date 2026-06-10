package news

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-news-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (1, 'test@example.com', 'Test', '', 'g1', '')`)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return database
}

// sampleArticle builds a fully-populated article for save tests.
func sampleArticle(id string, published time.Time) Article {
	return Article{
		ID:          id,
		Source:      "vg",
		SourceName:  "VG",
		Title:       "Saved headline " + id,
		URL:         "https://example.com/" + id,
		Summary:     "A summary for " + id,
		ImageURL:    "https://example.com/" + id + ".jpg",
		PublishedAt: published,
	}
}

// TestSavedArticleSurvivesAgeCap verifies that an article saved with a publish
// time well past the feed's 48h age cap is still returned by ListSaved — the
// saved store does not apply the feed's withinAge filtering.
func TestSavedArticleSurvivesAgeCap(t *testing.T) {
	d := setupTestDB(t)

	old := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago, well past maxArticleAge
	a := sampleArticle("aged-out", old)
	if err := SetSaved(d, 1, a, true); err != nil {
		t.Fatalf("SetSaved: %v", err)
	}

	saved, err := ListSaved(d, 1)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved article, got %d", len(saved))
	}
	if saved[0].ID != "aged-out" {
		t.Errorf("got id %q, want %q", saved[0].ID, "aged-out")
	}
	if saved[0].Title != a.Title {
		t.Errorf("got title %q, want %q", saved[0].Title, a.Title)
	}
}

// TestSavedArticleSurvivesTruncationCap verifies that saving many more articles
// than the feed's maxArticles cap still returns all of them from the store —
// ListSaved does not truncate to the newest N.
func TestSavedArticleSurvivesTruncationCap(t *testing.T) {
	d := setupTestDB(t)

	total := maxArticles + 50 // beyond the feed truncation cap
	base := time.Now().Add(-time.Hour)
	for i := 0; i < total; i++ {
		a := sampleArticle("art-"+itoa(i), base.Add(-time.Duration(i)*time.Minute))
		if err := SetSaved(d, 1, a, true); err != nil {
			t.Fatalf("SetSaved %d: %v", i, err)
		}
	}

	saved, err := ListSaved(d, 1)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(saved) != total {
		t.Fatalf("expected %d saved articles, got %d", total, len(saved))
	}
}

// TestSavedSnapshotFieldsRoundTrip verifies all snapshot fields survive a
// save/list round-trip, with the encrypted text fields decrypted correctly.
func TestSavedSnapshotFieldsRoundTrip(t *testing.T) {
	d := setupTestDB(t)

	published := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a := sampleArticle("round-trip", published)
	if err := SetSaved(d, 1, a, true); err != nil {
		t.Fatalf("SetSaved: %v", err)
	}

	saved, err := ListSaved(d, 1)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved article, got %d", len(saved))
	}
	got := saved[0]
	if got.Title != a.Title || got.URL != a.URL || got.Summary != a.Summary {
		t.Errorf("text fields mismatch: %+v", got)
	}
	if got.Source != a.Source || got.SourceName != a.SourceName || got.ImageURL != a.ImageURL {
		t.Errorf("metadata fields mismatch: %+v", got)
	}
	if !got.PublishedAt.Equal(published) {
		t.Errorf("got published_at %v, want %v", got.PublishedAt, published)
	}
	if !got.Saved {
		t.Error("expected Saved=true")
	}
}

// TestSavedSnapshotEncryptedAtRest verifies the sensitive text fields are not
// stored in plaintext (encryption-at-rest invariant from CLAUDE.md).
func TestSavedSnapshotEncryptedAtRest(t *testing.T) {
	d := setupTestDB(t)

	a := sampleArticle("encrypted", time.Now())
	if err := SetSaved(d, 1, a, true); err != nil {
		t.Fatalf("SetSaved: %v", err)
	}

	var title, url, summary string
	err := d.QueryRow(`SELECT title, url, summary FROM news_saved WHERE user_id=1 AND article_id=?`, a.ID).
		Scan(&title, &url, &summary)
	if err != nil {
		t.Fatalf("query raw row: %v", err)
	}
	if title == a.Title {
		t.Error("title stored in plaintext")
	}
	if url == a.URL {
		t.Error("url stored in plaintext")
	}
	if summary == a.Summary {
		t.Error("summary stored in plaintext")
	}
}

// TestReSaveRefreshesAllSnapshotFields is the regression test for the upsert
// fix: re-saving an article whose feed snapshot changed must update every
// snapshot field, not just title/url/summary.
func TestReSaveRefreshesAllSnapshotFields(t *testing.T) {
	d := setupTestDB(t)

	first := sampleArticle("churned", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := SetSaved(d, 1, first, true); err != nil {
		t.Fatalf("SetSaved first: %v", err)
	}

	updated := Article{
		ID:          "churned",
		Source:      "nrk",
		SourceName:  "NRK",
		Title:       "Updated headline",
		URL:         "https://example.com/updated",
		Summary:     "Updated summary",
		ImageURL:    "https://example.com/updated.jpg",
		PublishedAt: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
	}
	if err := SetSaved(d, 1, updated, true); err != nil {
		t.Fatalf("SetSaved updated: %v", err)
	}

	saved, err := ListSaved(d, 1)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved article after re-save, got %d", len(saved))
	}
	got := saved[0]
	if got.Source != updated.Source {
		t.Errorf("source not refreshed: got %q, want %q", got.Source, updated.Source)
	}
	if got.SourceName != updated.SourceName {
		t.Errorf("source_name not refreshed: got %q, want %q", got.SourceName, updated.SourceName)
	}
	if got.ImageURL != updated.ImageURL {
		t.Errorf("image_url not refreshed: got %q, want %q", got.ImageURL, updated.ImageURL)
	}
	if !got.PublishedAt.Equal(updated.PublishedAt) {
		t.Errorf("published_at not refreshed: got %v, want %v", got.PublishedAt, updated.PublishedAt)
	}
	if got.Title != updated.Title || got.URL != updated.URL || got.Summary != updated.Summary {
		t.Errorf("text fields not refreshed: %+v", got)
	}
}

// TestUnsaveRemovesOnlyThatRow verifies unsaving one article leaves the others.
func TestUnsaveRemovesOnlyThatRow(t *testing.T) {
	d := setupTestDB(t)

	keep := sampleArticle("keep", time.Now())
	drop := sampleArticle("drop", time.Now())
	if err := SetSaved(d, 1, keep, true); err != nil {
		t.Fatalf("SetSaved keep: %v", err)
	}
	if err := SetSaved(d, 1, drop, true); err != nil {
		t.Fatalf("SetSaved drop: %v", err)
	}

	if err := SetSaved(d, 1, drop, false); err != nil {
		t.Fatalf("unsave drop: %v", err)
	}

	saved, err := ListSaved(d, 1)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved article after unsave, got %d", len(saved))
	}
	if saved[0].ID != "keep" {
		t.Errorf("wrong row survived: got %q, want %q", saved[0].ID, "keep")
	}
}

// itoa is a tiny dependency-free int-to-string for building test IDs.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
