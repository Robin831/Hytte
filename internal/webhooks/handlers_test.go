package webhooks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);
		CREATE TABLE webhook_endpoints (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE webhook_requests (
			id INTEGER PRIMARY KEY,
			endpoint_id TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
			method TEXT NOT NULL,
			headers TEXT NOT NULL DEFAULT '{}',
			body TEXT NOT NULL DEFAULT '',
			query TEXT NOT NULL DEFAULT '',
			remote_addr TEXT NOT NULL DEFAULT '',
			received_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO users (google_id, email, name) VALUES ('g123', 'test@example.com', 'Test')",
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id int64
	err = db.QueryRow("SELECT id FROM users WHERE google_id = 'g123'").Scan(&id)
	if err != nil {
		t.Fatalf("select user: %v", err)
	}
	return id
}

// withUser creates a request with the user set in context (simulating auth middleware).
func withUser(r *http.Request, user *auth.User) *http.Request {
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

// withChiParam adds a chi URL param to the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateEndpoint(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	body := bytes.NewBufferString(`{"name":"My Hook"}`)
	req := httptest.NewRequest("POST", "/api/webhooks", body)
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	CreateEndpoint(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var ep Endpoint
	if err := json.NewDecoder(rec.Body).Decode(&ep); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ep.Name != "My Hook" {
		t.Errorf("expected name 'My Hook', got %q", ep.Name)
	}
	if ep.ID == "" {
		t.Error("expected non-empty endpoint ID")
	}
	// Verify timestamp is RFC3339 format.
	if _, err := time.Parse(time.RFC3339, ep.CreatedAt); err != nil {
		t.Errorf("expected RFC3339 timestamp, got %q: %v", ep.CreatedAt, err)
	}
}

func TestCreateEndpoint_DefaultName(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	req := httptest.NewRequest("POST", "/api/webhooks", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	CreateEndpoint(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var ep Endpoint
	json.NewDecoder(rec.Body).Decode(&ep)
	if ep.Name == "" {
		t.Error("expected default name, got empty")
	}
}

func TestCreateEndpoint_Unauthorized(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest("POST", "/api/webhooks", nil)
	rec := httptest.NewRecorder()

	CreateEndpoint(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestListEndpoints(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	// Insert two endpoints.
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'First')", userID)
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep2', ?, 'Second')", userID)

	req := httptest.NewRequest("GET", "/api/webhooks", nil)
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	ListEndpoints(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if len(result.Endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(result.Endpoints))
	}
}

func TestListEndpoints_IsolatedPerUser(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Create another user.
	db.Exec("INSERT INTO users (google_id, email, name) VALUES ('g456', 'other@example.com', 'Other')")
	var otherID int64
	db.QueryRow("SELECT id FROM users WHERE google_id = 'g456'").Scan(&otherID)

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Mine')", userID)
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep2', ?, 'Theirs')", otherID)

	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	req := httptest.NewRequest("GET", "/api/webhooks", nil)
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	ListEndpoints(db).ServeHTTP(rec, req)

	var result struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if len(result.Endpoints) != 1 {
		t.Errorf("expected 1 endpoint (own only), got %d", len(result.Endpoints))
	}
	if result.Endpoints[0].Name != "Mine" {
		t.Errorf("expected 'Mine', got %q", result.Endpoints[0].Name)
	}
}

func TestDeleteEndpoint(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'ToDelete')", userID)

	req := httptest.NewRequest("DELETE", "/api/webhooks/ep1", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	DeleteEndpoint(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify deleted.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM webhook_endpoints WHERE id = 'ep1'").Scan(&count)
	if count != 0 {
		t.Error("endpoint should be deleted")
	}
}

func TestDeleteEndpoint_NotOwner(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Create another user and their endpoint.
	db.Exec("INSERT INTO users (google_id, email, name) VALUES ('g456', 'other@example.com', 'Other')")
	var otherID int64
	db.QueryRow("SELECT id FROM users WHERE google_id = 'g456'").Scan(&otherID)
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'NotMine')", otherID)

	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	req := httptest.NewRequest("DELETE", "/api/webhooks/ep1", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	DeleteEndpoint(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-owned endpoint, got %d", rec.Code)
	}
}

func TestReceiveWebhook(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	hub := NewHub()

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Test')", userID)

	body := bytes.NewBufferString(`{"event":"test"}`)
	req := httptest.NewRequest("POST", "/api/hooks/ep1?foo=bar", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom", "hello")
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	ReceiveWebhook(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify request was stored.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM webhook_requests WHERE endpoint_id = 'ep1'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 stored request, got %d", count)
	}

	// Verify stored data.
	var method, storedBody, query, headersJSON string
	db.QueryRow("SELECT method, body, query, headers FROM webhook_requests WHERE endpoint_id = 'ep1'").
		Scan(&method, &storedBody, &query, &headersJSON)

	if method != "POST" {
		t.Errorf("expected POST, got %s", method)
	}
	if storedBody != `{"event":"test"}` {
		t.Errorf("expected body '{\"event\":\"test\"}', got %q", storedBody)
	}
	if query != "foo=bar" {
		t.Errorf("expected query 'foo=bar', got %q", query)
	}

	var headers map[string]string
	json.Unmarshal([]byte(headersJSON), &headers)
	if headers["X-Custom"] != "hello" {
		t.Errorf("expected X-Custom header 'hello', got %q", headers["X-Custom"])
	}
}

func TestReceiveWebhook_NonexistentEndpoint(t *testing.T) {
	db := setupTestDB(t)
	hub := NewHub()

	req := httptest.NewRequest("POST", "/api/hooks/nonexistent", nil)
	req = withChiParam(req, "endpointID", "nonexistent")
	rec := httptest.NewRecorder()

	ReceiveWebhook(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestListRequests(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Test')", userID)
	db.Exec(`INSERT INTO webhook_requests (endpoint_id, method, headers, body, query, remote_addr)
		VALUES ('ep1', 'GET', '{"Host":"localhost"}', '', 'a=1', '127.0.0.1')`)
	db.Exec(`INSERT INTO webhook_requests (endpoint_id, method, headers, body, query, remote_addr)
		VALUES ('ep1', 'POST', '{}', '{"x":1}', '', '127.0.0.1')`)

	req := httptest.NewRequest("GET", "/api/webhooks/ep1/requests", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	ListRequests(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result struct {
		Requests []Request `json:"requests"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if len(result.Requests) != 2 {
		t.Errorf("expected 2 requests, got %d", len(result.Requests))
	}
	// Verify timestamps are RFC3339 format.
	for _, r := range result.Requests {
		if _, err := time.Parse(time.RFC3339, r.ReceivedAt); err != nil {
			t.Errorf("expected RFC3339 received_at, got %q: %v", r.ReceivedAt, err)
		}
	}
}

func TestListRequests_NotOwner(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Another user owns this endpoint.
	db.Exec("INSERT INTO users (google_id, email, name) VALUES ('g456', 'other@example.com', 'Other')")
	var otherID int64
	db.QueryRow("SELECT id FROM users WHERE google_id = 'g456'").Scan(&otherID)
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'NotMine')", otherID)

	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	req := httptest.NewRequest("GET", "/api/webhooks/ep1/requests", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	ListRequests(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-owned endpoint, got %d", rec.Code)
	}
}

func TestClearRequests(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Test')", userID)
	db.Exec(`INSERT INTO webhook_requests (endpoint_id, method, headers) VALUES ('ep1', 'GET', '{}')`)
	db.Exec(`INSERT INTO webhook_requests (endpoint_id, method, headers) VALUES ('ep1', 'POST', '{}')`)

	req := httptest.NewRequest("DELETE", "/api/webhooks/ep1/requests", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	ClearRequests(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM webhook_requests WHERE endpoint_id = 'ep1'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 requests after clear, got %d", count)
	}
}

func TestHub_PublishSubscribe(t *testing.T) {
	hub := NewHub()

	ch := hub.subscribe("ep1")

	req := &Request{
		ID:         1,
		EndpointID: "ep1",
		Method:     "POST",
		Body:       "test",
	}
	hub.publish("ep1", req)

	select {
	case got := <-ch:
		if got.Method != "POST" || got.Body != "test" {
			t.Errorf("unexpected request: %+v", got)
		}
	default:
		t.Error("expected to receive published request")
	}

	hub.unsubscribe("ep1", ch)

	// Verify channel was cleaned up.
	hub.mu.RLock()
	_, exists := hub.subscribers["ep1"]
	hub.mu.RUnlock()
	if exists {
		t.Error("expected subscriber map to be cleaned up after last unsubscribe")
	}
}

func TestHub_PublishNoSubscribers(t *testing.T) {
	hub := NewHub()

	// Should not panic.
	hub.publish("ep1", &Request{ID: 1, Method: "GET"})
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if len(id1) != 16 {
		t.Errorf("expected 16-char hex ID, got %d chars: %s", len(id1), id1)
	}
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

func TestToRFC3339(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-03-09 12:30:45", "2026-03-09T12:30:45Z"},
		{"2026-01-01 00:00:00", "2026-01-01T00:00:00Z"},
		{"not-a-timestamp", "not-a-timestamp"}, // Invalid input returned as-is.
	}
	for _, tt := range tests {
		got := toRFC3339(tt.input)
		if got != tt.want {
			t.Errorf("toRFC3339(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
