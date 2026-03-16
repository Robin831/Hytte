package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- Hetzner token CRUD tests ---

func TestGetHetznerToken_Empty(t *testing.T) {
	db := setupTestDB(t)
	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestSetAndGetHetznerToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetHetznerToken(db, 1, "test-token-abc123"); err != nil {
		t.Fatalf("set: %v", err)
	}

	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "test-token-abc123" {
		t.Errorf("expected 'test-token-abc123', got %q", token)
	}
}

func TestSetHetznerToken_Upsert(t *testing.T) {
	db := setupTestDB(t)

	if err := SetHetznerToken(db, 1, "first"); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := SetHetznerToken(db, 1, "second"); err != nil {
		t.Fatalf("set second: %v", err)
	}

	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "second" {
		t.Errorf("expected 'second', got %q", token)
	}
}

func TestDeleteHetznerToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetHetznerToken(db, 1, "to-delete"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := DeleteHetznerToken(db, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty after delete, got %q", token)
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"abcdefghij", "******ghij"},
		{"ab", "****"},
		{"", "****"},
		{"abcd", "****"},
		{"abcde", "*bcde"},
	}
	for _, tt := range tests {
		got := MaskToken(tt.input)
		if got != tt.want {
			t.Errorf("MaskToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- Hetzner module Check tests ---

func TestHetznerModule_NoToken(t *testing.T) {
	db := setupTestDB(t)
	mod := NewHetznerModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no token, got %s", result.Status)
	}
	if result.Name != "hetzner_vps" {
		t.Errorf("expected name hetzner_vps, got %s", result.Name)
	}
}

func TestHetznerModule_APIError(t *testing.T) {
	db := setupTestDB(t)

	// Set up a mock server that returns an unauthorized error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer ts.Close()

	if err := SetHetznerToken(db, 1, "bad-token"); err != nil {
		t.Fatalf("set token: %v", err)
	}

	mod := newHetznerModule(db, ts.URL)
	result := mod.Check(1)

	if result.Status != StatusDown {
		t.Errorf("expected StatusDown for API error, got %s: %s", result.Status, result.Message)
	}
}

// --- Hetzner handler tests ---

func TestHetznerTokenGetHandler_NoToken(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/infra/hetzner/token", nil), 1)
	rec := httptest.NewRecorder()
	HetznerTokenGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Configured bool   `json:"configured"`
		Masked     string `json:"masked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Configured {
		t.Error("expected not configured")
	}
	if body.Masked != "" {
		t.Errorf("expected empty masked, got %q", body.Masked)
	}
}

func TestHetznerTokenSetHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"token":"my-secret-token"}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/hetzner/token", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HetznerTokenSetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "my-secret-token" {
		t.Errorf("expected 'my-secret-token', got %q", token)
	}
}

func TestHetznerTokenSetHandler_EmptyToken(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"token":""}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/hetzner/token", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HetznerTokenSetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty token, got %d", rec.Code)
	}
}

func TestHetznerTokenGetHandler_WithToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetHetznerToken(db, 1, "abcdefghijklmnop"); err != nil {
		t.Fatalf("set: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/hetzner/token", nil), 1)
	rec := httptest.NewRecorder()
	HetznerTokenGetHandler(db).ServeHTTP(rec, req)

	var body struct {
		Configured bool   `json:"configured"`
		Masked     string `json:"masked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Configured {
		t.Error("expected configured")
	}
	// Masked should not contain the full token.
	if body.Masked == "abcdefghijklmnop" {
		t.Error("masked token should not be the full token")
	}
	if !strings.HasSuffix(body.Masked, "mnop") {
		t.Errorf("expected masked to end with last 4 chars, got %q", body.Masked)
	}
}

func TestHetznerTokenDeleteHandler(t *testing.T) {
	db := setupTestDB(t)

	if err := SetHetznerToken(db, 1, "to-delete"); err != nil {
		t.Fatalf("set: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/hetzner/token", nil), 1)
	rec := httptest.NewRecorder()
	HetznerTokenDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	token, err := GetHetznerToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty after delete, got %q", token)
	}
}

// --- Docker host CRUD tests ---

func TestListDockerHosts_Empty(t *testing.T) {
	db := setupTestDB(t)
	hosts, err := ListDockerHosts(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}
}

func TestAddAndListDockerHosts(t *testing.T) {
	db := setupTestDB(t)

	host, err := AddDockerHost(db, 1, "Prod Docker", "https://docker.example.com:2376")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if host.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if host.Name != "Prod Docker" {
		t.Errorf("expected 'Prod Docker', got %q", host.Name)
	}

	hosts, err := ListDockerHosts(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].URL != "https://docker.example.com:2376" {
		t.Errorf("unexpected URL: %s", hosts[0].URL)
	}
}

func TestDeleteDockerHost(t *testing.T) {
	db := setupTestDB(t)

	host, err := AddDockerHost(db, 1, "Test", "https://docker.example.com:2376")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteDockerHost(db, 1, host.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	hosts, err := ListDockerHosts(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts after delete, got %d", len(hosts))
	}
}

func TestDeleteDockerHost_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteDockerHost(db, 1, 999)
	if err == nil {
		t.Error("expected error for non-existent host")
	}
}

// --- Docker module Check tests ---

func TestDockerModule_NoHosts(t *testing.T) {
	db := setupTestDB(t)
	mod := NewDockerModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no hosts, got %s", result.Status)
	}
	if result.Name != "docker" {
		t.Errorf("expected name docker, got %s", result.Name)
	}
}

func TestDockerModule_SSRFBlocked(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddDockerHost(db, 1, "Local Docker", "http://127.0.0.1:2375"); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewDockerModule(db)
	result := mod.Check(1)

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatal("expected details map")
	}
	hosts, ok := details["hosts"].([]DockerHostResult)
	if !ok {
		t.Fatal("expected hosts list")
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host result, got %d", len(hosts))
	}
	if hosts[0].Status != string(StatusDown) {
		t.Errorf("expected down (SSRF blocked), got %s", hosts[0].Status)
	}
	if !strings.Contains(hosts[0].Error, "blocked") {
		t.Errorf("expected 'blocked' in error, got: %s", hosts[0].Error)
	}
}

// --- Docker handler tests ---

func TestListDockerHostsHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddDockerHost(db, 1, "Docker", "https://docker.example.com:2376"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/docker-hosts", nil), 1)
	rec := httptest.NewRecorder()
	ListDockerHostsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Hosts []DockerHost `json:"hosts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(body.Hosts))
	}
}

func TestAddDockerHostHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Prod","url":"https://8.8.8.8:2376"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/docker-hosts", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDockerHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddDockerHostHandler_RejectsLocalhost(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Local","url":"http://localhost:2375"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/docker-hosts", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDockerHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for localhost URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddDockerHostHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"","url":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/docker-hosts", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDockerHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteDockerHostHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	host, err := AddDockerHost(db, 1, "Test", "https://docker.example.com:2376")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	idStr := strconv.FormatInt(host.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/docker-hosts/"+idStr, nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idStr)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteDockerHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteDockerHostHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/docker-hosts/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteDockerHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- Bandwidth module Check tests ---

func TestBandwidthModule_NoToken(t *testing.T) {
	db := setupTestDB(t)
	mod := NewBandwidthModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no token, got %s", result.Status)
	}
	if result.Name != "bandwidth" {
		t.Errorf("expected name bandwidth, got %s", result.Name)
	}
}
