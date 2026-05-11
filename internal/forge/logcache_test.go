package forge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// cacheTestSetup wires up an in-memory ForgeDB and writes a worker row whose
// log_path points at a freshly created file under HOME/.forge. Returns the DB,
// the absolute log path, and the cache key the handler will use so tests can
// inspect cache state directly.
func cacheTestSetup(t *testing.T, initialLines []string) (*DB, string, string) {
	t.Helper()
	fdb := setupTestDB(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	forgeDir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	logPath := filepath.Join(forgeDir, "cached.jsonl")
	if err := os.WriteFile(logPath, []byte(joinLines(initialLines)), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	fdb.db.Exec( //nolint:errcheck
		`INSERT INTO workers (id, bead_id, anvil, branch, pid, status, phase, title, started_at, log_path, pr_number) VALUES ('worker-c1', 'b1', 'a', 'feat/b1', 1, 'running', 'impl', 'T', ?, ?, 0)`,
		time.Now().UTC().Format(time.RFC3339), logPath,
	)

	// EvalSymlinks is part of the cache key the handler computes. On macOS
	// /var → /private/var, so use the resolved path here too.
	resolved, err := filepath.EvalSymlinks(logPath)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	key := "worker-c1|" + filepath.Clean(resolved)
	return fdb, logPath, key
}

func joinLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}

// textLine produces an assistant text-block stream-json line.
func textLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]}}`, text)
}

// callHandler invokes WorkerParsedLogHandler and decodes the JSON response.
func callHandler(t *testing.T, fdb *DB, query string) (int, []LogEntry) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := workerParsedLogRequest("worker-c1")
	if query != "" {
		req = workerParsedLogRequest("worker-c1", query)
	}
	WorkerParsedLogHandler(fdb).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var entries []LogEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec.Code, entries
}

// cacheSnapshot captures the per-entry fields tests assert against, copied
// out under both the cache and entry mutexes so subsequent reads are race-
// free even if a concurrent request is still in flight.
type cacheSnapshot struct {
	parseCount int
	nextOffset int64
	entryCount int
}

// snapshotEntry returns a stable snapshot of the cache entry for key.
func snapshotEntry(t *testing.T, fdb *DB, key string) cacheSnapshot {
	t.Helper()
	fdb.parsedLogs.mu.Lock()
	e, ok := fdb.parsedLogs.entries[key]
	fdb.parsedLogs.mu.Unlock()
	if !ok {
		t.Fatalf("cache entry %q missing", key)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return cacheSnapshot{
		parseCount: e.parseCount,
		nextOffset: e.nextOffset,
		entryCount: len(e.state.Entries),
	}
}

// TestParsedLogCache_UnchangedFileSkipsReParse verifies the fast path: a
// second request against an unchanged file does not re-scan any bytes and
// does not re-grow the entries slice.
func TestParsedLogCache_UnchangedFileSkipsReParse(t *testing.T) {
	fdb, _, key := cacheTestSetup(t, []string{
		textLine("first"),
		textLine("second"),
		textLine("third"),
	})

	if code, entries := callHandler(t, fdb, ""); code != http.StatusOK || len(entries) != 3 {
		t.Fatalf("first call: code=%d, entries=%d", code, len(entries))
	}
	first := snapshotEntry(t, fdb, key)
	if first.parseCount != 1 {
		t.Fatalf("expected parseCount=1 after first call, got %d", first.parseCount)
	}

	if code, entries := callHandler(t, fdb, ""); code != http.StatusOK || len(entries) != 3 {
		t.Fatalf("second call: code=%d, entries=%d", code, len(entries))
	}
	second := snapshotEntry(t, fdb, key)
	if second.parseCount != 1 {
		t.Errorf("expected parseCount to stay at 1 on unchanged file, got %d", second.parseCount)
	}
	if second.nextOffset != first.nextOffset {
		t.Errorf("expected nextOffset unchanged (%d), got %d", first.nextOffset, second.nextOffset)
	}
	if second.entryCount != 3 {
		t.Errorf("expected entries slice length 3, got %d", second.entryCount)
	}
}

// TestParsedLogCache_AppendOnlyParsesNewBytes verifies that appending to the
// log triggers exactly one additional parse pass that scans only the new
// bytes (offset advances by the appended length, not from zero).
func TestParsedLogCache_AppendOnlyParsesNewBytes(t *testing.T) {
	fdb, logPath, key := cacheTestSetup(t, []string{
		textLine("first"),
		textLine("second"),
	})

	if _, entries := callHandler(t, fdb, ""); len(entries) != 2 {
		t.Fatalf("expected 2 initial entries, got %d", len(entries))
	}
	before := snapshotEntry(t, fdb, key)
	offsetBefore := before.nextOffset

	// Append two more lines.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	appended := joinLines([]string{textLine("third"), textLine("fourth")})
	if _, err := f.WriteString(appended); err != nil {
		t.Fatalf("write append: %v", err)
	}
	f.Close()

	_, entries := callHandler(t, fdb, "")
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries after append, got %d", len(entries))
	}
	if entries[2].Content != "third" || entries[3].Content != "fourth" {
		t.Errorf("appended entries wrong: %+v", entries[2:])
	}
	// Sequence numbers must remain monotonically increasing across the
	// append boundary so the frontend's tail logic stays consistent.
	for i, e := range entries {
		if e.Seq != i {
			t.Errorf("entries[%d].Seq = %d, want %d", i, e.Seq, i)
		}
	}

	after := snapshotEntry(t, fdb, key)
	if after.parseCount != 2 {
		t.Errorf("expected parseCount=2 (initial + append), got %d", after.parseCount)
	}
	if after.nextOffset != offsetBefore+int64(len(appended)) {
		t.Errorf("expected nextOffset=%d, got %d", offsetBefore+int64(len(appended)), after.nextOffset)
	}
}

// TestParsedLogCache_TruncationInvalidates verifies that truncating the log
// (size shrinks below the cached offset) resets the cache so the next call
// re-parses from byte 0.
func TestParsedLogCache_TruncationInvalidates(t *testing.T) {
	fdb, logPath, key := cacheTestSetup(t, []string{
		textLine("alpha"),
		textLine("beta"),
		textLine("gamma"),
	})

	if _, entries := callHandler(t, fdb, ""); len(entries) != 3 {
		t.Fatalf("expected 3 initial entries, got %d", len(entries))
	}

	// Truncate the file and replace its content with a single different line.
	if err := os.WriteFile(logPath, []byte(textLine("brand-new")+"\n"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// Force the mtime forward so the file-shrunk branch fires deterministically
	// even on filesystems with coarse mtime granularity.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(logPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, entries := callHandler(t, fdb, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after truncate+rewrite, got %d", len(entries))
	}
	if entries[0].Content != "brand-new" {
		t.Errorf("expected new entry content, got %q", entries[0].Content)
	}
	if entries[0].Seq != 0 {
		t.Errorf("expected seq to reset to 0 after truncate, got %d", entries[0].Seq)
	}

	after := snapshotEntry(t, fdb, key)
	if after.parseCount != 2 {
		t.Errorf("expected parseCount=2 (initial + post-truncate re-parse), got %d", after.parseCount)
	}
}

// TestParsedLogCache_MtimeRegressionInvalidates verifies that a backwards-
// jumping mtime (file replaced with same-size content) also resets the cache.
func TestParsedLogCache_MtimeRegressionInvalidates(t *testing.T) {
	fdb, logPath, key := cacheTestSetup(t, []string{
		textLine("v1-a"),
		textLine("v1-b"),
	})

	if _, entries := callHandler(t, fdb, ""); len(entries) != 2 {
		t.Fatalf("expected 2 initial entries, got %d", len(entries))
	}

	// Rewrite with content of the same byte length but a backwards mtime.
	newContent := joinLines([]string{textLine("v2-a"), textLine("v2-b")})
	if err := os.WriteFile(logPath, []byte(newContent), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(logPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, entries := callHandler(t, fdb, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after mtime regression, got %d", len(entries))
	}
	if entries[0].Content != "v2-a" || entries[1].Content != "v2-b" {
		t.Errorf("expected re-parsed content, got %+v", entries)
	}

	after := snapshotEntry(t, fdb, key)
	if after.parseCount != 2 {
		t.Errorf("expected parseCount=2 after mtime regression, got %d", after.parseCount)
	}
}

// TestParsedLogCache_ConcurrentRequestsSerializeParse verifies that many
// goroutines hitting the same worker only trigger a single parse pass: all
// callers serialize on the per-entry mutex and only the first one observes
// new bytes.
func TestParsedLogCache_ConcurrentRequestsSerializeParse(t *testing.T) {
	fdb, _, key := cacheTestSetup(t, []string{
		textLine("c1"),
		textLine("c2"),
		textLine("c3"),
		textLine("c4"),
	})

	const goroutines = 16
	var wg sync.WaitGroup
	codes := make([]int, goroutines)
	bodies := make([][]byte, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			WorkerParsedLogHandler(fdb).ServeHTTP(rec, workerParsedLogRequest("worker-c1"))
			codes[idx] = rec.Code
			bodies[idx] = rec.Body.Bytes()
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("goroutine %d: expected 200, got %d", i, code)
		}
		var entries []LogEntry
		if err := json.Unmarshal(bodies[i], &entries); err != nil {
			t.Fatalf("goroutine %d: decode: %v", i, err)
		}
		if len(entries) != 4 {
			t.Errorf("goroutine %d: expected 4 entries, got %d", i, len(entries))
		}
	}

	after := snapshotEntry(t, fdb, key)
	if after.parseCount != 1 {
		t.Errorf("expected exactly 1 parse pass under contention, got %d", after.parseCount)
	}
}

// TestParsedLogCache_LRUEvictsOldest verifies the LRU evicts the entry with
// the oldest lastAccess once the cache is full.
func TestParsedLogCache_LRUEvictsOldest(t *testing.T) {
	c := newParsedLogCache(2)

	a := c.getOrCreate("a")
	b := c.getOrCreate("b")
	// Backdate lastAccess under the cache lock so writes are visible to
	// the next getOrCreate's evictOldestLocked scan.
	c.mu.Lock()
	a.lastAccess = time.Now().Add(-3 * time.Hour)
	b.lastAccess = time.Now().Add(-1 * time.Hour)
	c.mu.Unlock()

	// Inserting "c" should evict "a" (oldest lastAccess).
	c.getOrCreate("c")

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries["a"]; ok {
		t.Errorf("expected oldest entry 'a' to be evicted")
	}
	if _, ok := c.entries["b"]; !ok {
		t.Errorf("expected 'b' to be retained")
	}
	if _, ok := c.entries["c"]; !ok {
		t.Errorf("expected 'c' to be present")
	}
}
