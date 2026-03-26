package family

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Valid challenge_type values.
var validChallengeTypes = map[string]bool{
	"distance":      true,
	"duration":      true,
	"workout_count": true,
	"streak":        true,
	"custom":        true,
}

// Sentinel errors for challenge operations.
var (
	ErrChallengeNotFound    = errors.New("challenge not found")
	ErrInvalidChallengeType = errors.New("invalid challenge type")
	ErrInvalidDateRange     = errors.New("end_date must be after start_date")
	ErrNegativeStarReward   = errors.New("star_reward must be >= 0")
	ErrChildNotLinked       = errors.New("child is not linked to this parent")
	ErrParticipantNotFound  = errors.New("participant not found")
)

// Challenge is a parent-created challenge that children can participate in.
type Challenge struct {
	ID            int64   `json:"id"`
	CreatorID     int64   `json:"creator_id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	ChallengeType string  `json:"challenge_type"`
	TargetValue   float64 `json:"target_value"`
	StarReward    int     `json:"star_reward"`
	StartDate     string  `json:"start_date"`
	EndDate       string  `json:"end_date"`
	IsActive      bool    `json:"is_active"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// validateChallenge checks that challenge fields satisfy business rules.
func validateChallenge(challengeType string, starReward int, startDate, endDate string) error {
	if !validChallengeTypes[challengeType] {
		return fmt.Errorf("%w: %q", ErrInvalidChallengeType, challengeType)
	}
	if starReward < 0 {
		return ErrNegativeStarReward
	}

	var (
		startTime time.Time
		endTime   time.Time
		err       error
	)

	if startDate != "" {
		startTime, err = time.Parse("2006-01-02", startDate)
		if err != nil {
			return ErrInvalidDateRange
		}
	}
	if endDate != "" {
		endTime, err = time.Parse("2006-01-02", endDate)
		if err != nil {
			return ErrInvalidDateRange
		}
	}
	if !startTime.IsZero() && !endTime.IsZero() && !endTime.After(startTime) {
		return ErrInvalidDateRange
	}
	return nil
}

// CreateChallenge creates a new challenge owned by creatorID.
// Title and description are encrypted at rest.
func CreateChallenge(db *sql.DB, creatorID int64, title, description, challengeType string, targetValue float64, starReward int, startDate, endDate string, isActive bool) (*Challenge, error) {
	if err := validateChallenge(challengeType, starReward, startDate, endDate); err != nil {
		return nil, err
	}

	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	isActiveInt := 0
	if isActive {
		isActiveInt = 1
	}

	res, err := db.Exec(`
		INSERT INTO family_challenges
		  (creator_id, title, description, challenge_type, target_value, star_reward,
		   start_date, end_date, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, creatorID, encTitle, encDesc, challengeType, targetValue, starReward,
		startDate, endDate, isActiveInt, now, now)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Challenge{
		ID:            id,
		CreatorID:     creatorID,
		Title:         title,
		Description:   description,
		ChallengeType: challengeType,
		TargetValue:   targetValue,
		StarReward:    starReward,
		StartDate:     startDate,
		EndDate:       endDate,
		IsActive:      isActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// GetChallenges returns all challenges created by creatorID, newest first.
func GetChallenges(db *sql.DB, creatorID int64) ([]Challenge, error) {
	rows, err := db.Query(`
		SELECT id, creator_id, title, description, challenge_type, target_value, star_reward,
		       start_date, end_date, is_active, created_at, updated_at
		FROM family_challenges
		WHERE creator_id = ?
		ORDER BY created_at DESC
	`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var challenges []Challenge
	for rows.Next() {
		c, err := scanChallengeRow(rows)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, *c)
	}
	return challenges, rows.Err()
}

// UpdateChallenge updates a challenge by ID, verifying it belongs to creatorID.
func UpdateChallenge(db *sql.DB, id, creatorID int64, title, description, challengeType string, targetValue float64, starReward int, startDate, endDate string, isActive bool) (*Challenge, error) {
	if err := validateChallenge(challengeType, starReward, startDate, endDate); err != nil {
		return nil, err
	}

	var createdAt string
	err := db.QueryRow(`SELECT created_at FROM family_challenges WHERE id = ? AND creator_id = ?`, id, creatorID).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, err
	}

	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	isActiveInt := 0
	if isActive {
		isActiveInt = 1
	}

	res, err := db.Exec(`
		UPDATE family_challenges
		SET title = ?, description = ?, challenge_type = ?, target_value = ?,
		    star_reward = ?, start_date = ?, end_date = ?, is_active = ?, updated_at = ?
		WHERE id = ? AND creator_id = ?
	`, encTitle, encDesc, challengeType, targetValue, starReward, startDate, endDate, isActiveInt, now, id, creatorID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrChallengeNotFound
	}

	return &Challenge{
		ID:            id,
		CreatorID:     creatorID,
		Title:         title,
		Description:   description,
		ChallengeType: challengeType,
		TargetValue:   targetValue,
		StarReward:    starReward,
		StartDate:     startDate,
		EndDate:       endDate,
		IsActive:      isActive,
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}, nil
}

// DeleteChallenge permanently removes a challenge by ID, verifying it belongs to creatorID.
func DeleteChallenge(db *sql.DB, id, creatorID int64) error {
	res, err := db.Exec(`DELETE FROM family_challenges WHERE id = ? AND creator_id = ?`, id, creatorID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChallengeNotFound
	}
	return nil
}

// AddParticipant enrolls a child in a challenge. It verifies that the challenge
// belongs to creatorID and that childID is linked to creatorID as a parent.
func AddParticipant(db *sql.DB, challengeID, creatorID, childID int64) error {
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE id = ? AND creator_id = ?`, challengeID, creatorID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		return ErrChallengeNotFound
	}

	// Verify the child is linked to the creator via family_links.
	var linked int
	err = db.QueryRow(`SELECT COUNT(*) FROM family_links WHERE parent_id = ? AND child_id = ?`, creatorID, childID).Scan(&linked)
	if err != nil {
		return err
	}
	if linked == 0 {
		return ErrChildNotLinked
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT OR IGNORE INTO challenge_participants (challenge_id, child_id, added_at)
		VALUES (?, ?, ?)
	`, challengeID, childID, now)
	return err
}

// RemoveParticipant removes a child from a challenge, verifying the challenge
// belongs to creatorID.
func RemoveParticipant(db *sql.DB, challengeID, creatorID, childID int64) error {
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE id = ? AND creator_id = ?`, challengeID, creatorID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		return ErrChallengeNotFound
	}

	res, err := db.Exec(`
		DELETE FROM challenge_participants WHERE challenge_id = ? AND child_id = ?
	`, challengeID, childID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrParticipantNotFound
	}
	return nil
}

// ChallengeParticipant holds a child's participation record for a challenge.
type ChallengeParticipant struct {
	ChildID     int64  `json:"child_id"`
	Nickname    string `json:"nickname"`
	AvatarEmoji string `json:"avatar_emoji"`
	AddedAt     string `json:"added_at"`
	CompletedAt string `json:"completed_at"`
}

// GetChallengeParticipants returns all participants enrolled in a challenge
// owned by creatorID, joined with their family link nickname and avatar.
func GetChallengeParticipants(db *sql.DB, challengeID, creatorID int64) ([]ChallengeParticipant, error) {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_challenges WHERE id = ? AND creator_id = ?`, challengeID, creatorID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrChallengeNotFound
	}

	rows, err := db.Query(`
		SELECT cp.child_id,
		       COALESCE(fl.nickname, ''),
		       COALESCE(fl.avatar_emoji, '⭐'),
		       cp.added_at,
		       cp.completed_at
		FROM challenge_participants cp
		LEFT JOIN family_links fl ON fl.child_id = cp.child_id AND fl.parent_id = ?
		WHERE cp.challenge_id = ?
		ORDER BY COALESCE(fl.nickname, '') ASC
	`, creatorID, challengeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	participants := []ChallengeParticipant{}
	for rows.Next() {
		var p ChallengeParticipant
		var encNickname string
		var completedAt sql.NullString
		if err := rows.Scan(&p.ChildID, &encNickname, &p.AvatarEmoji, &p.AddedAt, &completedAt); err != nil {
			return nil, err
		}
		p.Nickname = encryption.DecryptLenient(encNickname)
		if completedAt.Valid {
			p.CompletedAt = completedAt.String
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

// GetAllChallengeParticipants returns participants for all challenges owned by
// creatorID in a single query, keyed by challenge ID.
func GetAllChallengeParticipants(db *sql.DB, creatorID int64) (map[int64][]ChallengeParticipant, error) {
	rows, err := db.Query(`
		SELECT cp.challenge_id, cp.child_id,
		       COALESCE(fl.nickname, ''),
		       COALESCE(fl.avatar_emoji, '⭐'),
		       cp.added_at,
		       cp.completed_at
		FROM challenge_participants cp
		JOIN family_challenges fc ON fc.id = cp.challenge_id AND fc.creator_id = ?
		LEFT JOIN family_links fl ON fl.child_id = cp.child_id AND fl.parent_id = ?
		ORDER BY COALESCE(fl.nickname, '') ASC
	`, creatorID, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[int64][]ChallengeParticipant{}
	for rows.Next() {
		var p ChallengeParticipant
		var challengeID int64
		var encNickname string
		var completedAt sql.NullString
		if err := rows.Scan(&challengeID, &p.ChildID, &encNickname, &p.AvatarEmoji, &p.AddedAt, &completedAt); err != nil {
			return nil, err
		}
		p.Nickname = encryption.DecryptLenient(encNickname)
		if completedAt.Valid {
			p.CompletedAt = completedAt.String
		}
		result[challengeID] = append(result[challengeID], p)
	}
	return result, rows.Err()
}

// scanChallengeRow scans one challenge from sql.Rows.
func scanChallengeRow(rows *sql.Rows) (*Challenge, error) {
	var c Challenge
	var encTitle, encDesc string
	var isActiveInt int

	if err := rows.Scan(
		&c.ID, &c.CreatorID, &encTitle, &encDesc,
		&c.ChallengeType, &c.TargetValue, &c.StarReward,
		&c.StartDate, &c.EndDate, &isActiveInt,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}

	c.Title = encryption.DecryptLenient(encTitle)
	c.Description = encryption.DecryptLenient(encDesc)
	c.IsActive = isActiveInt != 0
	return &c, nil
}
