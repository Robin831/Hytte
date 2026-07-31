package suggestions

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	// PendingMaxAgeDays is how long an AI-generated pending suggestion may sit
	// unacted-upon before the nightly cleanup deletes it to free its page-cap
	// slot for fresh ideas. User-authored suggestions never expire.
	PendingMaxAgeDays = 30

	// RejectedMaxAgeDays is how long rejected suggestions are kept for review
	// before being deleted.
	RejectedMaxAgeDays = 30
)

// CleanupResult reports what a CleanupPending pass removed.
type CleanupResult struct {
	ExpiredPending  int64
	ExpiredRejected int64
	Duplicates      int64
	Superseded      int64
}

// Total returns the number of rows removed across all categories.
func (r CleanupResult) Total() int64 {
	return r.ExpiredPending + r.ExpiredRejected + r.Duplicates + r.Superseded
}

// CleanupPending prunes a user's suggestion list ahead of a generation run so
// stale rows stop occupying MaxPendingPerPage slots:
//
//  1. AI-generated pending suggestions older than PendingMaxAgeDays are
//     deleted — if they haven't been planned in a month they've been passed
//     over, and the prompt's repeat-avoidance window (14 days) no longer
//     shields against regenerating better variants anyway.
//  2. Rejected suggestions are deleted RejectedMaxAgeDays after rejection
//     (falling back to generation time for legacy rows without rejected_at).
//  3. Duplicate pending suggestions — same page and same title ignoring
//     case/whitespace — are collapsed to the newest row.
//  4. Pending suggestions whose page+title matches an already planned or
//     bead-linked suggestion are deleted as superseded.
//
// Planned and bead_created rows are never touched, and user-authored pending
// suggestions are exempt from expiry (but do participate in dedupe, where the
// newest row wins regardless of source).
func CleanupPending(ctx context.Context, db *sql.DB, userID int64) (CleanupResult, error) {
	var res CleanupResult

	pendingCutoff := time.Now().UTC().AddDate(0, 0, -PendingMaxAgeDays).Format(time.RFC3339)
	r, err := db.ExecContext(ctx, `
		DELETE FROM suggestions
		WHERE user_id = ? AND status = ? AND source = ? AND generated_at < ?
	`, userID, StatusPending, SourceClaude, pendingCutoff)
	if err != nil {
		return res, fmt.Errorf("cleanup expired pending: %w", err)
	}
	res.ExpiredPending, _ = r.RowsAffected()

	rejectedCutoff := time.Now().UTC().AddDate(0, 0, -RejectedMaxAgeDays).Format(time.RFC3339)
	r, err = db.ExecContext(ctx, `
		DELETE FROM suggestions
		WHERE user_id = ? AND status = ? AND COALESCE(rejected_at, generated_at) < ?
	`, userID, StatusRejected, rejectedCutoff)
	if err != nil {
		return res, fmt.Errorf("cleanup expired rejected: %w", err)
	}
	res.ExpiredRejected, _ = r.RowsAffected()

	// Titles are stored in plaintext (unlike bodies), so duplicate detection
	// can happen in SQL. MAX(id) picks the newest row per (page, title) group
	// — ids are AUTOINCREMENT, so insertion order tracks generation order.
	r, err = db.ExecContext(ctx, `
		DELETE FROM suggestions
		WHERE user_id = ?1 AND status = ?2 AND id NOT IN (
			SELECT MAX(id) FROM suggestions
			WHERE user_id = ?1 AND status = ?2
			GROUP BY page_slug, lower(trim(title))
		)
	`, userID, StatusPending)
	if err != nil {
		return res, fmt.Errorf("cleanup duplicate pending: %w", err)
	}
	res.Duplicates, _ = r.RowsAffected()

	r, err = db.ExecContext(ctx, `
		DELETE FROM suggestions
		WHERE user_id = ?1 AND status = ?2 AND EXISTS (
			SELECT 1 FROM suggestions done
			WHERE done.user_id = ?1
			  AND done.page_slug = suggestions.page_slug
			  AND lower(trim(done.title)) = lower(trim(suggestions.title))
			  AND done.status IN (?3, ?4)
		)
	`, userID, StatusPending, StatusPlanned, StatusBeadCreated)
	if err != nil {
		return res, fmt.Errorf("cleanup superseded pending: %w", err)
	}
	res.Superseded, _ = r.RowsAffected()

	return res, nil
}
