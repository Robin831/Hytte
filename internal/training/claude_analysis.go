package training

import (
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

// AITagsHandler generates AI-powered tag suggestions for a workout.
// POST /api/training/workouts/{id}/ai-tags
func AITagsHandler(db *sql.DB) http.HandlerFunc {
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

		cfg, err := LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled"})
			return
		}

		workout, err := GetByID(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}

		prompt := buildTagPrompt(workout)
		result, err := RunPrompt(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude AI tags error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI analysis failed"})
			return
		}

		tags := parseTagResponse(result)
		writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	}
}

// AIFeedbackHandler generates AI training feedback for a workout.
// POST /api/training/workouts/{id}/ai-feedback
func AIFeedbackHandler(db *sql.DB) http.HandlerFunc {
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

		cfg, err := LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled"})
			return
		}

		workout, err := GetByID(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}

		prompt := buildFeedbackPrompt(workout)
		result, err := RunPrompt(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude AI feedback error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI analysis failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"feedback": result})
	}
}

// AICompareInsightsHandler generates AI-powered comparison insights for two workouts.
// POST /api/training/compare/ai-insights
func AICompareInsightsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
		var body struct {
			WorkoutAID int64 `json:"workout_a_id"`
			WorkoutBID int64 `json:"workout_b_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.WorkoutAID == 0 || body.WorkoutBID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workout_a_id and workout_b_id are required"})
			return
		}

		cfg, err := LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled"})
			return
		}

		workoutA, err := GetByID(db, body.WorkoutAID, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout A not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout A"})
			return
		}

		workoutB, err := GetByID(db, body.WorkoutBID, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout B not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout B"})
			return
		}

		prompt := buildComparePrompt(workoutA, workoutB)
		result, err := RunPrompt(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude AI compare insights error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI analysis failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"insights": result})
	}
}

// buildTagPrompt creates the prompt for AI tag generation.
func buildTagPrompt(w *Workout) string {
	var sb strings.Builder
	sb.WriteString("You are a sports training analyst. Analyze this workout and suggest 2-5 short, descriptive tags.\n")
	sb.WriteString("Return ONLY a JSON array of tag strings, nothing else. Example: [\"tempo run\", \"high intensity\", \"long run\"]\n\n")
	sb.WriteString("Workout data:\n")
	fmt.Fprintf(&sb, "- Sport: %s\n", w.Sport)
	fmt.Fprintf(&sb, "- Title: %s\n", w.Title)
	fmt.Fprintf(&sb, "- Date: %s\n", w.StartedAt)
	fmt.Fprintf(&sb, "- Duration: %d seconds\n", w.DurationSeconds)
	if w.DistanceMeters > 0 {
		fmt.Fprintf(&sb, "- Distance: %.0f meters\n", w.DistanceMeters)
	}
	if w.AvgHeartRate > 0 {
		fmt.Fprintf(&sb, "- Avg HR: %d bpm (Max: %d bpm)\n", w.AvgHeartRate, w.MaxHeartRate)
	}
	if w.AvgPaceSecPerKm > 0 {
		fmt.Fprintf(&sb, "- Avg Pace: %.1f sec/km\n", w.AvgPaceSecPerKm)
	}
	if w.Calories > 0 {
		fmt.Fprintf(&sb, "- Calories: %d\n", w.Calories)
	}
	if w.AscentMeters > 0 {
		fmt.Fprintf(&sb, "- Elevation gain: %.0f m\n", w.AscentMeters)
	}
	if len(w.Tags) > 0 {
		fmt.Fprintf(&sb, "- Existing tags: %s\n", strings.Join(w.Tags, ", "))
	}

	if len(w.Laps) > 1 {
		sb.WriteString("\nLap data:\n")
		for _, lap := range w.Laps {
			fmt.Fprintf(&sb, "  Lap %d: %.0fs, %.0fm", lap.LapNumber, lap.DurationSeconds, lap.DistanceMeters)
			if lap.AvgHeartRate > 0 {
				fmt.Fprintf(&sb, ", %d bpm", lap.AvgHeartRate)
			}
			if lap.AvgPaceSecPerKm > 0 {
				fmt.Fprintf(&sb, ", %.1f sec/km", lap.AvgPaceSecPerKm)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nSuggest tags that describe the workout type, intensity, and structure. Do NOT duplicate existing tags.")
	return sb.String()
}

// buildFeedbackPrompt creates the prompt for AI training feedback.
func buildFeedbackPrompt(w *Workout) string {
	var sb strings.Builder
	sb.WriteString("You are an experienced endurance coach. Provide brief, actionable training feedback for this workout.\n")
	sb.WriteString("Keep your response concise (3-5 short paragraphs max). Focus on:\n")
	sb.WriteString("1. What went well\n2. Areas for improvement\n3. Recovery/next session suggestions\n\n")
	sb.WriteString("Workout data:\n")
	fmt.Fprintf(&sb, "- Sport: %s\n", w.Sport)
	fmt.Fprintf(&sb, "- Title: %s\n", w.Title)
	fmt.Fprintf(&sb, "- Date: %s\n", w.StartedAt)
	fmt.Fprintf(&sb, "- Duration: %d seconds (%d min)\n", w.DurationSeconds, w.DurationSeconds/60)
	if w.DistanceMeters > 0 {
		fmt.Fprintf(&sb, "- Distance: %.0f meters (%.2f km)\n", w.DistanceMeters, w.DistanceMeters/1000)
	}
	if w.AvgHeartRate > 0 {
		fmt.Fprintf(&sb, "- Avg HR: %d bpm, Max HR: %d bpm\n", w.AvgHeartRate, w.MaxHeartRate)
	}
	if w.AvgPaceSecPerKm > 0 {
		mins := int(w.AvgPaceSecPerKm) / 60
		secs := int(w.AvgPaceSecPerKm) % 60
		fmt.Fprintf(&sb, "- Avg Pace: %d:%02d /km\n", mins, secs)
	}
	if w.AvgCadence > 0 {
		fmt.Fprintf(&sb, "- Avg Cadence: %d spm\n", w.AvgCadence)
	}
	if w.Calories > 0 {
		fmt.Fprintf(&sb, "- Calories: %d\n", w.Calories)
	}
	if w.AscentMeters > 0 {
		fmt.Fprintf(&sb, "- Elevation: +%.0fm / -%.0fm\n", w.AscentMeters, w.DescentMeters)
	}
	if len(w.Tags) > 0 {
		fmt.Fprintf(&sb, "- Tags: %s\n", strings.Join(w.Tags, ", "))
	}

	if len(w.Laps) > 1 {
		sb.WriteString("\nInterval/Lap breakdown:\n")
		for _, lap := range w.Laps {
			fmt.Fprintf(&sb, "  Lap %d: %.0fs", lap.LapNumber, lap.DurationSeconds)
			if lap.DistanceMeters > 0 {
				fmt.Fprintf(&sb, ", %.0fm", lap.DistanceMeters)
			}
			if lap.AvgHeartRate > 0 {
				fmt.Fprintf(&sb, ", avg HR %d (max %d)", lap.AvgHeartRate, lap.MaxHeartRate)
			}
			if lap.AvgPaceSecPerKm > 0 {
				mins := int(lap.AvgPaceSecPerKm) / 60
				secs := int(lap.AvgPaceSecPerKm) % 60
				fmt.Fprintf(&sb, ", pace %d:%02d/km", mins, secs)
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// buildComparePrompt creates the prompt for AI comparison insights.
func buildComparePrompt(a, b *Workout) string {
	var sb strings.Builder
	sb.WriteString("You are a sports training analyst. Compare these two workouts and provide insights.\n")
	sb.WriteString("Keep your response concise (3-4 short paragraphs). Focus on:\n")
	sb.WriteString("1. Key differences in performance\n2. Signs of improvement or regression\n3. What the data suggests about fitness trends\n\n")

	writeWorkoutSummary := func(label string, w *Workout) {
		fmt.Fprintf(&sb, "%s:\n", label)
		fmt.Fprintf(&sb, "  Title: %s\n", w.Title)
		fmt.Fprintf(&sb, "  Sport: %s\n", w.Sport)
		fmt.Fprintf(&sb, "  Date: %s\n", w.StartedAt)
		fmt.Fprintf(&sb, "  Duration: %d sec (%d min)\n", w.DurationSeconds, w.DurationSeconds/60)
		if w.DistanceMeters > 0 {
			fmt.Fprintf(&sb, "  Distance: %.0fm (%.2f km)\n", w.DistanceMeters, w.DistanceMeters/1000)
		}
		if w.AvgHeartRate > 0 {
			fmt.Fprintf(&sb, "  Avg HR: %d bpm, Max HR: %d bpm\n", w.AvgHeartRate, w.MaxHeartRate)
		}
		if w.AvgPaceSecPerKm > 0 {
			mins := int(w.AvgPaceSecPerKm) / 60
			secs := int(w.AvgPaceSecPerKm) % 60
			fmt.Fprintf(&sb, "  Avg Pace: %d:%02d /km\n", mins, secs)
		}
		if w.AvgCadence > 0 {
			fmt.Fprintf(&sb, "  Cadence: %d spm\n", w.AvgCadence)
		}
		if len(w.Tags) > 0 {
			fmt.Fprintf(&sb, "  Tags: %s\n", strings.Join(w.Tags, ", "))
		}

		if len(w.Laps) > 1 {
			sb.WriteString("  Laps:\n")
			for _, lap := range w.Laps {
				fmt.Fprintf(&sb, "    Lap %d: %.0fs", lap.LapNumber, lap.DurationSeconds)
				if lap.DistanceMeters > 0 {
					fmt.Fprintf(&sb, ", %.0fm", lap.DistanceMeters)
				}
				if lap.AvgHeartRate > 0 {
					fmt.Fprintf(&sb, ", %d bpm", lap.AvgHeartRate)
				}
				if lap.AvgPaceSecPerKm > 0 {
					mins := int(lap.AvgPaceSecPerKm) / 60
					secs := int(lap.AvgPaceSecPerKm) % 60
					fmt.Fprintf(&sb, ", %d:%02d/km", mins, secs)
				}
				sb.WriteString("\n")
			}
		}
	}

	writeWorkoutSummary("Workout A (earlier)", a)
	sb.WriteString("\n")
	writeWorkoutSummary("Workout B (later)", b)

	return sb.String()
}

// parseTagResponse extracts tags from Claude's response.
// Expects a JSON array like ["tag1", "tag2"] but handles plain text fallback.
func parseTagResponse(response string) []string {
	response = strings.TrimSpace(response)

	// Try JSON array parse first.
	var tags []string
	if err := json.Unmarshal([]byte(response), &tags); err == nil {
		// Filter empty tags and trim whitespace.
		result := []string{}
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				result = append(result, t)
			}
		}
		return result
	}

	// Extract JSON array if embedded in surrounding text.
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(response[start:end+1]), &tags); err == nil {
			result := []string{}
			for _, t := range tags {
				t = strings.TrimSpace(t)
				if t != "" {
					result = append(result, t)
				}
			}
			return result
		}
	}

	// Fallback: split by newlines or commas.
	lines := strings.FieldsFunc(response, func(c rune) bool {
		return c == '\n' || c == ','
	})
	result := []string{}
	for _, line := range lines {
		tag := strings.TrimSpace(line)
		tag = strings.TrimLeft(tag, "-•* ")
		tag = strings.Trim(tag, "\"'")
		if tag != "" {
			result = append(result, tag)
		}
	}
	return result
}
