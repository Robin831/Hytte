package forge

import (
	"bufio"
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
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 0, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

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

	select {
	case cmd := <-received:
		if cmd.Type != "retry_bead" {
			t.Errorf("expected command type 'retry_bead', got %q", cmd.Type)
		}
		var p retryBeadPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.BeadID != "Hytte-abc1" {
			t.Errorf("expected bead_id 'Hytte-abc1', got %q", p.BeadID)
		}
		if p.Anvil != "anvil1" {
			t.Errorf("expected anvil 'anvil1', got %q", p.Anvil)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

func TestRetryBeadHandler_SuccessWithPR(t *testing.T) {
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 1, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck
	prDBID := insertTestPR(t, fdb, 55, "anvil1", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

	rec := httptest.NewRecorder()
	RetryBeadHandler(fdb).ServeHTTP(rec, retryRequest("Hytte-abc1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case cmd := <-received:
		var p retryBeadPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.PRID != prDBID {
			t.Errorf("expected pr_id %d, got %d", prDBID, p.PRID)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- DismissBeadHandler ---

func dismissBeadRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/dismiss", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestDismissBeadHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	DismissBeadHandler(nil).ServeHTTP(rec, dismissBeadRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDismissBeadHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissBeadRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDismissBeadHandler_MalformedBeadID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissBeadRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDismissBeadHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 1, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck
	prDBID := insertTestPR(t, fdb, 77, "anvil1", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

	rec := httptest.NewRecorder()
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissBeadRequest("Hytte-abc1"))

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
		if cmd.Type != "dismiss_bead" {
			t.Errorf("expected command type 'dismiss_bead', got %q", cmd.Type)
		}
		var p dismissBeadPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.BeadID != "Hytte-abc1" {
			t.Errorf("expected bead_id 'Hytte-abc1', got %q", p.BeadID)
		}
		if p.Anvil != "anvil1" {
			t.Errorf("expected anvil 'anvil1', got %q", p.Anvil)
		}
		if p.PRID != prDBID {
			t.Errorf("expected pr_id %d, got %d", prDBID, p.PRID)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- ApproveBeadHandler ---

func approveBeadRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestApproveBeadHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	ApproveBeadHandler(nil).ServeHTTP(rec, approveBeadRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestApproveBeadHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ApproveBeadHandler(fdb).ServeHTTP(rec, approveBeadRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveBeadHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 1, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

	rec := httptest.NewRecorder()
	ApproveBeadHandler(fdb).ServeHTTP(rec, approveBeadRequest("Hytte-abc1"))

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
		if cmd.Type != "approve_as_is" {
			t.Errorf("expected command type 'approve_as_is', got %q", cmd.Type)
		}
		var p approveAsIsPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.BeadID != "Hytte-abc1" {
			t.Errorf("expected bead_id 'Hytte-abc1', got %q", p.BeadID)
		}
		if p.Anvil != "anvil1" {
			t.Errorf("expected anvil 'anvil1', got %q", p.Anvil)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- ForceSmithHandler ---

func forceSmithRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/force-smith", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestForceSmithHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	ForceSmithHandler(nil).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestForceSmithHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ForceSmithHandler(fdb).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForceSmithHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO retries (bead_id, anvil, retry_count, next_retry, needs_human, clarification_needed, last_error, updated_at, dispatch_failures)
		VALUES ('Hytte-abc1', 'anvil1', 1, NULL, 1, 0, 'err', ?, 0)
	`, nowStr) //nolint:errcheck

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

	rec := httptest.NewRecorder()
	ForceSmithHandler(fdb).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

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
		if cmd.Type != "force_smith" {
			t.Errorf("expected command type 'force_smith', got %q", cmd.Type)
		}
		var p forceSmithPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.BeadID != "Hytte-abc1" {
			t.Errorf("expected bead_id 'Hytte-abc1', got %q", p.BeadID)
		}
		if p.Anvil != "anvil1" {
			t.Errorf("expected anvil 'anvil1', got %q", p.Anvil)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- MergePRHandler ---

// insertTestPR inserts a PR row into the test DB and returns its database ID.
func insertTestPR(t *testing.T, db *DB, number int, anvil, beadID, branch string) int {
	t.Helper()
	res, err := db.db.Exec(
		`INSERT INTO prs (number, anvil, bead_id, branch, base_branch, title, status, created_at,
		 ci_fix_count, review_fix_count, ci_passing, rebase_count,
		 is_conflicting, has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		 VALUES (?, ?, ?, ?, 'main', 'Test PR', 'open', ?, 0, 0, 1, 0, 0, 0, 0, 0, 0)`,
		number, anvil, beadID, branch, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert test PR: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return int(id)
}

// receiveIPCCommand starts a socket listener and returns the received JSON
// command parsed into its type and payload fields.
func receiveIPCCommand(t *testing.T, socketPath string) <-chan ipcCommand {
	t.Helper()
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	received := make(chan ipcCommand, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Errorf("read IPC command: %v", err)
			return
		}
		var cmd ipcCommand
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &cmd); err != nil {
			t.Errorf("unmarshal IPC command: %v", err)
			return
		}
		received <- cmd
	}()
	return received
}

// mergePRRequest builds a request with a chi URL param {id} set to prID.
func mergePRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestMergePRHandler_EmptyID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	MergePRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMergePRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	MergePRHandler(fdb).ServeHTTP(rec, mergePRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergePRHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	MergePRHandler(nil).ServeHTTP(rec, mergePRRequest("1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMergePRHandler_NotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	MergePRHandler(fdb).ServeHTTP(rec, mergePRRequest("999"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMergePRHandler_MissingBranch(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "")
	rec := httptest.NewRecorder()
	MergePRHandler(fdb).ServeHTTP(rec, mergePRRequest(fmt.Sprintf("%d", prDBID)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing branch, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergePRHandler_MissingAnvil(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "", "Hytte-abc1", "forge/Hytte-abc1")
	rec := httptest.NewRecorder()
	MergePRHandler(fdb).ServeHTTP(rec, mergePRRequest(fmt.Sprintf("%d", prDBID)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing anvil, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergePRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	MergePRHandler(fdb).ServeHTTP(rec, mergePRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "pr_action" {
			t.Errorf("expected command type 'pr_action', got %q", cmd.Type)
		}
		var pa prActionPayload
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.Action != "merge" {
			t.Errorf("expected action 'merge', got %q", pa.Action)
		}
		if pa.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", pa.PRNumber)
		}
		if pa.Anvil != "hytte" {
			t.Errorf("expected anvil 'hytte', got %q", pa.Anvil)
		}
		if pa.Branch != "forge/Hytte-abc1" {
			t.Errorf("expected branch 'forge/Hytte-abc1', got %q", pa.Branch)
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

// --- DismissBeadHandler ---

func dismissRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/dismiss", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestDismissBeadHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	DismissBeadHandler(nil).ServeHTTP(rec, dismissRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDismissBeadHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads//dismiss", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	DismissBeadHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDismissBeadHandler_MalformedBeadID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDismissBeadHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDismissBeadHandler_Success(t *testing.T) {
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
	DismissBeadHandler(fdb).ServeHTTP(rec, dismissRequest("Hytte-abc1"))

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

// --- ApproveBeadHandler ---

func approveRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestApproveBeadHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	ApproveBeadHandler(nil).ServeHTTP(rec, approveRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestApproveBeadHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads//approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ApproveBeadHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApproveBeadHandler_MalformedBeadID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ApproveBeadHandler(fdb).ServeHTTP(rec, approveRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveBeadHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ApproveBeadHandler(fdb).ServeHTTP(rec, approveRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveBeadHandler_Success(t *testing.T) {
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
	ApproveBeadHandler(fdb).ServeHTTP(rec, approveRequest("Hytte-abc1"))

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

// --- ForceSmithHandler ---

func forceSmithRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/force-smith", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestForceSmithHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	ForceSmithHandler(nil).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestForceSmithHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads//force-smith", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ForceSmithHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestForceSmithHandler_MalformedBeadID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ForceSmithHandler(fdb).ServeHTTP(rec, forceSmithRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForceSmithHandler_BeadNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ForceSmithHandler(fdb).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForceSmithHandler_Success(t *testing.T) {
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
	ForceSmithHandler(fdb).ServeHTTP(rec, forceSmithRequest("Hytte-abc1"))

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

// RefreshHandler uses sendIPCCommand which requires a live socket; in tests
// the socket won't exist so we just verify it returns an error gracefully.
func TestRefreshHandler_NoDaemon(t *testing.T) {
	// Point to a non-existent socket so sendIPCCommand fails.
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

	now := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO bead_costs (bead_id, anvil, input_tokens, output_tokens, cache_read, cache_write, estimated_cost, updated_at) VALUES ('b1', 'hytte', 200, 100, 0, 0, 0.02, ?)`, now) //nolint:errcheck

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
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//bellows", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	BellowsPRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBellowsPRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	BellowsPRHandler(fdb).ServeHTTP(rec, bellowsPRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsPRHandler_NoDaemon(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	BellowsPRHandler(fdb).ServeHTTP(rec, bellowsPRRequest(fmt.Sprintf("%d", prDBID)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsPRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	BellowsPRHandler(fdb).ServeHTTP(rec, bellowsPRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "pr_action" {
			t.Errorf("expected command type 'pr_action', got %q", cmd.Type)
		}
		var pa prActionPayload
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.Action != "assign_bellows" {
			t.Errorf("expected action 'assign_bellows', got %q", pa.Action)
		}
		if pa.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", pa.PRNumber)
		}
		if pa.Anvil != "hytte" {
			t.Errorf("expected anvil 'hytte', got %q", pa.Anvil)
		}
		if pa.Branch != "forge/Hytte-abc1" {
			t.Errorf("expected branch 'forge/Hytte-abc1', got %q", pa.Branch)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
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
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//approve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ApprovePRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApprovePRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ApprovePRHandler(fdb).ServeHTTP(rec, approvePRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApprovePRHandler_NoDaemon(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	ApprovePRHandler(fdb).ServeHTTP(rec, approvePRRequest(fmt.Sprintf("%d", prDBID)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApprovePRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	ApprovePRHandler(fdb).ServeHTTP(rec, approvePRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "approve_as_is" {
			t.Errorf("expected command type 'approve_as_is', got %q", cmd.Type)
		}
		var pa struct {
			BeadID string `json:"bead_id"`
			Anvil  string `json:"anvil"`
		}
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.BeadID != "Hytte-abc1" {
			t.Errorf("expected bead_id 'Hytte-abc1', got %q", pa.BeadID)
		}
		if pa.Anvil != "hytte" {
			t.Errorf("expected anvil 'hytte', got %q", pa.Anvil)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
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

// --- BeadDetailHandler ---

func beadDetailRequest(beadID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/beads/"+beadID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestBeadDetailHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/beads/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	BeadDetailHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBeadDetailHandler_MalformedBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	BeadDetailHandler().ServeHTTP(rec, beadDetailRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeadDetailHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	script := `#!/bin/sh
echo '[{"id":"Hytte-test1","title":"Test bead","description":"A test","status":"open","priority":2,"issue_type":"task","owner":"test@example.com","created_at":"2026-01-01T00:00:00Z","created_by":"Test","updated_at":"2026-01-02T00:00:00Z","labels":["forgeReady"],"dependencies":[],"dependents":[{"id":"Hytte-dep1","title":"Dep","status":"open","priority":1,"issue_type":"bug","dependency_type":"blocks"}]}]'
`
	if err := os.WriteFile(fakeBd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	BeadDetailHandler().ServeHTTP(rec, beadDetailRequest("Hytte-test1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var detail BeadDetail
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID != "Hytte-test1" {
		t.Errorf("expected id Hytte-test1, got %s", detail.ID)
	}
	if detail.Title != "Test bead" {
		t.Errorf("expected title 'Test bead', got %s", detail.Title)
	}
	if detail.Priority != 2 {
		t.Errorf("expected priority 2, got %d", detail.Priority)
	}
	if len(detail.Labels) != 1 || detail.Labels[0] != "forgeReady" {
		t.Errorf("expected labels [forgeReady], got %v", detail.Labels)
	}
	if len(detail.Dependents) != 1 || detail.Dependents[0].ID != "Hytte-dep1" {
		t.Errorf("expected 1 dependent Hytte-dep1, got %v", detail.Dependents)
	}
	if detail.Dependents[0].DependencyType != "blocks" {
		t.Errorf("expected dependency_type 'blocks', got %s", detail.Dependents[0].DependencyType)
	}
	if detail.Dependents[0].Direction != "dependent" {
		t.Errorf("expected direction 'dependent', got %s", detail.Dependents[0].Direction)
	}
}

func TestBeadDetailHandler_NotFound(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	script := `#!/bin/sh
echo "error: bead not found" >&2
exit 1
`
	if err := os.WriteFile(fakeBd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	BeadDetailHandler().ServeHTTP(rec, beadDetailRequest("Hytte-none1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeadDetailHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	BeadDetailHandler().ServeHTTP(rec, beadDetailRequest("Hytte-abc1"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeBeadDetail_EmptyArrayFields(t *testing.T) {
	raw := map[string]any{
		"id":     "Hytte-x1",
		"title":  "Test",
		"status": "open",
	}
	detail := normalizeBeadDetail(raw)
	if detail.Labels == nil {
		t.Error("labels should be empty slice, not nil")
	}
	if detail.Comments == nil {
		t.Error("comments should be empty slice, not nil")
	}
	if detail.Dependencies == nil {
		t.Error("dependencies should be empty slice, not nil")
	}
	if detail.Dependents == nil {
		t.Error("dependents should be empty slice, not nil")
	}
}

// --- CommentHandler ---

func commentRequest(beadID, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/comment",
		strings.NewReader(fmt.Sprintf(`{"body":%q}`, body)))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCommentHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	CommentHandler().ServeHTTP(rec, commentRequest("../bad", "hello"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCommentHandler_EmptyBody(t *testing.T) {
	rec := httptest.NewRecorder()
	CommentHandler().ServeHTTP(rec, commentRequest("Hytte-abc1", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCommentHandler_BadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/Hytte-abc1/comment",
		strings.NewReader(`not-json`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	CommentHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCommentHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	CommentHandler().ServeHTTP(rec, commentRequest("Hytte-abc1", "looks good"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCommentHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	CommentHandler().ServeHTTP(rec, commentRequest("Hytte-abc1", "test"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- UpdatePriorityHandler ---

func priorityRequest(beadID string, jsonBody string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/forge/beads/"+beadID+"/priority",
		strings.NewReader(jsonBody))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestUpdatePriorityHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("../bad", `{"priority":1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdatePriorityHandler_MissingPriority(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("Hytte-abc1", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePriorityHandler_OutOfRange(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("Hytte-abc1", `{"priority":5}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePriorityHandler_NegativePriority(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("Hytte-abc1", `{"priority":-1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePriorityHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("Hytte-abc1", `{"priority":2}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePriorityHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	UpdatePriorityHandler().ServeHTTP(rec, priorityRequest("Hytte-abc1", `{"priority":1}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- UpdateStatusHandler ---

func statusRequest(beadID, status string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/forge/beads/"+beadID+"/status",
		strings.NewReader(fmt.Sprintf(`{"status":%q}`, status)))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestUpdateStatusHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdateStatusHandler().ServeHTTP(rec, statusRequest("../bad", "open"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateStatusHandler_InvalidStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdateStatusHandler().ServeHTTP(rec, statusRequest("Hytte-abc1", "invalid"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateStatusHandler_EmptyStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdateStatusHandler().ServeHTTP(rec, statusRequest("Hytte-abc1", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateStatusHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	for _, status := range beadStatuses {
		rec := httptest.NewRecorder()
		UpdateStatusHandler().ServeHTTP(rec, statusRequest("Hytte-abc1", status))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%s: expected 200, got %d: %s", status, rec.Code, rec.Body.String())
		}
	}
}

func TestUpdateStatusHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	UpdateStatusHandler().ServeHTTP(rec, statusRequest("Hytte-abc1", "open"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- UpdateAssigneeHandler ---

func assigneeRequest(beadID, assignee string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/forge/beads/"+beadID+"/assignee",
		strings.NewReader(fmt.Sprintf(`{"assignee":%q}`, assignee)))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestUpdateAssigneeHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdateAssigneeHandler().ServeHTTP(rec, assigneeRequest("../bad", "alice"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateAssigneeHandler_BadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/forge/beads/Hytte-abc1/assignee",
		strings.NewReader(`not-json`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	UpdateAssigneeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateAssigneeHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	UpdateAssigneeHandler().ServeHTTP(rec, assigneeRequest("Hytte-abc1", "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAssigneeHandler_EmptyAssignee(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	UpdateAssigneeHandler().ServeHTTP(rec, assigneeRequest("Hytte-abc1", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty assignee (unassign), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAssigneeHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	UpdateAssigneeHandler().ServeHTTP(rec, assigneeRequest("Hytte-abc1", "alice"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateAssigneeHandler_MissingField(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/forge/beads/Hytte-abc1/assignee",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	UpdateAssigneeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing assignee field, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAssigneeHandler_InvalidFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	UpdateAssigneeHandler().ServeHTTP(rec, assigneeRequest("Hytte-abc1", "invalid user!"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid assignee format, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- SetLabelsHandler ---

func setLabelsRequest(beadID string, labels []string) *http.Request {
	labelsJSON, _ := json.Marshal(labels)
	r := httptest.NewRequest(http.MethodPut, "/api/forge/beads/"+beadID+"/labels",
		strings.NewReader(fmt.Sprintf(`{"labels":%s}`, labelsJSON)))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestSetLabelsHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	SetLabelsHandler().ServeHTTP(rec, setLabelsRequest("../bad", []string{"a"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetLabelsHandler_NilLabels(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/forge/beads/Hytte-abc1/labels",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	SetLabelsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetLabelsHandler_InvalidLabel(t *testing.T) {
	rec := httptest.NewRecorder()
	SetLabelsHandler().ServeHTTP(rec, setLabelsRequest("Hytte-abc1", []string{"good", "bad label!"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetLabelsHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	SetLabelsHandler().ServeHTTP(rec, setLabelsRequest("Hytte-abc1", []string{"bug", "urgent"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetLabelsHandler_EmptyLabels(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	SetLabelsHandler().ServeHTTP(rec, setLabelsRequest("Hytte-abc1", []string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty labels (clear all), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetLabelsHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	SetLabelsHandler().ServeHTTP(rec, setLabelsRequest("Hytte-abc1", []string{"a"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- CloseBeadHandler ---

func closeBeadRequest(beadID, reason string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/forge/beads/"+beadID+"/close",
		strings.NewReader(fmt.Sprintf(`{"reason":%q}`, reason)))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", beadID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCloseBeadHandler_InvalidBeadID(t *testing.T) {
	rec := httptest.NewRecorder()
	CloseBeadHandler().ServeHTTP(rec, closeBeadRequest("../bad", "done"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCloseBeadHandler_EmptyReason(t *testing.T) {
	rec := httptest.NewRecorder()
	CloseBeadHandler().ServeHTTP(rec, closeBeadRequest("Hytte-abc1", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCloseBeadHandler_BadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/beads/Hytte-abc1/close",
		strings.NewReader(`not-json`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "Hytte-abc1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	CloseBeadHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCloseBeadHandler_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBd := filepath.Join(dir, "bd")
	if err := os.WriteFile(fakeBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rec := httptest.NewRecorder()
	CloseBeadHandler().ServeHTTP(rec, closeBeadRequest("Hytte-abc1", "completed the work"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCloseBeadHandler_BdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	CloseBeadHandler().ServeHTTP(rec, closeBeadRequest("Hytte-abc1", "done"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- ClosedPRsHandler ---

func TestClosedPRsHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/closed", nil)
	rec := httptest.NewRecorder()
	ClosedPRsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestClosedPRsHandler_Empty(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/closed", nil)
	rec := httptest.NewRecorder()
	ClosedPRsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var prs []PR
	if err := json.NewDecoder(rec.Body).Decode(&prs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if prs == nil {
		t.Error("expected non-nil slice")
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 prs, got %d", len(prs))
	}
}

func TestClosedPRsHandler_ReturnsClosedPRs(t *testing.T) {
	fdb := setupTestDB(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES
		  (1, 10, 'anvil1', 'b1', 'feat/b1', 'main', 'Open PR',   'open',   ?, NULL, 0, 0, 1, 0, 0, 0, 0, 1, 0),
		  (2, 11, 'anvil1', 'b2', 'feat/b2', 'main', 'Merged PR',  'merged', ?, ?,    0, 0, 0, 0, 0, 0, 0, 0, 0),
		  (3, 12, 'anvil1', 'b3', 'feat/b3', 'main', 'Closed PR',  'closed', ?, ?,    0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, nowStr, nowStr, nowStr, nowStr, nowStr) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/closed", nil)
	rec := httptest.NewRecorder()
	ClosedPRsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var prs []PR
	if err := json.NewDecoder(rec.Body).Decode(&prs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 closed/merged PRs, got %d", len(prs))
	}
	// Should not include the open PR.
	for _, pr := range prs {
		if pr.Status == "open" {
			t.Errorf("unexpected open PR in closed PRs response: #%d", pr.Number)
		}
	}
}

// --- anvilDirForBead ---

func writeAnvilConfig(t *testing.T, home, key, path string) {
	t.Helper()
	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("anvils:\n  %s:\n    path: %s\n", key, path)
	if err := os.WriteFile(filepath.Join(forgeDir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAnvilDirForBead_ExactMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := t.TempDir()
	writeAnvilConfig(t, home, "Hytte", want)

	got, err := anvilDirForBead("Hytte-abc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnvilDirForBead_LowercaseKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := t.TempDir()
	writeAnvilConfig(t, home, "hytte", want)

	// Bead ID uses capitalized prefix; config key is lowercase.
	got, err := anvilDirForBead("Hytte-abc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnvilDirForBead_MixedCaseKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := t.TempDir()
	writeAnvilConfig(t, home, "HYTTE", want)

	// Bead ID prefix and config key differ in casing — EqualFold scan catches it.
	got, err := anvilDirForBead("Hytte-abc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- FixCommentsPRHandler ---

func fixCommentsPRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/fix-comments", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestFixCommentsPRHandler_EmptyID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//fix-comments", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	FixCommentsPRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFixCommentsPRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	FixCommentsPRHandler(fdb).ServeHTTP(rec, fixCommentsPRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsPRHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	FixCommentsPRHandler(nil).ServeHTTP(rec, fixCommentsPRRequest("42"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsPRHandler_NoDaemon(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	FixCommentsPRHandler(fdb).ServeHTTP(rec, fixCommentsPRRequest(fmt.Sprintf("%d", prDBID)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsPRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	FixCommentsPRHandler(fdb).ServeHTTP(rec, fixCommentsPRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "pr_action" {
			t.Errorf("expected command type 'pr_action', got %q", cmd.Type)
		}
		var pa prActionPayload
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.Action != "burnish" {
			t.Errorf("expected action 'burnish', got %q", pa.Action)
		}
		if pa.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", pa.PRNumber)
		}
		if pa.Anvil != "hytte" {
			t.Errorf("expected anvil 'hytte', got %q", pa.Anvil)
		}
		if pa.Branch != "forge/Hytte-abc1" {
			t.Errorf("expected branch 'forge/Hytte-abc1', got %q", pa.Branch)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- FixCIPRHandler ---

func fixCIPRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/fix-ci", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestFixCIPRHandler_EmptyID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//fix-ci", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	FixCIPRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFixCIPRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	FixCIPRHandler(fdb).ServeHTTP(rec, fixCIPRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCIPRHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	FixCIPRHandler(nil).ServeHTTP(rec, fixCIPRRequest("42"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCIPRHandler_NoDaemon(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	FixCIPRHandler(fdb).ServeHTTP(rec, fixCIPRRequest(fmt.Sprintf("%d", prDBID)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCIPRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	FixCIPRHandler(fdb).ServeHTTP(rec, fixCIPRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "pr_action" {
			t.Errorf("expected command type 'pr_action', got %q", cmd.Type)
		}
		var pa prActionPayload
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.Action != "quench" {
			t.Errorf("expected action 'quench', got %q", pa.Action)
		}
		if pa.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", pa.PRNumber)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- FixConflictsPRHandler ---

func fixConflictsPRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/fix-conflicts", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestFixConflictsPRHandler_EmptyID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//fix-conflicts", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	FixConflictsPRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFixConflictsPRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	FixConflictsPRHandler(fdb).ServeHTTP(rec, fixConflictsPRRequest("not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixConflictsPRHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	FixConflictsPRHandler(nil).ServeHTTP(rec, fixConflictsPRRequest("42"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixConflictsPRHandler_NoDaemon(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	rec := httptest.NewRecorder()
	FixConflictsPRHandler(fdb).ServeHTTP(rec, fixConflictsPRRequest(fmt.Sprintf("%d", prDBID)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when daemon is not running, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixConflictsPRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	prDBID := insertTestPR(t, fdb, 42, "hytte", "Hytte-abc1", "forge/Hytte-abc1")

	socketPath := filepath.Join(t.TempDir(), "forge.sock")
	received := receiveIPCCommand(t, socketPath)

	t.Setenv("FORGE_IPC_SOCKET", socketPath)
	rec := httptest.NewRecorder()
	FixConflictsPRHandler(fdb).ServeHTTP(rec, fixConflictsPRRequest(fmt.Sprintf("%d", prDBID)))

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
		if cmd.Type != "pr_action" {
			t.Errorf("expected command type 'pr_action', got %q", cmd.Type)
		}
		var pa prActionPayload
		if err := json.Unmarshal(cmd.Payload, &pa); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if pa.Action != "rebase" {
			t.Errorf("expected action 'rebase', got %q", pa.Action)
		}
		if pa.PRNumber != 42 {
			t.Errorf("expected pr_number 42, got %d", pa.PRNumber)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

// --- ResetCountersPRHandler ---

func resetCountersPRRequest(prID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs/"+prID+"/reset-counters", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", prID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestResetCountersPRHandler_EmptyID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forge/prs//reset-counters", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ResetCountersPRHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestResetCountersPRHandler_InvalidID(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	ResetCountersPRHandler(fdb).ServeHTTP(rec, resetCountersPRRequest("abc"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestResetCountersPRHandler_NoDB(t *testing.T) {
	rec := httptest.NewRecorder()
	ResetCountersPRHandler(nil).ServeHTTP(rec, resetCountersPRRequest("42"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestResetCountersPRHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	_, err := fdb.db.Exec(
		`INSERT INTO prs (id, number, anvil, branch, base_branch, title, status, created_at, last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting, has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		 VALUES (42, 1, 'owner/repo', 'feat/x', 'main', 'Test PR', 'open', '2024-01-01', '2024-01-01', 3, 2, 0, 0, 0, 0, 0, 0, 1)`)
	if err != nil {
		t.Fatalf("insert PR: %v", err)
	}

	rec := httptest.NewRecorder()
	ResetCountersPRHandler(fdb).ServeHTTP(rec, resetCountersPRRequest("42"))

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

	var ciCount, reviewCount int
	if err := fdb.db.QueryRow(`SELECT ci_fix_count, review_fix_count FROM prs WHERE id = 42`).Scan(&ciCount, &reviewCount); err != nil {
		t.Fatalf("query: %v", err)
	}
	if ciCount != 0 || reviewCount != 0 {
		t.Errorf("expected counters reset to 0, got ci=%d review=%d", ciCount, reviewCount)
	}
}

// --- AllPRsHandler ---

func TestAllPRsHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/all", nil)
	AllPRsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AllPRsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// forge_prs must be empty when db is nil
	if resp.ForgePRs == nil || len(resp.ForgePRs) != 0 {
		t.Errorf("expected empty forge_prs, got %d", len(resp.ForgePRs))
	}
	// external_prs fetch is always attempted (db is not required); result may be non-empty
	if resp.ExternalPRs == nil {
		t.Error("expected non-nil external_prs")
	}
}

func TestAllPRsHandler_WithDB_ReturnsForgePRs(t *testing.T) {
	fdb := setupTestDB(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	fdb.db.Exec(`INSERT INTO prs (id, number, anvil, bead_id, branch, base_branch, title, status, created_at,
		last_checked, ci_fix_count, review_fix_count, ci_passing, rebase_count, is_conflicting,
		has_unresolved_threads, has_pending_reviews, has_approval, bellows_managed)
		VALUES
		  (1, 42, 'owner/repo', 'b1', 'feat/b1', 'main', 'Test PR', 'open', ?, NULL, 0, 0, 1, 0, 0, 0, 0, 1, 0)
	`, nowStr) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/all", nil)
	AllPRsHandler(fdb).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AllPRsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp.ForgePRs) != 1 {
		t.Fatalf("expected 1 forge PR, got %d", len(resp.ForgePRs))
	}
	if resp.ForgePRs[0].Number != 42 {
		t.Errorf("expected PR #42, got #%d", resp.ForgePRs[0].Number)
	}
	// External PRs should be empty (no gh CLI / forge config in test env)
	if resp.ExternalPRs == nil {
		t.Error("expected non-nil external_prs")
	}
}

func TestAllPRsHandler_NilDB_AttempsExternalFetch(t *testing.T) {
	// When db is nil, forge PRs are empty but external fetch is still attempted.
	// In the test environment there is no forge config, so external PRs will also be empty.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/forge/prs/all", nil)
	AllPRsHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AllPRsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp.ForgePRs) != 0 {
		t.Errorf("expected empty forge_prs, got %d", len(resp.ForgePRs))
	}
	// External PRs will be empty because there is no forge config in the test environment.
	if resp.ExternalPRs == nil {
		t.Error("expected non-nil external_prs slice")
	}
}

// --- parseGitHubRepo ---

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:Robin831/Hytte.git", "Robin831/Hytte"},
		{"https://github.com/Robin831/Hytte.git", "Robin831/Hytte"},
		{"https://github.com/Robin831/Hytte", "Robin831/Hytte"},
		{"git@github.com:owner/repo.git", "owner/repo"},
	}
	for _, tt := range tests {
		got := parseGitHubRepo(tt.input)
		if got != tt.want {
			t.Errorf("parseGitHubRepo(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- filterExternal ---

func TestFilterExternal(t *testing.T) {
	forgePRs := []PR{
		{Number: 1, Anvil: "owner/repo"},
		{Number: 3, Anvil: "owner/repo"},
	}
	allGitHub := []ExternalPR{
		{Number: 1, Anvil: "owner/repo", Title: "forge pr"},
		{Number: 2, Anvil: "owner/repo", Title: "external pr"},
		{Number: 3, Anvil: "owner/repo", Title: "another forge"},
		{Number: 4, Anvil: "owner/other", Title: "other repo pr"},
	}
	result := filterExternal(allGitHub, forgePRs)
	if len(result) != 2 {
		t.Fatalf("expected 2 external PRs, got %d", len(result))
	}
	if result[0].Number != 2 {
		t.Errorf("expected PR #2, got #%d", result[0].Number)
	}
	if result[1].Number != 4 {
		t.Errorf("expected PR #4, got #%d", result[1].Number)
	}
}

// --- ApproveExternalPRHandler / MergeExternalPRHandler ---

func extPRRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/approve", strings.NewReader(body))
}

func TestApproveExternalPRHandler_InvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest("{bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveExternalPRHandler_InvalidRepo(t *testing.T) {
	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest(`{"repo":"bad repo!","number":1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveExternalPRHandler_ZeroNumber(t *testing.T) {
	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest(`{"repo":"owner/repo","number":0}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveExternalPRHandler_BodyTooLarge(t *testing.T) {
	// Build a JSON body that exceeds the 4096-byte limit.
	large := `{"repo":"owner/repo","number":1,"extra":"` + strings.Repeat("x", 5000) + `"}`
	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest(large))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergeExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/merge", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	MergeExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergeExternalPRHandler_InvalidRepo(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/merge", strings.NewReader(`{"repo":"../evil","number":1}`))
	rec := httptest.NewRecorder()
	MergeExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveExternalPRHandler_Success(t *testing.T) {
	// Create a fake gh binary that succeeds.
	tmpBin := t.TempDir()
	ghScript := filepath.Join(tmpBin, "gh")
	if err := os.WriteFile(ghScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpBin)

	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest(`{"repo":"owner/repo","number":42}`))
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

func TestMergeExternalPRHandler_Success(t *testing.T) {
	tmpBin := t.TempDir()
	ghScript := filepath.Join(tmpBin, "gh")
	if err := os.WriteFile(ghScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpBin)

	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/merge", strings.NewReader(`{"repo":"owner/repo","number":7}`))
	rec := httptest.NewRecorder()
	MergeExternalPRHandler().ServeHTTP(rec, req)
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

func TestMergeExternalPRHandler_GhFailure(t *testing.T) {
	tmpBin := t.TempDir()
	ghScript := filepath.Join(tmpBin, "gh")
	if err := os.WriteFile(ghScript, []byte("#!/bin/sh\necho 'merge failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpBin)

	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/merge", strings.NewReader(`{"repo":"owner/repo","number":7}`))
	rec := httptest.NewRecorder()
	MergeExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// setupForgeConfigWithRepo creates a temp HOME containing a forge config with one
// anvil, and a fake git binary that returns remoteURL for any remote query.
// It returns the temp bin dir so the caller can add additional binaries (e.g. gh).
func setupForgeConfigWithRepo(t *testing.T, remoteURL string) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	forgeDir := filepath.Join(tmpHome, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	anvilPath := filepath.Join(tmpHome, "myrepo")
	if err := os.MkdirAll(anvilPath, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "anvils:\n  myAnvil:\n    path: " + anvilPath + "\n"
	if err := os.WriteFile(filepath.Join(forgeDir, "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpBin := t.TempDir()
	gitScript := filepath.Join(tmpBin, "git")
	if err := os.WriteFile(gitScript, []byte("#!/bin/sh\necho '"+remoteURL+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return tmpBin
}

func TestApproveExternalPRHandler_RepoNotAllowed(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/allowed-repo.git")
	t.Setenv("PATH", tmpBin)

	rec := httptest.NewRecorder()
	ApproveExternalPRHandler().ServeHTTP(rec, extPRRequest(`{"repo":"owner/other-repo","number":42}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergeExternalPRHandler_RepoNotAllowed(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/allowed-repo.git")
	t.Setenv("PATH", tmpBin)

	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/merge", strings.NewReader(`{"repo":"owner/other-repo","number":7}`))
	rec := httptest.NewRecorder()
	MergeExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- externalPRDaemonHandler-based handlers ---

func TestFixCommentsExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-comments", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	FixCommentsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsExternalPRHandler_InvalidRepo(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-comments", strings.NewReader(`{"repo":"../evil","number":1}`))
	rec := httptest.NewRecorder()
	FixCommentsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsExternalPRHandler_ZeroNumber(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-comments", strings.NewReader(`{"repo":"owner/repo","number":0}`))
	rec := httptest.NewRecorder()
	FixCommentsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsExternalPRHandler_NoDaemon(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-comments", strings.NewReader(`{"repo":"owner/repo","number":1}`))
	rec := httptest.NewRecorder()
	FixCommentsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCIExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-ci", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	FixCIExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCIExternalPRHandler_NoDaemon(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-ci", strings.NewReader(`{"repo":"owner/repo","number":1}`))
	rec := httptest.NewRecorder()
	FixCIExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixConflictsExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-conflicts", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	FixConflictsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixConflictsExternalPRHandler_NoDaemon(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-conflicts", strings.NewReader(`{"repo":"owner/repo","number":1}`))
	rec := httptest.NewRecorder()
	FixConflictsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/bellows", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	BellowsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsExternalPRHandler_NoDaemon(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/bellows", strings.NewReader(`{"repo":"owner/repo","number":1}`))
	rec := httptest.NewRecorder()
	BellowsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResetCountersExternalPRHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/reset-counters", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	ResetCountersExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResetCountersExternalPRHandler_NoDaemon(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))
	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/reset-counters", strings.NewReader(`{"repo":"owner/repo","number":1}`))
	rec := httptest.NewRecorder()
	ResetCountersExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixCommentsExternalPRHandler_RepoNotAllowed(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/allowed-repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))

	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/fix-comments", strings.NewReader(`{"repo":"owner/other-repo","number":42}`))
	rec := httptest.NewRecorder()
	FixCommentsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBellowsExternalPRHandler_RepoNotAllowed(t *testing.T) {
	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/allowed-repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", filepath.Join(t.TempDir(), "no-such.sock"))

	req := httptest.NewRequest(http.MethodPost, "/api/forge/ext-prs/bellows", strings.NewReader(`{"repo":"owner/other-repo","number":42}`))
	rec := httptest.NewRecorder()
	BellowsExternalPRHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- success-path tests for externalPRDaemonHandler-based handlers ---
// Each test spins up a real unix socket, asserts HTTP 200 and ok=true,
// and verifies the exact command string written to the socket (command + "repo#number").

func testExternalPRDaemonSuccess(t *testing.T, handler http.Handler, endpoint, expectedCmd string) {
	t.Helper()
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

	tmpBin := setupForgeConfigWithRepo(t, "https://github.com/owner/repo.git")
	t.Setenv("PATH", tmpBin)
	t.Setenv("FORGE_IPC_SOCKET", socketPath)

	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"repo":"owner/repo","number":42}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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
		if cmd != expectedCmd {
			t.Errorf("expected command %q, got %q", expectedCmd, cmd)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for command on socket")
	}
}

func TestFixCommentsExternalPRHandler_Success(t *testing.T) {
	testExternalPRDaemonSuccess(t, FixCommentsExternalPRHandler(), "/api/forge/ext-prs/fix-comments", `{"type":"external_pr_action","payload":{"repo":"owner/repo","number":42,"action":"fix-comments"}}`)
}

func TestFixCIExternalPRHandler_Success(t *testing.T) {
	testExternalPRDaemonSuccess(t, FixCIExternalPRHandler(), "/api/forge/ext-prs/fix-ci", `{"type":"external_pr_action","payload":{"repo":"owner/repo","number":42,"action":"fix-ci"}}`)
}

func TestFixConflictsExternalPRHandler_Success(t *testing.T) {
	testExternalPRDaemonSuccess(t, FixConflictsExternalPRHandler(), "/api/forge/ext-prs/fix-conflicts", `{"type":"external_pr_action","payload":{"repo":"owner/repo","number":42,"action":"rebase"}}`)
}

func TestBellowsExternalPRHandler_Success(t *testing.T) {
	testExternalPRDaemonSuccess(t, BellowsExternalPRHandler(), "/api/forge/ext-prs/bellows", `{"type":"external_pr_action","payload":{"repo":"owner/repo","number":42,"action":"bellows"}}`)
}

func TestResetCountersExternalPRHandler_Success(t *testing.T) {
	testExternalPRDaemonSuccess(t, ResetCountersExternalPRHandler(), "/api/forge/ext-prs/reset-counters", `{"type":"external_pr_action","payload":{"repo":"owner/repo","number":42,"action":"reset-counters"}}`)
}
