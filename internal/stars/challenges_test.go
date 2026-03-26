package stars

import (
	"context"
	"database/sql"
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
