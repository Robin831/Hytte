package forge

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
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
