package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// runCancelsMu guards runCancels. The map holds the cancel func of each
// in-flight run keyed by run_id so CancelHandler can abort the streaming
// goroutine. Entries are inserted before the goroutine starts and removed by
// the goroutine's deferred cleanup, so a successful Lookup means the run is
// still executing on this server instance. After a process restart the map is
// empty even if suggestion_runs rows are still marked in-flight (those are
// orphaned and must be reaped by separate housekeeping, not by this endpoint).
var (
	runCancelsMu sync.Mutex
	runCancels   = map[int64]context.CancelFunc{}
)

// registerRunCancel records the cancel func for an in-flight run.
func registerRunCancel(runID int64, cancel context.CancelFunc) {
	runCancelsMu.Lock()
	runCancels[runID] = cancel
	runCancelsMu.Unlock()
}

// unregisterRunCancel drops the cancel func for runID. Safe to call even if no
// entry exists.
func unregisterRunCancel(runID int64) {
	runCancelsMu.Lock()
	delete(runCancels, runID)
	runCancelsMu.Unlock()
}

// lookupRunCancel returns the registered cancel func for runID and whether the
// run is currently executing on this process.
func lookupRunCancel(runID int64) (context.CancelFunc, bool) {
	runCancelsMu.Lock()
	cancel, ok := runCancels[runID]
	runCancelsMu.Unlock()
	return cancel, ok
}

// OverallRunTimeout caps the entire RunHandler invocation as a safety bound
// against stalled Claude calls pinning the request indefinitely. It is not
// sized to cover the absolute worst case (RotationDefaultN × PerPageTimeout
// = 20 × 240s ≈ 80 min); pages that have not completed when the deadline
// fires are skipped. 30 minutes bounds typical multi-page runs while
// allowing normally-slow pages to complete without premature cancellation.
const OverallRunTimeout = 30 * time.Minute

// PlanTimeout caps a single PlanHandler Claude invocation. Declared as var so
// tests can shrink it to verify timeout handling without hanging for two
// minutes per run. Production callers must not mutate it.
var PlanTimeout = 120 * time.Second

// NewPageSlug is the synthetic page_slug used for "new page" suggestions that
// do not target an existing page in the registry.
const NewPageSlug = "__new_page__"

// sseEvent is a single Server-Sent Event the work goroutine emits to the
// client. Name is the event: line; Data is JSON-marshalled into the data: line.
type sseEvent struct {
	Name string
	Data any
}

// startedEvent is the first event the SSE stream emits after the run row has
// been inserted. The frontend uses run_id to correlate later UpdateSuggestionRun
// audit rows with the in-flight stream.
type startedEvent struct {
	RunID      int64    `json:"run_id"`
	TotalPages int      `json:"total_pages"`
	PageSlugs  []string `json:"page_slugs"`
}

// pageCompleteEvent is emitted after each runForPage finishes (success or
// failure). Status is "ok" when the per-page Claude call returned and at least
// one row was inserted, "error" otherwise. Error carries the page-level error
// message (Claude / parse failure) when status != "ok"; per-row insert
// failures are surfaced via the errors counter.
type pageCompleteEvent struct {
	PageSlug  string  `json:"page_slug"`
	Generated int     `json:"generated"`
	Errors    int     `json:"errors"`
	CostUSD   float64 `json:"cost_usd"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Status    string  `json:"status"`
	Error     string  `json:"error,omitempty"`
}

// pageSkippedCapEvent is emitted when a page is skipped because it already
// has MaxPendingPerPage pending suggestions. No Claude call is made and no
// row is written, so the event carries only the slug and the cap state.
// The frontend renders this as a separate "skipped" entry in the run log so
// the operator sees the page was attempted and intentionally bypassed,
// rather than mistaking it for a silently-dropped page.
type pageSkippedCapEvent struct {
	PageSlug     string `json:"page_slug"`
	PendingCount int    `json:"pending_count"`
	Cap          int    `json:"cap"`
}

// newPageCompleteEvent mirrors pageCompleteEvent but for the separate
// new-page Claude pass. PageSlug is always NewPageSlug.
type newPageCompleteEvent struct {
	PageSlug  string  `json:"page_slug"`
	Generated int     `json:"generated"`
	Errors    int     `json:"errors"`
	CostUSD   float64 `json:"cost_usd"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Status    string  `json:"status"`
	Error     string  `json:"error,omitempty"`
}

// doneEvent is the final event in the stream. Totals match the row written to
// suggestion_runs by UpdateSuggestionRun, so a client that misses
// page_complete events can still render the final outcome.
type doneEvent struct {
	RunID     int64   `json:"run_id"`
	Generated int     `json:"generated"`
	Errors    int     `json:"errors"`
	CostUSD   float64 `json:"cost_usd"`
}

// RunHandler triggers a suggestions-generation pass for all rotation-enabled
// pages in the registry plus a separate new-page idea pass, streaming progress
// to the client over Server-Sent Events. Admin-only — relies on
// auth.RequireAdmin upstream to guarantee a non-nil admin user in the request
// context.
//
// The work loop runs on a context derived from context.Background() (not
// r.Context()) so a client disconnect mid-run does not abort persistence: the
// suggestion_runs row is still updated with finished_at and per-page
// suggestions still land in the DB. Concurrency is bounded per-user — a second
// call while a previous run is in flight returns 409.
//
// POST /api/suggestions/run
// Response: text/event-stream with events:
//   - started        {run_id, total_pages, page_slugs}
//   - page_complete  {page_slug, generated, errors, cost_usd, elapsed_ms, status, error?}
//   - new_page_complete {…same shape as page_complete, page_slug=__new_page__}
//   - done           {run_id, generated, errors, cost_usd}
func RunHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("suggestions: load claude config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Concurrency guard: a single user may have at most one in-flight run
		// at a time. The partial index idx_suggestion_runs_user_inflight keeps
		// this lookup O(1). Returning 409 with the in-flight run_id lets the
		// frontend resume tailing the existing stream rather than fork a
		// duplicate one.
		if existing, err := InflightRunForUser(r.Context(), db, user.ID); err != nil {
			log.Printf("suggestions: check inflight runs for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check in-flight runs"})
			return
		} else if existing != 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":  "a suggestion run is already in progress",
				"run_id": existing,
			})
			return
		}

		pages, err := RotationEligible(r.Context(), db)
		if err != nil {
			log.Printf("suggestions: load rotation-eligible pages for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load page settings"})
			return
		}

		// Drop pages already at the per-page cap before scheduling work. The
		// per-page deficit check inside runForPage is the authoritative guard,
		// but this filter keeps the rotation slots focused on pages that need
		// work and prevents wasted "skipped" log spam in nightly runs.
		pages, err = FilterUnderCap(r.Context(), db, user.ID, pages, MaxPendingPerPage)
		if err != nil {
			log.Printf("suggestions: filter under cap for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load page settings"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		startedAt := time.Now().UTC()
		pageSlugs := make([]string, len(pages))
		for i, p := range pages {
			pageSlugs[i] = p.Slug
		}

		// Insert the in-flight row before any header is written so a 500 here
		// surfaces as a normal JSON error rather than a torn SSE stream.
		runRow := SuggestionRun{
			UserID:    user.ID,
			StartedAt: startedAt,
			Trigger:   TriggerManual,
			PageSlugs: BuildPageSlugsCSV(pageSlugs, true),
		}
		runID, err := InsertSuggestionRun(r.Context(), db, runRow)
		if err != nil {
			log.Printf("suggestions: insert in-flight run for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start run"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Detached context: client disconnect must NOT abort persistence. The
		// work goroutine uses runCtx so suggestion_runs and per-page rows
		// continue to be written even after r.Context() cancels.
		runCtx, cancel := context.WithTimeout(context.Background(), OverallRunTimeout)

		// Register the cancel func so CancelHandler can abort this run. The
		// deferred unregister inside the goroutine fires after UpdateSuggestionRun
		// has written finished_at, so a cancel call after completion sees the row
		// as finished rather than racing the registry cleanup.
		registerRunCancel(runID, cancel)

		eventCh := make(chan sseEvent, 16)

		go func() {
			defer close(eventCh)
			defer cancel()
			defer unregisterRunCancel(runID)

			eventCh <- sseEvent{Name: "started", Data: startedEvent{
				RunID:      runID,
				TotalPages: len(pages),
				PageSlugs:  pageSlugs,
			}}

			var totals RunResult

			for _, page := range pages {
				if err := runCtx.Err(); err != nil {
					log.Printf("suggestions: context done before page %q: %v", page.Slug, err)
					totals.Errors++
					break
				}
				pageStart := time.Now()
				inserted, failed, cost, err := runForPage(runCtx, db, cfg, user.ID, page)
				elapsed := time.Since(pageStart).Milliseconds()
				totals.Generated += inserted
				totals.Errors += failed
				totals.CostUSD += cost

				// At-cap is a normal outcome of the deficit check, not an
				// error. Surface it as its own event so the live progress
				// log can show the page was skipped intentionally.
				var atCap *PageAtCapError
				if errors.As(err, &atCap) {
					eventCh <- sseEvent{Name: "page_skipped_cap", Data: pageSkippedCapEvent{
						PageSlug:     atCap.PageSlug,
						PendingCount: atCap.PendingCount,
						Cap:          atCap.Cap,
					}}
					continue
				}

				ev := pageCompleteEvent{
					PageSlug:  page.Slug,
					Generated: inserted,
					Errors:    failed,
					CostUSD:   cost,
					ElapsedMS: elapsed,
					Status:    "ok",
				}
				if err != nil {
					totals.Errors++
					log.Printf("suggestions: page %q errored: %v", page.Slug, err)
					ev.Status = "error"
					ev.Error = err.Error()
					ev.Errors++
				}
				eventCh <- sseEvent{Name: "page_complete", Data: ev}
			}

			// Skip the new-page pass entirely when runCtx has been cancelled
			// (operator-triggered cancel or overall-run timeout). Calling
			// RunNewPageSuggestion with a cancelled context would just produce a
			// fast error event and pad the audit row's error count without doing
			// useful work.
			if runCtx.Err() == nil {
				newPageStart := time.Now()
				newPageResult, err := RunNewPageSuggestion(runCtx, db, cfg, user.ID)
				newPageElapsed := time.Since(newPageStart).Milliseconds()
				totals.Generated += newPageResult.Generated
				totals.Errors += newPageResult.Errors
				totals.CostUSD += newPageResult.CostUSD

				npEvent := newPageCompleteEvent{
					PageSlug:  NewPageSlug,
					Generated: newPageResult.Generated,
					Errors:    newPageResult.Errors,
					CostUSD:   newPageResult.CostUSD,
					ElapsedMS: newPageElapsed,
					Status:    "ok",
				}
				if err != nil {
					log.Printf("suggestions: new_page run for user %d: %v", user.ID, err)
					totals.Errors++
					npEvent.Status = "error"
					npEvent.Error = err.Error()
					npEvent.Errors++
				}
				eventCh <- sseEvent{Name: "new_page_complete", Data: npEvent}
			}

			// Update the audit row with the final totals using a fresh
			// context: runCtx may be near its deadline by now, and we want
			// the audit write to land regardless.
			updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := UpdateSuggestionRun(updateCtx, db, runID, totals.Generated, totals.Errors, totals.CostUSD); err != nil {
				log.Printf("suggestions: update run %d for user %d: %v", runID, user.ID, err)
			}
			updateCancel()

			eventCh <- sseEvent{Name: "done", Data: doneEvent{
				RunID:     runID,
				Generated: totals.Generated,
				Errors:    totals.Errors,
				CostUSD:   totals.CostUSD,
			}}
		}()

	streaming:
		for {
			select {
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				writeSSE(w, flusher, ev)
			case <-r.Context().Done():
				// Client disconnected. We stop streaming but intentionally do
				// NOT cancel runCtx — the work goroutine continues so
				// suggestion_runs and generated suggestions are persisted to
				// completion.
				break streaming
			}
		}
		// Keep draining eventCh (without writing) so the work goroutine can
		// never block on a full channel and leak.
		for range eventCh {
		}
	}
}

// CancelHandler aborts an in-flight suggestion run for the requesting admin.
// It cancels the work goroutine's runCtx, which causes the streaming loop to
// stop after the current page and exit cleanly — UpdateSuggestionRun still
// fires from the goroutine's deferred cleanup, so suggestion_runs.finished_at
// is set and counts reflect what was persisted before the cancel.
//
// Returns 404 when the id is unknown OR belongs to another user
// (enumeration-resistant), 409 when the run has already finished or is no
// longer tracked on this process (e.g. after a server restart, the in-memory
// cancel handle is gone), and 202 Accepted when the cancel signal was
// delivered. Cancellation is observed between pages, so the goroutine may take
// up to one page-completion to exit.
//
// POST /api/suggestions/run/{id}/cancel
// Response: 202 with {run_id, cancelled: true} on success.
func CancelHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		idStr := chi.URLParam(r, "id")
		runID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		var (
			ownerID    int64
			finishedAt sql.NullString
		)
		err = db.QueryRowContext(r.Context(), `
			SELECT user_id, finished_at FROM suggestion_runs WHERE id = ?
		`, runID).Scan(&ownerID, &finishedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
				return
			}
			log.Printf("suggestions: load run %d for cancel: %v", runID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load run"})
			return
		}

		// Cross-user access returns 404 so other users' run IDs are not
		// discoverable, matching the pattern used by RejectHandler/PlanHandler.
		if ownerID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}

		if finishedAt.Valid {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "run has already finished"})
			return
		}

		cancel, ok := lookupRunCancel(runID)
		if !ok {
			// The DB row says in-flight but no cancel handle exists on this
			// process. Either the run is on another instance or the process
			// restarted and orphaned the row. Either way, this handler cannot
			// stop it — surface 409 rather than silently succeeding.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "run is not active on this server"})
			return
		}
		cancel()

		writeJSON(w, http.StatusAccepted, map[string]any{
			"run_id":    runID,
			"cancelled": true,
		})
	}
}

// writeSSE serialises an event in the standard "event: <name>\ndata: <json>\n\n"
// shape and flushes it to the wire. Errors are swallowed because a write
// failure means the client is gone and the work goroutine continues anyway.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev sseEvent) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		log.Printf("suggestions: marshal sse event %q: %v", ev.Name, err)
		return
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, data); err != nil {
		return
	}
	flusher.Flush()
}

// listResponse is the shape returned by GET /api/suggestions: the caller gets
// one bucket per status, including the terminal bead_created bucket so the UI
// can surface what has already shipped.
type listResponse struct {
	Pending     []Suggestion `json:"pending"`
	Planned     []Suggestion `json:"planned"`
	Rejected    []Suggestion `json:"rejected"`
	BeadCreated []Suggestion `json:"bead_created"`
}

// ListHandler returns the admin user's suggestions partitioned by status. Each
// bucket is decrypted at the boundary by the store layer so the response body
// holds plaintext title/body/feedback/plan.
//
// GET /api/suggestions
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		pending, err := ListByStatus(r.Context(), db, user.ID, StatusPending)
		if err != nil {
			log.Printf("suggestions: list pending for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list suggestions"})
			return
		}
		planned, err := ListByStatus(r.Context(), db, user.ID, StatusPlanned)
		if err != nil {
			log.Printf("suggestions: list planned for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list suggestions"})
			return
		}
		rejected, err := ListByStatus(r.Context(), db, user.ID, StatusRejected)
		if err != nil {
			log.Printf("suggestions: list rejected for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list suggestions"})
			return
		}
		beadCreated, err := ListByStatus(r.Context(), db, user.ID, StatusBeadCreated)
		if err != nil {
			log.Printf("suggestions: list bead_created for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list suggestions"})
			return
		}

		writeJSON(w, http.StatusOK, listResponse{
			Pending:     nilToEmpty(pending),
			Planned:     nilToEmpty(planned),
			Rejected:    nilToEmpty(rejected),
			BeadCreated: nilToEmpty(beadCreated),
		})
	}
}

// isValidUserSuggestionType reports whether t is valid for user-authored
// suggestions. It delegates to validTypes from generate.go as the single enum
// source of truth, extending it with TypeNewPage, which users can propose
// explicitly but Claude generation does not produce per-page.
func isValidUserSuggestionType(t string) bool {
	return validTypes[t] || t == TypeNewPage
}

// CreateHandler accepts a user-authored suggestion. The row is written with
// source="user" and status="pending"; type, size, and page_slug are validated
// against the same enums Claude is held to plus the synthetic "__new_page__"
// slug. Body/title are encrypted at rest by Insert.
//
// POST /api/suggestions
// Request:  {"type": "...", "size": "...", "page_slug": "...", "title": "...", "body": "..."}
// Response: 201 with the inserted Suggestion (decrypted).
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Type     string `json:"type"`
			Size     string `json:"size"`
			PageSlug string `json:"page_slug"`
			Title    string `json:"title"`
			Body     string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Type = strings.TrimSpace(body.Type)
		body.Size = strings.TrimSpace(body.Size)
		body.PageSlug = strings.TrimSpace(body.PageSlug)
		body.Title = strings.TrimSpace(body.Title)
		body.Body = strings.TrimSpace(body.Body)

		if !isValidUserSuggestionType(body.Type) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type"})
			return
		}
		if !validSizes[body.Size] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid size"})
			return
		}
		if !isValidPageSlug(body.PageSlug) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown page_slug"})
			return
		}
		// new_page type and __new_page__ slug are exclusive to each other: one
		// implies the other. Accepting them independently would produce rows whose
		// type and target contradict each other (e.g. a "bugfix" aimed at no page,
		// or a "new_page" targeting an existing page).
		if (body.Type == TypeNewPage) != (body.PageSlug == NewPageSlug) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type new_page requires page_slug __new_page__ and vice versa"})
			return
		}
		if body.Title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
			return
		}
		if body.Body == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
			return
		}

		row := Suggestion{
			UserID:      user.ID,
			GeneratedAt: time.Now().UTC(),
			PageSlug:    body.PageSlug,
			Source:      SourceUser,
			Type:        body.Type,
			Size:        body.Size,
			Title:       body.Title,
			Body:        body.Body,
			Status:      StatusPending,
		}
		id, err := Insert(r.Context(), db, row)
		if err != nil {
			log.Printf("suggestions: create for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create suggestion"})
			return
		}

		created, err := GetByID(r.Context(), db, id)
		if err != nil {
			log.Printf("suggestions: load created suggestion %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load created suggestion"})
			return
		}
		writeJSON(w, http.StatusCreated, created)
	}
}

// RejectHandler marks a suggestion as rejected. Idempotent: re-rejecting an
// already-rejected suggestion returns 200 with the existing row instead of
// erroring, so the UI does not have to special-case duplicate clicks.
//
// POST /api/suggestions/{id}/reject
func RejectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		existing, err := GetByID(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
				return
			}
			log.Printf("suggestions: load %d for reject: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}

		// Return 404 rather than 403 so that other users' suggestion IDs are
		// not discoverable by enumeration.
		if existing.UserID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
			return
		}

		if existing.Status == StatusRejected {
			writeJSON(w, http.StatusOK, existing)
			return
		}

		// bead_created is terminal — a linked bead already exists. Allowing a
		// reject here would silently drop the suggestion from the admin UI while
		// leaving the bead metadata behind in the row.
		if existing.Status == StatusBeadCreated {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot reject a suggestion with a linked bead"})
			return
		}

		if err := MarkRejected(r.Context(), db, id); err != nil {
			log.Printf("suggestions: mark rejected %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reject suggestion"})
			return
		}
		updated, err := GetByID(r.Context(), db, id)
		if err != nil {
			log.Printf("suggestions: reload rejected %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// PlanHandler asks Claude to produce a concrete implementation plan for a
// suggestion and persists the result. Synchronous: blocks until Claude returns
// or PlanTimeout elapses. Not idempotent — re-planning replaces the previous
// plan and resets planned_at.
//
// POST /api/suggestions/{id}/plan
// Request:  { "feedback"?: string }
// Response: 200 with the updated Suggestion (status=planned, plan populated).
func PlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		// Body is optional — an empty/missing body is valid for a fresh plan.
		var body struct {
			Feedback string `json:"feedback"`
		}
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}
		body.Feedback = strings.TrimSpace(body.Feedback)

		existing, err := GetByID(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
				return
			}
			log.Printf("suggestions: load %d for plan: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}

		// Same enumeration-resistance pattern as RejectHandler: cross-user access
		// returns 404 so other users' suggestion IDs are not discoverable.
		if existing.UserID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
			return
		}

		// bead_created is terminal — a linked bead already exists. Flipping
		// status back to planned would leave bead_id/bead_created_at behind and
		// produce an inconsistent row.
		if existing.Status == StatusBeadCreated {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot plan a suggestion with a linked bead"})
			return
		}

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("suggestions: load claude config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		page := findPageBySlug(existing.PageSlug)
		prompt := buildPlanPrompt(*existing, page, body.Feedback)

		ctx, cancel := context.WithTimeout(r.Context(), PlanTimeout)
		defer cancel()

		plan, _, err := runPromptFn(ctx, cfg, prompt)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				log.Printf("suggestions: plan %d timed out after %s", id, PlanTimeout)
				writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "Claude timed out generating the plan"})
				return
			}
			log.Printf("suggestions: plan %d claude error: %v", id, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to generate plan"})
			return
		}

		plan = strings.TrimSpace(plan)
		if plan == "" {
			log.Printf("suggestions: plan %d: empty response from Claude", id)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Claude returned an empty plan"})
			return
		}

		if err := MarkPlanned(r.Context(), db, id, plan); err != nil {
			log.Printf("suggestions: mark planned %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save plan"})
			return
		}

		updated, err := GetByID(r.Context(), db, id)
		if err != nil {
			log.Printf("suggestions: reload planned %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// findPageBySlug returns the registry entry matching slug, or nil if the slug
// is the synthetic new-page sentinel or no longer present in the registry.
// buildPlanPrompt handles a nil result.
func findPageBySlug(slug string) *Page {
	if slug == NewPageSlug {
		return nil
	}
	for i := range Pages {
		if Pages[i].Slug == slug {
			return &Pages[i]
		}
	}
	return nil
}

// pageSummary is the lightweight page descriptor returned to the UI.
// RotationEnabled is a pointer so it serializes as JSON null when no row
// exists in suggestion_page_settings (the default-enabled case) and as a
// bool when the admin has explicitly opted in or out. The synthetic
// "__new_page__" entry is also rendered with null since rotation does not
// apply to it.
type pageSummary struct {
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	RotationEnabled *bool  `json:"rotation_enabled"`
}

// PagesHandler returns the registry of pages a user-authored suggestion can
// target, plus the synthetic "__new_page__" entry for proposing brand-new
// pages. Order is stable: registry order followed by the new-page sentinel.
// Rotation eligibility controls auto-generation, not which pages a user can
// target manually, so this endpoint serves the full registry. Each registered
// page also carries its current rotation_enabled override (null when no row
// exists in suggestion_page_settings — the default is on).
//
// GET /api/suggestions/pages
func PagesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := loadPageRotationSettings(r.Context(), db)
		if err != nil {
			log.Printf("suggestions: load page rotation settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load page settings"})
			return
		}

		pages := AllRegistered()
		out := make([]pageSummary, 0, len(pages)+1)
		for _, p := range pages {
			summary := pageSummary{Slug: p.Slug, Title: p.Title}
			if v, ok := settings[p.Slug]; ok {
				enabled := v
				summary.RotationEnabled = &enabled
			}
			out = append(out, summary)
		}
		out = append(out, pageSummary{Slug: NewPageSlug, Title: "New page"})
		writeJSON(w, http.StatusOK, out)
	}
}

// UpdatePageSettingsHandler upserts the rotation_enabled flag for a single
// registered page. The slug must be in the curated registry (AllRegistered);
// the synthetic "__new_page__" sentinel is rejected because rotation does
// not apply to it. Returns the updated pageSummary on success.
//
// PATCH /api/suggestions/pages/{slug}
// Request:  { "rotation_enabled": bool }
// Response: 200 with the updated pageSummary.
func UpdatePageSettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(chi.URLParam(r, "slug"))
		if slug == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug is required"})
			return
		}

		// Look up the page in the curated registry. Unknown slugs (including
		// the synthetic __new_page__) return 404 — rotation only applies to
		// real registered pages.
		page := findPageBySlug(slug)
		if page == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown page slug"})
			return
		}

		// Use a *bool so a missing field is distinguishable from an explicit
		// false. Either case where the field is absent is a 400.
		var body struct {
			RotationEnabled *bool `json:"rotation_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.RotationEnabled == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rotation_enabled is required"})
			return
		}

		enabled := 0
		if *body.RotationEnabled {
			enabled = 1
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := db.ExecContext(r.Context(), `
			INSERT INTO suggestion_page_settings (page_slug, rotation_enabled, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(page_slug) DO UPDATE SET
				rotation_enabled = excluded.rotation_enabled,
				updated_at = excluded.updated_at
		`, slug, enabled, now); err != nil {
			log.Printf("suggestions: upsert page settings %q: %v", slug, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save page settings"})
			return
		}

		updated := *body.RotationEnabled
		writeJSON(w, http.StatusOK, pageSummary{
			Slug:            page.Slug,
			Title:           page.Title,
			RotationEnabled: &updated,
		})
	}
}

// loadPageRotationSettings returns the current per-page rotation_enabled
// overrides keyed by page_slug. A missing slug means no override exists and
// the page defaults to enabled.
func loadPageRotationSettings(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT page_slug, rotation_enabled FROM suggestion_page_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var slug string
		var enabled int
		if err := rows.Scan(&slug, &enabled); err != nil {
			return nil, err
		}
		out[slug] = enabled == 1
	}
	return out, rows.Err()
}

// isValidPageSlug returns true if slug is the synthetic new-page sentinel or a
// slug from the registry. Users can target any registered page regardless of
// rotation state.
func isValidPageSlug(slug string) bool {
	if slug == NewPageSlug {
		return true
	}
	for _, p := range AllRegistered() {
		if p.Slug == slug {
			return true
		}
	}
	return false
}

// nilToEmpty returns an empty slice when given nil so JSON encoding produces
// `[]` rather than `null` — easier for the frontend to consume unconditionally.
func nilToEmpty(s []Suggestion) []Suggestion {
	if s == nil {
		return []Suggestion{}
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
