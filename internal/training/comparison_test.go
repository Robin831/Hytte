package training

import (
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var testHashCounter atomic.Int64

func makeLaps(durations ...float64) []Lap {
	laps := make([]Lap, len(durations))
	for i, d := range durations {
		laps[i] = Lap{LapNumber: i + 1, DurationSeconds: d}
	}
	return laps
}

func TestAreLapsSimilar_SameLapCount(t *testing.T) {
	a := makeLaps(300, 300, 300)
	b := makeLaps(310, 290, 305)
	if !areLapsSimilar(a, b, 0.3) {
		t.Error("expected similar laps with same count and close durations")
	}
}

func TestAreLapsSimilar_PlusOneLap(t *testing.T) {
	a := makeLaps(300, 300, 300)
	b := makeLaps(310, 290, 305, 300) // one extra lap
	if !areLapsSimilar(a, b, 0.3) {
		t.Error("expected similar with +1 lap difference")
	}
}

func TestAreLapsSimilar_MinusOneLap(t *testing.T) {
	a := makeLaps(300, 300, 300, 300)
	b := makeLaps(310, 290, 305) // one fewer lap
	if !areLapsSimilar(a, b, 0.3) {
		t.Error("expected similar with -1 lap difference")
	}
}

func TestAreLapsSimilar_RejectsLargeCountDiff(t *testing.T) {
	a := makeLaps(300, 300, 300)
	b := makeLaps(300, 300, 300, 300, 300) // +2 laps
	if areLapsSimilar(a, b, 0.3) {
		t.Error("expected rejection when lap counts differ by more than 1")
	}
}

func TestAreLapsSimilar_RejectsWildlyDifferentCounts(t *testing.T) {
	a := makeLaps(300, 300, 300)
	b := makeLaps(300, 300, 300, 300, 300, 300, 300, 300, 300, 300) // +7 laps
	if areLapsSimilar(a, b, 0.3) {
		t.Error("expected rejection when lap counts are wildly different")
	}
}

func TestAreLapsSimilar_RejectsDurationOutsideTolerance(t *testing.T) {
	a := makeLaps(300, 300, 300)
	b := makeLaps(300, 300, 500) // last lap way off
	if areLapsSimilar(a, b, 0.3) {
		t.Error("expected rejection when a lap duration exceeds tolerance")
	}
}

func TestAreLapsSimilar_EmptyLaps(t *testing.T) {
	if areLapsSimilar(nil, makeLaps(300), 0.3) {
		t.Error("expected false for empty first arg")
	}
	if areLapsSimilar(makeLaps(300), nil, 0.3) {
		t.Error("expected false for empty second arg")
	}
	if areLapsSimilar(nil, nil, 0.3) {
		t.Error("expected false for both empty")
	}
}

func TestAreLapsSimilar_SkipsZeroDuration(t *testing.T) {
	a := makeLaps(300, 0, 300)
	b := makeLaps(310, 0, 290)
	if !areLapsSimilar(a, b, 0.3) {
		t.Error("expected similar when zero-duration laps are skipped")
	}
}

func TestFindSimilarWorkouts_PlusMinusOne(t *testing.T) {
	database := setupTestDB(t)

	// Insert reference workout with 3 laps.
	refID := insertTestWorkout(t, database, 1, "running", 300, 300, 300)

	// Same lap count — should match.
	sameID := insertTestWorkout(t, database, 1, "running", 310, 290, 305)

	// +1 lap — should match.
	plusOneID := insertTestWorkout(t, database, 1, "running", 310, 290, 305, 300)

	// -1 lap — should match.
	minusOneID := insertTestWorkout(t, database, 1, "running", 310, 290)

	// +2 laps — should NOT match.
	insertTestWorkout(t, database, 1, "running", 310, 290, 305, 300, 300)

	// Different sport — should NOT match.
	insertTestWorkout(t, database, 1, "cycling", 300, 300, 300)

	results, err := FindSimilarWorkouts(database, refID, 1)
	if err != nil {
		t.Fatalf("FindSimilarWorkouts: %v", err)
	}

	ids := map[int64]bool{}
	for _, w := range results {
		ids[w.ID] = true
	}

	if !ids[sameID] {
		t.Error("expected same-count workout to be similar")
	}
	if !ids[plusOneID] {
		t.Error("expected +1 lap workout to be similar")
	}
	if !ids[minusOneID] {
		t.Error("expected -1 lap workout to be similar")
	}
	if len(results) != 3 {
		t.Errorf("expected 3 similar workouts, got %d", len(results))
	}
}

// insertTestWorkout inserts a workout with the given lap durations and returns its ID.
func insertTestWorkout(t *testing.T, db_ *sql.DB, userID int64, sport string, durations ...float64) int64 {
	t.Helper()
	hash := fmt.Sprintf("testhash%d", testHashCounter.Add(1))
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, sport, sport+" workout", time.Now().UTC().Format(time.RFC3339), sumFloats(durations), hash,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	wID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	for i, d := range durations {
		_, err := db_.Exec(
			`INSERT INTO workout_laps (workout_id, lap_number, start_offset_ms, duration_seconds, distance_meters,
			 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence)
			 VALUES (?, ?, 0, ?, 0, 0, 0, 0, 0)`,
			wID, i+1, d,
		)
		if err != nil {
			t.Fatalf("insert lap: %v", err)
		}
	}
	return wID
}

func sumFloats(fs []float64) float64 {
	var s float64
	for _, f := range fs {
		s += f
	}
	return s
}

// insertTestWorkoutWithHR inserts a workout whose laps have specified HR values.
func insertTestWorkoutWithHR(t *testing.T, db_ *sql.DB, userID int64, sport string, hrs []int, durations []float64) int64 {
	t.Helper()
	if len(hrs) != len(durations) {
		t.Fatal("hrs and durations must have the same length")
	}
	hash := fmt.Sprintf("testhash%d", testHashCounter.Add(1))
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, sport, sport+" workout", time.Now().UTC().Format(time.RFC3339), sumFloats(durations), hash,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	wID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	for i := range durations {
		_, err := db_.Exec(
			`INSERT INTO workout_laps (workout_id, lap_number, start_offset_ms, duration_seconds, distance_meters,
			 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence)
			 VALUES (?, ?, 0, ?, 0, ?, 0, 0, 0)`,
			wID, i+1, durations[i], hrs[i],
		)
		if err != nil {
			t.Fatalf("insert lap: %v", err)
		}
	}
	return wID
}

func TestCompareWorkouts_WithLapSelection(t *testing.T) {
	db := setupTestDB(t)

	// Workout A: 4 laps (warmup, interval, interval, cooldown).
	idA := insertTestWorkoutWithHR(t, db, 1, "running",
		[]int{120, 170, 175, 110},
		[]float64{600, 300, 300, 600},
	)
	// Workout B: 5 laps (warmup, jog, interval, interval, cooldown).
	idB := insertTestWorkoutWithHR(t, db, 1, "running",
		[]int{118, 140, 168, 172, 112},
		[]float64{600, 200, 300, 300, 600},
	)

	// Without lap selection, these are incompatible (different lap counts).
	result, err := CompareWorkouts(db, idA, idB, 1, nil, nil)
	if err != nil {
		t.Fatalf("CompareWorkouts: %v", err)
	}
	if result.Compatible {
		t.Error("expected incompatible without lap selection")
	}

	// With lap selection: pair A's intervals (1,2) with B's intervals (2,3) — 0-based.
	result, err = CompareWorkouts(db, idA, idB, 1, []int{1, 2}, []int{2, 3})
	if err != nil {
		t.Fatalf("CompareWorkouts with selection: %v", err)
	}
	if !result.Compatible {
		t.Errorf("expected compatible with lap selection, got reason: %s", result.Reason)
	}
	if len(result.LapDeltas) != 2 {
		t.Fatalf("expected 2 lap deltas, got %d", len(result.LapDeltas))
	}

	// Check that LapNumberA/B reflect the original lap numbers (1-based).
	if result.LapDeltas[0].LapNumberA != 2 || result.LapDeltas[0].LapNumberB != 3 {
		t.Errorf("first delta: expected lap_number_a=2, lap_number_b=3, got %d, %d",
			result.LapDeltas[0].LapNumberA, result.LapDeltas[0].LapNumberB)
	}

	// HR delta: B lap 3 (168) - A lap 2 (170) = -2.
	if result.LapDeltas[0].HRDelta != -2 {
		t.Errorf("expected HR delta -2, got %d", result.LapDeltas[0].HRDelta)
	}
}

func TestCompareWorkouts_LapSelectionMismatchedLengths(t *testing.T) {
	db := setupTestDB(t)
	idA := insertTestWorkout(t, db, 1, "running", 300, 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 300, 300, 300)

	result, err := CompareWorkouts(db, idA, idB, 1, []int{0, 1}, []int{0})
	if err != nil {
		t.Fatalf("CompareWorkouts: %v", err)
	}
	if result.Compatible {
		t.Error("expected incompatible when laps_a and laps_b differ in length")
	}
	if result.Reason != "laps_a and laps_b must have the same length" {
		t.Errorf("unexpected reason: %s", result.Reason)
	}
}

func TestCompareWorkouts_LapSelectionOutOfRange(t *testing.T) {
	db := setupTestDB(t)
	idA := insertTestWorkout(t, db, 1, "running", 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 300, 300)

	result, err := CompareWorkouts(db, idA, idB, 1, []int{0, 5}, []int{0, 1})
	if err != nil {
		t.Fatalf("CompareWorkouts: %v", err)
	}
	if result.Compatible {
		t.Error("expected incompatible when index is out of range")
	}
}

func TestCompareWorkouts_NilLapsFallsBackToAutomatic(t *testing.T) {
	db := setupTestDB(t)
	idA := insertTestWorkoutWithHR(t, db, 1, "running", []int{150, 160}, []float64{300, 300})
	idB := insertTestWorkoutWithHR(t, db, 1, "running", []int{148, 158}, []float64{310, 290})

	result, err := CompareWorkouts(db, idA, idB, 1, nil, nil)
	if err != nil {
		t.Fatalf("CompareWorkouts: %v", err)
	}
	if !result.Compatible {
		t.Errorf("expected compatible in automatic mode, got reason: %s", result.Reason)
	}
	if len(result.LapDeltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(result.LapDeltas))
	}
	// LapNumberA and LapNumberB should match in automatic mode.
	if result.LapDeltas[0].LapNumberA != result.LapDeltas[0].LapNumberB {
		t.Error("expected matching lap numbers in automatic mode")
	}
}
