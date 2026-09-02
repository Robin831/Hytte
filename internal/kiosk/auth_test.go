package kiosk

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec(`
		CREATE TABLE kiosk_tokens (
			id           INTEGER PRIMARY KEY,
			token_hash   TEXT NOT NULL UNIQUE,
			config       TEXT NOT NULL DEFAULT '{}',
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

func insertToken(t *testing.T, db *sql.DB, token, config string, expiresAt *string) {
	t.Helper()
	hash := hashToken(token)
	if expiresAt == nil {
		_, err := db.Exec(
			"INSERT INTO kiosk_tokens (token_hash, config) VALUES (?, ?)",
			hash, config,
		)
		if err != nil {
			t.Fatalf("insert token: %v", err)
		}
	} else {
		_, err := db.Exec(
			"INSERT INTO kiosk_tokens (token_hash, config, expires_at) VALUES (?, ?, ?)",
			hash, config, *expiresAt,
		)
		if err != nil {
			t.Fatalf("insert token with expiry: %v", err)
		}
	}
}

func ptr(s string) *string { return &s }

func TestKioskAuth_MissingToken(t *testing.T) {
	db := setupTestDB(t)
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/kiosk", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestKioskAuth_InvalidToken(t *testing.T) {
	db := setupTestDB(t)
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=badtoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestKioskAuth_ValidToken(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "mytoken", `{"screen":"main"}`, nil)

	var gotCfg KioskConfig
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCfg = GetKioskConfig(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=mytoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotCfg == nil {
		t.Fatal("expected kiosk config in context")
	}
	if gotCfg["screen"] != "main" {
		t.Errorf("expected screen=main, got %v", gotCfg["screen"])
	}
}

// The kiosk SPA authenticates exclusively via the Authorization header (see
// web/src/pages/KioskPage.tsx) so the token never reaches reverse-proxy access
// logs; the query parameter is only a fallback for bookmarked kiosk URLs.
func TestKioskAuth_BearerHeader(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "headertoken", `{"screen":"main"}`, nil)

	var gotCfg KioskConfig
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCfg = GetKioskConfig(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// No ?token= at all — the header is the only credential.
	req := httptest.NewRequest("GET", "/kiosk", nil)
	req.Header.Set("Authorization", "Bearer headertoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotCfg == nil {
		t.Fatal("expected kiosk config in context")
	}
	if gotCfg["screen"] != "main" {
		t.Errorf("expected screen=main, got %v", gotCfg["screen"])
	}
}

func TestKioskAuth_BearerHeaderTakesPrecedenceOverQuery(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "headertoken", `{"screen":"header"}`, nil)
	insertToken(t, db, "querytoken", `{"screen":"query"}`, nil)

	var gotCfg KioskConfig
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCfg = GetKioskConfig(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=querytoken", nil)
	req.Header.Set("Authorization", "Bearer headertoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotCfg["screen"] != "header" {
		t.Errorf("expected the header token's config (screen=header), got %v", gotCfg["screen"])
	}
}

// A malformed Authorization header must not shadow a usable ?token= value, and
// must not authenticate on its own.
func TestKioskAuth_NonBearerHeaderFallsBackToQuery(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "querytoken", `{"screen":"query"}`, nil)

	var gotCfg KioskConfig
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCfg = GetKioskConfig(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=querytoken", nil)
	req.Header.Set("Authorization", "Basic querytoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotCfg["screen"] != "query" {
		t.Errorf("expected the query token's config (screen=query), got %v", gotCfg["screen"])
	}
}

func TestKioskAuth_BearerHeaderInvalidToken(t *testing.T) {
	db := setupTestDB(t)
	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/kiosk", nil)
	req.Header.Set("Authorization", "Bearer badtoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestKioskAuth_ExpiredToken(t *testing.T) {
	db := setupTestDB(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	insertToken(t, db, "expiredtoken", `{}`, ptr(past))

	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for expired token")
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=expiredtoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "token expired" {
		t.Errorf("expected 'token expired', got %q", body["error"])
	}
}

func TestKioskAuth_FutureExpiry(t *testing.T) {
	db := setupTestDB(t)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	insertToken(t, db, "futuretoken", `{}`, ptr(future))

	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=futuretoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestKioskAuth_MalformedExpiry(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "malformedtoken", `{}`, ptr("not-a-date"))

	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for malformed expiry")
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=malformedtoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestKioskAuth_UpdatesLastUsedAt(t *testing.T) {
	db := setupTestDB(t)
	insertToken(t, db, "tracktoken", `{}`, nil)

	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=tracktoken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var lastUsedAt sql.NullString
	err := db.QueryRow(
		"SELECT last_used_at FROM kiosk_tokens WHERE token_hash = ?",
		hashToken("tracktoken"),
	).Scan(&lastUsedAt)
	if err != nil {
		t.Fatalf("query last_used_at: %v", err)
	}
	if !lastUsedAt.Valid || lastUsedAt.String == "" {
		t.Error("expected last_used_at to be set after successful auth")
	}
}

func TestKioskAuth_SQLiteDatetimeFormat(t *testing.T) {
	db := setupTestDB(t)
	// SQLite datetime format, expired
	insertToken(t, db, "sqliteexpired", `{}`, ptr("2000-01-01 00:00:00"))

	handler := KioskAuth(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for expired token")
	}))

	req := httptest.NewRequest("GET", "/kiosk?token=sqliteexpired", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestGetKioskConfig_NotInContext(t *testing.T) {
	cfg := GetKioskConfig(context.Background())
	if cfg != nil {
		t.Errorf("expected nil config from empty context, got %v", cfg)
	}
}
