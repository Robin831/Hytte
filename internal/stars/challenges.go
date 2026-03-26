// Package stars provides kid-facing APIs for the Stars/Rewards system.
// Parent-facing Challenge CRUD and participant management live in internal/family,
// reflecting the package boundary: family = parent operations, stars = child-facing rewards.
package stars

import (
	"database/sql"
	"errors"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ChallengeWithProgress wraps a Challenge with the authenticated child's current
// progress toward the challenge target. Returned by GET /api/stars/challenges.
type ChallengeWithProgress struct {
	ID            int64   `json:"id"`
	CreatorID     int64   `json:"creator_id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	ChallengeType string  `json:"challenge_type"`
	TargetValue   float64 `json:"target_value"`
	StarReward    int     `json:"star_reward"`
	StartDate     string  `json:"start_date"`
	EndDate       string  `json:"end_date"`
	IsActive      bool    `json:"is_active"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	CurrentValue  float64 `json:"current_value"`
	Completed     bool    `json:"completed"`
}

// GetActiveChallenges returns all active challenges in which childID is a
// participant, enriched with that child's current progress.
// Progress for all challenges is computed using at most two queries (one for
// workout aggregation, one for streak), rather than one query per challenge.
func GetActiveChallenges(db *sql.DB, childID int64) ([]ChallengeWithProgress, error) {
	rows, err := db.Query(`
		SELECT fc.id, fc.creator_id, fc.title, fc.description, fc.challenge_type,
		       fc.target_value, fc.star_reward, fc.start_date, fc.end_date,
		       fc.is_active, fc.created_at, fc.updated_at
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id
		WHERE cp.child_id = ? AND fc.is_active = 1
		ORDER BY fc.start_date DESC
	`, childID)
	if err != nil {
		return nil, err
	}

	// Collect all rows before closing, so batchChallengeProgress can issue its
	// own queries without holding an open rows cursor (avoids SQLite locking).
	var results []ChallengeWithProgress
	for rows.Next() {
		var c ChallengeWithProgress
		var encTitle, encDesc string
		var isActiveInt int

		if err := rows.Scan(
			&c.ID, &c.CreatorID, &encTitle, &encDesc,
			&c.ChallengeType, &c.TargetValue, &c.StarReward,
			&c.StartDate, &c.EndDate, &isActiveInt,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		c.Title = encryption.DecryptLenient(encTitle)
		c.Description = encryption.DecryptLenient(encDesc)
		c.IsActive = isActiveInt != 0
		results = append(results, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now that rows is closed, compute progress for all challenges in bulk.
	if err := batchChallengeProgress(db, childID, results); err != nil {
		return nil, err
	}

	return results, nil
}

// batchChallengeProgress computes progress for all challenges using at most two
// database queries: one aggregation JOIN across all workout-based challenges, and
// one streak lookup shared by all streak-type challenges. This avoids the N+1
// pattern of issuing one query per challenge.
//
// Units: distance in km, duration in minutes, workout_count as count, streak as
// current daily streak count. Custom challenges keep CurrentValue = 0.
func batchChallengeProgress(db *sql.DB, childID int64, challenges []ChallengeWithProgress) error {
	if len(challenges) == 0 {
		return nil
	}

	// Fetch current streak once; shared by all streak-type challenges.
	var currentStreak float64
	err := db.QueryRow(`
		SELECT COALESCE(current_count, 0)
		FROM streaks
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, childID).Scan(&currentStreak)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Build a lookup from challenge ID to slice index.
	idxByID := make(map[int64]int, len(challenges))
	for i, c := range challenges {
		idxByID[c.ID] = i
	}

	// Single query: aggregate workout metrics per challenge using a LEFT JOIN.
	// The per-row date-range filters mean each challenge gets its own slice of
	// workouts without a separate round-trip.
	// Empty start_date/end_date are treated as open-ended via the OR guards;
	// SQLite evaluates the OR short-circuit before date() can return NULL.
	progressRows, err := db.Query(`
		SELECT
			fc.id,
			fc.challenge_type,
			COALESCE(SUM(w.distance_meters) / 1000.0, 0),
			COALESCE(SUM(w.duration_seconds) / 60.0, 0),
			COUNT(w.id)
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id AND cp.child_id = ?
		LEFT JOIN workouts w ON w.user_id = ?
			AND (fc.start_date = '' OR w.started_at >= fc.start_date)
			AND (fc.end_date = '' OR w.started_at < date(fc.end_date, '+1 day'))
		WHERE fc.is_active = 1
		GROUP BY fc.id, fc.challenge_type
	`, childID, childID)
	if err != nil {
		return err
	}
	defer progressRows.Close()

	for progressRows.Next() {
		var id int64
		var challengeType string
		var distanceKm, durationMin float64
		var workoutCount int
		if err := progressRows.Scan(&id, &challengeType, &distanceKm, &durationMin, &workoutCount); err != nil {
			return err
		}
		idx, ok := idxByID[id]
		if !ok {
			continue
		}
		switch challengeType {
		case "distance":
			challenges[idx].CurrentValue = distanceKm
		case "duration":
			challenges[idx].CurrentValue = durationMin
		case "workout_count":
			challenges[idx].CurrentValue = float64(workoutCount)
		case "streak":
			challenges[idx].CurrentValue = currentStreak
		// "custom" and future types: CurrentValue stays 0.
		}
		c := &challenges[idx]
		c.Completed = c.TargetValue > 0 && c.CurrentValue >= c.TargetValue
	}
	return progressRows.Err()
}
