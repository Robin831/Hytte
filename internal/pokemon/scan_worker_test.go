package pokemon

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
)

// writeTempScanImage writes a tiny PNG-ish payload to a temp file and returns
// (path, sha256_hex). The bytes are not a real image — the worker does not
// inspect them; that is the HTTP endpoint's job (next bead). All the worker
// needs is a readable path on disk.
func writeTempScanImage(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "card.png")
	payload := append([]byte{0x89, 0x50, 0x4E, 0x47}, []byte("scan-worker-test")...)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatalf("write temp image: %v", err)
	}
	sum := sha256.Sum256(payload)
	return path, hex.EncodeToString(sum[:])
}

// enqueueScanJob inserts a queued pokemon_scan_jobs row with the supplied
// on-disk image path (encrypted via encryption.EncryptField, just like the
// production enqueue path will).
func enqueueScanJob(t *testing.T, db *sql.DB, userID int64, imagePath, imageHash string) int64 {
	t.Helper()
	enc, err := encryption.EncryptField(imagePath)
	if err != nil {
		t.Fatalf("encrypt image path: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, scanJobStatusQueued, enc, imageHash, time.Now().UTC())
	if err != nil {
		t.Fatalf("enqueue scan job: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// scanJobRow is the test-side view of the row, exposing the columns assertions
// care about. claude_response_enc + image_hash are read as nullable because
// the helper inserts and the worker writes them in different code paths.
type scanJobRow struct {
	Status         string
	MatchedCardID  sql.NullString
	MatchedVariant sql.NullInt64
	Confidence     sql.NullFloat64
	ErrorMessage   sql.NullString
	ResponseEnc    sql.NullString
	ProcessedAt    sql.NullTime
}

func readScanJob(t *testing.T, db *sql.DB, jobID int64) scanJobRow {
	t.Helper()
	var row scanJobRow
	err := db.QueryRow(`
		SELECT status, matched_card_id, matched_variant_id, confidence,
		       error_message, claude_response_enc, processed_at
		FROM pokemon_scan_jobs WHERE id = ?
	`, jobID).Scan(&row.Status, &row.MatchedCardID, &row.MatchedVariant,
		&row.Confidence, &row.ErrorMessage, &row.ResponseEnc, &row.ProcessedAt)
	if err != nil {
		t.Fatalf("read scan job %d: %v", jobID, err)
	}
	return row
}

func TestProcessScanJob_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.92}`, nil)

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusMatched {
		t.Fatalf("expected status=matched, got %q (err=%q)", row.Status, row.ErrorMessage.String)
	}
	if !row.MatchedCardID.Valid || row.MatchedCardID.String != "sv1-25" {
		t.Errorf("expected matched_card_id=sv1-25, got %+v", row.MatchedCardID)
	}
	if !row.Confidence.Valid || row.Confidence.Float64 != 0.92 {
		t.Errorf("expected confidence=0.92, got %+v", row.Confidence)
	}
	if !row.ResponseEnc.Valid || row.ResponseEnc.String == "" {
		t.Errorf("expected claude_response_enc to be populated, got %+v", row.ResponseEnc)
	}
	if !row.ProcessedAt.Valid {
		t.Errorf("expected processed_at to be set")
	}
}

func TestProcessScanJob_MalformedJSON_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, "i give up", nil)

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusNoMatch {
		t.Fatalf("expected status=no_match on malformed JSON, got %q", row.Status)
	}
	if !row.ErrorMessage.Valid || row.ErrorMessage.String == "" {
		t.Errorf("expected error_message to describe parse failure, got %+v", row.ErrorMessage)
	}
}

func TestProcessScanJob_ClaudeError_Failed(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, "", &stubError{msg: "claude CLI exploded"})

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusFailed {
		t.Fatalf("expected status=failed on Claude error, got %q", row.Status)
	}
	if !row.ErrorMessage.Valid || row.ErrorMessage.String == "" {
		t.Errorf("expected error_message to be populated, got %+v", row.ErrorMessage)
	}
}

func TestProcessScanJob_LowConfidence_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, `{"set_name":"x","set_id_hint":"","collector_number":"025","confidence":0.1}`, nil)

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusNoMatch {
		t.Fatalf("expected status=no_match on low confidence, got %q", row.Status)
	}
	if !row.Confidence.Valid || row.Confidence.Float64 != 0.1 {
		t.Errorf("expected confidence=0.1, got %+v", row.Confidence)
	}
	if row.ErrorMessage.String == "" {
		t.Errorf("expected reason in error_message")
	}
}

func TestProcessScanJob_NoCardMatch_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	// High confidence but a collector number that doesn't exist in the seed.
	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"999","confidence":0.9}`, nil)

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusNoMatch {
		t.Fatalf("expected status=no_match when no card found, got %q", row.Status)
	}
	if !row.Confidence.Valid || row.Confidence.Float64 != 0.9 {
		t.Errorf("expected confidence to pass through, got %+v", row.Confidence)
	}
}

func TestProcessScanJob_MissingImageFile_Failed(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Reference a file that does not exist on disk.
	missingPath := filepath.Join(t.TempDir(), "does-not-exist.png")
	jobID := enqueueScanJob(t, db, u.ID, missingPath, "deadbeef")

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, missingPath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusFailed {
		t.Fatalf("expected status=failed on missing image, got %q (err=%q)", row.Status, row.ErrorMessage.String)
	}
}

func TestProcessScanJob_ClaudeDisabled_Failed(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	// Intentionally do NOT enable Claude for this user.

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusFailed {
		t.Fatalf("expected status=failed when claude disabled, got %q", row.Status)
	}
}

// TestStartScanWorker_ProcessesQueuedJobs spins up the actual polling loop
// and verifies it picks up rows and writes terminal states. The poll interval
// is left at the production default; we cancel the worker as soon as both
// jobs reach a terminal status.
func TestStartScanWorker_ProcessesQueuedJobs(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Track the maximum number of concurrent stub invocations so we can
	// assert the semaphore actually let two jobs run in parallel rather than
	// serializing them. The stub also sleeps briefly so the overlap window
	// is wide enough to observe even on a busy CI box.
	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	stubScanPromptFn(t, func() (string, error) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		return `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.9}`, nil
	})

	imagePath1, hash1 := writeTempScanImage(t)
	imagePath2, hash2 := writeTempScanImage(t)
	job1 := enqueueScanJob(t, db, u.ID, imagePath1, hash1)
	job2 := enqueueScanJob(t, db, u.ID, imagePath2, hash2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		StartScanWorker(ctx, db)
		close(done)
	}()

	// Wait for both jobs to land in a terminal state. The worker runs an
	// immediate dispatch tick at startup, so we shouldn't have to wait long.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r1 := readScanJob(t, db, job1)
		r2 := readScanJob(t, db, job2)
		if r1.Status != scanJobStatusQueued && r1.Status != scanJobStatusProcessing &&
			r2.Status != scanJobStatusQueued && r2.Status != scanJobStatusProcessing {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	r1 := readScanJob(t, db, job1)
	r2 := readScanJob(t, db, job2)
	if r1.Status != scanJobStatusMatched {
		t.Errorf("job1 expected matched, got %q (err=%q)", r1.Status, r1.ErrorMessage.String)
	}
	if r2.Status != scanJobStatusMatched {
		t.Errorf("job2 expected matched, got %q (err=%q)", r2.Status, r2.ErrorMessage.String)
	}
	if maxObserved.Load() < int32(scanWorkerMaxConcurrent) {
		t.Errorf("expected max in-flight to reach %d (scanWorkerMaxConcurrent), got %d", scanWorkerMaxConcurrent, maxObserved.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("worker did not shut down after context cancel")
	}
}

// TestStartScanWorker_ShutsDownOnCancel verifies the worker returns promptly
// when its context is cancelled, even with no work in flight.
func TestStartScanWorker_ShutsDownOnCancel(t *testing.T) {
	db := setupTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		StartScanWorker(ctx, db)
		close(done)
	}()

	// Cancel right away — there is no queued work, so the worker should be
	// idle on the ticker channel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("worker did not shut down after context cancel")
	}
}

// TestStartScanWorker_CancelMidJob verifies the worker shuts down cleanly and
// leaves no row stuck in 'processing' when the context is cancelled while a
// Claude call is in-flight.
func TestStartScanWorker_CancelMidJob(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	// entered is closed the moment the stub is entered so the test knows the
	// job is in-flight before it cancels the worker context.
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stubScanPromptFn(t, func() (string, error) {
		close(entered)
		// Block until the worker context is cancelled, simulating a long-running
		// Claude call that is interrupted mid-flight.
		<-ctx.Done()
		return "", ctx.Err()
	})

	done := make(chan struct{})
	go func() {
		StartScanWorker(ctx, db)
		close(done)
	}()

	// Wait until the job is actually in-flight before pulling the plug.
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("stub never entered — job was not picked up by the worker")
	}

	// Cancel the worker context while the Claude call is still blocking.
	cancel()

	// The worker must drain in-flight goroutines and then exit.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("worker did not shut down after context cancel")
	}

	// The row must be in a terminal state — never stuck in 'processing'.
	row := readScanJob(t, db, jobID)
	if row.Status == scanJobStatusProcessing {
		t.Errorf("job left in 'processing' state after worker shutdown — expected a terminal state")
	}
	if row.Status == scanJobStatusQueued {
		t.Errorf("job left in 'queued' state — worker should have claimed and finalized it")
	}
}

// TestPollQueuedScanJobs_OrdersByCreatedAt asserts FIFO behaviour so the
// kid scanning a card first does not get jumped by a later submitter.
func TestPollQueuedScanJobs_OrdersByCreatedAt(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "scan@example.com")

	// Insert two rows with explicit timestamps so ordering is deterministic.
	older := time.Now().UTC().Add(-time.Hour)
	newer := time.Now().UTC()
	mustInsertScanJob(t, db, u.ID, "first.png", "h1", older)
	mustInsertScanJob(t, db, u.ID, "second.png", "h2", newer)

	jobs, err := pollQueuedScanJobs(context.Background(), db, 10)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if got, _ := encryption.DecryptField(jobs[0].ImagePathEnc); got != "first.png" {
		t.Errorf("expected first.png first, got %q", got)
	}
}

// TestClaimScanJob_OnceOnly proves the atomic claim works: a second call for
// the same id returns false, so two workers cannot process the same row.
func TestClaimScanJob_OnceOnly(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "scan@example.com")
	jobID := mustInsertScanJob(t, db, u.ID, "img.png", "h", time.Now().UTC())

	now := time.Now().UTC()
	ok, err := claimScanJob(context.Background(), db, jobID, now)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !ok {
		t.Fatalf("first claim should succeed")
	}

	ok, err = claimScanJob(context.Background(), db, jobID, now)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if ok {
		t.Fatalf("second claim should fail (row no longer queued)")
	}
}

// TestScanImagePath_CreatesPerUserDir confirms the per-user directory is
// created with the correct permissions and that the returned path follows
// the documented layout.
func TestScanImagePath_CreatesPerUserDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	path, err := ScanImagePath(42, 7)
	if err != nil {
		t.Fatalf("ScanImagePath: %v", err)
	}
	expected := filepath.Join(root, "pokemon-scans", "42", "7.jpg")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory, got %s", info.Mode())
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0077 != 0 {
			t.Errorf("expected 0700 perms (no group/other), got %o", info.Mode().Perm())
		}
	}
}

// --- helpers --------------------------------------------------------------

// stubError is the error type returned from the stub Claude function in
// failure tests. Using a dedicated type keeps the assertion explicit.
type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// encryptPath is a test helper that encrypts a plaintext on-disk path using
// the same encryption layer the production enqueue path will use.
func encryptPath(t *testing.T, path string) string {
	t.Helper()
	enc, err := encryption.EncryptField(path)
	if err != nil {
		t.Fatalf("encrypt path: %v", err)
	}
	return enc
}

// mustInsertScanJob inserts a queued scan job with a caller-supplied
// created_at so tests can deterministically assert ordering.
func mustInsertScanJob(t *testing.T, db *sql.DB, userID int64, imagePath, imageHash string, createdAt time.Time) int64 {
	t.Helper()
	enc, err := encryption.EncryptField(imagePath)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, scanJobStatusQueued, enc, imageHash, createdAt)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

// stubScanPromptFn replaces scanRunPromptFn with a closure that delegates to
// fn (a parameterless function returning the canned response + error). The
// original is restored on t.Cleanup. Used by the concurrency test which
// needs to track in-flight callers, not just the call count.
//
// Wrapped here (rather than reusing stubScanPrompt) because the existing
// helper takes a fixed response and we need the closure to observe state.
var stubScanPromptMu sync.Mutex

func stubScanPromptFn(t *testing.T, fn func() (string, error)) {
	t.Helper()
	stubScanPromptMu.Lock()
	defer stubScanPromptMu.Unlock()
	orig := scanRunPromptFn
	scanRunPromptFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (string, error) {
		return fn()
	}
	t.Cleanup(func() {
		stubScanPromptMu.Lock()
		defer stubScanPromptMu.Unlock()
		scanRunPromptFn = orig
	})
}

// capturedPush is a single (userID, payload) pair sent to scanPushSendFn while
// a test has the seam stubbed.
type capturedPush struct {
	userID  int64
	payload []byte
}

// pushCapture serialises writes to its underlying slice so concurrent tests
// (e.g. TestStartScanWorker_ProcessesQueuedJobs) can collect pushes without
// racing.
type pushCapture struct {
	mu    sync.Mutex
	calls []capturedPush
}

func (c *pushCapture) record(userID int64, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Copy the payload because callers reuse the buffer.
	cp := make([]byte, len(payload))
	copy(cp, payload)
	c.calls = append(c.calls, capturedPush{userID: userID, payload: cp})
}

func (c *pushCapture) snapshot() []capturedPush {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedPush, len(c.calls))
	copy(out, c.calls)
	return out
}

// stubScanPushCapture replaces scanPushSendFn with a no-op recorder that
// returns a single successful SendResult so callers in production code see a
// healthy response. The original function is restored on cleanup.
func stubScanPushCapture(t *testing.T) *pushCapture {
	t.Helper()
	rec := &pushCapture{}
	orig := scanPushSendFn
	scanPushSendFn = func(_ *sql.DB, userID int64, payload []byte) ([]push.SendResult, error) {
		rec.record(userID, payload)
		return []push.SendResult{{SubscriptionID: 1, StatusCode: http.StatusCreated}}, nil
	}
	t.Cleanup(func() { scanPushSendFn = orig })
	return rec
}

// decodeNotification unmarshals a captured payload, failing the test if the
// JSON is invalid.
func decodeNotification(t *testing.T, payload []byte) push.Notification {
	t.Helper()
	var n push.Notification
	if err := json.Unmarshal(payload, &n); err != nil {
		t.Fatalf("decode notification payload: %v (raw=%q)", err, payload)
	}
	return n
}

// TestProcessScanJob_PushEnabled_Matched verifies the matched terminal status
// fires a push with the expected title, body, tag, and click URL.
func TestProcessScanJob_PushEnabled_Matched(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)
	pushed := stubScanPushCapture(t)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.92}`, nil)
	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	calls := pushed.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(calls))
	}
	if calls[0].userID != u.ID {
		t.Errorf("push userID = %d, want %d", calls[0].userID, u.ID)
	}
	notif := decodeNotification(t, calls[0].payload)
	if notif.Title != "Scan result" {
		t.Errorf("title = %q, want %q", notif.Title, "Scan result")
	}
	wantBody := "Found: Pikachu (Scarlet & Violet Base)"
	if notif.Body != wantBody {
		t.Errorf("body = %q, want %q", notif.Body, wantBody)
	}
	wantTag := fmt.Sprintf("pokemon-scan-%d", jobID)
	if notif.Tag != wantTag {
		t.Errorf("tag = %q, want %q", notif.Tag, wantTag)
	}
	wantURL := fmt.Sprintf("/pokemon/scanned?focus=%d", jobID)
	if notif.URL != wantURL {
		t.Errorf("url = %q, want %q", notif.URL, wantURL)
	}
}

// TestProcessScanJob_PushEnabled_NoMatch verifies the no_match terminal status
// fires a push with the friendly retry copy.
func TestProcessScanJob_PushEnabled_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)
	pushed := stubScanPushCapture(t)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, "i give up", nil)
	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	calls := pushed.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(calls))
	}
	notif := decodeNotification(t, calls[0].payload)
	if notif.Body != "Couldn't read the card — try another angle" {
		t.Errorf("body = %q", notif.Body)
	}
	if notif.Tag != fmt.Sprintf("pokemon-scan-%d", jobID) {
		t.Errorf("tag = %q", notif.Tag)
	}
}

// TestProcessScanJob_PushEnabled_Failed verifies the failed terminal status
// (Claude CLI error here) fires a push with the failure copy.
func TestProcessScanJob_PushEnabled_Failed(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)
	pushed := stubScanPushCapture(t)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, "", &stubError{msg: "claude CLI exploded"})
	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	calls := pushed.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(calls))
	}
	notif := decodeNotification(t, calls[0].payload)
	if notif.Body != "Scan failed — try again" {
		t.Errorf("body = %q", notif.Body)
	}
}

// TestProcessScanJob_PushDisabled_NoSend verifies the opt-out preference
// suppresses the push entirely.
func TestProcessScanJob_PushDisabled_NoSend(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)
	if err := auth.SetPreference(db, u.ID, scanPushPreferenceKey, "false"); err != nil {
		t.Fatalf("set pref: %v", err)
	}
	pushed := stubScanPushCapture(t)

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.92}`, nil)
	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	if calls := pushed.snapshot(); len(calls) != 0 {
		t.Errorf("expected no push calls when disabled, got %d", len(calls))
	}
}

// TestProcessScanJob_PushStaleSubscriptionDeleted exercises the real
// push.SendToUser path against a test endpoint that returns 410 Gone. After
// the worker finishes, the stale subscription must be gone from the DB and
// the worker must not have errored out.
func TestProcessScanJob_PushStaleSubscriptionDeleted(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Test push endpoint that always responds 410 Gone — RFC 8030 marker for
	// a subscription the user agent has unregistered.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	t.Cleanup(ts.Close)

	curve := ecdh.P256()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	authSecret := base64.RawURLEncoding.EncodeToString(make([]byte, 16))

	if _, err := push.SaveSubscription(db, u.ID, ts.URL, p256dh, authSecret); err != nil {
		t.Fatalf("save subscription: %v", err)
	}

	// Point the worker's push HTTP client at the test server so TLS verifies.
	origClient := scanPushHTTPClient
	scanPushHTTPClient = ts.Client()
	t.Cleanup(func() { scanPushHTTPClient = origClient })

	imagePath, imageHash := writeTempScanImage(t)
	jobID := enqueueScanJob(t, db, u.ID, imagePath, imageHash)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.92}`, nil)
	processScanJob(context.Background(), db, scanJob{ID: jobID, UserID: u.ID, ImagePathEnc: encryptPath(t, imagePath)})

	// Worker must still finalize the row normally.
	row := readScanJob(t, db, jobID)
	if row.Status != scanJobStatusMatched {
		t.Errorf("expected matched status despite stale push subscription, got %q", row.Status)
	}

	// Subscription must be deleted by SendToUser's 410 handling.
	subs, err := push.GetSubscriptionsByUser(db, u.ID)
	if err != nil {
		t.Fatalf("get subscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected stale subscription to be deleted, got %d remaining", len(subs))
	}
}
