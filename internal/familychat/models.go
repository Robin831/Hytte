package familychat

import (
	"errors"
	"time"
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

// Conversation mirrors a row in family_chat_conversations. Name is the
// decrypted display name (empty for an unnamed 1:1 chat).
type Conversation struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	OwnerID   int64     `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Member mirrors a row in family_chat_members. LastReadAt is nil when the
// member has never read the conversation.
type Member struct {
	ConversationID int64      `json:"conversation_id"`
	UserID         int64      `json:"user_id"`
	Role           string     `json:"role"`
	JoinedAt       time.Time  `json:"joined_at"`
	LastReadAt     *time.Time `json:"last_read_at,omitempty"`
}

// Message mirrors a row in family_chat_messages. Body and AttachmentPath are
// the decrypted plaintext values. AttachmentPath/AttachmentMime are empty when
// the message has no attachment.
type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	SenderID       int64     `json:"sender_id"`
	Body           string    `json:"body"`
	AttachmentPath string    `json:"attachment_path,omitempty"`
	AttachmentMime string    `json:"attachment_mime,omitempty"`
	SentAt         time.Time `json:"sent_at"`
}
