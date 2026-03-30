package allowance

import (
	"database/sql"
	"errors"
	"fmt"
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
	ErrGoalNotFound         = errors.New("savings goal not found")
	ErrChoreNotTeamMode     = errors.New("chore is not in team completion mode")
	ErrAlreadyJoined        = errors.New("child has already joined this team session")
	ErrSessionNotWaiting    = errors.New("team session is not in waiting_for_team status")
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
func CreateChore(db *sql.DB, parentID int64, childID *int64, name, description string, amount float64, frequency, icon string, requiresApproval bool, completionMode string, minTeamSize int64, teamBonusPct float64) (*Chore, error) {
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
		   requires_approval, active, created_at, completion_mode, min_team_size, team_bonus_pct)
		VALUES (?, ?, ?, ?, ?, 'NOK', ?, ?, ?, 1, ?, ?, ?, ?)
	`, parentID, childID, encName, encDesc, amount, frequency, icon,
		boolToInt(requiresApproval), now, completionMode, minTeamSize, teamBonusPct)
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
		CompletionMode:   completionMode,
		MinTeamSize:      minTeamSize,
		TeamBonusPct:     teamBonusPct,
	}, nil
}

// GetChores returns all chores owned by parentID (active and inactive).
func GetChores(db *sql.DB, parentID int64) ([]Chore, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, description, amount, currency,
		       frequency, icon, requires_approval, active, created_at,
		       completion_mode, min_team_size, team_bonus_pct
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
		       frequency, icon, requires_approval, active, created_at,
		       completion_mode, min_team_size, team_bonus_pct
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
func UpdateChore(db *sql.DB, id, parentID int64, childID *int64, name, description string, amount float64, frequency, icon string, requiresApproval, active bool, completionMode string, minTeamSize int64, teamBonusPct float64) (*Chore, error) {
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
		    icon = ?, requires_approval = ?, active = ?,
		    completion_mode = ?, min_team_size = ?, team_bonus_pct = ?
		WHERE id = ? AND parent_id = ?
	`, childID, encName, encDesc, amount, frequency, icon,
		boolToInt(requiresApproval), boolToInt(active), completionMode, minTeamSize, teamBonusPct, id, parentID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrChoreNotFound
	}
	return GetChoreByID(db, id, parentID)
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
		       frequency, icon, requires_approval, active, created_at,
		       completion_mode, min_team_size, team_bonus_pct
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
		       c.completion_mode, c.min_team_size, c.team_bonus_pct,
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
	var teamChoreIDs []int64
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
			&cws.CompletionMode, &cws.MinTeamSize, &cws.TeamBonusPct,
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
		if cws.CompletionMode == "team" {
			teamChoreIDs = append(teamChoreIDs, cws.ID)
		}
		results = append(results, cws)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Enrich team chores with active session data.
	if len(teamChoreIDs) > 0 {
		sessions, err := GetActiveTeamSessions(db, childID, teamChoreIDs, date)
		if err != nil {
			return nil, err
		}
		for i := range results {
			if sess, ok := sessions[results[i].ID]; ok {
				results[i].ActiveTeamSession = sess
			}
		}
	}
	return results, nil
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

// DeleteCompletion removes a completion record by ID. Used for rollback when photo persistence fails.
func DeleteCompletion(db *sql.DB, completionID int64) error {
	_, err := db.Exec(`DELETE FROM allowance_completions WHERE id = ?`, completionID)
	return err
}

// GetPendingCompletions returns all pending completions for children linked to parentID.
func GetPendingCompletions(db *sql.DB, parentID int64) ([]CompletionWithDetails, error) {
	rows, err := db.Query(`
		SELECT comp.id, comp.chore_id, c.name, c.icon, c.amount,
		       comp.child_id, COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       comp.date, comp.status, comp.approved_by, comp.approved_at, comp.notes,
		       comp.quality_bonus, COALESCE(comp.photo_path, ''), comp.created_at
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
	completions, err := scanCompletionDetails(rows)
	if err != nil {
		return nil, err
	}
	if err := enrichWithTeamMemberNames(db, parentID, completions); err != nil {
		log.Printf("allowance: enrich team member names parent %d: %v", parentID, err)
		return nil, fmt.Errorf("enrich team member names: %w", err)
	}
	return completions, nil
}

// enrichWithTeamMemberNames populates TeamMemberNames for completions that have
// entries in allowance_team_completions (team chores). The names are resolved via
// family_links for parentID so they come out as the child's nickname.
func enrichWithTeamMemberNames(db *sql.DB, parentID int64, completions []CompletionWithDetails) error {
	if len(completions) == 0 {
		return nil
	}

	idxByID := make(map[int64]int, len(completions))
	placeholders := make([]string, len(completions))
	args := make([]any, 0, len(completions)+1)
	args = append(args, parentID)
	for i, c := range completions {
		idxByID[c.ID] = i
		placeholders[i] = "?"
		args = append(args, c.ID)
	}

	rows, err := db.Query(`
		SELECT atc.completion_id, COALESCE(NULLIF(fl.nickname, ''), u.name, '')
		FROM allowance_team_completions atc
		LEFT JOIN family_links fl ON fl.child_id = atc.child_id AND fl.parent_id = ?
		LEFT JOIN users u ON u.id = atc.child_id
		WHERE atc.completion_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY atc.completion_id ASC, atc.joined_at ASC, atc.child_id ASC
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var completionID int64
		var encNickname string
		if err := rows.Scan(&completionID, &encNickname); err != nil {
			return err
		}
		idx, ok := idxByID[completionID]
		if !ok {
			continue
		}
		nickname := decryptOrPlaintext(encNickname)
		if nickname == "" {
			continue
		}
		completions[idx].TeamMemberNames = append(completions[idx].TeamMemberNames, nickname)
	}
	return rows.Err()
}

// GetAllCompletions returns completions for children of parentID, optionally filtered by status.
func GetAllCompletions(db *sql.DB, parentID int64, status string) ([]CompletionWithDetails, error) {
	query := `
		SELECT comp.id, comp.chore_id, c.name, c.icon, c.amount,
		       comp.child_id, COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       comp.date, comp.status, comp.approved_by, comp.approved_at, comp.notes,
		       comp.quality_bonus, COALESCE(comp.photo_path, ''), comp.created_at
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
// limit <= 0 means no limit. Child nickname and avatar are included via family_links JOIN.
func GetPayouts(db *sql.DB, parentID int64, childID *int64, limit int) ([]Payout, error) {
	query := `
		SELECT ap.id, ap.parent_id, ap.child_id,
		       COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       ap.week_start, ap.base_amount, ap.bonus_amount, ap.total_amount,
		       ap.currency, ap.paid_out, ap.paid_at, ap.created_at
		FROM allowance_payouts ap
		LEFT JOIN family_links fl ON fl.child_id = ap.child_id AND fl.parent_id = ap.parent_id
		WHERE ap.parent_id = ?`
	args := []any{parentID}
	if childID != nil {
		query += " AND ap.child_id = ?"
		args = append(args, *childID)
	}
	query += " ORDER BY ap.week_start DESC"
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

// ---- Team completion storage ----

// StartTeamCompletion creates a completion with status 'waiting_for_team' for a team chore
// and inserts the initiating child into allowance_team_completions.
// Both writes are performed in a single transaction so a failure on the second insert
// does not leave an orphaned completion row.
func StartTeamCompletion(db *sql.DB, parentID, choreID, childID int64, date string) (*Completion, error) {
	chore, err := GetChoreByID(db, choreID, parentID)
	if err != nil {
		return nil, err
	}
	if !chore.Active {
		return nil, ErrChoreNotFound
	}
	if chore.CompletionMode != "team" {
		return nil, ErrChoreNotTeamMode
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Enforce one active team session per (chore, date) regardless of which child started it.
	var existingID int64
	err = tx.QueryRow(`
		SELECT id FROM allowance_completions
		WHERE chore_id = ? AND date = ? AND status IN ('waiting_for_team', 'pending', 'approved')
		LIMIT 1
	`, choreID, date).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		return nil, ErrCompletionExists
	}

	now := nowRFC3339()
	res, err := tx.Exec(`
		INSERT INTO allowance_completions (chore_id, child_id, date, status, notes, created_at)
		VALUES (?, ?, ?, 'waiting_for_team', '', ?)
	`, choreID, childID, date, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrCompletionExists
		}
		return nil, err
	}
	completionID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if _, err = tx.Exec(`
		INSERT INTO allowance_team_completions (completion_id, child_id, joined_at)
		VALUES (?, ?, ?)
	`, completionID, childID, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Completion{
		ID:        completionID,
		ChoreID:   choreID,
		ChildID:   childID,
		Date:      date,
		Status:    "waiting_for_team",
		CreatedAt: now,
	}, nil
}

// JoinTeamCompletion adds childID to a 'waiting_for_team' completion and promotes it
// to 'pending' once the chore's min_team_size is reached.
// The status check, insert, count, and optional status update are all performed in a
// single transaction to prevent race conditions when multiple children join simultaneously.
func JoinTeamCompletion(db *sql.DB, parentID, completionID, childID int64) (*Completion, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Re-read status and min_team_size inside the transaction so the check and insert are atomic.
	var comp Completion
	var minTeamSize int64
	err = tx.QueryRow(`
		SELECT comp.id, comp.chore_id, comp.child_id, comp.date, comp.status, comp.created_at,
		       c.min_team_size
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		WHERE comp.id = ? AND c.parent_id = ?
	`, completionID, parentID).Scan(
		&comp.ID, &comp.ChoreID, &comp.ChildID, &comp.Date, &comp.Status, &comp.CreatedAt,
		&minTeamSize,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCompletionNotFound
	}
	if err != nil {
		return nil, err
	}
	if comp.Status != "waiting_for_team" {
		return nil, ErrSessionNotWaiting
	}

	now := nowRFC3339()
	if _, err = tx.Exec(`
		INSERT INTO allowance_team_completions (completion_id, child_id, joined_at)
		VALUES (?, ?, ?)
	`, completionID, childID, now); err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrAlreadyJoined
		}
		return nil, err
	}

	var count int64
	if scanErr := tx.QueryRow(`
		SELECT COUNT(*) FROM allowance_team_completions WHERE completion_id = ?
	`, completionID).Scan(&count); scanErr != nil {
		return nil, scanErr
	}

	if count >= minTeamSize {
		result, err := tx.Exec(`UPDATE allowance_completions SET status = 'pending' WHERE id = ? AND status = 'waiting_for_team'`, completionID)
		if err != nil {
			return nil, err
		}
		// Only mark as promoted if this transaction actually changed the row.
		// Under concurrent joins, at most one transaction will see RowsAffected > 0,
		// preventing duplicate "team complete" notifications.
		if n, _ := result.RowsAffected(); n > 0 {
			comp.Status = "pending"
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &comp, nil
}

// GetActiveTeamSessions returns open (waiting_for_team) team sessions for the given chore IDs
// on date. Returns a map from chore_id → ActiveTeamSession.
func GetActiveTeamSessions(db *sql.DB, childID int64, choreIDs []int64, date string) (map[int64]*ActiveTeamSession, error) {
	if len(choreIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(choreIDs))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(choreIDs)+1)
	for _, id := range choreIDs {
		args = append(args, id)
	}
	args = append(args, date)

	rows, err := db.Query(`
		SELECT comp.id, comp.chore_id, tc.child_id
		FROM allowance_completions comp
		JOIN allowance_team_completions tc ON tc.completion_id = comp.id
		WHERE comp.chore_id IN (`+placeholders+`)
		  AND comp.date = ?
		  AND comp.status = 'waiting_for_team'
		ORDER BY comp.id, tc.joined_at
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]*ActiveTeamSession)
	for rows.Next() {
		var completionID, choreID, participantChildID int64
		if err := rows.Scan(&completionID, &choreID, &participantChildID); err != nil {
			return nil, err
		}
		sess, ok := result[choreID]
		if !ok {
			sess = &ActiveTeamSession{
				CompletionID: completionID,
				ParticipantIDs: []int64{},
			}
			result[choreID] = sess
		}
		sess.ParticipantIDs = append(sess.ParticipantIDs, participantChildID)
		sess.ParticipantCount++
		if participantChildID == childID {
			sess.CurrentChildJoined = true
		}
	}
	return result, rows.Err()
}

// GetTeamParticipantCounts returns the number of team participants for each completion ID.
// Only completion IDs with at least one participant appear in the result.
func GetTeamParticipantCounts(db *sql.DB, completionIDs []int64) (map[int64]int, error) {
	if len(completionIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(completionIDs))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(completionIDs))
	for i, id := range completionIDs {
		args[i] = id
	}

	rows, err := db.Query(`
		SELECT completion_id, COUNT(*) FROM allowance_team_completions
		WHERE completion_id IN (`+placeholders+`)
		GROUP BY completion_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]int)
	for rows.Next() {
		var id int64
		var cnt int
		if err := rows.Scan(&id, &cnt); err != nil {
			return nil, err
		}
		result[id] = cnt
	}
	return result, rows.Err()
}

// GetTeamParticipationsForChildInWeek returns approved team completions where childID is a
// participant but NOT the initiator (comp.child_id != childID), for the given week range.
func GetTeamParticipationsForChildInWeek(db *sql.DB, childID int64, weekStart, weekEnd string) ([]TeamParticipation, error) {
	rows, err := db.Query(`
		SELECT comp.id, comp.chore_id, comp.date, comp.status, comp.quality_bonus
		FROM allowance_completions comp
		JOIN allowance_team_completions tc ON tc.completion_id = comp.id
		WHERE tc.child_id = ?
		  AND comp.child_id != ?
		  AND comp.status = 'approved'
		  AND comp.date >= ? AND comp.date <= ?
	`, childID, childID, weekStart, weekEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TeamParticipation
	for rows.Next() {
		var tp TeamParticipation
		if err := rows.Scan(&tp.CompletionID, &tp.ChoreID, &tp.Date, &tp.Status, &tp.QualityBonus); err != nil {
			return nil, err
		}
		result = append(result, tp)
	}
	return result, rows.Err()
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
		&c.CompletionMode, &c.MinTeamSize, &c.TeamBonusPct,
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
		var encChoreName, encNickname, encNotes, photoPath string
		var approvedBy sql.NullInt64
		var approvedAt sql.NullString

		if err := rows.Scan(
			&c.ID, &c.ChoreID, &encChoreName, &c.ChoreIcon, &c.ChoreAmount,
			&c.ChildID, &encNickname, &c.ChildAvatar,
			&c.Date, &c.Status, &approvedBy, &approvedAt, &encNotes,
			&c.QualityBonus, &photoPath, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		c.ChoreName = decryptOrPlaintext(encChoreName)
		c.ChildNickname = decryptOrPlaintext(encNickname)
		c.Notes = decryptOrPlaintext(encNotes)
		if photoPath != "" {
			c.PhotoURL = fmt.Sprintf("/api/allowance/photos/%d", c.ID)
		}
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
		var encNickname string
		if err := rows.Scan(
			&p.ID, &p.ParentID, &p.ChildID, &encNickname, &p.ChildAvatar,
			&p.WeekStart, &p.BaseAmount, &p.BonusAmount, &p.TotalAmount,
			&p.Currency, &paidOutInt, &paidAt, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		p.ChildNickname = decryptOrPlaintext(encNickname)
		p.PaidOut = paidOutInt != 0
		if paidAt.Valid {
			p.PaidAt = &paidAt.String
		}
		payouts = append(payouts, p)
	}
	return payouts, rows.Err()
}

// ---- Savings goal storage ----

// GetSavingsGoals returns all savings goals for the given child under a parent.
// WeeksRemaining is computed from recent paid payout history.
func GetSavingsGoals(db *sql.DB, parentID, childID int64) ([]SavingsGoal, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, child_id, name, target_amount, current_amount,
		       currency, deadline, created_at, updated_at
		FROM allowance_savings_goals
		WHERE parent_id = ? AND child_id = ?
		ORDER BY created_at DESC
	`, parentID, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	goals, err := scanSavingsGoals(rows)
	if err != nil {
		return nil, err
	}

	avgWeekly := avgWeeklyEarnings(db, parentID, childID)
	for i := range goals {
		goals[i].WeeksRemaining = computeWeeksRemaining(&goals[i], avgWeekly)
	}
	return goals, nil
}

// CreateSavingsGoal inserts a new savings goal. Name is encrypted at rest.
func CreateSavingsGoal(db *sql.DB, parentID, childID int64, name string, targetAmount float64, deadline *string) (*SavingsGoal, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()

	var deadlineVal any
	if deadline != nil {
		deadlineVal = *deadline
	}

	res, err := db.Exec(`
		INSERT INTO allowance_savings_goals
		  (parent_id, child_id, name, target_amount, current_amount, currency, deadline, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, 'NOK', ?, ?, ?)
	`, parentID, childID, encName, targetAmount, deadlineVal, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	goal, err := getSavingsGoalByID(db, id, parentID)
	if err != nil {
		return nil, err
	}
	avgWeekly := avgWeeklyEarnings(db, parentID, childID)
	goal.WeeksRemaining = computeWeeksRemaining(goal, avgWeekly)
	return goal, nil
}

// UpdateSavingsGoal updates name, target, current amount, and deadline for a goal.
// Name is encrypted at rest.
func UpdateSavingsGoal(db *sql.DB, goalID, parentID, childID int64, name string, targetAmount, currentAmount float64, deadline *string) (*SavingsGoal, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()

	var deadlineVal any
	if deadline != nil {
		deadlineVal = *deadline
	}

	res, err := db.Exec(`
		UPDATE allowance_savings_goals
		SET name = ?, target_amount = ?, current_amount = ?, deadline = ?, updated_at = ?
		WHERE id = ? AND parent_id = ? AND child_id = ?
	`, encName, targetAmount, currentAmount, deadlineVal, now, goalID, parentID, childID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrGoalNotFound
	}
	goal, err := getSavingsGoalByID(db, goalID, parentID)
	if err != nil {
		return nil, err
	}
	avgWeekly := avgWeeklyEarnings(db, parentID, childID)
	goal.WeeksRemaining = computeWeeksRemaining(goal, avgWeekly)
	return goal, nil
}

// DeleteSavingsGoal removes a savings goal by ID, verifying parent and child ownership.
func DeleteSavingsGoal(db *sql.DB, goalID, parentID, childID int64) error {
	res, err := db.Exec(`DELETE FROM allowance_savings_goals WHERE id = ? AND parent_id = ? AND child_id = ?`, goalID, parentID, childID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGoalNotFound
	}
	return nil
}

func getSavingsGoalByID(db *sql.DB, goalID, parentID int64) (*SavingsGoal, error) {
	var g SavingsGoal
	var encName string
	var deadline sql.NullString
	err := db.QueryRow(`
		SELECT id, parent_id, child_id, name, target_amount, current_amount,
		       currency, deadline, created_at, updated_at
		FROM allowance_savings_goals
		WHERE id = ? AND parent_id = ?
	`, goalID, parentID).Scan(
		&g.ID, &g.ParentID, &g.ChildID, &encName, &g.TargetAmount, &g.CurrentAmount,
		&g.Currency, &deadline, &g.CreatedAt, &g.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGoalNotFound
	}
	if err != nil {
		return nil, err
	}
	g.Name = decryptOrPlaintext(encName)
	if deadline.Valid {
		g.Deadline = &deadline.String
	}
	return &g, nil
}

// GetSavingsGoalByID fetches a single savings goal scoped by (goalID, parentID, childID).
// Returns ErrGoalNotFound if no matching goal exists.
func GetSavingsGoalByID(db *sql.DB, goalID, parentID, childID int64) (*SavingsGoal, error) {
	var g SavingsGoal
	var encName string
	var deadline sql.NullString
	err := db.QueryRow(`
		SELECT id, parent_id, child_id, name, target_amount, current_amount,
		       currency, deadline, created_at, updated_at
		FROM allowance_savings_goals
		WHERE id = ? AND parent_id = ? AND child_id = ?
	`, goalID, parentID, childID).Scan(
		&g.ID, &g.ParentID, &g.ChildID, &encName, &g.TargetAmount, &g.CurrentAmount,
		&g.Currency, &deadline, &g.CreatedAt, &g.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGoalNotFound
	}
	if err != nil {
		return nil, err
	}
	g.Name = decryptOrPlaintext(encName)
	if deadline.Valid {
		g.Deadline = &deadline.String
	}
	return &g, nil
}

func scanSavingsGoals(rows *sql.Rows) ([]SavingsGoal, error) {
	var goals []SavingsGoal
	for rows.Next() {
		var g SavingsGoal
		var encName string
		var deadline sql.NullString
		if err := rows.Scan(
			&g.ID, &g.ParentID, &g.ChildID, &encName, &g.TargetAmount, &g.CurrentAmount,
			&g.Currency, &deadline, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			return nil, err
		}
		g.Name = decryptOrPlaintext(encName)
		if deadline.Valid {
			g.Deadline = &deadline.String
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// avgWeeklyEarnings returns the average total_amount from the last 8 paid payouts
// for a child. Returns 0 if there is insufficient history.
func avgWeeklyEarnings(db *sql.DB, parentID, childID int64) float64 {
	var total float64
	var count int
	rows, err := db.Query(`
		SELECT total_amount FROM allowance_payouts
		WHERE parent_id = ? AND child_id = ? AND paid_out = 1
		ORDER BY week_start DESC LIMIT 8
	`, parentID, childID)
	if err != nil {
		log.Printf("avgWeeklyEarnings: query failed for parent_id=%d child_id=%d: %v", parentID, childID, err)
		return 0
	}
	defer rows.Close()
	for rows.Next() {
		var amt float64
		if err := rows.Scan(&amt); err != nil {
			log.Printf("avgWeeklyEarnings: scan failed for parent_id=%d child_id=%d: %v", parentID, childID, err)
			continue
		}
		total += amt
		count++
	}
	if err := rows.Err(); err != nil {
		log.Printf("avgWeeklyEarnings: rows iteration error for parent_id=%d child_id=%d: %v", parentID, childID, err)
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// computeWeeksRemaining returns weeks until goal is reached given average weekly earnings.
// Returns nil if already reached or no earnings history available.
func computeWeeksRemaining(goal *SavingsGoal, avgWeekly float64) *float64 {
	remaining := goal.TargetAmount - goal.CurrentAmount
	if remaining <= 0 || avgWeekly <= 0 {
		return nil
	}
	weeks := remaining / avgWeekly
	return &weeks
}

// GetAllFamilyLinksWithAllowance returns all (parentID, childID) pairs where the parent
// has the kids_allowance feature enabled. Used by the weekly payout scheduler.
func GetAllFamilyLinksWithAllowance(db *sql.DB) ([]struct{ ParentID, ChildID int64 }, error) {
	rows, err := db.Query(`
		SELECT fl.parent_id, fl.child_id
		FROM family_links fl
		WHERE EXISTS (
			SELECT 1 FROM user_features uf
			WHERE uf.user_id = fl.parent_id AND uf.feature_key = 'kids_allowance' AND uf.enabled = 1
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []struct{ ParentID, ChildID int64 }
	for rows.Next() {
		var l struct{ ParentID, ChildID int64 }
		if err := rows.Scan(&l.ParentID, &l.ChildID); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}
