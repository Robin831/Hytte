package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// RunClaudeAnalysis runs Claude classification on a workout: builds the prompt,
// calls Claude, stores the analysis, applies ai: tags, and sets the AI title.
// It is safe to call from a goroutine with a detached context.
func RunClaudeAnalysis(ctx context.Context, db *sql.DB, workoutID, userID int64) error {
	workout, err := getWorkoutWithLaps(db, workoutID, userID)
	if err != nil {
		return fmt.Errorf("load workout: %w", err)
	}

	cfg, err := LoadClaudeConfig(db, userID)
	if err != nil {
		return fmt.Errorf("load claude config: %w", err)
	}
	if !cfg.Enabled {
		return fmt.Errorf("claude is not enabled")
	}

	prompt := BuildClassificationPrompt(workout)
	response, err := RunPrompt(ctx, cfg, prompt)
	if err != nil {
		return fmt.Errorf("claude prompt: %w", err)
	}

	analysisTag, analysisSummary, analysisType, analysisTitle := parseClaudeResponse(response)

	var aiTags []string
	if analysisTag != "" {
		aiTags = append(aiTags, "ai:"+analysisTag)
	}
	if analysisType != "" {
		aiTags = append(aiTags, "ai:"+analysisType)
	}

	analysis := &WorkoutAnalysis{
		UserID:       userID,
		WorkoutID:    workoutID,
		AnalysisType: "tag",
		Model:        cfg.Model,
		Prompt:       prompt,
		ResponseJSON: response,
		Tags:         strings.Join(aiTags, ","),
		Summary:      analysisSummary,
		Title:        analysisTitle,
	}

	if err := UpsertAnalysis(db, analysis); err != nil {
		return fmt.Errorf("store analysis: %w", err)
	}

	if len(aiTags) > 0 {
		if err := AddAITags(db, workoutID, userID, aiTags); err != nil {
			log.Printf("Failed to add AI tags to workout %d: %v", workoutID, err)
		}
	}

	if analysisTitle != "" {
		if err := SetAITitle(db, workoutID, userID, analysisTitle); err != nil {
			log.Printf("Failed to set AI title for workout %d: %v", workoutID, err)
		}
	}

	return nil
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
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude analysis failed"})
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

// parseClaudeResponse extracts tag, summary, type, and title from Claude's JSON response.
func parseClaudeResponse(response string) (tag, summary, workoutType, title string) {
	// Strip markdown code fences if present.
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		// Remove first and last lines (code fences).
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	response = strings.TrimSpace(response)

	var parsed struct {
		Type    string `json:"type"`
		Tag     string `json:"tag"`
		Summary string `json:"summary"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		// If parsing fails, use the raw response as summary.
		return "", response, "", ""
	}
	return parsed.Tag, parsed.Summary, parsed.Type, parsed.Title
}
