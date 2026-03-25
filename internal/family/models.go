package family

import "time"

// FamilyLink represents a parent-child account relationship.
type FamilyLink struct {
	ID          int64  `json:"id"`
	ParentID    int64  `json:"parent_id"`
	ChildID     int64  `json:"child_id"`
	Nickname    string `json:"nickname"`
	AvatarEmoji string `json:"avatar_emoji"`
	CreatedAt   string `json:"created_at"`
}

// InviteCode represents a single-use invite code for linking a child account.
type InviteCode struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	ParentID  int64     `json:"parent_id"`
	Used      bool      `json:"used"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt string    `json:"created_at"`
}
