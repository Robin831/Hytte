package family

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	// inviteCodeChars excludes ambiguous characters (0, O, 1, I, l).
	inviteCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	inviteCodeLen   = 6
	inviteTTL       = 24 * time.Hour
)

// Sentinel errors for invite code operations.
var (
	ErrInvalidCode     = errors.New("invalid invite code")
	ErrCodeAlreadyUsed = errors.New("invite code has already been used")
	ErrCodeExpired     = errors.New("invite code has expired")
	ErrAlreadyLinked   = errors.New("account is already linked to a parent")
	ErrSelfLink        = errors.New("cannot link to your own account")
)

// GenerateInviteCode creates a random 6-char invite code and stores it in the DB.
// The code expires after 24 hours and is single-use.
func GenerateInviteCode(db *sql.DB, parentID int64) (*InviteCode, error) {
	code, err := randomCode()
	if err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(inviteTTL)
	createdAtStr := now.Format(time.RFC3339)
	expiresAtStr := expiresAt.Format(time.RFC3339)

	res, err := db.Exec(`
		INSERT INTO invite_codes (code, parent_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, code, parentID, expiresAtStr, createdAtStr)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &InviteCode{
		ID:        id,
		Code:      code,
		ParentID:  parentID,
		Used:      false,
		ExpiresAt: expiresAt,
		CreatedAt: createdAtStr,
	}, nil
}

// AcceptInviteCode validates and accepts an invite code, linking the child to the parent.
// Returns the new FamilyLink on success.
func AcceptInviteCode(db *sql.DB, code string, childID int64) (*FamilyLink, error) {
	var inv struct {
		ID        int64
		ParentID  int64
		Used      int
		ExpiresAt string
	}

	err := db.QueryRow(`
		SELECT id, parent_id, used, expires_at
		FROM invite_codes
		WHERE code = ?
	`, code).Scan(&inv.ID, &inv.ParentID, &inv.Used, &inv.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCode
	}
	if err != nil {
		return nil, err
	}

	if inv.Used != 0 {
		return nil, ErrCodeAlreadyUsed
	}

	expiresAt, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, ErrCodeExpired
	}

	if inv.ParentID == childID {
		return nil, ErrSelfLink
	}

	// A child can only be linked to one parent.
	existing, err := GetParent(db, childID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAlreadyLinked
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`UPDATE invite_codes SET used = 1 WHERE id = ?`, inv.ID); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, inv.ParentID, childID, "", "⭐", now)
	if err != nil {
		return nil, err
	}

	linkID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &FamilyLink{
		ID:          linkID,
		ParentID:    inv.ParentID,
		ChildID:     childID,
		Nickname:    "",
		AvatarEmoji: "⭐",
		CreatedAt:   now,
	}, nil
}

// randomCode generates a random 6-character alphanumeric code.
func randomCode() (string, error) {
	charset := []byte(inviteCodeChars)
	code := make([]byte, inviteCodeLen)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		code[i] = charset[n.Int64()]
	}
	return string(code), nil
}
