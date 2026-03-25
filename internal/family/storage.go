package family

import (
	"database/sql"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// CreateLink links a child account to a parent account.
func CreateLink(db *sql.DB, parentID, childID int64, nickname, avatarEmoji string) (*FamilyLink, error) {
	encNickname, err := encryption.EncryptField(nickname)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, encNickname, avatarEmoji, now)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &FamilyLink{
		ID:          id,
		ParentID:    parentID,
		ChildID:     childID,
		Nickname:    nickname,
		AvatarEmoji: avatarEmoji,
		CreatedAt:   now,
	}, nil
}

// GetChildren returns all children linked to the given parent.
func GetChildren(db *sql.DB, parentID int64) ([]FamilyLink, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, nickname, avatar_emoji, created_at
		FROM family_links
		WHERE parent_id = ?
		ORDER BY created_at ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []FamilyLink
	for rows.Next() {
		var l FamilyLink
		var encNickname string
		if err := rows.Scan(&l.ID, &l.ParentID, &l.ChildID, &encNickname, &l.AvatarEmoji, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Nickname = decryptOrPlaintext(encNickname)
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetParent returns the parent link for a child, or nil if not linked.
func GetParent(db *sql.DB, childID int64) (*FamilyLink, error) {
	var l FamilyLink
	var encNickname string
	err := db.QueryRow(`
		SELECT id, parent_id, child_id, nickname, avatar_emoji, created_at
		FROM family_links
		WHERE child_id = ?
	`, childID).Scan(&l.ID, &l.ParentID, &l.ChildID, &encNickname, &l.AvatarEmoji, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.Nickname = decryptOrPlaintext(encNickname)
	return &l, nil
}

// UpdateChild updates the nickname and avatar emoji for a child link.
func UpdateChild(db *sql.DB, parentID, childID int64, nickname, avatarEmoji string) (*FamilyLink, error) {
	encNickname, err := encryption.EncryptField(nickname)
	if err != nil {
		return nil, err
	}

	// Verify the link exists before updating. SQLite RowsAffected returns 0 for
	// no-op updates (same values), so we cannot use it to distinguish "not found"
	// from "found but nothing changed".
	var l FamilyLink
	var enc string
	err = db.QueryRow(`
		SELECT id, parent_id, child_id, nickname, avatar_emoji, created_at
		FROM family_links
		WHERE parent_id = ? AND child_id = ?
	`, parentID, childID).Scan(&l.ID, &l.ParentID, &l.ChildID, &enc, &l.AvatarEmoji, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		UPDATE family_links SET nickname = ?, avatar_emoji = ?
		WHERE parent_id = ? AND child_id = ?
	`, encNickname, avatarEmoji, parentID, childID)
	if err != nil {
		return nil, err
	}

	l.Nickname = nickname
	l.AvatarEmoji = avatarEmoji
	return &l, nil
}

// RemoveChild removes the link between a parent and a child.
func RemoveChild(db *sql.DB, parentID, childID int64) error {
	res, err := db.Exec(`DELETE FROM family_links WHERE parent_id = ? AND child_id = ?`, parentID, childID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// IsParent returns true if the user has at least one linked child.
func IsParent(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM family_links WHERE parent_id = ?`, userID).Scan(&count)
	return count > 0, err
}

// IsChild returns true if the user is linked as a child.
func IsChild(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM family_links WHERE child_id = ?`, userID).Scan(&count)
	return count > 0, err
}

// decryptOrPlaintext decrypts a field value. If the value has the "enc:" prefix but
// decryption fails, it returns an empty string to avoid leaking ciphertext to callers.
// For legacy plaintext values (no "enc:" prefix), the value is returned as-is.
func decryptOrPlaintext(val string) string {
	if val == "" {
		return val
	}
	decrypted, err := encryption.DecryptField(val)
	if err != nil {
		if len(val) >= 4 && val[:4] == "enc:" {
			log.Printf("family: decrypt field failed for enc:-prefixed value: %v", err)
			return ""
		}
		log.Printf("family: decrypt field warning (legacy plaintext): %v", err)
		return val
	}
	return decrypted
}
