package settings

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbpkg "github.com/Robin831/Hytte/internal/db"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := dbpkg.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	return d
}

func TestGetAIPromptsHandler_ReturnsSeededDefaults(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/ai-prompts", nil)
	rr := httptest.NewRecorder()

	GetAIPromptsHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Prompts []AIPrompt `json:"prompts"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Prompts) == 0 {
		t.Fatal("expected at least one prompt in response")
	}

	// All seeded prompts should be marked as default.
	byKey := make(map[string]AIPrompt)
	for _, p := range resp.Prompts {
		byKey[p.Key] = p
	}

	for key := range DefaultPromptBodies {
		p, ok := byKey[key]
		if !ok {
			t.Errorf("expected prompt key %q in response", key)
			continue
		}
		if !p.IsDefault {
			t.Errorf("prompt %q should be marked as default after seeding", key)
		}
	}
}

func TestPutAIPromptHandler_UpsertAndGet(t *testing.T) {
	db := setupTestDB(t)

	body := bytes.NewBufferString(`{"body":"Custom insights prompt."}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/ai-prompts/insights", body)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Put("/api/settings/ai-prompts/{key}", PutAIPromptHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify the prompt is now loaded from DB.
	loaded := LoadPrompt(db, "insights", "fallback")
	if loaded != "Custom insights prompt." {
		t.Errorf("expected updated body, got %q", loaded)
	}
}

func TestPutAIPromptHandler_EmptyBodyRejected(t *testing.T) {
	db := setupTestDB(t)

	body := bytes.NewBufferString(`{"body":"   "}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/ai-prompts/insights", body)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Put("/api/settings/ai-prompts/{key}", PutAIPromptHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteAIPromptHandler_RemovesRow(t *testing.T) {
	db := setupTestDB(t)

	// First, write a custom body so deletion is observable.
	const customBody = "custom body that must be gone after delete"
	if _, err := db.Exec(
		`UPDATE ai_prompts SET prompt_body = ? WHERE prompt_key = 'insights'`, customBody,
	); err != nil {
		t.Fatalf("setup: write custom body: %v", err)
	}
	if got := LoadPrompt(db, "insights", "fallback"); got != customBody {
		t.Fatalf("setup: expected custom body in DB, got %q", got)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/settings/ai-prompts/insights", nil)
	rr := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Delete("/api/settings/ai-prompts/{key}", DeleteAIPromptHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// After deletion, LoadPrompt must NOT return the custom body — it should
	// return the provided fallback because the row no longer exists.
	const fallback = "hardcoded fallback"
	loaded := LoadPrompt(db, "insights", fallback)
	if loaded == customBody {
		t.Errorf("DELETE did not remove the row: LoadPrompt still returned the custom body")
	}
	if loaded != fallback {
		t.Errorf("expected fallback %q after delete, got %q", fallback, loaded)
	}
}

func TestDeleteAIPromptHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/settings/ai-prompts/nonexistent", nil)
	rr := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Delete("/api/settings/ai-prompts/{key}", DeleteAIPromptHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestLoadPrompt_FallsBackToDefault(t *testing.T) {
	db := setupTestDB(t)

	// Delete the insights row to simulate a missing override.
	if _, err := db.Exec(`DELETE FROM ai_prompts WHERE prompt_key = 'insights'`); err != nil {
		t.Fatalf("delete prompt: %v", err)
	}

	got := LoadPrompt(db, "insights", "my default")
	if got != "my default" {
		t.Errorf("expected fallback default, got %q", got)
	}
}

func TestGetAIPromptsHandler_ShowsCustomBody(t *testing.T) {
	db := setupTestDB(t)

	// Update the insights prompt to a custom body.
	if _, err := db.Exec(
		`UPDATE ai_prompts SET prompt_body = 'custom body', updated_at = '2026-01-01T00:00:00Z' WHERE prompt_key = 'insights'`,
	); err != nil {
		t.Fatalf("update prompt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/ai-prompts", nil)
	rr := httptest.NewRecorder()
	GetAIPromptsHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Prompts []AIPrompt `json:"prompts"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, p := range resp.Prompts {
		if p.Key == "insights" {
			if p.Body != "custom body" {
				t.Errorf("expected custom body, got %q", p.Body)
			}
			if p.IsDefault {
				t.Error("expected is_default=false for customized prompt")
			}
			return
		}
	}
	t.Error("insights prompt not found in response")
}
