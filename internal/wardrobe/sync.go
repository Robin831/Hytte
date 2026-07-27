package wardrobe

import (
	"database/sql"
	"fmt"

	"github.com/Robin831/Hytte/internal/family"
)

// SyncLinkedChildren ensures every child linked to the parent via family_links
// has a wardrobe kid profile, so linked kids appear without manual setup.
// Matching is on user_id, so it is idempotent and leaves manually created
// profiles (kids without an account, e.g. the youngest) untouched. Renames and
// avatar changes made in the wardrobe are preserved — the link only seeds the
// initial profile. Note: deleting a linked kid's profile re-imports it (empty)
// on the next visit; unlink the child in the family settings to remove them
// permanently.
func SyncLinkedChildren(db *sql.DB, parentID int64) error {
	links, err := family.GetChildren(db, parentID)
	if err != nil {
		return fmt.Errorf("wardrobe sync: list linked children: %w", err)
	}
	if len(links) == 0 {
		return nil
	}

	rows, err := db.Query("SELECT user_id FROM wardrobe_kids WHERE parent_id = ? AND user_id IS NOT NULL", parentID)
	if err != nil {
		return fmt.Errorf("wardrobe sync: list linked profiles: %w", err)
	}
	present := map[int64]bool{}
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			rows.Close()
			return fmt.Errorf("wardrobe sync: scan linked profile: %w", err)
		}
		present[userID] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, link := range links {
		if present[link.ChildID] {
			continue
		}
		name := link.Nickname
		if name == "" {
			if err := db.QueryRow("SELECT name FROM users WHERE id = ?", link.ChildID).Scan(&name); err != nil {
				return fmt.Errorf("wardrobe sync: load child name: %w", err)
			}
		}
		childID := link.ChildID
		if _, err := AddKid(db, Kid{
			ParentID:    parentID,
			UserID:      &childID,
			Name:        name,
			AvatarEmoji: link.AvatarEmoji,
		}); err != nil {
			return fmt.Errorf("wardrobe sync: import child %d: %w", link.ChildID, err)
		}
	}
	return nil
}
