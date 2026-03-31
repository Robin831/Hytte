package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// mockIPC is a stub IPCClient for use in handler tests.
type mockIPC struct {
	sendErr error
	sendOut []byte
}

func (m *mockIPC) Health() error { return nil }
func (m *mockIPC) SendCommand(cmd string) ([]byte, error) {
	return m.sendOut, m.sendErr
}

// --- StatusHandler ---

func TestStatusHandler_NilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/status", nil)
	rec := httptest.NewRecorder()
	StatusHandler(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestStatusHandler_WithDB_NilIPC(t *testing.T) {
	fdb := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/forge/status", nil)
	rec := httptest.NewRecorder()
	StatusHandler(fdb, nil).ServeHTTP(rec, req)

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
		QueueReady int      `json:"queue_ready"`
		NeedsHuman int      `json:"needs_human"`
		Stuck      []Retry  `json:"stuck"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DaemonHealthy {
		t.Error("expected daemon_healthy=false when IPC is nil")
	}
	if body.DaemonError == "" {
		t.Error("expected daemon_error to be set when IPC is nil")
	}
	if body.WorkerList == nil {
		t.Error("expected worker_list to be a non-nil slice")
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
	StatusHandler(fdb, nil).ServeHTTP(rec, req)

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

func TestRetryBeadHandler_NilIPC(t *testing.T) {
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
	rec := httptest.NewRecorder()
	// ID with characters that fail the regexp.
	RetryBeadHandler(nil).ServeHTTP(rec, retryRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRetryBeadHandler_Success(t *testing.T) {
	mock := &mockIPC{sendOut: []byte("ok")}
	rec := httptest.NewRecorder()
	RetryBeadHandler(mock).ServeHTTP(rec, retryRequest("Hytte-abc1"))

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

func TestRetryBeadHandler_SendCommandError(t *testing.T) {
	mock := &mockIPC{sendErr: fmt.Errorf("socket closed")}
	rec := httptest.NewRecorder()
	RetryBeadHandler(mock).ServeHTTP(rec, retryRequest("Hytte-abc1"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}
