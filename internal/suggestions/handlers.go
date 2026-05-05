package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// validUserSuggestionTypes mirrors validTypes from generate.go but additionally
// allows "new_page" — a user can author a new-page suggestion explicitly,
// whereas Claude generation does not produce them per-page.
var validUserSuggestionTypes = map[string]bool{
	TypeAddition:    true,
	TypeBugfix:      true,
	TypeImprovement: true,
	TypeRefactor:    true,
	TypeNewPage:     true,
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

		if !validUserSuggestionTypes[body.Type] {
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

		if existing.Status == StatusRejected {
			writeJSON(w, http.StatusOK, existing)
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
