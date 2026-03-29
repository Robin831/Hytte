package hrzones

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO users (google_id, email, name) VALUES ('g1', 'test@example.com', 'Test')",
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM users WHERE google_id = 'g1'").Scan(&id); err != nil {
		t.Fatalf("select user: %v", err)
	}
	return id
}

// TestGetDefaultZones_ZeroAndNegative ensures nil is returned for invalid maxHR.
func TestGetDefaultZones_ZeroAndNegative(t *testing.T) {
	for _, maxHR := range []int{0, -1, -100} {
		if zones := GetDefaultZones(maxHR); zones != nil {
			t.Errorf("GetDefaultZones(%d): expected nil, got %v", maxHR, zones)
		}
	}
}

// TestGetDefaultZones_Count verifies exactly 5 zones are returned.
func TestGetDefaultZones_Count(t *testing.T) {
	zones := GetDefaultZones(200)
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}
}

// TestGetDefaultZones_ZoneNumbers verifies zone numbers are 1-5.
func TestGetDefaultZones_ZoneNumbers(t *testing.T) {
	zones := GetDefaultZones(200)
	for i, z := range zones {
		if z.Zone != i+1 {
			t.Errorf("zones[%d].Zone = %d, want %d", i, z.Zone, i+1)
		}
	}
}

// TestGetDefaultZones_Boundaries verifies zone boundaries are computed correctly
// from the Olympiatoppen percentages (maxHR=200).
func TestGetDefaultZones_Boundaries(t *testing.T) {
	zones := GetDefaultZones(200)
	expected := []ZoneBoundary{
		{Zone: 1, MinBPM: 0, MaxBPM: 120},
		{Zone: 2, MinBPM: 120, MaxBPM: 144},
		{Zone: 3, MinBPM: 144, MaxBPM: 164},
		{Zone: 4, MinBPM: 164, MaxBPM: 184},
		{Zone: 5, MinBPM: 184, MaxBPM: 200},
	}
	for i, want := range expected {
		got := zones[i]
		if got.MinBPM != want.MinBPM || got.MaxBPM != want.MaxBPM {
			t.Errorf("zone %d: got {min=%d max=%d}, want {min=%d max=%d}",
				want.Zone, got.MinBPM, got.MaxBPM, want.MinBPM, want.MaxBPM)
		}
	}
}

// TestGetUserZones_NoPrefs returns nil when no prefs are set (no max_hr, no zone_boundaries).
func TestGetUserZones_NoPrefs(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	zones, err := GetUserZones(db, userID)
	if err != nil {
		t.Fatalf("GetUserZones: %v", err)
	}
	if zones != nil {
		t.Errorf("expected nil zones with no prefs, got %v", zones)
	}
}

// TestGetUserZones_MaxHR falls back to default zones when only max_hr is set.
func TestGetUserZones_MaxHR(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := auth.SetPreference(db, userID, "max_hr", "180"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	zones, err := GetUserZones(db, userID)
	if err != nil {
		t.Fatalf("GetUserZones: %v", err)
	}
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}
	// Zone 5 max should equal maxHR.
	if zones[4].MaxBPM != 180 {
		t.Errorf("zone 5 MaxBPM = %d, want 180", zones[4].MaxBPM)
	}
}

// TestGetUserZones_CustomZones uses zone_boundaries preference over max_hr.
func TestGetUserZones_CustomZones(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	custom := []ZoneBoundary{
		{Zone: 1, MinBPM: 0, MaxBPM: 110},
		{Zone: 2, MinBPM: 110, MaxBPM: 140},
		{Zone: 3, MinBPM: 140, MaxBPM: 160},
		{Zone: 4, MinBPM: 160, MaxBPM: 175},
		{Zone: 5, MinBPM: 175, MaxBPM: 190},
	}
	raw, _ := json.Marshal(custom)

	if err := auth.SetPreference(db, userID, "zone_boundaries", string(raw)); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	// Also set max_hr — custom zones should take precedence.
	if err := auth.SetPreference(db, userID, "max_hr", "200"); err != nil {
		t.Fatalf("SetPreference max_hr: %v", err)
	}

	zones, err := GetUserZones(db, userID)
	if err != nil {
		t.Fatalf("GetUserZones: %v", err)
	}
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}
	if zones[0].MaxBPM != 110 {
		t.Errorf("zone 1 MaxBPM = %d, want 110 (custom zones should override max_hr)", zones[0].MaxBPM)
	}
}

// TestGetUserZones_InvalidMaxHR returns an error when max_hr is not a valid integer.
func TestGetUserZones_InvalidMaxHR(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Write bad data directly — bypassing the handler validation.
	if _, err := db.Exec(
		"INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'max_hr', 'not-a-number')",
		userID,
	); err != nil {
		t.Fatalf("insert bad max_hr: %v", err)
	}

	_, err := GetUserZones(db, userID)
	if err == nil {
		t.Error("expected error for invalid max_hr, got nil")
	}
}

// TestGetUserZones_InvalidZoneBoundaries returns an error for malformed JSON.
func TestGetUserZones_InvalidZoneBoundaries(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'zone_boundaries', 'bad-json')",
		userID,
	); err != nil {
		t.Fatalf("insert bad zone_boundaries: %v", err)
	}

	_, err := GetUserZones(db, userID)
	if err == nil {
		t.Error("expected error for invalid zone_boundaries JSON, got nil")
	}
}
