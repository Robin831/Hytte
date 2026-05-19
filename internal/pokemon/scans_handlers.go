package pokemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// defaultScanListStatuses is the status filter applied when the caller omits
// ?status= on the list endpoint. It deliberately excludes the resolved
// terminal states (added, discarded) so the default view shows "still needs
// my attention" rather than a long historical tail.
var defaultScanListStatuses = []string{
	scanJobStatusQueued,
	scanJobStatusProcessing,
	scanJobStatusMatched,
	scanJobStatusNoMatch,
	scanJobStatusFailed,
}

// allowedScanStatuses whitelists the status values a caller may pass in
// ?status=. Anything outside this set is silently dropped so a typo doesn't
// surface as an empty page or a SQL error.
var allowedScanStatuses = map[string]bool{
	scanJobStatusQueued:     true,
	scanJobStatusProcessing: true,
	scanJobStatusMatched:    true,
	scanJobStatusNoMatch:    true,
	scanJobStatusFailed:     true,
	scanJobStatusAdded:      true,
	scanJobStatusDiscarded:  true,
}

// scanJobStatusAdded and scanJobStatusDiscarded are the resolution terminal
// states written by the resolve endpoint. Kept here next to the worker's
// existing status constants so the full lifecycle lives in one place.
const (
	scanJobStatusAdded     = "added"
	scanJobStatusDiscarded = "discarded"
)

// defaultScanListLimit is the default page size when no ?limit= is supplied.
const defaultScanListLimit = 50

// maxScanListLimit caps the explicit ?limit=. The endpoint is a per-user
// review list, not a bulk export, so an upper bound keeps responses snappy.
const maxScanListLimit = 200

// ScanJobDTO is the JSON shape returned by the list and resolve endpoints. It
// captures the lifecycle state plus enough card metadata that the frontend
// can render a matched job without a second request.
//
// ParsedSetName / ParsedCollectorNo carry whatever set name + collector number
// Claude could read from a no_match scan, so the "Enter manually" flow can
// pre-fill the AddCardPanel search even when the lookup itself failed.
type ScanJobDTO struct {
	ID                int64    `json:"id"`
	Status            string   `json:"status"`
	CreatedAt         string   `json:"created_at"`
	ProcessedAt       *string  `json:"processed_at,omitempty"`
	ResolvedAt        *string  `json:"resolved_at,omitempty"`
	Confidence        *float64 `json:"confidence,omitempty"`
	MatchedCard       *CardDTO `json:"matched_card,omitempty"`
	Set               *SetDTO  `json:"set,omitempty"`
	ErrorMessage      string   `json:"error_message,omitempty"`
	HasImage          bool     `json:"has_image"`
	ParsedSetName     string   `json:"parsed_set_name,omitempty"`
	ParsedCollectorNo string   `json:"parsed_collector_no,omitempty"`
}

// ScanPageDTO groups a pokemon_scan_pages parent row with all of its child
// scan jobs so the /pokemon/scanned UI can render the binder layout as a
// single grid block. Children are returned regardless of their individual
// status so the grid always shows the full N cells the user captured; the
// frontend filters or styles each cell by its own state.
type ScanPageDTO struct {
	ID            int64        `json:"id"`
	ExpectedCount int          `json:"expected_count"`
	MatchedCount  int          `json:"matched_count"`
	CreatedAt     string       `json:"created_at"`
	Children      []ScanJobDTO `json:"children"`
}

// extractScanHints decrypts the stored Claude response (if any) and returns
// the partial set name + collector number it managed to read. Used by the
// no_match path to pre-fill manual entry — failures collapse to empty
// strings so a corrupt blob never blocks the list response.
func extractScanHints(claudeResponseEnc sql.NullString) (string, string) {
	if !claudeResponseEnc.Valid || claudeResponseEnc.String == "" {
		return "", ""
	}
	raw, err := encryption.DecryptField(claudeResponseEnc.String)
	if err != nil || raw == "" {
		return "", ""
	}
	res, err := parseClaudeScanResult(raw)
	if err != nil || res == nil {
		return "", ""
	}
	return strings.TrimSpace(res.SetName), strings.TrimSpace(res.CollectorNumber)
}

// scanUUID returns a 32-character hex string suitable for a per-scan image
// filename. crypto/rand is used so the filename is unguessable, which is
// important because the image directory is shared across all of a user's
// scans (a leaked id could otherwise be used to brute-force other scans).
func scanUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// scanImagePathForUUID resolves the on-disk path for a given (user, uuid)
// pair, creating the per-user directory lazily with 0700 perms so one user's
// scans are unreadable by another OS account. The caller writes the bytes.
func scanImagePathForUUID(userID int64, uuid string) (string, error) {
	dir := filepath.Join(scanImageRoot(), strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create scan image dir %s: %w", dir, err)
	}
	return filepath.Join(dir, uuid+".jpg"), nil
}

// scanPageMaxImagesPerPage caps how many cards a single page upload may carry.
// Real-world binder pages top out at 18 (3×6); 30 leaves headroom for unusual
// layouts without making the upper bound a footgun for the cost cap (charging
// 30 against a 600/day cap leaves the kid plenty of further scans).
const scanPageMaxImagesPerPage = 30

// scanPageParseFormBytes caps the entire multipart body. Each card image is
// independently re-checked against scanMaxImageBytes so this just keeps the
// parser from buffering an absurd payload.
const scanPageParseFormBytes = (scanMaxImageBytes * scanPageMaxImagesPerPage) + (5 << 20)

// pageCell is one (row, col) entry in the JSON `cells` field of a page upload.
// Rows and columns are 0-indexed grid coordinates from the browser-side crop
// pipeline (Hytte-ku2w). For this bead we only use the array for shape +
// length validation against the image parts; persisting the coordinates to
// each child row is deferred to a follow-up that wires the review UI.
type pageCell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// PageScanHandler accepts a multipart upload of N cropped card images plus a
// JSON `cells` field giving the (row, col) of each card on the source page.
// It creates one parent pokemon_scan_pages row and N pokemon_scan_jobs
// children with page_id set — the worker pipeline picks the children up
// through its existing single-card flow unchanged.
//
// Order of operations is strict to keep the cost cap honest and to guarantee
// a 429 or 5xx leaves zero state behind:
//  1. parse + validate parts and the cells JSON;
//  2. optimistic pre-check of the daily cap (fast-fail before disk I/O);
//  3. read + validate every card part into memory (oversize / wrong MIME);
//  4. write all card images to disk;
//  5. open a transaction, INSERT the parent pokemon_scan_pages row (which
//     acquires the SQLite write lock), then re-check the daily cap under
//     that lock so two concurrent uploads from the same user cannot both
//     pass with a stale count; if the definitive check fails, rollback the
//     parent row AND delete the on-disk files;
//  6. insert N pokemon_scan_jobs children and commit. On any failure roll
//     back rows AND delete files so a 5xx also leaves zero state.
//
// The original full-page photo is intentionally not accepted here — the spec
// for this bead only takes the cropped per-card images. The parent's
// page_image_path_enc column stays at its empty default; a future iteration
// can extend the form if the review UI needs the source frame.
func PageScanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, scanPageParseFormBytes)
		if err := r.ParseMultipartForm(scanPageParseFormBytes); err != nil {
			respondError(w, http.StatusBadRequest, "failed to parse multipart form")
			return
		}
		defer r.MultipartForm.RemoveAll()

		files := r.MultipartForm.File["images"]
		n := len(files)
		if n == 0 {
			respondError(w, http.StatusBadRequest, "at least one image is required")
			return
		}
		if n > scanPageMaxImagesPerPage {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("too many images (max %d)", scanPageMaxImagesPerPage))
			return
		}

		rawCells := r.MultipartForm.Value["cells"]
		if len(rawCells) == 0 || strings.TrimSpace(rawCells[0]) == "" {
			respondError(w, http.StatusBadRequest, "cells is required")
			return
		}
		var cells []pageCell
		if err := json.Unmarshal([]byte(rawCells[0]), &cells); err != nil {
			respondError(w, http.StatusBadRequest, "invalid cells JSON")
			return
		}
		if len(cells) != n {
			respondError(w, http.StatusBadRequest, "cells length must match image count")
			return
		}

		// Cost cap charging N (Hytte-6czh): every child job spawns its own
		// Claude vision call so the cap MUST be checked against (used + N)
		// up front. We refuse the whole batch atomically — partial uploads
		// would burn cap on cards we never store. This explicitly runs
		// before any image bytes are persisted so a 429 leaves zero state.
		used, err := countScansToday(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: count scans today: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}
		dailyCap, err := getUserScanDailyCap(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: load scan daily cap: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}
		if dailyCap-used < n {
			respondJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":     "daily scan cap reached",
				"cap":       dailyCap,
				"used":      used,
				"requested": n,
			})
			return
		}

		// Read + validate every card part into memory before touching the
		// database or disk. One bad part (oversize, wrong MIME) must
		// reject the whole batch rather than leave half-written rows or
		// files behind.
		type cardImage struct {
			buf  []byte
			hash string
		}
		cards := make([]cardImage, n)
		for i, fh := range files {
			if fh.Size > scanMaxImageBytes {
				respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
				return
			}
			f, err := fh.Open()
			if err != nil {
				log.Printf("pokemon: open page card part %d: %v", i, err)
				respondError(w, http.StatusInternalServerError, "failed to read image")
				return
			}
			buf, readErr := io.ReadAll(io.LimitReader(f, scanMaxImageBytes+1))
			f.Close()
			if readErr != nil {
				log.Printf("pokemon: read page card part %d: %v", i, readErr)
				respondError(w, http.StatusInternalServerError, "failed to read image")
				return
			}
			if int64(len(buf)) > scanMaxImageBytes {
				respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
				return
			}
			if !scanAllowedMIMETypes[detectImageMIME(buf)] {
				respondError(w, http.StatusBadRequest, "unsupported image type")
				return
			}
			sum := sha256.Sum256(buf)
			cards[i] = cardImage{
				buf:  buf,
				hash: hex.EncodeToString(sum[:]),
			}
		}

		// Write all card images to disk before opening the transaction so
		// the tx is short (just N+1 INSERTs). If any write fails we delete
		// the ones already on disk and return 500 — no DB state was
		// touched, so there is nothing to roll back yet.
		type cardWrite struct {
			diskPath string
			pathEnc  string
			hash     string
		}
		writes := make([]cardWrite, 0, n)
		cleanupFiles := func() {
			for _, cw := range writes {
				os.Remove(cw.diskPath)
			}
		}
		for i := range cards {
			uuid, err := scanUUID()
			if err != nil {
				cleanupFiles()
				log.Printf("pokemon: generate child scan uuid: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to generate scan id")
				return
			}
			imagePath, err := scanImagePathForUUID(user.ID, uuid)
			if err != nil {
				cleanupFiles()
				log.Printf("pokemon: child scan image path: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to prepare scan storage")
				return
			}
			if err := os.WriteFile(imagePath, cards[i].buf, 0600); err != nil {
				cleanupFiles()
				log.Printf("pokemon: write child scan image: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to save scan image")
				return
			}
			pathEnc, err := encryption.EncryptField(imagePath)
			if err != nil {
				os.Remove(imagePath)
				cleanupFiles()
				log.Printf("pokemon: encrypt child scan path: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
				return
			}
			writes = append(writes, cardWrite{
				diskPath: imagePath,
				pathEnc:  pathEnc,
				hash:     cards[i].hash,
			})
		}

		// Single transaction so the parent row + N children commit atomically.
		// The parent INSERT runs first: once it acquires the SQLite write lock,
		// no concurrent writer can insert jobs between the definitive cap check
		// and our own child inserts. On any failure roll back rows AND delete
		// files so a 5xx leaves zero state, matching the 429 contract above.
		now := time.Now().UTC()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			cleanupFiles()
			log.Printf("pokemon: begin page scan tx: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}

		pageRes, err := tx.ExecContext(r.Context(), `
			INSERT INTO pokemon_scan_pages (user_id, page_image_path_enc, expected_count, matched_count, created_at)
			VALUES (?, '', ?, 0, ?)
		`, user.ID, n, now)
		if err != nil {
			tx.Rollback()
			cleanupFiles()
			log.Printf("pokemon: insert scan page: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}
		pageID, err := pageRes.LastInsertId()
		if err != nil {
			tx.Rollback()
			cleanupFiles()
			log.Printf("pokemon: scan page last id: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}

		// Definitive cap check under the write lock. The INSERT above acquired
		// the write lock so this COUNT sees all prior commits; no concurrent
		// writer can insert scan jobs while we hold it.
		capLoc := scanLocalLocation()
		capNow := time.Now().In(capLoc)
		startOfDay := time.Date(capNow.Year(), capNow.Month(), capNow.Day(), 0, 0, 0, 0, capLoc)
		var usedNow int
		if err := tx.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM pokemon_scan_jobs
			WHERE user_id = ? AND created_at >= ?
		`, user.ID, startOfDay.UTC()).Scan(&usedNow); err != nil {
			tx.Rollback()
			cleanupFiles()
			log.Printf("pokemon: count scans today in tx: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}
		if dailyCap-usedNow < n {
			tx.Rollback()
			cleanupFiles()
			respondJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":     "daily scan cap reached",
				"cap":       dailyCap,
				"used":      usedNow,
				"requested": n,
			})
			return
		}

		jobIDs := make([]int64, 0, n)
		for _, cw := range writes {
			res, err := tx.ExecContext(r.Context(), `
				INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, page_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, user.ID, scanJobStatusQueued, cw.pathEnc, cw.hash, pageID, now)
			if err != nil {
				tx.Rollback()
				cleanupFiles()
				log.Printf("pokemon: insert child scan job: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
				return
			}
			jobID, err := res.LastInsertId()
			if err != nil {
				tx.Rollback()
				cleanupFiles()
				log.Printf("pokemon: child scan last id: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
				return
			}
			jobIDs = append(jobIDs, jobID)
		}

		if err := tx.Commit(); err != nil {
			cleanupFiles()
			log.Printf("pokemon: commit page scan tx: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue page scan")
			return
		}

		respondJSON(w, http.StatusAccepted, map[string]any{
			"page_id": pageID,
			"job_ids": jobIDs,
			"count":   n,
		})
	}
}

// QueueScanHandler accepts a multipart upload, persists the image to disk,
// and enqueues a row for the worker to process. It returns 202 immediately
// so the camera UI does not block on Claude's vision call.
func QueueScanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, scanParseFormBytes)
		if err := r.ParseMultipartForm(scanParseFormBytes); err != nil {
			respondError(w, http.StatusBadRequest, "failed to parse multipart form")
			return
		}
		defer r.MultipartForm.RemoveAll()

		file, header, err := r.FormFile("image")
		if err != nil {
			respondError(w, http.StatusBadRequest, "image is required")
			return
		}
		defer file.Close()

		if header.Size > scanMaxImageBytes {
			respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
			return
		}

		// Read the whole upload into memory so we can sniff MIME, hash the
		// bytes for dedupe, and write to disk once. 5 MB cap keeps this safe.
		// LimitReader uses cap+1 so an over-cap payload is detectable as the
		// extra byte rather than a silent truncation (Warden rule).
		limited := io.LimitReader(file, scanMaxImageBytes+1)
		buf, err := io.ReadAll(limited)
		if err != nil {
			log.Printf("pokemon: read scan upload: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to read image")
			return
		}
		if int64(len(buf)) > scanMaxImageBytes {
			respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
			return
		}

		mime := detectImageMIME(buf)
		if !scanAllowedMIMETypes[mime] {
			respondError(w, http.StatusBadRequest, "unsupported image type")
			return
		}

		sum := sha256.Sum256(buf)
		imageHash := hex.EncodeToString(sum[:])

		// Dedupe before any disk write: when the auto-detect camera loop
		// fires the same frame multiple times while a card lingers we want
		// to short-circuit to the original row, not burn another Claude
		// call (and another image file) on identical bytes. The window is
		// short (scanDedupeWindow) so two genuinely separate scans of the
		// same printed card seconds apart still count as distinct work.
		if dupID, found, err := findRecentDuplicateScan(r.Context(), db, user.ID, imageHash); err != nil {
			log.Printf("pokemon: scan dedupe lookup: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		} else if found {
			w.Header().Set("X-Pokemon-Scan-Dedupe", "true")
			respondJSON(w, http.StatusAccepted, map[string]any{
				"id":     dupID,
				"status": scanJobStatusQueued,
			})
			return
		}

		// Daily cap: every queue attempt counts (queued, processing, any
		// terminal status, retries) because every attempt charges Claude
		// vision. The 429 body includes the cap + used so the frontend can
		// render a friendly "47 / 600 today" message without an extra GET.
		used, err := countScansToday(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: count scans today: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		}
		dailyCap, err := getUserScanDailyCap(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: load scan daily cap: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		}
		if used >= dailyCap {
			respondJSON(w, http.StatusTooManyRequests, map[string]any{
				"error": "daily scan cap reached",
				"cap":   dailyCap,
				"used":  used,
			})
			return
		}

		uuid, err := scanUUID()
		if err != nil {
			log.Printf("pokemon: generate scan uuid: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to generate scan id")
			return
		}
		imagePath, err := scanImagePathForUUID(user.ID, uuid)
		if err != nil {
			log.Printf("pokemon: scan image path: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to prepare scan storage")
			return
		}
		if err := os.WriteFile(imagePath, buf, 0600); err != nil {
			log.Printf("pokemon: write scan image: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to save scan image")
			return
		}

		pathEnc, err := encryption.EncryptField(imagePath)
		if err != nil {
			os.Remove(imagePath)
			log.Printf("pokemon: encrypt scan image path: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		}

		res, err := db.ExecContext(r.Context(), `
			INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, user.ID, scanJobStatusQueued, pathEnc, imageHash, time.Now().UTC())
		if err != nil {
			os.Remove(imagePath)
			log.Printf("pokemon: insert scan job: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		}
		jobID, err := res.LastInsertId()
		if err != nil {
			log.Printf("pokemon: scan job last id: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to enqueue scan")
			return
		}

		respondJSON(w, http.StatusAccepted, map[string]any{
			"id":     jobID,
			"status": scanJobStatusQueued,
		})
	}
}

// ScanCountsHandler returns the small badge-shaped counts the sidebar polls
// for: how many jobs are sitting in a needs-attention state (matched,
// no_match, failed) and the daily-cap usage so the camera UI can flag the
// limit without a separate request. Cheap on purpose — a single COUNT over
// the user's rows plus the cached daily counters.
func ScanCountsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var unresolved int
		err := db.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM pokemon_scan_jobs
			WHERE user_id = ? AND status IN (?, ?, ?)
		`, user.ID, scanJobStatusMatched, scanJobStatusNoMatch, scanJobStatusFailed).Scan(&unresolved)
		if err != nil {
			log.Printf("pokemon: count unresolved scans: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to count scans")
			return
		}
		used, err := countScansToday(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: count scans today: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to count scans")
			return
		}
		dailyCap, err := getUserScanDailyCap(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: load scan daily cap: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to count scans")
			return
		}
		respondJSON(w, http.StatusOK, map[string]int{
			"unresolved": unresolved,
			"today_used": used,
			"today_cap":  dailyCap,
		})
	}
}

// ListScansHandler returns the current user's scan jobs newest-first, scoped
// to the comma-separated ?status= filter (defaults to "not yet resolved").
//
// Multi-card binder uploads (pokemon_scan_pages parents with N child jobs)
// are returned in a separate "pages" array: a page block is included as soon
// as any child matches the filter, and the response carries ALL of that
// page's children regardless of status so the grid renders the full N cells.
// Standalone scans (page_id IS NULL) stay in "scans" so the frontend can
// interleave the two arrays by created_at.
func ListScansHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Today's used + cap travel with the list response so the frontend
		// can render "47 / 600 today" without an extra round trip. We
		// compute it for every code path below (including the
		// empty-filter short-circuit) so the field is always present.
		used, err := countScansToday(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: count scans today: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list scans")
			return
		}
		dailyCap, err := getUserScanDailyCap(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("pokemon: load scan daily cap: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list scans")
			return
		}
		today := map[string]int{"used": used, "cap": dailyCap}

		statuses := parseScanStatusFilter(r.URL.Query().Get("status"))
		if len(statuses) == 0 {
			respondJSON(w, http.StatusOK, map[string]any{
				"scans": []ScanJobDTO{},
				"pages": []ScanPageDTO{},
				"today": today,
			})
			return
		}
		limit := parseLimit(r, defaultScanListLimit, maxScanListLimit)

		placeholders := make([]string, len(statuses))
		args := []any{user.ID}
		for i, s := range statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		args = append(args, limit)

		query := `
			SELECT id, status, image_path_enc, matched_card_id, confidence,
			       error_message, created_at, processed_at, resolved_at,
			       claude_response_enc, page_id
			FROM pokemon_scan_jobs
			WHERE user_id = ? AND status IN (` + strings.Join(placeholders, ",") + `)
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		`
		rows, err := db.QueryContext(r.Context(), query, args...)
		if err != nil {
			log.Printf("pokemon: list scans: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list scans")
			return
		}

		standalone := make([]ScanJobDTO, 0)
		pageIDOrder := make([]int64, 0)
		pageSeen := make(map[int64]bool)
		matchedCardIDs := make([]string, 0)
		jobMatched := make(map[int64]string)
		for rows.Next() {
			dto, cardID, pageID, err := scanRowToDTO(rows)
			if err != nil {
				rows.Close()
				log.Printf("pokemon: scan list row: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to list scans")
				return
			}
			if cardID != "" {
				jobMatched[dto.ID] = cardID
				matchedCardIDs = append(matchedCardIDs, cardID)
			}
			if pageID.Valid {
				if !pageSeen[pageID.Int64] {
					pageSeen[pageID.Int64] = true
					pageIDOrder = append(pageIDOrder, pageID.Int64)
				}
				continue
			}
			standalone = append(standalone, dto)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			log.Printf("pokemon: iterate scans: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list scans")
			return
		}
		rows.Close()

		// Load each touched page and ALL its children so the grid renders
		// the captured layout in full regardless of which children matched
		// the filter. Order is preserved from the original (newest first)
		// query so the response is stable.
		pages := make([]ScanPageDTO, 0, len(pageIDOrder))
		for _, pid := range pageIDOrder {
			page, children, cardIDs, err := loadScanPage(r.Context(), db, user.ID, pid)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				log.Printf("pokemon: load scan page %d: %v", pid, err)
				respondError(w, http.StatusInternalServerError, "failed to list scans")
				return
			}
			page.Children = children
			for cid, cardID := range cardIDs {
				if _, ok := jobMatched[cid]; !ok {
					jobMatched[cid] = cardID
					matchedCardIDs = append(matchedCardIDs, cardID)
				}
			}
			pages = append(pages, page)
		}

		if len(matchedCardIDs) > 0 {
			cards, err := loadCardsByIDs(r.Context(), db, user.ID, matchedCardIDs)
			if err != nil {
				log.Printf("pokemon: load matched cards: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to load matched cards")
				return
			}
			rate, rateOK := loadRate(r, db)
			applyNOK(w, cards, rate, rateOK)
			byID := make(map[string]CardDTO, len(cards))
			for _, c := range cards {
				byID[c.ID] = c
			}
			sets := make(map[string]*SetDTO)
			lookupSet := func(setID string) *SetDTO {
				if setID == "" {
					return nil
				}
				if cached, ok := sets[setID]; ok {
					return cached
				}
				loaded, loadErr := loadSetByID(r.Context(), db, user.ID, setID)
				if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
					log.Printf("pokemon: load matched set %s: %v", setID, loadErr)
				}
				sets[setID] = loaded
				return loaded
			}
			hydrate := func(job *ScanJobDTO) {
				cardID, ok := jobMatched[job.ID]
				if !ok {
					return
				}
				card, ok := byID[cardID]
				if !ok {
					return
				}
				cardCopy := card
				job.MatchedCard = &cardCopy
				if set := lookupSet(cardCopy.SetID); set != nil {
					setCopy := *set
					job.Set = &setCopy
				}
			}
			for i := range standalone {
				hydrate(&standalone[i])
			}
			for i := range pages {
				for j := range pages[i].Children {
					hydrate(&pages[i].Children[j])
				}
			}
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"scans": standalone,
			"pages": pages,
			"today": today,
		})
	}
}

// scanRowToDTO decodes one pokemon_scan_jobs row (already SELECTed) into a
// ScanJobDTO. Returns the matched_card_id and page_id alongside so the caller
// can hydrate cards in a single follow-up query and route page-grouped rows
// into the ScanPageDTO bucket.
func scanRowToDTO(rows *sql.Rows) (ScanJobDTO, string, sql.NullInt64, error) {
	var (
		id             int64
		status         string
		pathEnc        string
		matchedCard    sql.NullString
		confidence     sql.NullFloat64
		errorMessage   sql.NullString
		createdAt      time.Time
		processedAt    sql.NullTime
		resolvedAt     sql.NullTime
		claudeResponse sql.NullString
		pageID         sql.NullInt64
	)
	if err := rows.Scan(&id, &status, &pathEnc, &matchedCard, &confidence,
		&errorMessage, &createdAt, &processedAt, &resolvedAt, &claudeResponse, &pageID); err != nil {
		return ScanJobDTO{}, "", sql.NullInt64{}, err
	}
	dto := ScanJobDTO{
		ID:        id,
		Status:    status,
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
		HasImage:  scanImageOnDisk(pathEnc),
	}
	if confidence.Valid {
		c := confidence.Float64
		dto.Confidence = &c
	}
	if processedAt.Valid {
		ts := processedAt.Time.UTC().Format(time.RFC3339)
		dto.ProcessedAt = &ts
	}
	if resolvedAt.Valid {
		ts := resolvedAt.Time.UTC().Format(time.RFC3339)
		dto.ResolvedAt = &ts
	}
	if errorMessage.Valid {
		dto.ErrorMessage = errorMessage.String
	}
	if status == scanJobStatusNoMatch {
		dto.ParsedSetName, dto.ParsedCollectorNo = extractScanHints(claudeResponse)
	}
	cardID := ""
	if matchedCard.Valid {
		cardID = matchedCard.String
	}
	return dto, cardID, pageID, nil
}

// loadScanPage fetches the parent row plus every child that belongs to it,
// regardless of the child's status. The matchedCardIDs map (child id →
// pokemon_cards.id) lets the caller hydrate card metadata for the children
// in the same batch as standalone scans.
func loadScanPage(ctx context.Context, db *sql.DB, userID, pageID int64) (ScanPageDTO, []ScanJobDTO, map[int64]string, error) {
	var (
		expected  int
		matched   int
		createdAt time.Time
	)
	err := db.QueryRowContext(ctx, `
		SELECT expected_count, matched_count, created_at
		FROM pokemon_scan_pages
		WHERE id = ? AND user_id = ?
	`, pageID, userID).Scan(&expected, &matched, &createdAt)
	if err != nil {
		return ScanPageDTO{}, nil, nil, err
	}
	page := ScanPageDTO{
		ID:            pageID,
		ExpectedCount: expected,
		MatchedCount:  matched,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, status, image_path_enc, matched_card_id, confidence,
		       error_message, created_at, processed_at, resolved_at,
		       claude_response_enc, page_id
		FROM pokemon_scan_jobs
		WHERE user_id = ? AND page_id = ?
		ORDER BY id ASC
	`, userID, pageID)
	if err != nil {
		return ScanPageDTO{}, nil, nil, err
	}
	defer rows.Close()

	children := make([]ScanJobDTO, 0, expected)
	cardIDs := make(map[int64]string)
	for rows.Next() {
		dto, cardID, _, err := scanRowToDTO(rows)
		if err != nil {
			return ScanPageDTO{}, nil, nil, err
		}
		if cardID != "" {
			cardIDs[dto.ID] = cardID
		}
		children = append(children, dto)
	}
	if err := rows.Err(); err != nil {
		return ScanPageDTO{}, nil, nil, err
	}
	return page, children, cardIDs, nil
}

// DeleteScanPageHandler discards every child of a pokemon_scan_pages parent
// that isn't already in a terminal added/discarded state, deletes the parent
// row, and removes the on-disk images. Children already added to the
// collection are left alone so the user does not lose a card they kept by
// accident when clicking "Discard page". Returns 204 on success, 404 when
// the page does not belong to the caller.
func DeleteScanPageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		pageID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || pageID <= 0 {
			respondError(w, http.StatusBadRequest, "invalid page id")
			return
		}

		// Verify ownership before touching anything. A row owned by another
		// user must 404 (not 403) to avoid leaking the existence of foreign
		// page ids.
		var ownerID int64
		err = db.QueryRowContext(r.Context(), `
			SELECT user_id FROM pokemon_scan_pages WHERE id = ?
		`, pageID).Scan(&ownerID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && ownerID != user.ID) {
			respondError(w, http.StatusNotFound, "page not found")
			return
		}
		if err != nil {
			log.Printf("pokemon: load scan page for delete: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}

		// Reject the discard while any child is still being processed by the
		// scan worker. A worker that finishes after we discard the row updates
		// by id only (no status guard), so it would silently overwrite the
		// 'discarded' status back to 'matched'/'no_match'/'failed' and leave
		// the image_path_enc pointing at a file we may have deleted.
		var processingCount int
		if err := db.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM pokemon_scan_jobs
			WHERE user_id = ? AND page_id = ? AND status = ?
		`, user.ID, pageID, scanJobStatusProcessing).Scan(&processingCount); err != nil {
			log.Printf("pokemon: check processing children: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}
		if processingCount > 0 {
			respondError(w, http.StatusConflict, "page has cards still being processed; retry after they complete")
			return
		}

		now := time.Now().UTC()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			log.Printf("pokemon: begin page delete tx: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}

		// Mark every non-terminal child as discarded and capture the image
		// paths of *exactly* the rows we just discarded via RETURNING. Doing
		// it as a single atomic statement closes the TOCTOU window where a
		// concurrent INSERT (new sibling) or status flip to 'added' between a
		// preliminary SELECT and the UPDATE could leave us deleting an image
		// file still referenced by a row we didn't touch. image_path_enc is
		// cleared in a follow-up UPDATE keyed by the returned ids so the
		// RETURNING values reflect the path *before* it was zeroed out.
		discardedRows, err := tx.QueryContext(r.Context(), `
			UPDATE pokemon_scan_jobs
			SET status = ?, resolved_at = ?
			WHERE user_id = ? AND page_id = ?
			  AND status NOT IN (?, ?)
			RETURNING id, image_path_enc
		`, scanJobStatusDiscarded, now, user.ID, pageID,
			scanJobStatusAdded, scanJobStatusDiscarded)
		if err != nil {
			tx.Rollback()
			log.Printf("pokemon: discard child scans: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}
		var (
			discardedIDs []int64
			imagePaths   []string
		)
		for discardedRows.Next() {
			var (
				cID int64
				enc string
			)
			if err := discardedRows.Scan(&cID, &enc); err != nil {
				discardedRows.Close()
				tx.Rollback()
				log.Printf("pokemon: scan discard returning: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to delete page")
				return
			}
			discardedIDs = append(discardedIDs, cID)
			if enc != "" {
				imagePaths = append(imagePaths, enc)
			}
		}
		if err := discardedRows.Err(); err != nil {
			discardedRows.Close()
			tx.Rollback()
			log.Printf("pokemon: iterate discard returning: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}
		discardedRows.Close()

		if len(discardedIDs) > 0 {
			placeholders := make([]string, len(discardedIDs))
			args := make([]any, len(discardedIDs))
			for i, id := range discardedIDs {
				placeholders[i] = "?"
				args[i] = id
			}
			if _, err := tx.ExecContext(r.Context(), `
				UPDATE pokemon_scan_jobs
				SET image_path_enc = ''
				WHERE id IN (`+strings.Join(placeholders, ",")+`)
			`, args...); err != nil {
				tx.Rollback()
				log.Printf("pokemon: clear discarded paths: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to delete page")
				return
			}
		}

		// Delete the parent row last so the children's page_id FK transitions
		// to NULL via ON DELETE SET NULL. The discarded children survive as
		// history rows reachable via the "Recently resolved" filter.
		if _, err := tx.ExecContext(r.Context(), `
			DELETE FROM pokemon_scan_pages WHERE id = ? AND user_id = ?
		`, pageID, user.ID); err != nil {
			tx.Rollback()
			log.Printf("pokemon: delete scan page row: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("pokemon: commit page delete: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete page")
			return
		}

		// File deletes happen post-commit: a crash mid-loop leaves orphaned
		// files reachable by the scan_cleanup pass, while deleting before
		// commit risks losing files we'd need to keep on rollback.
		for _, enc := range imagePaths {
			deleteScanImage(enc)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetScanImageHandler streams the raw image bytes for a scan the caller
// owns. The Content-Type is sniffed from the actual file bytes (the queue
// stores PNG/WebP/HEIC under a .jpg extension) so a JPEG-only header doesn't
// misrepresent the payload — and X-Content-Type-Options: nosniff is set so
// browsers do not second-guess the type and treat unexpected bytes as e.g.
// HTML.
func GetScanImageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		jobID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || jobID <= 0 {
			respondError(w, http.StatusBadRequest, "invalid scan id")
			return
		}

		var pathEnc string
		err = db.QueryRowContext(r.Context(), `
			SELECT image_path_enc
			FROM pokemon_scan_jobs
			WHERE id = ? AND user_id = ?
		`, jobID, user.ID).Scan(&pathEnc)
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "scan not found")
			return
		}
		if err != nil {
			log.Printf("pokemon: load scan image row: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load scan")
			return
		}

		path, err := encryption.DecryptField(pathEnc)
		if err != nil || path == "" {
			respondError(w, http.StatusNotFound, "scan image not available")
			return
		}
		f, err := os.Open(path)
		if err != nil {
			respondError(w, http.StatusNotFound, "scan image not available")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			respondError(w, http.StatusNotFound, "scan image not available")
			return
		}

		// Sniff the actual MIME from the first 512 bytes (the window
		// http.DetectContentType reads) so the response header matches
		// the bytes on disk regardless of the .jpg extension. Anything
		// outside the upload allow-list collapses to octet-stream so a
		// surprise payload type cannot be served as a known image type.
		sniff := make([]byte, 512)
		n, _ := io.ReadFull(f, sniff)
		ctype := detectImageMIME(sniff[:n])
		if !scanAllowedMIMETypes[ctype] {
			ctype = "application/octet-stream"
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			log.Printf("pokemon: seek scan image: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to read scan image")
			return
		}

		w.Header().Set("Content-Type", ctype)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
	}
}

// ResolveScanHandler advances a scan job to a terminal resolution state.
// Action "add" inserts the picked variant into the user's collection and
// drops the image; "discard" drops the image; "retry" requeues a failed scan
// for another worker pass and preserves the image.
func ResolveScanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		jobID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || jobID <= 0 {
			respondError(w, http.StatusBadRequest, "invalid scan id")
			return
		}

		var body struct {
			Action    string `json:"action"`
			VariantID int64  `json:"variant_id"`
			Condition string `json:"condition"`
			Notes     string `json:"notes"`
			Quantity  int    `json:"quantity"`
			// CardID, when non-empty on action=add, overrides the auto-matched
			// card. Used by the scan detail "Wrong match?" flow so the user can
			// reassign a misclassified scan to the right card without
			// discarding it. The override card is also persisted back to
			// pokemon_scan_jobs.matched_card_id so the resolved row reflects
			// what was actually added.
			CardID string `json:"card_id"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		body.Action = strings.TrimSpace(strings.ToLower(body.Action))
		body.Condition = strings.TrimSpace(body.Condition)
		body.CardID = strings.TrimSpace(body.CardID)

		var (
			status      string
			pathEnc     string
			matchedCard sql.NullString
		)
		err = db.QueryRowContext(r.Context(), `
			SELECT status, image_path_enc, matched_card_id
			FROM pokemon_scan_jobs
			WHERE id = ? AND user_id = ?
		`, jobID, user.ID).Scan(&status, &pathEnc, &matchedCard)
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "scan not found")
			return
		}
		if err != nil {
			log.Printf("pokemon: load scan for resolve: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load scan")
			return
		}

		now := time.Now().UTC()
		switch body.Action {
		case "add":
			if status != scanJobStatusMatched {
				respondError(w, http.StatusBadRequest, "scan must be in 'matched' status to add")
				return
			}
			if !matchedCard.Valid || matchedCard.String == "" {
				respondError(w, http.StatusBadRequest, "scan has no matched card")
				return
			}
			if body.VariantID <= 0 {
				respondError(w, http.StatusBadRequest, "variant_id is required for action=add")
				return
			}
			// Resolve the effective card: when the client overrides via
			// body.CardID (the "Wrong match?" flow), validate the override
			// exists in the catalogue before touching the job. A bad id is a
			// 404 — same shape the collection endpoint would return.
			targetCardID := matchedCard.String
			if body.CardID != "" {
				var exists bool
				if err := db.QueryRowContext(r.Context(),
					`SELECT EXISTS(SELECT 1 FROM pokemon_cards WHERE id = ?)`, body.CardID,
				).Scan(&exists); err != nil {
					log.Printf("pokemon: resolve add lookup override card: %v", err)
					respondError(w, http.StatusInternalServerError, "failed to resolve scan")
					return
				}
				if !exists {
					respondError(w, http.StatusNotFound, "override card not found")
					return
				}
				targetCardID = body.CardID
			}
			// Run the conditional status flip and the collection upsert in
			// one transaction so two concurrent /resolve calls can't both
			// observe status='matched' from the earlier SELECT and both
			// double-add. The UPDATE's `status = scanJobStatusMatched`
			// predicate is the atomic claim: only one tx will see a row
			// affected; the loser sees 0 rows and returns 409.
			//
			// When the client overrides the auto-match we also rewrite
			// matched_card_id in the same UPDATE so the resolved row reflects
			// what was actually added rather than the original (wrong) guess.
			tx, err := db.BeginTx(r.Context(), nil)
			if err != nil {
				log.Printf("pokemon: resolve add begin tx: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			// When the client overrides the matched card, confidence no longer
			// corresponds to the stored card, so clear it to avoid inconsistency.
			clearConfidence := body.CardID != "" && body.CardID != matchedCard.String
			var res sql.Result
			if clearConfidence {
				res, err = tx.ExecContext(r.Context(), `
					UPDATE pokemon_scan_jobs
					SET status = ?, matched_card_id = ?, matched_variant_id = ?, resolved_at = ?, image_path_enc = '', confidence = NULL
					WHERE id = ? AND user_id = ? AND status = ?
				`, scanJobStatusAdded, targetCardID, body.VariantID, now, jobID, user.ID, scanJobStatusMatched)
			} else {
				res, err = tx.ExecContext(r.Context(), `
					UPDATE pokemon_scan_jobs
					SET status = ?, matched_card_id = ?, matched_variant_id = ?, resolved_at = ?, image_path_enc = ''
					WHERE id = ? AND user_id = ? AND status = ?
				`, scanJobStatusAdded, targetCardID, body.VariantID, now, jobID, user.ID, scanJobStatusMatched)
			}
			if err != nil {
				tx.Rollback()
				log.Printf("pokemon: resolve add claim: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			affected, err := res.RowsAffected()
			if err != nil {
				tx.Rollback()
				log.Printf("pokemon: resolve add rows affected: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			if affected == 0 {
				tx.Rollback()
				respondError(w, http.StatusConflict, "scan is no longer in 'matched' status")
				return
			}
			if _, err := upsertCollection(r.Context(), tx, user.ID, targetCardID, body.VariantID, body.Quantity, body.Condition, body.Notes); err != nil {
				tx.Rollback()
				switch {
				case errors.Is(err, errVariantNotFound):
					respondError(w, http.StatusNotFound, "variant not found")
				case errors.Is(err, errVariantMismatch):
					respondError(w, http.StatusBadRequest, "variant does not belong to matched card")
				case errors.Is(err, errInvalidCondition):
					respondError(w, http.StatusBadRequest, "invalid condition")
				case errors.Is(err, errInvalidQuantity):
					respondError(w, http.StatusBadRequest, "quantity must be non-negative")
				default:
					log.Printf("pokemon: resolve add upsert: %v", err)
					respondError(w, http.StatusInternalServerError, "failed to add to collection")
				}
				return
			}
			if err := tx.Commit(); err != nil {
				log.Printf("pokemon: resolve add commit: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			deleteScanImage(pathEnc)

		case "discard":
			discardRes, err := db.ExecContext(r.Context(), `
				UPDATE pokemon_scan_jobs
				SET status = ?, resolved_at = ?, image_path_enc = ''
				WHERE id = ? AND user_id = ? AND status IN (?, ?, ?, ?)
			`, scanJobStatusDiscarded, now, jobID, user.ID,
				scanJobStatusQueued, scanJobStatusMatched, scanJobStatusNoMatch, scanJobStatusFailed)
			if err != nil {
				log.Printf("pokemon: resolve discard: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			discardAffected, err := discardRes.RowsAffected()
			if err != nil {
				log.Printf("pokemon: resolve discard rows affected: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to resolve scan")
				return
			}
			if discardAffected == 0 {
				respondError(w, http.StatusConflict, "scan is not in a resolvable status")
				return
			}
			deleteScanImage(pathEnc)

		case "retry":
			if status != scanJobStatusNoMatch && status != scanJobStatusFailed {
				respondError(w, http.StatusBadRequest, "scan can only be retried from 'no_match' or 'failed' status")
				return
			}
			retryRes, err := db.ExecContext(r.Context(), `
				UPDATE pokemon_scan_jobs
				SET status = ?, error_message = NULL, processed_at = NULL, matched_card_id = NULL,
				    confidence = NULL, claude_response_enc = NULL
				WHERE id = ? AND user_id = ? AND status IN (?, ?)
			`, scanJobStatusQueued, jobID, user.ID, scanJobStatusNoMatch, scanJobStatusFailed)
			if err != nil {
				log.Printf("pokemon: resolve retry: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to retry scan")
				return
			}
			retryAffected, err := retryRes.RowsAffected()
			if err != nil {
				log.Printf("pokemon: resolve retry rows affected: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to retry scan")
				return
			}
			if retryAffected == 0 {
				respondError(w, http.StatusConflict, "scan is no longer in a retryable status")
				return
			}

		default:
			respondError(w, http.StatusBadRequest, "action must be one of add, discard, retry")
			return
		}

		dto, err := loadScanJob(r, w, db, user.ID, jobID)
		if err != nil {
			log.Printf("pokemon: reload scan after resolve: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to read scan")
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"scan": dto})
	}
}

// parseScanStatusFilter normalises the comma-separated status query string,
// filters out unknown values, and returns the default set when the caller
// omits the parameter. An empty allowed list short-circuits the query so the
// caller gets an empty response instead of "everything".
func parseScanStatusFilter(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		out := make([]string, len(defaultScanListStatuses))
		copy(out, defaultScanListStatuses)
		return out
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(strings.ToLower(p))
		if !allowedScanStatuses[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// scanImageOnDisk reports whether the encrypted path resolves to a readable
// file. Returns false on decrypt errors or missing files so the API surface
// is "is there a file to show?" rather than leaking detail about what failed.
func scanImageOnDisk(pathEnc string) bool {
	if pathEnc == "" {
		return false
	}
	path, err := encryption.DecryptField(pathEnc)
	if err != nil || path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// deleteScanImage removes the on-disk image after a resolve. Failures are
// logged but do not abort the resolve — the row is already marked terminal,
// and stale files can be reaped out-of-band.
func deleteScanImage(pathEnc string) {
	if pathEnc == "" {
		return
	}
	path, err := encryption.DecryptField(pathEnc)
	if err != nil || path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("pokemon: delete scan image %s: %v", path, err)
	}
}

// loadCardsByIDs returns the catalogue rows for a slice of card ids with the
// current user's ownership flags hydrated. Used by the scan list endpoint to
// embed matched cards in the response.
func loadCardsByIDs(ctx context.Context, db *sql.DB, userID int64, ids []string) ([]CardDTO, error) {
	if len(ids) == 0 {
		return []CardDTO{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
		SELECT id, set_id, name, collector_no, rarity, image_small_url, image_large_url
		FROM pokemon_cards
		WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]CardDTO, 0, len(ids))
	cardIndex := make(map[string]int)
	for rows.Next() {
		var c CardDTO
		if err := rows.Scan(&c.ID, &c.SetID, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL); err != nil {
			return nil, err
		}
		c.Variants = []VariantDTO{}
		cardIndex[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return cards, nil
	}
	return hydrateVariantsCtx(ctx, db, userID, cards, cardIndex)
}

// loadScanJob re-reads a single scan job after a resolve so the response
// reflects the persisted state (status, resolved_at, etc.). Hydrates the
// matched card and set the same way the list endpoint does.
func loadScanJob(r *http.Request, w http.ResponseWriter, db *sql.DB, userID, jobID int64) (*ScanJobDTO, error) {
	var (
		id             int64
		status         string
		pathEnc        string
		matchedCard    sql.NullString
		confidence     sql.NullFloat64
		errorMessage   sql.NullString
		createdAt      time.Time
		processedAt    sql.NullTime
		resolvedAt     sql.NullTime
		claudeResponse sql.NullString
	)
	err := db.QueryRowContext(r.Context(), `
		SELECT id, status, image_path_enc, matched_card_id, confidence,
		       error_message, created_at, processed_at, resolved_at,
		       claude_response_enc
		FROM pokemon_scan_jobs
		WHERE id = ? AND user_id = ?
	`, jobID, userID).Scan(&id, &status, &pathEnc, &matchedCard, &confidence,
		&errorMessage, &createdAt, &processedAt, &resolvedAt, &claudeResponse)
	if err != nil {
		return nil, err
	}
	dto := &ScanJobDTO{
		ID:        id,
		Status:    status,
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
		HasImage:  scanImageOnDisk(pathEnc),
	}
	if confidence.Valid {
		c := confidence.Float64
		dto.Confidence = &c
	}
	if processedAt.Valid {
		ts := processedAt.Time.UTC().Format(time.RFC3339)
		dto.ProcessedAt = &ts
	}
	if resolvedAt.Valid {
		ts := resolvedAt.Time.UTC().Format(time.RFC3339)
		dto.ResolvedAt = &ts
	}
	if errorMessage.Valid {
		dto.ErrorMessage = errorMessage.String
	}
	if status == scanJobStatusNoMatch {
		dto.ParsedSetName, dto.ParsedCollectorNo = extractScanHints(claudeResponse)
	}
	if matchedCard.Valid && matchedCard.String != "" {
		cards, loadErr := loadCardsByIDs(r.Context(), db, userID, []string{matchedCard.String})
		if loadErr != nil {
			return nil, loadErr
		}
		if len(cards) > 0 {
			rate, rateOK := loadRate(r, db)
			applyNOK(w, cards, rate, rateOK)
			c := cards[0]
			dto.MatchedCard = &c
			if c.SetID != "" {
				set, setErr := loadSetByID(r.Context(), db, userID, c.SetID)
				if setErr != nil && !errors.Is(setErr, sql.ErrNoRows) {
					return nil, setErr
				}
				if set != nil {
					dto.Set = set
				}
			}
		}
	}
	return dto, nil
}
