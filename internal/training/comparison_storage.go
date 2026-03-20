package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
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

	responseJSON, err = encryption.DecryptField(responseJSON)
	if err != nil {
		return nil, fmt.Errorf("decrypt comparison response: %w", err)
	}

	var analysis ComparisonAnalysis
	if err := json.Unmarshal([]byte(responseJSON), &analysis); err != nil {
		return nil, fmt.Errorf("unmarshal cached comparison analysis: %w", err)
	}
	analysis.normalize()

	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		createdAt = normalizeTimestamp(createdAt)
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
	encPrompt, err := encryption.EncryptField(prompt)
	if err != nil {
		return fmt.Errorf("encrypt comparison prompt: %w", err)
	}
	encResponse, err := encryption.EncryptField(string(data))
	if err != nil {
		return fmt.Errorf("encrypt comparison response: %w", err)
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO comparison_analyses (user_id, workout_id_a, workout_id_b, model, prompt, response_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, idA, idB, model, encPrompt, encResponse, createdAt,
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

// ComparisonAnalysisSummary is a lightweight representation for list endpoints.
type ComparisonAnalysisSummary struct {
	ID         int64  `json:"id"`
	WorkoutIDA int64  `json:"workout_id_a"`
	WorkoutIDB int64  `json:"workout_id_b"`
	Model      string `json:"model"`
	CreatedAt  string `json:"created_at"`
	Summary    string `json:"summary"`
}

// ListComparisonAnalyses returns all cached comparison analyses for a user,
// ordered by most recent first.
func ListComparisonAnalyses(db *sql.DB, userID int64) ([]ComparisonAnalysisSummary, error) {
	rows, err := db.Query(
		`SELECT id, workout_id_a, workout_id_b, model, created_at, response_json FROM comparison_analyses WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var analyses []ComparisonAnalysisSummary
	for rows.Next() {
		var s ComparisonAnalysisSummary
		var responseJSON string
		if err := rows.Scan(&s.ID, &s.WorkoutIDA, &s.WorkoutIDB, &s.Model, &s.CreatedAt, &responseJSON); err != nil {
			return nil, err
		}
		responseJSON, err = encryption.DecryptField(responseJSON)
		if err != nil {
			return nil, fmt.Errorf("decrypt comparison response (id=%d): %w", s.ID, err)
		}
		// Extract just the summary from the full response JSON.
		var parsed ComparisonAnalysis
		if err := json.Unmarshal([]byte(responseJSON), &parsed); err != nil {
			return nil, fmt.Errorf("unmarshal comparison analysis summary (id=%d): %w", s.ID, err)
		}
		s.Summary = parsed.Summary
		s.CreatedAt = normalizeTimestamp(s.CreatedAt)
		analyses = append(analyses, s)
	}
	if analyses == nil {
		analyses = []ComparisonAnalysisSummary{}
	}
	return analyses, rows.Err()
}

// GetComparisonAnalysisByID retrieves a single cached comparison analysis by its primary key,
// scoped to the given user.
func GetComparisonAnalysisByID(db *sql.DB, id, userID int64) (*CachedComparisonAnalysis, error) {
	var responseJSON, model, createdAt string
	var workoutIDA, workoutIDB int64
	err := db.QueryRow(
		`SELECT workout_id_a, workout_id_b, model, created_at, response_json FROM comparison_analyses WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&workoutIDA, &workoutIDB, &model, &createdAt, &responseJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	responseJSON, err = encryption.DecryptField(responseJSON)
	if err != nil {
		return nil, fmt.Errorf("decrypt comparison response: %w", err)
	}

	var analysis ComparisonAnalysis
	if err := json.Unmarshal([]byte(responseJSON), &analysis); err != nil {
		return nil, fmt.Errorf("unmarshal cached comparison analysis: %w", err)
	}
	analysis.normalize()

	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		createdAt = normalizeTimestamp(createdAt)
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

// DeleteComparisonAnalysisByID removes a cached comparison analysis by its primary key,
// scoped to the given user.
func DeleteComparisonAnalysisByID(db *sql.DB, id, userID int64) error {
	res, err := db.Exec(
		`DELETE FROM comparison_analyses WHERE id = ? AND user_id = ?`,
		id, userID,
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

// normalizeTimestamp parses a timestamp string and returns it in RFC3339 format.
// If parsing fails, the original string is returned unchanged.
func normalizeTimestamp(ts string) string {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ts
}

// normalizeWorkoutIDs ensures the smaller ID is always first, so (A,B) and (B,A)
// map to the same cache key.
func normalizeWorkoutIDs(a, b int64) (int64, int64) {
	if a > b {
		return b, a
	}
	return a, b
}
