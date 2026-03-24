package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
// userProfileBlock is an optional pre-built user profile block; hasGoalRace indicates whether
// the user has a goal race set (avoids brittle string matching on the profile block text);
// zones is optional HR zone distribution;
// historicalContext is an optional pre-built historical context block;
// enrichedBlock is an optional pre-built block of computed training metrics (HR drift, pace CV, training load, ACR).
func buildInsightsPrompt(w *Workout, userProfileBlock string, hasGoalRace bool, zones []ZoneDistribution, historicalContext string, enrichedBlock string) string {
	dur := formatDurationSecs(w.DurationSeconds)
	dist := fmt.Sprintf("%.2f km", w.DistanceMeters/1000)

	var sb strings.Builder
	sb.WriteString("Analyze this workout and provide coaching insights. Respond with JSON only, no markdown.\n\n")

	if userProfileBlock != "" {
		sb.WriteString(userProfileBlock)
		sb.WriteString("\n")
	}

	if hasGoalRace {
		sb.WriteString("Consider how this workout fits into the athlete's preparation for their goal race.\n\n")
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

	if enrichedBlock != "" {
		sb.WriteString("\n")
		sb.WriteString(enrichedBlock)
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

	if historicalContext != "" {
		sb.WriteString("\n")
		sb.WriteString(historicalContext)
	}

	if historicalContext != "" {
		sb.WriteString(`
Respond with this exact JSON structure:
{
  "effort_summary": "Brief overall effort assessment",
  "pacing_analysis": "Analysis of pacing strategy and consistency",
  "hr_zones": "Heart rate zone distribution observations",
  "threshold_context": "Assessment of effort relative to user's personal thresholds and zones",
  "observations": ["observation 1", "observation 2"],
  "suggestions": ["suggestion 1", "suggestion 2"],
  "risk_flags": ["specific risk if ACR > 1.3 or load spike detected — empty array if none"],
  "trend_analysis": {
    "fitness_direction": "improving|stable|declining|insufficient data",
    "comparison_to_recent": "How this workout compares to recent similar workouts",
    "notable_changes": ["notable change 1", "notable change 2"]
  },
  "confidence_score": 0.85,
  "confidence_note": "Brief explanation of confidence level and any data limitations"
}`)
	} else {
		sb.WriteString(`
Respond with this exact JSON structure:
{
  "effort_summary": "Brief overall effort assessment",
  "pacing_analysis": "Analysis of pacing strategy and consistency",
  "hr_zones": "Heart rate zone distribution observations",
  "threshold_context": "Assessment of effort relative to user's personal thresholds and zones",
  "observations": ["observation 1", "observation 2"],
  "suggestions": ["suggestion 1", "suggestion 2"],
  "risk_flags": ["specific risk if ACR > 1.3 or load spike detected — empty array if none"],
  "confidence_score": 0.85,
  "confidence_note": "Brief explanation of confidence level and any data limitations"
}`)
	}

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

// ErrInsightsAlreadyCached is returned by RunInsightsAnalysis when insights are
// already in the cache and no new analysis was performed.
var ErrInsightsAlreadyCached = errors.New("insights already cached")

// RunInsightsAnalysis generates and caches AI coaching insights for a workout.
// If insights are already cached ErrInsightsAlreadyCached is returned.
// Returns ErrClaudeNotEnabled when Claude is disabled in the user's preferences.
// Safe to call from background goroutines; all errors are returned rather than written to HTTP.
func RunInsightsAnalysis(ctx context.Context, db *sql.DB, workoutID, userID int64) error {
	// Skip if already cached. Return the error to let the caller retry rather
	// than proceeding with potentially duplicate analysis work.
	cached, err := GetCachedInsights(db, workoutID, userID)
	if err != nil {
		return fmt.Errorf("check insights cache for workout %d: %w", workoutID, err)
	}
	if cached != nil {
		return ErrInsightsAlreadyCached
	}

	workout, err := GetByID(db, workoutID, userID)
	if err != nil {
		return fmt.Errorf("load workout %d: %w", workoutID, err)
	}

	cfg, err := LoadClaudeConfig(db, userID)
	if err != nil {
		return fmt.Errorf("load Claude config: %w", err)
	}
	if !cfg.Enabled {
		return ErrClaudeNotEnabled
	}

	profile := BuildUserTrainingProfile(db, userID)
	zones, zoneErr := GetZoneDistribution(db, workoutID, userID, profile.ThresholdHR)
	if zoneErr != nil {
		log.Printf("RunInsightsAnalysis: zone distribution for workout %d: %v", workoutID, zoneErr)
		zones = nil
	}

	historicalContext := BuildHistoricalContext(db, userID, workout)
	enrichedBlock := BuildEnrichedWorkoutBlock(db, workout)

	prompt := buildInsightsPrompt(workout, profile.Block, profile.HasGoalRace, zones, historicalContext, enrichedBlock)
	raw, err := runPromptFunc(ctx, cfg, prompt)
	if err != nil {
		return fmt.Errorf("Claude insights for workout %d: %w", workoutID, err)
	}

	insights, err := parseInsightsResponse(raw)
	if err != nil {
		return fmt.Errorf("parse insights response for workout %d: %w", workoutID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := SaveInsights(db, workoutID, userID, insights, cfg.Model, now); err != nil {
		return fmt.Errorf("cache insights for workout %d: %w", workoutID, err)
	}

	return nil
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

		// Generate and cache insights via the standalone function.
		genErr := RunInsightsAnalysis(r.Context(), db, id, user.ID)
		if genErr != nil && !errors.Is(genErr, ErrInsightsAlreadyCached) {
			log.Printf("Insights analysis failed for workout %d: %v", id, genErr)
			if errors.Is(genErr, ErrClaudeNotEnabled) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			} else if errors.Is(genErr, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate insights"})
			}
			return
		}

		// Retrieve the cached result. Mark as fresh only when insights were just generated.
		result, err := GetCachedInsights(db, id, user.ID)
		if err != nil || result == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve generated insights"})
			return
		}
		result.Cached = errors.Is(genErr, ErrInsightsAlreadyCached)
		writeJSON(w, http.StatusOK, map[string]any{"insights": result})
	}
}
