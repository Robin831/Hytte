package stars

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// setupStreaksTestDB creates an in-memory SQLite database with the tables
// required for streak tests (users, family_links, workouts, star_transactions,
// star_balances, streaks, streak_shields).
func setupStreaksTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id        INTEGER PRIMARY KEY,
		email     TEXT UNIQUE NOT NULL,
		name      TEXT NOT NULL,
		picture   TEXT NOT NULL DEFAULT '',
		google_id TEXT UNIQUE NOT NULL,
		is_admin  INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS family_links (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		nickname     TEXT NOT NULL DEFAULT '',
		avatar_emoji TEXT NOT NULL DEFAULT '⭐',
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id),
		UNIQUE(child_id)
	);

	CREATE TABLE IF NOT EXISTS workouts (
		id                  INTEGER PRIMARY KEY,
		user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		sport               TEXT NOT NULL DEFAULT 'other',
		started_at          TEXT NOT NULL DEFAULT '',
		duration_seconds    INTEGER NOT NULL DEFAULT 0,
		distance_meters     REAL NOT NULL DEFAULT 0,
		avg_heart_rate      INTEGER NOT NULL DEFAULT 0,
		max_heart_rate      INTEGER NOT NULL DEFAULT 0,
		avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
		calories            INTEGER NOT NULL DEFAULT 0,
		ascent_meters       REAL NOT NULL DEFAULT 0,
		fit_file_hash       TEXT NOT NULL DEFAULT '',
		created_at          TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS star_transactions (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		amount       INTEGER NOT NULL,
		reason       TEXT NOT NULL,
		description  TEXT NOT NULL DEFAULT '',
		reference_id INTEGER,
		created_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS star_balances (
		user_id         INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		total_earned    INTEGER NOT NULL DEFAULT 0,
		total_spent     INTEGER NOT NULL DEFAULT 0,
		current_balance INTEGER GENERATED ALWAYS AS (total_earned - total_spent) STORED
	);

	CREATE TABLE IF NOT EXISTS streaks (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		streak_type   TEXT NOT NULL,
		current_count INTEGER NOT NULL DEFAULT 0,
		longest_count INTEGER NOT NULL DEFAULT 0,
		last_activity TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, streak_type)
	);

	CREATE TABLE IF NOT EXISTS streak_shields (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used_at     TEXT NOT NULL DEFAULT '',
		shield_date TEXT NOT NULL DEFAULT ''
	);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// insertStreakUser inserts a test user and returns their ID.
func insertStreakUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO users (email, name, picture, google_id) VALUES (?, 'Test', '', ?)
	`, email, email)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// getStreak returns (current_count, longest_count) for a user and streak type.
func getStreak(t *testing.T, db *sql.DB, userID int64, streakType string) (current, longest int64) {
	t.Helper()
	err := db.QueryRow(`
		SELECT current_count, longest_count FROM streaks
		WHERE user_id = ? AND streak_type = ?
	`, userID, streakType).Scan(&current, &longest)
	if err == sql.ErrNoRows {
		return 0, 0
	}
	if err != nil {
		t.Fatalf("get streak: %v", err)
	}
	return
}

func TestUpdateStreak_FirstWorkout(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")

	day := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	if err := UpdateStreak(context.Background(), db, userID, day); err != nil {
		t.Fatalf("UpdateStreak: %v", err)
	}

	cur, longest := getStreak(t, db, userID, "daily_workout")
	if cur != 1 {
		t.Errorf("daily current_count = %d, want 1", cur)
	}
	if longest != 1 {
		t.Errorf("daily longest_count = %d, want 1", longest)
	}

	cur, longest = getStreak(t, db, userID, "weekly_workout")
	if cur != 1 {
		t.Errorf("weekly current_count = %d, want 1", cur)
	}
	if longest != 1 {
		t.Errorf("weekly longest_count = %d, want 1", longest)
	}
}

func TestUpdateStreak_SameDayNoOp(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")

	day := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	if err := UpdateStreak(context.Background(), db, userID, day); err != nil {
		t.Fatalf("first UpdateStreak: %v", err)
	}
	// Second call same day — should be a no-op.
	laterSameDay := time.Date(2026, 3, 10, 18, 0, 0, 0, time.UTC)
	if err := UpdateStreak(context.Background(), db, userID, laterSameDay); err != nil {
		t.Fatalf("second UpdateStreak: %v", err)
	}

	cur, _ := getStreak(t, db, userID, "daily_workout")
	if cur != 1 {
		t.Errorf("daily current_count after same-day duplicate = %d, want 1", cur)
	}
}

func TestUpdateStreak_ConsecutiveDays(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		day := time.Date(2026, 3, 10+i, 9, 0, 0, 0, time.UTC)
		if err := UpdateStreak(ctx, db, userID, day); err != nil {
			t.Fatalf("UpdateStreak day %d: %v", i+1, err)
		}
	}

	cur, longest := getStreak(t, db, userID, "daily_workout")
	if cur != 5 {
		t.Errorf("daily current_count = %d, want 5", cur)
	}
	if longest != 5 {
		t.Errorf("daily longest_count = %d, want 5", longest)
	}
}

func TestUpdateStreak_Break(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Build a 3-day streak.
	for i := 0; i < 3; i++ {
		day := time.Date(2026, 3, 10+i, 9, 0, 0, 0, time.UTC)
		if err := UpdateStreak(ctx, db, userID, day); err != nil {
			t.Fatalf("UpdateStreak day %d: %v", i+1, err)
		}
	}

	// Miss a day, then work out again.
	afterBreak := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC) // day 5, skipped day 4
	if err := UpdateStreak(ctx, db, userID, afterBreak); err != nil {
		t.Fatalf("UpdateStreak after break: %v", err)
	}

	cur, longest := getStreak(t, db, userID, "daily_workout")
	if cur != 1 {
		t.Errorf("daily current_count after break = %d, want 1", cur)
	}
	// Longest should still be 3 from the previous streak.
	if longest != 3 {
		t.Errorf("daily longest_count = %d, want 3", longest)
	}
}

func TestUpdateStreak_LongestCountTracking(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Build a 4-day streak, break it, then build a 2-day streak.
	for i := 0; i < 4; i++ {
		day := time.Date(2026, 3, 1+i, 9, 0, 0, 0, time.UTC)
		if err := UpdateStreak(ctx, db, userID, day); err != nil {
			t.Fatalf("build streak day %d: %v", i+1, err)
		}
	}

	// Break the streak.
	if err := UpdateStreak(ctx, db, userID, time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("UpdateStreak after break: %v", err)
	}
	if err := UpdateStreak(ctx, db, userID, time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("UpdateStreak after break day 2: %v", err)
	}

	cur, longest := getStreak(t, db, userID, "daily_workout")
	if cur != 2 {
		t.Errorf("current_count = %d, want 2", cur)
	}
	if longest != 4 {
		t.Errorf("longest_count = %d, want 4 (previous best)", longest)
	}
}

func TestUpdateStreak_WeeklyStreak_ConsecutiveWeeks(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Week 1: Monday 2026-03-02
	week1 := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	// Week 2: Monday 2026-03-09
	week2 := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	// Week 3: Friday 2026-03-20
	week3 := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	for i, d := range []time.Time{week1, week2, week3} {
		if err := UpdateStreak(ctx, db, userID, d); err != nil {
			t.Fatalf("UpdateStreak week %d: %v", i+1, err)
		}
	}

	cur, longest := getStreak(t, db, userID, "weekly_workout")
	if cur != 3 {
		t.Errorf("weekly current_count = %d, want 3", cur)
	}
	if longest != 3 {
		t.Errorf("weekly longest_count = %d, want 3", longest)
	}
}

func TestUpdateStreak_WeeklySameWeekNoOp(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Two workouts in the same ISO week.
	mon := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	fri := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)

	if err := UpdateStreak(ctx, db, userID, mon); err != nil {
		t.Fatalf("Monday UpdateStreak: %v", err)
	}
	if err := UpdateStreak(ctx, db, userID, fri); err != nil {
		t.Fatalf("Friday UpdateStreak: %v", err)
	}

	cur, _ := getStreak(t, db, userID, "weekly_workout")
	if cur != 1 {
		t.Errorf("weekly current_count after same-week duplicate = %d, want 1", cur)
	}
}

func TestUpdateStreak_WeeklyBreak(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	week1 := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	week2 := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	// Skip week 3; work out in week 4.
	week4 := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)

	for _, d := range []time.Time{week1, week2, week4} {
		if err := UpdateStreak(ctx, db, userID, d); err != nil {
			t.Fatalf("UpdateStreak: %v", err)
		}
	}

	cur, longest := getStreak(t, db, userID, "weekly_workout")
	if cur != 1 {
		t.Errorf("weekly current_count after break = %d, want 1", cur)
	}
	if longest != 2 {
		t.Errorf("weekly longest_count = %d, want 2", longest)
	}
}

func TestGetStreaks(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	day := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	if err := UpdateStreak(ctx, db, userID, day); err != nil {
		t.Fatalf("UpdateStreak: %v", err)
	}

	resp, err := GetStreaks(ctx, db, userID)
	if err != nil {
		t.Fatalf("GetStreaks: %v", err)
	}
	if resp.DailyWorkout.CurrentCount != 1 {
		t.Errorf("DailyWorkout.CurrentCount = %d, want 1", resp.DailyWorkout.CurrentCount)
	}
	if resp.DailyWorkout.LastActivity != "2026-03-10" {
		t.Errorf("DailyWorkout.LastActivity = %q, want %q", resp.DailyWorkout.LastActivity, "2026-03-10")
	}
	if resp.WeeklyWorkout.CurrentCount != 1 {
		t.Errorf("WeeklyWorkout.CurrentCount = %d, want 1", resp.WeeklyWorkout.CurrentCount)
	}
}

func TestGetStreaks_Empty(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")

	resp, err := GetStreaks(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("GetStreaks: %v", err)
	}
	if resp.DailyWorkout.CurrentCount != 0 {
		t.Errorf("expected empty daily streak, got %d", resp.DailyWorkout.CurrentCount)
	}
	if resp.WeeklyWorkout.CurrentCount != 0 {
		t.Errorf("expected empty weekly streak, got %d", resp.WeeklyWorkout.CurrentCount)
	}
}

func TestCheckStreakAtRisk_DailyAtRisk(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Set last_activity to yesterday manually.
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, userID, yesterday.Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	risk, err := CheckStreakAtRisk(ctx, db, userID)
	if err != nil {
		t.Fatalf("CheckStreakAtRisk: %v", err)
	}
	if !risk.DailyAtRisk {
		t.Error("expected DailyAtRisk = true")
	}
}

func TestCheckStreakAtRisk_DailyNotAtRisk(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Last activity is today — not at risk.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, userID, today.Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	risk, err := CheckStreakAtRisk(ctx, db, userID)
	if err != nil {
		t.Fatalf("CheckStreakAtRisk: %v", err)
	}
	if risk.DailyAtRisk {
		t.Error("expected DailyAtRisk = false when last_activity is today")
	}
}

func TestCheckStreakAtRisk_WeeklyAtRisk(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Insert weekly streak with last_activity in the previous ISO week.
	lastWeekDate := time.Now().UTC().AddDate(0, 0, -7)
	lastYear, lastWeek := lastWeekDate.ISOWeek()
	lastActivityStr := formatStreakDate("weekly_workout", lastWeekDate)

	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'weekly_workout', 2, 2, ?)
	`, userID, lastActivityStr); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	risk, err := CheckStreakAtRisk(ctx, db, userID)
	if err != nil {
		t.Fatalf("CheckStreakAtRisk: %v", err)
	}

	nowYear, nowWeek := time.Now().UTC().ISOWeek()
	nextMon := firstDayOfISOWeek(lastYear, lastWeek).AddDate(0, 0, 7)
	nextYear, nextWeekNum := nextMon.ISOWeek()

	if nowYear == nextYear && nowWeek == nextWeekNum {
		if !risk.WeeklyAtRisk {
			t.Error("expected WeeklyAtRisk = true")
		}
	} else {
		// The previous week was not the immediately preceding week (e.g. two weeks ago),
		// so the streak would already be broken — not "at risk".
		if risk.WeeklyAtRisk {
			t.Error("expected WeeklyAtRisk = false when last activity was two+ weeks ago")
		}
	}
}

func TestUseStreakShield_Basic(t *testing.T) {
	db := setupStreaksTestDB(t)
	parentID := insertStreakUser(t, db, "parent@test.com")
	childID := insertStreakUser(t, db, "child@test.com")
	ctx := context.Background()

	// Insert a streak for the child.
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 5, 5, ?)
	`, childID, yesterday.Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	if err := UseStreakShield(ctx, db, parentID, childID); err != nil {
		t.Fatalf("UseStreakShield: %v", err)
	}

	// Verify last_activity was advanced to today.
	today := time.Now().UTC().Format("2006-01-02")
	var lastActivity string
	if err := db.QueryRow(`
		SELECT last_activity FROM streaks WHERE user_id = ? AND streak_type = 'daily_workout'
	`, childID).Scan(&lastActivity); err != nil {
		t.Fatalf("query last_activity: %v", err)
	}
	if lastActivity != today {
		t.Errorf("last_activity = %q, want %q", lastActivity, today)
	}

	// Verify shield was recorded.
	var shieldCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM streak_shields WHERE parent_id = ? AND child_id = ?`,
		parentID, childID).Scan(&shieldCount); err != nil {
		t.Fatalf("count shields: %v", err)
	}
	if shieldCount != 1 {
		t.Errorf("shield count = %d, want 1", shieldCount)
	}
}

func TestUseStreakShield_WeeklyLimitEnforced(t *testing.T) {
	db := setupStreaksTestDB(t)
	parentID := insertStreakUser(t, db, "parent@test.com")
	childID := insertStreakUser(t, db, "child@test.com")
	ctx := context.Background()

	// Insert streak for child.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, childID, time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	// Use first shield — should succeed.
	if err := UseStreakShield(ctx, db, parentID, childID); err != nil {
		t.Fatalf("first shield: %v", err)
	}

	// Use second shield this week — should fail.
	if err := UseStreakShield(ctx, db, parentID, childID); err != ErrShieldLimitReached {
		t.Errorf("expected ErrShieldLimitReached, got %v", err)
	}
}

func TestUseStreakShield_DifferentWeeksReset(t *testing.T) {
	db := setupStreaksTestDB(t)
	parentID := insertStreakUser(t, db, "parent@test.com")
	childID := insertStreakUser(t, db, "child@test.com")
	ctx := context.Background()

	// Insert streak row.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, childID, time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	// Simulate a shield from last week by inserting directly with an old used_at.
	lastWeek := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO streak_shields (parent_id, child_id, used_at, shield_date)
		VALUES (?, ?, ?, ?)
	`, parentID, childID, lastWeek, "2026-03-10"); err != nil {
		t.Fatalf("setup shield: %v", err)
	}

	// First shield this week — should succeed because last week's shield doesn't count.
	if err := UseStreakShield(ctx, db, parentID, childID); err != nil {
		t.Fatalf("shield after new week: %v", err)
	}
}

func TestCheckConsistencyStars_MilestoneAwards(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Insert a streak row with current_count = 7.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 7, 7, ?)
	`, userID, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	awards, err := checkConsistencyStars(ctx, db, userID)
	if err != nil {
		t.Fatalf("checkConsistencyStars: %v", err)
	}

	// Should get streak_3day and streak_7day.
	reasons := make(map[string]bool)
	for _, a := range awards {
		reasons[a.Reason] = true
	}
	if !reasons["streak_3day"] {
		t.Error("expected streak_3day award")
	}
	if !reasons["streak_7day"] {
		t.Error("expected streak_7day award")
	}
	if reasons["streak_14day"] {
		t.Error("unexpected streak_14day award for streak of 7")
	}
}

func TestCheckConsistencyStars_Idempotent(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, userID, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("setup streak: %v", err)
	}

	// Pre-insert the streak_3day transaction to simulate already awarded.
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
		VALUES (?, 10, 'streak_3day', '3-day workout streak!', ?)
	`, userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("setup transaction: %v", err)
	}

	awards, err := checkConsistencyStars(ctx, db, userID)
	if err != nil {
		t.Fatalf("checkConsistencyStars: %v", err)
	}

	for _, a := range awards {
		if a.Reason == "streak_3day" {
			t.Error("streak_3day should not be awarded again")
		}
	}
}

func TestCheckTimeOfDayStars_EarlyBird(t *testing.T) {
	earlyMorning := time.Date(2026, 3, 10, 5, 30, 0, 0, time.UTC)
	awards := checkTimeOfDayStars(42, earlyMorning)
	if len(awards) != 1 {
		t.Fatalf("expected 1 award, got %d", len(awards))
	}
	if awards[0].Reason != "early_bird_42" {
		t.Errorf("reason = %q, want early_bird_42", awards[0].Reason)
	}
	if awards[0].Amount != 3 {
		t.Errorf("amount = %d, want 3", awards[0].Amount)
	}
}

func TestCheckTimeOfDayStars_NightOwl(t *testing.T) {
	lateNight := time.Date(2026, 3, 10, 22, 30, 0, 0, time.UTC)
	awards := checkTimeOfDayStars(99, lateNight)
	if len(awards) != 1 {
		t.Fatalf("expected 1 award, got %d", len(awards))
	}
	if awards[0].Reason != "night_owl_99" {
		t.Errorf("reason = %q, want night_owl_99", awards[0].Reason)
	}
}

func TestCheckTimeOfDayStars_DaytimeNoAward(t *testing.T) {
	midday := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	awards := checkTimeOfDayStars(1, midday)
	if len(awards) != 0 {
		t.Errorf("expected no awards for midday workout, got %d", len(awards))
	}
}

func TestCheckWeekendWarrior_BothDays(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// ISO week 11 of 2026: Mon=2026-03-09, Sat=2026-03-14, Sun=2026-03-15.
	sat := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	// Insert workouts for both Saturday and Sunday.
	for _, d := range []time.Time{sat, sun} {
		if _, err := db.Exec(`
			INSERT INTO workouts (user_id, sport, started_at, fit_file_hash, created_at)
			VALUES (?, 'run', ?, ?, ?)
		`, userID, d.Format(time.RFC3339),
			d.Format("hash-2006-01-02"),
			d.Format(time.RFC3339)); err != nil {
			t.Fatalf("setup workout: %v", err)
		}
	}

	awards, err := checkWeekendWarrior(ctx, db, userID, sun)
	if err != nil {
		t.Fatalf("checkWeekendWarrior: %v", err)
	}
	if len(awards) != 1 {
		t.Fatalf("expected 1 award, got %d", len(awards))
	}
	if awards[0].Amount != 15 {
		t.Errorf("amount = %d, want 15", awards[0].Amount)
	}
}

func TestCheckWeekendWarrior_OnlyOneDay(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Only Saturday workout.
	sat := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (?, 'run', ?, 'hash', ?)
	`, userID, sat.Format(time.RFC3339), sat.Format(time.RFC3339)); err != nil {
		t.Fatalf("setup workout: %v", err)
	}

	awards, err := checkWeekendWarrior(ctx, db, userID, sat)
	if err != nil {
		t.Fatalf("checkWeekendWarrior: %v", err)
	}
	if len(awards) != 0 {
		t.Errorf("expected no award for single day, got %d", len(awards))
	}
}

func TestCheckWeekendWarrior_Weekday(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	// Weekday workout should return immediately with no award.
	wed := time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)
	awards, err := checkWeekendWarrior(ctx, db, userID, wed)
	if err != nil {
		t.Fatalf("checkWeekendWarrior: %v", err)
	}
	if len(awards) != 0 {
		t.Errorf("expected no award for weekday, got %d", len(awards))
	}
}

func TestCheckWeekendWarrior_Idempotent(t *testing.T) {
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "u@test.com")
	ctx := context.Background()

	sat := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	for _, d := range []time.Time{sat, sun} {
		if _, err := db.Exec(`
			INSERT INTO workouts (user_id, sport, started_at, fit_file_hash, created_at)
			VALUES (?, 'run', ?, ?, ?)
		`, userID, d.Format(time.RFC3339), d.Format("hash-2006-01-02"), d.Format(time.RFC3339)); err != nil {
			t.Fatalf("setup workout: %v", err)
		}
	}

	// Pre-insert the weekend warrior transaction with the same reason format.
	year, week := sun.ISOWeek()
	reason := fmt.Sprintf("weekend_warrior_%d_%02d", year, week)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
		VALUES (?, 15, ?, 'Weekend warrior', ?)
	`, userID, reason, sun.Format(time.RFC3339)); err != nil {
		t.Fatalf("setup transaction: %v", err)
	}

	awards, err := checkWeekendWarrior(ctx, db, userID, sun)
	if err != nil {
		t.Fatalf("checkWeekendWarrior: %v", err)
	}
	if len(awards) != 0 {
		t.Errorf("expected no award when already awarded this week, got %d", len(awards))
	}
}

func TestFormatAndParseStreakDate_Daily(t *testing.T) {
	orig := time.Date(2026, 3, 15, 13, 45, 0, 0, time.UTC)
	formatted := formatStreakDate("daily_workout", orig)
	if formatted != "2026-03-15" {
		t.Errorf("formatStreakDate = %q, want %q", formatted, "2026-03-15")
	}
	parsed, err := parseStreakDate("daily_workout", formatted)
	if err != nil {
		t.Fatalf("parseStreakDate: %v", err)
	}
	if parsed.Year() != 2026 || parsed.Month() != 3 || parsed.Day() != 15 {
		t.Errorf("parsed date = %v, want 2026-03-15", parsed)
	}
}

func TestFormatAndParseStreakDate_Weekly(t *testing.T) {
	// 2026-03-15 is in ISO week 11 of 2026.
	d := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)
	formatted := formatStreakDate("weekly_workout", d)
	expectedYear, expectedWeek := d.ISOWeek()
	expected := fmt.Sprintf("%d-W%02d", expectedYear, expectedWeek)
	if formatted != expected {
		t.Errorf("formatStreakDate = %q, want %q", formatted, expected)
	}

	parsed, err := parseStreakDate("weekly_workout", formatted)
	if err != nil {
		t.Fatalf("parseStreakDate: %v", err)
	}
	parsedYear, parsedWeek := parsed.ISOWeek()
	if parsedYear != expectedYear || parsedWeek != expectedWeek {
		t.Errorf("parsed ISO week = %d-W%02d, want %d-W%02d", parsedYear, parsedWeek, expectedYear, expectedWeek)
	}
}

func TestFirstDayOfISOWeek(t *testing.T) {
	// 2026-W11: Monday should be 2026-03-09.
	mon := firstDayOfISOWeek(2026, 11)
	if mon.Year() != 2026 || mon.Month() != 3 || mon.Day() != 9 {
		t.Errorf("firstDayOfISOWeek(2026, 11) = %v, want 2026-03-09", mon)
	}
	if mon.Weekday() != time.Monday {
		t.Errorf("firstDayOfISOWeek(2026, 11) is not Monday, got %v", mon.Weekday())
	}
}

func TestSameDay(t *testing.T) {
	a := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	b := time.Date(2026, 3, 10, 23, 59, 59, 0, time.UTC)
	c := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)

	if !sameDay(a, b) {
		t.Error("expected sameDay(a, b) = true")
	}
	if sameDay(a, c) {
		t.Error("expected sameDay(a, c) = false")
	}
}

func TestUpdateStreak_TimezoneEdge(t *testing.T) {
	// Verify that UTC-based day boundary is used consistently.
	// A workout at 23:30 UTC and another at 00:30 UTC next day should be consecutive.
	db := setupStreaksTestDB(t)
	userID := insertStreakUser(t, db, "tz@test.com")
	ctx := context.Background()

	// Day 1: 23:30 UTC on March 10.
	d1 := time.Date(2026, 3, 10, 23, 30, 0, 0, time.UTC)
	// Day 2: 00:30 UTC on March 11 — next UTC day.
	d2 := time.Date(2026, 3, 11, 0, 30, 0, 0, time.UTC)

	if err := UpdateStreak(ctx, db, userID, d1); err != nil {
		t.Fatalf("d1 UpdateStreak: %v", err)
	}
	if err := UpdateStreak(ctx, db, userID, d2); err != nil {
		t.Fatalf("d2 UpdateStreak: %v", err)
	}

	cur, _ := getStreak(t, db, userID, "daily_workout")
	if cur != 2 {
		t.Errorf("daily current_count = %d, want 2 (consecutive UTC days)", cur)
	}
}
