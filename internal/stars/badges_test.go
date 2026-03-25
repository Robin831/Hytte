package stars

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// badgeTestDB creates an in-memory DB with all tables needed for badge tests.
func badgeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	extra := `
	CREATE TABLE IF NOT EXISTS badge_definitions (
		id          INTEGER PRIMARY KEY,
		key         TEXT UNIQUE NOT NULL,
		name        TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		category    TEXT NOT NULL DEFAULT '',
		tier        TEXT NOT NULL DEFAULT '',
		icon        TEXT NOT NULL DEFAULT '',
		xp_reward   INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_badges (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		badge_key  TEXT NOT NULL,
		earned_at  TEXT NOT NULL DEFAULT '',
		workout_id INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
		UNIQUE(user_id, badge_key)
	);

	CREATE INDEX IF NOT EXISTS idx_user_badges_user_id ON user_badges(user_id);`

	if _, err := db.Exec(extra); err != nil {
		t.Fatalf("create badge tables: %v", err)
	}
	return db
}

// TestSeedBadges_Idempotent verifies that SeedBadges inserts all definitions
// and that a second call does not fail or duplicate rows.
func TestSeedBadges_Idempotent(t *testing.T) {
	db := badgeTestDB(t)

	if err := SeedBadges(db); err != nil {
		t.Fatalf("first SeedBadges: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM badge_definitions`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != len(allBadges) {
		t.Errorf("want %d badge definitions, got %d", len(allBadges), count)
	}

	// Second call must not return an error.
	if err := SeedBadges(db); err != nil {
		t.Fatalf("second SeedBadges: %v", err)
	}

	// Row count must remain unchanged.
	var count2 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM badge_definitions`).Scan(&count2); err != nil {
		t.Fatalf("count2: %v", err)
	}
	if count2 != count {
		t.Errorf("after second seed: want %d rows, got %d", count, count2)
	}
}

// TestEvaluateBadges_ReturnsEmptySlice ensures that when no badges are newly
// earned the function returns an empty (non-nil) slice, so JSON serialises to
// [] not null.
func TestEvaluateBadges_ReturnsEmptySlice(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "zero@test.com")
	// Workout with no metrics — nothing should be awarded.
	w := WorkoutInput{ID: 0}

	badges, err := EvaluateBadges(context.Background(), db, userID, w)
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}
	if badges == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(badges) != 0 {
		t.Errorf("expected 0 badges, got %d", len(badges))
	}
}

// TestEvaluateBadges_NullWorkoutID verifies that a badge awarded without a
// real workout (ID=0) stores NULL in user_badges.workout_id rather than 0.
func TestEvaluateBadges_NullWorkoutID(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "nowkid@test.com")

	// Christmas Spirit: workout started on Dec 25, no real workout row.
	w := WorkoutInput{ID: 0} // no FK
	// Trigger by seeding started_at via a manual award call.
	now := time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	badge, err := awardBadge(context.Background(), db, userID, "badge_christmas_spirit", 0, now)
	if err != nil {
		t.Fatalf("awardBadge: %v", err)
	}
	if badge.WorkoutID != nil {
		t.Errorf("WorkoutID should be nil when input is 0, got %v", *badge.WorkoutID)
	}
	_ = w
}

// TestEvaluateBadges_DistanceBadge verifies that the 5K badge is awarded when
// the workout distance meets the threshold.
func TestEvaluateBadges_DistanceBadge(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "dist@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 300, 0, 360)

	w := WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
	}
	badges, err := EvaluateBadges(context.Background(), db, userID, w)
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_5k"] {
		t.Errorf("expected badge_5k to be awarded, got keys: %v", keys)
	}
	if !keys["badge_first_km"] {
		t.Errorf("expected badge_first_km to be awarded, got keys: %v", keys)
	}
}

// TestEvaluateBadges_NoDuplicates verifies that earning a badge twice does not
// insert duplicate rows — the second call must return an empty list.
func TestEvaluateBadges_NoDuplicates(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "dup@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	w := WorkoutInput{ID: wid, DistanceMeters: 5000, DurationSeconds: 1800}

	first, err := EvaluateBadges(context.Background(), db, userID, w)
	if err != nil {
		t.Fatalf("first EvaluateBadges: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected at least one badge on first call")
	}

	second, err := EvaluateBadges(context.Background(), db, userID, w)
	if err != nil {
		t.Fatalf("second EvaluateBadges: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 badges on second call, got %d", len(second))
	}
}

// TestEvaluateBadges_SpeedBadge verifies pace-based badge awarding.
func TestEvaluateBadges_SpeedBadge(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "speed@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 295) // 4:55 min/km → sub-5

	w := WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
		AvgPaceSecPerKm: 295, // 4:55 → under 300s → badge_sub_5_pace
	}
	badges, err := EvaluateBadges(context.Background(), db, userID, w)
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_sub_5_pace"] {
		t.Errorf("expected badge_sub_5_pace to be awarded, got keys: %v", keys)
	}
	if !keys["badge_sub_6_pace"] {
		t.Errorf("expected badge_sub_6_pace to be awarded, got keys: %v", keys)
	}
}

// TestAwardBadge_WorkoutIDNonZero verifies that a positive workout ID is stored.
func TestAwardBadge_WorkoutIDNonZero(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "wid@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	now := time.Now().UTC().Format(time.RFC3339)
	badge, err := awardBadge(context.Background(), db, userID, "badge_5k", wid, now)
	if err != nil {
		t.Fatalf("awardBadge: %v", err)
	}
	if badge.WorkoutID == nil {
		t.Error("WorkoutID should be non-nil for a real workout")
	} else if *badge.WorkoutID != wid {
		t.Errorf("WorkoutID = %d, want %d", *badge.WorkoutID, wid)
	}
}

// TestSeedBadges_AllCategories verifies that every expected category is
// represented in the seeded data.
func TestSeedBadges_AllCategories(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wantCategories := []string{"distance", "consistency", "speed", "variety", "heart", "fun", "secret"}
	for _, cat := range wantCategories {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM badge_definitions WHERE category = ?`, cat).Scan(&count); err != nil {
			t.Fatalf("count category %s: %v", cat, err)
		}
		if count == 0 {
			t.Errorf("category %q has no badge definitions", cat)
		}
	}
}
