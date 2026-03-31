package forge

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a read-only connection to the forge state database.
type DB struct {
	db *sql.DB
}

// Worker represents an active or recently completed forge worker.
type Worker struct {
	ID          string
	BeadID      string
	Anvil       string
	Branch      string
	PID         int
	Status      string
	Phase       string
	Title       string
	StartedAt   time.Time
	CompletedAt *time.Time
	UpdatedAt   *time.Time
	LogPath     string
	PRNumber    int
}

// PR represents a pull request tracked by forge.
type PR struct {
	ID                   int
	Number               int
	Anvil                string
	BeadID               string
	Branch               string
	BaseBranch           string
	Title                string
	Status               string
	CreatedAt            time.Time
	LastChecked          *time.Time
	CIFixCount           int
	ReviewFixCount       int
	CIPassing            bool
	RebaseCount          int
	IsConflicting        bool
	HasUnresolvedThreads bool
	HasPendingReviews    bool
	HasApproval          bool
	BellowsManaged       bool
}

// Event represents a forge event log entry.
type Event struct {
	ID        int
	Timestamp time.Time
	Type      string
	Message   string
	BeadID    string
	Anvil     string
}

// Retry represents a bead that needs human attention or has exceeded retry limits.
type Retry struct {
	BeadID               string
	Anvil                string
	RetryCount           int
	NextRetry            *time.Time
	NeedsHuman           bool
	ClarificationNeeded  bool
	LastError            string
	UpdatedAt            time.Time
	DispatchFailures     int
}

// CostSummary aggregates token usage and estimated cost over a period.
type CostSummary struct {
	Period        string
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheWrite    int64
	EstimatedCost float64
	CostLimit     float64
}

// QueueEntry represents a bead in the ready queue for a given anvil.
type QueueEntry struct {
	BeadID      string
	Anvil       string
	Title       string
	Priority    int
	Status      string
	Labels      string
	Section     string
	Assignee    string
	Description string
	UpdatedAt   time.Time
}

// Open opens the forge state database in read-only mode.
// It reads the path from the FORGE_STATE_DB environment variable,
// falling back to ~/.forge/state.db.
func Open() (*DB, error) {
	path := os.Getenv("FORGE_STATE_DB")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("forge: resolve home directory: %w", err)
		}
		path = filepath.Join(home, ".forge", "state.db")
	}

	// Open read-only; WAL mode is a property of the database file set by the
	// writer — we must not attempt to set it on a read-only connection.
	dsn := fmt.Sprintf("file:%s?mode=ro", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("forge: open state.db: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("forge: ping state.db: %w", err)
	}

	return &DB{db: db}, nil
}

// New wraps an existing *sql.DB as a forge DB. Intended for testing.
func New(db *sql.DB) *DB {
	return &DB{db: db}
}

// Close releases the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Workers returns active workers (status pending or running) and workers that
// completed within the last 24 hours.
func (d *DB) Workers() ([]Worker, error) {
	const q = `
		SELECT id, bead_id, anvil, branch, pid, status, phase, title,
		       started_at, completed_at, updated_at, log_path, pr_number
		FROM workers
		WHERE status IN ('pending', 'running')
		   OR (status IN ('done', 'failed', 'cancelled')
		       AND completed_at >= ?)
		ORDER BY started_at DESC
	`
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	rows, err := d.db.Query(q, cutoff)
	if err != nil {
		return nil, fmt.Errorf("forge: workers query: %w", err)
	}
	defer rows.Close()

	workers := []Worker{}
	for rows.Next() {
		var w Worker
		var startedAt, completedAt, updatedAt sql.NullString
		if err := rows.Scan(
			&w.ID, &w.BeadID, &w.Anvil, &w.Branch, &w.PID,
			&w.Status, &w.Phase, &w.Title,
			&startedAt, &completedAt, &updatedAt,
			&w.LogPath, &w.PRNumber,
		); err != nil {
			return nil, fmt.Errorf("forge: workers scan: %w", err)
		}
		if startedAt.Valid {
			if t, err := parseTime(startedAt.String); err == nil {
				w.StartedAt = t
			}
		}
		if completedAt.Valid {
			if t, err := parseTime(completedAt.String); err == nil {
				w.CompletedAt = &t
			}
		}
		if updatedAt.Valid {
			if t, err := parseTime(updatedAt.String); err == nil {
				w.UpdatedAt = &t
			}
		}
		workers = append(workers, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: workers rows: %w", err)
	}
	return workers, nil
}

// PRs returns open pull requests and those that are merge-ready (CI passing,
// approved, not conflicting, no unresolved threads).
func (d *DB) PRs() ([]PR, error) {
	const q = `
		SELECT id, number, anvil, bead_id, branch, base_branch, title, status,
		       created_at, last_checked,
		       ci_fix_count, review_fix_count, ci_passing, rebase_count,
		       is_conflicting, has_unresolved_threads, has_pending_reviews,
		       has_approval, bellows_managed
		FROM prs
		WHERE status = 'open'
		ORDER BY created_at DESC
	`
	rows, err := d.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("forge: prs query: %w", err)
	}
	defer rows.Close()

	prs := []PR{}
	for rows.Next() {
		var p PR
		var createdAt, lastChecked sql.NullString
		var ciPassing, isConflicting, hasUnresolvedThreads, hasPendingReviews, hasApproval, bellowsManaged int
		if err := rows.Scan(
			&p.ID, &p.Number, &p.Anvil, &p.BeadID, &p.Branch, &p.BaseBranch, &p.Title, &p.Status,
			&createdAt, &lastChecked,
			&p.CIFixCount, &p.ReviewFixCount, &ciPassing, &p.RebaseCount,
			&isConflicting, &hasUnresolvedThreads, &hasPendingReviews,
			&hasApproval, &bellowsManaged,
		); err != nil {
			return nil, fmt.Errorf("forge: prs scan: %w", err)
		}
		p.CIPassing = ciPassing != 0
		p.IsConflicting = isConflicting != 0
		p.HasUnresolvedThreads = hasUnresolvedThreads != 0
		p.HasPendingReviews = hasPendingReviews != 0
		p.HasApproval = hasApproval != 0
		p.BellowsManaged = bellowsManaged != 0
		if createdAt.Valid {
			if t, err := parseTime(createdAt.String); err == nil {
				p.CreatedAt = t
			}
		}
		if lastChecked.Valid {
			if t, err := parseTime(lastChecked.String); err == nil {
				p.LastChecked = &t
			}
		}
		prs = append(prs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: prs rows: %w", err)
	}
	return prs, nil
}

// Events returns recent events, optionally filtered by type and/or anvil.
// limit controls how many rows are returned (0 means 50).
func (d *DB) Events(limit int, eventType, anvil string) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}

	var conditions []string
	var args []any
	if eventType != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, eventType)
	}
	if anvil != "" {
		conditions = append(conditions, "anvil = ?")
		args = append(args, anvil)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	q := fmt.Sprintf(`
		SELECT id, timestamp, type, message, bead_id, anvil
		FROM events
		%s
		ORDER BY timestamp DESC
		LIMIT ?
	`, where)
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("forge: events query: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Type, &e.Message, &e.BeadID, &e.Anvil); err != nil {
			return nil, fmt.Errorf("forge: events scan: %w", err)
		}
		if t, err := parseTime(ts); err == nil {
			e.Timestamp = t
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: events rows: %w", err)
	}
	return events, nil
}

// Retries returns all retry rows where needs_human is set.
func (d *DB) Retries() ([]Retry, error) {
	const q = `
		SELECT bead_id, anvil, retry_count, next_retry,
		       needs_human, clarification_needed, last_error, updated_at,
		       dispatch_failures
		FROM retries
		WHERE needs_human = 1
		ORDER BY updated_at DESC
	`
	rows, err := d.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("forge: retries query: %w", err)
	}
	defer rows.Close()

	retries := []Retry{}
	for rows.Next() {
		var r Retry
		var nextRetry, updatedAt sql.NullString
		var needsHuman, clarificationNeeded int
		if err := rows.Scan(
			&r.BeadID, &r.Anvil, &r.RetryCount, &nextRetry,
			&needsHuman, &clarificationNeeded, &r.LastError, &updatedAt,
			&r.DispatchFailures,
		); err != nil {
			return nil, fmt.Errorf("forge: retries scan: %w", err)
		}
		r.NeedsHuman = needsHuman != 0
		r.ClarificationNeeded = clarificationNeeded != 0
		if nextRetry.Valid {
			if t, err := parseTime(nextRetry.String); err == nil {
				r.NextRetry = &t
			}
		}
		if updatedAt.Valid {
			if t, err := parseTime(updatedAt.String); err == nil {
				r.UpdatedAt = t
			}
		}
		retries = append(retries, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: retries rows: %w", err)
	}
	return retries, nil
}

// Costs returns an aggregated cost summary for the given period.
// period may be "today", "week", or "month". Any other value defaults to "today".
// The summary is derived from the daily_costs table.
func (d *DB) Costs(period string) (*CostSummary, error) {
	now := time.Now().UTC()
	var since string
	switch period {
	case "week":
		since = now.AddDate(0, 0, -6).Format("2006-01-02")
	case "month":
		since = now.AddDate(0, 0, -29).Format("2006-01-02")
	default:
		period = "today"
		since = now.Format("2006-01-02")
	}

	const q = `
		SELECT
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_read), 0),
			COALESCE(SUM(cache_write), 0),
			COALESCE(SUM(estimated_cost), 0.0),
			COALESCE(SUM(cost_limit), 0.0)
		FROM daily_costs
		WHERE date >= ?
	`

	var s CostSummary
	s.Period = period
	err := d.db.QueryRow(q, since).Scan(
		&s.InputTokens, &s.OutputTokens,
		&s.CacheRead, &s.CacheWrite,
		&s.EstimatedCost, &s.CostLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("forge: costs query: %w", err)
	}
	return &s, nil
}

// QueueCache returns all ready beads grouped by anvil.
// Only rows with section='ready' are included.
func (d *DB) QueueCache() ([]QueueEntry, error) {
	const q = `
		SELECT bead_id, anvil, title, priority, status, labels, section,
		       assignee, description, updated_at
		FROM queue_cache
		WHERE section = 'ready'
		ORDER BY anvil, priority ASC, updated_at ASC
	`
	rows, err := d.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("forge: queue_cache query: %w", err)
	}
	defer rows.Close()

	entries := []QueueEntry{}
	for rows.Next() {
		var e QueueEntry
		var updatedAt string
		if err := rows.Scan(
			&e.BeadID, &e.Anvil, &e.Title, &e.Priority, &e.Status, &e.Labels, &e.Section,
			&e.Assignee, &e.Description, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("forge: queue_cache scan: %w", err)
		}
		if t, err := parseTime(updatedAt); err == nil {
			e.UpdatedAt = t
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: queue_cache rows: %w", err)
	}
	return entries, nil
}

// parseTime parses a SQLite timestamp string into a time.Time.
// SQLite stores timestamps as RFC3339 or "2006-01-02 15:04:05" strings.
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("forge: cannot parse time %q", s)
}
