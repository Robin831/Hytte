package netatmo

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id       INTEGER PRIMARY KEY,
			email    TEXT NOT NULL,
			name     TEXT NOT NULL DEFAULT '',
			picture  TEXT NOT NULL DEFAULT '',
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE netatmo_oauth_tokens (
			user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			access_token  TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			expiry        TEXT NOT NULL DEFAULT '',
			updated_at    TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

func insertTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users (email, name) VALUES ('test@example.com', 'Test User')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestSaveAndLoadToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-oauth-token-storage")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	expiry := time.Now().Add(3600 * time.Second).UTC().Truncate(time.Second)
	original := &NetatmoToken{
		AccessToken:  "access-abc123",
		RefreshToken: "refresh-xyz789",
		Expiry:       expiry,
	}

	if err := SaveToken(db, userID, original); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	loaded, err := LoadToken(db, userID)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadToken returned nil, want token")
	}
	if loaded.AccessToken != original.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, original.AccessToken)
	}
	if loaded.RefreshToken != original.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, original.RefreshToken)
	}
	if !loaded.Expiry.Equal(expiry) {
		t.Errorf("Expiry: got %v, want %v", loaded.Expiry, expiry)
	}
}

func TestLoadToken_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	token, err := LoadToken(db, 9999)
	if err != nil {
		t.Fatalf("LoadToken: unexpected error: %v", err)
	}
	if token != nil {
		t.Errorf("LoadToken: got %v, want nil", token)
	}
}

func TestHasToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-has-token")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	has, err := HasToken(db, userID)
	if err != nil {
		t.Fatalf("HasToken: %v", err)
	}
	if has {
		t.Error("HasToken: got true before saving, want false")
	}

	if err := SaveToken(db, userID, &NetatmoToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	has, err = HasToken(db, userID)
	if err != nil {
		t.Fatalf("HasToken: %v", err)
	}
	if !has {
		t.Error("HasToken: got false after saving, want true")
	}
}

func TestDeleteToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-delete-token")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	if err := SaveToken(db, userID, &NetatmoToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	if err := DeleteToken(db, userID); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	token, err := LoadToken(db, userID)
	if err != nil {
		t.Fatalf("LoadToken after delete: %v", err)
	}
	if token != nil {
		t.Error("LoadToken after delete: got token, want nil")
	}
}

func TestSaveToken_Upsert(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-upsert")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	first := &NetatmoToken{AccessToken: "first-access", RefreshToken: "first-refresh"}
	if err := SaveToken(db, userID, first); err != nil {
		t.Fatalf("first SaveToken: %v", err)
	}

	second := &NetatmoToken{AccessToken: "second-access", RefreshToken: "second-refresh"}
	if err := SaveToken(db, userID, second); err != nil {
		t.Fatalf("second SaveToken: %v", err)
	}

	loaded, err := LoadToken(db, userID)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded.AccessToken != "second-access" {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, "second-access")
	}
}

func TestTokenIsExpired(t *testing.T) {
	past := &NetatmoToken{Expiry: time.Now().Add(-1 * time.Hour)}
	if !past.IsExpired() {
		t.Error("expected past token to be expired")
	}

	future := &NetatmoToken{Expiry: time.Now().Add(2 * time.Hour)}
	if future.IsExpired() {
		t.Error("expected future token to not be expired")
	}

	zero := &NetatmoToken{}
	if zero.IsExpired() {
		t.Error("expected zero expiry token to not be expired")
	}
}

func TestAuthorizationURL(t *testing.T) {
	c := NewOAuthClient("my-client-id", "my-secret", "http://localhost/callback")
	u := c.AuthorizationURL("random-state")

	if !strings.Contains(u, "client_id=my-client-id") {
		t.Errorf("URL missing client_id: %s", u)
	}
	if !strings.Contains(u, "read_station") {
		t.Errorf("URL missing read_station scope: %s", u)
	}
	if !strings.Contains(u, "state=random-state") {
		t.Errorf("URL missing state: %s", u)
	}
}

func TestExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "wrong grant_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "test-code" {
			http.Error(w, "wrong code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    10800,
		})
	}))
	defer srv.Close()

	c := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		redirectURL:  "http://localhost/cb",
		httpClient:   srv.Client(),
	}
	// Override the token URL by replacing httpClient with one pointing at the test server.
	// We patch doTokenRequest indirectly by swapping the URL in our test helper.
	// Since tokenURL is a package-level const, we test via a custom server that
	// mimics the same protocol.

	// Re-create using a transport that redirects to test server.
	c.httpClient = &http.Client{
		Transport: &redirectTransport{base: srv.URL},
	}

	token, err := c.ExchangeCode(context.Background(), "test-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "new-access" {
		t.Errorf("AccessToken: got %q, want %q", token.AccessToken, "new-access")
	}
	if token.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken: got %q, want %q", token.RefreshToken, "new-refresh")
	}
	if token.Expiry.IsZero() {
		t.Error("Expiry should not be zero")
	}
}

func TestRefreshToken_PreservesRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response without a new refresh token (Netatmo may not return one).
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-access",
			"expires_in":   10800,
		})
	}))
	defer srv.Close()

	c := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		redirectURL:  "http://localhost/cb",
		httpClient: &http.Client{
			Transport: &redirectTransport{base: srv.URL},
		},
	}

	token, err := c.RefreshToken(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if token.AccessToken != "refreshed-access" {
		t.Errorf("AccessToken: got %q, want %q", token.AccessToken, "refreshed-access")
	}
	// RefreshToken field is empty from the server — caller should preserve old one.
	if token.RefreshToken != "" {
		t.Errorf("RefreshToken: expected empty from server, got %q", token.RefreshToken)
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	s2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if s1 == s2 {
		t.Error("GenerateState returned the same value twice")
	}
	if len(s1) != 32 {
		t.Errorf("GenerateState length: got %d, want 32", len(s1))
	}
}

// redirectTransport rewrites request URLs to point at a test server.
type redirectTransport struct {
	base string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := rt.base + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	r2 := req.Clone(req.Context())
	u, _ := url.Parse(newURL)
	r2.URL = u
	return http.DefaultTransport.RoundTrip(r2)
}

