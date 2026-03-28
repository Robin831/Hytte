package allowance

import (
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Sentinel errors.
var (
	ErrChoreNotFound        = errors.New("chore not found")
	ErrCompletionNotFound   = errors.New("completion not found")
	ErrCompletionExists     = errors.New("chore already completed for this date")
	ErrCompletionNotPending = errors.New("completion is not in pending status")
	ErrExtraNotFound        = errors.New("extra task not found")
	ErrExtraNotOpen         = errors.New("extra task is not open for claiming")
	ErrBonusRuleNotFound    = errors.New("bonus rule not found")
	ErrPayoutNotFound       = errors.New("payout not found")
)

// decryptOrPlaintext decrypts a stored field value. For enc:-prefixed values
// that fail decryption, it logs and returns an empty string. For legacy
// plaintext (no prefix), it returns the value as-is with a warning.
func decryptOrPlaintext(val string) string {
	if val == "" {
		return val
	}
	decrypted, err := encryption.DecryptField(val)
	if err != nil {
		if strings.HasPrefix(val, "enc:") {
			log.Printf("allowance: decrypt field failed for enc:-prefixed value: %v", err)
			return ""
		}
		log.Printf("allowance: returning legacy plaintext value after decrypt failure: %v", err)
		return val
	}
	return decrypted
}

// nowRFC3339 returns the current UTC time formatted as RFC3339.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// boolToInt converts a Go bool to a SQLite-compatible integer.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueConstraintError returns true when err is a SQLite UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// ---- Chore storage ----

// CreateChore inserts a new chore owned by parentID.
// Name and description are encrypted at rest.
func CreateChore(db *sql.DB, parentID int64, childID *int64, name, description string, amount float64, frequency, icon string, requiresApproval bool) (*Chore, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}

	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO allowance_chores
		  (parent_id, child_id, name, description, amount, currency, frequency, icon,
		   requires_approval, active, created_at)
		VALUES (?, ?, ?, ?, ?, 'NOK', ?, ?, ?, 1, ?)
	`, parentID, childID, encName, encDesc, amount, frequency, icon,
		boolToInt(requiresApproval), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Chore{
		ID:               id,
		ParentID:         parentID,
		ChildID:          childID,
		Name:             name,
		Description:      description,
		Amount:           amount,
		Currency:         "NOK",
		Frequency:        frequency,
		Icon:             icon,
		RequiresApproval: requiresApproval,
		Active:           true,
		CreatedAt:        now,
	}, nil
}

// GetChores returns all chores owned by parentID (active and inactive).
func GetChores(db *sql.DB, parentID int64) ([]Chore, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, description, amount, currency,
		       frequency, icon, requires_approval, active, created_at
		FROM allowance_chores
		WHERE parent_id = ?
		ORDER BY created_at ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chores []Chore
	for rows.Next() {
		c, err := scanChoreRow(rows)
		if err != nil {
			return nil, err
		}
		chores = append(chores, *c)
	}
	return chores, rows.Err()
}

// GetChoreByID returns a single chore, verifying it belongs to parentID.
func GetChoreByID(db *sql.DB, id, parentID int64) (*Chore, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, description, amount, currency,
		       frequency, icon, requires_approval, active, created_at
		FROM allowance_chores
		WHERE id = ? AND parent_id = ?
	`, id, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrChoreNotFound
	}
	return scanChoreRow(rows)
}

// UpdateChore modifies a chore's mutable fields, verifying ownership by parentID.
func UpdateChore(db *sql.DB, id, parentID int64, childID *int64, name, description string, amount float64, frequency, icon string, requiresApproval, active bool) (*Chore, error) {
	var createdAt string
	err := db.QueryRow(`SELECT created_at FROM allowance_chores WHERE id = ? AND parent_id = ?`, id, parentID).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChoreNotFound
	}
	if err != nil {
		return nil, err
	}

	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}

	res, err := db.Exec(`
		UPDATE allowance_chores
		SET child_id = ?, name = ?, description = ?, amount = ?, frequency = ?,
		    icon = ?, requires_approval = ?, active = ?
		WHERE id = ? AND parent_id = ?
	`, childID, encName, encDesc, amount, frequency, icon,
		boolToInt(requiresApproval), boolToInt(active), id, parentID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrChoreNotFound
	}
	return &Chore{
		ID:               id,
		ParentID:         parentID,
		ChildID:          childID,
		Name:             name,
		Description:      description,
		Amount:           amount,
		Currency:         "NOK",
		Frequency:        frequency,
		Icon:             icon,
		RequiresApproval: requiresApproval,
		Active:           active,
		CreatedAt:        createdAt,
	}, nil
}

// DeactivateChore sets active=0 on a chore, verifying ownership.
func DeactivateChore(db *sql.DB, id, parentID int64) error {
	res, err := db.Exec(`UPDATE allowance_chores SET active = 0 WHERE id = ? AND parent_id = ?`, id, parentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChoreNotFound
	}
	return nil
}

// GetChildChores returns active chores assigned to childID (or to any child) under parentID.
func GetChildChores(db *sql.DB, parentID, childID int64) ([]Chore, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, description, amount, currency,
		       frequency, icon, requires_approval, active, created_at
		FROM allowance_chores
		WHERE parent_id = ? AND active = 1
		  AND (child_id IS NULL OR child_id = ?)
		ORDER BY created_at ASC
	`, parentID, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chores []Chore
	for rows.Next() {
		c, err := scanChoreRow(rows)
		if err != nil {
			return nil, err
		}
		chores = append(chores, *c)
	}
	return chores, rows.Err()
}

// GetChildChoresWithStatus returns active chores for childID with their completion
// status for the given date (YYYY-MM-DD).
func GetChildChoresWithStatus(db *sql.DB, parentID, childID int64, date string) ([]ChoreWithStatus, error) {
	rows, err := db.Query(`
		SELECT c.id, c.parent_id, c.child_id, c.name, c.description, c.amount, c.currency,
		       c.frequency, c.icon, c.requires_approval, c.active, c.created_at,
		       comp.id, comp.status, comp.notes
		FROM allowance_chores c
		LEFT JOIN allowance_completions comp
		  ON comp.chore_id = c.id AND comp.child_id = ? AND comp.date = ?
		WHERE c.parent_id = ? AND c.active = 1
		  AND (c.child_id IS NULL OR c.child_id = ?)
		ORDER BY c.created_at ASC
	`, childID, date, parentID, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChoreWithStatus
	for rows.Next() {
		var cws ChoreWithStatus
		var encName, encDesc string
		var childIDNull sql.NullInt64
		var reqApprovalInt, activeInt int
		var compID sql.NullInt64
		var compStatus, compNotes sql.NullString

		if err := rows.Scan(
			&cws.ID, &cws.ParentID, &childIDNull, &encName, &encDesc,
			&cws.Amount, &cws.Currency, &cws.Frequency, &cws.Icon,
			&reqApprovalInt, &activeInt, &cws.CreatedAt,
			&compID, &compStatus, &compNotes,
		); err != nil {
			return nil, err
		}
		cws.Name = decryptOrPlaintext(encName)
		cws.Description = decryptOrPlaintext(encDesc)
		cws.RequiresApproval = reqApprovalInt != 0
		cws.Active = activeInt != 0
		if childIDNull.Valid {
			cws.ChildID = &childIDNull.Int64
		}
		if compID.Valid {
			cws.CompletionID = &compID.Int64
		}
		if compStatus.Valid {
			cws.CompletionStatus = &compStatus.String
		}
		if compNotes.Valid {
			dec := decryptOrPlaintext(compNotes.String)
			cws.CompletionNotes = &dec
		}
		results = append(results, cws)
	}
	return results, rows.Err()
}

// ---- Completion storage ----

// CreateCompletion records a child claiming a chore as done.
// Returns ErrCompletionExists if there is already a completion for this chore/child/date.
func CreateCompletion(db *sql.DB, choreID, childID int64, date, notes string) (*Completion, error) {
	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO allowance_completions (chore_id, child_id, date, status, notes, created_at)
		VALUES (?, ?, ?, 'pending', ?, ?)
	`, choreID, childID, date, encNotes, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrCompletionExists
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Completion{
		ID:        id,
		ChoreID:   choreID,
		ChildID:   childID,
		Date:      date,
		Status:    "pending",
		Notes:     notes,
		CreatedAt: now,
	}, nil
}

// GetPendingCompletions returns all pending completions for children linked to parentID.
func GetPendingCompletions(db *sql.DB, parentID int64) ([]CompletionWithDetails, error) {
	rows, err := db.Query(`
		SELECT comp.id, comp.chore_id, c.name, c.icon, c.amount,
		       comp.child_id, COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       comp.date, comp.status, comp.approved_by, comp.approved_at, comp.notes,
		       comp.quality_bonus, comp.created_at
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		LEFT JOIN family_links fl ON fl.child_id = comp.child_id AND fl.parent_id = ?
		WHERE c.parent_id = ? AND comp.status = 'pending'
		ORDER BY comp.created_at ASC
	`, parentID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCompletionDetails(rows)
}

// GetAllCompletions returns completions for children of parentID, optionally filtered by status.
func GetAllCompletions(db *sql.DB, parentID int64, status string) ([]CompletionWithDetails, error) {
	query := `
		SELECT comp.id, comp.chore_id, c.name, c.icon, c.amount,
		       comp.child_id, COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       comp.date, comp.status, comp.approved_by, comp.approved_at, comp.notes,
		       comp.quality_bonus, comp.created_at
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		LEFT JOIN family_links fl ON fl.child_id = comp.child_id AND fl.parent_id = ?
		WHERE c.parent_id = ?`
	args := []any{parentID, parentID}
	if status != "" {
		query += " AND comp.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY comp.created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCompletionDetails(rows)
}

// ApproveCompletion sets a pending completion's status to approved.
// Verifies the completion belongs to a chore owned by parentID.
func ApproveCompletion(db *sql.DB, completionID, parentID int64) (*Completion, error) {
	return resolveCompletion(db, completionID, parentID, "approved", "")
}

// RejectCompletion sets a pending completion's status to rejected.
// reason is stored as notes; pass "" to leave notes unchanged.
func RejectCompletion(db *sql.DB, completionID, parentID int64, reason string) (*Completion, error) {
	return resolveCompletion(db, completionID, parentID, "rejected", reason)
}

func resolveCompletion(db *sql.DB, completionID, parentID int64, status, notes string) (*Completion, error) {
	var comp Completion
	var encNotes string
	var approvedBy sql.NullInt64
	var approvedAt sql.NullString

	err := db.QueryRow(`
		SELECT comp.id, comp.chore_id, comp.child_id, comp.date, comp.status,
		       comp.approved_by, comp.approved_at, comp.notes, comp.quality_bonus, comp.created_at
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		WHERE comp.id = ? AND c.parent_id = ?
	`, completionID, parentID).Scan(
		&comp.ID, &comp.ChoreID, &comp.ChildID, &comp.Date, &comp.Status,
		&approvedBy, &approvedAt, &encNotes, &comp.QualityBonus, &comp.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCompletionNotFound
	}
	if err != nil {
		return nil, err
	}
	if comp.Status != "pending" {
		return nil, ErrCompletionNotPending
	}
	if approvedBy.Valid {
		comp.ApprovedBy = &approvedBy.Int64
	}
	if approvedAt.Valid {
		comp.ApprovedAt = &approvedAt.String
	}

	now := nowRFC3339()

	var encNewNotes string
	if notes != "" {
		encNewNotes, err = encryption.EncryptField(notes)
		if err != nil {
			return nil, err
		}
	} else {
		encNewNotes = encNotes
	}

	_, err = db.Exec(`
		UPDATE allowance_completions
		SET status = ?, approved_by = ?, approved_at = ?, notes = ?
		WHERE id = ?
	`, status, parentID, now, encNewNotes, completionID)
	if err != nil {
		return nil, err
	}

	comp.Status = status
	comp.ApprovedBy = &parentID
	comp.ApprovedAt = &now
	if notes != "" {
		comp.Notes = notes
	} else {
		comp.Notes = decryptOrPlaintext(encNotes)
	}
	return &comp, nil
}

// AutoApproveStaleCompletions sets 'approved' on all pending completions older than
// autoApproveHours for chores owned by parentID.
// When childID > 0, only completions for that specific child are auto-approved.
// Returns the number of completions auto-approved.
func AutoApproveStaleCompletions(db *sql.DB, parentID, childID int64, autoApproveHours int) (int64, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(autoApproveHours) * time.Hour).Format(time.RFC3339)
	now := nowRFC3339()
	query := `
		UPDATE allowance_completions
		SET status = 'approved', approved_at = ?
		WHERE status = 'pending'
		  AND created_at <= ?
		  AND chore_id IN (SELECT id FROM allowance_chores WHERE parent_id = ?)`
	args := []any{now, cutoff, parentID}
	if childID > 0 {
		query += " AND child_id = ?"
		args = append(args, childID)
	}
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AddQualityBonus sets the quality_bonus amount on a completion owned by parentID.
// The completion must belong to a chore owned by parentID.
func AddQualityBonus(db *sql.DB, completionID, parentID int64, amount float64) (*Completion, error) {
	var comp Completion
	var encNotes string
	var approvedBy sql.NullInt64
	var approvedAt sql.NullString

	err := db.QueryRow(`
		SELECT comp.id, comp.chore_id, comp.child_id, comp.date, comp.status,
		       comp.approved_by, comp.approved_at, comp.notes, comp.quality_bonus, comp.created_at
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		WHERE comp.id = ? AND c.parent_id = ?
	`, completionID, parentID).Scan(
		&comp.ID, &comp.ChoreID, &comp.ChildID, &comp.Date, &comp.Status,
		&approvedBy, &approvedAt, &encNotes, &comp.QualityBonus, &comp.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCompletionNotFound
	}
	if err != nil {
		return nil, err
	}

	if approvedBy.Valid {
		comp.ApprovedBy = &approvedBy.Int64
	}
	if approvedAt.Valid {
		comp.ApprovedAt = &approvedAt.String
	}
	comp.Notes = decryptOrPlaintext(encNotes)

	res, err := db.Exec(`UPDATE allowance_completions SET quality_bonus = ? WHERE id = ?`, amount, completionID)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrCompletionNotFound
	}
	comp.QualityBonus = amount
	return &comp, nil
}

// GetChildCompletionsForWeek returns a child's completions for the 7 days starting at weekStart.
func GetChildCompletionsForWeek(db *sql.DB, childID int64, weekStart string) ([]Completion, error) {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return nil, err
	}
	weekEnd := start.AddDate(0, 0, 6).Format("2006-01-02")

	rows, err := db.Query(`
		SELECT id, chore_id, child_id, date, status, approved_by, approved_at, notes, quality_bonus, created_at
		FROM allowance_completions
		WHERE child_id = ? AND date >= ? AND date <= ?
		ORDER BY date ASC
	`, childID, weekStart, weekEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var completions []Completion
	for rows.Next() {
		var c Completion
		var encNotes string
		var approvedBy sql.NullInt64
		var approvedAt sql.NullString

		if err := rows.Scan(
			&c.ID, &c.ChoreID, &c.ChildID, &c.Date, &c.Status,
			&approvedBy, &approvedAt, &encNotes, &c.QualityBonus, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		c.Notes = decryptOrPlaintext(encNotes)
		if approvedBy.Valid {
			c.ApprovedBy = &approvedBy.Int64
		}
		if approvedAt.Valid {
			c.ApprovedAt = &approvedAt.String
		}
		completions = append(completions, c)
	}
	return completions, rows.Err()
}

// ---- Extra task storage ----

// CreateExtra inserts a one-off task posted by a parent.
// Name is encrypted at rest.
func CreateExtra(db *sql.DB, parentID int64, childID *int64, name string, amount float64, expiresAt *string) (*Extra, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO allowance_extras (parent_id, child_id, name, amount, currency, status, expires_at, created_at)
		VALUES (?, ?, ?, ?, 'NOK', 'open', ?, ?)
	`, parentID, childID, encName, amount, expiresAt, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Extra{
		ID:        id,
		ParentID:  parentID,
		ChildID:   childID,
		Name:      name,
		Amount:    amount,
		Currency:  "NOK",
		Status:    "open",
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}, nil
}

// GetExtras returns all extras for a parent.
func GetExtras(db *sql.DB, parentID int64) ([]Extra, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, amount, currency, status,
		       claimed_by, completed_at, approved_at, expires_at, created_at
		FROM allowance_extras
		WHERE parent_id = ?
		ORDER BY created_at DESC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtras(rows)
}

// GetOpenExtras returns open extras that are visible to childID under parentID.
func GetOpenExtras(db *sql.DB, parentID, childID int64) ([]Extra, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, amount, currency, status,
		       claimed_by, completed_at, approved_at, expires_at, created_at
		FROM allowance_extras
		WHERE parent_id = ? AND status = 'open'
		  AND (child_id IS NULL OR child_id = ?)
		ORDER BY created_at DESC
	`, parentID, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtras(rows)
}

// ClaimExtra transitions an extra from 'open' to 'claimed' by childID.
func ClaimExtra(db *sql.DB, extraID, childID int64) (*Extra, error) {
	res, err := db.Exec(`
		UPDATE allowance_extras
		SET status = 'claimed', claimed_by = ?
		WHERE id = ? AND status = 'open'
		  AND (child_id IS NULL OR child_id = ?)
	`, childID, extraID, childID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM allowance_extras WHERE id = ?`, extraID).Scan(&exists)
		if exists == 0 {
			return nil, ErrExtraNotFound
		}
		return nil, ErrExtraNotOpen
	}
	return getExtraByID(db, extraID)
}

// CompleteExtra transitions a claimed extra to 'completed' by the child who claimed it.
func CompleteExtra(db *sql.DB, extraID, childID int64) (*Extra, error) {
	now := nowRFC3339()
	res, err := db.Exec(`
		UPDATE allowance_extras
		SET status = 'completed', completed_at = ?
		WHERE id = ? AND claimed_by = ? AND status = 'claimed'
	`, now, extraID, childID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM allowance_extras WHERE id = ?`, extraID).Scan(&exists)
		if exists == 0 {
			return nil, ErrExtraNotFound
		}
		return nil, ErrExtraNotOpen
	}
	return getExtraByID(db, extraID)
}

// ApproveExtra transitions a claimed/completed extra to 'approved'.
func ApproveExtra(db *sql.DB, extraID, parentID int64) (*Extra, error) {
	now := nowRFC3339()
	res, err := db.Exec(`
		UPDATE allowance_extras
		SET status = 'approved', approved_at = ?
		WHERE id = ? AND parent_id = ? AND status IN ('claimed', 'completed')
	`, now, extraID, parentID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrExtraNotFound
	}
	return getExtraByID(db, extraID)
}

func getExtraByID(db *sql.DB, id int64) (*Extra, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, amount, currency, status,
		       claimed_by, completed_at, approved_at, expires_at, created_at
		FROM allowance_extras WHERE id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	extras, err := scanExtras(rows)
	if err != nil {
		return nil, err
	}
	if len(extras) == 0 {
		return nil, ErrExtraNotFound
	}
	return &extras[0], nil
}

// ---- Bonus rule storage ----

// GetBonusRules returns all bonus rules for a parent.
func GetBonusRules(db *sql.DB, parentID int64) ([]BonusRule, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, type, multiplier, flat_amount, active
		FROM allowance_bonus_rules
		WHERE parent_id = ?
		ORDER BY id ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []BonusRule
	for rows.Next() {
		var br BonusRule
		var activeInt int
		if err := rows.Scan(&br.ID, &br.ParentID, &br.Type, &br.Multiplier, &br.FlatAmount, &activeInt); err != nil {
			return nil, err
		}
		br.Active = activeInt != 0
		rules = append(rules, br)
	}
	return rules, rows.Err()
}

// UpsertBonusRule creates or updates a bonus rule for a parent by type.
// Requires UNIQUE(parent_id, type) on allowance_bonus_rules for atomicity.
func UpsertBonusRule(db *sql.DB, parentID int64, ruleType string, multiplier, flatAmount float64, active bool) (*BonusRule, error) {
	var id int64
	err := db.QueryRow(`
		INSERT INTO allowance_bonus_rules (parent_id, type, multiplier, flat_amount, active)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(parent_id, type) DO UPDATE SET
			multiplier  = excluded.multiplier,
			flat_amount = excluded.flat_amount,
			active      = excluded.active
		RETURNING id
	`, parentID, ruleType, multiplier, flatAmount, boolToInt(active)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &BonusRule{
		ID:         id,
		ParentID:   parentID,
		Type:       ruleType,
		Multiplier: multiplier,
		FlatAmount: flatAmount,
		Active:     active,
	}, nil
}

// ---- Payout storage ----

// GetPayouts returns weekly payouts for a parent, optionally filtered by childID.
// limit <= 0 means no limit.
func GetPayouts(db *sql.DB, parentID int64, childID *int64, limit int) ([]Payout, error) {
	query := `
		SELECT id, parent_id, child_id, week_start, base_amount, bonus_amount, total_amount,
		       currency, paid_out, paid_at, created_at
		FROM allowance_payouts
		WHERE parent_id = ?`
	args := []any{parentID}
	if childID != nil {
		query += " AND child_id = ?"
		args = append(args, *childID)
	}
	query += " ORDER BY week_start DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayouts(rows)
}

// UpsertPayout creates or updates a weekly payout record.
func UpsertPayout(db *sql.DB, parentID, childID int64, weekStart string, baseAmount, bonusAmount, totalAmount float64) (*Payout, error) {
	now := nowRFC3339()
	_, err := db.Exec(`
		INSERT INTO allowance_payouts
		  (parent_id, child_id, week_start, base_amount, bonus_amount, total_amount, currency, paid_out, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'NOK', 0, ?)
		ON CONFLICT(parent_id, child_id, week_start) DO UPDATE
		  SET base_amount  = excluded.base_amount,
		      bonus_amount = excluded.bonus_amount,
		      total_amount = excluded.total_amount
	`, parentID, childID, weekStart, baseAmount, bonusAmount, totalAmount, now)
	if err != nil {
		return nil, err
	}

	var p Payout
	var paidOutInt int
	var paidAt sql.NullString
	err = db.QueryRow(`
		SELECT id, parent_id, child_id, week_start, base_amount, bonus_amount, total_amount,
		       currency, paid_out, paid_at, created_at
		FROM allowance_payouts
		WHERE parent_id = ? AND child_id = ? AND week_start = ?
	`, parentID, childID, weekStart).Scan(
		&p.ID, &p.ParentID, &p.ChildID, &p.WeekStart,
		&p.BaseAmount, &p.BonusAmount, &p.TotalAmount,
		&p.Currency, &paidOutInt, &paidAt, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.PaidOut = paidOutInt != 0
	if paidAt.Valid {
		p.PaidAt = &paidAt.String
	}
	return &p, nil
}

// MarkPayoutPaid marks a payout as paid by setting paid_out=1 and paid_at.
func MarkPayoutPaid(db *sql.DB, payoutID, parentID int64) (*Payout, error) {
	now := nowRFC3339()
	res, err := db.Exec(`
		UPDATE allowance_payouts SET paid_out = 1, paid_at = ? WHERE id = ? AND parent_id = ?
	`, now, payoutID, parentID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrPayoutNotFound
	}

	var p Payout
	var paidOutInt int
	var paidAt sql.NullString
	err = db.QueryRow(`
		SELECT id, parent_id, child_id, week_start, base_amount, bonus_amount, total_amount,
		       currency, paid_out, paid_at, created_at
		FROM allowance_payouts WHERE id = ?
	`, payoutID).Scan(
		&p.ID, &p.ParentID, &p.ChildID, &p.WeekStart,
		&p.BaseAmount, &p.BonusAmount, &p.TotalAmount,
		&p.Currency, &paidOutInt, &paidAt, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.PaidOut = paidOutInt != 0
	if paidAt.Valid {
		p.PaidAt = &paidAt.String
	}
	return &p, nil
}

// ---- Settings storage ----

// GetSettings returns allowance settings for a parent-child pair,
// falling back to sensible defaults if no row exists.
func GetSettings(db *sql.DB, parentID, childID int64) (*Settings, error) {
	var s Settings
	err := db.QueryRow(`
		SELECT parent_id, child_id, base_weekly_amount, currency, auto_approve_hours, updated_at
		FROM allowance_settings
		WHERE parent_id = ? AND child_id = ?
	`, parentID, childID).Scan(
		&s.ParentID, &s.ChildID, &s.BaseWeeklyAmount, &s.Currency, &s.AutoApproveHours, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return &Settings{
			ParentID:         parentID,
			ChildID:          childID,
			BaseWeeklyAmount: 0,
			Currency:         "NOK",
			AutoApproveHours: 24,
		}, nil
	}
	return &s, err
}

// UpsertSettings creates or updates allowance settings for a parent-child pair.
func UpsertSettings(db *sql.DB, parentID, childID int64, baseWeeklyAmount float64, autoApproveHours int) (*Settings, error) {
	now := nowRFC3339()
	_, err := db.Exec(`
		INSERT INTO allowance_settings (parent_id, child_id, base_weekly_amount, currency, auto_approve_hours, updated_at)
		VALUES (?, ?, ?, 'NOK', ?, ?)
		ON CONFLICT(parent_id, child_id) DO UPDATE
		  SET base_weekly_amount = excluded.base_weekly_amount,
		      auto_approve_hours = excluded.auto_approve_hours,
		      updated_at         = excluded.updated_at
	`, parentID, childID, baseWeeklyAmount, autoApproveHours, now)
	if err != nil {
		return nil, err
	}
	return &Settings{
		ParentID:         parentID,
		ChildID:          childID,
		BaseWeeklyAmount: baseWeeklyAmount,
		Currency:         "NOK",
		AutoApproveHours: autoApproveHours,
		UpdatedAt:        now,
	}, nil
}

// ---- Scan helpers ----

func scanChoreRow(rows *sql.Rows) (*Chore, error) {
	var c Chore
	var encName, encDesc string
	var childIDNull sql.NullInt64
	var reqApprovalInt, activeInt int

	if err := rows.Scan(
		&c.ID, &c.ParentID, &childIDNull, &encName, &encDesc,
		&c.Amount, &c.Currency, &c.Frequency, &c.Icon,
		&reqApprovalInt, &activeInt, &c.CreatedAt,
	); err != nil {
		return nil, err
	}
	c.Name = decryptOrPlaintext(encName)
	c.Description = decryptOrPlaintext(encDesc)
	c.RequiresApproval = reqApprovalInt != 0
	c.Active = activeInt != 0
	if childIDNull.Valid {
		c.ChildID = &childIDNull.Int64
	}
	return &c, nil
}

func scanCompletionDetails(rows *sql.Rows) ([]CompletionWithDetails, error) {
	var results []CompletionWithDetails
	for rows.Next() {
		var c CompletionWithDetails
		var encChoreName, encNickname, encNotes string
		var approvedBy sql.NullInt64
		var approvedAt sql.NullString

		if err := rows.Scan(
			&c.ID, &c.ChoreID, &encChoreName, &c.ChoreIcon, &c.ChoreAmount,
			&c.ChildID, &encNickname, &c.ChildAvatar,
			&c.Date, &c.Status, &approvedBy, &approvedAt, &encNotes,
			&c.QualityBonus, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		c.ChoreName = decryptOrPlaintext(encChoreName)
		c.ChildNickname = decryptOrPlaintext(encNickname)
		c.Notes = decryptOrPlaintext(encNotes)
		if approvedBy.Valid {
			c.ApprovedBy = &approvedBy.Int64
		}
		if approvedAt.Valid {
			c.ApprovedAt = &approvedAt.String
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

func scanExtras(rows *sql.Rows) ([]Extra, error) {
	var extras []Extra
	for rows.Next() {
		var e Extra
		var encName string
		var childIDNull, claimedByNull sql.NullInt64
		var completedAt, approvedAt, expiresAt sql.NullString

		if err := rows.Scan(
			&e.ID, &e.ParentID, &childIDNull, &encName, &e.Amount, &e.Currency, &e.Status,
			&claimedByNull, &completedAt, &approvedAt, &expiresAt, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Name = decryptOrPlaintext(encName)
		if childIDNull.Valid {
			e.ChildID = &childIDNull.Int64
		}
		if claimedByNull.Valid {
			e.ClaimedBy = &claimedByNull.Int64
		}
		if completedAt.Valid {
			e.CompletedAt = &completedAt.String
		}
		if approvedAt.Valid {
			e.ApprovedAt = &approvedAt.String
		}
		if expiresAt.Valid {
			e.ExpiresAt = &expiresAt.String
		}
		extras = append(extras, e)
	}
	return extras, rows.Err()
}

func scanPayouts(rows *sql.Rows) ([]Payout, error) {
	var payouts []Payout
	for rows.Next() {
		var p Payout
		var paidOutInt int
		var paidAt sql.NullString
		if err := rows.Scan(
			&p.ID, &p.ParentID, &p.ChildID, &p.WeekStart,
			&p.BaseAmount, &p.BonusAmount, &p.TotalAmount,
			&p.Currency, &paidOutInt, &paidAt, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		p.PaidOut = paidOutInt != 0
		if paidAt.Valid {
			p.PaidAt = &paidAt.String
		}
		payouts = append(payouts, p)
	}
	return payouts, rows.Err()
}
