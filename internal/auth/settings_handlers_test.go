package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if resp.Sessions[0].ID != token[:8] {
		t.Errorf("expected ID %s, got %s", token[:8], resp.Sessions[0].ID)
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
