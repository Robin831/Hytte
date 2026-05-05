package suggestions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// withSavedPages temporarily overrides the global Pages registry for a test
// (so the handler doesn't try to generate against the full prod registry) and
// returns a restore function to defer.
func withSavedPages(replacement []Page) func() {
	prev := Pages
	Pages = replacement
	return func() { Pages = prev }
}

func TestRunHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validJSONResponse, nil
	})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	// Non-admin user.
	user := &auth.User{ID: 99, IsAdmin: false}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRunHandlerAdminReturnsCounts(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather", Enabled: true},
		{Slug: "notes", Title: "Notes", Enabled: true},
	})()
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validJSONResponse, nil
	})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", bytes.NewReader(nil))
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d: %s", rec.Code, rec.Body.String())
	}

	var got RunResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", got.Errors)
	}
	if got.Generated != 6 {
		t.Fatalf("expected 6 generated (3 per page × 2), got %d", got.Generated)
	}

	var rowCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 6 {
		t.Fatalf("expected 6 rows persisted, got %d", rowCount)
	}
}

func TestRunHandlerRequiresClaudeEnabled(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()
	// Note: claude_enabled is intentionally NOT set.

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when claude disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}
