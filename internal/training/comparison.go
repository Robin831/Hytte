package training

import (
	"database/sql"
	"fmt"
	"math"
)

// CompareWorkouts compares two workouts by matching their laps.
// When lapsA and lapsB are provided (both non-nil, same length), only the
// specified lap indices (0-based) are paired for comparison, bypassing the
// automatic compatibility checks. This enables comparing workouts with
// different lap counts by letting the caller choose which laps to match.
func CompareWorkouts(db *sql.DB, idA, idB, userID int64, lapsA, lapsB []int) (*ComparisonResult, error) {
	wA, err := getWorkoutWithLaps(db, idA, userID)
	if err != nil {
		return nil, err
	}
	wB, err := getWorkoutWithLaps(db, idB, userID)
	if err != nil {
		return nil, err
	}

	result := &ComparisonResult{
		WorkoutA: WorkoutSummary{ID: wA.ID, Title: wA.Title, StartedAt: wA.StartedAt, Sport: wA.Sport},
		WorkoutB: WorkoutSummary{ID: wB.ID, Title: wB.Title, StartedAt: wB.StartedAt, Sport: wB.Sport},
	}

	// If explicit lap selections are provided, use them directly.
	if lapsA != nil && lapsB != nil {
		if len(lapsA) != len(lapsB) {
			result.Reason = "laps_a and laps_b must have the same length"
			return result, nil
		}
		if len(lapsA) == 0 {
			result.Reason = "lap selections are empty"
			return result, nil
		}
		// Validate indices are in bounds.
		for _, idx := range lapsA {
			if idx < 0 || idx >= len(wA.Laps) {
				result.Reason = fmt.Sprintf("laps_a index %d out of range (workout A has %d laps)", idx, len(wA.Laps))
				return result, nil
			}
		}
		for _, idx := range lapsB {
			if idx < 0 || idx >= len(wB.Laps) {
				result.Reason = fmt.Sprintf("laps_b index %d out of range (workout B has %d laps)", idx, len(wB.Laps))
				return result, nil
			}
		}

		// Retain the sport check — cross-sport comparisons produce misleading deltas.
		if wA.Sport != wB.Sport {
			result.Reason = "different sports"
			return result, nil
		}

		result.Compatible = true
		return buildLapDeltas(result, wA, wB, lapsA, lapsB), nil
	}

	// Automatic mode: check compatibility — same sport, equal lap count, similar durations.
	if wA.Sport != wB.Sport {
		result.Reason = "different sports"
		return result, nil
	}

	if len(wA.Laps) == 0 || len(wB.Laps) == 0 {
		result.Reason = "one or both workouts have no laps"
		return result, nil
	}

	if len(wA.Laps) != len(wB.Laps) {
		result.Reason = "different number of laps"
		return result, nil
	}

	// Check that lap durations are within 20% tolerance.
	for i := range wA.Laps {
		durA := wA.Laps[i].DurationSeconds
		durB := wB.Laps[i].DurationSeconds
		if durA > 0 && durB > 0 {
			ratio := durA / durB
			if ratio < 0.8 || ratio > 1.2 {
				result.Reason = "lap durations differ significantly"
				return result, nil
			}
		}
	}

	result.Compatible = true

	// Build 1:1 lap pairing using sequential indices.
	indices := make([]int, len(wA.Laps))
	for i := range indices {
		indices[i] = i
	}
	return buildLapDeltas(result, wA, wB, indices, indices), nil
}

// buildLapDeltas computes deltas for the given lap index pairs and appends a summary.
func buildLapDeltas(result *ComparisonResult, wA, wB *Workout, lapsA, lapsB []int) *ComparisonResult {
	var totalHRDelta float64
	var totalPaceDelta float64
	for i := range lapsA {
		lapA := wA.Laps[lapsA[i]]
		lapB := wB.Laps[lapsB[i]]

		delta := LapDelta{
			LapNumber:    i + 1,
			LapNumberA:   lapA.LapNumber,
			LapNumberB:   lapB.LapNumber,
			DurationDiff: lapB.DurationSeconds - lapA.DurationSeconds,
			AvgHRA:       lapA.AvgHeartRate,
			AvgHRB:       lapB.AvgHeartRate,
			HRDelta:      lapB.AvgHeartRate - lapA.AvgHeartRate,
			PaceA:        lapA.AvgPaceSecPerKm,
			PaceB:        lapB.AvgPaceSecPerKm,
			PaceDelta:    lapB.AvgPaceSecPerKm - lapA.AvgPaceSecPerKm,
		}
		result.LapDeltas = append(result.LapDeltas, delta)
		totalHRDelta += float64(delta.HRDelta)
		totalPaceDelta += delta.PaceDelta
	}

	n := float64(len(lapsA))
	avgHRDelta := totalHRDelta / n
	avgPaceDelta := totalPaceDelta / n

	verdict := "no significant change"
	if avgHRDelta < -2 && math.Abs(avgPaceDelta) < 5 {
		verdict = "improving — lower HR at similar pace"
	} else if avgPaceDelta < -3 && math.Abs(avgHRDelta) < 3 {
		verdict = "improving — faster pace at similar HR"
	} else if avgHRDelta > 2 && avgPaceDelta > 3 {
		verdict = "declining — higher HR and slower pace"
	}

	result.Summary = &ComparisonSummary{
		AvgHRDelta:   avgHRDelta,
		AvgPaceDelta: avgPaceDelta,
		Verdict:      verdict,
	}

	return result
}

// FindSimilarWorkouts finds workouts with matching structure (same sport, ±1 lap count, similar durations).
func FindSimilarWorkouts(db *sql.DB, workoutID, userID int64) ([]Workout, error) {
	w, err := getWorkoutWithLaps(db, workoutID, userID)
	if err != nil {
		return nil, err
	}

	lapCount := len(w.Laps)
	minLaps := lapCount - 1
	if minLaps < 1 {
		minLaps = 1
	}
	maxLaps := lapCount + 1

	// Find workouts with same sport and lap count within ±1.
	rows, err := db.Query(`
		SELECT w.id
		FROM workouts w
		WHERE w.user_id = ? AND w.sport = ? AND w.id != ?
		AND (SELECT COUNT(*) FROM workout_laps l WHERE l.workout_id = w.id) BETWEEN ? AND ?
		ORDER BY w.started_at DESC
		LIMIT 20`, userID, w.Sport, workoutID, minLaps, maxLaps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidateIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		candidateIDs = append(candidateIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var similar []Workout
	for _, id := range candidateIDs {
		candidate, err := getWorkoutWithLaps(db, id, userID)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		// Check lap duration similarity on overlapping laps (within 30% tolerance).
		if areLapsSimilar(w.Laps, candidate.Laps, 0.3) {
			similar = append(similar, *candidate)
		}
	}
	return similar, nil
}

func areLapsSimilar(a, b []Lap, tolerance float64) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// Guard: only allow ±1 lap difference at most.
	diff := len(a) - len(b)
	if diff < -1 || diff > 1 {
		return false
	}
	// Compare overlapping laps.
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i].DurationSeconds <= 0 || b[i].DurationSeconds <= 0 {
			continue
		}
		ratio := a[i].DurationSeconds / b[i].DurationSeconds
		if ratio < (1-tolerance) || ratio > (1+tolerance) {
			return false
		}
	}
	return true
}
