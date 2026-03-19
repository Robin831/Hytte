package training

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestClaudeTestHandler_NonAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/claude-test", nil)
	req = withUser(req, 1)
	w := httptest.NewRecorder()

	ClaudeTestHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestClaudeTestHandler_Disabled(t *testing.T) {
	database := setupTestDB(t)

	// Claude is disabled by default (no preference set).
	req := httptest.NewRequest(http.MethodPost, "/api/settings/claude-test", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	ClaudeTestHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["ok"] != false {
		t.Errorf("expected ok=false, got %v", resp["ok"])
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Error("expected an error message when claude is disabled")
	}
}

func TestClaudeTestHandler_Enabled_CLINotFound(t *testing.T) {
	database := setupTestDB(t)

	// Enable claude with a non-existent binary path.
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_cli_path", "/nonexistent/path/claude-fake-binary"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/claude-test", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	ClaudeTestHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["ok"] != false {
		t.Errorf("expected ok=false, got %v", resp["ok"])
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Error("expected an error message when CLI binary not found")
	}
}
