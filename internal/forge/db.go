package forge

import (
	"database/sql"
	"errors"
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
	ID          string     `json:"id"`
	BeadID      string     `json:"bead_id"`
	Anvil       string     `json:"anvil"`
	Branch      string     `json:"branch"`
	PID         int        `json:"pid"`
	Status      string     `json:"status"`
	Phase       string     `json:"phase"`
	Title       string     `json:"title"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	LogPath     string     `json:"log_path"`
	PRNumber    int        `json:"pr_number"`
}

// PR represents a pull request tracked by forge.
type PR struct {
	ID                   int        `json:"id"`
	Number               int        `json:"number"`
	Anvil                string     `json:"anvil"`
	BeadID               string     `json:"bead_id"`
	Branch               string     `json:"branch"`
	BaseBranch           string     `json:"base_branch"`
	Title                string     `json:"title"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	LastChecked          *time.Time `json:"last_checked,omitempty"`
	CIFixCount           int        `json:"ci_fix_count"`
	ReviewFixCount       int        `json:"review_fix_count"`
	CIPassing            bool       `json:"ci_passing"`
	RebaseCount          int        `json:"rebase_count"`
	IsConflicting        bool       `json:"is_conflicting"`
	HasUnresolvedThreads bool       `json:"has_unresolved_threads"`
	HasPendingReviews    bool       `json:"has_pending_reviews"`
	HasApproval          bool       `json:"has_approval"`
	BellowsManaged       bool       `json:"bellows_managed"`
}

// Event represents a forge event log entry.
type Event struct {
	ID        int       `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	BeadID    string    `json:"bead_id"`
	Anvil     string    `json:"anvil"`
}

// Retry represents a bead that needs human attention or has exceeded retry limits.
type Retry struct {
	BeadID              string     `json:"bead_id"`
	Anvil               string     `json:"anvil"`
	RetryCount          int        `json:"retry_count"`
	NextRetry           *time.Time `json:"next_retry,omitempty"`
	NeedsHuman          bool       `json:"needs_human"`
	ClarificationNeeded bool       `json:"clarification_needed"`
	LastError           string     `json:"last_error"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DispatchFailures    int        `json:"dispatch_failures"`
}

// CostSummary aggregates token usage and estimated cost over a period.
type CostSummary struct {
	Period        string  `json:"period"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	CacheRead     int64   `json:"cache_read"`
	CacheWrite    int64   `json:"cache_write"`
	EstimatedCost float64 `json:"estimated_cost"`
	CostLimit     float64 `json:"cost_limit"`
}

// DailyCostEntry holds per-day cost data for trend charts.
type DailyCostEntry struct {
	Date          string  `json:"date"`
	EstimatedCost float64 `json:"estimated_cost"`
	CostLimit     float64 `json:"cost_limit"`
}

// BeadCost holds aggregated cost data for a single bead.
type BeadCost struct {
	BeadID        string  `json:"bead_id"`
	EstimatedCost float64 `json:"estimated_cost"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	CacheRead     int64   `json:"cache_read"`
	CacheWrite    int64   `json:"cache_write"`
}

// QueueEntry represents a bead in the ready queue for a given anvil.
type QueueEntry struct {
	BeadID      string    `json:"bead_id"`
	Anvil       string    `json:"anvil"`
	Title       string    `json:"title"`
	Priority    int       `json:"priority"`
	Status      string    `json:"status"`
	Labels      string    `json:"labels"`
	Section     string    `json:"section"`
	Assignee    string    `json:"assignee"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
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

	// Open read-only. WAL mode is a property of the database file set by the
	// writer; it cannot be configured on a read-only connection and is
	// intentionally not specified here. The connection inherits whatever
	// journal mode the writer has established.
	dsn := fmt.Sprintf("file:%s?mode=rw", path)
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
		       AND datetime(completed_at) >= datetime(?))
		ORDER BY datetime(started_at) DESC
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
		WHERE status NOT IN ('merged', 'closed')
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

// GetPRByID returns a single PR by its database ID, or nil if not found.
func (d *DB) GetPRByID(id int) (*PR, error) {
	const q = `
		SELECT id, number, anvil, bead_id, branch, base_branch, title, status,
		       created_at, last_checked,
		       ci_fix_count, review_fix_count, ci_passing, rebase_count,
		       is_conflicting, has_unresolved_threads, has_pending_reviews,
		       has_approval, bellows_managed
		FROM prs
		WHERE id = ?
	`
	var p PR
	var createdAt, lastChecked sql.NullString
	var ciPassing, isConflicting, hasUnresolvedThreads, hasPendingReviews, hasApproval, bellowsManaged int
	err := d.db.QueryRow(q, id).Scan(
		&p.ID, &p.Number, &p.Anvil, &p.BeadID, &p.Branch, &p.BaseBranch, &p.Title, &p.Status,
		&createdAt, &lastChecked,
		&p.CIFixCount, &p.ReviewFixCount, &ciPassing, &p.RebaseCount,
		&isConflicting, &hasUnresolvedThreads, &hasPendingReviews,
		&hasApproval, &bellowsManaged,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("forge: get pr by id: %w", err)
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
	return &p, nil
}

// ClosedPRs returns the last N merged or closed pull requests per anvil,
// ordered by last_checked descending (the most recent polling timestamp).
// No separate completion timestamp is stored; callers should treat last_checked
// as an approximation of when the PR was last observed closed/merged.
// perAnvil controls how many rows per anvil (default 5).
func (d *DB) ClosedPRs(perAnvil int) ([]PR, error) {
	if perAnvil <= 0 {
		perAnvil = 5
	}
	const q = `
		SELECT id, number, anvil, bead_id, branch, base_branch, title, status,
		       created_at, last_checked,
		       ci_fix_count, review_fix_count, ci_passing, rebase_count,
		       is_conflicting, has_unresolved_threads, has_pending_reviews,
		       has_approval, bellows_managed
		FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY anvil ORDER BY last_checked DESC) AS rn
			FROM prs
			WHERE status IN ('merged', 'closed')
		)
		WHERE rn <= ?
		ORDER BY anvil, last_checked DESC
	`
	rows, err := d.db.Query(q, perAnvil)
	if err != nil {
		return nil, fmt.Errorf("forge: closed_prs query: %w", err)
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
			return nil, fmt.Errorf("forge: closed_prs scan: %w", err)
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
		return nil, fmt.Errorf("forge: closed_prs rows: %w", err)
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

// RetryByBeadID returns the retry record for a given bead, or sql.ErrNoRows
// if no retry entry exists. Used by handlers that need the anvil name to
// invoke CLI commands.
func (d *DB) RetryByBeadID(beadID string) (*Retry, error) {
	const q = `
		SELECT bead_id, anvil, retry_count, next_retry,
		       needs_human, clarification_needed, last_error, updated_at,
		       dispatch_failures
		FROM retries
		WHERE bead_id = ?
	`
	var r Retry
	var nextRetry, updatedAt sql.NullString
	var needsHuman, clarificationNeeded int
	err := d.db.QueryRow(q, beadID).Scan(
		&r.BeadID, &r.Anvil, &r.RetryCount, &nextRetry,
		&needsHuman, &clarificationNeeded, &r.LastError, &updatedAt,
		&r.DispatchFailures,
	)
	if err != nil {
		return nil, err
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
	return &r, nil
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

// QueueAll returns all beads from the queue cache across all sections.
// Unlike QueueCache, this is not limited to section='ready' and includes
// unlabeled, in-progress, and needs-attention beads.
func (d *DB) QueueAll() ([]QueueEntry, error) {
	const q = `
		SELECT bead_id, anvil, title, priority, status, labels, section,
		       assignee, description, updated_at
		FROM queue_cache
		ORDER BY anvil, section, priority ASC, updated_at ASC
	`
	rows, err := d.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("forge: queue_cache all query: %w", err)
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
			return nil, fmt.Errorf("forge: queue_cache all scan: %w", err)
		}
		if t, err := parseTime(updatedAt); err == nil {
			e.UpdatedAt = t
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: queue_cache all rows: %w", err)
	}
	return entries, nil
}

// QueueEntryByBeadID returns the queue_cache row for a given bead ID,
// or sql.ErrNoRows if the bead is not in the queue.
func (d *DB) QueueEntryByBeadID(beadID string) (*QueueEntry, error) {
	const q = `
		SELECT bead_id, anvil, title, priority, status, labels, section,
		       assignee, description, updated_at
		FROM queue_cache
		WHERE bead_id = ?
		LIMIT 1
	`
	var e QueueEntry
	var updatedAt string
	err := d.db.QueryRow(q, beadID).Scan(
		&e.BeadID, &e.Anvil, &e.Title, &e.Priority, &e.Status, &e.Labels, &e.Section,
		&e.Assignee, &e.Description, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("forge: queue_cache by bead id scan: %w", err)
	}
	if t, err := parseTime(updatedAt); err == nil {
		e.UpdatedAt = t
	}
	return &e, nil
}

// WorkerByID returns a single worker by its ID, or an error if not found.
func (d *DB) WorkerByID(id string) (*Worker, error) {
	const q = `
		SELECT id, bead_id, anvil, branch, pid, status, phase, title,
		       started_at, completed_at, updated_at, log_path, pr_number
		FROM workers
		WHERE id = ?
	`
	var w Worker
	var startedAt, completedAt, updatedAt sql.NullString
	err := d.db.QueryRow(q, id).Scan(
		&w.ID, &w.BeadID, &w.Anvil, &w.Branch, &w.PID,
		&w.Status, &w.Phase, &w.Title,
		&startedAt, &completedAt, &updatedAt,
		&w.LogPath, &w.PRNumber,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("forge: worker %q not found: %w", id, sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("forge: worker query: %w", err)
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
	return &w, nil
}

// PRByID returns a single pull request by its numeric ID, or an error if not found.
func (d *DB) PRByID(id int) (*PR, error) {
	const q = `
		SELECT id, number, anvil, bead_id, branch, base_branch, title, status,
		       created_at, last_checked,
		       ci_fix_count, review_fix_count, ci_passing, rebase_count,
		       is_conflicting, has_unresolved_threads, has_pending_reviews,
		       has_approval, bellows_managed
		FROM prs
		WHERE id = ?
	`
	var p PR
	var createdAt, lastChecked sql.NullString
	var ciPassing, isConflicting, hasUnresolvedThreads, hasPendingReviews, hasApproval, bellowsManaged int
	err := d.db.QueryRow(q, id).Scan(
		&p.ID, &p.Number, &p.Anvil, &p.BeadID, &p.Branch, &p.BaseBranch, &p.Title, &p.Status,
		&createdAt, &lastChecked,
		&p.CIFixCount, &p.ReviewFixCount, &ciPassing, &p.RebaseCount,
		&isConflicting, &hasUnresolvedThreads, &hasPendingReviews,
		&hasApproval, &bellowsManaged,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("forge: PR %d not found: %w", id, sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("forge: PR query: %w", err)
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
	return &p, nil
}

// PRByBeadID returns the most recent PR associated with a bead, or nil if none
// exists. Used to populate PRID in IPC payloads.
func (d *DB) PRByBeadID(beadID string) (*PR, error) {
	const q = `
		SELECT id, number, anvil, bead_id, branch, base_branch, title, status,
		       created_at, last_checked,
		       ci_fix_count, review_fix_count, ci_passing, rebase_count,
		       is_conflicting, has_unresolved_threads, has_pending_reviews,
		       has_approval, bellows_managed
		FROM prs
		WHERE bead_id = ?
		ORDER BY id DESC
		LIMIT 1
	`
	var p PR
	var createdAt, lastChecked sql.NullString
	var ciPassing, isConflicting, hasUnresolvedThreads, hasPendingReviews, hasApproval, bellowsManaged int
	err := d.db.QueryRow(q, beadID).Scan(
		&p.ID, &p.Number, &p.Anvil, &p.BeadID, &p.Branch, &p.BaseBranch, &p.Title, &p.Status,
		&createdAt, &lastChecked,
		&p.CIFixCount, &p.ReviewFixCount, &ciPassing, &p.RebaseCount,
		&isConflicting, &hasUnresolvedThreads, &hasPendingReviews,
		&hasApproval, &bellowsManaged,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("forge: PR by bead query for bead %q: %w", beadID, err)
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
	return &p, nil
}

// EventsSince returns events with ID greater than lastID, ordered oldest-first.
// limit controls how many rows are returned (0 means 100).
func (d *DB) EventsSince(lastID int, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT id, timestamp, type, message, bead_id, anvil
		FROM events
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?
	`
	rows, err := d.db.Query(q, lastID, limit)
	if err != nil {
		return nil, fmt.Errorf("forge: events_since query: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Type, &e.Message, &e.BeadID, &e.Anvil); err != nil {
			return nil, fmt.Errorf("forge: events_since scan: %w", err)
		}
		if t, err := parseTime(ts); err == nil {
			e.Timestamp = t
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: events_since rows: %w", err)
	}
	return events, nil
}

// CostTrend returns per-day cost entries for the last `days` days (max 90).
// Results are ordered oldest-first so they can be fed directly into a chart.
func (d *DB) CostTrend(days int) ([]DailyCostEntry, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	const q = `
		SELECT date,
		       COALESCE(SUM(estimated_cost), 0.0),
		       COALESCE(SUM(cost_limit), 0.0)
		FROM daily_costs
		WHERE date >= ?
		GROUP BY date
		ORDER BY date ASC
	`
	rows, err := d.db.Query(q, since)
	if err != nil {
		return nil, fmt.Errorf("forge: cost_trend query: %w", err)
	}
	defer rows.Close()

	entries := []DailyCostEntry{}
	for rows.Next() {
		var e DailyCostEntry
		if err := rows.Scan(&e.Date, &e.EstimatedCost, &e.CostLimit); err != nil {
			return nil, fmt.Errorf("forge: cost_trend scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: cost_trend rows: %w", err)
	}
	return entries, nil
}

// tableExists reports whether the named table is present in the SQLite database.
func (d *DB) tableExists(name string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("forge: tableExists(%q): %w", name, err)
	}
	return count > 0, nil
}

// TopBeadCosts returns the most expensive beads in the last `days` days.
// If the bead_costs table does not exist (older forge versions), returns an
// empty slice rather than an error so the dashboard degrades gracefully.
func (d *DB) TopBeadCosts(days, limit int) ([]BeadCost, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if limit <= 0 {
		limit = 5
	}
	// Check for table existence before querying to avoid brittle error-string matching.
	exists, err := d.tableExists("bead_costs")
	if err != nil {
		return nil, err
	}
	if !exists {
		return []BeadCost{}, nil
	}
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	const q = `
		SELECT bead_id,
		       COALESCE(SUM(estimated_cost), 0.0),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cache_read), 0),
		       COALESCE(SUM(cache_write), 0)
		FROM bead_costs
		WHERE date(updated_at) >= ?
		GROUP BY bead_id
		ORDER BY SUM(estimated_cost) DESC
		LIMIT ?
	`
	rows, err := d.db.Query(q, since, limit)
	if err != nil {
		return nil, fmt.Errorf("forge: top_bead_costs query: %w", err)
	}
	defer rows.Close()

	beads := []BeadCost{}
	for rows.Next() {
		var b BeadCost
		if err := rows.Scan(&b.BeadID, &b.EstimatedCost, &b.InputTokens, &b.OutputTokens, &b.CacheRead, &b.CacheWrite); err != nil {
			return nil, fmt.Errorf("forge: top_bead_costs scan: %w", err)
		}
		beads = append(beads, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forge: top_bead_costs rows: %w", err)
	}
	return beads, nil
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
