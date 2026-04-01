package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// workerParsedLogRequest builds a GET request with chi URL param {id} set.
func workerParsedLogRequest(workerID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/forge/workers/"+workerID+"/log/parsed", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workerID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// writeLogFile creates a temporary log file with the given content and returns its path.
func writeLogFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write log file: %v", err)
	}
	return p
}

// --- ParseWorkerLog ---

func TestParseWorkerLog_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := writeLogFile(t, dir, "empty.jsonl", "")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseWorkerLog_FileNotFound(t *testing.T) {
	_, err := ParseWorkerLog("/nonexistent/path/log.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseWorkerLog_TextBlock(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello, world!"}]}}`
	dir := t.TempDir()
	p := writeLogFile(t, dir, "text.jsonl", line+"\n")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Type != "text" {
		t.Errorf("expected type=text, got %q", e.Type)
	}
	if e.Content != "Hello, world!" {
		t.Errorf("unexpected content: %q", e.Content)
	}
}

func TestParseWorkerLog_ThinkBlock(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"I am thinking..."}]}}`
	dir := t.TempDir()
	p := writeLogFile(t, dir, "think.jsonl", line+"\n")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Type != "think" {
		t.Errorf("expected type=think, got %q", e.Type)
	}
	if e.Content != "I am thinking..." {
		t.Errorf("unexpected content: %q", e.Content)
	}
}

func TestParseWorkerLog_ToolUseWithSuccess(t *testing.T) {
	assistantLine := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls -la"}}]}}`
	userLine := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"file1\nfile2","is_error":false}]}}`
	dir := t.TempDir()
	p := writeLogFile(t, dir, "tool.jsonl", assistantLine+"\n"+userLine+"\n")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (tool_result merged into tool_use), got %d", len(entries))
	}
	e := entries[0]
	if e.Type != "tool_use" {
		t.Errorf("expected type=tool_use, got %q", e.Type)
	}
	if e.Name != "Bash" {
		t.Errorf("expected name=Bash, got %q", e.Name)
	}
	if e.Status != "success" {
		t.Errorf("expected status=success, got %q", e.Status)
	}
	if !strings.Contains(e.Content, "ls -la") {
		t.Errorf("expected content to contain command, got %q", e.Content)
	}
	if !strings.Contains(e.Content, "file1") {
		t.Errorf("expected content to contain result, got %q", e.Content)
	}
}

func TestParseWorkerLog_ToolUseWithError(t *testing.T) {
	assistantLine := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu2","name":"Bash","input":{"command":"bad-cmd"}}]}}`
	userLine := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"command not found","is_error":true}]}}`
	dir := t.TempDir()
	p := writeLogFile(t, dir, "tool_err.jsonl", assistantLine+"\n"+userLine+"\n")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Status != "error" {
		t.Errorf("expected status=error, got %q", entries[0].Status)
	}
}

func TestParseWorkerLog_SkipsBlankTextBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"   "}]}}`
	dir := t.TempDir()
	p := writeLogFile(t, dir, "blank.jsonl", line+"\n")

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected blank text block to be skipped, got %d entries", len(entries))
	}
}

func TestParseWorkerLog_MalformedLinesSkipped(t *testing.T) {
	lines := "not json at all\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"valid"}]}}` + "\n"
	dir := t.TempDir()
	p := writeLogFile(t, dir, "mixed.jsonl", lines)

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry, got %d", len(entries))
	}
}

func TestParseWorkerLog_MultipleEntries(t *testing.T) {
	lines := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"plan"}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"doing thing"}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu3","name":"Read","input":{"file_path":"/tmp/foo"}}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu3","content":"file contents","is_error":false}]}}` + "\n"
	dir := t.TempDir()
	p := writeLogFile(t, dir, "multi.jsonl", lines)

	entries, err := ParseWorkerLog(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (think + text + tool_use), got %d", len(entries))
	}
	if entries[0].Type != "think" {
		t.Errorf("entries[0]: expected think, got %q", entries[0].Type)
	}
	if entries[1].Type != "text" {
		t.Errorf("entries[1]: expected text, got %q", entries[1].Type)
	}
	if entries[2].Type != "tool_use" {
		t.Errorf("entries[2]: expected tool_use, got %q", entries[2].Type)
	}
	if entries[2].Status != "success" {
		t.Errorf("entries[2]: expected status=success, got %q", entries[2].Status)
	}
}

// --- WorkerParsedLogHandler ---

func TestWorkerParsedLogHandler_NilDB(t *testing.T) {
	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(nil).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestWorkerParsedLogHandler_InvalidID(t *testing.T) {
	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(nil).ServeHTTP(rec, workerParsedLogRequest("../etc/passwd"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestWorkerParsedLogHandler_WorkerNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWorkerParsedLogHandler_NoLogPath(t *testing.T) {
	fdb := setupTestDB(t)
	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, '', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWorkerParsedLogHandler_PathTraversal(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, '../../../etc/passwd', 0)`, time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path-traversal attempt, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerParsedLogHandler_LogFileNotFound(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(forgeDir, "nonexistent.jsonl")

	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), logPath) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing log file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerParsedLogHandler_Success(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	logContent := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello from worker"}]}}` + "\n"
	logPath := writeLogFile(t, forgeDir, "worker.jsonl", logContent)

	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), logPath) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []LogEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Type != "text" {
		t.Errorf("expected type=text, got %q", entries[0].Type)
	}
	if entries[0].Content != "Hello from worker" {
		t.Errorf("unexpected content: %q", entries[0].Content)
	}
}

func TestWorkerParsedLogHandler_EmptyLogReturnsEmptyArray(t *testing.T) {
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := writeLogFile(t, forgeDir, "empty.jsonl", "")

	fdb.db.Exec(`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-abc1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`, time.Now().UTC().Format(time.RFC3339), logPath) //nolint:errcheck

	rec := httptest.NewRecorder()
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-abc1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []LogEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil slice for empty log")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
