package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// GetTrainingLoadHandler handles GET /api/training/load?weeks=N.
// It returns weekly load records, acute load, chronic load, ACR, and training status.
func GetTrainingLoadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		nWeeks := 12
		if q := r.URL.Query().Get("weeks"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 52 {
				nWeeks = n
			}
		}

		// Fetch enough weekly records for classification (need at least 4 for chronic baseline).
		fetchN := nWeeks
		if fetchN < 6 {
			fetchN = 6
		}
		loads, err := GetWeeklyLoads(db, user.ID, fetchN)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load training data"})
			return
		}

		// Compute acute/chronic load and ACR using the existing function for consistency.
		// ComputeACR uses a 7-day sum for acute load and a 28-day total divided by 4 for chronic load.
		acr, acute, chronic, err := ComputeACR(db, user.ID, time.Now().UTC())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to compute load metrics"})
			return
		}

		status := ClassifyTrainingStatus(loads, acr)

		// Trim to requested number of weeks.
		trimmed := loads
		if len(trimmed) > nWeeks {
			trimmed = trimmed[:nWeeks]
		}
		if trimmed == nil {
			trimmed = []WeeklyLoad{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"weeks":        trimmed,
			"acute_load":   math.Round(acute*100) / 100,
			"chronic_load": math.Round(chronic*100) / 100,
			"acr":          acr,
			"status":       status,
		})
	}
}

// AnalyzeTrainingSummaryHandler handles POST /api/training/summary/analyze.
// Request body: {"period": "week", "date": "2024-03-18"}
// Returns a structured AI-generated training summary, cached in training_summaries.
func AnalyzeTrainingSummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		var req struct {
			Period string `json:"period"`
			Date   string `json:"date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Period == "" {
			req.Period = "week"
		}
		if req.Date == "" {
			req.Date = time.Now().UTC().Format("2006-01-02")
		}

		periodStart, err := computePeriodStart(req.Period, req.Date)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		periodStartStr := periodStart.Format("2006-01-02")

		// Check cache.
		cached, cacheErr := getTrainingSummaryByPeriod(db, user.ID, req.Period, periodStartStr)
		if cacheErr != nil && cacheErr != sql.ErrNoRows {
			log.Printf("AnalyzeTrainingSummaryHandler: cache lookup error for user %d: %v", user.ID, cacheErr)
		}
		if cached != nil && cached.ResponseJSON != "" {
			respJSON, decErr := encryption.DecryptField(cached.ResponseJSON)
			if decErr != nil {
				// Decrypt failure means the stored value is unusable — fall through to regeneration.
				// Do NOT pass raw ciphertext downstream as JSON.
				log.Printf("AnalyzeTrainingSummaryHandler: decrypt response failed for user %d, re-generating: %v", user.ID, decErr)
			} else {
				var analysis SummaryAnalysis
				if unmarshalErr := json.Unmarshal([]byte(respJSON), &analysis); unmarshalErr == nil {
					analysis.normalize()
					writeJSON(w, http.StatusOK, SummaryAnalysisResponse{
						Period:      req.Period,
						PeriodStart: periodStartStr,
						Status:      cached.Status,
						ACR:         cached.ACR,
						AcuteLoad:   math.Round(cached.AcuteLoad*100) / 100,
						ChronicLoad: math.Round(cached.ChronicLoad*100) / 100,
						Analysis:    analysis,
						Model:       cached.Model,
						CreatedAt:   cached.UpdatedAt,
						Cached:      true,
					})
					return
				}
				log.Printf("AnalyzeTrainingSummaryHandler: cached response_json is invalid JSON for user %d, re-generating", user.ID)
			}
		}

		// Load Claude config.
		cfg, cfgErr := LoadClaudeConfig(db, user.ID)
		if cfgErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: failed to load claude config for user %d: %v", user.ID, cfgErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Compute load/ACR/status as of the last inclusive day of the period.
		// periodEndFor returns an exclusive end, so subtract one day to get the
		// last day of the period (ComputeACR treats asOfDate as inclusive).
		periodEnd := periodEndFor(req.Period, periodStart)
		asOf := periodEnd.AddDate(0, 0, -1)
		acr, acute, chronic, loadErr := ComputeACR(db, user.ID, asOf)
		if loadErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to compute load metrics"})
			return
		}
		// Fetch recent weekly loads and constrain them to the analysis period for status classification.
		loads, loadsErr := GetWeeklyLoads(db, user.ID, 6)
		if loadsErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load weekly data"})
			return
		}
		var filteredLoads []WeeklyLoad
		for _, wl := range loads {
			weekStart, parseErr := time.Parse("2006-01-02", wl.WeekStart)
			if parseErr != nil {
				continue
			}
			// Only consider weeks that end on or before the analysis period end.
			if !weekStart.AddDate(0, 0, 7).After(periodEnd) {
				filteredLoads = append(filteredLoads, wl)
			}
		}
		status := ClassifyTrainingStatus(filteredLoads, acr)

		// Build prompt context.
		profile := BuildUserTrainingProfile(db, user.ID)
		summaries, summariesErr := WeeklySummaries(db, user.ID)
		if summariesErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: failed to load weekly summaries for user %d: %v", user.ID, summariesErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load weekly summaries"})
			return
		}
		sportDist, sportDistErr := getWorkoutSportDistribution(db, user.ID, periodStart, periodEnd)
		if sportDistErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: failed to load workout sport distribution for user %d: %v", user.ID, sportDistErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load sport distribution"})
			return
		}

		// Restrict weekly summaries to the target period plus the 4 prior weeks,
		// all ending at the analyzed periodEnd. This avoids including unrelated
		// recent weeks when analyzing a backdated period.
		windowStart := periodStart.AddDate(0, 0, -28)
		var windowSummaries []WeeklySummary
		for _, s := range summaries {
			t, parseErr := time.Parse("2006-01-02", s.WeekStart)
			if parseErr != nil {
				continue
			}
			if !t.Before(windowStart) && !t.After(periodEnd) {
				windowSummaries = append(windowSummaries, s)
			}
		}

		prompt := buildSummaryAnalysisPrompt(profile.Block, req.Period, periodStartStr, windowSummaries, sportDist, status, acr, acute, chronic)

		// Call Claude.
		raw, claudeErr := runPromptFunc(r.Context(), cfg, prompt)
		if claudeErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: Claude error for user %d: %v", user.ID, claudeErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate analysis"})
			return
		}

		analysis, parseErr := parseSummaryAnalysisResponse(raw)
		if parseErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: parse error for user %d: %v", user.ID, parseErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse AI response"})
			return
		}

		// Encrypt and cache.
		now := time.Now().UTC().Format(time.RFC3339)
		respJSON, _ := json.Marshal(analysis)
		encPrompt, encPromptErr := encryption.EncryptField(prompt)
		if encPromptErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: encrypt prompt error for user %d: %v", user.ID, encPromptErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt AI context"})
			return
		}
		encResp, encRespErr := encryption.EncryptField(string(respJSON))
		if encRespErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: encrypt response error for user %d: %v", user.ID, encRespErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt AI context"})
			return
		}

		s := TrainingSummary{
			UserID:       user.ID,
			Period:       req.Period,
			WeekStart:    periodStartStr,
			Status:       status,
			ACR:          acr,
			AcuteLoad:    acute,
			ChronicLoad:  chronic,
			Prompt:       encPrompt,
			ResponseJSON: encResp,
			Model:        cfg.Model,
			UpdatedAt:    now,
		}
		if upsertErr := UpsertTrainingSummaryAnalysis(db, s); upsertErr != nil {
			log.Printf("AnalyzeTrainingSummaryHandler: failed to cache summary for user %d: %v", user.ID, upsertErr)
		}

		writeJSON(w, http.StatusOK, SummaryAnalysisResponse{
			Period:      req.Period,
			PeriodStart: periodStartStr,
			Status:      status,
			ACR:         acr,
			AcuteLoad:   math.Round(acute*100) / 100,
			ChronicLoad: math.Round(chronic*100) / 100,
			Analysis:    *analysis,
			Model:       cfg.Model,
			CreatedAt:   now,
			Cached:      false,
		})
	}
}

// computePeriodStart returns the start date for a given period containing the provided date.
// Supported periods: "week" (Monday), "month" (1st of month).
func computePeriodStart(period, dateStr string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		date, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD format", dateStr)
		}
	}
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	switch period {
	case "week":
		// Find the Monday of the week.
		offset := int(date.Weekday()) - 1
		if offset < 0 {
			offset = 6 // Sunday wraps to 6 days back
		}
		return date.AddDate(0, 0, -offset), nil
	case "month":
		return time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported period %q: use 'week' or 'month'", period)
	}
}

// periodEndFor returns the exclusive end time for a period starting at start.
func periodEndFor(period string, start time.Time) time.Time {
	switch period {
	case "month":
		return start.AddDate(0, 1, 0)
	default: // "week"
		return start.AddDate(0, 0, 7)
	}
}

// getWorkoutSportDistribution returns workout counts and total durations grouped by sport
// for workouts in [start, end).
func getWorkoutSportDistribution(db *sql.DB, userID int64, start, end time.Time) ([]SportDistribution, error) {
	rows, err := db.Query(`
		SELECT sport, COUNT(*) AS cnt, SUM(duration_seconds) AS total_dur
		FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at < ?
		GROUP BY sport
		ORDER BY cnt DESC, sport ASC`,
		userID,
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SportDistribution
	for rows.Next() {
		var d SportDistribution
		if err := rows.Scan(&d.Sport, &d.Count, &d.TotalDuration); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// buildSummaryAnalysisPrompt assembles the Claude prompt for a training period summary.
func buildSummaryAnalysisPrompt(
	userProfileBlock, period, periodStart string,
	summaries []WeeklySummary,
	sportDist []SportDistribution,
	status TrainingStatus,
	acr *float64,
	acute, chronic float64,
) string {
	var sb strings.Builder
	sb.WriteString("Analyze this training period and provide structured coaching feedback. Respond with JSON only, no markdown.\n\n")

	if userProfileBlock != "" {
		sb.WriteString(userProfileBlock)
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "Period: %s (%s)\n", periodStart, period)
	fmt.Fprintf(&sb, "Training Status: %s\n", status)
	if acr != nil {
		fmt.Fprintf(&sb, "ACR: %.2f (acute=%.1f, chronic=%.1f)\n", *acr, acute, chronic)
	} else {
		fmt.Fprintf(&sb, "Acute Load: %.1f, Chronic Load: %.1f (insufficient history for ACR)\n", acute, chronic)
	}

	// Include up to 5 recent weekly summaries (target period + 4 prior weeks).
	if len(summaries) > 0 {
		limit := 5
		if len(summaries) < limit {
			limit = len(summaries)
		}
		sb.WriteString("\nWeekly Training Summary (most recent weeks, newest first):\n")
		sb.WriteString("| Week | Duration | Distance | Workouts | Avg HR |\n")
		sb.WriteString("|------|----------|----------|----------|--------|\n")
		for _, s := range summaries[:limit] {
			hrStr := "--"
			if s.AvgHeartRate > 0 {
				hrStr = fmt.Sprintf("%.0f", s.AvgHeartRate)
			}
			fmt.Fprintf(&sb, "| %s | %s | %.1f km | %d | %s |\n",
				s.WeekStart, formatDurationSecs(s.TotalDuration), s.TotalDistance/1000, s.WorkoutCount, hrStr)
		}
	}

	if len(sportDist) > 0 {
		sb.WriteString("\nSport Distribution (this period):\n")
		sb.WriteString("| Sport | Count | Total Duration |\n")
		sb.WriteString("|-------|-------|----------------|\n")
		for _, d := range sportDist {
			fmt.Fprintf(&sb, "| %s | %d | %s |\n", d.Sport, d.Count, formatDurationSecs(d.TotalDuration))
		}
	}

	sb.WriteString(`
Respond with this exact JSON structure:
{
  "overview": "1-2 sentence overall assessment of this training period",
  "key_insights": ["insight 1", "insight 2"],
  "strengths": ["strength 1"],
  "concerns": ["concern 1"],
  "recommendations": ["recommendation 1", "recommendation 2"],
  "risk_flags": ["specific risk if ACR > 1.3 or rapid load increase — empty array if none"]
}`)

	return sb.String()
}

// parseSummaryAnalysisResponse extracts a SummaryAnalysis from the Claude response text.
func parseSummaryAnalysisResponse(raw string) (*SummaryAnalysis, error) {
	cleaned := raw
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}

	var analysis SummaryAnalysis
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		return nil, fmt.Errorf("parse summary analysis JSON: %w", err)
	}
	analysis.normalize()
	return &analysis, nil
}
