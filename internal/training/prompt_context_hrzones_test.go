package training

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/hrzones"
)

func TestBuildUserProfileBlock_StoredZoneBoundaries(t *testing.T) {
	db := setupTestDB(t)

	// Store custom zone boundaries as JSON in user_preferences.
	zones := []hrzones.ZoneBoundary{
		{Zone: 1, MinBPM: 0, MaxBPM: 110},
		{Zone: 2, MinBPM: 110, MaxBPM: 135},
		{Zone: 3, MinBPM: 135, MaxBPM: 155},
		{Zone: 4, MinBPM: 155, MaxBPM: 175},
		{Zone: 5, MinBPM: 175, MaxBPM: 195},
	}
	raw, err := json.Marshal(zones)
	if err != nil {
		t.Fatalf("marshal zones: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'zone_boundaries', ?)`, string(raw)); err != nil {
		t.Fatalf("insert zone_boundaries pref: %v", err)
	}

	result := BuildUserProfileBlock(db, 1)

	if result == "" {
		t.Fatal("expected non-empty profile block when zone_boundaries is set")
	}
	// Should use the custom label, not "estimated from max HR" or lactate-derived.
	if !strings.Contains(result, "Training Zones (custom)") {
		t.Errorf("expected 'Training Zones (custom)' label, got: %s", result)
	}
	// Each zone should be present with its BPM range.
	wantLines := []string{
		"Zone 1 (Recovery): 0-110 bpm",
		"Zone 2 (Aerobic): 110-135 bpm",
		"Zone 3 (Tempo): 135-155 bpm",
		"Zone 4 (Threshold): 155-175 bpm",
		"Zone 5 (VO2max): 175-195 bpm",
	}
	for _, want := range wantLines {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in profile block, got: %s", want, result)
		}
	}
	// Should NOT fall back to max-HR estimated zones.
	if strings.Contains(result, "estimated from max HR") {
		t.Errorf("should not contain 'estimated from max HR' when stored zones are set, got: %s", result)
	}
}

func TestBuildUserProfileBlock_StoredZonesBeatLactate(t *testing.T) {
	db := setupTestDB(t)

	// Insert a lactate test AND stored zone boundaries — stored zones should win.
	insertTestLactateTest(t, db, 1)

	zones := []hrzones.ZoneBoundary{
		{Zone: 1, MinBPM: 0, MaxBPM: 108},
		{Zone: 2, MinBPM: 108, MaxBPM: 130},
		{Zone: 3, MinBPM: 130, MaxBPM: 152},
		{Zone: 4, MinBPM: 152, MaxBPM: 172},
		{Zone: 5, MinBPM: 172, MaxBPM: 190},
	}
	raw, err := json.Marshal(zones)
	if err != nil {
		t.Fatalf("marshal zones: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'zone_boundaries', ?)`, string(raw)); err != nil {
		t.Fatalf("insert zone_boundaries pref: %v", err)
	}

	result := BuildUserProfileBlock(db, 1)

	// Stored zones take priority — must show custom label, not Olympiatoppen/lactate.
	if !strings.Contains(result, "Training Zones (custom)") {
		t.Errorf("expected stored zone boundaries to take priority over lactate zones, got: %s", result)
	}
	// Should not show lactate-derived zone labels.
	if strings.Contains(result, "Olympiatoppen") || strings.Contains(result, "Norwegian") {
		t.Errorf("should not show lactate-derived zone labels when stored zones are set, got: %s", result)
	}
}

func TestBuildUserProfileBlock_InvalidStoredZones(t *testing.T) {
	db := setupTestDB(t)

	// Store invalid JSON — should gracefully fall back without panicking.
	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'zone_boundaries', 'not-valid-json')`); err != nil {
		t.Fatalf("insert invalid pref: %v", err)
	}
	// Also set max_hr so there's something to fall back to.
	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'max_hr', '190')`); err != nil {
		t.Fatalf("insert max_hr: %v", err)
	}

	// Should not panic and should fall back to estimated zones.
	result := BuildUserProfileBlock(db, 1)

	// With max_hr set, Training Zones block should still appear (estimated).
	if !strings.Contains(result, "Training Zones") {
		t.Errorf("expected Training Zones in fallback output, got: %s", result)
	}
	// Must NOT show custom label since the JSON was invalid.
	if strings.Contains(result, "Training Zones (custom)") {
		t.Errorf("should not show custom label for invalid JSON, got: %s", result)
	}
}
