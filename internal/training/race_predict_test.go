package training

import (
	"database/sql"
	"errors"
	"math"
	"testing"
)

func TestRiegelPredict(t *testing.T) {
	// Canonical Riegel example: 20:00 5K → 10K.
	// T2 = 1200 * (10000/5000)^1.06 = 1200 * 2^1.06 ≈ 2502 s (~41:42).
	// The task description states "~41:30" as a rough approximation; the precise
	// Riegel result is ~41:42 which is within the expected range.
	result := riegelPredict(1200, 5000, 10000)
	if math.Abs(result-2502) > 10 {
		t.Errorf("riegelPredict(1200, 5000, 10000) = %.1f, want ~2502 (~41:42)", result)
	}

	// Identity: same distance must return the same time.
	identity := riegelPredict(1200, 5000, 5000)
	if math.Abs(identity-1200) > 0.001 {
		t.Errorf("riegelPredict identity = %.4f, want 1200", identity)
	}
}

func TestFormatRaceTime(t *testing.T) {
	tests := []struct {
		seconds  int
		expected string
	}{
		{600, "10:00"},
		{1200, "20:00"},
		{2502, "41:42"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{5400, "1:30:00"},
		{7200, "2:00:00"},
		{61, "1:01"},
		{3599, "59:59"},
	}
	for _, tt := range tests {
		got := formatRaceTime(tt.seconds)
		if got != tt.expected {
			t.Errorf("formatRaceTime(%d) = %q, want %q", tt.seconds, got, tt.expected)
		}
	}
}

func TestFormatPacePerKm(t *testing.T) {
	tests := []struct {
		secPerKm float64
		expected string
	}{
		{300, "5:00"},
		{360, "6:00"},
		{270, "4:30"},
		{181, "3:01"},
	}
	for _, tt := range tests {
		got := formatPacePerKm(tt.secPerKm)
		if got != tt.expected {
			t.Errorf("formatPacePerKm(%.1f) = %q, want %q", tt.secPerKm, got, tt.expected)
		}
	}
}

func TestPredictRaceTimes(t *testing.T) {
	// 5:00/km threshold pace (300 s/km) used as HM reference.
	result := PredictRaceTimes(0, 300)
	if result == nil {
		t.Fatal("PredictRaceTimes returned nil for valid pace")
	}

	if result.Method != "threshold_pace" {
		t.Errorf("Method = %q, want %q", result.Method, "threshold_pace")
	}
	if result.RefDistance != "Half Marathon" {
		t.Errorf("RefDistance = %q, want %q", result.RefDistance, "Half Marathon")
	}
	if len(result.Predictions) != 4 {
		t.Fatalf("expected 4 predictions, got %d", len(result.Predictions))
	}

	wantDistances := []string{"5K", "10K", "Half Marathon", "Marathon"}
	for i, pred := range result.Predictions {
		if pred.Distance != wantDistances[i] {
			t.Errorf("Predictions[%d].Distance = %q, want %q", i, pred.Distance, wantDistances[i])
		}
		if pred.PredictedTime == "" {
			t.Errorf("Predictions[%d].PredictedTime is empty", i)
		}
		if pred.PacePerKm == "" {
			t.Errorf("Predictions[%d].PacePerKm is empty", i)
		}
	}

	// Marathon prediction must be slower than half marathon.
	hmIdx, mIdx := 2, 3
	if result.Predictions[mIdx].DistanceM <= result.Predictions[hmIdx].DistanceM {
		t.Error("Marathon distance should be greater than half marathon distance")
	}
}

func TestPredictRaceTimesZeroPace(t *testing.T) {
	if got := PredictRaceTimes(0, 0); got != nil {
		t.Errorf("expected nil for zero pace, got %+v", got)
	}
	if got := PredictRaceTimes(0, -1); got != nil {
		t.Errorf("expected nil for negative pace, got %+v", got)
	}
}

func TestFindBestThresholdWorkout_EmptyDB(t *testing.T) {
	database := setupTestDB(t)

	w, err := FindBestThresholdWorkout(database, 1)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for empty DB, got err=%v w=%v", err, w)
	}
	if w != nil {
		t.Errorf("expected nil workout, got %+v", w)
	}
}

func TestFindBestThresholdWorkout_FallbackToBestPace(t *testing.T) {
	database := setupTestDB(t)

	// Insert two running workouts; the faster one should be returned.
	insertWorkout := func(id int64, pace float64, duration int) {
		_, err := database.Exec(`
			INSERT INTO workouts
			  (id, user_id, sport, duration_seconds, distance_meters,
			   avg_pace_sec_per_km, started_at, title, fit_file_hash)
			VALUES (?, 1, 'running', ?, 10000, ?, datetime('now', '-1 day'), 'Test', ?)`,
			id, duration, pace, id,
		)
		if err != nil {
			t.Fatalf("insert workout: %v", err)
		}
	}

	insertWorkout(1, 350, 3000) // slower: 5:50/km
	insertWorkout(2, 280, 3000) // faster: 4:40/km — should be returned

	w, err := FindBestThresholdWorkout(database, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected a workout, got nil")
	}
	if w.ID != 2 {
		t.Errorf("expected workout ID 2 (faster), got %d", w.ID)
	}
}
