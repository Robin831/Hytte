package webhooks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	dbpkg "github.com/Robin831/Hytte/internal/db"
	"github.com/go-chi/chi/v5"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := dbpkg.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
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
	id1, err := generateID()
	if err != nil {
		t.Fatalf("generateID: %v", err)
	}
	id2, err := generateID()
	if err != nil {
		t.Fatalf("generateID: %v", err)
	}

	if len(id1) != 16 {
		t.Errorf("expected 16-char hex ID, got %d chars: %s", len(id1), id1)
	}
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

func TestStreamRequests_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	hub := NewHub()

	req := httptest.NewRequest("GET", "/api/webhooks/ep1/stream", nil)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	StreamRequests(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestStreamRequests_NotOwner(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Another user owns this endpoint.
	db.Exec("INSERT INTO users (google_id, email, name) VALUES ('g456', 'other@example.com', 'Other')")
	var otherID int64
	db.QueryRow("SELECT id FROM users WHERE google_id = 'g456'").Scan(&otherID)
	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'NotMine')", otherID)

	hub := NewHub()
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	req := httptest.NewRequest("GET", "/api/webhooks/ep1/stream", nil)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	StreamRequests(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-owned endpoint, got %d", rec.Code)
	}
}

func TestStreamRequests_SSE(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	hub := NewHub()

	db.Exec("INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Test')", userID)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/webhooks/ep1/stream", nil).WithContext(ctx)
	req = withUser(req, user)
	req = withChiParam(req, "endpointID", "ep1")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StreamRequests(db, hub).ServeHTTP(rec, req)
	}()

	// Give the goroutine time to subscribe.
	time.Sleep(20 * time.Millisecond)

	// Publish a request via the hub.
	hub.publish("ep1", &Request{
		ID:         1,
		EndpointID: "ep1",
		Method:     "POST",
		Body:       "hello",
		Headers:    map[string]string{},
	})

	// Give time for the SSE frame to be written.
	time.Sleep(20 * time.Millisecond)

	// Cancel to end the stream.
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Errorf("expected SSE data frame in response, got: %q", body)
	}
	if !strings.Contains(body, `"method":"POST"`) {
		t.Errorf("expected method field in SSE payload, got: %q", body)
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
