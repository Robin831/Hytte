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

	svc, err := AddHealthService(db, "Test API", "http://localhost:8080/health")
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
	if services[0].URL != "http://localhost:8080/health" {
		t.Errorf("unexpected URL: %s", services[0].URL)
	}
}

func TestDeleteHealthService(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddHealthService(db, "Test", "http://example.com")
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
	if result.Status != StatusOK {
		t.Errorf("expected ok with no services, got %s", result.Status)
	}
	if result.Name != "health_checks" {
		t.Errorf("expected name health_checks, got %s", result.Name)
	}
}

func TestHealthCheckModule_WithServer(t *testing.T) {
	db := setupTestDB(t)

	// Spin up a test HTTP server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	if _, err := AddHealthService(db, "Test Server", ts.URL); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}

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
	if services[0].Status != string(StatusOK) {
		t.Errorf("expected service ok, got %s", services[0].Status)
	}
	if services[0].StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", services[0].StatusCode)
	}
}

func TestHealthCheckModule_DownServer(t *testing.T) {
	db := setupTestDB(t)

	// Spin up a server that returns 500.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	if _, err := AddHealthService(db, "Broken Server", ts.URL); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewHealthCheckModule(db)
	result := mod.Check()

	if result.Status != StatusDown {
		t.Errorf("expected down for 500 server, got %s", result.Status)
	}
}

func TestListHealthServicesHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddHealthService(db, "Svc", "http://example.com"); err != nil {
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

	payload := `{"name":"My API","url":"http://localhost:3000"}`
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
	svc, _ := AddHealthService(db, "Test", "http://example.com")

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
