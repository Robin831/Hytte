package familychat

import (
	"errors"
)

// ErrForbidden is returned by store helpers when the requesting user is not a
// member of the conversation they are trying to read from or write to. It is
// distinct from sql.ErrNoRows so callers can map it to 403 (or 404 if they
// prefer to hide existence).
var ErrForbidden = errors.New("familychat: not a conversation member")

// Role values for family_chat_members.role. Owners are members who can later
// be granted moderation actions (rename, delete, add/remove members); for the
// schema bead they are stored but not yet enforced anywhere.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)
