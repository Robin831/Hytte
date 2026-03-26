package stars

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

const (
	defaultWeeklyDistanceTargetKm  = 10.0
	defaultWeeklyDurationTargetMin = 150
	// streakMultiplierThreshold is the minimum weekly streak count to trigger the 1.5x multiplier.
	streakMultiplierThreshold = 2
)

// ChildWeeklySettings holds parent-configurable weekly bonus targets for a child.
type ChildWeeklySettings struct {
	WeeklyDistanceTargetKm  float64 `json:"weekly_distance_target_km"`
	WeeklyDurationTargetMin int     `json:"weekly_duration_target_min"`
}

// GetChildWeeklySettings reads a child's weekly target preferences.
// Falls back to defaults if preferences are not set.
func GetChildWeeklySettings(db *sql.DB, childID int64) (ChildWeeklySettings, error) {
	prefs, err := auth.GetPreferences(db, childID)
	if err != nil {
		return ChildWeeklySettings{}, err
	}

	s := ChildWeeklySettings{
		WeeklyDistanceTargetKm:  defaultWeeklyDistanceTargetKm,
		WeeklyDurationTargetMin: defaultWeeklyDurationTargetMin,
	}

	if v, ok := prefs["kids_stars_weekly_distance_target_km"]; ok {
		if f, parseErr := strconv.ParseFloat(v, 64); parseErr == nil && f > 0 {
			s.WeeklyDistanceTargetKm = f
		}
	}
	if v, ok := prefs["kids_stars_weekly_duration_target_min"]; ok {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			s.WeeklyDurationTargetMin = n
		}
	}

	return s, nil
}

// SetChildWeeklySetting stores a single weekly target preference for a child.
// Callers should pass one of the recognised kids_stars_weekly_* preference keys.
func SetChildWeeklySetting(db *sql.DB, childID int64, key, value string) error {
	return auth.SetPreference(db, childID, key, value)
}

// EvaluateWeeklyBonuses evaluates and records weekly bonus stars for a child.
// anyDateInWeek must be a time within the ISO week being evaluated (typically
// the Sunday that ends that week, or any day within that completed week).
// The function is idempotent: repeated calls for the same user and week are no-ops.
func EvaluateWeeklyBonuses(ctx context.Context, db *sql.DB, userID int64, anyDateInWeek time.Time) ([]StarAward, error) {
	key := weekKey(anyDateInWeek)

	// Idempotency guard: try to claim this (user, week) in a transaction.
	// If another concurrent call already evaluated this week, the insert will
	// be a no-op due to the UNIQUE constraint and we return early.
	guardTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("weekly bonus idempotency tx begin: %w", err)
	}

	res, err := guardTx.ExecContext(ctx, `
		INSERT INTO weekly_bonus_evaluations (user_id, week_key)
		VALUES (?, ?)
		ON CONFLICT(user_id, week_key) DO NOTHING
	`, userID, key)
	if err != nil {
		_ = guardTx.Rollback()
		return nil, fmt.Errorf("weekly bonus idempotency insert: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		_ = guardTx.Rollback()
		return nil, fmt.Errorf("weekly bonus idempotency rows affected: %w", err)
	}
	if affected == 0 {
		// Another concurrent evaluation already claimed this week.
		if err := guardTx.Rollback(); err != nil {
			log.Printf("weekly bonus idempotency rollback: %v", err)
		}
		return nil, nil
	}

	if err := guardTx.Commit(); err != nil {
		return nil, fmt.Errorf("weekly bonus idempotency tx commit: %w", err)
	}

	// Resolve the Monday of this ISO week.
	year, week := anyDateInWeek.UTC().ISOWeek()
	mon := firstDayOfISOWeek(year, week)
	weekStart := mon.Format(time.RFC3339)
	weekEnd := mon.AddDate(0, 0, 7).Format(time.RFC3339)

	// Count distinct active days and aggregate distance + duration for this week.
	rows, err := db.QueryContext(ctx, `
		SELECT date(started_at), COALESCE(SUM(distance_meters), 0), COALESCE(SUM(duration_seconds), 0)
		FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at < ?
		GROUP BY date(started_at)
	`, userID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("weekly bonus workout query: %w", err)
	}
	defer rows.Close()

	activeDays := 0
	totalDistanceM := 0.0
	totalDurationSec := 0
	for rows.Next() {
		var day string
		var dist float64
		var dur int
		if err := rows.Scan(&day, &dist, &dur); err != nil {
			return nil, fmt.Errorf("weekly bonus workout scan: %w", err)
		}
		activeDays++
		totalDistanceM += dist
		totalDurationSec += dur
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("weekly bonus workout rows: %w", err)
	}

	// Load targets from child preferences.
	settings, settingsErr := GetChildWeeklySettings(db, userID)
	if settingsErr != nil {
		log.Printf("stars: weekly bonus load settings user %d: %v — using defaults", userID, settingsErr)
		settings = ChildWeeklySettings{
			WeeklyDistanceTargetKm:  defaultWeeklyDistanceTargetKm,
			WeeklyDurationTargetMin: defaultWeeklyDurationTargetMin,
		}
	}

	// Load previous week's total distance for improvement comparison.
	prevMon := mon.AddDate(0, 0, -7)
	prevWeekStart := prevMon.Format(time.RFC3339)
	prevWeekEnd := mon.Format(time.RFC3339)
	var prevDistanceM float64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(distance_meters), 0)
		FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at < ?
	`, userID, prevWeekStart, prevWeekEnd).Scan(&prevDistanceM); err != nil {
		return nil, fmt.Errorf("stars: weekly bonus prev week query user %d: %w", userID, err)
	}

	// Load current weekly streak count.
	var weeklyStreakCount int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0) FROM streaks
		WHERE user_id = ? AND streak_type = 'weekly_workout'
	`, userID).Scan(&weeklyStreakCount); err != nil && err != sql.ErrNoRows {
		log.Printf("stars: weekly bonus streak query user %d: %v", userID, err)
	}

	// Evaluate each bonus. bonusCount tracks distinct non-multiplier bonuses
	// achieved; used to detect Perfect Week.
	bonusCount := 0
	var awards []StarAward

	// Active Every Day: worked out on 5 or more days this week.
	if activeDays >= 5 {
		bonusCount++
		awards = append(awards, StarAward{
			Amount:      10,
			Reason:      fmt.Sprintf("active_every_day_%s", key),
			Description: fmt.Sprintf("Active on %d days this week!", activeDays),
		})
	}

	// Week Complete: worked out on all 7 days.
	if activeDays == 7 {
		bonusCount++
		awards = append(awards, StarAward{
			Amount:      20,
			Reason:      fmt.Sprintf("week_complete_%s", key),
			Description: "Worked out every day this week!",
		})
	}

	// Distance Goal: met the weekly distance target.
	totalDistanceKm := totalDistanceM / 1000.0
	if totalDistanceKm >= settings.WeeklyDistanceTargetKm {
		bonusCount++
		awards = append(awards, StarAward{
			Amount:      20,
			Reason:      fmt.Sprintf("distance_goal_%s", key),
			Description: fmt.Sprintf("%.1f km distance goal achieved!", settings.WeeklyDistanceTargetKm),
		})
	}

	// Duration Goal: met the weekly duration target.
	totalDurationMin := totalDurationSec / 60
	if totalDurationMin >= settings.WeeklyDurationTargetMin {
		bonusCount++
		awards = append(awards, StarAward{
			Amount:      20,
			Reason:      fmt.Sprintf("duration_goal_%s", key),
			Description: fmt.Sprintf("%d min duration goal achieved!", settings.WeeklyDurationTargetMin),
		})
	}

	// Improvement Bonus: this week's total distance exceeds last week's.
	if totalDistanceM > prevDistanceM && totalDistanceM > 0 {
		bonusCount++
		awards = append(awards, StarAward{
			Amount:      10,
			Reason:      fmt.Sprintf("improvement_bonus_%s", key),
			Description: "Beat last week's total distance!",
		})
	}

	// Perfect Week: all 5 base bonuses achieved.
	const perfectWeekThreshold = 5
	if bonusCount >= perfectWeekThreshold {
		awards = append(awards, StarAward{
			Amount:      25,
			Reason:      fmt.Sprintf("perfect_week_%s", key),
			Description: "Perfect week — all goals achieved!",
		})
	}

	// Streak Multiplier x1.5: applies when the weekly workout streak meets the
	// threshold. The bonus is the ceiling of 50% of the current non-multiplier total.
	if weeklyStreakCount >= streakMultiplierThreshold && len(awards) > 0 {
		baseTotal := 0
		for _, a := range awards {
			baseTotal += a.Amount
		}
		bonus := int(float64(baseTotal)*0.5+0.5) // round half-up
		if bonus > 0 {
			awards = append(awards, StarAward{
				Amount:      bonus,
				Reason:      fmt.Sprintf("streak_multiplier_%s", key),
				Description: fmt.Sprintf("1.5x streak multiplier (week streak: %d)!", weeklyStreakCount),
			})
		}
	}

	// The slot was already claimed by the idempotency guard at the start.
	// Nothing extra to record when no awards were earned.
	if len(awards) == 0 {
		return nil, nil
	}

	// Record awards atomically.
	now := time.Now().UTC().Format(time.RFC3339)
	awardTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer awardTx.Rollback()

	totalAmount := 0
	for _, a := range awards {
		if _, insErr := awardTx.ExecContext(ctx, `
			INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, userID, a.Amount, a.Reason, a.Description, now); insErr != nil {
			return nil, fmt.Errorf("record weekly bonus transaction: %w", insErr)
		}
		totalAmount += a.Amount
	}

	if _, err := awardTx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, totalAmount); err != nil {
		return nil, fmt.Errorf("update star balance for weekly bonuses: %w", err)
	}

	if err := awardTx.Commit(); err != nil {
		return nil, err
	}

	return awards, nil
}

// weekKey returns the ISO week key string for a given time, e.g. "2024-W01".
func weekKey(t time.Time) string {
	year, week := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}
