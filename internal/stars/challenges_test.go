package stars

import (
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
