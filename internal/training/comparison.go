package training

import (
	"database/sql"
	"math"
)

// CompareWorkouts compares two workouts by matching their laps.
func CompareWorkouts(db *sql.DB, idA, idB, userID int64) (*ComparisonResult, error) {
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

	// Check compatibility: same sport, similar number of laps.
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

	// Build lap-by-lap comparison.
	var totalHRDelta float64
	var totalPaceDelta float64
	for i := range wA.Laps {
		lapA := wA.Laps[i]
		lapB := wB.Laps[i]

		delta := LapDelta{
			LapNumber:    lapA.LapNumber,
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

	n := float64(len(wA.Laps))
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

	return result, nil
}

// FindSimilarWorkouts finds workouts with matching structure (same sport, same lap count, similar durations).
func FindSimilarWorkouts(db *sql.DB, workoutID, userID int64) ([]Workout, error) {
	w, err := getWorkoutWithLaps(db, workoutID, userID)
	if err != nil {
		return nil, err
	}

	// Find workouts with same sport and same number of laps.
	rows, err := db.Query(`
		SELECT w.id
		FROM workouts w
		WHERE w.user_id = ? AND w.sport = ? AND w.id != ?
		AND (SELECT COUNT(*) FROM workout_laps l WHERE l.workout_id = w.id) = ?
		ORDER BY w.started_at DESC
		LIMIT 20`, userID, w.Sport, workoutID, len(w.Laps))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var similar []Workout
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		candidate, err := getWorkoutWithLaps(db, id, userID)
		if err != nil {
			continue
		}
		// Check lap duration similarity (within 30% tolerance).
		if areLapsSimilar(w.Laps, candidate.Laps, 0.3) {
			similar = append(similar, *candidate)
		}
	}
	return similar, nil
}

func areLapsSimilar(a, b []Lap, tolerance float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
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
