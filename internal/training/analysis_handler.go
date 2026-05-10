package training

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// sanitizeAnalysis clears internal fields before sending to the frontend.
func sanitizeAnalysis(a *WorkoutAnalysis) {
	a.Prompt = ""
	a.ResponseJSON = ""
}

// runInsightsFunc is the function used to run insights analysis from
// AnalyzeHandler. Override in tests to observe or stub the call.
var runInsightsFunc = RunInsightsAnalysis

// AnalyzeHandler handles POST /api/training/workouts/{id}/analyze.
// Returns cached analysis if available, otherwise runs Claude classification.
func AnalyzeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		// Check for cached result first.
		cached, err := GetAnalysis(db, user.ID, id, "tag")
		if err == nil && cached != nil {
			// Tag analysis is cached. Still attempt insights so workouts analyzed
			// before this change (tag cached, no insights row) get Insights on the
			// next Analyze click. Errors are log-only — the cached tag result wins.
			if insErr := runInsightsFunc(r.Context(), db, id, user.ID); insErr != nil {
				if !errors.Is(insErr, ErrInsightsAlreadyCached) && !errors.Is(insErr, ErrClaudeNotEnabled) {
					log.Printf("Insights analysis failed for workout %d after cached analyze: %v", id, insErr)
				}
			}
			sanitizeAnalysis(cached)
			writeJSON(w, http.StatusOK, map[string]any{"analysis": cached, "cached": true})
			return
		}
		if err != nil && err != sql.ErrNoRows {
			log.Printf("Failed to check cached analysis for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		// Refuse with 409 when no workout_context row exists. The user must
		// capture surface, run type, HR source, feel notes, and the planned
		// speed structure before Claude is called.
		if _, err := GetWorkoutContext(db, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "workout_context_required"})
				return
			}
			log.Printf("Failed to load workout context for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout context"})
			return
		}

		// Set status to pending before running analysis.
		if err := UpdateAnalysisStatus(db, id, user.ID, "pending"); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			} else {
				log.Printf("Failed to set pending analysis status for workout %d: %v", id, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			}
			return
		}

		// Run Claude analysis (builds prompt, calls Claude, stores results).
		if err := RunClaudeAnalysis(r.Context(), db, id, user.ID); err != nil {
			if updateErr := UpdateAnalysisStatus(db, id, user.ID, "failed"); updateErr != nil {
				log.Printf("Failed to set failed analysis status for workout %d: %v", id, updateErr)
			}
			log.Printf("Claude analysis failed for workout %d: %v", id, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			} else if errors.Is(err, ErrWorkoutContextRequired) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "workout_context_required"})
			} else if errors.Is(err, ErrClaudeNotEnabled) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": ErrClaudeNotEnabled.Error()})
			} else if strings.Contains(err.Error(), "failed to load Claude configuration") {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			} else if strings.Contains(err.Error(), "claude prompt:") {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude CLI not reachable"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude analysis failed"})
			}
			return
		}

		if err := UpdateAnalysisStatus(db, id, user.ID, "completed"); err != nil {
			log.Printf("Failed to set completed analysis status for workout %d: %v", id, err)
		}

		// Fetch the freshly stored analysis to return to the client.
		analysis, err := GetAnalysis(db, user.ID, id, "tag")
		if err != nil {
			log.Printf("Failed to load analysis after run for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load analysis"})
			return
		}

		// Manual click is explicit consent, so insights run regardless of the
		// ai_auto_analyze pref (which gates only the upload-time auto-trigger).
		// Failures are log-only — the tag analysis already succeeded.
		if insErr := runInsightsFunc(r.Context(), db, id, user.ID); insErr != nil {
			if !errors.Is(insErr, ErrInsightsAlreadyCached) && !errors.Is(insErr, ErrClaudeNotEnabled) {
				log.Printf("Insights analysis failed for workout %d after manual analyze: %v", id, insErr)
			}
		}

		sanitizeAnalysis(analysis)
		writeJSON(w, http.StatusOK, map[string]any{"analysis": analysis, "cached": false})
	}
}

// GetAnalysisHandler handles GET /api/training/workouts/{id}/analysis.
func GetAnalysisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		analysis, err := GetAnalysis(db, user.ID, id, "tag")
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no analysis found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load analysis"})
			return
		}

		sanitizeAnalysis(analysis)
		writeJSON(w, http.StatusOK, map[string]any{"analysis": analysis})
	}
}

// DeleteAnalysisHandler handles DELETE /api/training/workouts/{id}/analysis.
func DeleteAnalysisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		// Verify workout ownership.
		var ownerID int64
		err = db.QueryRow(`SELECT user_id FROM workouts WHERE id = ?`, id).Scan(&ownerID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		if ownerID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}

		if err := DeleteAnalysis(db, user.ID, id, "tag"); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete analysis"})
			return
		}

		// Also remove ai: tags from the workout (scoped to verified owner).
		if _, err := db.Exec(`DELETE FROM workout_tags WHERE workout_id = ? AND tag GLOB 'ai:*' AND workout_id IN (SELECT id FROM workouts WHERE user_id = ?)`, id, user.ID); err != nil {
			log.Printf("Failed to remove AI tags from workout %d: %v", id, err)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
