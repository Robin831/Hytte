package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// --- StatusHandler ---

func TestStatusHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/status", nil)
	rec := httptest.NewRecorder()
	StatusHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestStatusHandler_WithDB_NoDaemon(t *testing.T) {
	// Override daemonAlive so the test does not depend on a real running daemon.
	orig := daemonAlive
	daemonAlive = func() (bool, string) { return false, "no daemon in test" }
	t.Cleanup(func() { daemonAlive = orig })

	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/status", nil)
	rec := httptest.NewRecorder()
	StatusHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		DaemonHealthy bool   `json:"daemon_healthy"`
		DaemonError   string `json:"daemon_error"`
		Workers       struct {
			Active    int `json:"active"`
			Completed int `json:"completed"`
		} `json:"workers"`
		WorkerList []Worker `json:"worker_list"`
		PRsOpen    int      `json:"prs_open"`
		OpenPRs    []PR     `json:"open_prs"`
		QueueReady int      `json:"queue_ready"`
		NeedsHuman int      `json:"needs_human"`
		Stuck      []Retry  `json:"stuck"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// In test environment there is no running daemon, so daemon_healthy should
	// be false (PID file does not exist).
	if body.DaemonHealthy {
		t.Error("expected daemon_healthy=false when no daemon is running")
	}
	if body.DaemonError == "" {
		t.Error("expected daemon_error to be set when no daemon is running")
	}
	if body.WorkerList == nil {
		t.Error("expected worker_list to be a non-nil slice")
	}
	if body.OpenPRs == nil {
		t.Error("expected open_prs to be a non-nil slice")
	}
	if body.Stuck == nil {
		t.Error("expected stuck to be a non-nil slice")
	}
}

func TestStatusHandler_WithData(t *testing.T) {
	fdb := setupTestDB(t)

	now := time.Now().UTC()
	startedAt := now.Add(-1 * time.Hour).Format(time.RFC3339)
	recentDone := now.Add(-30 * time.Minute).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number) VALUES
		('w1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, NULL, NULL, '', 0),
		('w2', 'b2', 'a', 'feat/b2', 2, 'done', 'done', 'T', ?, ?, NULL, '', 0)
	`, startedAt, startedAt, recentDone) //nolint:errcheck

	fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES (1, 10, 'a', 'b1', 'feat/b1', 'main', 'PR', 'open', ?, NULL, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, nowStr) //nolint:errcheck

	fdb.db.Exec(`INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at)
		VALUES ('b3', 'a', 'T', 1, 'ready', '', 'ready', '', '', ?)
	`, nowStr) //nolint:errcheck

	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('b4', 'a', 2, NULL, 1, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/status", nil)
	rec := httptest.NewRecorder()
	StatusHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Workers struct {
			Active    int `json:"active"`
			Completed int `json:"completed"`
		} `json:"workers"`
		WorkerList []Worker `json:"worker_list"`
		PRsOpen    int      `json:"prs_open"`
		QueueReady int      `json:"queue_ready"`
		NeedsHuman int      `json:"needs_human"`
		Stuck      []Retry  `json:"stuck"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Workers.Active != 1 {
		t.Errorf("expected 1 active worker, got %d", body.Workers.Active)
	}
	if body.Workers.Completed != 1 {
		t.Errorf("expected 1 completed worker, got %d", body.Workers.Completed)
	}
	if body.PRsOpen != 1 {
		t.Errorf("expected 1 open PR, got %d", body.PRsOpen)
	}
	if body.QueueReady != 1 {
		t.Errorf("expected 1 queue entry, got %d", body.QueueReady)
	}
	if body.NeedsHuman != 1 {
		t.Errorf("expected 1 needs_human, got %d", body.NeedsHuman)
	}
	if body.WorkerList == nil {
		t.Error("expected worker_list to be a non-nil slice")
	}
	if len(body.WorkerList) != 2 {
		t.Errorf("expected 2 workers in worker_list, got %d", len(body.WorkerList))
	}
	if body.Stuck == nil {
		t.Error("expected stuck to be a non-nil slice")
	}
	if len(body.Stuck) != 1 {
		t.Errorf("expected 1 entry in stuck, got %d", len(body.Stuck))
	}
}

// --- WorkersHandler ---

func TestWorkersHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers", nil)
	rec := httptest.NewRecorder()
	WorkersHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestWorkersHandler_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers", nil)
	rec := httptest.NewRecorder()
	WorkersHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var workers []Worker
	if err := json.NewDecoder(rec.Body).Decode(&workers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if workers == nil {
		t.Error("expected non-nil slice")
	}
	if len(workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(workers))
	}
}

// --- QueueHandler ---

func TestQueueHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/queue", nil)
	rec := httptest.NewRecorder()
	QueueHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestQueueHandler_ReturnsEntries(t *testing.T) {
	fdb := setupTestDB(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at)
		VALUES ('b1', 'anvil1', 'Ready bead', 2, 'ready', '', 'ready', '', 'desc', ?)
	`, nowStr) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/queue", nil)
	rec := httptest.NewRecorder()
	QueueHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var entries []QueueEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].BeadID != "b1" {
		t.Errorf("expected bead_id 'b1', got %q", entries[0].BeadID)
	}
}

// --- PRsHandler ---

func TestPRsHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs", nil)
	rec := httptest.NewRecorder()
	PRsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestPRsHandler_ReturnsOpenPRs(t *testing.T) {
	fdb := setupTestDB(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES
		  (1, 10, 'anvil1', 'b1', 'feat/b1', 'main', 'Open PR', 'open', ?, NULL, 0, 0, 1, 0, 0, 0, 0, 1, 0),
		  (2, 11, 'anvil1', 'b2', 'feat/b2', 'main', 'Merged PR', 'merged', ?, NULL, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, nowStr, nowStr) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs", nil)
	rec := httptest.NewRecorder()
	PRsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var prs []PR
	if err := json.NewDecoder(rec.Body).Decode(&prs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 open PR, got %d", len(prs))
	}
	if prs[0].Number != 10 {
		t.Errorf("expected PR number 10, got %d", prs[0].Number)
	}
}

// --- EventsHandler ---

func TestEventsHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/events", nil)
	rec := httptest.NewRecorder()
	EventsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestEventsHandler_DefaultLimit(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/events", nil)
	rec := httptest.NewRecorder()
	EventsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var events []Event
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil slice")
	}
}

func TestEventsHandler_LimitUpperBound(t *testing.T) {
	fdb := setupTestDB(t)

	// Insert 10 events to confirm the handler does not reject oversized limit param.
	nowStr := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 10; i++ {
		fdb.db.Exec(`INSERT INTO events (timestamp, type, message, bead_id, anvil) VALUES (?, 'test', 'msg', 'b1', 'a')`, nowStr) //nolint:errcheck
	}

	// Request with limit exceeding the upper bound — should clamp, not error.
	req := httptest.NewRequest(http.MethodGet, "/api/forge/events?limit=9999", nil)
	rec := httptest.NewRecorder()
	EventsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for oversized limit, got %d: %s", rec.Code, rec.Body.String())
	}

	var events []Event
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// All 10 events are returned (clamped limit 500 still includes all 10).
	if len(events) != 10 {
		t.Errorf("expected 10 events, got %d", len(events))
	}
}

func TestEventsHandler_FilterByType(t *testing.T) {
	fdb := setupTestDB(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO events (timestamp, type, message, bead_id, anvil) VALUES
		(?, 'worker_start', 'started', 'b1', 'a'),
		(?, 'pr_opened', 'opened', 'b2', 'a')
	`, nowStr, nowStr) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/events?type=worker_start", nil)
	rec := httptest.NewRecorder()
	EventsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var events []Event
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "worker_start" {
		t.Errorf("expected type 'worker_start', got %q", events[0].Type)
	}
}

// --- CostsHandler ---

func TestCostsHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs", nil)
	rec := httptest.NewRecorder()
	CostsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestCostsHandler_DefaultPeriod(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs", nil)
	rec := httptest.NewRecorder()
	CostsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var summary CostSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Period == "" {
		t.Error("expected period to be set in response")
	}
}

func TestCostsHandler_WithData(t *testing.T) {
	fdb := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	fdb.db.Exec(`INSERT INTO daily_costs (date, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, cost_limit)
		VALUES (?, 5000, 2000, 0, 0, 0.25, 1.0)
	`, today) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs?period=today", nil)
	rec := httptest.NewRecorder()
	CostsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var summary CostSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Period != "today" {
		t.Errorf("expected period 'today', got %q", summary.Period)
	}
	if summary.InputTokens != 5000 {
		t.Errorf("expected input_tokens 5000, got %d", summary.InputTokens)
	}
	if summary.EstimatedCost != 0.25 {
		t.Errorf("expected estimated_cost 0.25, got %f", summary.EstimatedCost)
	}
}

// --- RetryBeadHandler ---

// retryRequest builds a request with a chi URL param {id} set to beadID.
func retryRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/retry", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRetryBeadHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	RetryBeadHandler(nil).ServeHTTP(rec, retryRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestRetryBeadHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	// Use a chi context with an empty ID to hit the "bead ID required" branch.
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads//retry", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	RetryBeadHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRetryBeadHandler_MalformedBeadID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	// ID with characters that fail the regexp.
	RetryBeadHandler(fdb).ServeHTTP(rec, retryRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRetryBeadHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	RetryBeadHandler(fdb).ServeHTTP(rec, retryRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRetryBeadHandler_Success(t *testing.T) {
	// Create a fake forge binary that exits 0.
	dir := t.TempDir()
	fakeForge := filepath.Join(dir, "forge")
	if err := os.WriteFile(fakeForge, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 0, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck

	rec := httptest.NewRecorder()
	RetryBeadHandler(fdb).ServeHTTP(rec, retryRequest("Hytte-abc1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}
}

// --- MergePRHandler ---

// mergePRRequest builds a request with a chi URL param {id} set to prID.
func mergePRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestMergePRHandler_EmptyID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	MergePRHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMergePRHandler_InvalidID(t *testing.T) {
	rec := httptest.NewRecorder()
	MergePRHandler().ServeHTTP(rec, mergePRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergePRHandler_Success(t *testing.T) {
	// Start a temporary Unix socket listener to capture the command.
	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		received <- strings.TrimSpace(string(buf[:n]))
	}()

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	MergePRHandler().ServeHTTP(rec, mergePRRequest("42"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}

	select {
	case cmd := <-received:
		if cmd != "merge-pr 42" {
			t.Errorf("expected command 'merge-pr 42', got %q", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- KillWorkerHandler ---

func killWorkerRequest(workerID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/workers/"+workerID+"/kill", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workerID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestKillWorkerHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	KillWorkerHandler(nil).ServeHTTP(rec, killWorkerRequest("worker-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestKillWorkerHandler_EmptyID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/workers//kill", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	KillWorkerHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestKillWorkerHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	KillWorkerHandler(fdb).ServeHTTP(rec, killWorkerRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestKillWorkerHandler_WorkerNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	KillWorkerHandler(fdb).ServeHTTP(rec, killWorkerRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestKillWorkerHandler_Success(t *testing.T) {
	// Create a fake forge binary that exits 0.
	dir := t.TempDir()
	fakeForge := filepath.Join(dir, "forge")
	if err := os.WriteFile(fakeForge, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, completed_at, updated_at, log_path, pr_number)
		VALUES ('worker-abc1', 'Hytte-abc1', 'anvil1', 'feat/test', 1, 'running', 'impl', 'T', ?, NULL, NULL, '', 0)
	`, nowStr) //nolint:errcheck

	rec := httptest.NewRecorder()
	KillWorkerHandler(fdb).ServeHTTP(rec, killWorkerRequest("worker-abc1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}
}

// --- RefreshHandler ---

// RefreshHandler uses signalDaemon which requires a live socket; in tests
// the socket won't exist so we just verify it returns an error gracefully.
func TestRefreshHandler_NoDaemon(t *testing.T) {
	// Point to a non-existent socket so signalDaemon fails.
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/action/refresh", nil)
	rec := httptest.NewRecorder()
	RefreshHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- RestartForgeHandler ---

func TestRestartForgeHandler_ScriptNotFound(t *testing.T) {
	// Point home to a temp dir with no restart.sh.
	t.Setenv("HOME", t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/forge/restart", nil)
	rec := httptest.NewRecorder()
	RestartForgeHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when script missing, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRestartForgeHandler_ScriptExists(t *testing.T) {
	// Create a minimal restart.sh that exits immediately.
	home := t.TempDir()
	t.Setenv("HOME", home)
	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	script := filepath.Join(forgeDir, "restart.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/forge/restart", nil)
	rec := httptest.NewRecorder()
	RestartForgeHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}
}

// --- ActivityStreamHandler ---

func TestActivityStreamHandler_NilDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/activity/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ActivityStreamHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestActivityStreamHandler_SSEHeaders(t *testing.T) {
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO events (id, timestamp, type, message, bead_id, anvil) VALUES (1, ?, 'dispatch', 'worker started', 'b1', 'a')`, nowStr) //nolint:errcheck

	// Cancel context immediately so the polling loop exits right away.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/activity/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ActivityStreamHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"dispatch"`) {
		t.Errorf("expected initial event in body, got: %s", body)
	}
}

func TestActivityStreamHandler_EmptyDB(t *testing.T) {
	fdb := setupTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/activity/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ActivityStreamHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
}

// --- WorkerLogHandler ---

// workerLogRequest builds a GET request with chi URL param {id} set.
func workerLogRequest(workerID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers/"+workerID+"/log", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workerID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestWorkerLogHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	WorkerLogHandler(nil).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestWorkerLogHandler_InvalidID(t *testing.T) {
	rec := httptest.NewRecorder()
	WorkerLogHandler(nil).ServeHTTP(rec, workerLogRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestWorkerLogHandler_WorkerNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWorkerLogHandler_NoLogPath(t *testing.T) {
	fdb := setupTestDB(t)
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, '', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWorkerLogHandler_PathTraversal(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Relative path that traverses outside ~/.forge/
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, '../../../etc/passwd', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path-traversal attempt, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerLogHandler_AbsolutePathUnderHomeButNotForgeOrWorkers(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a sensitive file somewhere under $HOME but outside ~/.forge and .workers
	sensitiveDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sensitiveDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sensitiveFile := filepath.Join(sensitiveDir, "id_rsa")
	if err := os.WriteFile(sensitiveFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), sensitiveFile) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path under $HOME outside forge/workers, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerLogHandler_AbsolutePathOutsideHome(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Use an absolute path that is entirely outside $HOME (e.g. /etc/passwd)
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, '/etc/passwd', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path outside $HOME, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerLogHandler_SymlinkRejected(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a real file and a symlink pointing to it inside ~/.forge
	realFile := filepath.Join(home, "real.log")
	if err := os.WriteFile(realFile, []byte("data\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	symlink := filepath.Join(forgeDir, "link.log")
	if err := os.Symlink(realFile, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), symlink) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogRequest("worker-abc1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for symlink path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerLogHandler_AbsolutePathUnderWorkersAllowed(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Simulate a worker log stored in a .workers subdirectory (outside ~/.forge)
	workersDir := filepath.Join(home, "source", "project", ".workers", "worker-abc1")
	if err := os.MkdirAll(workersDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(workersDir, "output.log")
	if err := os.WriteFile(logFile, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), logFile) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogTailRequest("worker-abc1", "10"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid .workers path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerLogHandler_SSEStreamInitialContent(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create the log file inside ~/.forge/
	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(forgeDir, "worker-abc1.log")
	if err := os.WriteFile(logFile, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// Store relative path in the DB so the handler resolves it against ~/.forge/
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, 'worker-abc1.log', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	// Build request with chi route context and a cancelled context so the tail loop exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "worker-abc1")
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers/worker-abc1/log", nil).
		WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "line one") {
		t.Errorf("expected 'line one' in SSE body, got: %s", body)
	}
	if !strings.Contains(body, "line two") {
		t.Errorf("expected 'line two' in SSE body, got: %s", body)
	}
}

func workerLogTailRequest(workerID, tail string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers/"+workerID+"/log?tail="+tail, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workerID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestWorkerLogHandler_TailReturnsLastNLines(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write 5 lines; request tail=3 — expect only the last 3 returned.
	content := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	logFile := filepath.Join(forgeDir, "worker-abc1.log")
	if err := os.WriteFile(logFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, 'worker-abc1.log', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogTailRequest("worker-abc1", "3"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON content-type, got %q", ct)
	}
	var resp struct {
		Lines []string `json:"lines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(resp.Lines), resp.Lines)
	}
	if resp.Lines[0] != "gamma" || resp.Lines[1] != "delta" || resp.Lines[2] != "epsilon" {
		t.Errorf("unexpected lines: %v", resp.Lines)
	}
}

func TestWorkerLogHandler_TailEmptyFileReturnsEmptySlice(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(forgeDir, "worker-abc1.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, 'worker-abc1.log', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogTailRequest("worker-abc1", "100"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// lines must be a JSON array (not null) so clients can use Array.isArray() reliably.
	body := rec.Body.String()
	if !strings.Contains(body, `"lines":[]`) {
		t.Errorf("expected lines:[] for empty file, got: %s", body)
	}
}

func TestWorkerLogHandler_TailFewerLinesThanN(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(forgeDir, "worker-abc1.log")
	if err := os.WriteFile(logFile, []byte("only line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, 'worker-abc1.log', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerLogHandler(fdb).ServeHTTP(rec, workerLogTailRequest("worker-abc1", "200"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Lines []string `json:"lines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0] != "only line" {
		t.Errorf("expected [\"only line\"], got %v", resp.Lines)
	}
}

// --- CostsTrendHandler ---

func TestCostsTrendHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/trend", nil)
	rec := httptest.NewRecorder()
	CostsTrendHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestCostsTrendHandler_EmptyReturnsArray(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/trend?days=7", nil)
	rec := httptest.NewRecorder()
	CostsTrendHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []DailyCostEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil array, got null")
	}
}

func TestCostsTrendHandler_WithData(t *testing.T) {
	fdb := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	fdb.db.Exec(`INSERT INTO daily_costs (date, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, cost_limit) VALUES (?, 1000, 500, 0, 0, 0.05, 1.0)`, today) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/trend?days=7", nil)
	rec := httptest.NewRecorder()
	CostsTrendHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []DailyCostEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Date != today {
		t.Errorf("expected date %q, got %q", today, entries[0].Date)
	}
	if entries[0].EstimatedCost != 0.05 {
		t.Errorf("expected cost 0.05, got %f", entries[0].EstimatedCost)
	}
}

func TestCostsTrendHandler_InvalidDaysIgnored(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/trend?days=notanumber", nil)
	rec := httptest.NewRecorder()
	CostsTrendHandler(fdb).ServeHTTP(rec, req)
	// Should still return 200 with default 7-day window.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid days param, got %d", rec.Code)
	}
}

// --- TopBeadCostsHandler ---

func TestTopBeadCostsHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/beads", nil)
	rec := httptest.NewRecorder()
	TopBeadCostsHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestTopBeadCostsHandler_EmptyReturnsArray(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/beads?days=7&limit=5", nil)
	rec := httptest.NewRecorder()
	TopBeadCostsHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var beads []BeadCost
	if err := json.NewDecoder(rec.Body).Decode(&beads); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if beads == nil {
		t.Error("expected non-nil array, got null")
	}
}

func TestTopBeadCostsHandler_WithData(t *testing.T) {
	fdb := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	fdb.db.Exec(`INSERT INTO bead_costs (date, bead_id, input_tokens, output_tokens, estimated_cost) VALUES (?, 'b1', 200, 100, 0.02)`, today) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/beads?days=7&limit=5", nil)
	rec := httptest.NewRecorder()
	TopBeadCostsHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var beads []BeadCost
	if err := json.NewDecoder(rec.Body).Decode(&beads); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(beads) != 1 {
		t.Fatalf("expected 1 bead, got %d", len(beads))
	}
	if beads[0].BeadID != "b1" {
		t.Errorf("expected bead_id 'b1', got %q", beads[0].BeadID)
	}
}

func TestTopBeadCostsHandler_LimitCappedAt20(t *testing.T) {
	fdb := setupTestDB(t)
	// limit=999 should be rejected; default of 5 applied.
	req := httptest.NewRequest(http.MethodGet, "/api/forge/costs/beads?days=7&limit=999", nil)
	rec := httptest.NewRecorder()
	TopBeadCostsHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- BellowsPRHandler ---

func bellowsPRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/bellows", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestBellowsPRHandler_EmptyID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//bellows", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	BellowsPRHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBellowsPRHandler_InvalidID(t *testing.T) {
	rec := httptest.NewRecorder()
	BellowsPRHandler().ServeHTTP(rec, bellowsPRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsPRHandler_NoDaemon(t *testing.T) {
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	BellowsPRHandler().ServeHTTP(rec, bellowsPRRequest("42"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- ApprovePRHandler ---

func approvePRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestApprovePRHandler_EmptyID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ApprovePRHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApprovePRHandler_InvalidID(t *testing.T) {
	rec := httptest.NewRecorder()
	ApprovePRHandler().ServeHTTP(rec, approvePRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApprovePRHandler_NoDaemon(t *testing.T) {
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	ApprovePRHandler().ServeHTTP(rec, approvePRRequest("42"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- FullQueueHandler ---

func TestFullQueueHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/queue/all", nil)
	rec := httptest.NewRecorder()
	FullQueueHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestFullQueueHandler_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/queue/all", nil)
	rec := httptest.NewRecorder()
	FullQueueHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []QueueEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFullQueueHandler_WithData(t *testing.T) {
	fdb := setupTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := fdb.db.Exec(`
		INSERT INTO queue_cache (bead_id, anvil, title, priority, status, labels, section, assignee, description, updated_at) VALUES
		  ('b1', 'anvil-a', 'Ready',      1, 'queued', 'forgeReady', 'ready',       '', '', ?),
		  ('b2', 'anvil-a', 'In-prog',    2, 'running', '',          'in-progress', '', '', ?)
	`, now, now)
	if err != nil {
		t.Fatalf("insert queue_cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/forge/queue/all", nil)
	rec := httptest.NewRecorder()
	FullQueueHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []QueueEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

// --- AddLabelHandler ---

func addLabelRequest(beadID, label string) *http.Request {
	body := strings.NewReader(fmt.Sprintf(`{"label":%q}`, label))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/labels", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestAddLabelHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads//labels",
		strings.NewReader(`{"label":"forgeReady"}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	AddLabelHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddLabelHandler_MalformedBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	AddLabelHandler().ServeHTTP(rec, addLabelRequest("../etc/passwd", "forgeReady"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddLabelHandler_InvalidLabel(t *testing.T) {
	rec := httptest.NewRecorder()
	AddLabelHandler().ServeHTTP(rec, addLabelRequest("Hytte-abc1", "bad label!"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddLabelHandler_BadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/Hytte-abc1/labels",
		strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	AddLabelHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddLabelHandler_Success(t *testing.T) {
	// Create a fake bd binary that exits 0.
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	AddLabelHandler().ServeHTTP(rec, addLabelRequest("Hytte-abc1", "forgeReady"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}
}

func TestAddLabelHandler_BdNotFound(t *testing.T) {
	// When bd is not in PATH or user bin dirs, the handler should return 500.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // prevent fallback to ~/.local/bin or ~/bin
	rec := httptest.NewRecorder()
	AddLabelHandler().ServeHTTP(rec, addLabelRequest("Hytte-abc1", "forgeReady"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- RemoveLabelHandler ---

func removeLabelRequest(beadID, label string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete,
		"/api/forge/beads/"+beadID+"/labels/"+label, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	rctx.URLParams.Add("label", label)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRemoveLabelHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/forge/beads//labels/forgeReady", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	rctx.URLParams.Add("label", "forgeReady")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	RemoveLabelHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRemoveLabelHandler_MalformedBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	RemoveLabelHandler().ServeHTTP(rec, removeLabelRequest("../etc/passwd", "forgeReady"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveLabelHandler_InvalidLabel(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/forge/beads/Hytte-abc1/labels/bad", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	rctx.URLParams.Add("label", "bad label!") // invalid: contains space
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	RemoveLabelHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveLabelHandler_Success(t *testing.T) {
	// Create a fake bd binary that exits 0.
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	RemoveLabelHandler().ServeHTTP(rec, removeLabelRequest("Hytte-abc1", "forgeReady"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["ok"] {
		t.Error("expected ok=true in response")
	}
}

func TestRemoveLabelHandler_BdNotFound(t *testing.T) {
	// When bd is not in PATH or user bin dirs, the handler should return 500.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // prevent fallback to ~/.local/bin or ~/bin
	rec := httptest.NewRecorder()
	RemoveLabelHandler().ServeHTTP(rec, removeLabelRequest("Hytte-abc1", "forgeReady"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
