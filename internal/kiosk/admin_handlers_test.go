package kiosk

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupAdminTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec(`
		CREATE TABLE kiosk_tokens (
			id           INTEGER PRIMARY KEY,
			token_hash   TEXT NOT NULL UNIQUE,
			name         TEXT NOT NULL DEFAULT '',
			config       TEXT NOT NULL DEFAULT '{}',
			created_by   TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL DEFAULT '',
			expires_at   TEXT,
			last_used_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func adminUser() *auth.User {
	return &auth.User{ID: 1, Email: "admin@example.com", IsAdmin: true}
}

func withAdminUser(r *http.Request, u *auth.User) *http.Request {
	ctx := auth.ContextWithUser(r.Context(), u)
	return r.WithContext(ctx)
}

func TestCreateTokenHandler_Success(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	body := `{"name":"test-kiosk","config":{"screen":"main"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp createTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected plaintext token in response")
	}
	if len(resp.Token) != 64 {
		t.Errorf("expected 64-char hex token, got len=%d", len(resp.Token))
	}
	if resp.Name != "test-kiosk" {
		t.Errorf("expected name=test-kiosk, got %q", resp.Name)
	}
	if resp.CreatedBy != "admin@example.com" {
		t.Errorf("expected created_by=admin@example.com, got %q", resp.CreatedBy)
	}
	if resp.ID == 0 {
		t.Error("expected non-zero ID")
	}

	// Verify the hash is stored, not the plaintext.
	var storedHash string
	err := db.QueryRow("SELECT token_hash FROM kiosk_tokens WHERE id = ?", resp.ID).Scan(&storedHash)
	if err != nil {
		t.Fatalf("query token_hash: %v", err)
	}
	if storedHash == resp.Token {
		t.Error("token hash should not equal plaintext token")
	}
	if storedHash != hashToken(resp.Token) {
		t.Error("stored hash does not match SHA-256 of plaintext token")
	}
}

func TestCreateTokenHandler_MissingName(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	body := `{"config":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTokenHandler_InvalidConfig(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	body := `{"name":"kiosk","config":"not-an-object"}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// "not-an-object" is valid JSON (a string), so this should succeed with config stored as-is.
	// The config field accepts any valid JSON.
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 for valid JSON config, got %d", rec.Code)
	}
}

func TestCreateTokenHandler_InvalidExpiresAt(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	body := `{"name":"kiosk","expires_at":"not-a-date"}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid expires_at, got %d", rec.Code)
	}
}

func TestCreateTokenHandler_WithExpiry(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	expiry := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"name":"expiring","expires_at":%q}`, expiry)
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp createTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExpiresAt == nil {
		t.Error("expected expires_at in response")
	}
}

func TestCreateTokenHandler_NoUser(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := CreateTokenHandler(db)

	body := `{"name":"kiosk"}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	// No user injected into context.
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without user, got %d", rec.Code)
	}
}

func TestListTokensHandler_Empty(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := ListTokensHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/kiosk/tokens", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tokens []tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty list, got %d tokens", len(tokens))
	}
}

func TestListTokensHandler_ReturnsMetadata(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create a token via the create handler.
	createHandler := CreateTokenHandler(db)
	body := `{"name":"list-test","config":{"foo":"bar"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/tokens", bytes.NewBufferString(body))
	req = withAdminUser(req, adminUser())
	rec := httptest.NewRecorder()
	createHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", rec.Code, rec.Body.String())
	}
	var created createTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// List tokens.
	listHandler := ListTokensHandler(db)
	listReq := httptest.NewRequest(http.MethodGet, "/api/kiosk/tokens", nil)
	listRec := httptest.NewRecorder()
	listHandler.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}

	var tokens []tokenResponse
	if err := json.NewDecoder(listRec.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	tok := tokens[0]
	if tok.ID != created.ID {
		t.Errorf("ID mismatch: got %d, want %d", tok.ID, created.ID)
	}
	if tok.Name != "list-test" {
		t.Errorf("name mismatch: got %q", tok.Name)
	}
	if tok.CreatedBy != "admin@example.com" {
		t.Errorf("created_by mismatch: got %q", tok.CreatedBy)
	}
}

func TestDeleteTokenHandler_Success(t *testing.T) {
	db := setupAdminTestDB(t)

	// Insert a token directly.
	_, err := db.Exec(
		`INSERT INTO kiosk_tokens (token_hash, name, config, created_by, created_at) VALUES (?, ?, '{}', '', ?)`,
		hashToken("deletetoken"), "to-delete", time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM kiosk_tokens WHERE token_hash = ?", hashToken("deletetoken")).Scan(&id); err != nil {
		t.Fatalf("query inserted token id: %v", err)
	}

	handler := DeleteTokenHandler(db)

	// Use chi test router so URL params are parsed.
	r := chi.NewRouter()
	r.Delete("/api/kiosk/tokens/{id}", handler)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/kiosk/tokens/%d", id), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM kiosk_tokens WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("query count after delete: %v", err)
	}
	if count != 0 {
		t.Error("token should have been deleted")
	}
}

func TestDeleteTokenHandler_NotFound(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := DeleteTokenHandler(db)

	r := chi.NewRouter()
	r.Delete("/api/kiosk/tokens/{id}", handler)

	req := httptest.NewRequest(http.MethodDelete, "/api/kiosk/tokens/9999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteTokenHandler_InvalidID(t *testing.T) {
	db := setupAdminTestDB(t)
	handler := DeleteTokenHandler(db)

	r := chi.NewRouter()
	r.Delete("/api/kiosk/tokens/{id}", handler)

	req := httptest.NewRequest(http.MethodDelete, "/api/kiosk/tokens/notanumber", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
