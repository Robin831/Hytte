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

	// Set up a mock server that returns an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer ts.Close()

	if err := SetHetznerToken(db, 1, "bad-token"); err != nil {
		t.Fatalf("set token: %v", err)
	}

	mod := NewHetznerModule(db)
	mod.baseURL = ts.URL // point at mock server instead of real Hetzner API

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

func TestBandwidthModule_Success(t *testing.T) {
	db := setupTestDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"servers": []map[string]any{
				{
					"id":               1,
					"name":             "web-01",
					"included_traffic": int64(2_000_000_000_000), // 2 TB
					"ingoing_traffic":  int64(100_000_000_000),   // 0.1 TB
					"outgoing_traffic": int64(500_000_000_000),   // 0.5 TB → 25% of 2 TB
				},
				{
					"id":               2,
					"name":             "db-01",
					"included_traffic": int64(1_000_000_000_000), // 1 TB
					"ingoing_traffic":  int64(0),
					"outgoing_traffic": int64(850_000_000_000),  // 0.85 TB → 85% → StatusDegraded
				},
			},
		})
	}))
	defer ts.Close()

	if err := SetHetznerToken(db, 1, "test-token"); err != nil {
		t.Fatal(err)
	}

	mod := &BandwidthModule{
		db:      db,
		baseURL: ts.URL,
		client:  &http.Client{},
	}

	result := mod.Check(1)
	// One server at 85% → overall StatusDegraded
	if result.Status != StatusDegraded {
		t.Errorf("expected degraded (one server >80%%), got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any details, got %T", result.Details)
	}
	servers, ok := details["servers"].([]BandwidthServer)
	if !ok {
		t.Fatalf("expected []BandwidthServer in details, got %T", details["servers"])
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}

	// web-01: 500 GB outgoing / 2 TB included = 25%
	if servers[0].Name != "web-01" {
		t.Errorf("expected web-01, got %s", servers[0].Name)
	}
	if got := servers[0].UsagePercent; got < 24.9 || got > 25.1 {
		t.Errorf("web-01 usage: expected ~25%%, got %.2f%%", got)
	}

	// db-01: 850 GB / 1 TB = 85%
	if servers[1].Name != "db-01" {
		t.Errorf("expected db-01, got %s", servers[1].Name)
	}
	if got := servers[1].UsagePercent; got < 84.9 || got > 85.1 {
		t.Errorf("db-01 usage: expected ~85%%, got %.2f%%", got)
	}
}

// --- Docker module success path test ---

func TestDockerModule_CheckHost_Success(t *testing.T) {
	db := setupTestDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id":     "abc123def456789012",
				"Names":  []string{"/web"},
				"Image":  "nginx:latest",
				"State":  "running",
				"Status": "Up 2 hours",
			},
			{
				"Id":     "deadbeef00112233",
				"Names":  []string{"/worker"},
				"Image":  "myapp:v1",
				"State":  "exited",
				"Status": "Exited (0) 1 hour ago",
			},
		})
	}))
	defer ts.Close()

	// Insert Docker host directly to bypass ValidateServiceURL (test server is localhost).
	_, err := db.Exec(
		`INSERT INTO infra_docker_hosts (user_id, name, url, created_at) VALUES (?, ?, ?, ?)`,
		1, "test-host", ts.URL, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}

	mod := &DockerModule{
		db:          db,
		client:      &http.Client{},
		validateURL: func(string) error { return nil }, // bypass SSRF check for test server
	}

	result := mod.Check(1)
	// One container is exited → StatusDegraded
	if result.Status != StatusDegraded {
		t.Errorf("expected degraded (one container exited), got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any details, got %T", result.Details)
	}
	hosts, ok := details["hosts"].([]DockerHostResult)
	if !ok {
		t.Fatalf("expected []DockerHostResult in details, got %T", details["hosts"])
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host result, got %d", len(hosts))
	}

	containers := hosts[0].Containers
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	// Verify ID truncation to 12 chars.
	if containers[0].ID != "abc123def456" {
		t.Errorf("expected truncated ID 'abc123def456', got %q", containers[0].ID)
	}
	if containers[1].ID != "deadbeef0011" {
		t.Errorf("expected truncated ID 'deadbeef0011', got %q", containers[1].ID)
	}

	// Verify name trimming (leading slash removed).
	if containers[0].Name != "web" {
		t.Errorf("expected name 'web', got %q", containers[0].Name)
	}
	if containers[1].Name != "worker" {
		t.Errorf("expected name 'worker', got %q", containers[1].Name)
	}

	if containers[0].State != "running" {
		t.Errorf("expected running, got %q", containers[0].State)
	}
	if containers[1].State != "exited" {
		t.Errorf("expected exited, got %q", containers[1].State)
	}
}
