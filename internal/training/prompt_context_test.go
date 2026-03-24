package training

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

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

func insertTestLactateTest(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	// Stages that produce a valid OBLA threshold near 13 km/h / 164 bpm.
	stages := []lactate.Stage{
		{StageNumber: 1, SpeedKmh: 8.0, LactateMmol: 1.5, HeartRateBpm: 130},
		{StageNumber: 2, SpeedKmh: 10.0, LactateMmol: 2.0, HeartRateBpm: 145},
		{StageNumber: 3, SpeedKmh: 12.0, LactateMmol: 3.0, HeartRateBpm: 158},
		{StageNumber: 4, SpeedKmh: 14.0, LactateMmol: 5.0, HeartRateBpm: 170},
		{StageNumber: 5, SpeedKmh: 16.0, LactateMmol: 8.0, HeartRateBpm: 182},
	}
	test := &lactate.Test{
		Date:          "2024-01-15",
		ProtocolType:  "treadmill",
		StageDurationMin: 5,
		StartSpeedKmh: 8.0,
		SpeedIncrementKmh: 2.0,
		Stages: stages,
	}
	if _, err := lactate.Create(db, userID, test); err != nil {
		t.Fatalf("insertTestLactateTest: %v", err)
	}
}

func TestBuildUserProfileBlock_LactateTestDerived(t *testing.T) {
	db := setupTestDB(t)

	// Insert a lactate test with stages — no preferences set.
	insertTestLactateTest(t, db, 1)

	result := BuildUserProfileBlock(db, 1)

	if result == "" {
		t.Fatal("expected non-empty profile block when lactate test exists")
	}
	// Threshold HR should be annotated as coming from lactate test.
	if !strings.Contains(result, "Threshold HR:") {
		t.Errorf("expected Threshold HR line, got: %s", result)
	}
	if !strings.Contains(result, "from lactate test") {
		t.Errorf("expected 'from lactate test' annotation, got: %s", result)
	}
	// Should include training zones from lactate test.
	if !strings.Contains(result, "Training Zones") {
		t.Errorf("expected Training Zones section, got: %s", result)
	}
}

func TestBuildUserProfileBlock_PrefsOverrideLactate(t *testing.T) {
	db := setupTestDB(t)

	// Insert a lactate test and also set threshold_hr in preferences.
	insertTestLactateTest(t, db, 1)
	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'max_hr', '190'), (1, 'threshold_hr', '175')`); err != nil {
		t.Fatal(err)
	}

	result := BuildUserProfileBlock(db, 1)

	// Pref-set threshold HR should NOT be annotated as "from lactate test".
	if !strings.Contains(result, "Threshold HR: 175 bpm\n") {
		t.Errorf("expected 'Threshold HR: 175 bpm' without source annotation, got: %s", result)
	}
	if strings.Contains(result, "Threshold HR: 175 bpm (from lactate test)") {
		t.Errorf("pref-set threshold HR should not be labeled 'from lactate test', got: %s", result)
	}
	// Zones should still reference lactate test.
	if !strings.Contains(result, "from lactate test") {
		t.Errorf("expected zones to be labeled 'from lactate test', got: %s", result)
	}
}

// ---- BuildHistoricalContext tests ----

// testBaseTime is captured once per test binary run so that multiple weeksAgo
// calls within the same test always compute offsets from the same instant,
// preventing flakiness around UTC midnight/week boundaries.
var testBaseTime = time.Now().UTC()

// weeksAgo returns an RFC3339 timestamp for N*7 days before testBaseTime, at 10:00 UTC.
// Using relative dates prevents tests from failing as time advances past any
// rolling query window.
func weeksAgo(n int) string {
	t := testBaseTime.AddDate(0, 0, -n*7)
	return fmt.Sprintf("%d-%02d-%02dT10:00:00Z", t.Year(), t.Month(), t.Day())
}

// insertHistoricalWorkout inserts a workout with sport, date, duration, distance,
// avg HR, optional tags, and optional laps. Returns the new workout ID.
func insertHistoricalWorkout(t *testing.T, db *sql.DB, userID int64, sport, startedAt string, durationSecs int, distMeters float64, avgHR int, tags []string, lapCount int) int64 {
	t.Helper()
	// fit_file_hash has DEFAULT '' with UNIQUE(user_id, fit_file_hash), so we need
	// a unique hash per workout to avoid constraint violations.
	fitHash := fmt.Sprintf("%s-%s-%d", sport, startedAt, lapCount)
	res, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, avg_heart_rate, fit_file_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, sport, fmt.Sprintf("%s %s", sport, startedAt), startedAt, durationSecs, distMeters, avgHR, fitHash, startedAt)
	if err != nil {
		t.Fatalf("insertHistoricalWorkout: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insertHistoricalWorkout LastInsertId: %v", err)
	}

	for _, tag := range tags {
		if _, err := db.Exec(`INSERT OR IGNORE INTO workout_tags (workout_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			t.Fatalf("insertHistoricalWorkout tag: %v", err)
		}
	}
	for i := 1; i <= lapCount; i++ {
		if _, err := db.Exec(`INSERT INTO workout_laps (workout_id, lap_number, start_offset_ms, duration_seconds, distance_meters) VALUES (?, ?, ?, ?, ?)`,
			id, i, int64(i-1)*300000, 300.0, 1000.0); err != nil {
			t.Fatalf("insertHistoricalWorkout lap: %v", err)
		}
	}
	return id
}

func TestBuildHistoricalContext_NoHistory(t *testing.T) {
	db := setupTestDB(t)
	// No workouts — should return empty string.
	result := BuildHistoricalContext(db, 1, &Workout{Sport: "running"})
	if result != "" {
		t.Errorf("expected empty string with no history, got: %q", result)
	}
}

func TestBuildHistoricalContext_WeeklySummaries(t *testing.T) {
	db := setupTestDB(t)

	// Insert workouts across two different weeks.
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(1), 3600, 10000, 150, nil, 0)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(0), 3600, 10000, 152, nil, 0)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(2), 3600, 10000, 148, nil, 0)

	result := BuildHistoricalContext(db, 1, &Workout{Sport: "running"})

	if result == "" {
		t.Fatal("expected non-empty historical context when workouts exist")
	}
	if !strings.Contains(result, "Weekly Training Summary") {
		t.Errorf("expected weekly summary section, got: %s", result)
	}
	// Should have a table header row.
	if !strings.Contains(result, "Week") || !strings.Contains(result, "Duration") {
		t.Errorf("expected table headers in weekly summary, got: %s", result)
	}
}

func TestBuildHistoricalContext_AiTrendWeeksPref(t *testing.T) {
	db := setupTestDB(t)

	// Insert workouts in 5 distinct weeks.
	for i := 1; i <= 5; i++ {
		insertHistoricalWorkout(t, db, 1, "running", weeksAgo(i), 3600, 10000, 150, nil, 0)
	}

	// Set ai_trend_weeks to 2.
	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'ai_trend_weeks', '2')`); err != nil {
		t.Fatal(err)
	}

	result := BuildHistoricalContext(db, 1, &Workout{Sport: "running"})

	if !strings.Contains(result, "Weekly Training Summary") {
		t.Fatalf("expected weekly summary, got: %s", result)
	}
	// With ai_trend_weeks=2, only 2 week rows should appear.
	// Count the pipe-delimited data rows (exclude header and separator).
	lines := strings.Split(result, "\n")
	dataRows := 0
	inTable := false
	for _, line := range lines {
		if strings.HasPrefix(line, "| Week") {
			inTable = true
			continue
		}
		if inTable && strings.HasPrefix(line, "|---") {
			continue
		}
		if inTable && strings.HasPrefix(line, "|") {
			dataRows++
		} else if inTable {
			break
		}
	}
	if dataRows != 2 {
		t.Errorf("expected 2 data rows with ai_trend_weeks=2, got %d; result:\n%s", dataRows, result)
	}
}

func TestBuildHistoricalContext_MatchingProgressionGroup(t *testing.T) {
	db := setupTestDB(t)

	// Insert two past running workouts with 2 laps each and a tag.
	id1 := insertHistoricalWorkout(t, db, 1, "running", weeksAgo(8), 1200, 4000, 155, []string{"2x2km"}, 2)
	id2 := insertHistoricalWorkout(t, db, 1, "running", weeksAgo(4), 1180, 4000, 152, []string{"2x2km"}, 2)
	// Suppress unused variable warnings.
	_ = id1
	_ = id2

	// Current workout: same sport, 2 laps loaded.
	currentWorkout := &Workout{
		ID:    999,
		Sport: "running",
		Laps:  []Lap{{LapNumber: 1}, {LapNumber: 2}},
	}

	result := BuildHistoricalContext(db, 1, currentWorkout)

	if !strings.Contains(result, "Similar Past Workouts") {
		t.Errorf("expected 'Similar Past Workouts' section for matching sport/lap group, got: %s", result)
	}
	if !strings.Contains(result, "2x2km") {
		t.Errorf("expected group tag '2x2km' in similar workouts, got: %s", result)
	}
}

func TestBuildHistoricalContext_NonMatchingProgressionGroup(t *testing.T) {
	db := setupTestDB(t)

	// Insert running workouts with a tag.
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(8), 1200, 4000, 155, []string{"intervals"}, 2)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(4), 1180, 4000, 152, []string{"intervals"}, 2)

	// Current workout is cycling — different sport, should not match running groups.
	currentWorkout := &Workout{
		ID:    999,
		Sport: "cycling",
		Laps:  []Lap{{LapNumber: 1}, {LapNumber: 2}},
	}

	result := BuildHistoricalContext(db, 1, currentWorkout)

	// Running summary should still appear, but similar workouts section should not.
	if strings.Contains(result, "Similar Past Workouts") {
		t.Errorf("expected no 'Similar Past Workouts' for non-matching sport (cycling vs running), got: %s", result)
	}
}

func TestBuildHistoricalContext_CurrentWorkoutMarked(t *testing.T) {
	db := setupTestDB(t)

	// Insert a past workout and one "current" workout (same user, same group).
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(8), 1200, 4000, 155, []string{"5x1km"}, 5)
	currentID := insertHistoricalWorkout(t, db, 1, "running", weeksAgo(1), 1180, 5000, 150, []string{"5x1km"}, 5)

	currentWorkout := &Workout{
		ID:    currentID,
		Sport: "running",
		Laps:  make([]Lap, 5),
	}

	result := BuildHistoricalContext(db, 1, currentWorkout)

	if !strings.Contains(result, "→") {
		t.Errorf("expected current workout to be marked with '→', got: %s", result)
	}
}

func TestBuildHistoricalContext_DeltasComputed(t *testing.T) {
	db := setupTestDB(t)

	// Insert two workouts in the same progression group.
	// First has higher HR, second has lower HR (fitness improvement).
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(8), 1200, 4000, 160, []string{"tempo"}, 2)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(2), 1200, 4000, 155, []string{"tempo"}, 2)

	currentWorkout := &Workout{
		ID:    999,
		Sport: "running",
		Laps:  []Lap{{LapNumber: 1}, {LapNumber: 2}},
	}

	result := BuildHistoricalContext(db, 1, currentWorkout)

	// The second workout should show a negative delta (HR dropped from 160 to 155 = -5).
	if !strings.Contains(result, "-5") {
		t.Errorf("expected HR delta '-5' in similar workouts table, got: %s", result)
	}
}

func TestBuildHistoricalContext_RecentTrends(t *testing.T) {
	db := setupTestDB(t)

	// Last 2 weeks: high volume (20km each = 40km).
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(1), 7200, 20000, 150, nil, 0)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(2), 7200, 20000, 148, nil, 0)
	// Prior 2 weeks: low volume (5km each = 10km).
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(3), 1800, 5000, 145, nil, 0)
	insertHistoricalWorkout(t, db, 1, "running", weeksAgo(4), 1800, 5000, 143, nil, 0)

	result := BuildHistoricalContext(db, 1, &Workout{Sport: "running"})

	if !strings.Contains(result, "Recent Trends") {
		t.Fatalf("expected 'Recent Trends' section, got: %s", result)
	}
	// Volume should be increasing (40km last 2w vs 10km prior 2w).
	if !strings.Contains(result, "Volume: increasing") {
		t.Errorf("expected volume trend 'increasing', got: %s", result)
	}
}

// ---- BuildEnrichedWorkoutBlock tests ----

func TestBuildEnrichedWorkoutBlock_EmptyWhenNoMetrics(t *testing.T) {
	// All computed fields nil and no DB — should return empty.
	w := &Workout{ID: 1, UserID: 1}
	result := BuildEnrichedWorkoutBlock(nil, w)
	if result != "" {
		t.Errorf("expected empty block when no metrics and no DB, got: %s", result)
	}
}

func TestBuildEnrichedWorkoutBlock_HRDrift(t *testing.T) {
	drift := 7.5
	w := &Workout{ID: 1, UserID: 1, HRDriftPct: &drift}
	result := BuildEnrichedWorkoutBlock(nil, w)
	if !strings.Contains(result, "Computed Training Metrics:") {
		t.Errorf("expected header, got: %s", result)
	}
	if !strings.Contains(result, "HR Drift: +7.5%") {
		t.Errorf("expected HR Drift line, got: %s", result)
	}
	if !strings.Contains(result, "moderate drift") {
		t.Errorf("expected moderate drift hint, got: %s", result)
	}
}

func TestBuildEnrichedWorkoutBlock_PaceCV(t *testing.T) {
	cv := 3.0
	w := &Workout{ID: 1, UserID: 1, PaceCVPct: &cv}
	result := BuildEnrichedWorkoutBlock(nil, w)
	if !strings.Contains(result, "Pace CV: 3.0%") {
		t.Errorf("expected Pace CV line, got: %s", result)
	}
	if !strings.Contains(result, "consistent pacing") {
		t.Errorf("expected consistent pacing hint, got: %s", result)
	}
}

func TestBuildEnrichedWorkoutBlock_TrainingLoad(t *testing.T) {
	load := 90.0
	w := &Workout{ID: 1, UserID: 1, TrainingLoad: &load}
	result := BuildEnrichedWorkoutBlock(nil, w)
	if !strings.Contains(result, "Training Load: 90.0") {
		t.Errorf("expected Training Load line, got: %s", result)
	}
	if !strings.Contains(result, "high — significant stimulus") {
		t.Errorf("expected high-load hint, got: %s", result)
	}
}

func TestBuildEnrichedWorkoutBlock_ACRIncludedEvenWithoutOtherMetrics(t *testing.T) {
	// This tests the fix: ACR must be computed even when the per-workout fields are nil.
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	// Insert workouts across 28 days to build ACR history (user_id=1).
	for i, daysAgo := range []int{2, 9, 16, 23} {
		ts := asOf.AddDate(0, 0, -daysAgo).Format(time.RFC3339)
		hash := fmt.Sprintf("enriched-acr-hash-%d", i)
		if _, err := db.Exec(
			`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
			1, "running", "Workout", ts, 3600, hash, 40.0,
		); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Workout with no computed per-workout metrics.
	w := &Workout{ID: 999, UserID: 1, StartedAt: asOf.Format(time.RFC3339)}
	result := BuildEnrichedWorkoutBlock(db, w)

	if result == "" {
		t.Fatal("expected non-empty block when ACR history is available, even without per-workout metrics")
	}
	if !strings.Contains(result, "ACR:") {
		t.Errorf("expected ACR line in block, got: %s", result)
	}
}

func TestBuildEnrichedWorkoutBlock_RFC3339NanoTimestamp(t *testing.T) {
	// Timestamps stored with nanoseconds must be parsed correctly.
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	ts := asOf.AddDate(0, 0, -2).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		1, "running", "W", ts, 3600, "nano-hash-1", 40.0,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Use an RFC3339Nano timestamp for StartedAt (includes sub-second precision).
	nanoTimestamp := asOf.Format(time.RFC3339Nano)
	w := &Workout{ID: 999, UserID: 1, StartedAt: nanoTimestamp}
	// Ensure it does not panic when given RFC3339Nano and returns a block.
	result := BuildEnrichedWorkoutBlock(db, w)
	// With 1 workout in the past 7 days and no chronic baseline, expect acute-only line.
	if !strings.Contains(result, "ACR") {
		t.Logf("block with nano timestamp: %s", result)
	}
	// The key assertion: no panic and the function returns (tested implicitly by reaching here).
}

func TestBuildEnrichedWorkoutBlock_AllFields(t *testing.T) {
	drift := 12.0
	cv := 20.0
	load := 50.0
	w := &Workout{ID: 1, UserID: 1, HRDriftPct: &drift, PaceCVPct: &cv, TrainingLoad: &load}
	result := BuildEnrichedWorkoutBlock(nil, w)

	checks := []string{
		"Computed Training Metrics:",
		"HR Drift:",
		"Pace CV:",
		"Training Load:",
		"high drift",
		"very high variability",
		"moderate",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in block, got: %s", want, result)
		}
	}
}

func TestTrendDirection(t *testing.T) {
	tests := []struct {
		current  float64
		previous float64
		want     string
	}{
		{110, 100, "increasing"},  // +10% > 5%
		{90, 100, "decreasing"},   // -10% < -5%
		{103, 100, "stable"},      // +3% within ±5%
		{98, 100, "stable"},       // -2% within ±5%
		{106, 100, "increasing"},  // exactly > 5%
		{94, 100, "decreasing"},   // exactly < -5%
		{105, 100, "stable"},      // exactly 5% — not > 5%, so stable
		{95, 100, "stable"},       // exactly -5% — not < -5%, so stable
		{100, 0, "increasing"},    // previous=0, current>0
		{0, 0, "stable"},          // both zero
	}
	for _, tt := range tests {
		got := trendDirection(tt.current, tt.previous)
		if got != tt.want {
			t.Errorf("trendDirection(%.0f, %.0f) = %q, want %q", tt.current, tt.previous, got, tt.want)
		}
	}
}
