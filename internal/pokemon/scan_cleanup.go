package pokemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// scanAutoDiscardPrefKey is the per-user preference that overrides the default
// 24-hour auto-discard window. Stored as a base-10 integer in hours. 0 disables
// auto-discard for that user; ScanAutoDiscardMaxHours caps the upper end so a
// runaway pref can't keep stale images forever.
const scanAutoDiscardPrefKey = "pokemon_scan_auto_discard_hours"

// ScanAutoDiscardDefaultHours is the default retention window for unresolved
// matched/no_match/failed scans before the cleanup pass discards them.
const ScanAutoDiscardDefaultHours = 24

// ScanAutoDiscardMaxHours caps the per-user override at one week.
const ScanAutoDiscardMaxHours = 168

// scanStaleQueuedHours is the threshold after which a job stuck in 'queued' or
// 'processing' is forcibly transitioned to 'failed'. One hour is well above the
// happy-path duration (Claude vision finishes in seconds) but well below the
// 24-hour user-action window — a worker crash or unclean shutdown should not
// leave a row blocking the worker for a full day.
const scanStaleQueuedHours = 1 * time.Hour

// scanStaleErrorMessage is the error_message written to rows the cleanup pass
// transitions from queued/processing → failed. Kept as a constant so tests and
// support tooling can match on the exact string.
const scanStaleErrorMessage = "stale — worker did not complete in time"

// scanAutoDiscardErrorMessage is the error_message stamped on rows the
// cleanup pass auto-discards when the row does not already carry one (matched
// rows reach the terminal state with NULL error_message; no_match/failed rows
// already have a meaningful one we should preserve).
const scanAutoDiscardErrorMessage = "auto-discarded after 24h"

// scanCleanupInterval is how often the cleanup goroutine wakes. Mirrors the
// existing session cleanup cadence in cmd/server/main.go — once an hour is
// frequent enough that nothing lingers far past its window and infrequent
// enough that the pass costs effectively nothing.
const scanCleanupInterval = 1 * time.Hour

// getUserAutoDiscardHours returns the clamped retention window for the user.
// Missing / blank / non-integer values fall back to ScanAutoDiscardDefaultHours
// so a malformed pref cannot silently disable cleanup. The result is clamped
// to [0, ScanAutoDiscardMaxHours]; 0 means "never auto-discard for this user".
func getUserAutoDiscardHours(ctx context.Context, db *sql.DB, userID int64) (int, error) {
	var raw string
	err := db.QueryRowContext(ctx, `
		SELECT value FROM user_preferences WHERE user_id = ? AND key = ?
	`, userID, scanAutoDiscardPrefKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanAutoDiscardDefaultHours, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load scan auto-discard pref: %w", err)
	}
	n, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return ScanAutoDiscardDefaultHours, nil
	}
	if n < 0 {
		return 0, nil
	}
	if n > ScanAutoDiscardMaxHours {
		return ScanAutoDiscardMaxHours, nil
	}
	return n, nil
}

// ScanCleanupResult is the per-run summary. Returned from RunScanCleanup so
// tests can assert without scraping logs; production logs the same numbers.
type ScanCleanupResult struct {
	Discarded   int
	StaleFailed int
}

// RunScanCleanup performs both housekeeping passes once: stale queued /
// processing jobs are forced to 'failed', and unresolved terminal rows past
// the per-user retention window are auto-discarded (image file removed,
// status='discarded', resolved_at stamped). The now parameter makes the age
// thresholds deterministic in tests.
func RunScanCleanup(ctx context.Context, db *sql.DB, now time.Time) (ScanCleanupResult, error) {
	var result ScanCleanupResult

	staleCutoff := now.Add(-scanStaleQueuedHours)
	// Queued jobs: measure staleness from created_at (they have never been
	// claimed, so that is the only relevant timestamp).
	staleQueuedRes, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?, error_message = ?, processed_at = ?
		WHERE status = ? AND created_at < ?
	`, scanJobStatusFailed, scanStaleErrorMessage, now.UTC(),
		scanJobStatusQueued, staleCutoff.UTC())
	if err != nil {
		return result, fmt.Errorf("mark stale queued scan jobs failed: %w", err)
	}
	if n, err := staleQueuedRes.RowsAffected(); err == nil {
		result.StaleFailed += int(n)
	}
	// Processing jobs: measure staleness from processing_started_at (stamped
	// when the worker claims the row). Fall back to created_at for rows that
	// pre-date the column so existing stuck jobs are still cleaned up.
	staleProcessingRes, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?, error_message = ?, processed_at = ?
		WHERE status = ? AND COALESCE(processing_started_at, created_at) < ?
	`, scanJobStatusFailed, scanStaleErrorMessage, now.UTC(),
		scanJobStatusProcessing, staleCutoff.UTC())
	if err != nil {
		return result, fmt.Errorf("mark stale processing scan jobs failed: %w", err)
	}
	if n, err := staleProcessingRes.RowsAffected(); err == nil {
		result.StaleFailed += int(n)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, image_path_enc, created_at, error_message
		FROM pokemon_scan_jobs
		WHERE status IN (?, ?, ?) AND resolved_at IS NULL
	`, scanJobStatusMatched, scanJobStatusNoMatch, scanJobStatusFailed)
	if err != nil {
		return result, fmt.Errorf("select unresolved scan jobs: %w", err)
	}

	type candidate struct {
		id           int64
		userID       int64
		pathEnc      string
		createdAt    time.Time
		errorMessage sql.NullString
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.userID, &c.pathEnc, &c.createdAt, &c.errorMessage); err != nil {
			rows.Close()
			return result, fmt.Errorf("scan unresolved row: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, fmt.Errorf("iterate unresolved rows: %w", err)
	}
	rows.Close()

	hoursByUser := make(map[int64]int)
	for _, c := range candidates {
		hours, ok := hoursByUser[c.userID]
		if !ok {
			h, err := getUserAutoDiscardHours(ctx, db, c.userID)
			if err != nil {
				log.Printf("pokemon: scan cleanup load pref user=%d: %v", c.userID, err)
				hoursByUser[c.userID] = ScanAutoDiscardDefaultHours
				hours = ScanAutoDiscardDefaultHours
			} else {
				hoursByUser[c.userID] = h
				hours = h
			}
		}
		if hours <= 0 {
			continue
		}
		ageCutoff := now.Add(-time.Duration(hours) * time.Hour)
		if !c.createdAt.Before(ageCutoff) {
			continue
		}

		newErr := scanAutoDiscardErrorMessage
		if c.errorMessage.Valid && c.errorMessage.String != "" {
			newErr = c.errorMessage.String
		}
		discardRes, err := db.ExecContext(ctx, `
			UPDATE pokemon_scan_jobs
			SET status = ?, resolved_at = ?, error_message = ?, image_path_enc = ''
			WHERE id = ? AND resolved_at IS NULL AND status IN (?, ?, ?)
		`, scanJobStatusDiscarded, now.UTC(), newErr, c.id,
			scanJobStatusMatched, scanJobStatusNoMatch, scanJobStatusFailed)
		if err != nil {
			log.Printf("pokemon: scan cleanup discard job=%d: %v", c.id, err)
			continue
		}
		// Guard against the race where the user resolved or retried the scan
		// between our SELECT and this UPDATE. Only delete the image and count the
		// discard when the UPDATE actually changed the row.
		n, err := discardRes.RowsAffected()
		if err != nil {
			log.Printf("pokemon: scan cleanup discard job=%d rows affected: %v", c.id, err)
			continue
		}
		if n == 0 {
			continue
		}
		deleteScanImageBestEffort(c.pathEnc)
		result.Discarded++
	}

	return result, nil
}

// deleteScanImageBestEffort removes the on-disk scan image, ignoring missing
// files. Errors other than ErrNotExist are logged but never abort the cleanup
// pass — the DB row is already discarded; a leftover file can be reaped later.
func deleteScanImageBestEffort(pathEnc string) {
	if pathEnc == "" {
		return
	}
	path, err := encryption.DecryptField(pathEnc)
	if err != nil || path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("pokemon: scan cleanup remove image %s: %v", path, err)
	}
}

// StartScanCleanupLoop runs RunScanCleanup once at startup and then every
// scanCleanupInterval until ctx is cancelled. Call site: a single
// `go pokemon.StartScanCleanupLoop(notifCtx, db)` from cmd/server/main.go
// alongside the existing scan worker and the nightly suggestions cron.
func StartScanCleanupLoop(ctx context.Context, db *sql.DB) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pokemon: scan cleanup loop panic: %v", r)
		}
	}()

	runOnce := func() {
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		res, err := RunScanCleanup(runCtx, db, time.Now().UTC())
		if err != nil {
			log.Printf("pokemon: scan cleanup: %v", err)
			return
		}
		if res.Discarded > 0 || res.StaleFailed > 0 {
			log.Printf("pokemon: scan cleanup auto-discarded %d scans, failed %d stale jobs",
				res.Discarded, res.StaleFailed)
		}
	}

	// Best-effort startup pass so a row past its window after a restart is
	// reaped without waiting a full interval. Matches the pattern used by the
	// session cleanup and currency sync.
	runOnce()

	ticker := time.NewTicker(scanCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
