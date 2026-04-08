package calendar

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-calendar-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return d
}

func createTestUser(t *testing.T, d *sql.DB) *auth.User {
	t.Helper()
	_, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test User', 'google-123')`)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return &auth.User{ID: 1, Email: "test@example.com", Name: "Test User"}
}

func TestEventsHandler_MissingParams(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	client := NewClient(d)

	handler := EventsHandler(d, client)

	req := httptest.NewRequest(http.MethodGet, "/api/calendar/events", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message")
	}
}

func TestEventsHandler_InvalidStartTime(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	client := NewClient(d)

	handler := EventsHandler(d, client)

	req := httptest.NewRequest(http.MethodGet, "/api/calendar/events?start=baddate&end=2026-04-01", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestEventsHandler_EmptyCache(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	client := NewClient(d)

	handler := EventsHandler(d, client)

	req := httptest.NewRequest(http.MethodGet, "/api/calendar/events?start=2026-04-01&end=2026-04-30", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	events, ok := resp["events"].([]any)
	if !ok {
		t.Fatal("expected events array in response")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestCalendarsHandler_NotConnected(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	client := NewClient(d)

	handler := CalendarsHandler(d, client)

	req := httptest.NewRequest(http.MethodGet, "/api/calendar/calendars", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["connected"] != false {
		t.Error("expected connected=false when no token stored")
	}
}

func TestSettingsHandler_SaveCalendars(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	handler := SettingsHandler(d)

	body := `{"calendar_ids": ["cal1@google.com", "cal2@google.com"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/calendar/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Verify the preference was saved.
	prefs, err := auth.GetPreferences(d, user.ID)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if prefs["calendar_visible_ids"] != "cal1@google.com,cal2@google.com" {
		t.Errorf("expected saved calendar IDs, got: %s", prefs["calendar_visible_ids"])
	}
}

func TestSettingsHandler_InvalidJSON(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	handler := SettingsHandler(d)

	req := httptest.NewRequest(http.MethodPut, "/api/calendar/settings", strings.NewReader("not json"))
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	events := []Event{
		{
			ID:          "evt-1",
			CalendarID:  "primary",
			Title:       "Morning Run",
			Description: "Easy 5km",
			Location:    "Park",
			StartTime:   "2026-04-08T07:00:00Z",
			EndTime:     "2026-04-08T08:00:00Z",
			AllDay:      false,
			Status:      "confirmed",
			Color:       "",
		},
		{
			ID:          "evt-2",
			CalendarID:  "primary",
			Title:       "Easter",
			Description: "",
			Location:    "",
			StartTime:   "2026-04-05",
			EndTime:     "2026-04-06",
			AllDay:      true,
			Status:      "confirmed",
			Color:       "1",
		},
	}

	if err := UpsertEvents(d, 1, events); err != nil {
		t.Fatalf("upsert events: %v", err)
	}

	// Query all events in April.
	result, err := QueryEvents(d, 1, nil, "2026-04-01", "2026-04-30T23:59:59Z")
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}

	// Verify decrypted content.
	if result[0].Title != "Easter" && result[1].Title != "Easter" {
		t.Error("expected to find 'Easter' event after decrypt")
	}

	// Delete one event.
	if err := DeleteEvents(d, 1, "primary", []string{"evt-1"}); err != nil {
		t.Fatalf("delete events: %v", err)
	}

	result, err = QueryEvents(d, 1, nil, "2026-04-01", "2026-04-30T23:59:59Z")
	if err != nil {
		t.Fatalf("query events after delete: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 event after delete, got %d", len(result))
	}
	if result[0].ID != "evt-2" {
		t.Errorf("expected evt-2 to remain, got %s", result[0].ID)
	}
}

func TestSyncTokenRoundTrip(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// No token initially.
	token, err := LoadSyncToken(d, 1, "primary")
	if err != nil {
		t.Fatalf("load sync token: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty sync token, got %q", token)
	}

	// Save a token.
	if err := SaveSyncToken(d, 1, "primary", "abc123"); err != nil {
		t.Fatalf("save sync token: %v", err)
	}

	token, err = LoadSyncToken(d, 1, "primary")
	if err != nil {
		t.Fatalf("load sync token: %v", err)
	}
	if token != "abc123" {
		t.Errorf("expected 'abc123', got %q", token)
	}

	// Update the token.
	if err := SaveSyncToken(d, 1, "primary", "def456"); err != nil {
		t.Fatalf("update sync token: %v", err)
	}

	token, err = LoadSyncToken(d, 1, "primary")
	if err != nil {
		t.Fatalf("load updated sync token: %v", err)
	}
	if token != "def456" {
		t.Errorf("expected 'def456', got %q", token)
	}

	// Clear all sync state.
	if err := ClearSyncState(d, 1); err != nil {
		t.Fatalf("clear sync state: %v", err)
	}

	token, err = LoadSyncToken(d, 1, "primary")
	if err != nil {
		t.Fatalf("load cleared sync token: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty after clear, got %q", token)
	}
}

func TestParseFlexibleTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2026-04-08", false},
		{"2026-04-08T10:00:00Z", false},
		{"2026-04-08T10:00:00+02:00", false},
		{"baddate", true},
		{"", true},
	}

	for _, tt := range tests {
		_, err := parseFlexibleTime(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseFlexibleTime(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestLoadVisibleCalendars(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// No preference set.
	ids := loadVisibleCalendars(d, 1)
	if len(ids) != 0 {
		t.Errorf("expected nil/empty, got %v", ids)
	}

	// Set preference.
	if err := auth.SetPreference(d, 1, "calendar_visible_ids", "a@g.com,b@g.com"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	ids = loadVisibleCalendars(d, 1)
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}
	if ids[0] != "a@g.com" || ids[1] != "b@g.com" {
		t.Errorf("unexpected IDs: %v", ids)
	}
}
