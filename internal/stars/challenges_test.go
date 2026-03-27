package stars

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// insertChallenge inserts a family_challenge row and returns its ID.
func insertChallenge(t *testing.T, db *sql.DB, creatorID int64, challengeType string, targetValue float64, startDate, endDate string, isActive bool) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	encTitle, err := encryption.EncryptField("Test Challenge")
	if err != nil {
		t.Fatalf("encrypt title: %v", err)
	}
	encDesc, err := encryption.EncryptField("Test Desc")
	if err != nil {
		t.Fatalf("encrypt description: %v", err)
	}
	isActiveInt := 0
	if isActive {
		isActiveInt = 1
	}
	res, err := db.Exec(`
		INSERT INTO family_challenges
		  (creator_id, title, description, challenge_type, target_value, star_reward,
		   start_date, end_date, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, creatorID, encTitle, encDesc, challengeType, targetValue, 5,
		startDate, endDate, isActiveInt, now, now)
	if err != nil {
		t.Fatalf("insert challenge: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// enrollParticipant adds a child to a challenge.
func enrollParticipant(t *testing.T, db *sql.DB, challengeID, childID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO challenge_participants (challenge_id, child_id, added_at)
		VALUES (?, ?, ?)
	`, challengeID, childID, now); err != nil {
		t.Fatalf("enroll participant: %v", err)
	}
}

func TestGetActiveChallengesEmpty(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "child@test.com")

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("expected 0 challenges, got %d", len(challenges))
	}
}

func TestGetActiveChallengesCustomType(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	cID := insertChallenge(t, db, parentID, "custom", 0, "2026-01-01", "2026-12-31", true)
	enrollParticipant(t, db, cID, childID)

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 1 {
		t.Fatalf("expected 1 challenge, got %d", len(challenges))
	}
	if challenges[0].Title != "Test Challenge" {
		t.Errorf("expected decrypted title, got %q", challenges[0].Title)
	}
	if challenges[0].CurrentValue != 0 {
		t.Errorf("custom challenge should have progress 0, got %v", challenges[0].CurrentValue)
	}
}

func TestGetActiveChallengesInactiveExcluded(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	cID := insertChallenge(t, db, parentID, "custom", 0, "", "", false) // inactive
	enrollParticipant(t, db, cID, childID)

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("inactive challenge should not be returned, got %d", len(challenges))
	}
}

func TestGetActiveChallengesWorkoutCount(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	cID := insertChallenge(t, db, parentID, "workout_count", 5, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Insert 2 workouts today.
	insertWorkout(t, db, childID, 1800, 5000, 300, 0, 0)
	insertWorkout(t, db, childID, 2400, 8000, 400, 0, 0)

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 1 {
		t.Fatalf("expected 1, got %d", len(challenges))
	}
	if challenges[0].CurrentValue != 2 {
		t.Errorf("expected workout count 2, got %v", challenges[0].CurrentValue)
	}
	if challenges[0].Completed {
		t.Error("should not be completed with 2/5 workouts")
	}
}

func TestGetActiveChallengesDistanceProgress(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	// Target: 10km
	cID := insertChallenge(t, db, parentID, "distance", 10.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Insert workout with 15000m = 15km.
	insertWorkout(t, db, childID, 3600, 15000, 500, 0, 0)

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 1 {
		t.Fatalf("expected 1, got %d", len(challenges))
	}
	if challenges[0].CurrentValue != 15.0 {
		t.Errorf("expected 15.0 km progress, got %v", challenges[0].CurrentValue)
	}
	if !challenges[0].Completed {
		t.Error("15km >= 10km target should be completed")
	}
}

func TestGetActiveChallengesStreakProgress(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	cID := insertChallenge(t, db, parentID, "streak", 7.0, "2026-01-01", "2026-12-31", true)
	enrollParticipant(t, db, cID, childID)

	// Seed a streak of 5.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 5, 5, ?)
	`, childID, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("seed streak: %v", err)
	}

	challenges, err := GetActiveChallenges(db, childID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 1 {
		t.Fatalf("expected 1, got %d", len(challenges))
	}
	if challenges[0].CurrentValue != 5 {
		t.Errorf("expected streak 5, got %v", challenges[0].CurrentValue)
	}
	if challenges[0].Completed {
		t.Error("5 < 7 should not be completed")
	}
}

func TestGetActiveChallengesNotParticipant(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	otherID := insertUser(t, db, "other@test.com")
	linkChild(t, db, parentID, childID)

	cID := insertChallenge(t, db, parentID, "custom", 0, "", "", true)
	enrollParticipant(t, db, cID, childID) // only childID enrolled

	challenges, err := GetActiveChallenges(db, otherID)
	if err != nil {
		t.Fatalf("GetActiveChallenges: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("otherID is not a participant, expected 0, got %d", len(challenges))
	}
}

// --- UpdateChallengeProgress tests ---

// getCompletedAt returns the completed_at value for a participant row.
func getCompletedAt(t *testing.T, db *sql.DB, challengeID, childID int64) string {
	t.Helper()
	var v string
	err := db.QueryRow(`SELECT completed_at FROM challenge_participants WHERE challenge_id = ? AND child_id = ?`, challengeID, childID).Scan(&v)
	if err != nil {
		t.Fatalf("getCompletedAt: %v", err)
	}
	return v
}

func TestUpdateChallengeProgress_DistanceCompletion(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	// Target: 10 km
	cID := insertChallenge(t, db, parentID, "distance", 10.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Workout with 12 000 m = 12 km — exceeds target.
	wID := insertWorkout(t, db, childID, 3600, 12000, 0, 0, 0)

	w := WorkoutInput{ID: wID, DistanceMeters: 12000, DurationSeconds: 3600}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress: %v", err)
	}

	if getCompletedAt(t, db, cID, childID) == "" {
		t.Error("expected completed_at to be set after distance target reached")
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 5 { // star_reward = 5 (set by insertChallenge)
		t.Errorf("expected 5 stars earned, got %d", earned)
	}
}

func TestUpdateChallengeProgress_NotYetComplete(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	cID := insertChallenge(t, db, parentID, "distance", 20.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Only 5 km — not enough.
	wID := insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)

	w := WorkoutInput{ID: wID, DistanceMeters: 5000, DurationSeconds: 1800}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress: %v", err)
	}

	if getCompletedAt(t, db, cID, childID) != "" {
		t.Error("expected completed_at to be empty when target not reached")
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 0 {
		t.Errorf("expected 0 stars, got %d", earned)
	}
}

func TestUpdateChallengeProgress_NoDoubleAward(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	cID := insertChallenge(t, db, parentID, "distance", 5.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	wID := insertWorkout(t, db, childID, 3600, 10000, 0, 0, 0)
	w := WorkoutInput{ID: wID, DistanceMeters: 10000, DurationSeconds: 3600}

	// First call — should award 5 stars.
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("first UpdateChallengeProgress: %v", err)
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 5 {
		t.Errorf("after first call: expected 5 stars, got %d", earned)
	}

	// Second call — already completed, should not award again.
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("second UpdateChallengeProgress: %v", err)
	}
	earned, _, _ = getBalance(t, db, childID)
	if earned != 5 {
		t.Errorf("after second call: expected still 5 stars, got %d (double award!)", earned)
	}
}

func TestUpdateChallengeProgress_WorkoutCount(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	cID := insertChallenge(t, db, parentID, "workout_count", 2.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// First workout — target not yet reached.
	wID1 := insertWorkout(t, db, childID, 1800, 3000, 0, 0, 0)
	w1 := WorkoutInput{ID: wID1, DurationSeconds: 1800, DistanceMeters: 3000}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w1); err != nil {
		t.Fatalf("UpdateChallengeProgress (1): %v", err)
	}
	if getCompletedAt(t, db, cID, childID) != "" {
		t.Error("should not complete after 1 of 2 workouts")
	}

	// Second workout — should trigger completion.
	wID2 := insertWorkout(t, db, childID, 1800, 3000, 0, 0, 0)
	w2 := WorkoutInput{ID: wID2, DurationSeconds: 1800, DistanceMeters: 3000}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w2); err != nil {
		t.Fatalf("UpdateChallengeProgress (2): %v", err)
	}
	if getCompletedAt(t, db, cID, childID) == "" {
		t.Error("expected completed_at set after 2nd workout meets target")
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 5 {
		t.Errorf("expected 5 stars after completion, got %d", earned)
	}
}

func TestUpdateChallengeProgress_StreakType(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	cID := insertChallenge(t, db, parentID, "streak", 7.0, "2026-01-01", "2026-12-31", true)
	enrollParticipant(t, db, cID, childID)

	// Seed a 7-day streak.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 7, 7, ?)
	`, childID, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("seed streak: %v", err)
	}

	wID := insertWorkout(t, db, childID, 1800, 3000, 0, 0, 0)
	w := WorkoutInput{ID: wID, DurationSeconds: 1800, DistanceMeters: 3000}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress: %v", err)
	}

	if getCompletedAt(t, db, cID, childID) == "" {
		t.Error("expected completed_at set when streak meets target")
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 5 {
		t.Errorf("expected 5 stars, got %d", earned)
	}
}

func TestUpdateChallengeProgress_DurationCompletion(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	// Target: 60 minutes
	cID := insertChallenge(t, db, parentID, "duration", 60.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Workout with 4500 seconds = 75 minutes — exceeds target.
	wID := insertWorkout(t, db, childID, 4500, 0, 0, 0, 0)
	w := WorkoutInput{ID: wID, DurationSeconds: 4500, DistanceMeters: 0}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress: %v", err)
	}

	if getCompletedAt(t, db, cID, childID) == "" {
		t.Error("expected completed_at to be set after duration target reached")
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 5 {
		t.Errorf("expected 5 stars earned, got %d", earned)
	}
}

func TestUpdateChallengeProgress_DurationNotYetComplete(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	today := time.Now().UTC().Format("2006-01-02")
	// Target: 120 minutes
	cID := insertChallenge(t, db, parentID, "duration", 120.0, today, today, true)
	enrollParticipant(t, db, cID, childID)

	// Workout with 1800 seconds = 30 minutes — not enough.
	wID := insertWorkout(t, db, childID, 1800, 0, 0, 0, 0)
	w := WorkoutInput{ID: wID, DurationSeconds: 1800, DistanceMeters: 0}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress: %v", err)
	}

	if getCompletedAt(t, db, cID, childID) != "" {
		t.Error("expected completed_at to be empty when duration target not reached")
	}
}

func TestUpdateChallengeProgress_NoParticipants(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "child@test.com")

	w := WorkoutInput{ID: 1, DurationSeconds: 3600, DistanceMeters: 10000}
	if err := UpdateChallengeProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateChallengeProgress with no challenges: %v", err)
	}
}

func TestGenerateWeeklyChallenges_NoChildren(t *testing.T) {
	db := setupTestDB(t)
	// With no children linked, generation should be a no-op.
	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("GenerateWeeklyChallenges: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE is_system = 1`).Scan(&count); err != nil {
		t.Fatalf("count system challenges: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 system challenges, got %d", count)
	}
}

func TestGenerateWeeklyChallenges_CreatesExpectedChallenges(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("GenerateWeeklyChallenges: %v", err)
	}

	// Expect 4 system challenges: 3 shared + 1 per-child "Beat Last Week".
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE is_system = 1`).Scan(&count); err != nil {
		t.Fatalf("count system challenges: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 system challenges, got %d", count)
	}

	// Each child should be enrolled in all 4 challenges.
	var participants int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM challenge_participants cp
		JOIN family_challenges fc ON fc.id = cp.challenge_id
		WHERE fc.is_system = 1 AND cp.child_id = ?
	`, childID).Scan(&participants); err != nil {
		t.Fatalf("count participants: %v", err)
	}
	if participants != 4 {
		t.Errorf("expected child enrolled in 4 challenges, got %d", participants)
	}
}

func TestGenerateWeeklyChallenges_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Call twice; second call should be a no-op.
	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("first GenerateWeeklyChallenges: %v", err)
	}
	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("second GenerateWeeklyChallenges: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE is_system = 1`).Scan(&count); err != nil {
		t.Fatalf("count system challenges: %v", err)
	}
	if count != 4 {
		t.Errorf("expected exactly 4 after idempotent calls, got %d", count)
	}
}

func TestGenerateWeeklyChallenges_BeatLastWeekTarget(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Insert a previous-week workout: 10 km.
	year, week := time.Now().UTC().ISOWeek()
	prevMon := firstDayOfISOWeek(year, week).AddDate(0, 0, -7)
	prevWeekMid := prevMon.AddDate(0, 0, 2).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO workouts (user_id, duration_seconds, distance_meters, sport, started_at, fit_file_hash, created_at)
		VALUES (?, 3600, 10000, 'running', ?, 'hash-prev', ?)
	`, childID, prevWeekMid, prevWeekMid); err != nil {
		t.Fatalf("insert prev workout: %v", err)
	}

	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("GenerateWeeklyChallenges: %v", err)
	}

	// "Beat Last Week" target should be 10km * 1.1 = 11.0 km.
	var target float64
	if err := db.QueryRow(`
		SELECT target_value FROM family_challenges
		WHERE is_system = 1 AND challenge_type = 'distance'
		  AND creator_id = 0
		LIMIT 1
	`).Scan(&target); err != nil {
		t.Fatalf("get Beat Last Week target: %v", err)
	}
	if target != 11.0 {
		t.Errorf("expected target 11.0 km, got %.2f", target)
	}
}

func TestGenerateWeeklyChallenges_BeatLastWeekMinTarget(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// No previous workouts — target should default to 1.0 km.
	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("GenerateWeeklyChallenges: %v", err)
	}

	var target float64
	if err := db.QueryRow(`
		SELECT target_value FROM family_challenges
		WHERE is_system = 1 AND challenge_type = 'distance'
		LIMIT 1
	`).Scan(&target); err != nil {
		t.Fatalf("get Beat Last Week target: %v", err)
	}
	if target != 1.0 {
		t.Errorf("expected default target 1.0 km, got %.2f", target)
	}
}

func TestGenerateWeeklyChallenges_MultipleChildren(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	child1ID := insertUser(t, db, "child1@test.com")
	child2ID := insertUser(t, db, "child2@test.com")
	linkChild(t, db, parentID, child1ID)
	linkChild(t, db, parentID, child2ID)

	if err := GenerateWeeklyChallenges(context.Background(), db); err != nil {
		t.Fatalf("GenerateWeeklyChallenges: %v", err)
	}

	// 3 shared + 1 per child = 3 + 2 = 5 system challenges total.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE is_system = 1`).Scan(&count); err != nil {
		t.Fatalf("count system challenges: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 system challenges for 2 children, got %d", count)
	}

	// Each child should be enrolled in 4 challenges (3 shared + 1 own Beat Last Week).
	for _, childID := range []int64{child1ID, child2ID} {
		var enrolled int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM challenge_participants cp
			JOIN family_challenges fc ON fc.id = cp.challenge_id
			WHERE fc.is_system = 1 AND cp.child_id = ?
		`, childID).Scan(&enrolled); err != nil {
			t.Fatalf("count enrolled for child %d: %v", childID, err)
		}
		if enrolled != 4 {
			t.Errorf("expected child %d enrolled in 4 challenges, got %d", childID, enrolled)
		}
	}
}

// --- Beat My Parent: age scaling, comparison, and bonus tests ---

// TestAgeScalingMath_TableDriven verifies that ChildDistanceScaled equals
// ChildDistanceRaw * (parent_age / child_age) for a range of age combinations.
// Birthdays are set as Jan 1 so the full age is reached by the January anchor date.
func TestAgeScalingMath_TableDriven(t *testing.T) {
	// Monday of ISO week 2025-W02, stable anchor to avoid week-boundary flakiness.
	anchor := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		childAge   int
		parentAge  int
		rawMeters  float64
		wantScaled float64
	}{
		{"child10_parent40_scale4x", 10, 40, 3000, 12000},
		{"child8_parent32_scale4x", 8, 32, 2500, 10000},
		{"child12_parent36_scale3x", 12, 36, 5000, 15000},
		{"same_age_scale1x", 35, 35, 7000, 7000},
		{"child_older_parent_younger_scale0_5x", 40, 20, 10000, 5000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			parentID := insertUser(t, db, "parent@scale.com")
			childID := insertUser(t, db, "child@scale.com")
			linkChild(t, db, parentID, childID)

			childBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.childAge)
			parentBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.parentAge)
			if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
				t.Fatalf("set child birthday: %v", err)
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
				t.Fatalf("set parent birthday: %v", err)
			}

			insertWorkoutAt(t, db, childID, 3600, tc.rawMeters, anchor.Format(time.RFC3339))
			// Parent needs a nominal workout so distance comparison doesn't interfere.
			insertWorkoutAt(t, db, parentID, 3600, 1, anchor.Format(time.RFC3339))

			status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
			if err != nil {
				t.Fatalf("GetBeatMyParentStatus: %v", err)
			}
			if status.ChildDistanceRaw != tc.rawMeters {
				t.Errorf("raw: got %.0f, want %.0f", status.ChildDistanceRaw, tc.rawMeters)
			}
			if status.ChildDistanceScaled != tc.wantScaled {
				t.Errorf("scaled: got %.0f, want %.0f (raw=%.0f, childAge=%d, parentAge=%d)",
					status.ChildDistanceScaled, tc.wantScaled, tc.rawMeters, tc.childAge, tc.parentAge)
			}
		})
	}
}

// TestBeatParentComparison_TableDriven verifies IsBeatingParent true/false for a
// range of (scaled child distance, parent distance) combinations, including cases
// where age scaling changes the outcome.
func TestBeatParentComparison_TableDriven(t *testing.T) {
	anchor := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		childMeters  float64
		parentMeters float64
		// 0 = no birthday set → ageScalingFactor defaults to 1.0
		childAge    int
		parentAge   int
		wantBeating bool
	}{
		{"child_ahead_no_scaling", 15000, 10000, 0, 0, true},
		{"parent_ahead_no_scaling", 5000, 20000, 0, 0, false},
		{"tied_no_scaling", 10000, 10000, 0, 0, false}, // strict greater-than required
		{"child_wins_via_age_scaling", 3000, 10000, 10, 40, true},   // 3000 * 4 = 12000 > 10000
		{"child_loses_despite_scaling", 2000, 10000, 8, 32, false},  // 2000 * 4 = 8000 < 10000
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			parentID := insertUser(t, db, "parent@cmp.com")
			childID := insertUser(t, db, "child@cmp.com")
			linkChild(t, db, parentID, childID)

			if tc.childAge > 0 {
				childBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.childAge)
				if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
					t.Fatalf("set child birthday: %v", err)
				}
			}
			if tc.parentAge > 0 {
				parentBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.parentAge)
				if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
					t.Fatalf("set parent birthday: %v", err)
				}
			}

			if tc.childMeters > 0 {
				insertWorkoutAt(t, db, childID, 3600, tc.childMeters, anchor.Format(time.RFC3339))
			}
			if tc.parentMeters > 0 {
				insertWorkoutAt(t, db, parentID, 3600, tc.parentMeters, anchor.Format(time.RFC3339))
			}

			status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
			if err != nil {
				t.Fatalf("GetBeatMyParentStatus: %v", err)
			}
			if status.IsBeatingParent != tc.wantBeating {
				t.Errorf("IsBeatingParent: got %v, want %v (child=%.0fm, parent=%.0fm, childAge=%d, parentAge=%d, scaled=%.0fm)",
					status.IsBeatingParent, tc.wantBeating,
					tc.childMeters, tc.parentMeters, tc.childAge, tc.parentAge,
					status.ChildDistanceScaled)
			}
		})
	}
}

// TestBeatParentBonus_CreditedExactlyOnce verifies that the 25-star beat-parent
// bonus is recorded exactly once when EvaluateWeeklyBonuses is called twice for
// the same (child, week). The idempotency guard in weekly_bonus_evaluations must
// prevent a second award.
func TestBeatParentBonus_CreditedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)
	anchor := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	parentID := insertUser(t, db, "parent@bonus.com")
	childID := insertUser(t, db, "child@bonus.com")
	linkChild(t, db, parentID, childID)

	// Child runs 20 km; parent runs 5 km. No age scaling (no birthdays set).
	insertWorkoutAt(t, db, childID, 7200, 20000, anchor.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 1800, 5000, anchor.Format(time.RFC3339))

	// First evaluation — beat-parent bonus must be awarded.
	awards1, err := EvaluateWeeklyBonuses(ctx, db, childID, anchor)
	if err != nil {
		t.Fatalf("first EvaluateWeeklyBonuses: %v", err)
	}

	expectedReason := fmt.Sprintf("beat_parent_%s", weekKey(anchor))
	found := false
	for _, a := range awards1 {
		if a.Reason == expectedReason {
			found = true
			if a.Amount != 25 {
				t.Errorf("beat-parent bonus: got %d stars, want 25", a.Amount)
			}
		}
	}
	if !found {
		t.Fatalf("beat-parent bonus not found in first evaluation; got awards: %v", awardReasons(awards1))
	}

	earned1, _, _ := getBalance(t, db, childID)

	// Second evaluation for the same week — idempotency guard must fire and return nil.
	awards2, err := EvaluateWeeklyBonuses(ctx, db, childID, anchor)
	if err != nil {
		t.Fatalf("second EvaluateWeeklyBonuses: %v", err)
	}
	if awards2 != nil {
		t.Errorf("expected no awards on second call (idempotent); got: %v", awardReasons(awards2))
	}

	earned2, _, _ := getBalance(t, db, childID)
	if earned2 != earned1 {
		t.Errorf("balance changed after second evaluation: was %d, now %d (double award!)", earned1, earned2)
	}

	// The beat-parent transaction must appear exactly once in the DB.
	var txCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = ?`,
		childID, expectedReason).Scan(&txCount); err != nil {
		t.Fatalf("count beat-parent transactions: %v", err)
	}
	if txCount != 1 {
		t.Errorf("expected exactly 1 beat-parent transaction in DB, got %d", txCount)
	}
}

// TestBeatParent_ParentZeroWorkouts verifies that when a parent has no workouts
// for the week, GetBeatMyParentStatus does not panic or error (no divide-by-zero
// in the age scaling factor) and returns IsBeatingParent=false when neither
// party has logged any distance.
func TestBeatParent_ParentZeroWorkouts(t *testing.T) {
	anchor := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)
	db := setupTestDB(t)

	parentID := insertUser(t, db, "parent@zero.com")
	childID := insertUser(t, db, "child@zero.com")
	linkChild(t, db, parentID, childID)

	// Neither user has workouts and no birthdays are configured.
	// ageScalingFactor must return 1.0 (not divide by child age = 0).
	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus with zero workouts: %v", err)
	}
	if status.ChildDistanceRaw != 0 {
		t.Errorf("expected child raw=0, got %v", status.ChildDistanceRaw)
	}
	if status.ChildDistanceScaled != 0 {
		t.Errorf("expected child scaled=0, got %v", status.ChildDistanceScaled)
	}
	if status.ParentDistance != 0 {
		t.Errorf("expected parent=0, got %v", status.ParentDistance)
	}
	if status.IsBeatingParent {
		t.Error("expected IsBeatingParent=false when both have zero distance")
	}
}
