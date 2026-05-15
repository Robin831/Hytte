package pokemon

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// insertScanJobAt seeds a row with a caller-supplied created_at and on-disk
// image path encrypted via the same layer as production. Returns the row id so
// tests can assert the after-state directly.
func insertScanJobAt(t *testing.T, db *sql.DB, userID int64, status, imagePath string, createdAt time.Time) int64 {
	t.Helper()
	enc := ""
	if imagePath != "" {
		var err error
		enc, err = encryption.EncryptField(imagePath)
		if err != nil {
			t.Fatalf("encrypt path: %v", err)
		}
	}
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, status, enc, "deadbeef", createdAt.UTC())
	if err != nil {
		t.Fatalf("insert scan job: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

// writeScanImageFile drops a placeholder file at <root>/pokemon-scans/<uid>/<name>
// so the cleanup pass has something concrete to remove. Returns the absolute
// path; the file's contents don't matter.
func writeScanImageFile(t *testing.T, root string, userID int64, name string) string {
	t.Helper()
	dir := filepath.Join(root, "pokemon-scans", strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("scan-bytes"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func readCleanupRow(t *testing.T, db *sql.DB, id int64) (status, errMsg string, resolvedAt sql.NullTime, pathEnc string) {
	t.Helper()
	var em sql.NullString
	if err := db.QueryRow(`
		SELECT status, COALESCE(error_message, ''), resolved_at, image_path_enc
		FROM pokemon_scan_jobs WHERE id = ?
	`, id).Scan(&status, &em, &resolvedAt, &pathEnc); err != nil {
		t.Fatalf("read cleanup row: %v", err)
	}
	return status, em.String, resolvedAt, pathEnc
}

func TestRunScanCleanup_DiscardsOldMatched(t *testing.T) {
	root := t.TempDir()
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "old@example.com")

	now := time.Now().UTC()
	imagePath := writeScanImageFile(t, root, u.ID, "old.jpg")
	id := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, imagePath, now.Add(-25*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.Discarded != 1 {
		t.Errorf("expected discarded=1, got %d", res.Discarded)
	}

	status, errMsg, resolvedAt, pathEnc := readCleanupRow(t, db, id)
	if status != scanJobStatusDiscarded {
		t.Errorf("expected status=discarded, got %q", status)
	}
	if !resolvedAt.Valid {
		t.Errorf("expected resolved_at to be set")
	}
	if errMsg != scanAutoDiscardErrorMessage {
		t.Errorf("expected error_message=%q, got %q", scanAutoDiscardErrorMessage, errMsg)
	}
	if pathEnc != "" {
		t.Errorf("expected image_path_enc cleared, got %q", pathEnc)
	}
	if _, err := os.Stat(imagePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected image file removed, stat err=%v", err)
	}
}

func TestRunScanCleanup_LeavesYoungMatchedAlone(t *testing.T) {
	root := t.TempDir()
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "young@example.com")

	now := time.Now().UTC()
	imagePath := writeScanImageFile(t, root, u.ID, "young.jpg")
	id := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, imagePath, now.Add(-5*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.Discarded != 0 {
		t.Errorf("expected discarded=0, got %d", res.Discarded)
	}

	status, _, resolvedAt, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusMatched {
		t.Errorf("expected status=matched (untouched), got %q", status)
	}
	if resolvedAt.Valid {
		t.Errorf("expected resolved_at to remain NULL")
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Errorf("expected image file preserved, got err=%v", err)
	}
}

func TestRunScanCleanup_FailsStaleProcessing(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "stuck@example.com")

	now := time.Now().UTC()
	id := insertScanJobAt(t, db, u.ID, scanJobStatusProcessing, "", now.Add(-2*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.StaleFailed != 1 {
		t.Errorf("expected stale_failed=1, got %d", res.StaleFailed)
	}

	status, errMsg, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusFailed {
		t.Errorf("expected status=failed, got %q", status)
	}
	if errMsg != scanStaleErrorMessage {
		t.Errorf("expected error_message=%q, got %q", scanStaleErrorMessage, errMsg)
	}
}

func TestRunScanCleanup_FailsStaleQueued(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "stuck@example.com")

	now := time.Now().UTC()
	id := insertScanJobAt(t, db, u.ID, scanJobStatusQueued, "", now.Add(-90*time.Minute))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.StaleFailed != 1 {
		t.Errorf("expected stale_failed=1, got %d", res.StaleFailed)
	}

	status, errMsg, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusFailed {
		t.Errorf("expected status=failed, got %q", status)
	}
	if errMsg != scanStaleErrorMessage {
		t.Errorf("expected error_message=%q, got %q", scanStaleErrorMessage, errMsg)
	}
}

func TestRunScanCleanup_LeavesYoungProcessingAlone(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "running@example.com")

	now := time.Now().UTC()
	id := insertScanJobAt(t, db, u.ID, scanJobStatusProcessing, "", now.Add(-10*time.Minute))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.StaleFailed != 0 {
		t.Errorf("expected stale_failed=0, got %d", res.StaleFailed)
	}
	status, _, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusProcessing {
		t.Errorf("expected status=processing (untouched), got %q", status)
	}
}

func TestRunScanCleanup_UserPrefZeroDisables(t *testing.T) {
	root := t.TempDir()
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "infinite@example.com")
	if err := auth.SetPreference(db, u.ID, scanAutoDiscardPrefKey, "0"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	now := time.Now().UTC()
	imagePath := writeScanImageFile(t, root, u.ID, "infinite.jpg")
	id := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, imagePath, now.Add(-25*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.Discarded != 0 {
		t.Errorf("expected discarded=0 with pref=0, got %d", res.Discarded)
	}

	status, _, resolvedAt, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusMatched {
		t.Errorf("expected status=matched (untouched), got %q", status)
	}
	if resolvedAt.Valid {
		t.Errorf("expected resolved_at to remain NULL")
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Errorf("expected image preserved with pref=0, stat err=%v", err)
	}
}

func TestRunScanCleanup_UserPref48Hours(t *testing.T) {
	root := t.TempDir()
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "two-day@example.com")
	if err := auth.SetPreference(db, u.ID, scanAutoDiscardPrefKey, "48"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	now := time.Now().UTC()
	youngImg := writeScanImageFile(t, root, u.ID, "young.jpg")
	oldImg := writeScanImageFile(t, root, u.ID, "old.jpg")
	youngID := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, youngImg, now.Add(-25*time.Hour))
	oldID := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, oldImg, now.Add(-49*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if res.Discarded != 1 {
		t.Errorf("expected discarded=1, got %d", res.Discarded)
	}

	youngStatus, _, _, _ := readCleanupRow(t, db, youngID)
	if youngStatus != scanJobStatusMatched {
		t.Errorf("expected young row untouched, got status=%q", youngStatus)
	}
	if _, err := os.Stat(youngImg); err != nil {
		t.Errorf("expected young image preserved, err=%v", err)
	}

	oldStatus, _, oldResolved, _ := readCleanupRow(t, db, oldID)
	if oldStatus != scanJobStatusDiscarded {
		t.Errorf("expected old row discarded, got status=%q", oldStatus)
	}
	if !oldResolved.Valid {
		t.Errorf("expected resolved_at set on old row")
	}
	if _, err := os.Stat(oldImg); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected old image removed, stat err=%v", err)
	}
}

func TestRunScanCleanup_MissingImageFileNotFatal(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "no-image@example.com")

	now := time.Now().UTC()
	// Path that points nowhere — cleanup should still discard the DB row.
	missingPath := filepath.Join(t.TempDir(), "never-existed.jpg")
	id := insertScanJobAt(t, db, u.ID, scanJobStatusMatched, missingPath, now.Add(-25*time.Hour))

	res, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup unexpectedly errored: %v", err)
	}
	if res.Discarded != 1 {
		t.Errorf("expected discarded=1, got %d", res.Discarded)
	}

	status, _, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusDiscarded {
		t.Errorf("expected status=discarded despite missing image, got %q", status)
	}
}

func TestRunScanCleanup_PreservesExistingErrorMessage(t *testing.T) {
	root := t.TempDir()
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "had-error@example.com")

	now := time.Now().UTC()
	imagePath := writeScanImageFile(t, root, u.ID, "noisy.jpg")
	enc, err := encryption.EncryptField(imagePath)
	if err != nil {
		t.Fatalf("encrypt path: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, scanJobStatusNoMatch, enc, "deadbeef", "low confidence", now.Add(-30*time.Hour))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()

	if _, err := RunScanCleanup(context.Background(), db, now); err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}

	status, errMsg, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusDiscarded {
		t.Errorf("expected discarded, got %q", status)
	}
	if errMsg != "low confidence" {
		t.Errorf("expected original error_message preserved, got %q", errMsg)
	}
}

func TestRunScanCleanup_LeavesResolvedRowsAlone(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "resolved@example.com")

	now := time.Now().UTC()
	// Already resolved (matched + resolved_at): should never be touched again.
	resolvedAt := now.Add(-26 * time.Hour)
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, scanJobStatusAdded, "", "deadbeef", now.Add(-30*time.Hour), resolvedAt)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()

	cleanupRes, err := RunScanCleanup(context.Background(), db, now)
	if err != nil {
		t.Fatalf("RunScanCleanup: %v", err)
	}
	if cleanupRes.Discarded != 0 {
		t.Errorf("expected discarded=0 for resolved rows, got %d", cleanupRes.Discarded)
	}
	status, _, _, _ := readCleanupRow(t, db, id)
	if status != scanJobStatusAdded {
		t.Errorf("expected status=added (untouched), got %q", status)
	}
}

func TestGetUserAutoDiscardHours_DefaultWhenMissing(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "default@example.com")

	got, err := getUserAutoDiscardHours(context.Background(), db, u.ID)
	if err != nil {
		t.Fatalf("getUserAutoDiscardHours: %v", err)
	}
	if got != ScanAutoDiscardDefaultHours {
		t.Errorf("expected default=%d, got %d", ScanAutoDiscardDefaultHours, got)
	}
}

func TestGetUserAutoDiscardHours_ClampsAndDefaults(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "weird@example.com")

	cases := []struct {
		raw  string
		want int
	}{
		{"0", 0},
		{"48", 48},
		{"168", 168},
		{"500", ScanAutoDiscardMaxHours},
		{"-5", 0},
		{"not-a-number", ScanAutoDiscardDefaultHours},
		{"", ScanAutoDiscardDefaultHours},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if err := auth.SetPreference(db, u.ID, scanAutoDiscardPrefKey, tc.raw); err != nil {
				t.Fatalf("set pref: %v", err)
			}
			got, err := getUserAutoDiscardHours(context.Background(), db, u.ID)
			if err != nil {
				t.Fatalf("getUserAutoDiscardHours: %v", err)
			}
			if got != tc.want {
				t.Errorf("raw=%q: expected %d, got %d", tc.raw, tc.want, got)
			}
		})
	}
}
