package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// GetCachedComparisonAnalysis retrieves a cached comparison analysis for two workouts
// owned by userID, or returns nil if none exists. Workout IDs are normalized so
// (A,B) and (B,A) hit the same cache entry.
func GetCachedComparisonAnalysis(db *sql.DB, workoutIDA, workoutIDB, userID int64) (*CachedComparisonAnalysis, error) {
	idA, idB := normalizeWorkoutIDs(workoutIDA, workoutIDB)

	var responseJSON, model, createdAt string
	err := db.QueryRow(
		`SELECT response_json, model, created_at FROM comparison_analyses WHERE user_id = ? AND workout_id_a = ? AND workout_id_b = ?`,
		userID, idA, idB,
	).Scan(&responseJSON, &model, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var analysis ComparisonAnalysis
	if err := json.Unmarshal([]byte(responseJSON), &analysis); err != nil {
		return nil, fmt.Errorf("unmarshal cached comparison analysis: %w", err)
	}
	analysis.normalize()

	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &CachedComparisonAnalysis{
		ComparisonAnalysis: analysis,
		WorkoutIDA:         workoutIDA,
		WorkoutIDB:         workoutIDB,
		Model:              model,
		CreatedAt:          createdAt,
		Cached:             true,
	}, nil
}

// SaveComparisonAnalysis caches a comparison analysis for two workouts owned by userID.
func SaveComparisonAnalysis(db *sql.DB, workoutIDA, workoutIDB, userID int64, analysis *ComparisonAnalysis, model, prompt, createdAt string) error {
	idA, idB := normalizeWorkoutIDs(workoutIDA, workoutIDB)

	data, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO comparison_analyses (user_id, workout_id_a, workout_id_b, model, prompt, response_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, idA, idB, model, prompt, string(data), createdAt,
	)
	return err
}

// DeleteComparisonAnalysis removes a cached comparison analysis for two workouts.
func DeleteComparisonAnalysis(db *sql.DB, workoutIDA, workoutIDB, userID int64) error {
	idA, idB := normalizeWorkoutIDs(workoutIDA, workoutIDB)

	res, err := db.Exec(
		`DELETE FROM comparison_analyses WHERE user_id = ? AND workout_id_a = ? AND workout_id_b = ?`,
		userID, idA, idB,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteComparisonAnalysesForWorkout removes all cached comparison analyses that
// reference the given workout (as either A or B).
func DeleteComparisonAnalysesForWorkout(db *sql.DB, workoutID, userID int64) error {
	_, err := db.Exec(
		`DELETE FROM comparison_analyses WHERE user_id = ? AND (workout_id_a = ? OR workout_id_b = ?)`,
		userID, workoutID, workoutID,
	)
	return err
}

// normalizeWorkoutIDs ensures the smaller ID is always first, so (A,B) and (B,A)
// map to the same cache key.
func normalizeWorkoutIDs(a, b int64) (int64, int64) {
	if a > b {
		return b, a
	}
	return a, b
}
