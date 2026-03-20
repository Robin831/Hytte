package training

import (
	"database/sql"
	"encoding/json"
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

		// Load workout with laps for prompt building.
		workout, err := getWorkoutWithLaps(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}

		// Load Claude config.
		cfg, err := LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Build prompt and call Claude.
		prompt := BuildClassificationPrompt(workout)
		response, err := RunPrompt(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude analysis failed for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude analysis failed"})
			return
		}

		// Parse Claude's JSON response.
		analysisTag, analysisSummary, analysisType, analysisTitle := parseClaudeResponse(response)

		// Build tag list from response, prefixed with ai:.
		var aiTags []string
		if analysisTag != "" {
			aiTags = append(aiTags, "ai:"+analysisTag)
		}
		if analysisType != "" {
			aiTags = append(aiTags, "ai:"+analysisType)
		}

		tagsStr := strings.Join(aiTags, ",")

		analysis := &WorkoutAnalysis{
			UserID:       user.ID,
			WorkoutID:    id,
			AnalysisType: "tag",
			Model:        cfg.Model,
			Prompt:       prompt,
			ResponseJSON: response,
			Tags:         tagsStr,
			Summary:      analysisSummary,
			Title:        analysisTitle,
		}

		if err := UpsertAnalysis(db, analysis); err != nil {
			log.Printf("Failed to store analysis for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store analysis"})
			return
		}

		// Apply ai: tags to the workout.
		if len(aiTags) > 0 {
			if err := AddAITags(db, id, user.ID, aiTags); err != nil {
				log.Printf("Failed to add AI tags to workout %d: %v", id, err)
			}
		}

		// Update workout title if the user hasn't manually set one.
		if analysisTitle != "" {
			if err := SetAITitle(db, id, user.ID, analysisTitle); err != nil {
				log.Printf("Failed to set AI title for workout %d: %v", id, err)
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
