package family

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// isSQLiteBusy returns true when err is a SQLite SQLITE_BUSY / "database is
// locked" error. These can occur under concurrent write load and are safe to
// retry.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

// Sentinel errors for reward and claim operations.
var (
	ErrRewardNotFound    = errors.New("reward not found")
	ErrInsufficientStars = errors.New("insufficient star balance")
	ErrRewardNotActive   = errors.New("reward is not active")
	ErrMaxClaimsReached  = errors.New("reward has reached max claims limit")
	ErrClaimNotFound     = errors.New("claim not found")
	ErrClaimNotPending   = errors.New("claim is not in pending status")
)

// boolToInt converts a bool to 0/1 for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateReward creates a new reward owned by parentID.
// Sensitive fields (title, description, parentNote) are encrypted at rest.
func CreateReward(db *sql.DB, parentID int64, title, description, iconEmoji, parentNote string, starCost int, isActive bool, maxClaims *int) (*Reward, error) {
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}
	encNote, err := encryption.EncryptField(parentNote)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`
		INSERT INTO family_rewards
		  (parent_id, title, description, star_cost, icon_emoji, is_active, max_claims, parent_note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, parentID, encTitle, encDesc, starCost, iconEmoji, boolToInt(isActive), maxClaims, encNote, now, now)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Reward{
		ID:          id,
		ParentID:    parentID,
		Title:       title,
		Description: description,
		StarCost:    starCost,
		IconEmoji:   iconEmoji,
		IsActive:    isActive,
		MaxClaims:   maxClaims,
		ParentNote:  parentNote,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetRewards returns all rewards for a parent, including the parent_note.
func GetRewards(db *sql.DB, parentID int64) ([]Reward, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, title, description, star_cost, icon_emoji, is_active, max_claims,
		       parent_note, created_at, updated_at
		FROM family_rewards
		WHERE parent_id = ?
		ORDER BY created_at ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		r, err := scanReward(rows)
		if err != nil {
			return nil, err
		}
		rewards = append(rewards, *r)
	}
	return rewards, rows.Err()
}

// GetActiveRewards returns only active (is_active=1) rewards for a parent.
// The parent_note is not included — this is intended for the kid-facing view.
func GetActiveRewards(db *sql.DB, parentID int64) ([]Reward, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, title, description, star_cost, icon_emoji, is_active, max_claims,
		       parent_note, created_at, updated_at
		FROM family_rewards
		WHERE parent_id = ? AND is_active = 1
		ORDER BY created_at ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		r, err := scanRewardNoNote(rows)
		if err != nil {
			return nil, err
		}
		rewards = append(rewards, *r)
	}
	return rewards, rows.Err()
}

// GetRewardByID returns a single reward by ID, verifying it belongs to parentID.
func GetRewardByID(db *sql.DB, id, parentID int64) (*Reward, error) {
	var r Reward
	var encTitle, encDesc, encNote string
	var isActiveInt int
	var maxClaims sql.NullInt64

	err := db.QueryRow(`
		SELECT id, parent_id, title, description, star_cost, icon_emoji, is_active, max_claims,
		       parent_note, created_at, updated_at
		FROM family_rewards
		WHERE id = ? AND parent_id = ?
	`, id, parentID).Scan(
		&r.ID, &r.ParentID, &encTitle, &encDesc,
		&r.StarCost, &r.IconEmoji, &isActiveInt,
		&maxClaims, &encNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrRewardNotFound
	}
	if err != nil {
		return nil, err
	}

	r.Title = decryptOrPlaintext(encTitle)
	r.Description = decryptOrPlaintext(encDesc)
	r.ParentNote = decryptOrPlaintext(encNote)
	r.IsActive = isActiveInt != 0
	if maxClaims.Valid {
		v := int(maxClaims.Int64)
		r.MaxClaims = &v
	}
	return &r, nil
}

// UpdateReward updates a reward by ID, verifying it belongs to parentID.
func UpdateReward(db *sql.DB, id, parentID int64, title, description, iconEmoji, parentNote string, starCost int, isActive bool, maxClaims *int) (*Reward, error) {
	// Verify ownership and get immutable created_at.
	var createdAt string
	err := db.QueryRow(`SELECT created_at FROM family_rewards WHERE id = ? AND parent_id = ?`, id, parentID).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrRewardNotFound
	}
	if err != nil {
		return nil, err
	}

	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, err
	}
	encNote, err := encryption.EncryptField(parentNote)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`
		UPDATE family_rewards
		SET title = ?, description = ?, star_cost = ?, icon_emoji = ?,
		    is_active = ?, max_claims = ?, parent_note = ?, updated_at = ?
		WHERE id = ? AND parent_id = ?
	`, encTitle, encDesc, starCost, iconEmoji, boolToInt(isActive), maxClaims, encNote, now, id, parentID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrRewardNotFound
	}

	return &Reward{
		ID:          id,
		ParentID:    parentID,
		Title:       title,
		Description: description,
		StarCost:    starCost,
		IconEmoji:   iconEmoji,
		IsActive:    isActive,
		MaxClaims:   maxClaims,
		ParentNote:  parentNote,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}, nil
}

// DeleteReward permanently removes a reward by ID, verifying it belongs to parentID.
func DeleteReward(db *sql.DB, id, parentID int64) error {
	res, err := db.Exec(`DELETE FROM family_rewards WHERE id = ? AND parent_id = ?`, id, parentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRewardNotFound
	}
	return nil
}

// ClaimReward atomically deducts stars and creates a pending reward claim.
// Returns ErrInsufficientStars if the child cannot afford the reward,
// ErrRewardNotActive if the reward is inactive, or ErrMaxClaimsReached if
// the claim limit has been hit.
// ClaimReward attempts to claim a reward on behalf of a child. It retries on
// SQLITE_BUSY so that concurrent callers serialise gracefully rather than
// surfacing a raw lock error to the caller.
func ClaimReward(db *sql.DB, childID, rewardID int64) (*RewardClaim, error) {
	const maxAttempts = 10
	for attempt := 0; attempt < maxAttempts; attempt++ {
		claim, err := claimRewardOnce(db, childID, rewardID)
		if err == nil {
			return claim, nil
		}
		if isSQLiteBusy(err) {
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}
		return nil, err
	}
	return nil, errors.New("reward claim failed: database busy after retries")
}

func claimRewardOnce(db *sql.DB, childID, rewardID int64) (*RewardClaim, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Read reward details inside the transaction, verifying the child is linked
	// to the reward's parent. This prevents cross-family claim attempts.
	var starCost, isActiveInt int
	var maxClaims sql.NullInt64
	err = tx.QueryRow(`
		SELECT fr.star_cost, fr.is_active, fr.max_claims
		FROM family_rewards fr
		JOIN family_links fl ON fl.parent_id = fr.parent_id AND fl.child_id = ?
		WHERE fr.id = ?
	`, childID, rewardID).Scan(&starCost, &isActiveInt, &maxClaims)
	if err == sql.ErrNoRows {
		return nil, ErrRewardNotFound
	}
	if err != nil {
		return nil, err
	}
	if isActiveInt == 0 {
		return nil, ErrRewardNotActive
	}

	// Enforce max_claims if set (count non-denied claims).
	if maxClaims.Valid {
		var totalClaims int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM reward_claims
			WHERE reward_id = ? AND status != 'denied'
		`, rewardID).Scan(&totalClaims); err != nil {
			return nil, err
		}
		if int64(totalClaims) >= maxClaims.Int64 {
			return nil, ErrMaxClaimsReached
		}
	}

	// Check the child's current star balance.
	var currentBalance int
	err = tx.QueryRow(`
		SELECT COALESCE(current_balance, 0)
		FROM star_balances WHERE user_id = ?
	`, childID).Scan(&currentBalance)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if currentBalance < starCost {
		return nil, ErrInsufficientStars
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert the pending claim.
	res, err := tx.Exec(`
		INSERT INTO reward_claims (reward_id, child_id, status, stars_spent, created_at)
		VALUES (?, ?, 'pending', ?, ?)
	`, rewardID, childID, starCost, now)
	if err != nil {
		return nil, err
	}
	claimID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Record the negative star transaction.
	_, err = tx.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (?, ?, 'reward_claim', 'Reward claimed', ?, ?)
	`, childID, -starCost, claimID, now)
	if err != nil {
		return nil, err
	}

	// Update the balance: increase total_spent.
	_, err = tx.Exec(`
		INSERT INTO star_balances (user_id, total_earned, total_spent)
		VALUES (?, 0, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_spent = total_spent + excluded.total_spent
	`, childID, starCost)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &RewardClaim{
		ID:         claimID,
		RewardID:   rewardID,
		ChildID:    childID,
		Status:     "pending",
		StarsSpent: starCost,
		CreatedAt:  now,
	}, nil
}

// GetClaimsByUser returns all claims for a child, with reward title and icon.
func GetClaimsByUser(db *sql.DB, childID int64) ([]KidClaimView, error) {
	rows, err := db.Query(`
		SELECT rc.id, rc.reward_id, fr.title, fr.icon_emoji,
		       rc.status, rc.stars_spent, rc.note, rc.resolved_at, rc.created_at
		FROM reward_claims rc
		JOIN family_rewards fr ON fr.id = rc.reward_id
		WHERE rc.child_id = ?
		ORDER BY rc.created_at DESC
	`, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []KidClaimView
	for rows.Next() {
		var c KidClaimView
		var encTitle, encNote string
		var resolvedAt sql.NullString
		if err := rows.Scan(
			&c.ID, &c.RewardID, &encTitle, &c.RewardIcon,
			&c.Status, &c.StarsSpent, &encNote, &resolvedAt, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		c.RewardTitle = decryptOrPlaintext(encTitle)
		c.Note = decryptOrPlaintext(encNote)
		if resolvedAt.Valid {
			c.ResolvedAt = &resolvedAt.String
		}
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// GetPendingClaims returns all pending claims across all children of parentID.
func GetPendingClaims(db *sql.DB, parentID int64) ([]ClaimWithDetails, error) {
	return queryClaims(db, parentID, "pending")
}

// GetAllClaims returns all claims across all children of parentID.
// Pass status="" to return all statuses, or a specific status to filter.
func GetAllClaims(db *sql.DB, parentID int64, status string) ([]ClaimWithDetails, error) {
	return queryClaims(db, parentID, status)
}

// queryClaims is the shared implementation for GetPendingClaims and GetAllClaims.
func queryClaims(db *sql.DB, parentID int64, status string) ([]ClaimWithDetails, error) {
	query := `
		SELECT rc.id, rc.reward_id, fr.title, fr.icon_emoji, fr.star_cost,
		       rc.child_id, COALESCE(fl.nickname, ''), COALESCE(fl.avatar_emoji, '⭐'),
		       rc.status, rc.stars_spent, rc.note, rc.resolved_at, rc.created_at
		FROM reward_claims rc
		JOIN family_rewards fr ON fr.id = rc.reward_id
		LEFT JOIN family_links fl ON fl.child_id = rc.child_id AND fl.parent_id = ?
		WHERE fr.parent_id = ?`
	args := []any{parentID, parentID}
	if status != "" {
		query += " AND rc.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY rc.created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []ClaimWithDetails
	for rows.Next() {
		var c ClaimWithDetails
		var encTitle, encNickname, encNote string
		var resolvedAt sql.NullString
		if err := rows.Scan(
			&c.ID, &c.RewardID, &encTitle, &c.RewardIcon, &c.StarCost,
			&c.ChildID, &encNickname, &c.ChildAvatar,
			&c.Status, &c.StarsSpent, &encNote, &resolvedAt, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		c.RewardTitle = decryptOrPlaintext(encTitle)
		c.ChildNickname = decryptOrPlaintext(encNickname)
		c.Note = decryptOrPlaintext(encNote)
		if resolvedAt.Valid {
			c.ResolvedAt = &resolvedAt.String
		}
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// ResolveClaim approves or denies a pending claim.
// Denying a claim refunds the stars to the child.
// Returns ErrClaimNotFound if the claim doesn't belong to a reward owned by parentID.
// Returns ErrClaimNotPending if the claim has already been resolved.
func ResolveClaim(db *sql.DB, claimID, parentID int64, status, note string) (*RewardClaim, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Verify the claim belongs to a reward owned by this parent, and is pending.
	var claim RewardClaim
	err = tx.QueryRow(`
		SELECT rc.id, rc.reward_id, rc.child_id, rc.status, rc.stars_spent, rc.created_at
		FROM reward_claims rc
		JOIN family_rewards fr ON fr.id = rc.reward_id
		WHERE rc.id = ? AND fr.parent_id = ?
	`, claimID, parentID).Scan(
		&claim.ID, &claim.RewardID, &claim.ChildID,
		&claim.Status, &claim.StarsSpent, &claim.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrClaimNotFound
	}
	if err != nil {
		return nil, err
	}
	if claim.Status != "pending" {
		return nil, ErrClaimNotPending
	}

	encNote, err := encryption.EncryptField(note)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, err = tx.Exec(`
		UPDATE reward_claims
		SET status = ?, note = ?, resolved_at = ?
		WHERE id = ?
	`, status, encNote, now, claimID)
	if err != nil {
		return nil, err
	}

	// Refund stars on denial.
	if status == "denied" && claim.StarsSpent > 0 {
		_, err = tx.Exec(`
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, ?, 'reward_refund', 'Reward claim denied, stars refunded', ?, ?)
		`, claim.ChildID, claim.StarsSpent, claimID, now)
		if err != nil {
			return nil, err
		}

		_, err = tx.Exec(`
			UPDATE star_balances SET total_spent = total_spent - ?
			WHERE user_id = ?
		`, claim.StarsSpent, claim.ChildID)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	claim.Status = status
	claim.Note = note
	claim.ResolvedAt = &now
	return &claim, nil
}

// GetRewardTitleByID returns the decrypted title of a reward.
// Used for composing push notification messages.
func GetRewardTitleByID(db *sql.DB, rewardID int64) (string, error) {
	var encTitle string
	err := db.QueryRow(`SELECT title FROM family_rewards WHERE id = ?`, rewardID).Scan(&encTitle)
	if err != nil {
		return "", err
	}
	return decryptOrPlaintext(encTitle), nil
}

// scanReward scans a reward row including the encrypted parent_note.
func scanReward(rows *sql.Rows) (*Reward, error) {
	var r Reward
	var encTitle, encDesc, encNote string
	var isActiveInt int
	var maxClaims sql.NullInt64

	if err := rows.Scan(
		&r.ID, &r.ParentID, &encTitle, &encDesc,
		&r.StarCost, &r.IconEmoji, &isActiveInt,
		&maxClaims, &encNote, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}

	r.Title = decryptOrPlaintext(encTitle)
	r.Description = decryptOrPlaintext(encDesc)
	r.ParentNote = decryptOrPlaintext(encNote)
	r.IsActive = isActiveInt != 0
	if maxClaims.Valid {
		v := int(maxClaims.Int64)
		r.MaxClaims = &v
	}
	return &r, nil
}

// scanRewardNoNote scans a reward row but omits the parent_note from the result.
// Used for kid-facing endpoints where the parent's private note must not be exposed.
func scanRewardNoNote(rows *sql.Rows) (*Reward, error) {
	var r Reward
	var encTitle, encDesc, ignoredNote string
	var isActiveInt int
	var maxClaims sql.NullInt64

	if err := rows.Scan(
		&r.ID, &r.ParentID, &encTitle, &encDesc,
		&r.StarCost, &r.IconEmoji, &isActiveInt,
		&maxClaims, &ignoredNote, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}

	r.Title = decryptOrPlaintext(encTitle)
	r.Description = decryptOrPlaintext(encDesc)
	r.IsActive = isActiveInt != 0
	if maxClaims.Valid {
		v := int(maxClaims.Int64)
		r.MaxClaims = &v
	}
	return &r, nil
}
