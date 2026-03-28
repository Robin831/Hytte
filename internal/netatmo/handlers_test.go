package netatmo

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

// setupHandlerTestDB opens an in-memory SQLite database with the tables needed
// to test the Netatmo HTTP handlers.
func setupHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id      INTEGER PRIMARY KEY,
			email   TEXT NOT NULL,
			name    TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE netatmo_readings (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			timestamp   TEXT NOT NULL,
			module_type TEXT NOT NULL,
			metric      TEXT NOT NULL,
			value       REAL NOT NULL
		);
		CREATE INDEX idx_netatmo_readings_user_ts ON netatmo_readings(user_id, timestamp);
		INSERT INTO users (id, email) VALUES (1, 'test@example.com');
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// injectCache pre-populates the client cache so GetStationsData returns cached
// data without making a real HTTP request.
func injectCache(c *Client, userID int64, readings *ModuleReadings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[userID] = cacheEntry{readings: readings, fetchedAt: time.Now()}
}

func requestWithUser(userID int64) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	u := &auth.User{ID: userID}
	return r.WithContext(auth.ContextWithUser(r.Context(), u))
}

func newStubClient(db *sql.DB) *Client {
	return &Client{
		oauth:      &OAuthClient{},
		db:         db,
		cache:      make(map[int64]cacheEntry),
		httpClient: nil, // not used when cache is populated
	}
}

func TestCurrentHandler_Success(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 21.5, Humidity: 50, CO2: 800, Noise: 35, Pressure: 1013.0},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := CurrentHandler(client, sqlDB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(1))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got ModuleReadings
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Indoor == nil {
		t.Fatal("expected indoor readings in response")
	}
	if got.Indoor.Temperature != 21.5 {
		t.Errorf("temperature: got %v, want 21.5", got.Indoor.Temperature)
	}
}

func TestCurrentHandler_APIError(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	// Client with no cache entry and no configured OAuth/token storage in the
	// test DB — GetStationsData will fail when trying to access Netatmo data.
	client := newStubClient(sqlDB)

	h := CurrentHandler(client, sqlDB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(99))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHistoryHandler_Success(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	// Pre-populate history with a stored reading.
	seed := ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 20.0, Humidity: 45},
		FetchedAt: time.Now().Add(-time.Hour),
	}
	if err := StoreReadings(sqlDB, 1, seed); err != nil {
		t.Fatalf("store seed: %v", err)
	}

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 21.0, Humidity: 48},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := HistoryHandler(client, sqlDB)
	w := httptest.NewRecorder()
	req := requestWithUser(1)
	req.URL.RawQuery = "hours=24"
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Readings []Reading `json:"readings"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Readings) == 0 {
		t.Fatal("expected readings in response, got none")
	}
}

func TestHistoryHandler_InvalidHoursDefaultsTo24(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 22.0},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := HistoryHandler(client, sqlDB)
	w := httptest.NewRecorder()
	req := requestWithUser(1)
	req.URL.RawQuery = "hours=notanumber"
	h.ServeHTTP(w, req)

	// Should default to 24 hours and succeed.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- OAuth handler tests ---

func TestOAuthLoginHandler_NotConfigured(t *testing.T) {
	unconfigured := &OAuthClient{} // no clientID or clientSecret
	h := OAuthLoginHandler(unconfigured)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/auth/login", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOAuthLoginHandler_Redirect(t *testing.T) {
	c := NewOAuthClient("test-client-id", "test-secret", "http://localhost/callback")
	h := OAuthLoginHandler(c)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/auth/login", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "api.netatmo.com") {
		t.Errorf("redirect location %q doesn't contain api.netatmo.com", location)
	}
	var stateCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "netatmo_state" {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("expected netatmo_state cookie to be set")
	}
	if stateCookie.Value == "" {
		t.Error("netatmo_state cookie should not be empty")
	}
}

func TestOAuthCallbackHandler_NoStateCookie(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := NewOAuthClient("id", "secret", "http://localhost/cb")
	h := OAuthCallbackHandler(c, db)

	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/callback?state=abc&code=xyz", nil)
	r = r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: 1}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "netatmo=error") {
		t.Errorf("expected redirect to error, got %q", w.Header().Get("Location"))
	}
}

func TestOAuthCallbackHandler_StateMismatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := NewOAuthClient("id", "secret", "http://localhost/cb")
	h := OAuthCallbackHandler(c, db)

	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/callback?state=wrong&code=xyz", nil)
	r.AddCookie(&http.Cookie{Name: "netatmo_state", Value: "correct"})
	r = r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: 1}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "netatmo=error") {
		t.Errorf("expected redirect to error, got %q", w.Header().Get("Location"))
	}
}

func TestOAuthCallbackHandler_NoCode(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := NewOAuthClient("id", "secret", "http://localhost/cb")
	h := OAuthCallbackHandler(c, db)

	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/callback?state=abc", nil)
	r.AddCookie(&http.Cookie{Name: "netatmo_state", Value: "abc"})
	r = r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: 1}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "netatmo=error") {
		t.Errorf("expected redirect to error, got %q", w.Header().Get("Location"))
	}
}

func TestOAuthCallbackHandler_ExchangeError(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Token endpoint returns an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		redirectURL:  "http://localhost/cb",
		httpClient:   &http.Client{Transport: &redirectTransport{base: srv.URL}},
	}
	h := OAuthCallbackHandler(c, db)

	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/callback?state=abc&code=bad-code", nil)
	r.AddCookie(&http.Cookie{Name: "netatmo_state", Value: "abc"})
	r = r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: 1}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "netatmo=error") {
		t.Errorf("expected redirect to error, got %q", w.Header().Get("Location"))
	}
}

func TestOAuthCallbackHandler_Success(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-callback-success-handler")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	c := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		redirectURL:  "http://localhost/cb",
		httpClient:   &http.Client{Transport: &redirectTransport{base: srv.URL}},
	}
	h := OAuthCallbackHandler(c, db)

	r := httptest.NewRequest(http.MethodGet, "/api/netatmo/callback?state=mystate&code=mycode", nil)
	r.AddCookie(&http.Cookie{Name: "netatmo_state", Value: "mystate"})
	r = r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: userID}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Location"), "netatmo=connected") {
		t.Errorf("expected redirect to connected, got %q", w.Header().Get("Location"))
	}

	has, err := HasToken(db, userID)
	if err != nil {
		t.Fatalf("HasToken: %v", err)
	}
	if !has {
		t.Error("expected token to be saved after successful callback")
	}
}

func TestOAuthStatusHandler_NotConnected(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	h := OAuthStatusHandler(db)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(9999))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["connected"] {
		t.Error("expected connected=false for user with no token")
	}
}

func TestOAuthStatusHandler_Connected(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-status-connected-handler")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	if err := SaveToken(db, userID, &NetatmoToken{AccessToken: "tok", RefreshToken: "ref"}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := OAuthStatusHandler(db)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(userID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp["connected"] {
		t.Error("expected connected=true for user with token")
	}
}

func TestOAuthDisconnectHandler_Success(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-disconnect-success-handler")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	if err := SaveToken(db, userID, &NetatmoToken{AccessToken: "tok", RefreshToken: "ref"}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := OAuthDisconnectHandler(db)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(userID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "disconnected" {
		t.Errorf("status: got %q, want %q", resp["status"], "disconnected")
	}

	has, err := HasToken(db, userID)
	if err != nil {
		t.Fatalf("HasToken: %v", err)
	}
	if has {
		t.Error("expected token to be deleted after disconnect")
	}
}
