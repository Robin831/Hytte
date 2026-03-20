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
			sanitizeAnalysis(cached)
			writeJSON(w, http.StatusOK, map[string]any{"analysis": cached, "cached": true})
			return
		}
		if err != nil && err != sql.ErrNoRows {
			log.Printf("Failed to check cached analysis for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		// Run Claude analysis (builds prompt, calls Claude, stores results).
		if err := RunClaudeAnalysis(r.Context(), db, id, user.ID); err != nil {
			log.Printf("Claude analysis failed for workout %d: %v", id, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
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

		// Fetch the freshly stored analysis to return to the client.
		analysis, err := GetAnalysis(db, user.ID, id, "tag")
		if err != nil {
			log.Printf("Failed to load analysis after run for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load analysis"})
			return
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
