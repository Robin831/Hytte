package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// withUser adds an authenticated user to the request context.
func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

// withChiParam adds a chi URL param to the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// stubModule is a test module that returns a fixed result.
type stubModule struct {
	name        string
	displayName string
	description string
	status      ModuleStatus
	message     string
}

func (s *stubModule) Name() string        { return s.name }
func (s *stubModule) DisplayName() string { return s.displayName }
func (s *stubModule) Description() string { return s.description }
func (s *stubModule) Check() ModuleResult {
	return ModuleResult{
		Name:      s.name,
		Status:    s.status,
		Message:   s.message,
		CheckedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestModulesListHandler(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()
	reg.Register(&stubModule{name: "test_mod", displayName: "Test Module", description: "A test", status: StatusOK})

	req := withUser(httptest.NewRequest("GET", "/api/infra/modules", nil), 1)
	rec := httptest.NewRecorder()
	ModulesListHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Modules []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Enabled     bool   `json:"enabled"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(body.Modules))
	}
	if body.Modules[0].Name != "test_mod" {
		t.Errorf("expected test_mod, got %s", body.Modules[0].Name)
	}
	if !body.Modules[0].Enabled {
		t.Error("expected module to be enabled by default")
	}
}

func TestModuleToggleHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()
	reg.Register(&stubModule{name: "test_mod", displayName: "Test", description: "test", status: StatusOK})

	payload := `{"enabled":false}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/modules/test_mod", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "test_mod")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ModuleToggleHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	enabled, err := IsModuleEnabled(db, 1, "test_mod")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if enabled {
		t.Error("expected module to be disabled after toggle")
	}
}

func TestModuleToggleHandler_UnknownModule(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()

	payload := `{"enabled":false}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/modules/nope", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "nope")
	rec := httptest.NewRecorder()
	ModuleToggleHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStatusHandler_AllOK(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()
	reg.Register(&stubModule{name: "a", displayName: "A", description: "a", status: StatusOK, message: "fine"})
	reg.Register(&stubModule{name: "b", displayName: "B", description: "b", status: StatusOK, message: "also fine"})

	req := withUser(httptest.NewRequest("GET", "/api/infra/status", nil), 1)
	rec := httptest.NewRecorder()
	StatusHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Overall string         `json:"overall"`
		Modules []ModuleResult `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Overall != string(StatusOK) {
		t.Errorf("expected overall ok, got %s", body.Overall)
	}
	if len(body.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(body.Modules))
	}
}

func TestStatusHandler_Degraded(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()
	reg.Register(&stubModule{name: "a", displayName: "A", description: "a", status: StatusOK})
	reg.Register(&stubModule{name: "b", displayName: "B", description: "b", status: StatusDegraded})

	req := withUser(httptest.NewRequest("GET", "/api/infra/status", nil), 1)
	rec := httptest.NewRecorder()
	StatusHandler(db, reg).ServeHTTP(rec, req)

	var body struct {
		Overall string `json:"overall"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Overall != string(StatusDegraded) {
		t.Errorf("expected degraded, got %s", body.Overall)
	}
}

func TestStatusHandler_SkipsDisabled(t *testing.T) {
	db := setupTestDB(t)
	reg := NewRegistry()
	reg.Register(&stubModule{name: "a", displayName: "A", description: "a", status: StatusOK})
	reg.Register(&stubModule{name: "b", displayName: "B", description: "b", status: StatusDown})

	// Disable module "b"
	SetModuleEnabled(db, 1, "b", false)

	req := withUser(httptest.NewRequest("GET", "/api/infra/status", nil), 1)
	rec := httptest.NewRecorder()
	StatusHandler(db, reg).ServeHTTP(rec, req)

	var body struct {
		Overall string         `json:"overall"`
		Modules []ModuleResult `json:"modules"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Overall != string(StatusOK) {
		t.Errorf("expected ok (disabled module skipped), got %s", body.Overall)
	}
	if len(body.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(body.Modules))
	}
}

func TestModuleDetailHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	_ = db
	reg := NewRegistry()
	reg.Register(&stubModule{name: "test_mod", displayName: "Test", description: "test", status: StatusOK, message: "all good"})

	req := httptest.NewRequest("GET", "/api/infra/modules/test_mod/detail", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "name", "test_mod")
	rec := httptest.NewRecorder()
	ModuleDetailHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result ModuleResult
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Name != "test_mod" {
		t.Errorf("expected test_mod, got %s", result.Name)
	}
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s", result.Status)
	}
}

func TestModuleDetailHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	_ = db
	reg := NewRegistry()

	req := httptest.NewRequest("GET", "/api/infra/modules/nope/detail", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "name", "nope")
	rec := httptest.NewRecorder()
	ModuleDetailHandler(db, reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
