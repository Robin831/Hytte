package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
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
	if len(body["preferences"]) != 0 {
		t.Errorf("expected empty preferences, got %v", body["preferences"])
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
	body := `{"preferences":{"notification_filter_events":"{\"push\":true,\"pull_request\":false,\"release\":true}"}}`
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
	body := `{"preferences":{"notification_filter_events":"{\"push\":true,\"bogus_event\":false}"}}`
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
	body := `{"preferences":{"notification_filter_events":` + string(mustMarshalString(string(eventsJSON))) + `}}`
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

// mustMarshalString JSON-encodes a string value (wraps it in quotes with escaping).
func mustMarshalString(s string) []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
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
	body2 := `{"preferences":{"quick_links":` + string(mustMarshalString(updatedJSON)) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(linksJSON)) + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many links, got %d; body: %s", rec.Code, rec.Body.String())
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
	if len(resp["preferences"]) != 0 {
		t.Errorf("disallowed key should not be stored, got %v", resp["preferences"])
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

	var resp struct {
		Sessions []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
			ExpiresAt string `json:"expires_at"`
			Current   bool   `json:"current"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(string(linksData))) + `}}`
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
	body := `{"preferences":{"quick_links":` + string(mustMarshalString(string(linksData))) + `}}`
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

func TestPreferencesPutHandler_GoalRaceKeys(t *testing.T) {
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
	prefs := resp["preferences"]
	for _, key := range []string{"goal_race_name", "goal_race_date", "goal_race_distance", "goal_race_target_time"} {
		if prefs[key] == "" {
			t.Errorf("expected %s to be set in response, got empty string", key)
		}
	}
	if prefs["goal_race_name"] != "Oslo Marathon" {
		t.Errorf("expected goal_race_name=Oslo Marathon, got %q", prefs["goal_race_name"])
	}
	if prefs["goal_race_date"] != "2026-09-20" {
		t.Errorf("expected goal_race_date=2026-09-20, got %q", prefs["goal_race_date"])
	}
	if prefs["goal_race_distance"] != "42.2" {
		t.Errorf("expected goal_race_distance=42.2, got %q", prefs["goal_race_distance"])
	}
	if prefs["goal_race_target_time"] != "3:45:00" {
		t.Errorf("expected goal_race_target_time=3:45:00, got %q", prefs["goal_race_target_time"])
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
	body := `{"preferences":{"zone_boundaries":` + zones + `}}`
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
	body := `{"preferences":{"zone_boundaries":` + zones + `}}`
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
	body := `{"preferences":{"zone_boundaries":` + zones + `}}`
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
	body := `{"preferences":{"zone_boundaries":` + zones + `}}`
	req := httptest.NewRequest("PUT", "/api/settings/preferences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for max_bpm <= min_bpm, got %d", rec.Code)
	}
}
