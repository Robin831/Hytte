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

// Reward is a parent-defined reward that a child can claim with stars.
type Reward struct {
	ID          int64  `json:"id"`
	ParentID    int64  `json:"parent_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	StarCost    int    `json:"star_cost"`
	IconEmoji   string `json:"icon_emoji"`
	IsActive    bool   `json:"is_active"`
	MaxClaims   *int   `json:"max_claims"` // nil = unlimited
	ParentNote  string `json:"parent_note,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// RewardClaim is a child's pending or resolved request to redeem a reward.
type RewardClaim struct {
	ID         int64   `json:"id"`
	RewardID   int64   `json:"reward_id"`
	ChildID    int64   `json:"child_id"`
	Status     string  `json:"status"` // pending, approved, denied
	StarsSpent int     `json:"stars_spent"`
	Note       string  `json:"note,omitempty"`
	ResolvedAt *string `json:"resolved_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// ClaimWithDetails is a reward claim enriched with reward and child info,
// returned to the parent-facing claims endpoint.
type ClaimWithDetails struct {
	ID            int64   `json:"id"`
	RewardID      int64   `json:"reward_id"`
	RewardTitle   string  `json:"reward_title"`
	RewardIcon    string  `json:"reward_icon"`
	StarCost      int     `json:"star_cost"`
	ChildID       int64   `json:"child_id"`
	ChildNickname string  `json:"child_nickname"`
	ChildAvatar   string  `json:"child_avatar"`
	Status        string  `json:"status"`
	StarsSpent    int     `json:"stars_spent"`
	Note          string  `json:"note,omitempty"`
	ResolvedAt    *string `json:"resolved_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

// KidClaimView is a reward claim with reward info, returned to the kid-facing endpoint.
type KidClaimView struct {
	ID          int64   `json:"id"`
	RewardID    int64   `json:"reward_id"`
	RewardTitle string  `json:"reward_title"`
	RewardIcon  string  `json:"reward_icon"`
	Status      string  `json:"status"`
	StarsSpent  int     `json:"stars_spent"`
	Note        string  `json:"note,omitempty"`
	ResolvedAt  *string `json:"resolved_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}
