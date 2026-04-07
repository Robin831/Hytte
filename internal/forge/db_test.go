package forge

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
		CREATE TABLE workers (
			id TEXT PRIMARY KEY,
			bead_id TEXT,
			anvil TEXT,
			branch TEXT,
			pid INTEGER,
			status TEXT,
			phase TEXT,
			title TEXT,
			started_at TEXT,
			completed_at TEXT,
			updated_at TEXT,
			log_path TEXT,
			pr_number INTEGER
		);
		CREATE TABLE prs (
			id INTEGER PRIMARY KEY,
			number INTEGER,
			anvil TEXT,
			bead_id TEXT,
			branch TEXT,
			base_branch TEXT,
			title TEXT,
			status TEXT,
			created_at TEXT,
			last_checked TEXT,
			ci_fix_count INTEGER,
			review_fix_count INTEGER,
			ci_passing INTEGER,
			rebase_count INTEGER,
			is_conflicting INTEGER,
			has_unresolved_threads INTEGER,
			has_pending_reviews INTEGER,
			has_approval INTEGER,
			bellows_managed INTEGER
		);
		CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			timestamp TEXT,
			type TEXT,
			message TEXT,
			bead_id TEXT,
			anvil TEXT
		);
		CREATE TABLE retries (
			bead_id TEXT PRIMARY KEY,
			anvil TEXT,
			retry_count INTEGER,
			next_retry TEXT,
			needs_human INTEGER,
			clarification_needed INTEGER,
			last_error TEXT,
			updated_at TEXT,
			dispatch_failures INTEGER
		);
		CREATE TABLE daily_costs (
			date TEXT,
			input_tokens INTEGER,
			output_tokens INTEGER,
			cache_read INTEGER,
			cache_write INTEGER,
			estimated_cost REAL,
			cost_limit REAL
		);
		CREATE TABLE queue_cache (
			bead_id TEXT PRIMARY KEY,
			anvil TEXT,
			title TEXT,
			priority INTEGER,
			status TEXT,
			labels TEXT,
			section TEXT,
			assignee TEXT,
			description TEXT,
			updated_at TEXT
		);
		CREATE TABLE bead_costs (
			bead_id TEXT NOT NULL,
			anvil TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read INTEGER NOT NULL DEFAULT 0,
			cache_write INTEGER NOT NULL DEFAULT 0,
			estimated_cost REAL NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (bead_id, anvil)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return New(db)
}

func TestWorkers_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	workers, err := fdb.Workers()
	if err != nil {
		t.Fatalf("Workers: %v", err)
	}
	if workers == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(workers))
	}
}

func TestWorkers_ActiveAndRecent(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC()
	recentDone := now.Add(-1 * time.Hour).Format(time.RFC3339)
	oldDone := now.Add(-48 * time.Hour).Format(time.RFC3339)
	startedAt := now.Add(-2 * time.Hour).Format(time.RFC3339)

	_, err := fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number)
		VALUES
		  ('w1', 'b1', 'anvil1', 'feat/b1', 1001, 'running', 'impl', 'Running bead', ?, NULL, NULL, '/log/w1', 0),
		  ('w2', 'b2', 'anvil1', 'feat/b2', 1002, 'done', 'done', 'Recent done', ?, ?, NULL, '/log/w2', 42),
		  ('w3', 'b3', 'anvil1', 'feat/b3', 1003, 'done', 'done', 'Old done', ?, ?, NULL, '/log/w3', 0)
	`, startedAt, startedAt, recentDone, oldDone, oldDone)
	if err != nil {
		t.Fatalf("insert workers: %v", err)
	}

	workers, err := fdb.Workers()
	if err != nil {
		t.Fatalf("Workers: %v", err)
	}
	if len(workers) != 2 {
		t.Errorf("expected 2 workers (running + recent done), got %d", len(workers))
	}

	// Verify the running worker fields
	var runningFound bool
	for _, w := range workers {
		if w.ID == "w1" {
			runningFound = true
			if w.Status != "running" {
				t.Errorf("expected status 'running', got %q", w.Status)
			}
			if w.PRNumber != 0 {
				t.Errorf("expected pr_number 0, got %d", w.PRNumber)
			}
		}
		if w.ID == "w2" && w.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", w.PRNumber)
		}
	}
	if !runningFound {
		t.Error("running worker w1 not found in results")
	}
}

func TestPRs_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	prs, err := fdb.PRs()
	if err != nil {
		t.Fatalf("PRs: %v", err)
	}
	if prs == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestPRs_OpenOnly(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at, last_checked,
		                 ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		                 has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES
		  (1, 10, 'anvil1', 'b1', 'feat/b1', 'main', 'Open PR', 'open', ?, NULL, 0, 0, 1, 0, 0, 0, 0, 1, 0),
		  (2, 11, 'anvil1', 'b2', 'feat/b2', 'main', 'Merged PR', 'merged', ?, NULL, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, now, now)
	if err != nil {
		t.Fatalf("insert prs: %v", err)
	}

	prs, err := fdb.PRs()
	if err != nil {
		t.Fatalf("PRs: %v", err)
	}
	if len(prs) != 1 {
		t.Errorf("expected 1 open PR, got %d", len(prs))
	}
	if prs[0].Number != 10 {
		t.Errorf("expected PR number 10, got %d", prs[0].Number)
	}
	if !prs[0].CIPassing {
		t.Error("expected ci_passing=true")
	}
	if !prs[0].HasApproval {
		t.Error("expected has_approval=true")
	}
}

func TestEvents_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	events, err := fdb.Events(0, "", "")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestEvents_FilterByType(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES
		  (1, ?, 'worker_start', 'Worker started', 'b1', 'anvil1'),
		  (2, ?, 'pr_opened', 'PR opened', 'b1', 'anvil1'),
		  (3, ?, 'worker_start', 'Worker started', 'b2', 'anvil2')
	`, now, now, now)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	events, err := fdb.Events(10, "worker_start", "")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 worker_start events, got %d", len(events))
	}

	events, err = fdb.Events(10, "worker_start", "anvil2")
	if err != nil {
		t.Fatalf("Events with anvil filter: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event for anvil2, got %d", len(events))
	}
}

func TestRetries_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	retries, err := fdb.Retries()
	if err != nil {
		t.Fatalf("Retries: %v", err)
	}
	if retries == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestRetries_NeedsHumanOnly(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures) VALUES
		  ('b1', 'anvil1', 3, NULL, 1, 0, 'timed out', ?, 0),
		  ('b2', 'anvil1', 1, NULL, 0, 0, '', ?, 0)
	`, now, now)
	if err != nil {
		t.Fatalf("insert retries: %v", err)
	}

	retries, err := fdb.Retries()
	if err != nil {
		t.Fatalf("Retries: %v", err)
	}
	if len(retries) != 1 {
		t.Errorf("expected 1 needs_human retry, got %d", len(retries))
	}
	if retries[0].BeadID != "b1" {
		t.Errorf("expected bead_id 'b1', got %q", retries[0].BeadID)
	}
	if !retries[0].NeedsHuman {
		t.Error("expected needs_human=true")
	}
}

func TestStuckPRs(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// CI exhausted: ci_fix_count=5, ci_passing=0
	if _, err := fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES (1, 10, 'a', 'ci-stuck', 'feat/ci', 'main', 'CI', 'open', ?, ?, 5, 0, 0, 0, 0, 0, 0, 0, 0)
	`, now, now); err != nil {
		t.Fatalf("insert ci-stuck PR: %v", err)
	}

	// Review exhausted: review_fix_count=6, has_unresolved_threads=1
	if _, err := fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES (2, 11, 'a', 'rev-stuck', 'feat/rev', 'main', 'Rev', 'open', ?, ?, 0, 6, 1, 0, 0, 1, 0, 0, 0)
	`, now, now); err != nil {
		t.Fatalf("insert rev-stuck PR: %v", err)
	}

	// Healthy PR — below thresholds
	if _, err := fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES (3, 12, 'a', 'ok', 'feat/ok', 'main', 'OK', 'open', ?, ?, 2, 1, 1, 0, 0, 0, 0, 0, 0)
	`, now, now); err != nil {
		t.Fatalf("insert healthy PR: %v", err)
	}

	// Merged PR with high counts — should NOT appear (status != 'open')
	if _, err := fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES (4, 13, 'a', 'merged', 'feat/m', 'main', 'Merged', 'merged', ?, ?, 5, 5, 0, 0, 0, 1, 0, 0, 0)
	`, now, now); err != nil {
		t.Fatalf("insert merged PR: %v", err)
	}

	stuck, err := fdb.StuckPRs(5, 5)
	if err != nil {
		t.Fatalf("StuckPRs: %v", err)
	}
	if len(stuck) != 2 {
		t.Fatalf("expected 2 stuck PRs, got %d", len(stuck))
	}

	byBead := map[string]Retry{}
	for _, s := range stuck {
		byBead[s.BeadID] = s
	}
	if _, ok := byBead["ci-stuck"]; !ok {
		t.Error("expected ci-stuck in results")
	}
	if _, ok := byBead["rev-stuck"]; !ok {
		t.Error("expected rev-stuck in results")
	}
	if !byBead["ci-stuck"].NeedsHuman {
		t.Error("expected NeedsHuman=true for ci-stuck")
	}
}

func TestCosts_NoData(t *testing.T) {
	fdb := setupTestDB(t)
	summary, err := fdb.Costs("today")
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.Period != "today" {
		t.Errorf("expected period 'today', got %q", summary.Period)
	}
	if summary.InputTokens != 0 || summary.EstimatedCost != 0 {
		t.Errorf("expected zero cost, got %+v", summary)
	}
}

func TestCosts_Aggregation(t *testing.T) {
	fdb := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	_, err := fdb.db.Exec(`
		INSERT INTO daily_costs (date, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, cost_limit) VALUES
		  (?, 1000, 500, 200, 100, 0.05, 1.0),
		  (?, 2000, 1000, 300, 150, 0.10, 1.0)
	`, today, today)
	if err != nil {
		t.Fatalf("insert daily_costs: %v", err)
	}

	summary, err := fdb.Costs("today")
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if summary.InputTokens != 3000 {
		t.Errorf("expected input_tokens 3000, got %d", summary.InputTokens)
	}
	if summary.OutputTokens != 1500 {
		t.Errorf("expected output_tokens 1500, got %d", summary.OutputTokens)
	}
}

func TestQueueCache_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	entries, err := fdb.QueueCache()
	if err != nil {
		t.Fatalf("QueueCache: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

// --- WorkerByID ---

func TestWorkerByID_Found(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number)
		VALUES ('w1', 'b1', 'anvil1', 'feat/b1', 1001, 'running', 'impl', 'My bead', ?, '/log/w1', 7)
	`, now)
	if err != nil {
		t.Fatalf("insert worker: %v", err)
	}

	w, err := fdb.WorkerByID("w1")
	if err != nil {
		t.Fatalf("WorkerByID: %v", err)
	}
	if w.ID != "w1" {
		t.Errorf("expected ID 'w1', got %q", w.ID)
	}
	if w.BeadID != "b1" {
		t.Errorf("expected bead_id 'b1', got %q", w.BeadID)
	}
	if w.Status != "running" {
		t.Errorf("expected status 'running', got %q", w.Status)
	}
	if w.PRNumber != 7 {
		t.Errorf("expected pr_number 7, got %d", w.PRNumber)
	}
	if w.LogPath != "/log/w1" {
		t.Errorf("expected log_path '/log/w1', got %q", w.LogPath)
	}
}

func TestWorkerByID_NotFound(t *testing.T) {
	fdb := setupTestDB(t)

	_, err := fdb.WorkerByID("no-such-worker")
	if err == nil {
		t.Fatal("expected error for missing worker, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected errors.Is(err, sql.ErrNoRows) to be true, got: %v", err)
	}
}

// --- EventsSince ---

func TestEventsSince_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	events, err := fdb.EventsSince(0, 50)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestEventsSince_ReturnsOnlyAfterID(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES
		  (1, ?, 'dispatch', 'first', 'b1', 'a'),
		  (2, ?, 'dispatch', 'second', 'b1', 'a'),
		  (3, ?, 'dispatch', 'third', 'b1', 'a')
	`, now, now, now)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	events, err := fdb.EventsSince(1, 50)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after id=1, got %d", len(events))
	}
	if events[0].ID != 2 {
		t.Errorf("expected first event id=2, got %d", events[0].ID)
	}
	if events[1].ID != 3 {
		t.Errorf("expected second event id=3, got %d", events[1].ID)
	}
}

func TestEventsSince_OrderedAscending(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES
		  (10, ?, 'a', 'msg10', 'b1', 'a'),
		  (5,  ?, 'b', 'msg5',  'b1', 'a'),
		  (20, ?, 'c', 'msg20', 'b1', 'a')
	`, now, now, now)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	events, err := fdb.EventsSince(0, 50)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// Should be ordered by id ASC.
	if events[0].ID >= events[1].ID || events[1].ID >= events[2].ID {
		t.Errorf("events not in ascending id order: %d, %d, %d", events[0].ID, events[1].ID, events[2].ID)
	}
}

func TestEventsSince_DefaultLimit(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	for i := 1; i <= 5; i++ {
		fdb.db.Exec(`INSERT INTO events (timestamp, type, message, bead_id, anvil) VALUES (?, 'test', 'msg', 'b1', 'a')`, now) //nolint:errcheck
	}

	// limit=0 should default to 100.
	events, err := fdb.EventsSince(0, 0)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
}

func TestQueueCache_ReadyOnly(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at) VALUES
		  ('b1', 'anvil1', 'Ready bead', 2, 'ready', '', 'ready', '', 'desc', ?),
		  ('b2', 'anvil1', 'Blocked bead', 1, 'blocked', '', 'blocked', '', 'desc', ?)
	`, now, now)
	if err != nil {
		t.Fatalf("insert queue_cache: %v", err)
	}

	entries, err := fdb.QueueCache()
	if err != nil {
		t.Fatalf("QueueCache: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 ready entry, got %d", len(entries))
	}
	if entries[0].BeadID != "b1" {
		t.Errorf("expected bead_id 'b1', got %q", entries[0].BeadID)
	}
}

// --- CostTrend ---

func TestCostTrend_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	entries, err := fdb.CostTrend(7)
	if err != nil {
		t.Fatalf("CostTrend: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestCostTrend_ReturnsRecentDays(t *testing.T) {
	fdb := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")

	_, err := fdb.db.Exec(`
		INSERT INTO daily_costs (date, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, cost_limit) VALUES
		  (?, 100, 50, 0, 0, 0.01, 1.0),
		  (?, 200, 100, 0, 0, 0.02, 1.0),
		  (?, 500, 200, 0, 0, 0.05, 1.0)
	`, old, yesterday, today)
	if err != nil {
		t.Fatalf("insert daily_costs: %v", err)
	}

	entries, err := fdb.CostTrend(7)
	if err != nil {
		t.Fatalf("CostTrend: %v", err)
	}
	// Only yesterday and today should be within the last 7 days.
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (yesterday + today), got %d", len(entries))
	}
	// Results should be oldest-first.
	if entries[0].Date != yesterday {
		t.Errorf("expected first entry date %q, got %q", yesterday, entries[0].Date)
	}
	if entries[1].Date != today {
		t.Errorf("expected second entry date %q, got %q", today, entries[1].Date)
	}
}

func TestCostTrend_DefaultsAndCap(t *testing.T) {
	fdb := setupTestDB(t)

	// days=0 should default to 7 (no panic, returns empty).
	entries, err := fdb.CostTrend(0)
	if err != nil {
		t.Fatalf("CostTrend(0): %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil slice for days=0")
	}

	// days > 90 should be capped at 90 (no panic).
	entries, err = fdb.CostTrend(200)
	if err != nil {
		t.Fatalf("CostTrend(200): %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil slice for days=200")
	}
}

// --- TopBeadCosts ---

func TestTopBeadCosts_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	beads, err := fdb.TopBeadCosts(7, 5)
	if err != nil {
		t.Fatalf("TopBeadCosts: %v", err)
	}
	if beads == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(beads) != 0 {
		t.Errorf("expected 0 beads, got %d", len(beads))
	}
}

func TestTopBeadCosts_OrderedByDescCost(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES
		  ('cheap-bead', 'hytte', 100, 50, 0, 0, 0.01, ?),
		  ('expensive-bead', 'hytte', 5000, 2000, 0, 0, 0.50, ?),
		  ('mid-bead', 'hytte', 1000, 500, 0, 0, 0.10, ?)
	`, now, now, now)
	if err != nil {
		t.Fatalf("insert bead_costs: %v", err)
	}

	beads, err := fdb.TopBeadCosts(7, 5)
	if err != nil {
		t.Fatalf("TopBeadCosts: %v", err)
	}
	if len(beads) != 3 {
		t.Fatalf("expected 3 beads, got %d", len(beads))
	}
	if beads[0].BeadID != "expensive-bead" {
		t.Errorf("expected most expensive bead first, got %q", beads[0].BeadID)
	}
	if beads[0].EstimatedCost != 0.50 {
		t.Errorf("expected cost 0.50, got %f", beads[0].EstimatedCost)
	}
}

func TestTopBeadCosts_LimitRespected(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range 6 {
		fdb.db.Exec(`INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES (?, 'hytte', 100, 50, 0, 0, ?, ?)`, //nolint:errcheck
			fmt.Sprintf("bead-%d", i), float64(i)*0.01, now)
	}

	beads, err := fdb.TopBeadCosts(7, 3)
	if err != nil {
		t.Fatalf("TopBeadCosts with limit=3: %v", err)
	}
	if len(beads) != 3 {
		t.Errorf("expected 3 beads (limit), got %d", len(beads))
	}
}

func TestTopBeadCosts_MissingTable(t *testing.T) {
	// Use a DB without the bead_costs table to verify graceful degradation.
	rawDB, err := sql.Open("sqlite", "file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	rawDB.SetMaxIdleConns(1)
	t.Cleanup(func() { rawDB.Close() })

	fdb := &DB{db: rawDB}
	beads, err := fdb.TopBeadCosts(7, 5)
	if err != nil {
		t.Fatalf("expected nil error for missing bead_costs table, got: %v", err)
	}
	if beads == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(beads) != 0 {
		t.Errorf("expected 0 beads, got %d", len(beads))
	}
}

func TestTopBeadCosts_ExcludesOldDates(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES
		  ('new-bead', 'hytte', 100, 50, 0, 0, 0.05, ?),
		  ('old-bead', 'hytte', 100, 50, 0, 0, 0.99, ?)
	`, now, old)
	if err != nil {
		t.Fatalf("insert bead_costs: %v", err)
	}

	beads, err := fdb.TopBeadCosts(7, 5)
	if err != nil {
		t.Fatalf("TopBeadCosts: %v", err)
	}
	if len(beads) != 1 {
		t.Errorf("expected 1 bead (within 7 days), got %d", len(beads))
	}
	if beads[0].BeadID != "new-bead" {
		t.Errorf("expected 'new-bead', got %q", beads[0].BeadID)
	}
}

// --- ClosedPRs ---

func TestClosedPRs_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	prs, err := fdb.ClosedPRs(5)
	if err != nil {
		t.Fatalf("ClosedPRs: %v", err)
	}
	if prs == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 prs, got %d", len(prs))
	}
}

func TestClosedPRs_ReturnsClosedAndMerged(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	olderStr := now.Add(-1 * time.Hour).Format(time.RFC3339)

	_, err := fdb.db.Exec(`
		INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at, last_checked,
		                 ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		                 has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES
		  (1, 10, 'anvil1', 'b1', 'feat/b1', 'main', 'Open PR',   'open',   ?, NULL, 0, 0, 1, 0, 0, 0, 0, 0, 0),
		  (2, 11, 'anvil1', 'b2', 'feat/b2', 'main', 'Merged PR',  'merged', ?, ?,    0, 0, 0, 0, 0, 0, 0, 0, 0),
		  (3, 12, 'anvil1', 'b3', 'feat/b3', 'main', 'Closed PR',  'closed', ?, ?,    0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, nowStr, nowStr, nowStr, nowStr, olderStr)
	if err != nil {
		t.Fatalf("insert prs: %v", err)
	}

	prs, err := fdb.ClosedPRs(5)
	if err != nil {
		t.Fatalf("ClosedPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 closed/merged PRs, got %d", len(prs))
	}
	// Ordered by last_checked DESC within anvil — merged PR (nowStr) should come first.
	if prs[0].Number != 11 {
		t.Errorf("expected first PR number 11 (merged, most recent), got %d", prs[0].Number)
	}
	if prs[1].Number != 12 {
		t.Errorf("expected second PR number 12 (closed, older), got %d", prs[1].Number)
	}
}

func TestClosedPRs_PerAnvilLimit(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC()
	// Insert 4 merged PRs for anvil1 and 2 for anvil2.
	for i := 0; i < 4; i++ {
		ts := now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339)
		fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at, last_checked,
			ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
			has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
			VALUES (?, ?, 'anvil1', ?, 'feat/x', 'main', 'PR', 'merged', ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0)`,
			i+1, i+100, fmt.Sprintf("b%d", i), ts, ts) //nolint:errcheck
	}
	for i := 0; i < 2; i++ {
		ts := now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339)
		fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at, last_checked,
			ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
			has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
			VALUES (?, ?, 'anvil2', ?, 'feat/y', 'main', 'PR', 'closed', ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0)`,
			i+10, i+200, fmt.Sprintf("c%d", i), ts, ts) //nolint:errcheck
	}

	prs, err := fdb.ClosedPRs(2)
	if err != nil {
		t.Fatalf("ClosedPRs: %v", err)
	}
	// Should get 2 from anvil1 + 2 from anvil2 = 4 total.
	if len(prs) != 4 {
		t.Fatalf("expected 4 PRs (2 per anvil), got %d", len(prs))
	}
	// Verify ordering: anvil1 first, then anvil2.
	if prs[0].Anvil != "anvil1" {
		t.Errorf("expected first result from anvil1, got %q", prs[0].Anvil)
	}
	if prs[2].Anvil != "anvil2" {
		t.Errorf("expected third result from anvil2, got %q", prs[2].Anvil)
	}
}

func TestClosedPRs_DefaultPerAnvil(t *testing.T) {
	fdb := setupTestDB(t)
	// perAnvil <= 0 should default to 5 — no panic.
	prs, err := fdb.ClosedPRs(0)
	if err != nil {
		t.Fatalf("ClosedPRs(0): %v", err)
	}
	if prs == nil {
		t.Error("expected non-nil slice for perAnvil=0")
	}
}

// --- QueueAll ---

func TestQueueAll_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	entries, err := fdb.QueueAll()
	if err != nil {
		t.Fatalf("QueueAll: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestQueueAll_ReturnsAllSections(t *testing.T) {
	fdb := setupTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := fdb.db.Exec(`
		INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at) VALUES
		  ('b1', 'anvil-a', 'Ready bead',       1, 'queued',      'forgeReady', 'ready',           '', '', ?),
		  ('b2', 'anvil-a', 'In-progress bead',  2, 'running',     '',           'in-progress',     '', '', ?),
		  ('b3', 'anvil-b', 'Unlabeled bead',    3, 'queued',      '',           'unlabeled',       '', '', ?),
		  ('b4', 'anvil-b', 'Needs-attention',   4, 'stuck',       '',           'needs-attention', '', '', ?)
	`, now, now, now, now)
	if err != nil {
		t.Fatalf("insert queue_cache: %v", err)
	}

	entries, err := fdb.QueueAll()
	if err != nil {
		t.Fatalf("QueueAll: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries across all sections, got %d", len(entries))
	}

	// Verify all sections are present.
	sections := make(map[string]bool)
	for _, e := range entries {
		sections[e.Section] = true
	}
	for _, s := range []string{"ready", "in-progress", "unlabeled", "needs-attention"} {
		if !sections[s] {
			t.Errorf("expected section %q in results", s)
		}
	}
}

func TestQueueAll_OrderedByAnvilThenSection(t *testing.T) {
	fdb := setupTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := fdb.db.Exec(`
		INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at) VALUES
		  ('b1', 'anvil-z', 'Z unlabeled', 1, 'queued', '', 'unlabeled', '', '', ?),
		  ('b2', 'anvil-a', 'A ready',     1, 'queued', '', 'ready',     '', '', ?)
	`, now, now)
	if err != nil {
		t.Fatalf("insert queue_cache: %v", err)
	}

	entries, err := fdb.QueueAll()
	if err != nil {
		t.Fatalf("QueueAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Anvil != "anvil-a" {
		t.Errorf("expected first entry anvil 'anvil-a', got %q", entries[0].Anvil)
	}
	if entries[1].Anvil != "anvil-z" {
		t.Errorf("expected second entry anvil 'anvil-z', got %q", entries[1].Anvil)
	}
}

// --- QueueEntryByBeadID ---

func TestQueueEntryByBeadID_Found(t *testing.T) {
	fdb := setupTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at)
		VALUES ('Hytte-xyz1', 'anvil1', 'My Bead', 3, 'ready', 'bug', 'ready', 'user', 'a desc', ?)
	`, now)
	if err != nil {
		t.Fatalf("insert queue_cache: %v", err)
	}

	entry, err := fdb.QueueEntryByBeadID("Hytte-xyz1")
	if err != nil {
		t.Fatalf("QueueEntryByBeadID: %v", err)
	}
	if entry.BeadID != "Hytte-xyz1" {
		t.Errorf("expected bead_id 'Hytte-xyz1', got %q", entry.BeadID)
	}
	if entry.Anvil != "anvil1" {
		t.Errorf("expected anvil 'anvil1', got %q", entry.Anvil)
	}
	if entry.Section != "ready" {
		t.Errorf("expected section 'ready', got %q", entry.Section)
	}
}

func TestQueueEntryByBeadID_NotFound(t *testing.T) {
	fdb := setupTestDB(t)

	_, err := fdb.QueueEntryByBeadID("Hytte-missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

// --- AnvilCosts ---

func TestAnvilCosts_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	anvils, err := fdb.AnvilCosts(7)
	if err != nil {
		t.Fatalf("AnvilCosts: %v", err)
	}
	if anvils == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(anvils) != 0 {
		t.Errorf("expected 0 anvils, got %d", len(anvils))
	}
}

func TestAnvilCosts_OrderedByDescCost(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES ('b1', 'cheap', 100, 50, 0, 0, 0.01, ?), ('b2', 'expensive', 500, 250, 0, 0, 0.10, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert bead_costs: %v", err)
	}

	anvils, err := fdb.AnvilCosts(7)
	if err != nil {
		t.Fatalf("AnvilCosts: %v", err)
	}
	if len(anvils) != 2 {
		t.Fatalf("expected 2 anvils, got %d", len(anvils))
	}
	if anvils[0].Anvil != "expensive" {
		t.Errorf("expected first anvil to be 'expensive' (highest cost), got %q", anvils[0].Anvil)
	}
}

func TestAnvilCosts_MissingTable(t *testing.T) {
	rawDB, err := sql.Open("sqlite", "file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	rawDB.SetMaxIdleConns(1)
	t.Cleanup(func() { rawDB.Close() })

	fdb := &DB{db: rawDB}
	anvils, err := fdb.AnvilCosts(7)
	if err != nil {
		t.Fatalf("expected nil error for missing bead_costs table, got: %v", err)
	}
	if anvils == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(anvils) != 0 {
		t.Errorf("expected 0 anvils, got %d", len(anvils))
	}
}

func TestAnvilCosts_ExcludesOldDates(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	_, err := fdb.db.Exec(`INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES ('b1', 'recent', 100, 50, 0, 0, 0.01, ?), ('b2', 'old', 100, 50, 0, 0, 0.01, ?)`, now, old)
	if err != nil {
		t.Fatalf("insert bead_costs: %v", err)
	}

	anvils, err := fdb.AnvilCosts(7)
	if err != nil {
		t.Fatalf("AnvilCosts: %v", err)
	}
	if len(anvils) != 1 {
		t.Errorf("expected 1 anvil (within 7 days), got %d", len(anvils))
	}
	if len(anvils) == 1 && anvils[0].Anvil != "recent" {
		t.Errorf("expected 'recent' anvil, got %q", anvils[0].Anvil)
	}
}

func TestAnvilCosts_DefaultsAndCap(t *testing.T) {
	fdb := setupTestDB(t)

	// days=0 should default to 7 (no panic, returns empty).
	anvils, err := fdb.AnvilCosts(0)
	if err != nil {
		t.Fatalf("AnvilCosts(0): %v", err)
	}
	if anvils == nil {
		t.Error("expected non-nil slice for days=0")
	}

	// days > 90 should be capped at 90 (no panic).
	anvils, err = fdb.AnvilCosts(200)
	if err != nil {
		t.Fatalf("AnvilCosts(200): %v", err)
	}
	if anvils == nil {
		t.Error("expected non-nil slice for days=200")
	}
}

// --- EventsPaginated ---

func TestEventsPaginated_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	result, err := fdb.EventsPaginated(50, 0, "", "", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}
	if result.Events == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(result.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(result.Events))
	}
}

func TestEventsPaginated_PaginationAndFilters(t *testing.T) {
	fdb := setupTestDB(t)

	// Insert 5 events with different types, timestamps, and anvils.
	_, err := fdb.db.Exec(`
		INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES
		  (1, '2026-04-01T10:00:00Z', 'worker_start', 'Worker started for bead b1', 'b1', 'anvil1'),
		  (2, '2026-04-02T10:00:00Z', 'pr_opened', 'PR opened for bead b1', 'b1', 'anvil1'),
		  (3, '2026-04-03T10:00:00Z', 'worker_start', 'Worker started for bead b2', 'b2', 'anvil2'),
		  (4, '2026-04-04T10:00:00Z', 'worker_done', 'Worker done for bead b2', 'b2', 'anvil2'),
		  (5, '2026-04-05T10:00:00Z', 'dispatch', 'Dispatched bead b3', 'b3', 'anvil1')
	`)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	// Test basic pagination: limit=2, offset=0
	result, err := fdb.EventsPaginated(2, 0, "", "", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(result.Events))
	}
	// Results are DESC by timestamp, so most recent first.
	if result.Events[0].ID != 5 {
		t.Errorf("expected first event id=5, got %d", result.Events[0].ID)
	}

	// Test offset: limit=2, offset=2
	result, err = fdb.EventsPaginated(2, 2, "", "", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated offset: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Events) != 2 {
		t.Errorf("expected 2 events at offset 2, got %d", len(result.Events))
	}

	// Test type filter
	result, err = fdb.EventsPaginated(50, 0, "worker_start", "", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated type filter: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 worker_start events, got %d", result.Total)
	}

	// Test anvil filter
	result, err = fdb.EventsPaginated(50, 0, "", "anvil2", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated anvil filter: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 anvil2 events, got %d", result.Total)
	}

	// Test date range filter
	result, err = fdb.EventsPaginated(50, 0, "", "", "", "2026-04-02", "2026-04-04")
	if err != nil {
		t.Fatalf("EventsPaginated date range: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("expected 3 events in date range, got %d", result.Total)
	}

	// Test search
	result, err = fdb.EventsPaginated(50, 0, "", "", "bead b2", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated search: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 events matching 'bead b2', got %d", result.Total)
	}

	// Test combined filters: type + anvil
	result, err = fdb.EventsPaginated(50, 0, "worker_start", "anvil1", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated combined: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 worker_start event in anvil1, got %d", result.Total)
	}
}

func TestEventsPaginated_SearchEscapesLIKEChars(t *testing.T) {
	fdb := setupTestDB(t)

	_, err := fdb.db.Exec(`
		INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES
		  (1, '2026-04-01T10:00:00Z', 'test', '100% complete', 'b1', 'a'),
		  (2, '2026-04-01T10:00:00Z', 'test', 'some other message', 'b2', 'a'),
		  (3, '2026-04-01T10:00:00Z', 'test', 'file_name_here', 'b3', 'a')
	`)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	// Search for literal "%" - should match only the event containing "%"
	result, err := fdb.EventsPaginated(50, 0, "", "", "100%", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated search %%: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 event matching '100%%', got %d", result.Total)
	}

	// Search for literal "_" - should match only the event containing "_"
	result, err = fdb.EventsPaginated(50, 0, "", "", "file_name", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated search _: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 event matching 'file_name', got %d", result.Total)
	}
}

func TestEventsPaginated_DefaultLimitAndOffset(t *testing.T) {
	fdb := setupTestDB(t)

	// limit <= 0 defaults to 50, offset < 0 defaults to 0
	result, err := fdb.EventsPaginated(-1, -5, "", "", "", "", "")
	if err != nil {
		t.Fatalf("EventsPaginated negative params: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- Ingots ---

func TestIngots_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	result, err := fdb.Ingots(50, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("Ingots: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}
	if result.Ingots == nil {
		t.Error("expected non-nil empty ingots slice")
	}
	if result.Metrics.TotalBeads != 0 {
		t.Errorf("expected 0 total_beads, got %d", result.Metrics.TotalBeads)
	}
}

func TestIngots_WithData(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC()
	started := now.Add(-2 * time.Hour).Format(time.RFC3339)
	completed := now.Add(-1 * time.Hour).Format(time.RFC3339)

	_, err := fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number) VALUES
		  ('w1', 'b1', 'anvil1', 'feat/b1', 1, 'done', 'done', 'First', ?, ?, NULL, '', 10),
		  ('w2', 'b2', 'anvil1', 'feat/b2', 2, 'failed', 'impl', 'Second', ?, ?, NULL, '', 0),
		  ('w3', 'b3', 'anvil2', 'feat/b3', 3, 'running', 'impl', 'Third', ?, NULL, NULL, '', 0)
	`, started, completed, started, completed, started)
	if err != nil {
		t.Fatalf("insert workers: %v", err)
	}

	result, err := fdb.Ingots(50, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("Ingots: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
	if len(result.Ingots) != 3 {
		t.Errorf("expected 3 ingots, got %d", len(result.Ingots))
	}

	// Verify metrics
	m := result.Metrics
	if m.TotalBeads != 3 {
		t.Errorf("expected 3 total_beads, got %d", m.TotalBeads)
	}
	if m.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", m.SuccessCount)
	}
	if m.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", m.FailureCount)
	}
	if m.RunningCount != 1 {
		t.Errorf("expected 1 running, got %d", m.RunningCount)
	}
	if m.SuccessRate < 0.49 || m.SuccessRate > 0.51 {
		t.Errorf("expected success_rate ~0.5, got %f", m.SuccessRate)
	}
	if m.AvgDurationSec <= 0 {
		t.Error("expected positive avg_duration_seconds for completed workers")
	}

	// Verify first ingot has duration set (completed workers should have duration)
	var foundDone bool
	for _, ing := range result.Ingots {
		if ing.Status == "done" {
			foundDone = true
			if ing.DurationSec == nil {
				t.Error("expected duration_seconds set for done ingot")
			}
			if ing.PRNumber != 10 {
				t.Errorf("expected pr_number 10, got %d", ing.PRNumber)
			}
		}
		if ing.Status == "running" && ing.DurationSec != nil {
			t.Error("expected nil duration_seconds for running ingot")
		}
	}
	if !foundDone {
		t.Error("expected to find a done ingot")
	}
}

func TestIngots_StatusFilter(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number) VALUES
		  ('w1', 'b1', 'a', 'feat/b1', 1, 'done', 'done', 'T1', ?, NULL, NULL, '', 0),
		  ('w2', 'b2', 'a', 'feat/b2', 2, 'failed', 'impl', 'T2', ?, NULL, NULL, '', 0)
	`, now, now) //nolint:errcheck

	result, err := fdb.Ingots(50, 0, "done", "", "", "")
	if err != nil {
		t.Fatalf("Ingots with status filter: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 done, got %d", result.Total)
	}
}

func TestIngots_SearchFilter(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number) VALUES
		  ('w1', 'Hytte-abc', 'a', 'feat/b1', 1, 'done', 'done', 'Fix login', ?, NULL, NULL, '', 0),
		  ('w2', 'Forge-xyz', 'a', 'feat/b2', 2, 'done', 'done', 'Add feature', ?, NULL, NULL, '', 0)
	`, now, now) //nolint:errcheck

	// Search by bead_id
	result, err := fdb.Ingots(50, 0, "", "Hytte", "", "")
	if err != nil {
		t.Fatalf("Ingots with search: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 match for 'Hytte', got %d", result.Total)
	}

	// Search by title
	result, err = fdb.Ingots(50, 0, "", "login", "", "")
	if err != nil {
		t.Fatalf("Ingots with title search: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 match for 'login', got %d", result.Total)
	}
}

func TestIngots_Pagination(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 5; i++ {
		fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number)
			VALUES (?, ?, 'a', 'feat/b', 1, 'done', 'done', 'T', ?, NULL, NULL, '', 0)`,
			fmt.Sprintf("w%d", i), fmt.Sprintf("b%d", i), now) //nolint:errcheck
	}

	result, err := fdb.Ingots(2, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("Ingots page 1: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Ingots) != 2 {
		t.Errorf("expected 2 ingots on page, got %d", len(result.Ingots))
	}

	// Second page
	result, err = fdb.Ingots(2, 2, "", "", "", "")
	if err != nil {
		t.Fatalf("Ingots page 2: %v", err)
	}
	if len(result.Ingots) != 2 {
		t.Errorf("expected 2 ingots on page 2, got %d", len(result.Ingots))
	}
}

func TestIngots_DefaultLimitAndOffset(t *testing.T) {
	fdb := setupTestDB(t)

	// limit <= 0 defaults to 50, offset < 0 defaults to 0
	result, err := fdb.Ingots(-1, -5, "", "", "", "")
	if err != nil {
		t.Fatalf("Ingots negative params: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestIngots_DateRange(t *testing.T) {
	fdb := setupTestDB(t)

	old := "2024-01-15T10:00:00Z"
	recent := "2024-06-15T10:00:00Z"
	fdb.db.Exec(`
		INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number) VALUES
		  ('w1', 'b1', 'a', 'feat/b1', 1, 'done', 'done', 'Old', ?, NULL, NULL, '', 0),
		  ('w2', 'b2', 'a', 'feat/b2', 2, 'done', 'done', 'Recent', ?, NULL, NULL, '', 0)
	`, old, recent) //nolint:errcheck

	// Filter from 2024-06-01 should only return the recent one
	result, err := fdb.Ingots(50, 0, "", "", "2024-06-01", "")
	if err != nil {
		t.Fatalf("Ingots with from date: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 result from 2024-06-01, got %d", result.Total)
	}

	// Filter to 2024-03-01 should only return the old one
	result, err = fdb.Ingots(50, 0, "", "", "", "2024-03-01")
	if err != nil {
		t.Fatalf("Ingots with to date: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 result to 2024-03-01, got %d", result.Total)
	}
}

func TestIngots_SearchEscapesSpecialChars(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number)
		VALUES ('w1', 'b1', 'a', 'feat/b1', 1, 'done', 'done', 'test%special_chars', ?, NULL, NULL, '', 0)`,
		now) //nolint:errcheck

	// Search with % should be escaped and match literally
	result, err := fdb.Ingots(50, 0, "", "%special", "", "")
	if err != nil {
		t.Fatalf("Ingots with special chars: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 match for '%%special', got %d", result.Total)
	}
}
