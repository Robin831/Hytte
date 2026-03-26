package family

import (
	"errors"
	"testing"
)

func TestCreateChallenge(t *testing.T) {
	db := setupTestDB(t)

	c, err := CreateChallenge(db, 1, "Run 10km", "Complete 10km total", "distance", 10.0, 5, "2026-01-01", "2026-01-31", true)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	if c.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if c.Title != "Run 10km" {
		t.Errorf("expected title 'Run 10km', got %q", c.Title)
	}
	if c.ChallengeType != "distance" {
		t.Errorf("expected type 'distance', got %q", c.ChallengeType)
	}
	if c.TargetValue != 10.0 {
		t.Errorf("expected target 10.0, got %v", c.TargetValue)
	}
	if c.StarReward != 5 {
		t.Errorf("expected star_reward 5, got %d", c.StarReward)
	}
	if !c.IsActive {
		t.Error("expected challenge to be active")
	}
	if c.CreatorID != 1 {
		t.Errorf("expected creator_id 1, got %d", c.CreatorID)
	}
}

func TestCreateChallengeInvalidType(t *testing.T) {
	db := setupTestDB(t)

	_, err := CreateChallenge(db, 1, "Bad", "", "invalid_type", 1.0, 0, "", "", true)
	if !errors.Is(err, ErrInvalidChallengeType) {
		t.Errorf("expected ErrInvalidChallengeType, got %v", err)
	}
}

func TestCreateChallengeNegativeReward(t *testing.T) {
	db := setupTestDB(t)

	_, err := CreateChallenge(db, 1, "Bad", "", "custom", 1.0, -1, "", "", true)
	if !errors.Is(err, ErrNegativeStarReward) {
		t.Errorf("expected ErrNegativeStarReward, got %v", err)
	}
}

func TestCreateChallengeInvalidDateRange(t *testing.T) {
	db := setupTestDB(t)

	_, err := CreateChallenge(db, 1, "Bad", "", "custom", 1.0, 0, "2026-01-31", "2026-01-01", true)
	if !errors.Is(err, ErrInvalidDateRange) {
		t.Errorf("expected ErrInvalidDateRange, got %v", err)
	}
}

func TestGetChallenges(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateChallenge(db, 1, "Challenge A", "", "custom", 0, 0, "", "", true); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := CreateChallenge(db, 1, "Challenge B", "", "streak", 7.0, 10, "2026-01-01", "2026-01-31", false); err != nil {
		t.Fatalf("create B: %v", err)
	}

	challenges, err := GetChallenges(db, 1)
	if err != nil {
		t.Fatalf("GetChallenges: %v", err)
	}
	if len(challenges) != 2 {
		t.Fatalf("expected 2 challenges, got %d", len(challenges))
	}
}

func TestGetChallengesEmpty(t *testing.T) {
	db := setupTestDB(t)

	challenges, err := GetChallenges(db, 1)
	if err != nil {
		t.Fatalf("GetChallenges: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("expected 0 challenges, got %d", len(challenges))
	}
}

func TestGetChallengesOtherCreatorNotReturned(t *testing.T) {
	db := setupTestDB(t)

	// User 2 creates a challenge.
	if _, err := CreateChallenge(db, 2, "Other", "", "custom", 0, 0, "", "", true); err != nil {
		t.Fatalf("create: %v", err)
	}

	challenges, err := GetChallenges(db, 1)
	if err != nil {
		t.Fatalf("GetChallenges: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("expected 0 challenges for user 1, got %d", len(challenges))
	}
}

func TestUpdateChallenge(t *testing.T) {
	db := setupTestDB(t)

	orig, err := CreateChallenge(db, 1, "Original", "Desc", "custom", 5.0, 3, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := UpdateChallenge(db, orig.ID, 1, "Updated", "New desc", "streak", 7.0, 10, "2026-01-01", "2026-01-31", false)
	if err != nil {
		t.Fatalf("UpdateChallenge: %v", err)
	}
	if updated.Title != "Updated" {
		t.Errorf("expected title 'Updated', got %q", updated.Title)
	}
	if updated.ChallengeType != "streak" {
		t.Errorf("expected type 'streak', got %q", updated.ChallengeType)
	}
	if updated.IsActive {
		t.Error("expected challenge to be inactive after update")
	}
	if updated.CreatedAt != orig.CreatedAt {
		t.Errorf("created_at changed: %q → %q", orig.CreatedAt, updated.CreatedAt)
	}
}

func TestUpdateChallengeNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := UpdateChallenge(db, 9999, 1, "X", "", "custom", 0, 0, "", "", true)
	if !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestUpdateChallengeWrongCreator(t *testing.T) {
	db := setupTestDB(t)

	c, err := CreateChallenge(db, 1, "Mine", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// User 2 tries to update user 1's challenge.
	_, err = UpdateChallenge(db, c.ID, 2, "Stolen", "", "custom", 0, 0, "", "", true)
	if !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestDeleteChallenge(t *testing.T) {
	db := setupTestDB(t)

	c, err := CreateChallenge(db, 1, "ToDelete", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := DeleteChallenge(db, c.ID, 1); err != nil {
		t.Fatalf("DeleteChallenge: %v", err)
	}

	challenges, err := GetChallenges(db, 1)
	if err != nil {
		t.Fatalf("GetChallenges after delete: %v", err)
	}
	if len(challenges) != 0 {
		t.Errorf("expected 0 challenges after delete, got %d", len(challenges))
	}
}

func TestDeleteChallengeNotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteChallenge(db, 9999, 1)
	if !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestAddParticipant(t *testing.T) {
	db := setupTestDB(t)

	// Link child (user 2) to parent (user 1).
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (1, 2, 'Kid', '⭐', '2026-01-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert link: %v", err)
	}

	c, err := CreateChallenge(db, 1, "Team Run", "", "distance", 50.0, 20, "2026-01-01", "2026-01-31", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := AddParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	// Adding again is idempotent (INSERT OR IGNORE).
	if err := AddParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("AddParticipant second time: %v", err)
	}
}

func TestAddParticipantChildNotLinked(t *testing.T) {
	db := setupTestDB(t)

	c, err := CreateChallenge(db, 1, "Test", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// No family_link between user 1 and user 2.
	err = AddParticipant(db, c.ID, 1, 2)
	if !errors.Is(err, ErrChildNotLinked) {
		t.Errorf("expected ErrChildNotLinked, got %v", err)
	}
}

func TestAddParticipantChallengeNotFound(t *testing.T) {
	db := setupTestDB(t)

	err := AddParticipant(db, 9999, 1, 2)
	if !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestRemoveParticipant(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (1, 2, 'Kid', '⭐', '2026-01-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert link: %v", err)
	}

	c, err := CreateChallenge(db, 1, "Test", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := AddParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	if err := RemoveParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
}

func TestRemoveParticipantNotFound(t *testing.T) {
	db := setupTestDB(t)

	c, err := CreateChallenge(db, 1, "Test", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = RemoveParticipant(db, c.ID, 1, 2)
	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("expected ErrParticipantNotFound, got %v", err)
	}
}

func TestRemoveParticipantChallengeNotFound(t *testing.T) {
	db := setupTestDB(t)

	err := RemoveParticipant(db, 9999, 1, 2)
	if !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestChallengeEncryptionRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	title := "Secret Challenge"
	desc := "This is confidential"
	c, err := CreateChallenge(db, 1, title, desc, "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	// Verify the DB stores ciphertext, not plaintext.
	var rawTitle, rawDesc string
	if err := db.QueryRow(`SELECT title, description FROM family_challenges WHERE id = ?`, c.ID).Scan(&rawTitle, &rawDesc); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if rawTitle == title {
		t.Error("title should be encrypted in DB, not plaintext")
	}
	if rawDesc == desc {
		t.Error("description should be encrypted in DB, not plaintext")
	}

	// Verify retrieval decrypts correctly.
	challenges, err := GetChallenges(db, 1)
	if err != nil {
		t.Fatalf("GetChallenges: %v", err)
	}
	if len(challenges) != 1 {
		t.Fatalf("expected 1, got %d", len(challenges))
	}
	if challenges[0].Title != title {
		t.Errorf("expected decrypted title %q, got %q", title, challenges[0].Title)
	}
	if challenges[0].Description != desc {
		t.Errorf("expected decrypted desc %q, got %q", desc, challenges[0].Description)
	}
}
