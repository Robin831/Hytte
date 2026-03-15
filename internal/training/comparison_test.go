package training

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

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
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters)
		 VALUES (?, ?, ?, datetime('now'), ?, 0)`,
		userID, sport, sport+" workout", sumFloats(durations),
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	wID, _ := res.LastInsertId()

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
