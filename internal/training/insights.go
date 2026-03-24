package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)


// GetCachedInsights retrieves cached insights for a workout owned by userID, or returns nil if none exist.
func GetCachedInsights(db *sql.DB, workoutID, userID int64) (*CachedInsights, error) {
	var response, model, createdAt string
	err := db.QueryRow(
		`SELECT response, model, created_at FROM training_insights WHERE workout_id = ? AND user_id = ?`,
		workoutID, userID,
	).Scan(&response, &model, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var insights TrainingInsights
	if err := json.Unmarshal([]byte(response), &insights); err != nil {
		return nil, fmt.Errorf("unmarshal cached insights: %w", err)
	}
	insights.normalize()

	// Normalize legacy rows that have an empty created_at.
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &CachedInsights{
		TrainingInsights: insights,
		Model:            model,
		CreatedAt:        createdAt,
		Cached:           true,
	}, nil
}

// SaveInsights caches insights for a workout owned by userID.
func SaveInsights(db *sql.DB, workoutID, userID int64, insights *TrainingInsights, model, createdAt string) error {
	data, err := json.Marshal(insights)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO training_insights (workout_id, user_id, response, model, created_at) VALUES (?, ?, ?, ?, ?)`,
		workoutID, userID, string(data), model, createdAt,
	)
	return err
}

// formatDurationSecs formats a duration in seconds as h:mm:ss (when hours > 0) or m:ss.
func formatDurationSecs(secs int) string {
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// buildInsightsPrompt constructs the prompt to send to Claude for workout analysis.
// userProfileBlock is an optional pre-built user profile block; zones is optional HR zone distribution.
func buildInsightsPrompt(w *Workout, userProfileBlock string, zones []ZoneDistribution) string {
	dur := formatDurationSecs(w.DurationSeconds)
	dist := fmt.Sprintf("%.2f km", w.DistanceMeters/1000)

	var sb strings.Builder
	sb.WriteString("Analyze this workout and provide coaching insights. Respond with JSON only, no markdown.\n\n")

	if userProfileBlock != "" {
		sb.WriteString(userProfileBlock)
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "Date: %s\n", w.StartedAt)
	fmt.Fprintf(&sb, "Sport: %s\n", w.Sport)
	fmt.Fprintf(&sb, "Duration: %s, Distance: %s\n", dur, dist)
	if w.AvgHeartRate > 0 {
		fmt.Fprintf(&sb, "Avg HR: %d bpm, Max HR: %d bpm\n", w.AvgHeartRate, w.MaxHeartRate)
	}
	if w.AvgPaceSecPerKm > 0 {
		paceMin := int(w.AvgPaceSecPerKm) / 60
		paceSec := int(w.AvgPaceSecPerKm) % 60
		fmt.Fprintf(&sb, "Avg Pace: %d:%02d /km\n", paceMin, paceSec)
	}
	if w.AvgCadence > 0 {
		fmt.Fprintf(&sb, "Avg Cadence: %d spm\n", w.AvgCadence)
	}
	if w.AscentMeters > 0 {
		fmt.Fprintf(&sb, "Elevation: +%.0fm / -%.0fm\n", w.AscentMeters, w.DescentMeters)
	}

	if len(w.Laps) > 1 {
		sb.WriteString("\nLaps:\n")
		sb.WriteString("| # | Duration | Distance | Avg HR | Max HR | Pace |\n")
		sb.WriteString("|---|----------|----------|--------|--------|------|\n")
		for _, lap := range w.Laps {
			lapDur := formatDurationSecs(int(lap.DurationSeconds))
			lapDist := fmt.Sprintf("%.2f km", lap.DistanceMeters/1000)
			lapPace := "--:--"
			if lap.AvgPaceSecPerKm > 0 {
				lapPace = fmt.Sprintf("%d:%02d", int(lap.AvgPaceSecPerKm)/60, int(lap.AvgPaceSecPerKm)%60)
			}
			hrStr := "-"
			if lap.AvgHeartRate > 0 {
				hrStr = strconv.Itoa(lap.AvgHeartRate)
			}
			maxHRStr := "-"
			if lap.MaxHeartRate > 0 {
				maxHRStr = strconv.Itoa(lap.MaxHeartRate)
			}
			fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s | %s /km |\n",
				lap.LapNumber, lapDur, lapDist, hrStr, maxHRStr, lapPace)
		}
	}

	if len(zones) > 0 {
		sb.WriteString("\nHR Zone Distribution:\n")
		sb.WriteString("| Zone | Name | Time | % |\n")
		sb.WriteString("|------|------|------|---|\n")
		for _, z := range zones {
			fmt.Fprintf(&sb, "| %d | %s | %s | %.0f%% |\n",
				z.Zone, z.Name, formatDurationSecs(int(z.DurationS)), z.Percentage)
		}
	}

	sb.WriteString(`
Respond with this exact JSON structure:
{
  "effort_summary": "Brief overall effort assessment",
  "pacing_analysis": "Analysis of pacing strategy and consistency",
  "hr_zones": "Heart rate zone distribution observations",
  "threshold_context": "Assessment of effort relative to user's personal thresholds and zones",
  "observations": ["observation 1", "observation 2"],
  "suggestions": ["suggestion 1", "suggestion 2"]
}`)

	return sb.String()
}

// parseInsightsResponse extracts TrainingInsights from the Claude response text.
func parseInsightsResponse(raw string) (*TrainingInsights, error) {
	// Claude may wrap the JSON in markdown code fences.
	cleaned := raw
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}

	var insights TrainingInsights
	if err := json.Unmarshal([]byte(cleaned), &insights); err != nil {
		return nil, fmt.Errorf("parse insights JSON: %w", err)
	}
	insights.normalize()
	return &insights, nil
}

// InsightsHandler handles POST /api/training/workouts/{id}/insights.
func InsightsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		// Check cache first (scoped to user).
		cached, err := GetCachedInsights(db, id, user.ID)
		if err != nil {
			log.Printf("Failed to check insights cache: %v", err)
		}
		if cached != nil {
			writeJSON(w, http.StatusOK, map[string]any{"insights": cached})
			return
		}

		// Load workout data (verifies ownership).
		workout, err := GetByID(db, id, user.ID)
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

		// Build user profile block and extract threshold HR in a single preferences load.
		profile := BuildUserTrainingProfile(db, user.ID)
		userProfileBlock := profile.Block

		// Get zone distribution using the user's threshold HR (falls back to 0 if unset).
		zones, zoneErr := GetZoneDistribution(db, id, user.ID, profile.ThresholdHR)
		if zoneErr != nil {
			log.Printf("Failed to get zone distribution for workout %d: %v", id, zoneErr)
			zones = nil
		}

		// Build prompt and call Claude.
		prompt := buildInsightsPrompt(workout, userProfileBlock, zones)
		raw, err := runPromptFunc(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude insights error for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate insights"})
			return
		}

		insights, err := parseInsightsResponse(raw)
		if err != nil {
			log.Printf("Failed to parse insights response for workout %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse AI response"})
			return
		}

		// Use a single timestamp for both the cached row and the response,
		// so first-request and cached responses always agree on created_at.
		now := time.Now().UTC().Format(time.RFC3339)

		// Cache the result.
		if err := SaveInsights(db, id, user.ID, insights, cfg.Model, now); err != nil {
			log.Printf("Failed to cache insights for workout %d: %v", id, err)
		}

		result := &CachedInsights{
			TrainingInsights: *insights,
			Model:            cfg.Model,
			CreatedAt:        now,
			Cached:           false,
		}
		writeJSON(w, http.StatusOK, map[string]any{"insights": result})
	}
}
