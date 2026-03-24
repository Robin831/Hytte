package training

import (
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/lactate"
)

func TestBuildUserProfileBlock_Empty(t *testing.T) {
	db := setupTestDB(t)
	// User has no preferences and no lactate tests — should return empty string.
	result := BuildUserProfileBlock(db, 1)
	if result != "" {
		t.Errorf("expected empty string for user with no profile data, got: %s", result)
	}
}

func TestBuildUserProfileBlock_MaxHROnly(t *testing.T) {
	db := setupTestDB(t)

	// Set max HR only.
	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'max_hr', '195')`)
	if err != nil {
		t.Fatal(err)
	}

	result := BuildUserProfileBlock(db, 1)

	if result == "" {
		t.Fatal("expected non-empty profile block when max_hr is set")
	}
	if !strings.Contains(result, "Max HR: 195 bpm") {
		t.Errorf("expected Max HR in profile, got: %s", result)
	}
	// Should include estimated zones based on max HR.
	if !strings.Contains(result, "Training Zones") {
		t.Errorf("expected Training Zones section when max HR is known, got: %s", result)
	}
	if !strings.Contains(result, "estimated from max HR") {
		t.Errorf("expected 'estimated from max HR' source label, got: %s", result)
	}
}

func TestBuildUserProfileBlock_FullProfile(t *testing.T) {
	db := setupTestDB(t)

	prefs := []struct{ k, v string }{
		{"max_hr", "195"},
		{"resting_hr", "48"},
		{"threshold_hr", "172"},
		{"threshold_pace", "270"}, // 4:30/km
	}
	for _, p := range prefs {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, p.k, p.v); err != nil {
			t.Fatalf("insert pref %s: %v", p.k, err)
		}
	}

	result := BuildUserProfileBlock(db, 1)

	checks := []string{
		"Max HR: 195 bpm",
		"Resting HR: 48 bpm",
		"Threshold HR: 172 bpm",
		"Threshold Pace: 4:30/km",
		"Training Zones",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in profile block, got: %s", want, result)
		}
	}
}

func TestBuildUserProfileBlock_NoProfile(t *testing.T) {
	db := setupTestDB(t)
	// No preferences at all — returns empty string.
	result := BuildUserProfileBlock(db, 99)
	if result != "" {
		t.Errorf("expected empty string for unknown user, got: %q", result)
	}
}

func TestBuildMaxHRZones(t *testing.T) {
	zones := buildMaxHRZones(200)

	if zones == nil {
		t.Fatal("expected non-nil zones result")
	}
	if len(zones.Zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones.Zones))
	}
	if zones.MaxHR != 200 {
		t.Errorf("expected MaxHR=200, got %d", zones.MaxHR)
	}
	// Zone 5 should top out at maxHR.
	last := zones.Zones[len(zones.Zones)-1]
	if last.MaxHR != 200 {
		t.Errorf("zone 5 MaxHR should be maxHR (200), got %d", last.MaxHR)
	}
	// Zone 1 should start at 0.
	if zones.Zones[0].MinHR != 0 {
		t.Errorf("zone 1 MinHR should be 0, got %d", zones.Zones[0].MinHR)
	}
}

func TestFormatZoneLine_NoSpeed(t *testing.T) {
	z := lactate.TrainingZone{
		Zone:  2,
		Name:  "I2 - Endurance",
		MinHR: 120,
		MaxHR: 150,
	}
	line := formatZoneLine(z)
	if !strings.Contains(line, "Zone 2") {
		t.Errorf("expected Zone 2 in line, got: %s", line)
	}
	if !strings.Contains(line, "120-150 bpm") {
		t.Errorf("expected HR range in line, got: %s", line)
	}
	// No speed data — should not contain "/km".
	if strings.Contains(line, "/km") {
		t.Errorf("should not contain pace info when no speed data, got: %s", line)
	}
}

func TestFormatZoneLine_Zone1WithSpeed(t *testing.T) {
	z := lactate.TrainingZone{
		Zone:        1,
		Name:        "I1 - Recovery",
		MinHR:       0,
		MaxHR:       140,
		MinSpeedKmh: 0,
		MaxSpeedKmh: 8.28, // ~7:15/km
	}
	line := formatZoneLine(z)
	if !strings.Contains(line, "slower than") {
		t.Errorf("zone 1 should say 'slower than', got: %s", line)
	}
}

func TestFormatZoneLine_MidZoneWithSpeed(t *testing.T) {
	z := lactate.TrainingZone{
		Zone:        2,
		Name:        "I2 - Endurance",
		MinHR:       140,
		MaxHR:       162,
		MinSpeedKmh: 8.28,
		MaxSpeedKmh: 9.43, // ~6:20/km
	}
	line := formatZoneLine(z)
	// Should contain a pace range (faster-slower/km).
	if !strings.Contains(line, "/km") {
		t.Errorf("expected pace range in line, got: %s", line)
	}
	// Should not say "slower than".
	if strings.Contains(line, "slower than") {
		t.Errorf("mid zone should not say 'slower than', got: %s", line)
	}
}

func TestFormatPaceFromSpeed(t *testing.T) {
	tests := []struct {
		speed float64
		want  string
	}{
		{10.0, "6:00"},  // 3600/10 = 360s = 6:00
		{12.0, "5:00"},  // 3600/12 = 300s = 5:00
		{15.0, "4:00"},  // 3600/15 = 240s = 4:00
		{0, "--:--"},
		{-1, "--:--"},
		// Near-boundary case: 3600/10.01 ≈ 359.64s → rounds to 360s = 6:00.
		// Old buggy code: mins=int(359.64)/60=5, secs=round(359.64)%60=0 → wrong "5:00".
		{10.01, "6:00"},
	}
	for _, tt := range tests {
		got := formatPaceFromSpeed(tt.speed)
		if got != tt.want {
			t.Errorf("formatPaceFromSpeed(%.2f) = %q, want %q", tt.speed, got, tt.want)
		}
	}
}

func TestBuildUserTrainingProfile_ThresholdHR(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'max_hr', '200'), (1, 'threshold_hr', '175')`)
	if err != nil {
		t.Fatal(err)
	}

	profile := BuildUserTrainingProfile(db, 1)
	if profile.ThresholdHR != 175 {
		t.Errorf("expected ThresholdHR=175, got %d", profile.ThresholdHR)
	}
	if !strings.Contains(profile.Block, "Threshold HR: 175 bpm") {
		t.Errorf("expected threshold HR in block, got: %s", profile.Block)
	}
}

func TestBuildUserProfileBlock_EasyPaceRange(t *testing.T) {
	db := setupTestDB(t)

	prefs := []struct{ k, v string }{
		{"max_hr", "195"},
		{"easy_pace_min", "330"}, // 5:30/km
		{"easy_pace_max", "420"}, // 7:00/km
	}
	for _, p := range prefs {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, p.k, p.v); err != nil {
			t.Fatalf("insert pref %s: %v", p.k, err)
		}
	}

	result := BuildUserProfileBlock(db, 1)

	if !strings.Contains(result, "Easy Pace Range: 5:30-7:00/km") {
		t.Errorf("expected easy pace range in profile, got: %s", result)
	}
}

func TestParseIntPref(t *testing.T) {
	prefs := map[string]string{
		"max_hr":   "195",
		"bad_val":  "abc",
		"neg_val":  "-5",
		"zero_val": "0",
	}

	if got := parseIntPref(prefs, "max_hr"); got != 195 {
		t.Errorf("expected 195, got %d", got)
	}
	if got := parseIntPref(prefs, "missing"); got != 0 {
		t.Errorf("expected 0 for missing key, got %d", got)
	}
	if got := parseIntPref(prefs, "bad_val"); got != 0 {
		t.Errorf("expected 0 for non-integer value, got %d", got)
	}
	if got := parseIntPref(prefs, "neg_val"); got != 0 {
		t.Errorf("expected 0 for negative value, got %d", got)
	}
	if got := parseIntPref(prefs, "zero_val"); got != 0 {
		t.Errorf("expected 0 for zero value, got %d", got)
	}
}
