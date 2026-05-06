package suggestions

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/forge"
	"github.com/go-chi/chi/v5"
)

// BeadCreateTimeout caps how long we wait for `bd create` to return. Declared
// as var so tests do not have to wait the full 60s when exercising timeouts.
var BeadCreateTimeout = 60 * time.Second

// MaxBeadTitleLength caps the title we hand to bd create. bd has its own
// limits and we don't want to surprise it with arbitrarily long titles.
const MaxBeadTitleLength = 120

// beadIDPattern matches the freshly-issued bead ID in `bd create` stdout.
// The format is "✓ Created issue: <Anvil>-XXXX — ..." and we accept any anvil
// prefix here so we don't have to rebuild this regex if we ever rename Hytte.
var beadIDPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*-[a-z0-9]+`)

// bdCreateFn is the function used to invoke `bd create`. Replaced in tests,
// mirroring the runPromptFn pattern in generate.go.
var bdCreateFn = defaultBdCreate

// defaultBdCreate runs `bd create` against the Hytte anvil. Title is passed
// inline; the description body is streamed on stdin via --body-file -.
func defaultBdCreate(ctx context.Context, cwd, title, body string) (stdout, stderr string, err error) {
	args := []string{
		"create", title,
		"--type", "feature",
		"--priority", "3",
		"--labels", "forgeReady",
		"--body-file", "-",
	}
	cmd := exec.CommandContext(ctx, forge.ResolveCommand("bd"), args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(body)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	return outBuf.String(), errBuf.String(), runErr
}

// CreateBeadHandler turns a planned suggestion into a real bead by shelling
// out to `bd create` against the Hytte anvil with the forgeReady label set.
// Admin-only — relies on auth.RequireAdmin upstream.
//
// POST /api/suggestions/{id}/bead
// Response: 200 with the updated Suggestion (status=bead_created, bead_id set)
func CreateBeadHandler(db *sql.DB) http.HandlerFunc {
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
			log.Printf("suggestions: load %d for bead create: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}

		// Cross-user enumeration guard, same as RejectHandler/PlanHandler.
		if existing.UserID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
			return
		}

		if existing.Status != StatusPlanned {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "suggestion must be planned before creating a bead"})
			return
		}

		if strings.TrimSpace(existing.Plan) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "suggestion has no plan body; edit and save a plan before creating a bead"})
			return
		}

		cwd, err := forge.AnvilDirForBead("Hytte-x")
		if err != nil {
			log.Printf("suggestions: resolve Hytte anvil dir: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to resolve Hytte anvil directory"})
			return
		}

		title := capTitle(existing.Title, MaxBeadTitleLength)
		body := buildBeadBody(*existing)

		ctx, cancel := context.WithTimeout(r.Context(), BeadCreateTimeout)
		defer cancel()

		stdout, stderr, runErr := bdCreateFn(ctx, cwd, title, body)
		if runErr != nil {
			trimmed := strings.TrimSpace(stderr)
			if trimmed == "" {
				trimmed = runErr.Error()
			}
			const maxStderrLen = 500
			if len(trimmed) > maxStderrLen {
				trimmed = trimmed[:maxStderrLen] + "…"
			}
			log.Printf("suggestions: bd create for suggestion %d failed: %v (stderr=%s)", id, runErr, trimmed)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("bd create failed: %s", trimmed)})
			return
		}

		beadID := beadIDPattern.FindString(stdout)
		if beadID == "" {
			log.Printf("suggestions: bd create for suggestion %d succeeded but no bead ID in stdout: %q", id, stdout)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "bd create succeeded but no bead ID was returned"})
			return
		}

		// Use a fresh context for persistence so a client disconnect after bd create
		// completes doesn't leave the bead unlinked in the DB.
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer persistCancel()

		if err := MarkBeadCreated(persistCtx, db, id, beadID); err != nil {
			log.Printf("suggestions: mark bead created %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record bead creation"})
			return
		}

		updated, err := GetByID(persistCtx, db, id)
		if err != nil {
			log.Printf("suggestions: reload bead-created %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load suggestion"})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// capTitle returns the input trimmed and truncated at max characters. Truncation
// happens on rune boundaries so we never split a multibyte character mid-codepoint.
func capTitle(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max]))
}

// buildBeadBody assembles the markdown description we hand to bd create.
// It leads with the saved plan body, then appends footer sections so the
// resulting bead is self-contained and traceable back to the suggestion.
func buildBeadBody(s Suggestion) string {
	var b strings.Builder
	if plan := strings.TrimSpace(s.Plan); plan != "" {
		b.WriteString(plan)
		b.WriteString("\n\n")
	}
	b.WriteString("## Source\n\n")
	fmt.Fprintf(&b, "Created from Suggestions page (suggestion id %d, page slug %q, source=%s).\n", s.ID, s.PageSlug, s.Source)
	if feedback := strings.TrimSpace(s.Feedback); feedback != "" {
		b.WriteString("\n## User feedback when planning\n\n")
		b.WriteString(feedback)
		b.WriteString("\n")
	}
	if body := strings.TrimSpace(s.Body); body != "" {
		b.WriteString("\n## Original suggestion\n\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}
