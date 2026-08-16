package training

import (
	"database/sql"
	"time"
)

// SaveVO2maxEstimate inserts or replaces a VO2max estimate for a workout.
// If an estimate already exists for the same (user_id, workout_id) pair it is overwritten.
func SaveVO2maxEstimate(db *sql.DB, est *VO2maxEstimate) error {
	if est.EstimatedAt == "" {
		est.EstimatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	row := db.QueryRow(`
		INSERT INTO vo2max_estimates (user_id, workout_id, vo2max, method, estimated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, workout_id) DO UPDATE SET
			vo2max       = excluded.vo2max,
			method       = excluded.method,
			estimated_at = excluded.estimated_at
		RETURNING id`,
		est.UserID, est.WorkoutID, est.VO2max, est.Method, est.EstimatedAt,
	)
	return row.Scan(&est.ID)
}

// vo2maxValidEstimateSQL excludes velocity-based (daniels) estimates computed
// from indoor workouts: treadmill watch distance is a cadence artifact, so
// those rows are noise. Estimates stored before EstimateVO2max learned to skip
// them are filtered here rather than deleted, and the HR-ratio method stays
// valid indoors (it uses no velocity).
const vo2maxValidEstimateSQL = `
	SELECT v.id, v.user_id, v.workout_id, v.vo2max, v.method, v.estimated_at
	FROM vo2max_estimates v
	JOIN workouts w ON w.id = v.workout_id
	WHERE v.user_id = ?
	  AND NOT (v.method = 'daniels' AND COALESCE(w.is_indoor, 0) = 1)`

// GetVO2maxHistory returns up to n VO2max estimates for a user ordered by
// estimated_at ascending (oldest first). It fetches the most recent n rows
// so the result always reflects recent performance, not the oldest records.
func GetVO2maxHistory(db *sql.DB, userID int64, n int) ([]VO2maxEstimate, error) {
	rows, err := db.Query(`
		SELECT id, user_id, workout_id, vo2max, method, estimated_at
		FROM (`+vo2maxValidEstimateSQL+`
			ORDER BY v.estimated_at DESC
			LIMIT ?
		) ORDER BY estimated_at ASC`,
		userID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	estimates := make([]VO2maxEstimate, 0)
	for rows.Next() {
		var e VO2maxEstimate
		if err := rows.Scan(&e.ID, &e.UserID, &e.WorkoutID, &e.VO2max, &e.Method, &e.EstimatedAt); err != nil {
			return nil, err
		}
		estimates = append(estimates, e)
	}
	return estimates, rows.Err()
}

// GetLatestVO2max returns the most recent VO2max estimate for a user.
// Returns sql.ErrNoRows if no estimate has been stored yet.
func GetLatestVO2max(db *sql.DB, userID int64) (*VO2maxEstimate, error) {
	var e VO2maxEstimate
	err := db.QueryRow(`
		`+vo2maxValidEstimateSQL+`
		ORDER BY v.estimated_at DESC
		LIMIT 1`,
		userID,
	).Scan(&e.ID, &e.UserID, &e.WorkoutID, &e.VO2max, &e.Method, &e.EstimatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}
