package suggestions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Source values for the source column.
const (
	SourceClaude = "claude"
	SourceUser   = "user"
)

// Type values for the type column.
const (
	TypeAddition    = "addition"
	TypeBugfix      = "bugfix"
	TypeImprovement = "improvement"
	TypeRefactor    = "refactor"
	TypeNewPage     = "new_page"
)

// Size values for the size column.
const (
	SizeS = "s"
	SizeM = "m"
	SizeL = "l"
)

// Status values for the status column.
const (
	StatusPending     = "pending"
	StatusRejected    = "rejected"
	StatusPlanned     = "planned"
	StatusBeadCreated = "bead_created"
)

// Suggestion is the decrypted in-memory representation of a row in the
// suggestions table. Body, Feedback, and Plan are plaintext on this struct;
// they are encrypted when written and decrypted when read.
type Suggestion struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	GeneratedAt   time.Time  `json:"generated_at"`
	PageSlug      string     `json:"page_slug"`
	Source        string     `json:"source"`
	Type          string     `json:"type"`
	Size          string     `json:"size"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	Status        string     `json:"status"`
	Feedback      string     `json:"feedback,omitempty"`
	Plan          string     `json:"plan,omitempty"`
	BeadID        string     `json:"bead_id,omitempty"`
	RejectedAt    *time.Time `json:"rejected_at,omitempty"`
	PlannedAt     *time.Time `json:"planned_at,omitempty"`
	BeadCreatedAt *time.Time `json:"bead_created_at,omitempty"`
}

// Insert persists a new suggestion. Body, Feedback, and Plan are encrypted at
// the boundary using encryption.EncryptField. Returns the new row ID.
func Insert(ctx context.Context, db *sql.DB, s Suggestion) (int64, error) {
	encBody, err := encryption.EncryptField(s.Body)
	if err != nil {
		return 0, fmt.Errorf("encrypt body: %w", err)
	}
	encFeedback, err := encryption.EncryptField(s.Feedback)
	if err != nil {
		return 0, fmt.Errorf("encrypt feedback: %w", err)
	}
	encPlan, err := encryption.EncryptField(s.Plan)
	if err != nil {
		return 0, fmt.Errorf("encrypt plan: %w", err)
	}

	generatedAt := s.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	status := s.Status
	if status == "" {
		status = StatusPending
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO suggestions (
			user_id, generated_at, page_slug, source, type, size,
			title, body_enc, status, feedback_enc, plan_enc, bead_id,
			rejected_at, planned_at, bead_created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		s.UserID,
		generatedAt.UTC().Format(time.RFC3339),
		s.PageSlug,
		s.Source,
		s.Type,
		s.Size,
		s.Title,
		encBody,
		status,
		nullableEncrypted(encFeedback),
		nullableEncrypted(encPlan),
		nullableString(s.BeadID),
		nullableTime(s.RejectedAt),
		nullableTime(s.PlannedAt),
		nullableTime(s.BeadCreatedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert suggestion: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// ListByStatus returns all suggestions for a user filtered by status, newest first.
// Encrypted fields are decrypted; on decrypt error the value is replaced with an
// empty string and the error is logged (see decryptField for rationale).
func ListByStatus(ctx context.Context, db *sql.DB, userID int64, status string) ([]Suggestion, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, generated_at, page_slug, source, type, size,
		       title, body_enc, status, feedback_enc, plan_enc, bead_id,
		       rejected_at, planned_at, bead_created_at
		FROM suggestions
		WHERE user_id = ? AND status = ?
		ORDER BY generated_at DESC, id DESC
	`, userID, status)
	if err != nil {
		return nil, fmt.Errorf("query suggestions: %w", err)
	}
	defer rows.Close()

	var out []Suggestion
	for rows.Next() {
		s, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetByID returns a single suggestion by ID. Returns sql.ErrNoRows if not found.
func GetByID(ctx context.Context, db *sql.DB, id int64) (*Suggestion, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, generated_at, page_slug, source, type, size,
		       title, body_enc, status, feedback_enc, plan_enc, bead_id,
		       rejected_at, planned_at, bead_created_at
		FROM suggestions
		WHERE id = ?
	`, id)
	s, err := scanSuggestion(row)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MarkRejected sets status to "rejected" and records the rejected_at timestamp.
func MarkRejected(ctx context.Context, db *sql.DB, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
		UPDATE suggestions SET status = ?, rejected_at = ? WHERE id = ?
	`, StatusRejected, now, id)
	if err != nil {
		return fmt.Errorf("mark rejected: %w", err)
	}
	return checkRowsAffected(res, id)
}

// MarkPlanned sets status to "planned", stores the encrypted plan body, and
// records planned_at.
func MarkPlanned(ctx context.Context, db *sql.DB, id int64, plan string) error {
	encPlan, err := encryption.EncryptField(plan)
	if err != nil {
		return fmt.Errorf("encrypt plan: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
		UPDATE suggestions SET status = ?, plan_enc = ?, planned_at = ?, rejected_at = NULL WHERE id = ?
	`, StatusPlanned, nullableEncrypted(encPlan), now, id)
	if err != nil {
		return fmt.Errorf("mark planned: %w", err)
	}
	return checkRowsAffected(res, id)
}

// MarkBeadCreated sets status to "bead_created", stores the bead ID, and
// records bead_created_at.
func MarkBeadCreated(ctx context.Context, db *sql.DB, id int64, beadID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
		UPDATE suggestions SET status = ?, bead_id = ?, bead_created_at = ? WHERE id = ?
	`, StatusBeadCreated, nullableString(beadID), now, id)
	if err != nil {
		return fmt.Errorf("mark bead created: %w", err)
	}
	return checkRowsAffected(res, id)
}

// recentForPage returns suggestions for a given page slug whose status is not
// "rejected" and that were generated within the last `days` days. Used by the
// generator to discourage repeat suggestions in the prompt.
func recentForPage(ctx context.Context, db *sql.DB, userID int64, pageSlug string, days int) ([]Suggestion, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, generated_at, page_slug, source, type, size,
		       title, body_enc, status, feedback_enc, plan_enc, bead_id,
		       rejected_at, planned_at, bead_created_at
		FROM suggestions
		WHERE user_id = ? AND page_slug = ?
		  AND status IN (?, ?, ?)
		  AND generated_at >= ?
		ORDER BY generated_at DESC
	`, userID, pageSlug,
		StatusPending, StatusPlanned, StatusBeadCreated,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("recent suggestions: %w", err)
	}
	defer rows.Close()

	var out []Suggestion
	for rows.Next() {
		s, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// scanSuggestion decodes one row into a Suggestion, decrypting encrypted fields.
// Accepts either *sql.Row or *sql.Rows via the rowScanner interface.
func scanSuggestion(scanner rowScanner) (Suggestion, error) {
	var (
		s             Suggestion
		generatedAt   string
		bodyEnc       string
		feedbackEnc   sql.NullString
		planEnc       sql.NullString
		beadID        sql.NullString
		rejectedAt    sql.NullString
		plannedAt     sql.NullString
		beadCreatedAt sql.NullString
	)
	if err := scanner.Scan(
		&s.ID, &s.UserID, &generatedAt, &s.PageSlug, &s.Source, &s.Type, &s.Size,
		&s.Title, &bodyEnc, &s.Status, &feedbackEnc, &planEnc, &beadID,
		&rejectedAt, &plannedAt, &beadCreatedAt,
	); err != nil {
		return s, err
	}

	if t, err := parseTimestamp(generatedAt); err == nil {
		s.GeneratedAt = t
	}

	s.Body = decryptField(bodyEnc, "body", s.ID)
	if feedbackEnc.Valid {
		s.Feedback = decryptField(feedbackEnc.String, "feedback", s.ID)
	}
	if planEnc.Valid {
		s.Plan = decryptField(planEnc.String, "plan", s.ID)
	}
	if beadID.Valid {
		s.BeadID = beadID.String
	}
	if rejectedAt.Valid {
		if t, err := parseTimestamp(rejectedAt.String); err == nil {
			s.RejectedAt = &t
		}
	}
	if plannedAt.Valid {
		if t, err := parseTimestamp(plannedAt.String); err == nil {
			s.PlannedAt = &t
		}
	}
	if beadCreatedAt.Valid {
		if t, err := parseTimestamp(beadCreatedAt.String); err == nil {
			s.BeadCreatedAt = &t
		}
	}
	return s, nil
}

// rowScanner unifies *sql.Row and *sql.Rows for scanSuggestion.
type rowScanner interface {
	Scan(dest ...any) error
}

// decryptField returns the decrypted plaintext. On failure it logs a warning
// and returns the original ciphertext so the suggestion remains inspectable
// (the operator can see what was stored and recover it if needed). Legacy
// plaintext values (without the enc: prefix) are passed through transparently
// by encryption.DecryptField itself, so they never reach this error path.
func decryptField(value, field string, id int64) string {
	dec, err := encryption.DecryptField(value)
	if err != nil {
		log.Printf("suggestions: decrypt %s for id=%d failed (returning ciphertext): %v", field, id, err)
		return value
	}
	return dec
}

func nullableEncrypted(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time string")
	}
	return time.Parse(time.RFC3339, s)
}

func checkRowsAffected(res sql.Result, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no suggestion with id %d", id)
	}
	return nil
}
