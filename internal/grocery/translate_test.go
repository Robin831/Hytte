package grocery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

func mockRunPrompt(response string, err error) func() {
	orig := runPrompt
	runPrompt = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return response, err
	}
	return func() { runPrompt = orig }
}

func TestTranslateAndNormalize(t *testing.T) {
	cfg := &training.ClaudeConfig{Enabled: true, CLIPath: "claude", Model: "test"}

	t.Run("parses JSON response", func(t *testing.T) {
		cleanup := mockRunPrompt(`[{"item":"Egg","original":"ไข่","language":"th"}]`, nil)
		defer cleanup()

		items, err := TranslateAndNormalize(context.Background(), cfg, "ไข่")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		if items[0].Content != "Egg" {
			t.Errorf("got content %q, want %q", items[0].Content, "Egg")
		}
		if items[0].OriginalText != "ไข่" {
			t.Errorf("got original %q, want %q", items[0].OriginalText, "ไข่")
		}
		if items[0].SourceLanguage != "th" {
			t.Errorf("got language %q, want %q", items[0].SourceLanguage, "th")
		}
	})

	t.Run("strips markdown code fences", func(t *testing.T) {
		fenced := "```json\n[{\"item\":\"Melk\",\"original\":\"milk\",\"language\":\"en\"}]\n```"
		cleanup := mockRunPrompt(fenced, nil)
		defer cleanup()

		items, err := TranslateAndNormalize(context.Background(), cfg, "milk")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 1 || items[0].Content != "Melk" {
			t.Errorf("got %+v, want Melk", items)
		}
	})

	t.Run("returns error on CLI failure", func(t *testing.T) {
		cleanup := mockRunPrompt("", fmt.Errorf("CLI not found"))
		defer cleanup()

		_, err := TranslateAndNormalize(context.Background(), cfg, "eggs")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "claude translation") {
			t.Errorf("error %q should contain 'claude translation'", err.Error())
		}
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		cleanup := mockRunPrompt("not json", nil)
		defer cleanup()

		_, err := TranslateAndNormalize(context.Background(), cfg, "eggs")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "parsing claude response (output length") {
			t.Errorf("error %q should contain 'parsing claude response (output length'", err.Error())
		}
	})
}

func TestHandleTranslateValidation(t *testing.T) {
	db := setupTestDB(t)
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

	t.Run("rejects invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/grocery/translate", bytes.NewBufferString("not json"))
		req = withUser(req, user)
		w := httptest.NewRecorder()
		HandleTranslate(db)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("rejects empty text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/grocery/translate", bytes.NewBufferString(`{"text":"  "}`))
		req = withUser(req, user)
		w := httptest.NewRecorder()
		HandleTranslate(db)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("rejects oversized body", func(t *testing.T) {
		// 4097 bytes of JSON payload
		big := `{"text":"` + strings.Repeat("a", 4090) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/grocery/translate", bytes.NewBufferString(big))
		req = withUser(req, user)
		w := httptest.NewRecorder()
		HandleTranslate(db)(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
		}
	})

	t.Run("returns 400 when Claude is disabled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/grocery/translate", bytes.NewBufferString(`{"text":"eggs"}`))
		req = withUser(req, user)
		w := httptest.NewRecorder()
		HandleTranslate(db)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["error"] != "claude is not enabled" {
			t.Errorf("error = %q, want %q", resp["error"], "claude is not enabled")
		}
	})

	t.Run("success with mocked Claude", func(t *testing.T) {
		cleanup := mockRunPrompt(`[{"item":"Egg","original":"eggs","language":"en"}]`, nil)
		defer cleanup()

		// Set up claude_enabled preference for the test user.
		_, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
		if err != nil {
			t.Fatalf("insert preference: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/grocery/translate", bytes.NewBufferString(`{"text":"eggs"}`))
		req = withUser(req, user)
		w := httptest.NewRecorder()
		HandleTranslate(db)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp struct {
			Items []TranslatedItem `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("got %d items, want 1", len(resp.Items))
		}
		if resp.Items[0].Content != "Egg" {
			t.Errorf("got content %q, want %q", resp.Items[0].Content, "Egg")
		}
	})
}
