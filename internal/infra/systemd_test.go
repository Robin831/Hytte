package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// mockSystemdChecker returns configured results for testing.
type mockSystemdChecker struct {
	states map[string][2]string // unit -> [activeState, subState]
	err    error
}

func (m *mockSystemdChecker) UnitStatus(unit string) (string, string, error) {
	if m.err != nil {
		return "", "", m.err
	}
	if state, ok := m.states[unit]; ok {
		return state[0], state[1], nil
	}
	return "", "", fmt.Errorf("unit %s not found", unit)
}

// --- CRUD tests ---

func TestListSystemdServices_Empty(t *testing.T) {
	db := setupTestDB(t)
	services, err := ListSystemdServices(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

func TestAddAndListSystemdServices(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddSystemdService(db, 1, "Nginx", "nginx.service")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if svc.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if svc.Unit != "nginx.service" {
		t.Errorf("unexpected unit: %s", svc.Unit)
	}

	services, err := ListSystemdServices(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
}

func TestDeleteSystemdService(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddSystemdService(db, 1, "Docker", "docker.service")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteSystemdService(db, 1, svc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	services, err := ListSystemdServices(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services after delete, got %d", len(services))
	}
}

func TestDeleteSystemdService_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteSystemdService(db, 1, 999)
	if err == nil {
		t.Error("expected error for non-existent service")
	}
}

func TestDeleteSystemdService_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	svc, err := AddSystemdService(db, 1, "Test", "test.service")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// User 2 should not be able to delete user 1's service.
	err = DeleteSystemdService(db, 2, svc.ID)
	if err == nil {
		t.Error("expected error when deleting another user's service")
	}
}

// --- Module Check tests ---

func TestSystemdModule_NoServices(t *testing.T) {
	db := setupTestDB(t)
	mod := NewSystemdModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no services, got %s", result.Status)
	}
	if result.Name != "systemd" {
		t.Errorf("expected name systemd, got %s", result.Name)
	}
}

func TestSystemdModule_AllActive(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddSystemdService(db, 1, "Nginx", "nginx.service"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddSystemdService(db, 1, "Docker", "docker.service"); err != nil {
		t.Fatal(err)
	}

	mod := &SystemdModule{
		db: db,
		checker: &mockSystemdChecker{
			states: map[string][2]string{
				"nginx.service":  {"active", "running"},
				"docker.service": {"active", "running"},
			},
		},
	}

	result := mod.Check(1)
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	services, ok := details["services"].([]SystemdServiceResult)
	if !ok {
		t.Fatalf("expected []SystemdServiceResult, got %T", details["services"])
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 results, got %d", len(services))
	}
	for _, svc := range services {
		if svc.Status != string(StatusOK) {
			t.Errorf("expected ok for %s, got %s", svc.Unit, svc.Status)
		}
	}
}

func TestSystemdModule_SomeFailed(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddSystemdService(db, 1, "Nginx", "nginx.service"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddSystemdService(db, 1, "Broken", "broken.service"); err != nil {
		t.Fatal(err)
	}

	mod := &SystemdModule{
		db: db,
		checker: &mockSystemdChecker{
			states: map[string][2]string{
				"nginx.service":  {"active", "running"},
				"broken.service": {"failed", "failed"},
			},
		},
	}

	result := mod.Check(1)
	if result.Status != StatusDegraded {
		t.Errorf("expected degraded with one failed, got %s: %s", result.Status, result.Message)
	}
}

func TestSystemdModule_AllFailed(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddSystemdService(db, 1, "Bad", "bad.service"); err != nil {
		t.Fatal(err)
	}

	mod := &SystemdModule{
		db: db,
		checker: &mockSystemdChecker{
			err: fmt.Errorf("systemctl not available"),
		},
	}

	result := mod.Check(1)
	if result.Status != StatusDown {
		t.Errorf("expected down when all fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSystemdModule_Reloading(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddSystemdService(db, 1, "Reloading", "nginx.service"); err != nil {
		t.Fatal(err)
	}

	mod := &SystemdModule{
		db: db,
		checker: &mockSystemdChecker{
			states: map[string][2]string{
				"nginx.service": {"reloading", "reload"},
			},
		},
	}

	result := mod.Check(1)
	// A reloading service marks the module as degraded (not OK).
	if result.Status != StatusDegraded {
		t.Errorf("expected module status degraded when a service is reloading, got %s", result.Status)
	}
	details := result.Details.(map[string]any)
	services := details["services"].([]SystemdServiceResult)
	if services[0].Status != string(StatusDegraded) {
		t.Errorf("expected degraded for reloading service, got %s", services[0].Status)
	}
}

// --- Validation tests ---

func TestValidateUnitName(t *testing.T) {
	tests := []struct {
		unit    string
		wantErr bool
	}{
		{"nginx.service", false},
		{"docker.service", false},
		{"postgresql@14.service", false},
		{"sshd.socket", false},
		{"daily-backup.timer", false},
		{"home.mount", false},
		{"multi-user.target", false},
		{"", true},                 // no suffix
		{"nginx", true},            // no suffix
		{"../etc/passwd", true},    // path traversal
		{"rm -rf /.service", true}, // spaces
		{strings.Repeat("a", 257) + ".service", true}, // too long
	}

	for _, tt := range tests {
		err := validateUnitName(tt.unit)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateUnitName(%q) err=%v, wantErr=%v", tt.unit, err, tt.wantErr)
		}
	}
}

// --- Handler tests ---

func TestListSystemdServicesHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddSystemdService(db, 1, "Nginx", "nginx.service"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/systemd-services", nil), 1)
	rec := httptest.NewRecorder()
	ListSystemdServicesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Services []SystemdService `json:"services"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(body.Services))
	}
}

func TestAddSystemdServiceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Nginx","unit":"nginx.service"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/systemd-services", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSystemdServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddSystemdServiceHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"","unit":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/systemd-services", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSystemdServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddSystemdServiceHandler_InvalidUnit(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Bad","unit":"not-a-unit"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/systemd-services", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddSystemdServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSystemdServiceHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	svc, err := AddSystemdService(db, 1, "Test", "test.service")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	idStr := strconv.FormatInt(svc.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/systemd-services/"+idStr, nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idStr)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteSystemdServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSystemdServiceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/systemd-services/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteSystemdServiceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
