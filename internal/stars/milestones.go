package stars

import (
	"context"
	"database/sql"
	"fmt"
)

// distanceMilestone represents a single-workout distance threshold award.
type distanceMilestone struct {
	minMeters   float64
	stars       int
	reason      string
	description string
}

var singleWorkoutMilestones = []distanceMilestone{
	{21100, 50, "half_marathon_legend", "Half marathon distance!"},
	{10000, 20, "10k_hero", "10K workout!"},
	{5000, 10, "5k_finisher", "5K workout!"},
	{1000, 5, "first_kilometer", "First kilometer!"},
}

// checkDistanceMilestones evaluates distance milestone awards for a workout.
// Each single-workout milestone is only awarded once per lifetime.
// Cumulative milestones fire when the cumulative total crosses a threshold.
func checkDistanceMilestones(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) ([]StarAward, error) {
	var awards []StarAward

	// Single-workout milestones (award each once per lifetime).
	for _, m := range singleWorkoutMilestones {
		if w.DistanceMeters < m.minMeters {
			continue
		}
		already, err := hasReason(db, userID, m.reason)
		if err != nil {
			return nil, err
		}
		if already {
			continue
		}
		awards = append(awards, StarAward{
			Amount:      m.stars,
			Reason:      m.reason,
			Description: m.description,
		})
	}

	// Cumulative distance milestones.
	cumAwards, err := checkCumulativeMilestones(ctx, db, userID, w)
	if err != nil {
		return nil, err
	}
	awards = append(awards, cumAwards...)

	return awards, nil
}

type cumulativeMilestone struct {
	thresholdMeters float64
	stars           int
	reason          string
	description     string
}

var cumulativeMilestones = []cumulativeMilestone{
	{1_000_000, 150, "titan_1000k", "1000K lifetime distance!"},
	{500_000, 75, "explorer_500k", "500K lifetime distance!"},
	{100_000, 25, "century_club", "100K lifetime distance!"},
}

// checkCumulativeMilestones checks whether the current workout crosses any cumulative
// distance threshold for the first time.
func checkCumulativeMilestones(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) ([]StarAward, error) {
	var awards []StarAward

	// Total distance for this user across all workouts.
	var totalMeters float64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(distance_meters), 0)
		FROM workouts
		WHERE user_id = ?
	`, userID).Scan(&totalMeters)
	if err != nil {
		return nil, err
	}

	previousTotal := totalMeters - w.DistanceMeters

	for _, m := range cumulativeMilestones {
		if totalMeters < m.thresholdMeters {
			continue
		}
		// Only fire if the previous total was below the threshold (first crossing).
		if previousTotal >= m.thresholdMeters {
			continue
		}
		// Double-check idempotency via star_transactions.
		already, err := hasReason(db, userID, m.reason)
		if err != nil {
			return nil, err
		}
		if already {
			continue
		}
		awards = append(awards, StarAward{
			Amount:      m.stars,
			Reason:      m.reason,
			Description: m.description,
		})
	}

	return awards, nil
}

// checkPersonalRecords evaluates PR awards for a workout.
func checkPersonalRecords(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) ([]StarAward, error) {
	var awards []StarAward
	fastest5KAwarded := false

	// Longest Run: new personal best distance.
	if w.DistanceMeters > 0 {
		var prevBest float64
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(distance_meters), 0)
			FROM workouts
			WHERE user_id = ? AND id != ?
		`, userID, w.ID).Scan(&prevBest)
		if err != nil {
			return nil, fmt.Errorf("pr longest run: %w", err)
		}
		if w.DistanceMeters > prevBest {
			awards = append(awards, StarAward{
				Amount:      10,
				Reason:      "pr_longest_run",
				Description: fmt.Sprintf("New longest run: %.1f km!", w.DistanceMeters/1000),
			})
		}
	}

	// Highest Calorie Burn: new personal best calories.
	if w.Calories > 0 {
		var prevBest int
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(calories), 0)
			FROM workouts
			WHERE user_id = ? AND id != ?
		`, userID, w.ID).Scan(&prevBest)
		if err != nil {
			return nil, fmt.Errorf("pr calorie burn: %w", err)
		}
		if w.Calories > prevBest {
			awards = append(awards, StarAward{
				Amount:      8,
				Reason:      "pr_calorie_burn",
				Description: fmt.Sprintf("New calorie record: %d kcal!", w.Calories),
			})
		}
	}

	// Most Elevation: new single-workout ascent record.
	if w.AscentMeters > 0 {
		var prevBest float64
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(ascent_meters), 0)
			FROM workouts
			WHERE user_id = ? AND id != ?
		`, userID, w.ID).Scan(&prevBest)
		if err != nil {
			return nil, fmt.Errorf("pr elevation: %w", err)
		}
		if w.AscentMeters > prevBest {
			awards = append(awards, StarAward{
				Amount:      8,
				Reason:      "pr_elevation",
				Description: fmt.Sprintf("New elevation record: %.0f m!", w.AscentMeters),
			})
		}
	}

	// Fastest 5K: best pace for workouts >= 5km.
	if w.DistanceMeters >= 5000 && w.AvgPaceSecPerKm > 0 {
		var prevBestPace float64
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(avg_pace_sec_per_km), 0)
			FROM workouts
			WHERE user_id = ? AND id != ? AND distance_meters >= 5000 AND avg_pace_sec_per_km > 0
		`, userID, w.ID).Scan(&prevBestPace)
		if err != nil {
			return nil, fmt.Errorf("pr fastest 5k: %w", err)
		}
		// Lower pace = faster. If no previous 5K or new pace is faster.
		if prevBestPace == 0 || w.AvgPaceSecPerKm < prevBestPace {
			awards = append(awards, StarAward{
				Amount:      10,
				Reason:      "pr_fastest_5k",
				Description: "New fastest 5K pace!",
			})
			fastest5KAwarded = true
		}
	}

	// Fastest Pace: best pace for workouts > 2km.
	if w.DistanceMeters > 2000 && w.AvgPaceSecPerKm > 0 {
		var prevBestPace float64
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(avg_pace_sec_per_km), 0)
			FROM workouts
			WHERE user_id = ? AND id != ? AND distance_meters > 2000 AND avg_pace_sec_per_km > 0
		`, userID, w.ID).Scan(&prevBestPace)
		if err != nil {
			return nil, fmt.Errorf("pr fastest pace: %w", err)
		}
		if prevBestPace == 0 || w.AvgPaceSecPerKm < prevBestPace {
			// Only award if pr_fastest_5k was not already awarded in this call to avoid double-counting.
			if !fastest5KAwarded {
				awards = append(awards, StarAward{
					Amount:      10,
					Reason:      "pr_fastest_pace",
					Description: "New fastest pace record!",
				})
			}
		}
	}

	return awards, nil
}

// hasReason checks whether a star_transaction with the given reason already
// exists for the user (used to enforce once-per-lifetime milestone idempotency).
func hasReason(db *sql.DB, userID int64, reason string) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = ?
	`, userID, reason).Scan(&count)
	return count > 0, err
}
