package familychat

import (
	"database/sql"
	"errors"
)

// MembershipFn reports whether userID is a member of conversation convID.
// Returning (false, nil) means "not a member" (caller should 404). Returning
// a non-nil error means the check itself failed; the caller decides whether
// to map that to 500 (genuine failure) or 404 (avoid leaking existence).
type MembershipFn func(userID, convID int64) (bool, error)

// DefaultMembership returns a MembershipFn backed by the family_chat_members
// table from the canonical family chat schema.
func DefaultMembership(db *sql.DB) MembershipFn {
	return func(userID, convID int64) (bool, error) {
		var one int
		err := db.QueryRow(
			`SELECT 1 FROM family_chat_members WHERE conversation_id = ? AND user_id = ?`,
			convID, userID,
		).Scan(&one)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
}
