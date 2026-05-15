package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
)

// scanWorkerMaxConcurrent caps the number of Claude vision calls the worker
// has in flight at once. Two is a deliberate trade-off: the kids may scan a
// stack of cards in quick succession, but each Claude call uses meaningful
// quota and the CLI sub-process is heavy. Two keeps interactive scanning
// responsive without melting the rate limit.
const scanWorkerMaxConcurrent = 2

// scanWorkerPollInterval is how often the worker checks for new queued jobs.
// Five seconds keeps the user-perceived latency low (queue endpoint returns
// 202; the worker picks it up within a few seconds) without hammering the DB.
const scanWorkerPollInterval = 5 * time.Second

// scanWorkerBatchSize bounds the queued rows fetched per tick. The poll runs
// often enough that a small batch is sufficient; a larger batch only matters
// during catch-up after a restart.
const scanWorkerBatchSize = 10

// scanJobStatus enumerates the lifecycle values stored in
// pokemon_scan_jobs.status. The worker only writes queued → processing →
// {matched, no_match, failed}; later beads add the {added, discarded}
// resolution transitions.
const (
	scanJobStatusQueued     = "queued"
	scanJobStatusProcessing = "processing"
	scanJobStatusMatched    = "matched"
	scanJobStatusNoMatch    = "no_match"
	scanJobStatusFailed     = "failed"
)

// scanJob is the worker's internal view of a pokemon_scan_jobs row. The
// HTTP endpoint (next bead) writes these; the worker only reads + updates.
type scanJob struct {
	ID           int64
	UserID       int64
	ImagePathEnc string
}

// scanImageRoot returns the base directory under which scan images are
// stored. Honours UPLOAD_ROOT for deploys that mount a dedicated volume; the
// production server's default is /var/lib/hytte. The directory is not
// created here — ScanImagePath does that lazily per-user.
func scanImageRoot() string {
	if root := os.Getenv("UPLOAD_ROOT"); root != "" {
		return filepath.Join(root, "pokemon-scans")
	}
	return "/var/lib/hytte/pokemon-scans"
}

// ScanImagePath returns the absolute path where the scan image for the given
// (user, job) should live: <root>/<user_id>/<job_id>.jpg. The per-user
// directory is created lazily with 0700 permissions so one user's scans are
// not readable by another. Callers must already hold the bytes — this helper
// does not touch the file itself.
func ScanImagePath(userID, jobID int64) (string, error) {
	dir := filepath.Join(scanImageRoot(), strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create scan image dir %s: %w", dir, err)
	}
	return filepath.Join(dir, fmt.Sprintf("%d.jpg", jobID)), nil
}

// StartScanWorker runs the async scanner worker until ctx is cancelled. It
// polls pokemon_scan_jobs for queued rows, claims them atomically, and
// processes up to scanWorkerMaxConcurrent at a time. On shutdown it waits for
// in-flight jobs to reach a terminal state so no row is left in 'processing'.
//
// Call site: a single `go pokemon.StartScanWorker(notifCtx, db)` from
// cmd/server/main.go, alongside the suggestion-scheduler and other
// long-running background routines.
func StartScanWorker(ctx context.Context, db *sql.DB) {
	sem := make(chan struct{}, scanWorkerMaxConcurrent)
	var wg sync.WaitGroup

	ticker := time.NewTicker(scanWorkerPollInterval)
	defer ticker.Stop()

	// Run one immediate tick so a row enqueued just before startup is picked
	// up without waiting a full interval. This mirrors the currency sync
	// pattern (best-effort startup pass before entering the ticker loop).
	dispatchScanJobs(ctx, db, sem, &wg)

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			dispatchScanJobs(ctx, db, sem, &wg)
		}
	}
}

// dispatchScanJobs polls a single batch and spawns processor goroutines up to
// the semaphore limit. If the context is cancelled mid-dispatch we stop
// enqueueing new work — in-flight goroutines complete on their own.
func dispatchScanJobs(ctx context.Context, db *sql.DB, sem chan struct{}, wg *sync.WaitGroup) {
	if ctx.Err() != nil {
		return
	}
	jobs, err := pollQueuedScanJobs(ctx, db, scanWorkerBatchSize)
	if err != nil {
		log.Printf("pokemon: scan worker poll: %v", err)
		return
	}
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(j scanJob) {
			defer wg.Done()
			defer func() { <-sem }()
			processScanJob(ctx, db, j)
		}(job)
	}
}

// pollQueuedScanJobs returns up to limit queued rows ordered by created_at so
// the oldest queued scan runs first. Only the columns the worker needs are
// read; the row is fully claimed (status='processing') inside processScanJob.
func pollQueuedScanJobs(ctx context.Context, db *sql.DB, limit int) ([]scanJob, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, image_path_enc
		FROM pokemon_scan_jobs
		WHERE status = ?
		ORDER BY created_at, id
		LIMIT ?
	`, scanJobStatusQueued, limit)
	if err != nil {
		return nil, fmt.Errorf("query queued scan jobs: %w", err)
	}
	defer rows.Close()

	var jobs []scanJob
	for rows.Next() {
		var j scanJob
		if err := rows.Scan(&j.ID, &j.UserID, &j.ImagePathEnc); err != nil {
			return nil, fmt.Errorf("scan queued row: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queued rows: %w", err)
	}
	return jobs, nil
}

// claimScanJob atomically transitions a row from queued → processing. It
// returns true when this caller won the claim, false when another worker
// already picked the row (or it has moved on to a terminal state). Using
// status as part of the WHERE clause means concurrent workers — including
// future horizontally-scaled instances — never both process the same job.
// processed_at is intentionally NOT set here — it is written by the finalize
// helpers when the job reaches a terminal state so the column reflects
// completion time, not claim time.
func claimScanJob(ctx context.Context, db *sql.DB, jobID int64) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?
		WHERE id = ? AND status = ?
	`, scanJobStatusProcessing, jobID, scanJobStatusQueued)
	if err != nil {
		return false, fmt.Errorf("claim scan job %d: %w", jobID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim scan job %d rows affected: %w", jobID, err)
	}
	return n == 1, nil
}

// processScanJob runs a single queued job end-to-end. It is structured so
// that every exit path lands in a terminal status — a panic or unexpected
// error must never leave the row stuck in 'processing'. The function never
// returns an error: failures are recorded on the row itself via
// finalizeScanJob.
func processScanJob(ctx context.Context, db *sql.DB, job scanJob) {
	// finalizeCtx is used for the final UPDATE so a parent-context cancellation
	// mid-job still records the terminal state. We give it a short deadline to
	// avoid blocking shutdown indefinitely if the DB itself is unhealthy.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pokemon: scan worker panic on job %d: %v", job.ID, r)
			finalizeScanJobFailed(db, job.ID, fmt.Sprintf("worker panic: %v", r))
			notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		}
	}()

	claimed, err := claimScanJob(ctx, db, job.ID)
	if err != nil {
		log.Printf("pokemon: scan worker claim job %d: %v", job.ID, err)
		return
	}
	if !claimed {
		// Another instance picked it up, or the row was already terminal.
		return
	}

	imagePath, err := encryption.DecryptField(job.ImagePathEnc)
	if err != nil {
		finalizeScanJobFailed(db, job.ID, fmt.Sprintf("decrypt image path: %v", err))
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}
	if imagePath == "" {
		finalizeScanJobFailed(db, job.ID, "empty image path")
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}
	if _, err := os.Stat(imagePath); err != nil {
		finalizeScanJobFailed(db, job.ID, fmt.Sprintf("stat image: %v", err))
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}

	cfg, err := training.LoadClaudeConfig(db, job.UserID)
	if err != nil {
		finalizeScanJobFailed(db, job.ID, fmt.Sprintf("load claude config: %v", err))
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}
	if !cfg.Enabled {
		finalizeScanJobFailed(db, job.ID, "claude is not enabled for this user")
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}

	raw, err := scanRunPromptFn(ctx, cfg, scanPrompt, imagePath)
	if err != nil {
		finalizeScanJobFailed(db, job.ID, fmt.Sprintf("claude scan: %v", err))
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}

	respEnc, encErr := encryption.EncryptField(raw)
	if encErr != nil {
		// A failed encrypt of the raw response is not fatal — we'd still
		// like to record the match outcome. Log and continue with a NULL
		// claude_response_enc.
		log.Printf("pokemon: scan worker encrypt response for job %d: %v", job.ID, encErr)
		respEnc = ""
	}

	result, parseErr := parseClaudeScanResult(raw)
	if parseErr != nil {
		finalizeScanJobNoMatch(db, job.ID, 0, fmt.Sprintf("could not parse vision response: %v", parseErr), respEnc)
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusNoMatch, "", "")
		return
	}
	if result.Confidence < scanConfidenceThreshold {
		finalizeScanJobNoMatch(db, job.ID, result.Confidence, "low confidence", respEnc)
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusNoMatch, "", "")
		return
	}

	candidates, reason, err := findScanCandidates(ctx, db, job.UserID, result)
	if err != nil {
		finalizeScanJobFailed(db, job.ID, fmt.Sprintf("find candidates: %v", err))
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusFailed, "", "")
		return
	}
	if len(candidates) == 0 {
		if reason == "" {
			reason = "no candidate found"
		}
		finalizeScanJobNoMatch(db, job.ID, result.Confidence, reason, respEnc)
		notifyScanResult(db, job.UserID, job.ID, scanJobStatusNoMatch, "", "")
		return
	}

	// Multiple candidates are still a 'matched' outcome — the resolution UI
	// (next bead) lets the user pick. We record the first candidate's card id
	// so the row has a non-null reference; variant id stays NULL because the
	// user picks the specific kind (normal vs reverse vs holo) during resolve.
	matched := candidates[0]
	finalizeScanJobMatched(db, job.ID, matched.Card.ID, result.Confidence, respEnc)
	setName := ""
	if matched.Set != nil {
		setName = matched.Set.Name
	}
	notifyScanResult(db, job.UserID, job.ID, scanJobStatusMatched, matched.Card.Name, setName)
}

// finalizeScanJobMatched sets the terminal 'matched' state. Uses a short
// background deadline so a cancelled parent context does not lose the result.
// processed_at is stamped here (not at claim time) so it reflects when the
// worker actually finished the job.
func finalizeScanJobMatched(db *sql.DB, jobID int64, cardID string, confidence float64, respEnc string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?, matched_card_id = ?, confidence = ?, claude_response_enc = ?, error_message = NULL, processed_at = ?
		WHERE id = ?
	`, scanJobStatusMatched, cardID, confidence, nullIfEmpty(respEnc), time.Now().UTC(), jobID); err != nil {
		log.Printf("pokemon: finalize matched scan job %d: %v", jobID, err)
	}
}

// finalizeScanJobNoMatch sets the terminal 'no_match' state with the supplied
// reason in error_message. The Claude response (if encryptable) is preserved
// for debugging. processed_at is stamped here on completion.
func finalizeScanJobNoMatch(db *sql.DB, jobID int64, confidence float64, reason, respEnc string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?, confidence = ?, error_message = ?, claude_response_enc = ?, processed_at = ?
		WHERE id = ?
	`, scanJobStatusNoMatch, confidence, reason, nullIfEmpty(respEnc), time.Now().UTC(), jobID); err != nil {
		log.Printf("pokemon: finalize no_match scan job %d: %v", jobID, err)
	}
}

// finalizeScanJobFailed sets the terminal 'failed' state with the error
// message. Used for infrastructure failures (disk, Claude CLI, DB) — distinct
// from no_match which means "Claude returned no usable answer". processed_at
// is stamped here on completion.
func finalizeScanJobFailed(db *sql.DB, jobID int64, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `
		UPDATE pokemon_scan_jobs
		SET status = ?, error_message = ?, processed_at = ?
		WHERE id = ?
	`, scanJobStatusFailed, message, time.Now().UTC(), jobID); err != nil {
		log.Printf("pokemon: finalize failed scan job %d: %v", jobID, err)
	}
}

// nullIfEmpty returns nil for an empty string so the column stores SQL NULL
// rather than the empty TEXT — keeps the schema clean and lets "has response"
// queries use IS NOT NULL.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// scanPushPreferenceKey is the user preference that opts a kid out of per-scan
// push notifications. Default is enabled — only an explicit "false" disables.
const scanPushPreferenceKey = "pokemon_scan_push_enabled"

// scanPushHTTPClient is the HTTP client used for sending scan-result push
// notifications. A short timeout keeps the worker goroutine from stalling
// indefinitely on a slow push endpoint.
var scanPushHTTPClient = &http.Client{Timeout: 10 * time.Second}

// scanPushSendFn is the package-level seam tests override to capture push
// payloads without exercising the real HTTP transport. Production wires it to
// push.SendToUser.
var scanPushSendFn = func(db *sql.DB, userID int64, payload []byte) ([]push.SendResult, error) {
	return push.SendToUser(db, scanPushHTTPClient, userID, payload)
}

// scanPushEnabled returns whether the user accepts per-scan push notifications.
// Missing preference defaults to true; only the literal string "false" opts a
// user out. DB errors fail-open so a transient lookup failure does not
// permanently silence notifications.
func scanPushEnabled(db *sql.DB, userID int64) bool {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("pokemon: scan push load preferences for user %d: %v", userID, err)
		return true
	}
	v, ok := prefs[scanPushPreferenceKey]
	if !ok {
		return true
	}
	return v != "false"
}

// notifyScanResult dispatches a push notification for a scan job that just
// transitioned to a terminal state. All errors are logged and swallowed —
// push delivery must never block the worker. cardName and setName are used
// only for the matched status; ignored otherwise.
func notifyScanResult(db *sql.DB, userID, jobID int64, status, cardName, setName string) {
	if !scanPushEnabled(db, userID) {
		return
	}
	notif, ok := buildScanResultNotification(status, jobID, cardName, setName)
	if !ok {
		return
	}
	payload, err := json.Marshal(notif)
	if err != nil {
		log.Printf("pokemon: marshal scan push for job %d: %v", jobID, err)
		return
	}
	if _, err := scanPushSendFn(db, userID, payload); err != nil {
		log.Printf("pokemon: send scan push for job %d: %v", jobID, err)
	}
}

// buildScanResultNotification returns the Notification body for a terminal
// status. The tag collapses repeats for the same job; the URL deep-links the
// scanned page focused on the row so a notification click takes the kid
// straight to the result. Unknown statuses return ok=false so callers can
// skip the send.
func buildScanResultNotification(status string, jobID int64, cardName, setName string) (push.Notification, bool) {
	var body string
	switch status {
	case scanJobStatusMatched:
		if setName != "" {
			body = fmt.Sprintf("Found: %s (%s)", cardName, setName)
		} else if cardName != "" {
			body = fmt.Sprintf("Found: %s", cardName)
		} else {
			body = "Card matched"
		}
	case scanJobStatusNoMatch:
		body = "Couldn't read the card — try another angle"
	case scanJobStatusFailed:
		body = "Scan failed — try again"
	default:
		return push.Notification{}, false
	}
	return push.Notification{
		Title: "Scan result",
		Body:  body,
		Tag:   fmt.Sprintf("pokemon-scan-%d", jobID),
		URL:   fmt.Sprintf("/pokemon/scanned?focus=%d", jobID),
	}, true
}
