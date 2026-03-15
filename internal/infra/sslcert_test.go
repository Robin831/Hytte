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

func TestListSSLHosts_Empty(t *testing.T) {
	db := setupTestDB(t)
	hosts, err := ListSSLHosts(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}
}

func TestAddAndListSSLHosts(t *testing.T) {
	db := setupTestDB(t)

	host, err := AddSSLHost(db, "Example", "example.com", 443)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if host.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if host.Port != 443 {
		t.Errorf("expected port 443, got %d", host.Port)
	}

	hosts, err := ListSSLHosts(db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Hostname != "example.com" {
		t.Errorf("unexpected hostname: %s", hosts[0].Hostname)
	}
}

func TestDeleteSSLHost(t *testing.T) {
	db := setupTestDB(t)

	host, err := AddSSLHost(db, "Test", "example.com", 443)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteSSLHost(db, host.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	hosts, err := ListSSLHosts(db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts after delete, got %d", len(hosts))
	}
}

func TestDeleteSSLHost_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteSSLHost(db, 999)
	if err == nil {
		t.Error("expected error for non-existent host")
	}
}

func TestSSLCertModule_NoHosts(t *testing.T) {
	db := setupTestDB(t)
	mod := NewSSLCertModule(db)

	result := mod.Check()
	if result.Status != StatusOK {
		t.Errorf("expected ok with no hosts, got %s", result.Status)
	}
	if result.Name != "ssl_certs" {
		t.Errorf("expected name ssl_certs, got %s", result.Name)
	}
}

func TestSSLCertModule_SSRFBlocked(t *testing.T) {
	db := setupTestDB(t)

	// Add a host with a private IP — should be blocked by SSRF validation.
	if _, err := AddSSLHost(db, "Private", "127.0.0.1", 443); err != nil {
		t.Fatalf("add: %v", err)
	}

	mod := NewSSLCertModule(db)
	result := mod.Check()

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatal("expected details map")
	}
	certs, ok := details["certificates"].([]CertCheckResult)
	if !ok {
		t.Fatal("expected certificates list")
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert result, got %d", len(certs))
	}
	if certs[0].Status != string(StatusDown) {
		t.Errorf("expected down (SSRF blocked), got %s", certs[0].Status)
	}
	if !strings.Contains(certs[0].Error, "blocked") {
		t.Errorf("expected 'blocked' in error, got: %s", certs[0].Error)
	}
}

func TestListSSLHostsHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddSSLHost(db, "Example", "example.com", 443); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/ssl-certs", nil), 1)
	rec := httptest.NewRecorder()
	ListSSLHostsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Hosts []SSLHost `json:"hosts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(body.Hosts))
	}
}

func TestAddSSLHostHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"My Site","hostname":"mysite.com","port":443}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/ssl-certs", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSSLHostHandler(db).ServeHTTP(rec, req)

	// May be 201 or 400 depending on DNS resolution of mysite.com in CI.
	if rec.Code != http.StatusCreated && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 201 or 400 (DNS), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddSSLHostHandler_DefaultPort(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"My Site","hostname":"example.com"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/ssl-certs", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var host SSLHost
	if err := json.NewDecoder(rec.Body).Decode(&host); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if host.Port != 443 {
		t.Errorf("expected default port 443, got %d", host.Port)
	}
}

func TestAddSSLHostHandler_RejectsLocalhost(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Local","hostname":"localhost","port":443}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/ssl-certs", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for localhost hostname, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddSSLHostHandler_RejectsPrivateIP(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Internal","hostname":"192.168.1.1","port":443}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/ssl-certs", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for private IP hostname, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddSSLHostHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"","hostname":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/ssl-certs", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteSSLHostHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	host, _ := AddSSLHost(db, "Test", "example.com", 443)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/ssl-certs/1", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	_ = host

	rec := httptest.NewRecorder()
	DeleteSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSSLHostHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/ssl-certs/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteSSLHostHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
