package suggestions

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// Trigger values for the suggestion_runs.trigger column. They are also the
// only values accepted by the table's CHECK constraint.
const (
	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"
)

// DefaultRunsLimit is the page size GET /api/suggestions/runs uses when the
// caller omits ?limit. MaxRunsLimit is the hard cap the same handler enforces
// regardless of what the client requests.
const (
	DefaultRunsLimit = 20
	MaxRunsLimit     = 100
)

// SuggestionRun is the in-memory representation of a row in the
// suggestion_runs table. PageSlugs is stored as a CSV string in SQLite — it
// is exposed as a CSV string here too so the JSON shape mirrors the table
// columns exactly (the frontend splits on ',' itself).
type SuggestionRun struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Trigger    string     `json:"trigger"`
	PageSlugs  string     `json:"page_slugs"`
	Generated  int        `json:"generated"`
	Errors     int        `json:"errors"`
	CostUSD    float64    `json:"cost_usd"`
}

// InsertSuggestionRun persists a completed (or in-progress, when FinishedAt is
// nil) suggestion-run row. The caller is responsible for assembling the
// page_slugs CSV; this helper does no formatting beyond storing the supplied
// string verbatim. Timestamps are stored as RFC3339 strings to match the
// existing convention in this package (see store.go).
func InsertSuggestionRun(ctx context.Context, db *sql.DB, row SuggestionRun) (int64, error) {
	if row.Trigger != TriggerManual && row.Trigger != TriggerScheduled {
		return 0, fmt.Errorf("invalid trigger %q", row.Trigger)
	}
	var finishedAt any
	if row.FinishedAt != nil {
		finishedAt = row.FinishedAt.UTC().Format(time.RFC3339)
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO suggestion_runs
		    (user_id, started_at, finished_at, trigger, page_slugs, generated, errors, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.UserID,
		row.StartedAt.UTC().Format(time.RFC3339),
		finishedAt,
		row.Trigger,
		row.PageSlugs,
		row.Generated,
		row.Errors,
		row.CostUSD,
	)
	if err != nil {
		return 0, fmt.Errorf("insert suggestion_run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// ListSuggestionRuns returns the most recent suggestion-run rows for userID,
// newest-first. limit is clamped to [1, MaxRunsLimit] by the caller; this
// function applies the LIMIT directly without further validation.
func ListSuggestionRuns(ctx context.Context, db *sql.DB, userID int64, limit int) ([]SuggestionRun, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, started_at, finished_at, trigger, page_slugs, generated, errors, cost_usd
		FROM suggestion_runs
		WHERE user_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list suggestion_runs: %w", err)
	}
	defer rows.Close()

	var out []SuggestionRun
	for rows.Next() {
		var (
			r          SuggestionRun
			startedAt  string
			finishedAt sql.NullString
		)
		if err := rows.Scan(
			&r.ID,
			&r.UserID,
			&startedAt,
			&finishedAt,
			&r.Trigger,
			&r.PageSlugs,
			&r.Generated,
			&r.Errors,
			&r.CostUSD,
		); err != nil {
			return nil, fmt.Errorf("scan suggestion_run: %w", err)
		}
		if t, err := parseTimestamp(startedAt); err == nil {
			r.StartedAt = t
		}
		if finishedAt.Valid {
			if t, err := parseTimestamp(finishedAt.String); err == nil {
				r.FinishedAt = &t
			}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}

// BuildPageSlugsCSV joins page slugs with commas, appending NewPageSlug when
// the new-page pass also ran. Order is preserved so the operator can read the
// CSV as the order pages were processed.
func BuildPageSlugsCSV(pageSlugs []string, includedNewPage bool) string {
	all := pageSlugs
	if includedNewPage {
		all = append([]string{}, pageSlugs...)
		all = append(all, NewPageSlug)
	}
	return strings.Join(all, ",")
}

// RunsHandler returns the most recent suggestion-run rows for the requesting
// admin, newest-first. Admin-only — relies on auth.RequireAdmin upstream.
//
// GET /api/suggestions/runs?limit=20
// Response: JSON array of SuggestionRun, newest first.
func RunsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		limit := DefaultRunsLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
				return
			}
			limit = parsed
		}
		if limit < 1 {
			limit = 1
		}
		if limit > MaxRunsLimit {
			limit = MaxRunsLimit
		}

		runs, err := ListSuggestionRuns(r.Context(), db, user.ID, limit)
		if err != nil {
			log.Printf("suggestions: list runs for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
			return
		}

		if runs == nil {
			runs = []SuggestionRun{}
		}
		writeJSON(w, http.StatusOK, runs)
	}
}
