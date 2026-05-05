package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// OverallRunTimeout caps the entire RunHandler invocation. Five pages × 90s
// per-page worst case fits under 10 minutes, but Claude can occasionally
// stall — we want to return a result rather than hang the request indefinitely.
const OverallRunTimeout = 10 * time.Minute

// PlanTimeout caps a single PlanHandler Claude invocation. Declared as var so
// tests can shrink it to verify timeout handling without hanging for two
// minutes per run. Production callers must not mutate it.
var PlanTimeout = 120 * time.Second

// NewPageSlug is the synthetic page_slug used for "new page" suggestions that
// do not target an existing page in the registry.
const NewPageSlug = "__new_page__"

// RunHandler triggers a synchronous suggestions-generation pass for all enabled
// pages in the registry. Admin-only — relies on auth.RequireAdmin upstream to
// guarantee a non-nil admin user in the request context.
//
// POST /api/suggestions/run
// Response: { "generated": int, "errors": int }
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

		ctx, cancel := context.WithTimeout(r.Context(), OverallRunTimeout)
		defer cancel()

		result := RunSuggestionsForPages(ctx, db, cfg, user.ID, EnabledPages())
		writeJSON(w, http.StatusOK, result)
	}
}

// listResponse is the shape returned by GET /api/suggestions: the caller gets
// one bucket per actionable status. Suggestions in the bead_created status are
// terminal and not surfaced here.
type listResponse struct {
	Pending  []Suggestion `json:"pending"`
	Planned  []Suggestion `json:"planned"`
	Rejected []Suggestion `json:"rejected"`
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

		writeJSON(w, http.StatusOK, listResponse{
			Pending:  nilToEmpty(pending),
			Planned:  nilToEmpty(planned),
			Rejected: nilToEmpty(rejected),
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

		plan, err := runPromptFn(ctx, cfg, prompt)
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
type pageSummary struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// PagesHandler returns the registry of pages a user-authored suggestion can
// target, plus the synthetic "__new_page__" entry for proposing brand-new
// pages. Order is stable: registry order followed by the new-page sentinel.
//
// GET /api/suggestions/pages
func PagesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pages := EnabledPages()
		out := make([]pageSummary, 0, len(pages)+1)
		for _, p := range pages {
			out = append(out, pageSummary{Slug: p.Slug, Title: p.Title})
		}
		out = append(out, pageSummary{Slug: NewPageSlug, Title: "New page"})
		writeJSON(w, http.StatusOK, out)
	}
}

// isValidPageSlug returns true if slug is the synthetic new-page sentinel or a
// slug from the enabled-pages registry.
func isValidPageSlug(slug string) bool {
	if slug == NewPageSlug {
		return true
	}
	for _, p := range EnabledPages() {
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
