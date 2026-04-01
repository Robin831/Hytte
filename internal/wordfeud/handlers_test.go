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
	"github.com/go-chi/chi/v5"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
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

	// Store a plain-text token (simulating unencrypted for test simplicity)
	auth.SetPreference(database, user.ID, "wordfeud_session_token", "test-token")

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

	auth.SetPreference(database, user.ID, "wordfeud_session_token", "test-token")

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

	// Verify token was saved
	prefs, _ := auth.GetPreferences(database, user.ID)
	if prefs["wordfeud_session_token"] == "" {
		t.Error("expected wordfeud_session_token to be saved")
	}
}
