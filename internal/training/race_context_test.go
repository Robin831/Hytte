package training

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

func TestClassifyPacing(t *testing.T) {
	tests := []struct {
		name string
		laps []Lap
		want string
	}{
		{
			name: "negative split - getting faster",
			laps: []Lap{
				{LapNumber: 1, AvgPaceSecPerKm: 330, DistanceMeters: 1000},
				{LapNumber: 2, AvgPaceSecPerKm: 325, DistanceMeters: 1000},
				{LapNumber: 3, AvgPaceSecPerKm: 310, DistanceMeters: 1000},
				{LapNumber: 4, AvgPaceSecPerKm: 300, DistanceMeters: 1000},
			},
			want: "negative",
		},
		{
			name: "positive split - slowing down",
			laps: []Lap{
				{LapNumber: 1, AvgPaceSecPerKm: 280, DistanceMeters: 1000},
				{LapNumber: 2, AvgPaceSecPerKm: 285, DistanceMeters: 1000},
				{LapNumber: 3, AvgPaceSecPerKm: 310, DistanceMeters: 1000},
				{LapNumber: 4, AvgPaceSecPerKm: 330, DistanceMeters: 1000},
			},
			want: "positive",
		},
		{
			name: "even pacing",
			laps: []Lap{
				{LapNumber: 1, AvgPaceSecPerKm: 300, DistanceMeters: 1000},
				{LapNumber: 2, AvgPaceSecPerKm: 302, DistanceMeters: 1000},
				{LapNumber: 3, AvgPaceSecPerKm: 299, DistanceMeters: 1000},
				{LapNumber: 4, AvgPaceSecPerKm: 301, DistanceMeters: 1000},
			},
			want: "even",
		},
		{
			name: "single lap",
			laps: []Lap{
				{LapNumber: 1, AvgPaceSecPerKm: 300, DistanceMeters: 5000},
			},
			want: "even",
		},
		{
			name: "no pace data",
			laps: []Lap{
				{LapNumber: 1, AvgPaceSecPerKm: 0, DistanceMeters: 1000},
				{LapNumber: 2, AvgPaceSecPerKm: 0, DistanceMeters: 1000},
			},
			want: "even",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyPacing(tt.laps)
			if got != tt.want {
				t.Errorf("classifyPacing() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRiegelCompare(t *testing.T) {
	// 20:00 5K runner doing a 10K.
	// Riegel predicts ~41:42 (2502s). If they actually run 43:00 (2580s), that's ~3.1% slower.
	delta := riegelCompare(2580, 10000, 1200, 5000)
	if delta < 2.0 || delta > 4.0 {
		t.Errorf("riegelCompare = %.2f%%, expected ~3.1%%", delta)
	}

	// Faster than predicted: 40:00 (2400s) vs predicted ~41:42 (2502s) → negative delta.
	delta2 := riegelCompare(2400, 10000, 1200, 5000)
	if delta2 >= 0 {
		t.Errorf("riegelCompare = %.2f%%, expected negative (faster than predicted)", delta2)
	}

	// Exact match should be ~0%.
	predicted := riegelPredict(1200, 5000, 10000)
	delta3 := riegelCompare(int(math.Round(predicted)), 10000, 1200, 5000)
	if math.Abs(delta3) > 0.5 {
		t.Errorf("riegelCompare for exact prediction = %.2f%%, expected ~0%%", delta3)
	}
}

func TestIsRaceWorkout(t *testing.T) {
	raceID := int64(42)

	tests := []struct {
		name string
		w    *Workout
		want bool
	}{
		{
			name: "race_id set",
			w:    &Workout{RaceID: &raceID},
			want: true,
		},
		{
			name: "ai:type:race tag",
			w:    &Workout{Tags: []string{"ai:type:race", "ai:threshold"}},
			want: true,
		},
		{
			name: "not a race",
			w:    &Workout{Tags: []string{"ai:type:easy", "auto:treadmill"}},
			want: false,
		},
		{
			name: "no tags no race_id",
			w:    &Workout{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRaceWorkout(tt.w)
			if got != tt.want {
				t.Errorf("isRaceWorkout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildRaceContext_NotRace(t *testing.T) {
	db := setupTestDB(t)
	w := &Workout{ID: 1, UserID: 1, Sport: "running", Tags: []string{"ai:type:easy"}}
	rc := BuildRaceContext(db, w)
	if rc != nil {
		t.Error("expected nil RaceContext for non-race workout")
	}
}

func TestBuildRaceContext_WithRaceID(t *testing.T) {
	database := setupTestDB(t)

	// Insert a race.
	encName, err := encryption.EncryptField("Oslo Marathon")
	if err != nil {
		t.Fatalf("encrypt race name: %v", err)
	}
	encNotes, err := encryption.EncryptField("Goal is sub-3:30")
	if err != nil {
		t.Fatalf("encrypt race notes: %v", err)
	}
	targetTime := 12600 // 3:30:00
	_, err = database.Exec(`
		INSERT INTO stride_races (id, user_id, name, date, distance_m, target_time, priority, notes, created_at)
		VALUES (1, 1, ?, '2026-04-05', 42195, ?, 'A', ?, ?)
	`, encName, targetTime, encNotes, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert race: %v", err)
	}

	// Insert a workout linked to the race.
	raceID := int64(1)
	_, err = database.Exec(`
		INSERT INTO workouts (id, user_id, sport, duration_seconds, distance_meters, avg_pace_sec_per_km, started_at, title, fit_file_hash, race_id)
		VALUES (1, 1, 'running', 12900, 42195, 306, ?, 'Marathon', 'hash1', 1)
	`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	w := &Workout{
		ID:              1,
		UserID:          1,
		Sport:           "running",
		DurationSeconds: 12900,
		DistanceMeters:  42195,
		RaceID:          &raceID,
		Tags:            []string{"ai:type:race"},
		Laps: []Lap{
			{LapNumber: 1, AvgPaceSecPerKm: 300, DistanceMeters: 10000},
			{LapNumber: 2, AvgPaceSecPerKm: 302, DistanceMeters: 10000},
			{LapNumber: 3, AvgPaceSecPerKm: 310, DistanceMeters: 10000},
			{LapNumber: 4, AvgPaceSecPerKm: 320, DistanceMeters: 12195},
		},
	}

	rc := BuildRaceContext(database, w)
	if rc == nil {
		t.Fatal("expected non-nil RaceContext")
	}

	if rc.RaceName != "Oslo Marathon" {
		t.Errorf("RaceName = %q, want %q", rc.RaceName, "Oslo Marathon")
	}
	if rc.Priority != "A" {
		t.Errorf("Priority = %q, want %q", rc.Priority, "A")
	}
	if rc.TargetTime == nil || *rc.TargetTime != 12600 {
		t.Errorf("TargetTime = %v, want 12600", rc.TargetTime)
	}
	if rc.ActualTime != 12900 {
		t.Errorf("ActualTime = %d, want 12900", rc.ActualTime)
	}
	if rc.PacingProfile != "positive" {
		t.Errorf("PacingProfile = %q, want %q", rc.PacingProfile, "positive")
	}
	if rc.Notes != "Goal is sub-3:30" {
		t.Errorf("Notes = %q, want %q", rc.Notes, "Goal is sub-3:30")
	}
}

func TestFormatRacePromptSection(t *testing.T) {
	targetTime := 1200 // 20:00

	rc := &RaceContext{
		RaceName:      "Park Run",
		RaceDate:      "2026-04-05",
		DistanceM:     5000,
		TargetTime:    &targetTime,
		ActualTime:    1170, // 19:30
		Priority:      "B",
		PacingProfile: "negative",
		Notes:         "Felt strong",
		RiegelComparisons: []RiegelComparison{
			{
				RefRaceName: "10K City Run",
				RefDistance:  10000,
				RefTime:     2500,
				PredictedS:  1194,
				ActualS:     1170,
				DeltaPct:    -2.0,
			},
		},
		TrainingPhase:  "build",
		WeeklyVolumeKm: 45.5,
		PreviousRaces: []PastRace{
			{Name: "Spring 5K", Date: "2026-01-15", DistanceM: 5000, TimeS: 1250, PacePerKm: "4:10"},
		},
	}

	result := FormatRacePromptSection(rc)

	// Check key sections are present.
	checks := []string{
		"RACE ANALYSIS CONTEXT",
		"Park Run",
		"B-race",
		"Target Time: 20:00",
		"Actual Time: 19:30",
		"FASTER than target",
		"Negative split",
		"Riegel Formula Comparison",
		"10K City Run",
		"-2.0%",
		"Training Block Context",
		"build",
		"45.5 km",
		"Felt strong",
		"Previous Races",
		"Spring 5K",
		"RACE ANALYSIS INSTRUCTIONS",
		"Race Execution Analysis",
		"Goal Assessment",
		"Training Effectiveness",
		"Next-Race Recommendations",
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("FormatRacePromptSection missing %q", check)
		}
	}
}

func TestFormatRacePromptSection_Nil(t *testing.T) {
	result := FormatRacePromptSection(nil)
	if result != "" {
		t.Errorf("expected empty string for nil context, got %q", result)
	}
}

func TestBuildInsightsPrompt_WithRaceContext(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 1200,
		DistanceMeters:  5000,
		StartedAt:       "2026-04-05T10:00:00Z",
	}

	targetTime := 1250
	rc := &RaceContext{
		RaceName:      "Test Race",
		DistanceM:     5000,
		TargetTime:    &targetTime,
		ActualTime:    1200,
		Priority:      "A",
		PacingProfile: "even",
	}

	prompt := buildInsightsPrompt(w, "Analyze this workout.", "", false, nil, "", "", "", rc)

	if !strings.Contains(prompt, "RACE ANALYSIS CONTEXT") {
		t.Error("expected race context in prompt")
	}
	if !strings.Contains(prompt, "Test Race") {
		t.Error("expected race name in prompt")
	}
	if !strings.Contains(prompt, "RACE ANALYSIS INSTRUCTIONS") {
		t.Error("expected race analysis instructions in prompt")
	}
}

func TestBuildInsightsPrompt_WithoutRaceContext(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		StartedAt:       "2026-04-05T10:00:00Z",
	}

	prompt := buildInsightsPrompt(w, "Analyze this workout.", "", false, nil, "", "", "", nil)

	if strings.Contains(prompt, "RACE ANALYSIS CONTEXT") {
		t.Error("did not expect race context in prompt for nil RaceContext")
	}
}
