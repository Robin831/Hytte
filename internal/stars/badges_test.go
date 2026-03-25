package stars

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
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

// ---- Marathon distance threshold ----

// TestMarathonDistanceBadge_Qualifying verifies that exactly 42195m earns
// the marathon badge.
func TestMarathonDistanceBadge_Qualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "marathon@test.com")
	wid := insertWorkout(t, db, userID, 14400, 42195, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  42195,
		DurationSeconds: 14400,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_marathon"] {
		t.Errorf("expected badge_marathon for 42195m, got keys: %v", keys)
	}
	// Half-marathon badge should also be awarded as cumulative threshold is met.
	if !keys["badge_half_marathon"] {
		t.Errorf("expected badge_half_marathon for 42195m, got keys: %v", keys)
	}
}

// TestMarathonDistanceBadge_BelowThreshold verifies that 42194m does NOT earn
// the marathon badge (just one metre under the 42.195km threshold).
func TestMarathonDistanceBadge_BelowThreshold(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "almostmarathon@test.com")
	wid := insertWorkout(t, db, userID, 14400, 42194, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  42194,
		DurationSeconds: 14400,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_marathon" {
			t.Errorf("badge_marathon should NOT be awarded for 42194m")
		}
	}
}

// ---- Distance badge: non-qualifying ----

// TestDistanceBadge_NonQualifying verifies no distance badge is awarded for a
// workout shorter than 1km.
func TestDistanceBadge_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "short@test.com")
	wid := insertWorkout(t, db, userID, 300, 500, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  500,
		DurationSeconds: 300,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		switch b.BadgeKey {
		case "badge_first_km", "badge_5k", "badge_10k", "badge_half_marathon", "badge_marathon", "badge_ultramarathon":
			t.Errorf("unexpected distance badge %s for 500m workout", b.BadgeKey)
		}
	}
}

// ---- Speed badge: non-qualifying ----

// TestSpeedBadge_NonQualifying verifies that a slow pace awards no speed badge.
func TestSpeedBadge_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "slow@test.com")
	wid := insertWorkout(t, db, userID, 3600, 5000, 0, 0, 720) // 12 min/km

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 3600,
		AvgPaceSecPerKm: 720,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		switch b.BadgeKey {
		case "badge_sub_6_pace", "badge_sub_5_pace", "badge_sub_4_30_pace",
			"badge_sub_4_pace", "badge_sub_3_30_pace", "badge_speed_demon":
			t.Errorf("unexpected speed badge %s for 12 min/km pace", b.BadgeKey)
		}
	}
}

// ---- Palindrome pace detection ----

// TestIsPalindromeStr exercises the palindrome helper directly.
func TestIsPalindromeStr(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"555", true},   // 5:55
		{"444", true},   // 4:44
		{"121", true},   // 1:21
		{"1221", true},  // 12:21
		{"530", false},  // 5:30
		{"600", false},  // 6:00
		{"", true},      // empty is trivially palindrome
		{"a", true},     // single char
		{"ab", false},
	}

	for _, tc := range tests {
		got := isPalindromeStr(tc.input)
		if got != tc.want {
			t.Errorf("isPalindromeStr(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestPalindromePacerBadge_Qualifying verifies that a 5:55 min/km pace
// (355 s/km → "555") earns badge_palindrome_pacer.
func TestPalindromePacerBadge_Qualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "palindrome@test.com")
	wid := insertWorkout(t, db, userID, 2130, 6000, 0, 0, 355)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  6000,
		DurationSeconds: 2130,
		AvgPaceSecPerKm: 355, // 5:55 → "555" palindrome
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_palindrome_pacer"] {
		t.Errorf("expected badge_palindrome_pacer for 5:55 pace (355 s/km), got keys: %v", keys)
	}
}

// TestPalindromePacerBadge_NonQualifying verifies that a 5:30 pace (330 s/km →
// "530") does NOT earn badge_palindrome_pacer.
func TestPalindromePacerBadge_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "nonpalindrome@test.com")
	wid := insertWorkout(t, db, userID, 1980, 6000, 0, 0, 330)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  6000,
		DurationSeconds: 1980,
		AvgPaceSecPerKm: 330, // 5:30 → "530" not palindrome
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_palindrome_pacer" {
			t.Errorf("badge_palindrome_pacer should NOT be awarded for 5:30 pace (330 s/km)")
		}
	}
}

// ---- Secret badges hidden when unearned ----

// insertWorkoutAt inserts a workout with a specific started_at timestamp.
func insertWorkoutAt(t *testing.T, db *sql.DB, userID int64, durationSec int, distanceM float64, startedAt string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO workouts (user_id, duration_seconds, distance_meters, calories, ascent_meters,
		                      avg_pace_sec_per_km, started_at, fit_file_hash, created_at)
		VALUES (?, ?, ?, 0, 0, 0, ?, ?, ?)
	`, userID, durationSec, distanceM, startedAt,
		fmt.Sprintf("hash-%d-%d", userID, time.Now().UnixNano()),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertWorkoutAt: %v", err)
	}
	id, err2 := res.LastInsertId()
	if err2 != nil {
		t.Fatalf("insertWorkoutAt LastInsertId: %v", err2)
	}
	return id
}

// TestAvailableBadgesHandler_SecretHiddenWhenUnearned verifies that unearned
// secret badges are absent from the AvailableBadgesHandler response.
func TestAvailableBadgesHandler_SecretHiddenWhenUnearned(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "secrettest@test.com")
	user := &auth.User{ID: userID, Email: "secrettest@test.com", Name: "Secret Test"}

	handler := AvailableBadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges/available"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AvailableBadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	for _, b := range resp {
		if b.Category == "secret" {
			t.Errorf("secret badge %q should not appear in available list for a user who has not earned any secret badges", b.Key)
		}
	}
}

// TestAvailableBadgesHandler_SecretVisibleWhenEarned verifies that an earned
// secret badge appears in the available list with earned=true.
func TestAvailableBadgesHandler_SecretVisibleWhenEarned(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "earnedsecret@test.com")
	user := &auth.User{ID: userID, Email: "earnedsecret@test.com", Name: "Earned Secret"}

	// Manually award a secret badge.
	now := time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := awardBadge(context.Background(), db, userID, "badge_christmas_spirit", 0, now); err != nil {
		t.Fatalf("awardBadge: %v", err)
	}

	handler := AvailableBadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges/available"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AvailableBadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	found := false
	for _, b := range resp {
		if b.Key == "badge_christmas_spirit" {
			found = true
			if !b.Earned {
				t.Errorf("earned secret badge should have earned=true")
			}
		}
	}
	if !found {
		t.Error("earned secret badge badge_christmas_spirit should appear in available list")
	}
}

// ---- Consistency badges ----

// TestConsistencyBadges_StreakQualifying verifies streak badges are awarded
// when the user has a current streak of at least 3 days.
func TestConsistencyBadges_StreakQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "streak3@test.com")

	// Insert streak record directly.
	_, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 3, 3, ?)
	`, userID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	wid := insertWorkout(t, db, userID, 1800, 3000, 0, 0, 0)
	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  3000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_streak_3"] {
		t.Errorf("expected badge_streak_3 for 3-day streak, got keys: %v", keys)
	}
	// 3-day streak should NOT trigger 7-day badge.
	if keys["badge_streak_7"] {
		t.Errorf("badge_streak_7 should NOT be awarded for only a 3-day streak")
	}
}

// TestConsistencyBadges_StreakNonQualifying verifies that a streak of 2 does
// not earn any streak badge.
func TestConsistencyBadges_StreakNonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "streak2@test.com")

	_, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 2, 2, ?)
	`, userID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	wid := insertWorkout(t, db, userID, 1800, 3000, 0, 0, 0)
	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  3000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		switch b.BadgeKey {
		case "badge_streak_3", "badge_streak_7", "badge_streak_14", "badge_streak_30":
			t.Errorf("unexpected streak badge %s for 2-day streak", b.BadgeKey)
		}
	}
}

// TestConsistencyBadges_WorkoutCount verifies workout count badges.
func TestConsistencyBadges_WorkoutCount(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "workoutcount@test.com")

	// Insert 24 workouts, then evaluate one more.
	for i := 0; i < 24; i++ {
		insertWorkout(t, db, userID, 1800, 3000, 0, 0, 0)
	}
	// This is the 25th workout.
	wid := insertWorkout(t, db, userID, 1800, 3000, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  3000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_25_workouts"] {
		t.Errorf("expected badge_25_workouts after 25 workouts, got keys: %v", keys)
	}
	if keys["badge_50_workouts"] {
		t.Errorf("badge_50_workouts should NOT be awarded after only 25 workouts")
	}
}

// ---- Fun badges ----

// TestFunBadges_EarlyBird verifies early bird badge is awarded for a workout
// that started before 6:00 AM.
func TestFunBadges_EarlyBird(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "earlybird@test.com")
	startedAt := time.Date(2025, 6, 1, 5, 30, 0, 0, time.UTC).Format(time.RFC3339)
	wid := insertWorkoutAt(t, db, userID, 1800, 5000, startedAt)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_early_bird"] {
		t.Errorf("expected badge_early_bird for 5:30 AM workout, got keys: %v", keys)
	}
	if keys["badge_night_owl"] {
		t.Errorf("badge_night_owl should NOT be awarded for 5:30 AM workout")
	}
}

// TestFunBadges_EarlyBird_NonQualifying verifies no early bird badge for a
// workout started after 6:00 AM.
func TestFunBadges_EarlyBird_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "notearlybird@test.com")
	startedAt := time.Date(2025, 6, 1, 7, 0, 0, 0, time.UTC).Format(time.RFC3339)
	wid := insertWorkoutAt(t, db, userID, 1800, 5000, startedAt)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_early_bird" {
			t.Errorf("badge_early_bird should NOT be awarded for 7:00 AM workout")
		}
	}
}

// TestFunBadges_CalorieCrusher verifies the calorie crusher badge.
func TestFunBadges_CalorieCrusher(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "calories@test.com")
	wid := insertWorkout(t, db, userID, 3600, 10000, 1000, 0, 360)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  10000,
		DurationSeconds: 3600,
		Calories:        1000,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_calorie_crusher"] {
		t.Errorf("expected badge_calorie_crusher for 1000 calories, got keys: %v", keys)
	}
}

// TestFunBadges_CalorieCrusher_NonQualifying verifies no calorie crusher badge
// when calories are below 1000.
func TestFunBadges_CalorieCrusher_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "fewcalories@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 500, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
		Calories:        500,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_calorie_crusher" {
			t.Errorf("badge_calorie_crusher should NOT be awarded for 500 calories")
		}
	}
}

// ---- Variety badges ----

// TestVarietyBadges_TwoSports verifies that having 2 distinct sports in
// workouts earns badge_2_sports.
func TestVarietyBadges_TwoSports(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "variety@test.com")

	// Insert a workout with sport = 'running'.
	_, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, duration_seconds, distance_meters, started_at, fit_file_hash, created_at)
		VALUES (?, 'running', 1800, 5000, ?, ?, ?)
	`, userID, time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("hash-r-%d", time.Now().UnixNano()),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert running workout: %v", err)
	}

	// Insert a workout with sport = 'cycling' — this is the second distinct sport.
	var res2 sql.Result
	res2, err = db.Exec(`
		INSERT INTO workouts (user_id, sport, duration_seconds, distance_meters, started_at, fit_file_hash, created_at)
		VALUES (?, 'cycling', 3600, 20000, ?, ?, ?)
	`, userID, time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("hash-c-%d", time.Now().UnixNano()),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert cycling workout: %v", err)
	}
	wid, err := res2.LastInsertId()
	if err != nil {
		t.Fatalf("insert cycling workout LastInsertId: %v", err)
	}

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  20000,
		DurationSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_2_sports"] {
		t.Errorf("expected badge_2_sports for 2 distinct sports, got keys: %v", keys)
	}
	if keys["badge_3_sports"] {
		t.Errorf("badge_3_sports should NOT be awarded for only 2 sports")
	}
}

// ---- Heart badges ----

// TestHeartBadges_BigHeart verifies the big heart badge (avg HR > 150, 60+ min).
func TestHeartBadges_BigHeart(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "bigheart@test.com")
	wid := insertWorkout(t, db, userID, 3700, 10000, 0, 0, 370)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  10000,
		DurationSeconds: 3700,
		AvgHeartRate:    155,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_big_heart"] {
		t.Errorf("expected badge_big_heart for avg HR 155 + 3700s, got keys: %v", keys)
	}
}

// TestHeartBadges_BigHeart_NonQualifying verifies no badge when HR or duration
// is too low.
func TestHeartBadges_BigHeart_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "smallheart@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800, // only 30 min
		AvgHeartRate:    160,  // high HR but short duration
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_big_heart" {
			t.Errorf("badge_big_heart should NOT be awarded for only 30 min, even with high HR")
		}
	}
}

// TestHeartBadges_HRWarrior verifies the HR warrior badge (max HR > 190).
func TestHeartBadges_HRWarrior(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "hrwarrior@test.com")
	wid := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
		MaxHeartRate:    195,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_hr_warrior"] {
		t.Errorf("expected badge_hr_warrior for max HR 195, got keys: %v", keys)
	}
}

// ---- Secret badges: special date triggers ----

// TestSecretBadges_ChristmasSpirit verifies badge_christmas_spirit is awarded
// for a workout on December 25.
func TestSecretBadges_ChristmasSpirit(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "christmas@test.com")
	startedAt := time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	wid := insertWorkoutAt(t, db, userID, 1800, 5000, startedAt)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	keys := make(map[string]bool)
	for _, b := range badges {
		keys[b.BadgeKey] = true
	}
	if !keys["badge_christmas_spirit"] {
		t.Errorf("expected badge_christmas_spirit for Dec 25 workout, got keys: %v", keys)
	}
}

// TestSecretBadges_ChristmasSpirit_NonQualifying verifies no Christmas badge
// on December 24.
func TestSecretBadges_ChristmasSpirit_NonQualifying(t *testing.T) {
	db := badgeTestDB(t)
	if err := SeedBadges(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID := insertUser(t, db, "christmas24@test.com")
	startedAt := time.Date(2025, 12, 24, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	wid := insertWorkoutAt(t, db, userID, 1800, 5000, startedAt)

	badges, err := EvaluateBadges(context.Background(), db, userID, WorkoutInput{
		ID:              wid,
		DistanceMeters:  5000,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("EvaluateBadges: %v", err)
	}

	for _, b := range badges {
		if b.BadgeKey == "badge_christmas_spirit" {
			t.Errorf("badge_christmas_spirit should NOT be awarded on Dec 24")
		}
	}
}
