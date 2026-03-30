package allowance

// Chore is a recurring task defined by a parent with a fixed NOK value.
type Chore struct {
	ID               int64   `json:"id"`
	ParentID         int64   `json:"parent_id"`
	ChildID          *int64  `json:"child_id"` // nil = assigned to any child
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
	Frequency        string  `json:"frequency"` // daily, weekly, once
	Icon             string  `json:"icon"`
	RequiresApproval bool    `json:"requires_approval"`
	Active           bool    `json:"active"`
	CreatedAt        string  `json:"created_at"`
	CompletionMode   string  `json:"completion_mode"`  // solo, team
	MinTeamSize      int64   `json:"min_team_size"`    // minimum participants for team mode
	TeamBonusPct     float64 `json:"team_bonus_pct"`   // bonus percentage awarded for team completion
}

// Completion records a child's claim that a chore is done.
type Completion struct {
	ID           int64   `json:"id"`
	ChoreID      int64   `json:"chore_id"`
	ChildID      int64   `json:"child_id"`
	Date         string  `json:"date"` // YYYY-MM-DD
	Status       string  `json:"status"` // pending, approved, rejected
	ApprovedBy   *int64  `json:"approved_by,omitempty"`
	ApprovedAt   *string `json:"approved_at,omitempty"`
	Notes        string  `json:"notes,omitempty"`
	QualityBonus float64 `json:"quality_bonus"`
	PhotoURL     string  `json:"photo_url,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// TeamCompletion records a child's participation in a team chore completion.
type TeamCompletion struct {
	ID           int64  `json:"id"`
	CompletionID int64  `json:"completion_id"`
	ChildID      int64  `json:"child_id"`
	JoinedAt     string `json:"joined_at"`
}

// CompletionWithDetails is a completion enriched with chore and child info.
type CompletionWithDetails struct {
	ID            int64   `json:"id"`
	ChoreID       int64   `json:"chore_id"`
	ChoreName     string  `json:"chore_name"`
	ChoreIcon     string  `json:"chore_icon"`
	ChoreAmount   float64 `json:"chore_amount"`
	ChildID       int64   `json:"child_id"`
	ChildNickname string  `json:"child_nickname"`
	ChildAvatar   string  `json:"child_avatar"`
	Date          string  `json:"date"`
	Status        string  `json:"status"`
	ApprovedBy    *int64  `json:"approved_by,omitempty"`
	ApprovedAt    *string `json:"approved_at,omitempty"`
	Notes         string  `json:"notes,omitempty"`
	QualityBonus  float64 `json:"quality_bonus,omitempty"`
	PhotoURL      string  `json:"photo_url,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

// ActiveTeamSession describes the open team session for a team chore on a given date.
type ActiveTeamSession struct {
	CompletionID       int64   `json:"completion_id"`
	ParticipantCount   int     `json:"participant_count"`
	ParticipantIDs     []int64 `json:"participant_ids"`
	CurrentChildJoined bool    `json:"current_child_joined"`
}

// ChoreWithStatus is a chore with the child's completion status for a given date.
type ChoreWithStatus struct {
	Chore
	CompletionID      *int64             `json:"completion_id,omitempty"`
	CompletionStatus  *string            `json:"completion_status,omitempty"`
	CompletionNotes   *string            `json:"completion_notes,omitempty"`
	ActiveTeamSession *ActiveTeamSession `json:"active_team_session,omitempty"`
}

// TeamParticipation records a child's participation in a team completion where they are not
// the initiator (comp.child_id != participantID). Used by the earnings calculator.
type TeamParticipation struct {
	CompletionID int64
	ChoreID      int64
	Date         string
	Status       string
	QualityBonus float64
}

// Extra is a one-off task posted by a parent that children can claim.
type Extra struct {
	ID          int64   `json:"id"`
	ParentID    int64   `json:"parent_id"`
	ChildID     *int64  `json:"child_id"` // nil = open to any child
	Name        string  `json:"name"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Status      string  `json:"status"` // open, claimed, completed, approved, expired
	ClaimedBy   *int64  `json:"claimed_by,omitempty"`
	CompletedAt *string `json:"completed_at,omitempty"`
	ApprovedAt  *string `json:"approved_at,omitempty"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// BonusRule defines automatic bonus criteria for a parent's family.
type BonusRule struct {
	ID         int64   `json:"id"`
	ParentID   int64   `json:"parent_id"`
	Type       string  `json:"type"` // full_week, early_bird, streak, quality
	Multiplier float64 `json:"multiplier"`
	FlatAmount float64 `json:"flat_amount"`
	Active     bool    `json:"active"`
}

// Payout is a weekly earnings summary for a single child.
type Payout struct {
	ID            int64   `json:"id"`
	ParentID      int64   `json:"parent_id"`
	ChildID       int64   `json:"child_id"`
	ChildNickname string  `json:"child_nickname,omitempty"`
	ChildAvatar   string  `json:"child_avatar,omitempty"`
	WeekStart     string  `json:"week_start"` // YYYY-MM-DD (Monday)
	BaseAmount    float64 `json:"base_amount"`
	BonusAmount   float64 `json:"bonus_amount"`
	TotalAmount   float64 `json:"total_amount"`
	Currency      string  `json:"currency"`
	PaidOut       bool    `json:"paid_out"`
	PaidAt        *string `json:"paid_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

// Settings holds per-child allowance configuration.
type Settings struct {
	ParentID         int64   `json:"parent_id"`
	ChildID          int64   `json:"child_id"`
	BaseWeeklyAmount float64 `json:"base_weekly_amount"`
	Currency         string  `json:"currency"`
	AutoApproveHours int     `json:"auto_approve_hours"`
	UpdatedAt        string  `json:"updated_at"`
}

// WeeklyEarnings is the computed earnings breakdown for a child's week.
type WeeklyEarnings struct {
	ChildID       int64   `json:"child_id"`
	WeekStart     string  `json:"week_start"`
	BaseAllowance float64 `json:"base_allowance"`
	ChoreEarnings float64 `json:"chore_earnings"`
	BonusAmount   float64 `json:"bonus_amount"`
	TotalAmount   float64 `json:"total_amount"`
	Currency      string  `json:"currency"`
	ApprovedCount int     `json:"approved_count"`
}

// SavingsGoal is a financial target set for a child (e.g., "new bike, 500 NOK").
// Name is stored encrypted. WeeksRemaining is computed on read, not stored.
type SavingsGoal struct {
	ID             int64    `json:"id"`
	ParentID       int64    `json:"parent_id"`
	ChildID        int64    `json:"child_id"`
	Name           string   `json:"name"`
	TargetAmount   float64  `json:"target_amount"`
	CurrentAmount  float64  `json:"current_amount"`
	Currency       string   `json:"currency"`
	Deadline       *string  `json:"deadline,omitempty"` // YYYY-MM-DD
	WeeksRemaining *float64 `json:"weeks_remaining,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}
