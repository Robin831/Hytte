package news

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// TestSavedListHandlerIndependentOfFeedCaps verifies that GET /api/news/saved
// serves bookmarked articles straight from the store — including ones older than
// the feed's 48h age cap and beyond its 100-item truncation cap — with the full
// decrypted snapshot payload.
func TestSavedListHandlerIndependentOfFeedCaps(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService()

	// One article that the feed would have aged out (10 days old) and enough
	// total articles to exceed the feed's maxArticles truncation cap.
	total := maxArticles + 5
	for i := 0; i < total; i++ {
		a := sampleArticle("h-"+itoa(i), time.Now().Add(-10*24*time.Hour))
		if err := SetSaved(d, 1, a, true); err != nil {
			t.Fatalf("SetSaved %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/news/saved", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1}))
	rec := httptest.NewRecorder()

	svc.SavedListHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Articles []Article `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Articles) != total {
		t.Fatalf("got %d saved articles, want %d (feed caps must not apply)", len(resp.Articles), total)
	}

	// Verify the snapshot fields come back decrypted and complete.
	got := resp.Articles[0]
	if got.Title == "" || got.URL == "" || got.Summary == "" {
		t.Errorf("expected decrypted text fields, got %+v", got)
	}
	if got.Source == "" || got.SourceName == "" {
		t.Errorf("expected source metadata, got %+v", got)
	}
	if got.PublishedAt.IsZero() {
		t.Error("expected non-zero published_at")
	}
}

// TestSavedListHandlerEmpty verifies the handler returns an empty array (not
// null) when the user has no saved articles.
func TestSavedListHandlerEmpty(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService()

	req := httptest.NewRequest(http.MethodGet, "/api/news/saved", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1}))
	rec := httptest.NewRecorder()

	svc.SavedListHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp struct {
		Articles []Article `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Articles == nil {
		t.Error("expected non-nil articles array")
	}
	if len(resp.Articles) != 0 {
		t.Errorf("expected 0 articles, got %d", len(resp.Articles))
	}
}
