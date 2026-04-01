package wordfeud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Use a fixed in-memory encryption key for tests to avoid writing a key file.
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-wordfeud")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
	// In-memory SQLite with a pool can create multiple isolated DBs; force a single connection.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })
	return database
}

func createTestUser(t *testing.T, database *sql.DB) *auth.User {
	t.Helper()
	user, err := auth.UpsertUser(database, "google-123", "test@example.com", "Test User", "")
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

func requestWithUser(r *http.Request, user *auth.User) *http.Request {
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func TestGamesHandler_NoToken(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)
	client := NewClient()

	handler := GamesHandler(database, client)
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/games", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "no Wordfeud session token") {
		t.Errorf("unexpected error message: %s", resp["error"])
	}
}

func TestGamesHandler_WithToken(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	// Set up mock Wordfeud API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"games": []map[string]any{
					{
						"id": 42,
						"players": []map[string]any{
							{"username": "me", "id": 1, "score": 10},
							{"username": "them", "id": 2, "score": 20},
						},
						"is_running":     true,
						"current_player": 0,
						"last_move":      map[string]any{"user_id": 2, "move_type": "move", "points": 15},
					},
				},
			},
		})
	}))
	defer srv.Close()

	encToken, err := encryption.EncryptField("test-token")
	if err != nil {
		t.Fatalf("failed to encrypt test token: %v", err)
	}
	auth.SetPreference(database, user.ID, "wordfeud_session_token", encToken)

	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	handler := GamesHandler(database, client)
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/games", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Games []GameSummary `json:"games"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Games) != 1 {
		t.Fatalf("got %d games, want 1", len(resp.Games))
	}
	if resp.Games[0].ID != 42 {
		t.Errorf("got ID %d, want 42", resp.Games[0].ID)
	}
}

func TestGameHandler_InvalidID(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)
	client := NewClient()
	cache := NewGameCache()

	handler := GameHandler(database, client, cache)

	// Set up chi context with URL param
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/games/abc", nil)
	req = requestWithUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGameHandler_Success(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"game": map[string]any{
					"id":             55,
					"players":        []map[string]any{{"username": "a", "id": 1, "score": 0}, {"username": "b", "id": 2, "score": 0}},
					"tiles":          [][]int{},
					"rack":           [][]int{{1, 1}},
					"is_running":     true,
					"current_player": 0,
					"moves":          []map[string]any{},
				},
			},
		})
	}))
	defer srv.Close()

	encToken, err := encryption.EncryptField("test-token")
	if err != nil {
		t.Fatalf("failed to encrypt test token: %v", err)
	}
	auth.SetPreference(database, user.ID, "wordfeud_session_token", encToken)

	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	cache := NewGameCache()
	handler := GameHandler(database, client, cache)

	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/games/55", nil)
	req = requestWithUser(req, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "55")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)
	client := NewClient()

	handler := LoginHandler(database, client)
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/login", strings.NewReader(`{"email":"","password":""}`))
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestConnectHandler_MissingFields(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)
	client := NewClient()

	handler := ConnectHandler(database, client)
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/connect", strings.NewReader(`{"email":"","password":""}`))
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestConnectHandler_Success(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"id":         12345,
				"session_id": "new-session-token",
			},
		})
	}))
	defer srv.Close()

	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	handler := ConnectHandler(database, client)

	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/connect",
		strings.NewReader(`{"email":"test@example.com","password":"pass"}`))
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "connected" {
		t.Errorf("got status %q, want %q", resp["status"], "connected")
	}

	// Verify credentials were saved and encrypted.
	prefs, _ := auth.GetPreferences(database, user.ID)
	if prefs["wordfeud_session_token"] == "" {
		t.Error("expected wordfeud_session_token to be saved")
	}
	if prefs["wordfeud_email"] == "" {
		t.Error("expected wordfeud_email to be saved")
	}
	if prefs["wordfeud_password"] == "" {
		t.Error("expected wordfeud_password to be saved")
	}
	// Verify they are encrypted (not plaintext).
	if prefs["wordfeud_email"] == "test@example.com" {
		t.Error("wordfeud_email should be encrypted, not plaintext")
	}
	if prefs["wordfeud_password"] == "pass" {
		t.Error("wordfeud_password should be encrypted, not plaintext")
	}
}

func TestConnectHandler_InvalidCredentials(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"content": map[string]any{},
		})
	}))
	defer srv.Close()

	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	handler := ConnectHandler(database, client)

	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/connect",
		strings.NewReader(`{"email":"wrong@example.com","password":"wrong"}`))
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDisconnectHandler(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	// Store credentials first.
	for _, kv := range []struct{ k, v string }{
		{"wordfeud_email", "test@example.com"},
		{"wordfeud_password", "secret"},
		{"wordfeud_session_token", "test-session-token"},
	} {
		enc, err := encryption.EncryptField(kv.v)
		if err != nil {
			t.Fatalf("failed to encrypt %s: %v", kv.k, err)
		}
		auth.SetPreference(database, user.ID, kv.k, enc)
	}

	handler := DisconnectHandler(database)
	req := httptest.NewRequest(http.MethodDelete, "/api/wordfeud/disconnect", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "disconnected" {
		t.Errorf("got status %q, want %q", resp["status"], "disconnected")
	}

	// Verify all credentials were removed.
	prefs, _ := auth.GetPreferences(database, user.ID)
	for _, key := range []string{"wordfeud_email", "wordfeud_password", "wordfeud_session_token"} {
		if prefs[key] != "" {
			t.Errorf("expected %s to be removed", key)
		}
	}
}

func TestDisconnectHandler_NoToken(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	handler := DisconnectHandler(database)
	req := httptest.NewRequest(http.MethodDelete, "/api/wordfeud/disconnect", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should succeed even if no token exists.
	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}
}

func TestStatusHandler_Connected(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	for _, kv := range []struct{ k, v string }{
		{"wordfeud_email", "player@example.com"},
		{"wordfeud_session_token", "test-session-token"},
	} {
		enc, err := encryption.EncryptField(kv.v)
		if err != nil {
			t.Fatalf("failed to encrypt %s: %v", kv.k, err)
		}
		auth.SetPreference(database, user.ID, kv.k, enc)
	}

	handler := StatusHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/status", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["connected"] != true {
		t.Errorf("got connected=%v, want true", resp["connected"])
	}
	if resp["email"] != "player@example.com" {
		t.Errorf("got email=%v, want player@example.com", resp["email"])
	}
}

func TestStatusHandler_Disconnected(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	handler := StatusHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/status", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["connected"] != false {
		t.Errorf("got connected=%v, want false", resp["connected"])
	}
}

func TestLoginHandler_Success(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"id":         12345,
				"session_id": "new-session-token",
			},
		})
	}))
	defer srv.Close()

	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	handler := LoginHandler(database, client)

	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/login",
		strings.NewReader(`{"email":"test@example.com","password":"pass"}`))
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify credentials were saved.
	prefs, _ := auth.GetPreferences(database, user.ID)
	if prefs["wordfeud_session_token"] == "" {
		t.Error("expected wordfeud_session_token to be saved")
	}
	if prefs["wordfeud_email"] == "" {
		t.Error("expected wordfeud_email to be saved")
	}
	if prefs["wordfeud_password"] == "" {
		t.Error("expected wordfeud_password to be saved")
	}
}
