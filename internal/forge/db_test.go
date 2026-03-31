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
			date TEXT,
			bead_id TEXT,
			input_tokens INTEGER,
			output_tokens INTEGER,
			estimated_cost REAL
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

	today := time.Now().UTC().Format("2006-01-02")
	_, err := fdb.db.Exec(`
		INSERT INTO bead_costs (date, bead_id, input_tokens, output_tokens, estimated_cost) VALUES
		  (?, 'cheap-bead', 100, 50, 0.01),
		  (?, 'expensive-bead', 5000, 2000, 0.50),
		  (?, 'mid-bead', 1000, 500, 0.10)
	`, today, today, today)
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

	today := time.Now().UTC().Format("2006-01-02")
	for i := range 6 {
		fdb.db.Exec(`INSERT INTO bead_costs (date, bead_id, input_tokens, output_tokens, estimated_cost) VALUES (?, ?, 100, 50, ?)`, //nolint:errcheck
			today, fmt.Sprintf("bead-%d", i), float64(i)*0.01)
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

	today := time.Now().UTC().Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	_, err := fdb.db.Exec(`
		INSERT INTO bead_costs (date, bead_id, input_tokens, output_tokens, estimated_cost) VALUES
		  (?, 'new-bead', 100, 50, 0.05),
		  (?, 'old-bead', 100, 50, 0.99)
	`, today, old)
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
