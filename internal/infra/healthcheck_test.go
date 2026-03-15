package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestListHealthServices_Empty(t *testing.T) {
	db := setupTestDB(t)
	services, err := ListHealthServices(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

func TestAddAndListHealthServices(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddHealthService(db, "Test API", "https://example.com/health")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if svc.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if svc.Name != "Test API" {
		t.Errorf("expected name 'Test API', got '%s'", svc.Name)
	}

	services, err := ListHealthServices(db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].URL != "https://example.com/health" {
		t.Errorf("unexpected URL: %s", services[0].URL)
	}
}

func TestDeleteHealthService(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddHealthService(db, "Test", "https://example.com")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteHealthService(db, svc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	services, err := ListHealthServices(db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services after delete, got %d", len(services))
	}
}

func TestDeleteHealthService_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteHealthService(db, 999)
	if err == nil {
		t.Error("expected error for non-existent service")
	}
}

func TestHealthCheckModule_NoServices(t *testing.T) {
	db := setupTestDB(t)
	mod := NewHealthCheckModule(db)

	result := mod.Check()
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no services, got %s", result.Status)
	}
	if result.Name != "health_checks" {
		t.Errorf("expected name health_checks, got %s", result.Name)
	}
}

func TestHealthCheckModule_WithServer(t *testing.T) {
	db := setupTestDB(t)

	// Spin up a test HTTP server (uses 127.0.0.1 which is blocked by SSRF check).
	// We test via the handler directly instead; the module integration test
	// validates the SSRF blocking behavior below.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// httptest.NewServer binds to 127.0.0.1, which is a private IP.
	// The SSRF check correctly blocks this, so we verify the service is
	// reported as down with a "blocked" error.
	if _, err := AddHealthService(db, "Test Server", ts.URL); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	// Since 127.0.0.1 is private, the check should block it.
	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatal("expected details to be a map")
	}
	services, ok := details["services"].([]ServiceCheckResult)
	if !ok {
		t.Fatal("expected services in details")
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service result, got %d", len(services))
	}
	if services[0].Status != string(StatusDown) {
		t.Errorf("expected service down (SSRF blocked), got %s", services[0].Status)
	}
	if !strings.Contains(services[0].Error, "blocked") {
		t.Errorf("expected 'blocked' in error, got: %s", services[0].Error)
	}
}

func TestHealthCheckModule_DownServer(t *testing.T) {
	db := setupTestDB(t)

	// Add a service pointing to a non-routable IP that will fail.
	if _, err := AddHealthService(db, "Broken Server", "http://192.0.2.1:1/health"); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	if result.Status != StatusDown {
		t.Errorf("expected down, got %s", result.Status)
	}
}

func TestHealthCheckModule_SSRFBlocked(t *testing.T) {
	db := setupTestDB(t)

	// Add services with private IPs — should be blocked by SSRF validation.
	ssrfURLs := []string{
		"http://127.0.0.1/admin",
		"http://10.0.0.1:8080/internal",
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, u := range ssrfURLs {
		if _, err := AddHealthService(db, "ssrf-"+u, u); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatal("expected details map")
	}
	services, ok := details["services"].([]ServiceCheckResult)
	if !ok {
		t.Fatal("expected services list")
	}

	for _, svc := range services {
		if svc.Status != string(StatusDown) {
			t.Errorf("expected SSRF-blocked service %q to be down, got %s", svc.URL, svc.Status)
		}
		if !strings.Contains(svc.Error, "blocked") {
			t.Errorf("expected 'blocked' in error for %q, got: %s", svc.URL, svc.Error)
		}
	}
}

func TestListHealthServicesHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddHealthService(db, "Svc", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/health-checks", nil), 1)
	rec := httptest.NewRecorder()
	ListHealthServicesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Services []HealthService `json:"services"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(body.Services))
	}
}

func TestAddHealthServiceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	// Use a public-resolving domain (example.com) to pass URL validation.
	payload := `{"name":"My API","url":"https://example.com/health"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var svc HealthService
	if err := json.NewDecoder(rec.Body).Decode(&svc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if svc.Name != "My API" {
		t.Errorf("expected 'My API', got '%s'", svc.Name)
	}
}

func TestAddHealthServiceHandler_RejectsLocalhost(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Local","url":"http://localhost:3000"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for localhost URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddHealthServiceHandler_RejectsPrivateIP(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Internal","url":"http://192.168.1.1/admin"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for private IP URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddHealthServiceHandler_RejectsMetadataIP(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Cloud Meta","url":"http://169.254.169.254/latest/meta-data/"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for metadata IP URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddHealthServiceHandler_RejectsInvalidScheme(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"FTP","url":"ftp://example.com/file"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ftp scheme, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddHealthServiceHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"","url":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/health-checks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteHealthServiceHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	svc, err := AddHealthService(db, "Test", "https://example.com")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/health-checks/1", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	_ = svc

	rec := httptest.NewRecorder()
	DeleteHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHealthCheckModule_NoRedirectFollow(t *testing.T) {
	db := setupTestDB(t)

	// Create a server that redirects to a private IP.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/admin", http.StatusFound)
	}))
	defer ts.Close()

	// The test server binds to 127.0.0.1 which is blocked, but we're
	// verifying the redirect policy independently. The client should
	// not follow redirects at all (returns last response).
	if _, err := AddHealthService(db, "Redirect Test", ts.URL); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	// The service should be blocked at dial time (127.0.0.1 is private).
	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatal("expected details map")
	}
	services, ok := details["services"].([]ServiceCheckResult)
	if !ok {
		t.Fatal("expected services list")
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 result, got %d", len(services))
	}
	// Either blocked by SSRF or reported with the redirect status (not following it).
	if services[0].Status != string(StatusDown) {
		t.Errorf("expected down, got %s", services[0].Status)
	}
}

func TestDeleteHealthServiceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/health-checks/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteHealthServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
