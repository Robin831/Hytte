package stars

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// weeklyTestSchema adds the weekly_bonus_evaluations table required by
// EvaluateWeeklyBonuses to an in-memory test database.
const weeklyTestSchema = `
CREATE TABLE IF NOT EXISTS weekly_bonus_evaluations (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	week_key     TEXT NOT NULL,
	evaluated_at TEXT NOT NULL DEFAULT '',
	UNIQUE(user_id, week_key)
);`

// setupWeeklyDB returns a test DB that includes all base stars tables plus the
// weekly_bonus_evaluations table.
func setupWeeklyDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	if _, err := db.Exec(weeklyTestSchema); err != nil {
		t.Fatalf("create weekly schema: %v", err)
	}
	return db
}

// hasAwardReason reports whether any award in the slice has the given reason string.
func hasAwardReason(awards []StarAward, reason string) bool {
	for _, a := range awards {
		if a.Reason == reason {
			return true
		}
	}
	return false
}

// awardReasons returns the reason strings of all awards (for test failure messages).
func awardReasons(awards []StarAward) []string {
	out := make([]string, len(awards))
	for i, a := range awards {
		out[i] = a.Reason
	}
	return out
}

// monday2025W12 is the Monday of ISO week 2025-W12 (2025-03-17), used as a
// stable anchor date in tests so results do not drift with wall-clock time.
var monday2025W12 = time.Date(2025, 3, 17, 9, 0, 0, 0, time.UTC)

// ts formats t as RFC3339 UTC, matching the string signature of insertWorkoutAt.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// TestWeeklyBonus_ActiveEveryDay verifies the active_every_day bonus fires when
// a child works out on exactly 5 days (below the 7-day week_complete threshold).
func TestWeeklyBonus_ActiveEveryDay(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w1.com")
	childID := insertUser(t, db, "child@w1.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	for i := 0; i < 5; i++ {
		insertWorkoutAt(t, db, childID, 3600, 1000, ts(mon.AddDate(0, 0, i)))
	}

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	key := weekKey(mon)
	if !hasAwardReason(awards, "active_every_day_"+key) {
		t.Errorf("expected active_every_day bonus; got %v", awardReasons(awards))
	}
	if hasAwardReason(awards, "week_complete_"+key) {
		t.Error("did not expect week_complete bonus for 5 days")
	}
}

// TestWeeklyBonus_ActiveEveryDay_BelowThreshold verifies the bonus is NOT awarded
// when fewer than 5 days are active.
func TestWeeklyBonus_ActiveEveryDay_BelowThreshold(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w1b.com")
	childID := insertUser(t, db, "child@w1b.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	for i := 0; i < 4; i++ {
		insertWorkoutAt(t, db, childID, 3600, 1000, ts(mon.AddDate(0, 0, i)))
	}

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if hasAwardReason(awards, "active_every_day_"+weekKey(mon)) {
		t.Error("did not expect active_every_day for 4 active days")
	}
}

// TestWeeklyBonus_WeekComplete verifies both active_every_day and week_complete are
// awarded when a child works out all 7 days.
func TestWeeklyBonus_WeekComplete(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w2.com")
	childID := insertUser(t, db, "child@w2.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	for i := 0; i < 7; i++ {
		insertWorkoutAt(t, db, childID, 3600, 1000, ts(mon.AddDate(0, 0, i)))
	}

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	key := weekKey(mon)
	if !hasAwardReason(awards, "active_every_day_"+key) {
		t.Errorf("expected active_every_day; got %v", awardReasons(awards))
	}
	if !hasAwardReason(awards, "week_complete_"+key) {
		t.Errorf("expected week_complete; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_DistanceGoal_Default verifies the distance_goal bonus fires with
// the default 10 km weekly threshold.
func TestWeeklyBonus_DistanceGoal_Default(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w3.com")
	childID := insertUser(t, db, "child@w3.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 3600, 11000, ts(mon)) // 11 km

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "distance_goal_"+weekKey(mon)) {
		t.Errorf("expected distance_goal; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_DistanceGoal_CustomTarget verifies that a parent-configured
// distance target overrides the default.
func TestWeeklyBonus_DistanceGoal_CustomTarget(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w4.com")
	childID := insertUser(t, db, "child@w4.com")
	linkChild(t, db, parentID, childID)

	if err := SetChildWeeklySetting(db, childID, "kids_stars_weekly_distance_target_km", "5.00"); err != nil {
		t.Fatalf("SetChildWeeklySetting: %v", err)
	}

	mon := monday2025W12
	// 6 km — exceeds custom 5 km target but is below the default 10 km.
	insertWorkoutAt(t, db, childID, 3600, 6000, ts(mon))

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "distance_goal_"+weekKey(mon)) {
		t.Errorf("expected distance_goal with custom 5 km target; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_DurationGoal_Default verifies the duration_goal bonus fires with
// the default 150-minute threshold.
func TestWeeklyBonus_DurationGoal_Default(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w5.com")
	childID := insertUser(t, db, "child@w5.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 160*60, 1000, ts(mon)) // 160 min

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "duration_goal_"+weekKey(mon)) {
		t.Errorf("expected duration_goal; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_DurationGoal_CustomTarget verifies that a parent-configured
// duration target overrides the default.
func TestWeeklyBonus_DurationGoal_CustomTarget(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w6.com")
	childID := insertUser(t, db, "child@w6.com")
	linkChild(t, db, parentID, childID)

	if err := SetChildWeeklySetting(db, childID, "kids_stars_weekly_duration_target_min", "60"); err != nil {
		t.Fatalf("SetChildWeeklySetting: %v", err)
	}

	mon := monday2025W12
	// 70 min — meets custom 60 min target but not the default 150 min.
	insertWorkoutAt(t, db, childID, 70*60, 1000, ts(mon))

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "duration_goal_"+weekKey(mon)) {
		t.Errorf("expected duration_goal with custom 60 min target; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_ImprovementBonus verifies the improvement_bonus fires when this
// week's total distance exceeds last week's.
func TestWeeklyBonus_ImprovementBonus(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w7.com")
	childID := insertUser(t, db, "child@w7.com")
	linkChild(t, db, parentID, childID)

	prevMon := monday2025W12.AddDate(0, 0, -7)
	insertWorkoutAt(t, db, childID, 3600, 5000, ts(prevMon)) // 5 km last week

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 3600, 8000, ts(mon)) // 8 km this week

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "improvement_bonus_"+weekKey(mon)) {
		t.Errorf("expected improvement_bonus; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_ImprovementBonus_NotAwarded_WhenRegressed verifies no improvement
// bonus when this week's distance is lower than last week's.
func TestWeeklyBonus_ImprovementBonus_NotAwarded_WhenRegressed(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w8.com")
	childID := insertUser(t, db, "child@w8.com")
	linkChild(t, db, parentID, childID)

	prevMon := monday2025W12.AddDate(0, 0, -7)
	insertWorkoutAt(t, db, childID, 3600, 10000, ts(prevMon)) // 10 km last week

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 3600, 8000, ts(mon)) // 8 km — regression

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if hasAwardReason(awards, "improvement_bonus_"+weekKey(mon)) {
		t.Error("did not expect improvement_bonus when distance regressed")
	}
}

// TestWeeklyBonus_StreakMultiplier verifies the 1.5x streak multiplier fires when
// the weekly workout streak meets the threshold.
func TestWeeklyBonus_StreakMultiplier(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w9.com")
	childID := insertUser(t, db, "child@w9.com")
	linkChild(t, db, parentID, childID)

	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'weekly_workout', ?, ?, '')
	`, childID, streakMultiplierThreshold, streakMultiplierThreshold); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	mon := monday2025W12
	// One workout meeting distance (11 km) and duration (160 min) goals.
	insertWorkoutAt(t, db, childID, 160*60, 11000, ts(mon))

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	key := weekKey(mon)
	if !hasAwardReason(awards, "streak_multiplier_"+key) {
		t.Errorf("expected streak_multiplier; got %v", awardReasons(awards))
	}

	// Multiplier amount must be ceiling(50% of non-multiplier total).
	var nonMultiplierTotal, multiplierAmount int
	for _, a := range awards {
		if a.Reason == "streak_multiplier_"+key {
			multiplierAmount = a.Amount
		} else {
			nonMultiplierTotal += a.Amount
		}
	}
	want := int(float64(nonMultiplierTotal)*0.5 + 0.5)
	if multiplierAmount != want {
		t.Errorf("streak_multiplier amount = %d, want %d (50%% of non-multiplier total %d)",
			multiplierAmount, want, nonMultiplierTotal)
	}
}

// TestWeeklyBonus_StreakMultiplier_BelowThreshold verifies no multiplier fires when
// the weekly streak count is below the threshold.
func TestWeeklyBonus_StreakMultiplier_BelowThreshold(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w10.com")
	childID := insertUser(t, db, "child@w10.com")
	linkChild(t, db, parentID, childID)

	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'weekly_workout', 1, 1, '')
	`, childID); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 160*60, 11000, ts(mon))

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if hasAwardReason(awards, "streak_multiplier_"+weekKey(mon)) {
		t.Error("did not expect streak_multiplier when streak is below threshold")
	}
}

// TestWeeklyBonus_PerfectWeek verifies the perfect_week bonus fires when all five
// base bonuses are achieved in the same week.
func TestWeeklyBonus_PerfectWeek(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w11.com")
	childID := insertUser(t, db, "child@w11.com")
	linkChild(t, db, parentID, childID)

	// Previous week: 5 km so the improvement bonus can fire this week.
	prevMon := monday2025W12.AddDate(0, 0, -7)
	insertWorkoutAt(t, db, childID, 3600, 5000, ts(prevMon))

	mon := monday2025W12
	// 7 days × 30 min × 2.5 km → total 210 min (>150) + 17.5 km (>10) + all 7 days.
	for i := 0; i < 7; i++ {
		insertWorkoutAt(t, db, childID, 30*60, 2500, ts(mon.AddDate(0, 0, i)))
	}

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("EvaluateWeeklyBonuses: %v", err)
	}

	if !hasAwardReason(awards, "perfect_week_"+weekKey(mon)) {
		t.Errorf("expected perfect_week; got %v", awardReasons(awards))
	}
}

// TestWeeklyBonus_Idempotency verifies that calling EvaluateWeeklyBonuses twice
// for the same user and week only awards stars once.
func TestWeeklyBonus_Idempotency(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w12.com")
	childID := insertUser(t, db, "child@w12.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12
	insertWorkoutAt(t, db, childID, 3600, 11000, ts(mon))

	awards1, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(awards1) == 0 {
		t.Fatal("expected awards on first call")
	}

	awards2, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if awards2 != nil {
		t.Errorf("expected nil on idempotent second call, got %v", awards2)
	}

	// Balance must equal only the first-call total.
	var balance int
	if err := db.QueryRow(`SELECT COALESCE(current_balance, 0) FROM star_balances WHERE user_id = ?`, childID).Scan(&balance); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	want := 0
	for _, a := range awards1 {
		want += a.Amount
	}
	if balance != want {
		t.Errorf("balance = %d, want %d (idempotency violated)", balance, want)
	}
}

// TestWeeklyBonus_NoWorkouts verifies the function returns nil awards and still
// marks the week as evaluated (so it is not re-evaluated later).
func TestWeeklyBonus_NoWorkouts(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)

	parentID := insertUser(t, db, "parent@w13.com")
	childID := insertUser(t, db, "child@w13.com")
	linkChild(t, db, parentID, childID)

	mon := monday2025W12

	awards, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if awards != nil {
		t.Errorf("expected nil awards with no workouts, got %v", awards)
	}

	// Second call must also be a no-op (idempotency guard inserted on empty result).
	awards2, err := EvaluateWeeklyBonuses(ctx, db, childID, mon)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if awards2 != nil {
		t.Errorf("expected nil on second call with no workouts, got %v", awards2)
	}
}

// TestGetChildWeeklySettings_Defaults verifies that default values are returned
// when no preferences are stored for the child.
func TestGetChildWeeklySettings_Defaults(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@s1.com")
	childID := insertUser(t, db, "child@s1.com")
	linkChild(t, db, parentID, childID)

	settings, err := GetChildWeeklySettings(db, childID)
	if err != nil {
		t.Fatalf("GetChildWeeklySettings: %v", err)
	}
	if settings.WeeklyDistanceTargetKm != defaultWeeklyDistanceTargetKm {
		t.Errorf("distance default = %.2f, want %.2f",
			settings.WeeklyDistanceTargetKm, defaultWeeklyDistanceTargetKm)
	}
	if settings.WeeklyDurationTargetMin != defaultWeeklyDurationTargetMin {
		t.Errorf("duration default = %d, want %d",
			settings.WeeklyDurationTargetMin, defaultWeeklyDurationTargetMin)
	}
}

// TestGetChildWeeklySettings_CustomValues verifies that parent-set preferences are
// read correctly.
func TestGetChildWeeklySettings_CustomValues(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@s2.com")
	childID := insertUser(t, db, "child@s2.com")
	linkChild(t, db, parentID, childID)

	if err := SetChildWeeklySetting(db, childID, "kids_stars_weekly_distance_target_km", "7.50"); err != nil {
		t.Fatalf("set distance: %v", err)
	}
	if err := SetChildWeeklySetting(db, childID, "kids_stars_weekly_duration_target_min", "90"); err != nil {
		t.Fatalf("set duration: %v", err)
	}

	settings, err := GetChildWeeklySettings(db, childID)
	if err != nil {
		t.Fatalf("GetChildWeeklySettings: %v", err)
	}
	if settings.WeeklyDistanceTargetKm != 7.5 {
		t.Errorf("distance = %.2f, want 7.50", settings.WeeklyDistanceTargetKm)
	}
	if settings.WeeklyDurationTargetMin != 90 {
		t.Errorf("duration = %d, want 90", settings.WeeklyDurationTargetMin)
	}
}
