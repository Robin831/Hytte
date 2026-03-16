package infra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetModulePreferences_Empty(t *testing.T) {
	db := setupTestDB(t)
	prefs, err := GetModulePreferences(db, 1, "health_checks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected 0 prefs, got %d", len(prefs))
	}
}

func TestSetAndGetModulePreferences(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "health_checks", "timeout", "30"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetModulePreference(db, 1, "health_checks", "interval", "60"); err != nil {
		t.Fatalf("set: %v", err)
	}

	prefs, err := GetModulePreferences(db, 1, "health_checks")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("expected 2 prefs, got %d", len(prefs))
	}
	// Ordered by key.
	if prefs[0].Key != "interval" || prefs[0].Value != "60" {
		t.Errorf("unexpected pref[0]: %+v", prefs[0])
	}
	if prefs[1].Key != "timeout" || prefs[1].Value != "30" {
		t.Errorf("unexpected pref[1]: %+v", prefs[1])
	}
}

func TestSetModulePreference_Upsert(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "dns", "timeout", "10"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetModulePreference(db, 1, "dns", "timeout", "20"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	prefs, err := GetModulePreferences(db, 1, "dns")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("expected 1 pref after upsert, got %d", len(prefs))
	}
	if prefs[0].Value != "20" {
		t.Errorf("expected updated value 20, got %s", prefs[0].Value)
	}
}

func TestDeleteModulePreference(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "dns", "timeout", "10"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := DeleteModulePreference(db, 1, "dns", "timeout"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	prefs, err := GetModulePreferences(db, 1, "dns")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected 0 prefs after delete, got %d", len(prefs))
	}
}

func TestDeleteModulePreference_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteModulePreference(db, 1, "dns", "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent preference")
	}
}

func TestGetAllModulePreferences(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "dns", "timeout", "10"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetModulePreference(db, 1, "health_checks", "interval", "30"); err != nil {
		t.Fatalf("set: %v", err)
	}

	prefs, err := GetAllModulePreferences(db, 1)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("expected 2 prefs across modules, got %d", len(prefs))
	}
	// Ordered by module, key.
	if prefs[0].Module != "dns" {
		t.Errorf("expected dns first, got %s", prefs[0].Module)
	}
	if prefs[1].Module != "health_checks" {
		t.Errorf("expected health_checks second, got %s", prefs[1].Module)
	}
}

func TestGetModulePreferences_UserIsolation(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "dns", "timeout", "10"); err != nil {
		t.Fatalf("set: %v", err)
	}

	// User 2 should see no preferences.
	prefs, err := GetModulePreferences(db, 2, "dns")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected 0 prefs for user 2, got %d", len(prefs))
	}
}

// --- Handler tests ---

func TestModulePreferencesGetHandler(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	registry.Register(NewHealthCheckModule(db))

	if err := SetModulePreference(db, 1, "health_checks", "timeout", "30"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/modules/health_checks/preferences", nil), 1)
	req = withChiParam(req, "name", "health_checks")
	rec := httptest.NewRecorder()
	ModulePreferencesGetHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Preferences []ModulePreference `json:"preferences"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Preferences) != 1 {
		t.Errorf("expected 1 preference, got %d", len(body.Preferences))
	}
}

func TestModulePreferencesGetHandler_UnknownModule(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	req := withUser(httptest.NewRequest("GET", "/api/infra/modules/unknown/preferences", nil), 1)
	req = withChiParam(req, "name", "unknown")
	rec := httptest.NewRecorder()
	ModulePreferencesGetHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestModulePreferencesPutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	registry.Register(NewHealthCheckModule(db))

	payload := `{"key":"timeout","value":"30"}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/modules/health_checks/preferences", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "health_checks")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ModulePreferencesPutHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestModulePreferencesPutHandler_EmptyKey(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	registry.Register(NewHealthCheckModule(db))

	payload := `{"key":"","value":"30"}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/modules/health_checks/preferences", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "health_checks")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ModulePreferencesPutHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestModulePreferencesDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	registry.Register(NewHealthCheckModule(db))

	if err := SetModulePreference(db, 1, "health_checks", "timeout", "30"); err != nil {
		t.Fatal(err)
	}

	payload := `{"key":"timeout"}`
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/modules/health_checks/preferences", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "health_checks")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ModulePreferencesDeleteHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAllModulePreferencesHandler(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModulePreference(db, 1, "dns", "timeout", "10"); err != nil {
		t.Fatal(err)
	}
	if err := SetModulePreference(db, 1, "health_checks", "interval", "30"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/modules/preferences", nil), 1)
	rec := httptest.NewRecorder()
	AllModulePreferencesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Preferences []ModulePreference `json:"preferences"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Preferences) != 2 {
		t.Errorf("expected 2 preferences, got %d", len(body.Preferences))
	}
}

func TestModulePreferencesDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	registry.Register(NewHealthCheckModule(db))

	payload := `{"key":"nonexistent"}`
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/modules/health_checks/preferences", strings.NewReader(payload)), 1)
	req = withChiParam(req, "name", "health_checks")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ModulePreferencesDeleteHandler(db, registry).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
