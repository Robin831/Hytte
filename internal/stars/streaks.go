package stars

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrShieldLimitReached is returned when the weekly streak shield limit is exceeded.
var ErrShieldLimitReached = errors.New("weekly shield limit reached")

// weeklyShieldLimit is the maximum number of streak shields a parent can use
// per calendar week for a given child.
const weeklyShieldLimit = 1

// UpdateStreak updates the daily and weekly workout streaks for a user when
// they complete a workout on workoutDate. Same-day / same-week calls are
// no-ops. Consecutive days / weeks increment the streak; gaps reset it to 1.
func UpdateStreak(ctx context.Context, db *sql.DB, userID int64, workoutDate time.Time) error {
	if err := updateStreakType(ctx, db, userID, "daily_workout", workoutDate); err != nil {
		return fmt.Errorf("daily streak: %w", err)
	}
	if err := updateStreakType(ctx, db, userID, "weekly_workout", workoutDate); err != nil {
		return fmt.Errorf("weekly streak: %w", err)
	}
	return nil
}

// updateStreakType updates a single streak row (daily or weekly) for the user.
func updateStreakType(ctx context.Context, db *sql.DB, userID int64, streakType string, workoutDate time.Time) error {
	var lastActivity string
	var current, longest int64
	err := db.QueryRowContext(ctx, `
		SELECT current_count, longest_count, last_activity
		FROM streaks
		WHERE user_id = ? AND streak_type = ?
	`, userID, streakType).Scan(&current, &longest, &lastActivity)

	dateStr := formatStreakDate(streakType, workoutDate)

	if err == sql.ErrNoRows {
		_, insErr := db.ExecContext(ctx, `
			INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
			VALUES (?, ?, 1, 1, ?)
		`, userID, streakType, dateStr)
		return insErr
	}
	if err != nil {
		return err
	}

	// Same period — no-op.
	if lastActivity == dateStr {
		return nil
	}

	// Determine whether this workout continues or resets the streak.
	lastDate, parseErr := parseStreakDate(streakType, lastActivity)

	var newCount int64
	if parseErr != nil {
		newCount = 1
	} else {
		switch streakType {
		case "daily_workout":
			yesterday := workoutDate.UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)
			if sameDay(lastDate, yesterday) {
				newCount = current + 1
			} else {
				newCount = 1
			}
		case "weekly_workout":
			lastYear, lastWeek := lastDate.ISOWeek()
			// Monday of the week after lastDate's week.
			lastMon := firstDayOfISOWeek(lastYear, lastWeek)
			nextMon := lastMon.AddDate(0, 0, 7)
			nextYear, nextWeekNum := nextMon.ISOWeek()
			curYear, curWeek := workoutDate.UTC().ISOWeek()
			if curYear == nextYear && curWeek == nextWeekNum {
				newCount = current + 1
			} else {
				newCount = 1
			}
		}
	}

	newLongest := max(longest, newCount)

	_, err = db.ExecContext(ctx, `
		UPDATE streaks
		SET current_count = ?, longest_count = ?, last_activity = ?
		WHERE user_id = ? AND streak_type = ?
	`, newCount, newLongest, dateStr, userID, streakType)
	return err
}

// GetStreaks returns the daily and weekly streak data for a user.
func GetStreaks(ctx context.Context, db *sql.DB, userID int64) (StreaksResponse, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT streak_type, current_count, longest_count, last_activity
		FROM streaks
		WHERE user_id = ? AND streak_type IN ('daily_workout', 'weekly_workout')
	`, userID)
	if err != nil {
		return StreaksResponse{}, err
	}
	defer rows.Close()

	resp := StreaksResponse{}
	for rows.Next() {
		var streakType string
		var info StreakInfo
		if err := rows.Scan(&streakType, &info.CurrentCount, &info.LongestCount, &info.LastActivity); err != nil {
			return StreaksResponse{}, err
		}
		switch streakType {
		case "daily_workout":
			resp.DailyWorkout = info
		case "weekly_workout":
			resp.WeeklyWorkout = info
		}
	}
	return resp, rows.Err()
}

// StreakAtRisk reports which streaks are at risk of being broken.
type StreakAtRisk struct {
	DailyAtRisk  bool `json:"daily_at_risk"`
	WeeklyAtRisk bool `json:"weekly_at_risk"`
}

// CheckStreakAtRisk returns whether the user's daily or weekly streak is at risk
// of breaking. A daily streak is at risk when the last workout was yesterday and
// no workout has been logged today yet. A weekly streak is at risk when the last
// workout was last week and no workout has been logged this week yet.
func CheckStreakAtRisk(ctx context.Context, db *sql.DB, userID int64) (StreakAtRisk, error) {
	now := time.Now().UTC()
	streaks, err := GetStreaks(ctx, db, userID)
	if err != nil {
		return StreakAtRisk{}, err
	}
	result := StreakAtRisk{}

	// Daily: at risk if last_activity was yesterday.
	if streaks.DailyWorkout.CurrentCount > 0 && streaks.DailyWorkout.LastActivity != "" {
		last, parseErr := parseStreakDate("daily_workout", streaks.DailyWorkout.LastActivity)
		if parseErr == nil {
			yesterday := now.Truncate(24 * time.Hour).AddDate(0, 0, -1)
			result.DailyAtRisk = sameDay(last, yesterday)
		}
	}

	// Weekly: at risk if last_activity was in the previous ISO week.
	if streaks.WeeklyWorkout.CurrentCount > 0 && streaks.WeeklyWorkout.LastActivity != "" {
		last, parseErr := parseStreakDate("weekly_workout", streaks.WeeklyWorkout.LastActivity)
		if parseErr == nil {
			lastYear, lastWeek := last.ISOWeek()
			// Monday of the week after last's week.
			lastMon := firstDayOfISOWeek(lastYear, lastWeek)
			nextMon := lastMon.AddDate(0, 0, 7)
			nextYear, nextWeekNum := nextMon.ISOWeek()
			curYear, curWeek := now.ISOWeek()
			result.WeeklyAtRisk = (curYear == nextYear && curWeek == nextWeekNum)
		}
	}

	return result, nil
}

// UseStreakShield uses a parent's streak shield to protect a child's daily
// streak. It records the shield in streak_shields, then advances the child's
// daily streak last_activity to today so the streak continues.
// Returns ErrShieldLimitReached if the weekly limit has been reached.
func UseStreakShield(ctx context.Context, db *sql.DB, parentID, childID int64) error {
	now := time.Now().UTC()

	// Compute the start of the current calendar week (Monday).
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	weekStart := now.Truncate(24 * time.Hour).AddDate(0, 0, -daysSinceMonday)

	var weeklyCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM streak_shields
		WHERE parent_id = ? AND child_id = ? AND used_at >= ?
	`, parentID, childID, weekStart.Format(time.RFC3339)).Scan(&weeklyCount); err != nil {
		return fmt.Errorf("check shield usage: %w", err)
	}
	if weeklyCount >= weeklyShieldLimit {
		return ErrShieldLimitReached
	}

	todayStr := formatStreakDate("daily_workout", now)

	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO streak_shields (parent_id, child_id, used_at, shield_date)
		VALUES (?, ?, ?, ?)
	`, parentID, childID, now.Format(time.RFC3339), todayStr); err != nil {
		return fmt.Errorf("record shield: %w", err)
	}

	// Advance the child's daily streak last_activity to today so it doesn't break.
	if _, err := tx.ExecContext(ctx, `
		UPDATE streaks SET last_activity = ?
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, todayStr, childID); err != nil {
		return fmt.Errorf("advance streak: %w", err)
	}

	return tx.Commit()
}

// checkConsistencyStars returns star awards for streak milestones that the user
// has not yet been awarded. Each milestone fires at most once per lifetime.
func checkConsistencyStars(ctx context.Context, db *sql.DB, userID int64) ([]StarAward, error) {
	var current int64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0) FROM streaks
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, userID).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	milestones := []struct {
		threshold int64
		stars     int
		reason    string
		desc      string
	}{
		{3, 10, "streak_3day", "3-day workout streak!"},
		{7, 25, "streak_7day", "7-day workout streak!"},
		{14, 50, "streak_14day", "14-day workout streak!"},
		{30, 100, "streak_30day", "30-day workout streak!"},
	}

	var awards []StarAward
	for _, m := range milestones {
		if current < m.threshold {
			continue
		}
		already, alreadyErr := hasReason(db, userID, m.reason)
		if alreadyErr != nil {
			return nil, alreadyErr
		}
		if already {
			continue
		}
		awards = append(awards, StarAward{
			Amount:      m.stars,
			Reason:      m.reason,
			Description: m.desc,
		})
	}
	return awards, nil
}

// checkTimeOfDayStars returns star awards for early-morning or late-night workouts.
// Awards are per-workout; the reason includes the workout ID so recordAwards
// remains idempotent if EvaluateWorkout is called more than once for the same workout.
func checkTimeOfDayStars(workoutID int64, workoutDate time.Time) []StarAward {
	var awards []StarAward
	hour := workoutDate.UTC().Hour()
	if hour < 6 {
		awards = append(awards, StarAward{
			Amount:      3,
			Reason:      fmt.Sprintf("early_bird_%d", workoutID),
			Description: "Early bird — workout before 6 AM!",
		})
	}
	if hour >= 22 {
		awards = append(awards, StarAward{
			Amount:      3,
			Reason:      fmt.Sprintf("night_owl_%d", workoutID),
			Description: "Night owl — workout after 10 PM!",
		})
	}
	return awards
}

// checkWeekendWarrior returns a star award when the user has completed workouts
// on both Saturday and Sunday of the current ISO week. Fires once per week.
func checkWeekendWarrior(ctx context.Context, db *sql.DB, userID int64, workoutDate time.Time) ([]StarAward, error) {
	weekday := workoutDate.UTC().Weekday()
	if weekday != time.Saturday && weekday != time.Sunday {
		return nil, nil
	}

	year, week := workoutDate.UTC().ISOWeek()
	reason := fmt.Sprintf("weekend_warrior_%d_%02d", year, week)
	already, err := hasReason(db, userID, reason)
	if err != nil {
		return nil, err
	}
	if already {
		return nil, nil
	}

	mon := firstDayOfISOWeek(year, week)
	satStr := mon.AddDate(0, 0, 5).Format("2006-01-02")
	sunStr := mon.AddDate(0, 0, 6).Format("2006-01-02")

	var satCount, sunCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workouts
		WHERE user_id = ? AND date(started_at) = ?
	`, userID, satStr).Scan(&satCount); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workouts
		WHERE user_id = ? AND date(started_at) = ?
	`, userID, sunStr).Scan(&sunCount); err != nil {
		return nil, err
	}

	if satCount > 0 && sunCount > 0 {
		return []StarAward{{
			Amount:      15,
			Reason:      reason,
			Description: "Weekend warrior — worked out on both Saturday and Sunday!",
		}}, nil
	}
	return nil, nil
}

// formatStreakDate formats workoutDate as the canonical streak period string.
// Daily streaks use "2006-01-02"; weekly streaks use "2006-W01" (ISO week).
func formatStreakDate(streakType string, t time.Time) string {
	if streakType == "weekly_workout" {
		year, week := t.UTC().ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	}
	return t.UTC().Format("2006-01-02")
}

// parseStreakDate parses a canonical streak period string back into a time.Time.
// For weekly streaks it returns the Monday of that ISO week.
func parseStreakDate(streakType, s string) (time.Time, error) {
	if streakType == "weekly_workout" {
		var year, week int
		if _, err := fmt.Sscanf(s, "%d-W%d", &year, &week); err != nil {
			return time.Time{}, fmt.Errorf("parse weekly streak date %q: %w", s, err)
		}
		return firstDayOfISOWeek(year, week), nil
	}
	return time.Parse("2006-01-02", s)
}

// sameDay reports whether two times fall on the same UTC calendar day.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// firstDayOfISOWeek returns the Monday (UTC midnight) of the given ISO year/week.
func firstDayOfISOWeek(year, week int) time.Time {
	// January 4th is always in ISO week 1.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	jan4DOW := int(jan4.Weekday())
	if jan4DOW == 0 {
		jan4DOW = 7 // Sunday → 7 in ISO
	}
	week1Monday := jan4.AddDate(0, 0, 1-jan4DOW)
	return week1Monday.AddDate(0, 0, (week-1)*7)
}
