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
)

// CompareAnalyzeHandler handles POST /api/training/compare/analyze?a={id}&b={id}.
// It generates AI-powered natural language comparison insights for two workouts,
// with caching so repeated requests return instantly.
func CompareAnalyzeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		idA, errA := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
		idB, errB := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
		if errA != nil || errB != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "both 'a' and 'b' workout IDs are required"})
			return
		}
		if idA <= 0 || idB <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workout IDs must be positive integers"})
			return
		}
		if idA == idB {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot compare a workout with itself"})
			return
		}

		// Check cache first.
		cached, err := GetCachedComparisonAnalysis(db, idA, idB, user.ID)
		if err != nil {
			log.Printf("Failed to check comparison analysis cache: %v", err)
		}
		if cached != nil {
			writeJSON(w, http.StatusOK, map[string]any{"analysis": cached})
			return
		}

		// Load both workouts (verifies ownership).
		workoutA, err := getWorkoutWithLaps(db, idA, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout A not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout A"})
			return
		}

		workoutB, err := getWorkoutWithLaps(db, idB, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout B not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout B"})
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

		// Run the structural comparison first so we can include it in the prompt.
		comparison, err := CompareWorkouts(db, idA, idB, user.ID, nil, nil)
		if err != nil {
			log.Printf("Structural comparison failed for %d vs %d: %v", idA, idB, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to run structural comparison"})
			return
		}

		// Build prompt and call Claude.
		prompt := buildComparisonAnalysisPrompt(workoutA, workoutB, comparison)
		raw, err := runPromptFunc(r.Context(), cfg, prompt)
		if err != nil {
			log.Printf("Claude comparison analysis error for %d vs %d: %v", idA, idB, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate comparison analysis"})
			return
		}

		analysis, err := parseComparisonAnalysisResponse(raw)
		if err != nil {
			log.Printf("Failed to parse comparison analysis response for %d vs %d: %v", idA, idB, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse AI response"})
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)

		// Cache the result.
		if err := SaveComparisonAnalysis(db, idA, idB, user.ID, analysis, cfg.Model, prompt, now); err != nil {
			log.Printf("Failed to cache comparison analysis for %d vs %d: %v", idA, idB, err)
		}

		result := &CachedComparisonAnalysis{
			ComparisonAnalysis: *analysis,
			WorkoutIDA:         idA,
			WorkoutIDB:         idB,
			Model:              cfg.Model,
			CreatedAt:          now,
			Cached:             false,
		}
		writeJSON(w, http.StatusOK, map[string]any{"analysis": result})
	}
}

// buildComparisonAnalysisPrompt constructs the prompt for AI-powered comparison analysis.
func buildComparisonAnalysisPrompt(wA, wB *Workout, comparison *ComparisonResult) string {
	var sb strings.Builder

	sb.WriteString("Compare these two workouts and provide coaching insights. Respond with JSON only, no markdown.\n\n")

	// Workout A summary.
	sb.WriteString("=== Workout A ===\n")
	writeWorkoutSummaryForPrompt(&sb, wA)

	// Workout B summary.
	sb.WriteString("\n=== Workout B ===\n")
	writeWorkoutSummaryForPrompt(&sb, wB)

	// Include structural comparison results if available.
	if comparison != nil && comparison.Compatible && comparison.Summary != nil {
		sb.WriteString("\n=== Lap-by-Lap Comparison ===\n")
		sb.WriteString("| Pair | Lap A | Lap B | HR A | HR B | HR Δ | Pace A | Pace B | Pace Δ |\n")
		sb.WriteString("|------|-------|-------|------|------|------|--------|--------|--------|\n")
		for _, d := range comparison.LapDeltas {
			paceAStr := formatPace(d.PaceA)
			paceBStr := formatPace(d.PaceB)
			paceDeltaStr := formatPaceDelta(d.PaceDelta)
			fmt.Fprintf(&sb, "| %d | %d | %d | %d | %d | %+d | %s | %s | %s |\n",
				d.LapNumber, d.LapNumberA, d.LapNumberB,
				d.AvgHRA, d.AvgHRB, d.HRDelta,
				paceAStr, paceBStr, paceDeltaStr)
		}
		fmt.Fprintf(&sb, "\nOverall: avg HR delta %+.1f bpm, avg pace delta %+.1f s/km\n", comparison.Summary.AvgHRDelta, comparison.Summary.AvgPaceDelta)
		fmt.Fprintf(&sb, "Structural verdict: %s\n", comparison.Summary.Verdict)
	} else if comparison != nil && !comparison.Compatible {
		fmt.Fprintf(&sb, "\nNote: workouts are not structurally compatible for lap-by-lap comparison (%s). Compare overall metrics instead.\n", comparison.Reason)
	}

	sb.WriteString(`
Respond with this exact JSON structure:
{
  "summary": "2-3 sentence overall comparison of the two workouts",
  "strengths": ["strength 1", "strength 2"],
  "weaknesses": ["area for improvement 1", "area for improvement 2"],
  "observations": ["notable observation 1", "notable observation 2"]
}

Guidelines:
- "strengths" should highlight positive trends or strong performances across the two workouts
- "weaknesses" should identify areas where performance declined or could improve
- "observations" should note interesting patterns, differences, or contextual factors
- Be specific — reference actual numbers (HR, pace, duration) from the data
- Keep each bullet point concise (1-2 sentences)`)

	return sb.String()
}

// writeWorkoutSummaryForPrompt writes a formatted workout summary into the string builder.
func writeWorkoutSummaryForPrompt(sb *strings.Builder, w *Workout) {
	fmt.Fprintf(sb, "Date: %s\n", w.StartedAt)
	fmt.Fprintf(sb, "Sport: %s\n", w.Sport)
	if w.Title != "" {
		fmt.Fprintf(sb, "Title: %s\n", w.Title)
	}
	dur := formatDurationSecs(w.DurationSeconds)
	dist := fmt.Sprintf("%.2f km", w.DistanceMeters/1000)
	fmt.Fprintf(sb, "Duration: %s, Distance: %s\n", dur, dist)
	if w.AvgHeartRate > 0 {
		fmt.Fprintf(sb, "Avg HR: %d bpm, Max HR: %d bpm\n", w.AvgHeartRate, w.MaxHeartRate)
	}
	if w.AvgPaceSecPerKm > 0 {
		fmt.Fprintf(sb, "Avg Pace: %s /km\n", formatPace(w.AvgPaceSecPerKm))
	}
	if w.AvgCadence > 0 {
		fmt.Fprintf(sb, "Avg Cadence: %d spm\n", w.AvgCadence)
	}
	if w.AscentMeters > 0 {
		fmt.Fprintf(sb, "Elevation: +%.0fm / -%.0fm\n", w.AscentMeters, w.DescentMeters)
	}

	if len(w.Laps) > 0 {
		sb.WriteString("\nLaps:\n")
		sb.WriteString("| # | Duration | Distance | Avg HR | Max HR | Pace |\n")
		sb.WriteString("|---|----------|----------|--------|--------|------|\n")
		for _, lap := range w.Laps {
			lapDur := formatDurationSecs(int(lap.DurationSeconds))
			lapDist := fmt.Sprintf("%.2f km", lap.DistanceMeters/1000)
			lapPace := "--:--"
			if lap.AvgPaceSecPerKm > 0 {
				lapPace = formatPace(lap.AvgPaceSecPerKm)
			}
			hrStr := "-"
			if lap.AvgHeartRate > 0 {
				hrStr = strconv.Itoa(lap.AvgHeartRate)
			}
			maxHRStr := "-"
			if lap.MaxHeartRate > 0 {
				maxHRStr = strconv.Itoa(lap.MaxHeartRate)
			}
			fmt.Fprintf(sb, "| %d | %s | %s | %s | %s | %s /km |\n",
				lap.LapNumber, lapDur, lapDist, hrStr, maxHRStr, lapPace)
		}
	}
}

// formatPace formats pace in seconds per km as m:ss.
func formatPace(secPerKm float64) string {
	if secPerKm <= 0 {
		return "--:--"
	}
	m := int(secPerKm) / 60
	s := int(secPerKm) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatPaceDelta formats a pace delta with sign as +m:ss or -m:ss.
func formatPaceDelta(delta float64) string {
	sign := "+"
	if delta < 0 {
		sign = "-"
		delta = -delta
	}
	m := int(delta) / 60
	s := int(delta) % 60
	return fmt.Sprintf("%s%d:%02d", sign, m, s)
}

// parseComparisonAnalysisResponse extracts ComparisonAnalysis from the Claude response text.
func parseComparisonAnalysisResponse(raw string) (*ComparisonAnalysis, error) {
	// Claude may wrap the JSON in markdown code fences.
	cleaned := raw
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}

	var analysis ComparisonAnalysis
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		return nil, fmt.Errorf("parse comparison analysis JSON: %w", err)
	}
	analysis.normalize()
	return &analysis, nil
}
