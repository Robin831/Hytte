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
	defer rows.Close()

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
			return nil, err
		}
		c.Title = decryptChallengeField(encTitle)
		c.Description = decryptChallengeField(encDesc)
		c.IsActive = isActiveInt != 0

		progress, err := calcChallengeProgress(db, childID, &c)
		if err != nil {
			return nil, err
		}
		c.CurrentValue = progress
		c.Completed = c.TargetValue > 0 && progress >= c.TargetValue

		results = append(results, c)
	}
	return results, rows.Err()
}

// calcChallengeProgress computes the child's current progress for a challenge.
// Units: distance in km, duration in minutes, workout_count as count, streak as
// current daily streak count. Custom challenges return 0 (no auto-calculation).
func calcChallengeProgress(db *sql.DB, childID int64, c *ChallengeWithProgress) (float64, error) {
	switch c.ChallengeType {
	case "distance":
		var total sql.NullFloat64
		err := db.QueryRow(`
			SELECT SUM(distance_meters) / 1000.0
			FROM workouts
			WHERE user_id = ? AND date(started_at) >= ? AND date(started_at) <= ?
		`, childID, c.StartDate, c.EndDate).Scan(&total)
		if err != nil {
			return 0, err
		}
		return total.Float64, nil

	case "duration":
		var total sql.NullFloat64
		err := db.QueryRow(`
			SELECT SUM(duration_seconds) / 60.0
			FROM workouts
			WHERE user_id = ? AND date(started_at) >= ? AND date(started_at) <= ?
		`, childID, c.StartDate, c.EndDate).Scan(&total)
		if err != nil {
			return 0, err
		}
		return total.Float64, nil

	case "workout_count":
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM workouts
			WHERE user_id = ? AND date(started_at) >= ? AND date(started_at) <= ?
		`, childID, c.StartDate, c.EndDate).Scan(&count)
		if err != nil {
			return 0, err
		}
		return float64(count), nil

	case "streak":
		var current int
		err := db.QueryRow(`
			SELECT COALESCE(current_count, 0)
			FROM streaks
			WHERE user_id = ? AND streak_type = 'daily_workout'
		`, childID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		return float64(current), nil

	default:
		// 'custom' and future types: no automatic progress calculation.
		return 0, nil
	}
}

// decryptChallengeField decrypts an encrypted challenge field. Returns the
// plaintext as-is for legacy values. Returns empty string if decryption of an
// enc:-prefixed value fails, to avoid leaking ciphertext.
func decryptChallengeField(val string) string {
	if val == "" {
		return val
	}
	decrypted, err := encryption.DecryptField(val)
	if err != nil {
		if len(val) >= 4 && val[:4] == "enc:" {
			return ""
		}
		return val
	}
	return decrypted
}
