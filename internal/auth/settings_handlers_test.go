package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

func TestPreferencesGetHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesGetHandler(db))
	req := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// partner_income is always injected with its default even for new users.
	prefs := body["preferences"]
	if len(prefs) != 1 || prefs["partner_income"] != "0" {
		t.Errorf("expected only partner_income:0 default, got %v", prefs)
	}
}

func TestPreferencesGetHandler_NonAdminHidesClaudePrefs(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Seed claude preferences directly in the DB.
	for _, key := range []string{"claude_enabled", "claude_cli_path", "claude_model"} {
		if err := SetPreference(db, userID, key, "test-value"); err != nil {
			t.Fatalf("SetPreference(%s): %v", key, err)
		}
	}
	// Also set a normal preference.
	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference(theme): %v", err)
	}

	handler := RequireAuth(db)(PreferencesGetHandler(db))
	req := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	prefs := body["preferences"]

	// Non-admin should not see claude preferences.
	for _, key := range []string{"claude_enabled", "claude_cli_path", "claude_model"} {
		if _, ok := prefs[key]; ok {
			t.Errorf("non-admin should not see %s in preferences", key)
		}
	}
	// But should still see normal preferences.
	if prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %q", prefs["theme"])
	}
}

func TestPreferencesGetHandler_AdminSeesClaudePrefs(t *testing.T) {
	db := setupTestDB(t)
	adminID := createTestAdminUser(t, db)
	token, _, err := CreateSession(db, adminID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Seed claude preferences.
	for _, key := range []string{"claude_enabled", "claude_cli_path", "claude_model"} {
		if err := SetPreference(db, adminID, key, "test-value"); err != nil {
			t.Fatalf("SetPreference(%s): %v", key, err)
		}
	}

	handler := RequireAuth(db)(PreferencesGetHandler(db))
	req := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	prefs := body["preferences"]

	// Admin should see claude preferences.
	for _, key := range []string{"claude_enabled", "claude_cli_path", "claude_model"} {
		if prefs[key] != "test-value" {
			t.Errorf("admin should see %s=test-value, got %q", key, prefs[key])
		}
	}
}

func TestPreferencesPutHandler_NonAdminRejectsClaudePrefs(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"claude_enabled":"true"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "Claude AI features are restricted to admin users" {
		t.Errorf("unexpected error: %q", resp["error"])
	}
}

func TestPreferencesPutHandler_AllowedKey(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"theme":"dark"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %q", resp["preferences"]["theme"])
	}
}

func TestPreferencesPutHandler_WeatherLocation(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Set weather_location
	body := `{"preferences":{"weather_location":"Stavanger"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["weather_location"] != "Stavanger" {
		t.Errorf("expected weather_location=Stavanger, got %q", resp["preferences"]["weather_location"])
	}

	// Verify round-trip via GET
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["weather_location"] != "Stavanger" {
		t.Errorf("GET expected weather_location=Stavanger, got %q", resp2["preferences"]["weather_location"])
	}
}

func TestPreferencesPutHandler_RecentLocations(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// The value is a JSON-encoded string containing an array of locations.
	body := `{"preferences":{"recent_locations":"[{\"name\":\"Oslo\",\"lat\":59.9139,\"lon\":10.7522}]"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["recent_locations"] == "" {
		t.Error("expected recent_locations to be stored")
	}

	// Verify round-trip via GET.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["recent_locations"] == "" {
		t.Error("GET expected recent_locations to be persisted")
	}
}

func TestPreferencesPutHandler_NotificationsEnabled(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Enable notifications
	body := `{"preferences":{"notifications_enabled":"true"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["notifications_enabled"] != "true" {
		t.Errorf("expected notifications_enabled=true, got %q", resp["preferences"]["notifications_enabled"])
	}

	// Disable notifications
	body = `{"preferences":{"notifications_enabled":"false"}}`
	req = httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d", rec.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode disable: %v", err)
	}
	if resp2["preferences"]["notifications_enabled"] != "false" {
		t.Errorf("expected notifications_enabled=false, got %q", resp2["preferences"]["notifications_enabled"])
	}

	// Verify round-trip via GET
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp3 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp3); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp3["preferences"]["notifications_enabled"] != "false" {
		t.Errorf("GET expected notifications_enabled=false, got %q", resp3["preferences"]["notifications_enabled"])
	}
}

func TestPreferencesPutHandler_QuietHours(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	body := `{"preferences":{"quiet_hours_enabled":"true","quiet_hours_start":"22:00","quiet_hours_end":"07:00","quiet_hours_timezone":"Europe/Oslo"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["quiet_hours_enabled"] != "true" {
		t.Errorf("expected quiet_hours_enabled=true, got %q", resp["preferences"]["quiet_hours_enabled"])
	}
	if resp["preferences"]["quiet_hours_start"] != "22:00" {
		t.Errorf("expected quiet_hours_start=22:00, got %q", resp["preferences"]["quiet_hours_start"])
	}
	if resp["preferences"]["quiet_hours_end"] != "07:00" {
		t.Errorf("expected quiet_hours_end=07:00, got %q", resp["preferences"]["quiet_hours_end"])
	}
	if resp["preferences"]["quiet_hours_timezone"] != "Europe/Oslo" {
		t.Errorf("expected quiet_hours_timezone=Europe/Oslo, got %q", resp["preferences"]["quiet_hours_timezone"])
	}

	// Verify round-trip via GET.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["quiet_hours_timezone"] != "Europe/Oslo" {
		t.Errorf("GET expected quiet_hours_timezone=Europe/Oslo, got %q", resp2["preferences"]["quiet_hours_timezone"])
	}
}

func TestPreferencesPutHandler_NotificationFilterSources(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Set source filters — disable generic, keep github enabled
	body := `{"preferences":{"notification_filter_sources":"{\"github\":true,\"generic\":false}"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored := resp["preferences"]["notification_filter_sources"]
	if stored == "" {
		t.Fatal("expected notification_filter_sources to be stored")
	}

	// Parse and verify the stored JSON object.
	var filters map[string]bool
	if err := json.Unmarshal([]byte(stored), &filters); err != nil {
		t.Fatalf("unmarshal stored filters: %v", err)
	}
	if !filters["github"] {
		t.Error("expected github=true")
	}
	if filters["generic"] {
		t.Error("expected generic=false")
	}

	// Verify round-trip via GET.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["notification_filter_sources"] != stored {
		t.Errorf("GET round-trip mismatch: got %q, want %q", resp2["preferences"]["notification_filter_sources"], stored)
	}
}

func TestPreferencesPutHandler_NotificationFilterEvents(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Set event filters — disable pull_request, keep push and release enabled
	body := `{"preferences":{"notification_filter_events":{"push":true,"pull_request":false,"release":true}}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored := resp["preferences"]["notification_filter_events"]
	if stored == "" {
		t.Fatal("expected notification_filter_events to be stored")
	}

	// Parse and verify the stored JSON object.
	var filters map[string]bool
	if err := json.Unmarshal([]byte(stored), &filters); err != nil {
		t.Fatalf("unmarshal stored filters: %v", err)
	}
	if !filters["push"] {
		t.Error("expected push=true")
	}
	if filters["pull_request"] {
		t.Error("expected pull_request=false")
	}
	if !filters["release"] {
		t.Error("expected release=true")
	}

	// Verify round-trip via GET.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["notification_filter_events"] != stored {
		t.Errorf("GET round-trip mismatch: got %q, want %q", resp2["preferences"]["notification_filter_events"], stored)
	}
}

func TestPreferencesPutHandler_NotificationFilterEvents_UnknownEventRejected(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"notification_filter_events":{"push":true,"bogus_event":false}}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown event type, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "unknown event type: bogus_event" {
		t.Errorf("expected error about bogus_event, got %q", resp["error"])
	}
}

func TestPreferencesPutHandler_NotificationFilterEvents_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"notification_filter_events":"not valid json"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "notification_filter_events must be a JSON object mapping event keys to booleans" {
		t.Errorf("unexpected error: %q", resp["error"])
	}
}

func TestPreferencesPutHandler_NotificationFilterEvents_AllForgeEvents(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Build a JSON object containing every allowed event type.
	allEvents := make(map[string]bool, len(AllowedEventTypes))
	for _, et := range AllowedEventTypes {
		allEvents[et.Key] = true
	}
	eventsJSON, err := json.Marshal(allEvents)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"notification_filter_events":` + string(eventsJSON) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for all valid events, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify round-trip: stored value should parse back to all keys.
	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored := resp["preferences"]["notification_filter_events"]
	var roundTrip map[string]bool
	if err := json.Unmarshal([]byte(stored), &roundTrip); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
	for _, et := range AllowedEventTypes {
		if !roundTrip[et.Key] {
			t.Errorf("expected %s=true in round-trip, got %v", et.Key, roundTrip[et.Key])
		}
	}
}

func TestEventTypesHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/settings/event-types", nil)
	rec := httptest.NewRecorder()
	EventTypesHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		EventTypes []EventType `json:"event_types"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.EventTypes) != len(AllowedEventTypes) {
		t.Fatalf("expected %d event types, got %d", len(AllowedEventTypes), len(resp.EventTypes))
	}
	// Verify first and last entries match the canonical list.
	if resp.EventTypes[0].Key != AllowedEventTypes[0].Key {
		t.Errorf("first key: expected %q, got %q", AllowedEventTypes[0].Key, resp.EventTypes[0].Key)
	}
	last := len(AllowedEventTypes) - 1
	if resp.EventTypes[last].Key != AllowedEventTypes[last].Key {
		t.Errorf("last key: expected %q, got %q", AllowedEventTypes[last].Key, resp.EventTypes[last].Key)
	}
}

func TestPreferencesPutHandler_QuickLinks(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Store quick_links as a JSON-encoded array of link objects.
	linksJSON := `[{"title":"Example","url":"https://example.com"},{"title":"Go Docs","url":"https://go.dev"}]`
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored := resp["preferences"]["quick_links"]
	if stored == "" {
		t.Fatal("expected quick_links to be stored")
	}

	// Parse and verify the stored JSON array.
	var links []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal([]byte(stored), &links); err != nil {
		t.Fatalf("unmarshal stored links: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].Title != "Example" || links[0].URL != "https://example.com" {
		t.Errorf("link[0] mismatch: got %+v", links[0])
	}
	if links[1].Title != "Go Docs" || links[1].URL != "https://go.dev" {
		t.Errorf("link[1] mismatch: got %+v", links[1])
	}

	// Verify round-trip via GET.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}

	var resp2 map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp2["preferences"]["quick_links"] != stored {
		t.Errorf("GET round-trip mismatch: got %q, want %q", resp2["preferences"]["quick_links"], stored)
	}

	// Update: remove one link and verify the update persists.
	updatedJSON := `[{"title":"Go Docs","url":"https://go.dev"}]`
	body2 := `{"preferences":{"quick_links":` + updatedJSON + `}}`
	req3 := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body2))
	req3.Header.Set("Content-Type", "application/json")
	req3.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d", rec3.Code)
	}

	var resp3 map[string]map[string]string
	if err := json.NewDecoder(rec3.Body).Decode(&resp3); err != nil {
		t.Fatalf("update decode: %v", err)
	}
	var updatedLinks []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal([]byte(resp3["preferences"]["quick_links"]), &updatedLinks); err != nil {
		t.Fatalf("unmarshal updated links: %v", err)
	}
	if len(updatedLinks) != 1 {
		t.Fatalf("expected 1 link after update, got %d", len(updatedLinks))
	}
	if updatedLinks[0].Title != "Go Docs" {
		t.Errorf("expected remaining link title 'Go Docs', got %q", updatedLinks[0].Title)
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsJavascriptURL(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	linksJSON := `[{"title":"XSS","url":"javascript:alert(1)"}]`
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for javascript: URL, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsEmptyTitle(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	linksJSON := `[{"title":"","url":"https://example.com"}]`
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty title, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsDataURL(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	linksJSON := `[{"title":"Sneaky","url":"data:text/html,<script>alert(1)</script>"}]`
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for data: URL, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsEmptyHost(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	linksJSON := `[{"title":"Empty host","url":"http://"}]`
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty-host URL, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsTooMany(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Build a JSON array with 51 links (over the 50 limit).
	var links []string
	for range 51 {
		links = append(links, `{"title":"Link","url":"https://example.com"}`)
	}
	linksJSON := "[" + strings.Join(links, ",") + "]"
	body := `{"preferences":{"quick_links":` + linksJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many links, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_SkyWatchLocation(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	value := `{"name":"Bergen","lat":60.3913,"lon":5.3221}`
	body := `{"preferences":{"skywatch_location":` + strconv.Quote(value) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp["preferences"]["skywatch_location"]; got != value {
		t.Errorf("expected skywatch_location %q, got %q", value, got)
	}

	// It must also round-trip through a GET.
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["skywatch_location"] != value {
		t.Errorf("expected stored skywatch_location %q, got %q", value, stored["skywatch_location"])
	}
}

func TestPreferencesPutHandler_SkyWatchLocationRejectsInvalid(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	tests := []struct {
		name  string
		value string
	}{
		{"not JSON", "Bergen"},
		{"missing coordinates", `{"name":"Bergen"}`},
		{"latitude out of range", `{"name":"Nowhere","lat":95,"lon":5}`},
		{"longitude out of range", `{"name":"Nowhere","lat":60,"lon":-181}`},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `","lat":60,"lon":5}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"preferences":{"skywatch_location":` + strconv.Quote(tc.value) + `}}`
			req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "session", Value: token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPreferencesPutHandler_DisallowedKey(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"evil_key":"bad_value"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only the partner_income default should be present; the disallowed key must not be stored.
	prefs := resp["preferences"]
	if len(prefs) != 1 || prefs["partner_income"] != "0" {
		t.Errorf("expected only partner_income:0 default, got %v", prefs)
	}
}

func TestPreferencesPutHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestSessionsListHandler(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(SessionsListHandler(db))
	req := httptest.NewRequest("GET", "/api/settings/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	resp := decodeSessionsList(t, rec)
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
	}
	if !resp.Sessions[0].Current {
		t.Error("expected session to be marked as current")
	}
	expectedID := hashToken(token)[:8]
	if resp.Sessions[0].ID != expectedID {
		t.Errorf("expected ID %s, got %s", expectedID, resp.Sessions[0].ID)
	}
	// A session created without a request has no user agent to label.
	if resp.Sessions[0].DeviceLabel != UnknownDeviceLabel {
		t.Errorf("expected %q, got %q", UnknownDeviceLabel, resp.Sessions[0].DeviceLabel)
	}
	if resp.Sessions[0].LastSeenAt == nil {
		t.Error("expected last_seen_at to be set on a fresh session")
	}
}

// sessionListItem mirrors one entry returned by SessionsListHandler.
type sessionListItem struct {
	ID          string  `json:"id"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	DeviceLabel string  `json:"device_label"`
	LastSeenAt  *string `json:"last_seen_at"`
	Current     bool    `json:"current"`
}

type sessionsListResponse struct {
	Sessions []sessionListItem `json:"sessions"`
}

func decodeSessionsList(t *testing.T, rec *httptest.ResponseRecorder) sessionsListResponse {
	t.Helper()
	var resp sessionsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// listSessions runs SessionsListHandler behind RequireAuth for the given token.
func listSessions(t *testing.T, db *sql.DB, token string) sessionsListResponse {
	t.Helper()
	handler := RequireAuth(db)(SessionsListHandler(db))
	req := httptest.NewRequest("GET", "/api/settings/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	return decodeSessionsList(t, rec)
}

func TestSessionsListHandler_DeviceLabelFromUserAgent(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-session-device-label")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	signIn := httptest.NewRequest("GET", "/api/auth/google/callback", nil)
	signIn.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1")
	token, _, err := CreateSessionForRequest(db, userID, signIn)
	if err != nil {
		t.Fatalf("CreateSessionForRequest: %v", err)
	}

	resp := listSessions(t, db, token)
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
	}
	if resp.Sessions[0].DeviceLabel != "Safari on iPhone" {
		t.Errorf("expected %q, got %q", "Safari on iPhone", resp.Sessions[0].DeviceLabel)
	}
	if resp.Sessions[0].LastSeenAt == nil {
		t.Fatal("expected last_seen_at to be set")
	}
	if _, err := time.Parse(time.RFC3339, *resp.Sessions[0].LastSeenAt); err != nil {
		t.Errorf("last_seen_at is not RFC3339: %v", err)
	}
}

func TestSessionsListHandler_LegacySessionWithoutMetadata(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// The current session, plus a row predating user_agent/last_seen_at.
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		hashToken("legacy-token"), userID, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert legacy session: %v", err)
	}

	resp := listSessions(t, db, token)
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
	var legacy *sessionListItem
	for i := range resp.Sessions {
		if resp.Sessions[i].ID == hashToken("legacy-token")[:8] {
			legacy = &resp.Sessions[i]
		}
	}
	if legacy == nil {
		t.Fatal("legacy session missing from the list")
	}
	if legacy.DeviceLabel != UnknownDeviceLabel {
		t.Errorf("expected %q, got %q", UnknownDeviceLabel, legacy.DeviceLabel)
	}
	if legacy.LastSeenAt != nil {
		t.Errorf("expected last_seen_at to be null, got %q", *legacy.LastSeenAt)
	}
}

// revokeSession runs SessionRevokeHandler behind a chi router so {id} resolves.
func revokeSession(t *testing.T, db *sql.DB, cookieToken, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.With(RequireAuth(db)).Delete("/api/settings/sessions/{id}", SessionRevokeHandler(db))
	req := httptest.NewRequest("DELETE", "/api/settings/sessions/"+id, nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: cookieToken})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSessionRevokeHandler_DeletesOnlyTheTargetSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	current, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession current: %v", err)
	}
	other, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession other: %v", err)
	}

	rec := revokeSession(t, db, current, hashToken(other)[:8])
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The revoked session's cookie is rejected on its next request.
	if _, err := ValidateSession(db, other); err != sql.ErrNoRows {
		t.Errorf("expected the revoked session to be gone, got %v", err)
	}
	if _, err := ValidateSession(db, current); err != nil {
		t.Errorf("current session should survive: %v", err)
	}
}

func TestSessionRevokeHandler_CurrentSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	current, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := revokeSession(t, db, current, hashToken(current)[:8])
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if _, err := ValidateSession(db, current); err != nil {
		t.Errorf("current session should survive: %v", err)
	}
}

func TestSessionRevokeHandler_UnknownAndInvalidIDs(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	current, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Unknown prefix, too short, non-hex, and uppercase all fail to resolve.
	for _, id := range []string{"deadbeef", "abc", "zzzzzzzz", "DEADBEEF"} {
		rec := revokeSession(t, db, current, id)
		if rec.Code != http.StatusNotFound {
			t.Errorf("id %q: expected 404, got %d", id, rec.Code)
		}
	}
}

func TestSessionRevokeHandler_AmbiguousPrefix(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	current, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Two sessions sharing a prefix: the prefix alone can't identify either.
	for _, token := range []string{"aaaa1111", "aaaa2222"} {
		if _, err := db.Exec(
			"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
			token, userID, time.Now().Add(time.Hour),
		); err != nil {
			t.Fatalf("insert session %s: %v", token, err)
		}
	}

	rec := revokeSession(t, db, current, "aaaa")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an ambiguous prefix, got %d", rec.Code)
	}
	var remaining int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sessions WHERE token LIKE 'aaaa%'",
	).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 2 {
		t.Errorf("expected both ambiguous sessions to survive, got %d", remaining)
	}
}

func TestSessionRevokeHandler_OtherUsersSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO users (google_id, email, name) VALUES ('g-other', 'other@test.com', 'Other')",
	); err != nil {
		t.Fatalf("insert other user: %v", err)
	}
	var otherID int64
	if err := db.QueryRow("SELECT id FROM users WHERE google_id = 'g-other'").Scan(&otherID); err != nil {
		t.Fatalf("select other user: %v", err)
	}

	current, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	victim, _, err := CreateSession(db, otherID)
	if err != nil {
		t.Fatalf("CreateSession victim: %v", err)
	}

	rec := revokeSession(t, db, current, hashToken(victim)[:8])
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if _, err := ValidateSession(db, victim); err != nil {
		t.Errorf("another user's session must not be revoked: %v", err)
	}
}

func TestSignOutEverywhereHandler(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Create two sessions.
	token1, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	_, _, err = CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}

	handler := RequireAuth(db)(SignOutEverywhereHandler(db))
	req := httptest.NewRequest("POST", "/api/settings/sessions/revoke-others", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token1})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify only one session remains.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", userID).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 session remaining, got %d", count)
	}

	// The remaining session should be token1.
	if _, err := ValidateSession(db, token1); err != nil {
		t.Error("current session should still be valid")
	}
}

func TestDeleteAccountHandler(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(DeleteAccountHandler(db))
	req := httptest.NewRequest("DELETE", "/api/settings/account", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify user is deleted.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Error("expected user to be deleted")
	}

	// Verify session cookie is cleared.
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected session cookie to be cleared")
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsInvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	body := `{"preferences":{"quick_links":"not valid json"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid quick_links JSON, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsTitleTooLong(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Create a title with 201 characters.
	longTitle := strings.Repeat("a", 201)
	type link struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	linksData, _ := json.Marshal([]link{{Title: longTitle, URL: "https://example.com"}})
	body := `{"preferences":{"quick_links":` + string(linksData) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for title too long, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "quick link title must not exceed 200 characters" {
		t.Errorf("unexpected error: %q", resp["error"])
	}
}

func TestPreferencesPutHandler_QuickLinksRejectsURLTooLong(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	// Create a URL with 2049+ characters.
	longURL := "https://example.com/" + strings.Repeat("a", 2030)
	type link struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	linksData, _ := json.Marshal([]link{{Title: "Long URL", URL: longURL}})
	body := `{"preferences":{"quick_links":` + string(linksData) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for URL too long, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "quick link URL must not exceed 2048 characters" {
		t.Errorf("unexpected error: %q", resp["error"])
	}
}

// TestPreferencesPutHandler_AdminClaudeCliPathEncryptedRoundtrip verifies that
// the DB stores an encrypted value and the GET/PUT responses return plaintext.
func TestPreferencesPutHandler_AdminClaudeCliPathEncryptedRoundtrip(t *testing.T) {
	database := setupTestDB(t)
	adminID := createTestAdminUser(t, database)
	token, _, err := CreateSession(database, adminID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const cliPath = "/usr/local/bin/claude"

	// PUT the claude_cli_path as an admin.
	putHandler := RequireAuth(database)(PreferencesPutHandler(database))
	body := `{"preferences":{"claude_cli_path":"` + cliPath + `"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	putHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Assert the PUT response contains the decrypted plaintext.
	var putResp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if putResp["preferences"]["claude_cli_path"] != cliPath {
		t.Errorf("PUT response: expected decrypted %q, got %q", cliPath, putResp["preferences"]["claude_cli_path"])
	}

	// Assert the raw DB value is encrypted (has the enc: prefix).
	rawPrefs, err := GetPreferences(database, adminID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	rawVal := rawPrefs["claude_cli_path"]
	if !strings.HasPrefix(rawVal, "enc:") {
		t.Errorf("DB value should be encrypted (enc: prefix), got %q", rawVal)
	}
	// Also verify it decrypts back to the original.
	decrypted, err := encryption.DecryptField(rawVal)
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if decrypted != cliPath {
		t.Errorf("decrypted value: expected %q, got %q", cliPath, decrypted)
	}

	// Assert the GET response also returns the decrypted plaintext.
	getHandler := RequireAuth(database)(PreferencesGetHandler(database))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}
	var getResp map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if getResp["preferences"]["claude_cli_path"] != cliPath {
		t.Errorf("GET response: expected decrypted %q, got %q", cliPath, getResp["preferences"]["claude_cli_path"])
	}
}

// TestPreferencesPutHandler_NonAdminPutDoesNotExposeClaudePrefs verifies that
// a non-admin updating a non-claude preference does not receive Claude keys in the response.
func TestPreferencesPutHandler_NonAdminPutDoesNotExposeClaudePrefs(t *testing.T) {
	database := setupTestDB(t)
	adminID := createTestAdminUser(t, database)
	userID := createTestUser(t, database)

	// Store a claude_cli_path as admin so it exists in the DB.
	adminToken, _, err := CreateSession(database, adminID)
	if err != nil {
		t.Fatalf("CreateSession admin: %v", err)
	}
	putAdminHandler := RequireAuth(database)(PreferencesPutHandler(database))
	adminBody := `{"preferences":{"claude_cli_path":"/usr/local/bin/claude","claude_enabled":"true"}}`
	adminReq := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(adminBody))
	adminReq.Header.Set("Content-Type", "application/json")
	adminReq.AddCookie(&http.Cookie{Name: "session", Value: adminToken})
	adminRec := httptest.NewRecorder()
	putAdminHandler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin PUT expected 200, got %d", adminRec.Code)
	}

	// Now have a non-admin update their own "theme" preference.
	userToken, _, err := CreateSession(database, userID)
	if err != nil {
		t.Fatalf("CreateSession user: %v", err)
	}
	// Seed a claude pref for the non-admin user directly to test isolation.
	if err := SetPreference(database, userID, "claude_cli_path", "enc:someencryptedvalue"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	putHandler := RequireAuth(database)(PreferencesPutHandler(database))
	body := `{"preferences":{"theme":"dark"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: userToken})
	rec := httptest.NewRecorder()
	putHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("non-admin PUT expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	prefs := resp["preferences"]
	for _, key := range []string{"claude_enabled", "claude_cli_path", "claude_model"} {
		if _, ok := prefs[key]; ok {
			t.Errorf("non-admin PUT response should not include %s", key)
		}
	}
	if prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark in response, got %q", prefs["theme"])
	}
}

// The legacy goal_race_* preferences were removed with the macro plan: races
// live in stride_races now. The allow-list drops unknown keys silently, so the
// contract to assert is that nothing is written back for them.
func TestPreferencesPutHandler_GoalRaceKeysRejected(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{
		"goal_race_name":"Oslo Marathon",
		"goal_race_date":"2026-09-20",
		"goal_race_distance":"42.2",
		"goal_race_target_time":"3:45:00"
	}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"goal_race_name", "goal_race_date", "goal_race_distance", "goal_race_target_time"} {
		if v, ok := resp["preferences"][key]; ok {
			t.Errorf("%s should no longer be an accepted preference, got %q", key, v)
		}
	}

	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	for _, key := range []string{"goal_race_name", "goal_race_date", "goal_race_distance", "goal_race_target_time"} {
		if v, ok := stored[key]; ok {
			t.Errorf("%s should not have been persisted, got %q", key, v)
		}
	}
}

// The Stride switches used to be settable only by hand in SQL; they are part of
// the allow-list now, with the same bounds the planner reads them under.
func TestPreferencesPutHandler_StrideKeys(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	if err := SetUserFeature(db, userID, "stride", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := RequireAuth(db)(PreferencesPutHandler(db))

	put := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := put(t, `{"preferences":{"stride_enabled":"true","stride_available_days":"5","stride_weekly_distance_cap":"70"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	want := map[string]string{"stride_enabled": "true", "stride_available_days": "5", "stride_weekly_distance_cap": "70"}
	for k, v := range want {
		if stored[k] != v {
			t.Errorf("%s = %q, want %q", k, stored[k], v)
		}
	}

	// Clearing is allowed — an empty string wipes the value.
	if rec := put(t, `{"preferences":{"stride_weekly_distance_cap":""}}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing stride_weekly_distance_cap: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Out-of-range and non-boolean values are rejected outright.
	rejected := []struct {
		name string
		body string
	}{
		{"days too low", `{"preferences":{"stride_available_days":"0"}}`},
		{"days too high", `{"preferences":{"stride_available_days":"8"}}`},
		{"days not a number", `{"preferences":{"stride_available_days":"most"}}`},
		{"cap too low", `{"preferences":{"stride_weekly_distance_cap":"0"}}`},
		{"cap too high", `{"preferences":{"stride_weekly_distance_cap":"501"}}`},
		{"enabled not a boolean", `{"preferences":{"stride_enabled":"yes"}}`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if rec := put(t, tc.body); rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// A rejected write must not have disturbed the accepted values.
	stored, err = GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["stride_enabled"] != "true" || stored["stride_available_days"] != "5" {
		t.Errorf("rejected writes changed stored prefs: %q / %q", stored["stride_enabled"], stored["stride_available_days"])
	}
}

// The stride_* preferences are feature-gated server-side, not just hidden in
// the UI: stride_enabled enrols the user in the weekly Stride cron, which picks
// athletes on that preference alone.
func TestPreferencesPutHandler_StrideKeysRequireFeature(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := RequireAuth(db)(PreferencesPutHandler(db))

	put := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	strideBodies := []struct {
		name string
		body string
	}{
		{"enabled", `{"preferences":{"stride_enabled":"true"}}`},
		{"available days", `{"preferences":{"stride_available_days":"5"}}`},
		{"weekly distance cap", `{"preferences":{"stride_weekly_distance_cap":"70"}}`},
		{"custom prompt", `{"preferences":{"stride_custom_prompt":"go easy"}}`},
		{"treadmill calibration", `{"preferences":{"stride_treadmill_calibration":"3% offset"}}`},
		{"mixed with an allowed key", `{"preferences":{"theme":"dark","stride_enabled":"true"}}`},
	}
	for _, tc := range strideBodies {
		t.Run("without feature/"+tc.name, func(t *testing.T) {
			if rec := put(t, tc.body); rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Nothing was persisted — not even the non-stride key sent alongside.
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	for k := range stored {
		if strings.HasPrefix(k, "stride_") || k == "theme" {
			t.Errorf("%s should not have been written without the stride feature (got %q)", k, stored[k])
		}
	}

	// With the feature enabled the same write succeeds.
	if err := SetUserFeature(db, userID, "stride", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	if rec := put(t, `{"preferences":{"stride_enabled":"true"}}`); rec.Code != http.StatusOK {
		t.Fatalf("with feature: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	stored, err = GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["stride_enabled"] != "true" {
		t.Errorf("stride_enabled = %q, want \"true\"", stored["stride_enabled"])
	}
}

// The case the gate actually changes for existing data: a user who enrolled
// while the feature was on and then had it revoked. The gate must not lock them
// in — turning Stride off and clearing values has to keep working, otherwise
// they stay in the weekly cron with the switches hidden and no way out.
func TestPreferencesPutHandler_StrideDisableAllowedAfterFeatureRevoked(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := RequireAuth(db)(PreferencesPutHandler(db))

	put := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Enrol while the feature is on.
	if err := SetUserFeature(db, userID, "stride", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	if rec := put(t, `{"preferences":{"stride_enabled":"true","stride_available_days":"5","stride_custom_prompt":"go easy"}}`); rec.Code != http.StatusOK {
		t.Fatalf("enrolling with feature: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// An admin revokes the feature. The stride_enabled row survives, so the
	// weekly cron would still pick the athlete up on the preference alone.
	if err := SetUserFeature(db, userID, "stride", false); err != nil {
		t.Fatalf("SetUserFeature(false): %v", err)
	}

	// Turning it back on stays blocked...
	if rec := put(t, `{"preferences":{"stride_enabled":"true"}}`); rec.Code != http.StatusForbidden {
		t.Errorf("re-enabling without the feature: expected 403, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// ...but turning it off, and clearing the other stride values, must work.
	if rec := put(t, `{"preferences":{"stride_enabled":"false","stride_available_days":"","stride_custom_prompt":""}}`); rec.Code != http.StatusOK {
		t.Fatalf("disabling after revocation: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["stride_enabled"] != "false" {
		t.Errorf("stride_enabled = %q, want \"false\" — the user must be able to opt out", stored["stride_enabled"])
	}
	if stored["stride_available_days"] != "" || stored["stride_custom_prompt"] != "" {
		t.Errorf("clearing writes were rejected: days=%q prompt=%q", stored["stride_available_days"], stored["stride_custom_prompt"])
	}
}

// Admins bypass feature checks everywhere else, and the stride preference gate
// is no exception.
func TestPreferencesPutHandler_StrideKeysAllowedForAdmin(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestAdminUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := RequireAuth(db)(PreferencesPutHandler(db))

	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(`{"preferences":{"stride_enabled":"true"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d; body: %s", rec.Code, rec.Body.String())
	}
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["stride_enabled"] != "true" {
		t.Errorf("stride_enabled = %q, want \"true\"", stored["stride_enabled"])
	}
}

func TestPreferencesPutHandler_ZoneBoundaries_Valid(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	zones := `[{"zone":1,"min_bpm":0,"max_bpm":120},{"zone":2,"min_bpm":120,"max_bpm":144},{"zone":3,"min_bpm":144,"max_bpm":164},{"zone":4,"min_bpm":164,"max_bpm":184},{"zone":5,"min_bpm":184,"max_bpm":200}]`
	zonesValue, _ := json.Marshal(zones)
	body := `{"preferences":{"zone_boundaries":` + string(zonesValue) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid zone_boundaries, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_ZoneBoundaries_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"zone_boundaries":"not-valid-json"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON zone_boundaries, got %d", rec.Code)
	}
}

func TestPreferencesPutHandler_ZoneBoundaries_WrongCount(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	// Only 3 zones instead of 5.
	zones := `[{"zone":1,"min_bpm":0,"max_bpm":120},{"zone":2,"min_bpm":120,"max_bpm":144},{"zone":3,"min_bpm":144,"max_bpm":164}]`
	zonesValue, _ := json.Marshal(zones)
	body := `{"preferences":{"zone_boundaries":` + string(zonesValue) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong zone count, got %d", rec.Code)
	}
}

func TestPreferencesPutHandler_ZoneBoundaries_InvalidZoneNumber(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	// Zone number 0 is invalid (must be 1-5).
	zones := `[{"zone":0,"min_bpm":0,"max_bpm":120},{"zone":2,"min_bpm":120,"max_bpm":144},{"zone":3,"min_bpm":144,"max_bpm":164},{"zone":4,"min_bpm":164,"max_bpm":184},{"zone":5,"min_bpm":184,"max_bpm":200}]`
	zonesValue, _ := json.Marshal(zones)
	body := `{"preferences":{"zone_boundaries":` + string(zonesValue) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid zone number, got %d", rec.Code)
	}
}

func TestPreferencesPutHandler_ZoneBoundaries_MaxLessThanMin(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	// Zone 2 has max_bpm <= min_bpm.
	zones := `[{"zone":1,"min_bpm":0,"max_bpm":120},{"zone":2,"min_bpm":144,"max_bpm":120},{"zone":3,"min_bpm":144,"max_bpm":164},{"zone":4,"min_bpm":164,"max_bpm":184},{"zone":5,"min_bpm":184,"max_bpm":200}]`
	zonesValue, _ := json.Marshal(zones)
	body := `{"preferences":{"zone_boundaries":` + string(zonesValue) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for max_bpm <= min_bpm, got %d", rec.Code)
	}
}

// TestPreferencesPutHandler_StrideCustomPromptEncryptedRoundtrip verifies that
// stride_custom_prompt is stored encrypted and GET/PUT responses return plaintext.
func TestPreferencesPutHandler_StrideCustomPromptEncryptedRoundtrip(t *testing.T) {
	database := setupTestDB(t)
	userID := createTestUser(t, database)
	if err := SetUserFeature(database, userID, "stride", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	token, _, err := CreateSession(database, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const prompt = "Focus on threshold intervals. I prefer morning runs."

	// PUT the stride_custom_prompt.
	putHandler := RequireAuth(database)(PreferencesPutHandler(database))
	body := `{"preferences":{"stride_custom_prompt":"` + prompt + `"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	putHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Assert the PUT response contains the decrypted plaintext.
	var putResp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if putResp["preferences"]["stride_custom_prompt"] != prompt {
		t.Errorf("PUT response: expected decrypted %q, got %q", prompt, putResp["preferences"]["stride_custom_prompt"])
	}

	// Assert the raw DB value is encrypted (has the enc: prefix).
	rawPrefs, err := GetPreferences(database, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	rawVal := rawPrefs["stride_custom_prompt"]
	if !strings.HasPrefix(rawVal, "enc:") {
		t.Errorf("DB value should be encrypted (enc: prefix), got %q", rawVal)
	}
	// Verify it decrypts back to the original.
	decrypted, err := encryption.DecryptField(rawVal)
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if decrypted != prompt {
		t.Errorf("decrypted value: expected %q, got %q", prompt, decrypted)
	}

	// Assert the GET response also returns the decrypted plaintext.
	getHandler := RequireAuth(database)(PreferencesGetHandler(database))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}
	var getResp map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if getResp["preferences"]["stride_custom_prompt"] != prompt {
		t.Errorf("GET response: expected decrypted %q, got %q", prompt, getResp["preferences"]["stride_custom_prompt"])
	}
}

// TestPreferencesPutHandler_StrideTreadmillCalibrationEncryptedRoundtrip verifies
// that stride_treadmill_calibration is accepted, stored encrypted, and returned as
// plaintext by both PUT and GET — the same contract as stride_custom_prompt.
func TestPreferencesPutHandler_StrideTreadmillCalibrationEncryptedRoundtrip(t *testing.T) {
	database := setupTestDB(t)
	userID := createTestUser(t, database)
	if err := SetUserFeature(database, userID, "stride", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	token, _, err := CreateSession(database, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const calibration = "Belt sits ~3% below outdoor km/h at matched HR."

	putHandler := RequireAuth(database)(PreferencesPutHandler(database))
	body := `{"preferences":{"stride_treadmill_calibration":"` + calibration + `"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	putHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var putResp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if putResp["preferences"]["stride_treadmill_calibration"] != calibration {
		t.Errorf("PUT response: expected decrypted %q, got %q", calibration, putResp["preferences"]["stride_treadmill_calibration"])
	}

	rawPrefs, err := GetPreferences(database, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	rawVal := rawPrefs["stride_treadmill_calibration"]
	if !strings.HasPrefix(rawVal, "enc:") {
		t.Errorf("DB value should be encrypted (enc: prefix), got %q", rawVal)
	}
	decrypted, err := encryption.DecryptField(rawVal)
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if decrypted != calibration {
		t.Errorf("decrypted value: expected %q, got %q", calibration, decrypted)
	}

	getHandler := RequireAuth(database)(PreferencesGetHandler(database))
	req2 := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec2 := httptest.NewRecorder()
	getHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec2.Code)
	}
	var getResp map[string]map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if getResp["preferences"]["stride_treadmill_calibration"] != calibration {
		t.Errorf("GET response: expected decrypted %q, got %q", calibration, getResp["preferences"]["stride_treadmill_calibration"])
	}
}

// TestPreferencesGetHandler_DropsUndecryptableStridePrefs pins the drop-on-corrupt
// contract centralised in decryptStridePrefs: a stored value that carries the
// enc: prefix but cannot be decrypted is omitted from the response entirely
// rather than handed back to the client as raw ciphertext.
func TestPreferencesGetHandler_DropsUndecryptableStridePrefs(t *testing.T) {
	database := setupTestDB(t)
	userID := createTestUser(t, database)
	token, _, err := CreateSession(database, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const corrupt = "enc:not-valid-base64-ciphertext"
	for _, key := range encryptedStridePrefs {
		if err := SetPreference(database, userID, key, corrupt); err != nil {
			t.Fatalf("SetPreference(%s): %v", key, err)
		}
	}
	// A plain preference alongside them must still come back untouched.
	if err := SetPreference(database, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference(theme): %v", err)
	}

	handler := RequireAuth(database)(PreferencesGetHandler(database))
	req := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	prefs := body["preferences"]
	for _, key := range encryptedStridePrefs {
		if v, ok := prefs[key]; ok {
			t.Errorf("%s should be omitted when it fails to decrypt, got %q", key, v)
		}
	}
	if strings.Contains(rec.Body.String(), corrupt) {
		t.Error("undecryptable ciphertext leaked into the response body")
	}
	if prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark to survive, got %q", prefs["theme"])
	}
}

// TestPreferencesPutHandler_RegnemesterMutedRoundtrip verifies that the
// regnemester_muted preference is accepted by PUT and returned by GET,
// covering both "true" and "false" values.
func TestPreferencesPutHandler_RegnemesterMutedRoundtrip(t *testing.T) {
	database := setupTestDB(t)
	userID := createTestUser(t, database)
	token, _, err := CreateSession(database, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	putHandler := RequireAuth(database)(PreferencesPutHandler(database))
	getHandler := RequireAuth(database)(PreferencesGetHandler(database))

	put := func(value string) map[string]string {
		body := `{"preferences":{"regnemester_muted":"` + value + `"}}`
		req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		putHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %q expected 200, got %d; body: %s", value, rec.Code, rec.Body.String())
		}
		var resp map[string]map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode PUT response: %v", err)
		}
		return resp["preferences"]
	}

	get := func() map[string]string {
		req := httptest.NewRequest("GET", "/api/settings/preferences", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		getHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET expected 200, got %d", rec.Code)
		}
		var resp map[string]map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		return resp["preferences"]
	}

	// PUT true, confirm PUT response and subsequent GET both return "true".
	putResp := put("true")
	if putResp["regnemester_muted"] != "true" {
		t.Errorf("PUT response: expected regnemester_muted=true, got %q", putResp["regnemester_muted"])
	}
	if getResp := get(); getResp["regnemester_muted"] != "true" {
		t.Errorf("GET response: expected regnemester_muted=true, got %q", getResp["regnemester_muted"])
	}

	// Raw DB value should be stored as plaintext (this preference is non-sensitive).
	rawPrefs, err := GetPreferences(database, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if rawPrefs["regnemester_muted"] != "true" {
		t.Errorf("DB value: expected plaintext \"true\", got %q", rawPrefs["regnemester_muted"])
	}

	// Flip to false and verify the new value is persisted and returned.
	if putResp := put("false"); putResp["regnemester_muted"] != "false" {
		t.Errorf("PUT response: expected regnemester_muted=false, got %q", putResp["regnemester_muted"])
	}
	if getResp := get(); getResp["regnemester_muted"] != "false" {
		t.Errorf("GET response: expected regnemester_muted=false, got %q", getResp["regnemester_muted"])
	}
}

// TestPreferencesPutHandler_BatchMultipleKeys verifies that a single request
// carrying several keys persists all of them (one atomic batch write).
func TestPreferencesPutHandler_BatchMultipleKeys(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	// All five HR/pace fields in one batch (the motivating use case).
	body := `{"preferences":{"max_hr":"190","threshold_hr":"175","threshold_pace":"300","resting_hr":"50","easy_pace_min":"360"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]string{
		"max_hr":         "190",
		"threshold_hr":   "175",
		"threshold_pace": "300",
		"resting_hr":     "50",
		"easy_pace_min":  "360",
	}
	for k, v := range want {
		if resp["preferences"][k] != v {
			t.Errorf("response %s: expected %q, got %q", k, v, resp["preferences"][k])
		}
	}

	// Verify every key was persisted to the DB.
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	for k, v := range want {
		if stored[k] != v {
			t.Errorf("DB %s: expected %q, got %q", k, v, stored[k])
		}
	}
}

// TestPreferencesPutHandler_BatchPartialInvalidRejectsAll verifies that if any
// key in a batch fails validation, the whole batch is rejected and no key is
// written (all-or-nothing).
func TestPreferencesPutHandler_BatchPartialInvalidRejectsAll(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	// max_hr is valid, but resting_hr is out of range (30–100) — the batch must fail.
	body := `{"preferences":{"max_hr":"185","resting_hr":"5"}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range value, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Neither key should have been persisted (validation happens before any write).
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if _, ok := stored["max_hr"]; ok {
		t.Errorf("max_hr must not be persisted when the batch is rejected, got %q", stored["max_hr"])
	}
	if _, ok := stored["resting_hr"]; ok {
		t.Errorf("resting_hr must not be persisted when the batch is rejected, got %q", stored["resting_hr"])
	}
}

// TestPreferencesPutHandler_BatchEmpty verifies that an empty batch is accepted
// as a no-op and returns the current preferences.
func TestPreferencesPutHandler_BatchEmpty(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Seed one existing preference so we can confirm it survives the no-op.
	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty batch, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["preferences"]["theme"] != "dark" {
		t.Errorf("expected existing theme=dark to survive empty batch, got %q", resp["preferences"]["theme"])
	}
}

// TestSetPreferences_Transaction verifies the batched upsert helper writes every
// pair and that an empty map is a no-op.
func TestSetPreferences_Transaction(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Empty map is a no-op.
	if err := SetPreferences(db, userID, map[string]string{}); err != nil {
		t.Fatalf("SetPreferences(empty): %v", err)
	}

	// Insert several keys.
	if err := SetPreferences(db, userID, map[string]string{
		"theme":  "dark",
		"max_hr": "190",
	}); err != nil {
		t.Fatalf("SetPreferences(insert): %v", err)
	}
	stored, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["theme"] != "dark" || stored["max_hr"] != "190" {
		t.Errorf("after insert: got %v", stored)
	}

	// Upsert one existing key and add a new one.
	if err := SetPreferences(db, userID, map[string]string{
		"max_hr":     "185",
		"resting_hr": "48",
	}); err != nil {
		t.Fatalf("SetPreferences(upsert): %v", err)
	}
	stored, err = GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if stored["max_hr"] != "185" || stored["resting_hr"] != "48" || stored["theme"] != "dark" {
		t.Errorf("after upsert: got %v", stored)
	}
}

func TestPreferencesPutHandler_DashboardWidgets(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	layoutJSON := `{"order":["greeting","calendar","weather"],"hidden":["lactate"]}`
	body := `{"preferences":{"dashboard_widgets":` + layoutJSON + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored := resp["preferences"]["dashboard_widgets"]
	if stored == "" {
		t.Fatal("expected dashboard_widgets to be stored")
	}

	var layout struct {
		Order  []string `json:"order"`
		Hidden []string `json:"hidden"`
	}
	if err := json.Unmarshal([]byte(stored), &layout); err != nil {
		t.Fatalf("unmarshal stored layout: %v", err)
	}
	if len(layout.Order) != 3 || layout.Order[1] != "calendar" {
		t.Errorf("unexpected order: %v", layout.Order)
	}
	if len(layout.Hidden) != 1 || layout.Hidden[0] != "lactate" {
		t.Errorf("unexpected hidden: %v", layout.Hidden)
	}

	// A GET must round-trip the same value.
	getHandler := RequireAuth(db)(PreferencesGetHandler(db))
	getReq := httptest.NewRequest("GET", "/api/settings/preferences", nil)
	getReq.AddCookie(&http.Cookie{Name: "session", Value: token})
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET, got %d", getRec.Code)
	}
	var getResp map[string]map[string]string
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if getResp["preferences"]["dashboard_widgets"] != stored {
		t.Errorf("GET round-trip mismatch: got %q, want %q", getResp["preferences"]["dashboard_widgets"], stored)
	}
}

func TestPreferencesPutHandler_DashboardWidgetsEmptyArraysResetsLayout(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))
	body := `{"preferences":{"dashboard_widgets":{"order":[],"hidden":[]}}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPreferencesPutHandler_DashboardWidgetsRejectsInvalid(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	handler := RequireAuth(db)(PreferencesPutHandler(db))

	longID := `"` + strings.Repeat("a", 65) + `"`
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = `"w` + strconv.Itoa(i) + `"`
	}

	cases := []struct {
		name  string
		value string
	}{
		{"not an object", `["greeting"]`},
		{"order is not an array", `{"order":"greeting","hidden":[]}`},
		{"order contains a non-string", `{"order":[1,2],"hidden":[]}`},
		{"unknown field", `{"order":[],"hidden":[],"columns":3}`},
		{"bare string", `"greeting"`},
		{"order too long", `{"order":[` + strings.Join(tooMany, ",") + `],"hidden":[]}`},
		{"hidden too long", `{"order":[],"hidden":[` + strings.Join(tooMany, ",") + `]}`},
		{"id too long", `{"order":[` + longID + `],"hidden":[]}`},
		{"id has bad characters", `{"order":["Greeting!"],"hidden":[]}`},
		{"hidden id has bad characters", `{"order":[],"hidden":["quick links"]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"preferences":{"dashboard_widgets":` + tc.value + `}}`
			req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "session", Value: token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
			}
			var errResp map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if errResp["error"] == "" {
				t.Error("expected a JSON error message")
			}
		})
	}

	// Nothing may have been persisted by the rejected requests.
	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if _, ok := prefs["dashboard_widgets"]; ok {
		t.Error("expected dashboard_widgets not to be stored after rejections")
	}
}
