package training

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/hrzones"
	"github.com/Robin831/Hytte/internal/lactate"
)

// UserTrainingProfile holds a user's training profile block (for prompt injection) and key
// parsed values derived from a single preferences load.
type UserTrainingProfile struct {
	Block       string
	ThresholdHR int
	HasGoalRace bool
}

// BuildUserTrainingProfile loads user preferences once and returns the full profile.
// Use this in handlers so that ThresholdHR is available without a second DB round-trip.
func BuildUserTrainingProfile(db *sql.DB, userID int64) UserTrainingProfile {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("BuildUserTrainingProfile: failed to load preferences for user %d: %v", userID, err)
		return UserTrainingProfile{}
	}
	block, thresholdHR, hasGoalRace := buildUserProfileFromPrefs(prefs, db, userID)
	return UserTrainingProfile{Block: block, ThresholdHR: thresholdHR, HasGoalRace: hasGoalRace}
}

// BuildUserProfileBlock builds a structured text block with the user's personal
// training profile for injection into AI prompts. Returns an empty string if no
// useful profile data is available.
func BuildUserProfileBlock(db *sql.DB, userID int64) string {
	return BuildUserTrainingProfile(db, userID).Block
}

// buildUserProfileFromPrefs is the internal implementation that accepts already-loaded prefs.
// Returns (block, thresholdHR, hasGoalRace).
func buildUserProfileFromPrefs(prefs map[string]string, db *sql.DB, userID int64) (string, int, bool) {
	// Parse preference values.
	maxHR := parseIntPref(prefs, "max_hr")
	restingHR := parseIntPref(prefs, "resting_hr")
	thresholdHR := parseIntPref(prefs, "threshold_hr")
	thresholdPace := parseIntPref(prefs, "threshold_pace") // sec/km
	easyPaceMin := parseIntPref(prefs, "easy_pace_min")    // sec/km
	easyPaceMax := parseIntPref(prefs, "easy_pace_max")    // sec/km

	// Parse goal race preferences.
	goalRaceName := prefs["goal_race_name"]
	goalRaceDate := prefs["goal_race_date"]
	goalRaceDistance := prefs["goal_race_distance"]
	goalRaceTargetTime := prefs["goal_race_target_time"]
	hasGoal := goalRaceName != "" || goalRaceDate != "" || goalRaceDistance != "" || goalRaceTargetTime != ""

	// Check for user-stored zone boundaries (highest priority — set explicitly by the user
	// via the HR Zones settings UI). These take precedence over lactate/max-HR derived zones.
	// ParseZoneBoundaries applies the same validation rules as the settings UI (5 zones,
	// monotonic boundaries) and returns zones sorted by zone number for stable output.
	var storedZoneBoundaries []hrzones.ZoneBoundary
	if raw, ok := prefs["zone_boundaries"]; ok && raw != "" {
		zones, parseErr := hrzones.ParseZoneBoundaries(raw)
		if parseErr != nil {
			log.Printf("buildUserProfileFromPrefs: invalid zone_boundaries for user %d: %v", userID, parseErr)
		} else {
			storedZoneBoundaries = zones
		}
	}

	// Try to load zones from the most recent lactate test.
	// Skipped when stored zone boundaries are present — they are the highest-priority source
	// and the lactate query/computation is unnecessary when custom zones are already set.
	var zonesResult *lactate.ZonesResult
	var zonesSource string
	var thresholdHRSource string

	if len(storedZoneBoundaries) == 0 {
		latestTest, err := getLatestLactateTest(db, userID)
		if err != nil {
			log.Printf("buildUserProfileFromPrefs: failed to query latest lactate test for user %d: %v", userID, err)
		}

		if latestTest != nil && len(latestTest.Stages) >= 2 {
			thresholds := lactate.CalculateThresholds(latestTest.Stages)
			var best *lactate.ThresholdResult
			for i := range thresholds {
				if thresholds[i].Valid {
					best = &thresholds[i]
					break
				}
			}

			if best != nil {
				// Auto-populate threshold values from lactate test if not set in preferences.
				if thresholdHR == 0 && best.HeartRateBpm > 0 {
					thresholdHR = best.HeartRateBpm
					thresholdHRSource = "from lactate test"
				}
				if thresholdPace == 0 && best.SpeedKmh > 0 {
					// Convert speed (km/h) to pace (sec/km): 3600 / speed.
					thresholdPace = int(math.Round(3600.0 / best.SpeedKmh))
				}

				// Derive zone thresholds from preferences first (if set), falling back to
				// lactate-derived values only when missing.
				zoneThresholdHR := thresholdHR
				zoneThresholdSpeed := 0.0
				if thresholdPace > 0 {
					// Convert pace (sec/km) to speed (km/h): 3600 / pace.
					zoneThresholdSpeed = 3600.0 / float64(thresholdPace)
				} else if best.SpeedKmh > 0 {
					zoneThresholdSpeed = best.SpeedKmh
				}

				zonesResult = lactate.CalculateZones(lactate.ZoneSystemOlympiatoppen, zoneThresholdSpeed, zoneThresholdHR, maxHR)
				zonesSource = "from lactate test"
			}
		}

		// If no lactate data but max HR is known, estimate zones from max HR percentages.
		if zonesResult == nil && maxHR > 0 {
			zonesResult = buildMaxHRZones(maxHR)
			zonesSource = "estimated from max HR"
		}
	}

	// Nothing useful to show — omit the block entirely.
	if maxHR == 0 && thresholdHR == 0 && zonesResult == nil && len(storedZoneBoundaries) == 0 && !hasGoal {
		return "", 0, false
	}

	var sb strings.Builder
	sb.WriteString("User Profile:\n")

	if maxHR > 0 {
		fmt.Fprintf(&sb, "- Max HR: %d bpm\n", maxHR)
	}
	if restingHR > 0 {
		fmt.Fprintf(&sb, "- Resting HR: %d bpm\n", restingHR)
	}
	if thresholdHR > 0 {
		if thresholdHRSource != "" {
			fmt.Fprintf(&sb, "- Threshold HR: %d bpm (%s)\n", thresholdHR, thresholdHRSource)
		} else {
			fmt.Fprintf(&sb, "- Threshold HR: %d bpm\n", thresholdHR)
		}
	}
	if thresholdPace > 0 {
		fmt.Fprintf(&sb, "- Threshold Pace: %d:%02d/km\n", thresholdPace/60, thresholdPace%60)
	}
	if easyPaceMin > 0 && easyPaceMax > 0 {
		fmt.Fprintf(&sb, "- Easy Pace Range: %d:%02d-%d:%02d/km\n",
			easyPaceMin/60, easyPaceMin%60, easyPaceMax/60, easyPaceMax%60)
	} else if easyPaceMin > 0 {
		fmt.Fprintf(&sb, "- Easy Pace Min: %d:%02d/km\n", easyPaceMin/60, easyPaceMin%60)
	} else if easyPaceMax > 0 {
		fmt.Fprintf(&sb, "- Easy Pace Max: %d:%02d/km\n", easyPaceMax/60, easyPaceMax%60)
	}

	if len(storedZoneBoundaries) > 0 {
		// Stored zone boundaries have been set explicitly by the user — use them directly.
		sb.WriteString("- Training Zones (custom):\n")
		for _, z := range storedZoneBoundaries {
			fmt.Fprintf(&sb, "  Zone %d (%s): %d-%d bpm\n", z.Zone, hrzones.ZoneName(z.Zone), z.MinBPM, z.MaxBPM)
		}
	} else if zonesResult != nil && len(zonesResult.Zones) > 0 {
		zoneLabel := "Olympiatoppen"
		if zonesResult.System == lactate.ZoneSystemNorwegian {
			zoneLabel = "Norwegian"
		}
		if zonesSource != "" {
			fmt.Fprintf(&sb, "- Training Zones (%s, %s):\n", zoneLabel, zonesSource)
		} else {
			fmt.Fprintf(&sb, "- Training Zones (%s):\n", zoneLabel)
		}
		for _, z := range zonesResult.Zones {
			fmt.Fprintf(&sb, "  %s\n", formatZoneLine(z))
		}
	}

	if hasGoal {
		sb.WriteString("Goal Race:\n")
		if goalRaceName != "" {
			fmt.Fprintf(&sb, "- Event: %s\n", goalRaceName)
		}
		if goalRaceDate != "" {
			now := time.Now().UTC()
			raceTime, err := time.ParseInLocation("2006-01-02", goalRaceDate, time.UTC)
			if err == nil && now.Before(raceTime) {
				weeksUntil := int(raceTime.Sub(now).Hours()) / (24 * 7)
				fmt.Fprintf(&sb, "- Date: %s (%d weeks away)\n", goalRaceDate, weeksUntil)
			} else {
				fmt.Fprintf(&sb, "- Date: %s\n", goalRaceDate)
			}
		}
		if goalRaceDistance != "" {
			fmt.Fprintf(&sb, "- Distance: %s km\n", goalRaceDistance)
		}
		if goalRaceTargetTime != "" {
			fmt.Fprintf(&sb, "- Target Time: %s\n", goalRaceTargetTime)
		}
	}

	return sb.String(), thresholdHR, hasGoal
}

// parseIntPref reads a preference key as a positive integer, returning 0 if absent or invalid.
func parseIntPref(prefs map[string]string, key string) int {
	v, ok := prefs[key]
	if !ok || v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// getLatestLactateTest returns the most recent lactate test with stages for the user,
// or nil if no tests exist.
func getLatestLactateTest(db *sql.DB, userID int64) (*lactate.Test, error) {
	tests, err := lactate.List(db, userID)
	if err != nil {
		return nil, err
	}
	if len(tests) == 0 {
		return nil, nil
	}
	// List returns tests ordered by date DESC — take the first (most recent).
	return lactate.GetByID(db, tests[0].ID, userID)
}

// buildMaxHRZones creates approximate training zones based on standard max HR
// percentages. Used as a fallback when no lactate test is available.
func buildMaxHRZones(maxHR int) *lactate.ZonesResult {
	zones := []lactate.TrainingZone{
		{Zone: 1, Name: "I1 - Recovery", MinHR: 0, MaxHR: int(math.Round(float64(maxHR) * 0.60))},
		{Zone: 2, Name: "I2 - Endurance", MinHR: int(math.Round(float64(maxHR) * 0.60)), MaxHR: int(math.Round(float64(maxHR) * 0.70))},
		{Zone: 3, Name: "I3 - Tempo", MinHR: int(math.Round(float64(maxHR) * 0.70)), MaxHR: int(math.Round(float64(maxHR) * 0.80))},
		{Zone: 4, Name: "I4 - Threshold", MinHR: int(math.Round(float64(maxHR) * 0.80)), MaxHR: int(math.Round(float64(maxHR) * 0.90))},
		{Zone: 5, Name: "I5 - VO2max", MinHR: int(math.Round(float64(maxHR) * 0.90)), MaxHR: maxHR},
	}
	return &lactate.ZonesResult{
		System: lactate.ZoneSystemOlympiatoppen,
		MaxHR:  maxHR,
		Zones:  zones,
	}
}

// formatZoneLine formats a single training zone as a human-readable string.
func formatZoneLine(z lactate.TrainingZone) string {
	zoneName := fmt.Sprintf("Zone %d (%s)", z.Zone, z.Name)
	hrRange := fmt.Sprintf("%d-%d bpm", z.MinHR, z.MaxHR)

	if z.MaxSpeedKmh <= 0 {
		// No speed data (max HR-based zones): show HR range only.
		return zoneName + ": " + hrRange
	}

	if z.MinSpeedKmh <= 0 {
		// Zone 1: speeds start from zero — "slower than X".
		return zoneName + ": " + hrRange + " / slower than " + formatPaceFromSpeed(z.MaxSpeedKmh) + "/km"
	}

	// Range zone: show pace range from faster boundary to slower boundary.
	fasterPace := formatPaceFromSpeed(z.MaxSpeedKmh)
	slowerPace := formatPaceFromSpeed(z.MinSpeedKmh)
	return zoneName + ": " + hrRange + " / " + fasterPace + "-" + slowerPace + "/km"
}

// formatPaceFromSpeed converts a speed in km/h to a pace string (m:ss/km).
func formatPaceFromSpeed(speedKmh float64) string {
	if speedKmh <= 0 {
		return "--:--"
	}
	totalSecs := int(math.Round(3600.0 / speedKmh))
	return fmt.Sprintf("%d:%02d", totalSecs/60, totalSecs%60)
}

// BuildHistoricalContext builds a historical context block for AI prompts,
// including weekly training summaries, similar past workouts, recent trends,
// workout type distribution, and race predictions.
// Returns an empty string if no historical data is available.
func BuildHistoricalContext(db *sql.DB, userID int64, workout *Workout) string {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("BuildHistoricalContext: failed to load preferences for user %d: %v", userID, err)
		prefs = map[string]string{}
	}

	nWeeks := 8
	if n := parseIntPref(prefs, "ai_trend_weeks"); n > 0 {
		nWeeks = n
	}

	summaries, err := WeeklySummaries(db, userID)
	if err != nil {
		log.Printf("BuildHistoricalContext: failed to load weekly summaries for user %d: %v", userID, err)
	}

	groups, err := GetProgression(db, userID)
	if err != nil {
		log.Printf("BuildHistoricalContext: failed to load progression for user %d: %v", userID, err)
	}

	dist, distErr := GetWorkoutTypeDistribution(db, userID, nWeeks)
	if distErr != nil {
		log.Printf("BuildHistoricalContext: failed to load type distribution for user %d: %v", userID, distErr)
	}

	var preds *RacePredictions
	if thresholdWorkout, twErr := FindBestThresholdWorkout(db, userID); twErr == nil && thresholdWorkout != nil {
		preds = PredictRaceTimes(0, thresholdWorkout.AvgPaceSecPerKm)
	}

	if len(summaries) == 0 && len(groups) == 0 && len(dist) == 0 && preds == nil {
		return ""
	}

	var sb strings.Builder

	if len(summaries) > 0 {
		writeWeeklySummarySection(&sb, summaries, nWeeks)
	}

	if len(groups) > 0 && workout != nil {
		writeSimilarWorkoutsSection(&sb, groups, workout)
	}

	if len(summaries) >= 4 {
		writeRecentTrendsSection(&sb, summaries)
	}

	if len(dist) > 0 {
		writeTypeDistributionSection(&sb, dist)
	}

	if preds != nil {
		writeRacePredictionsSection(&sb, preds)
	}

	return sb.String()
}

// writeWeeklySummarySection formats the last nWeeks of weekly training data as a table.
func writeWeeklySummarySection(sb *strings.Builder, summaries []WeeklySummary, nWeeks int) {
	limit := nWeeks
	if limit > len(summaries) {
		limit = len(summaries)
	}

	// Mark the current week as incomplete so Claude doesn't draw conclusions
	// from a partial week's data (e.g. flagging "volume drop" on Tuesday).
	now := time.Now()
	weekday := int(now.Weekday())
	daysBack := (weekday - 1 + 7) % 7
	currentMonday := now.AddDate(0, 0, -daysBack).Format("2006-01-02")

	sb.WriteString("Weekly Training Summary:\n")
	sb.WriteString("| Week | Duration | Distance | Workouts | Avg HR | Note |\n")
	sb.WriteString("|------|----------|----------|----------|--------|------|\n")

	for _, s := range summaries[:limit] {
		hrStr := "--"
		if s.AvgHeartRate > 0 {
			hrStr = fmt.Sprintf("%.0f", s.AvgHeartRate)
		}
		distStr := fmt.Sprintf("%.1f km", s.TotalDistance/1000)
		note := ""
		if s.WeekStart == currentMonday {
			note = "INCOMPLETE — week in progress, do NOT compare volume against completed weeks"
		}
		fmt.Fprintf(sb, "| %s | %s | %s | %d | %s | %s |\n",
			s.WeekStart, formatDurationSecs(s.TotalDuration), distStr, s.WorkoutCount, hrStr, note)
	}
	sb.WriteString("\n")
}

// writeSimilarWorkoutsSection finds progression groups matching the current workout
// (same sport, lap count within ±1) and formats them as a table with the current
// workout marked and per-entry deltas computed.
func writeSimilarWorkoutsSection(sb *strings.Builder, groups []ProgressionGroup, workout *Workout) {
	lapCount := len(workout.Laps)

	var matched []ProgressionGroup
	for _, g := range groups {
		if g.Sport != workout.Sport {
			continue
		}
		diff := g.LapCount - lapCount
		if diff < 0 {
			diff = -diff
		}
		if diff <= 1 {
			matched = append(matched, g)
		}
	}

	if len(matched) == 0 {
		return
	}

	sb.WriteString("Similar Past Workouts:\n")
	for _, g := range matched {
		label := g.Sport
		if g.Tag != "" {
			label = fmt.Sprintf("%s (%s)", g.Tag, g.Sport)
		}
		fmt.Fprintf(sb, "Group: %s, %d laps\n", label, g.LapCount)
		sb.WriteString("| Date | Avg HR | Avg Pace | ΔHR | ΔPace (s) |\n")
		sb.WriteString("|------|--------|----------|-----|----------|\n")

		for i, p := range g.Workouts {
			date := p.Date
			if len(date) > 10 {
				date = date[:10]
			}
			marker := " "
			if p.WorkoutID == workout.ID {
				marker = "→"
			}

			hrStr := "--"
			if p.AvgHR > 0 {
				hrStr = fmt.Sprintf("%.0f", p.AvgHR)
			}
			paceStr := "--"
			if p.AvgPace > 0 {
				rounded := int(math.Round(p.AvgPace))
				paceStr = fmt.Sprintf("%d:%02d", rounded/60, rounded%60)
			}

			deltaHR := "--"
			deltaPace := "--"
			if i > 0 {
				prev := g.Workouts[i-1]
				if p.AvgHR > 0 && prev.AvgHR > 0 {
					d := p.AvgHR - prev.AvgHR
					if d >= 0 {
						deltaHR = fmt.Sprintf("+%.0f", d)
					} else {
						deltaHR = fmt.Sprintf("%.0f", d)
					}
				}
				if p.AvgPace > 0 && prev.AvgPace > 0 {
					d := math.Round(p.AvgPace) - math.Round(prev.AvgPace)
					if d >= 0 {
						deltaPace = fmt.Sprintf("+%.0f", d)
					} else {
						deltaPace = fmt.Sprintf("%.0f", d)
					}
				}
			}

			fmt.Fprintf(sb, "|%s%s | %s | %s | %s | %s |\n",
				marker, date, hrStr, paceStr, deltaHR, deltaPace)
		}
		sb.WriteString("\n")
	}
}

// writeRecentTrendsSection computes volume, intensity, and frequency trends
// by comparing the last 2 weeks against the prior 2 weeks.
// Requires at least 4 summaries so both comparison windows are fully populated.
func writeRecentTrendsSection(sb *strings.Builder, summaries []WeeklySummary) {
	// summaries are DESC (most recent first).
	// Skip the current incomplete week — comparing a partial week against
	// completed weeks always shows a misleading "decreasing" trend.
	now := time.Now()
	weekday := int(now.Weekday())
	daysBack := (weekday - 1 + 7) % 7 // Monday=0
	currentMonday := now.AddDate(0, 0, -daysBack).Format("2006-01-02")
	if len(summaries) > 0 && summaries[0].WeekStart == currentMonday {
		summaries = summaries[1:]
	}

	n := len(summaries)
	if n < 4 {
		return
	}

	last2End := 2
	if last2End > n {
		last2End = n
	}
	prior2Start := last2End
	prior2End := prior2Start + 2
	if prior2End > n {
		prior2End = n
	}

	last2 := summaries[:last2End]
	prior2 := summaries[prior2Start:prior2End]

	var lastDist, priorDist float64
	for _, s := range last2 {
		lastDist += s.TotalDistance
	}
	for _, s := range prior2 {
		priorDist += s.TotalDistance
	}

	var lastHRSum, priorHRSum float64
	var lastHRCount, priorHRCount int
	for _, s := range last2 {
		if s.AvgHeartRate > 0 {
			lastHRSum += s.AvgHeartRate
			lastHRCount++
		}
	}
	for _, s := range prior2 {
		if s.AvgHeartRate > 0 {
			priorHRSum += s.AvgHeartRate
			priorHRCount++
		}
	}

	var lastFreq, priorFreq int
	for _, s := range last2 {
		lastFreq += s.WorkoutCount
	}
	for _, s := range prior2 {
		priorFreq += s.WorkoutCount
	}

	sb.WriteString("Recent Trends (last 4 weeks):\n")

	volTrend := trendDirection(lastDist, priorDist)
	fmt.Fprintf(sb, "- Volume: %s (%.1f km last 2w vs %.1f km prior 2w)\n",
		volTrend, lastDist/1000, priorDist/1000)

	if lastHRCount > 0 || priorHRCount > 0 {
		lastHRAvg, priorHRAvg := 0.0, 0.0
		if lastHRCount > 0 {
			lastHRAvg = lastHRSum / float64(lastHRCount)
		}
		if priorHRCount > 0 {
			priorHRAvg = priorHRSum / float64(priorHRCount)
		}
		intTrend := trendDirection(lastHRAvg, priorHRAvg)
		fmt.Fprintf(sb, "- Intensity: %s (avg HR %.0f vs %.0f)\n",
			intTrend, lastHRAvg, priorHRAvg)
	}

	freqTrend := trendDirection(float64(lastFreq), float64(priorFreq))
	fmt.Fprintf(sb, "- Frequency: %s (%d workouts last 2w vs %d prior 2w)\n",
		freqTrend, lastFreq, priorFreq)
}

// trendDirection returns "increasing", "decreasing", or "stable" based on comparing
// two values. A relative change of more than 5% is considered a trend.
func trendDirection(current, previous float64) string {
	if previous == 0 {
		if current > 0 {
			return "increasing"
		}
		return "stable"
	}
	change := (current - previous) / previous
	if change > 0.05 {
		return "increasing"
	}
	if change < -0.05 {
		return "decreasing"
	}
	return "stable"
}

// BuildEnrichedWorkoutBlock formats the computed training metrics for a workout
// (HR drift, pace CV, training load, and ACR) into a labelled text block with
// interpretation hints for injection into AI prompts.
// Returns an empty string when no metrics are available.
func BuildEnrichedWorkoutBlock(db *sql.DB, w *Workout) string {
	var body strings.Builder

	if w.HRDriftPct != nil {
		drift := *w.HRDriftPct
		var hint string
		switch {
		case drift > 10:
			hint = "high drift — possible fatigue or dehydration"
		case drift > 5:
			hint = "moderate drift — effort increased toward the end"
		case drift < -5:
			hint = "negative drift — HR decreased, possible pacing strategy or warm-up effect"
		default:
			hint = "stable HR across the effort"
		}
		fmt.Fprintf(&body, "- HR Drift: %+.1f%% (%s)\n", drift, hint)
	}

	if w.PaceCVPct != nil {
		cv := *w.PaceCVPct
		var hint string
		switch {
		case cv > 15:
			hint = "very high variability — highly uneven effort, intervals or terrain"
		case cv > 8:
			hint = "moderate variability — some pace fluctuation"
		default:
			hint = "consistent pacing"
		}
		fmt.Fprintf(&body, "- Pace CV: %.1f%% (%s)\n", cv, hint)
	}

	if w.TrainingLoad != nil {
		load := *w.TrainingLoad
		var hint string
		switch {
		case load > 80:
			hint = "high — significant stimulus, expect fatigue"
		case load > 60:
			hint = "moderately high"
		case load < 30:
			hint = "low — easy/recovery session"
		default:
			hint = "moderate"
		}
		fmt.Fprintf(&body, "- Training Load: %.1f (%s)\n", load, hint)
	}

	// Always attempt ACR computation when a DB connection is available — it is
	// independent of the per-workout computed fields and must not be skipped even
	// when HRDriftPct, PaceCVPct, and TrainingLoad are all nil.
	if db != nil {
		workoutDate, acrOK := time.Now().UTC(), true
		if w.StartedAt != "" {
			// Try both nanosecond and standard RFC3339 formats, as timestamps from
			// different sources may include or omit the sub-second component.
			if t, err := time.Parse(time.RFC3339Nano, w.StartedAt); err == nil {
				workoutDate = t
			} else if t, err := time.Parse(time.RFC3339, w.StartedAt); err == nil {
				workoutDate = t
			} else {
				// Don't fall back to time.Now() for an unparseable timestamp — it would
				// produce ACR for today rather than for the workout being analysed.
				log.Printf("BuildEnrichedWorkoutBlock: invalid StartedAt %q for workout %d, skipping ACR", w.StartedAt, w.ID)
				acrOK = false
			}
		}
		if acrOK {
			acr, acute, chronic, err := ComputeACR(db, w.UserID, workoutDate)
			if err != nil {
				log.Printf("BuildEnrichedWorkoutBlock: ComputeACR error for user %d: %v", w.UserID, err)
			} else if acr != nil {
				ratio := *acr
				var hint string
				switch {
				case ratio > 1.5:
					hint = "high injury risk — acute load far exceeds chronic baseline"
				case ratio > 1.3:
					hint = "caution — above the optimal 0.8–1.3 window"
				case ratio < 0.8:
					hint = "undertraining — below chronic baseline"
				default:
					hint = "optimal range (0.8–1.3)"
				}
				fmt.Fprintf(&body, "- ACR: %.2f (acute=%.1f, chronic=%.1f) — %s\n",
					ratio, acute, chronic, hint)
			} else if acute > 0 {
				fmt.Fprintf(&body, "- ACR: insufficient history (acute=%.1f, no chronic baseline yet)\n", acute)
			}
		}
	}

	if body.Len() == 0 {
		return ""
	}
	return "Computed Training Metrics:\n" + body.String()
}

// writeTypeDistributionSection formats the AI-tagged workout type distribution as a
// bullet list sorted by count descending.
func writeTypeDistributionSection(sb *strings.Builder, dist map[string]int) {
	if len(dist) == 0 {
		return
	}

	type kv struct {
		tag string
		cnt int
	}
	pairs := make([]kv, 0, len(dist))
	for tag, cnt := range dist {
		pairs = append(pairs, kv{tag, cnt})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].cnt != pairs[j].cnt {
			return pairs[i].cnt > pairs[j].cnt
		}
		return pairs[i].tag < pairs[j].tag
	})

	sb.WriteString("Workout Type Distribution (AI-tagged):\n")
	for _, p := range pairs {
		label := strings.TrimPrefix(p.tag, "ai:type:")
		fmt.Fprintf(sb, "- %s: %d workouts\n", label, p.cnt)
	}
	sb.WriteString("\n")
}

// writeRacePredictionsSection formats race time predictions as a markdown table.
func writeRacePredictionsSection(sb *strings.Builder, preds *RacePredictions) {
	if preds == nil || len(preds.Predictions) == 0 {
		return
	}
	fmt.Fprintf(sb, "Race Predictions (based on %s pace, Riegel formula):\n", preds.RefDistance)
	sb.WriteString("| Distance | Predicted Time | Pace/km |\n")
	sb.WriteString("|----------|----------------|----------|\n")
	for _, p := range preds.Predictions {
		fmt.Fprintf(sb, "| %s | %s | %s |\n", p.Distance, p.PredictedTime, p.PacePerKm)
	}
	sb.WriteString("\n")
}

// RaceContext holds enriched race data for AI prompt injection when analyzing
// a race workout. Built by BuildRaceContext.
type RaceContext struct {
	RaceName      string
	RaceDate      string
	DistanceM     float64
	TargetTime    *int // seconds
	ActualTime    int  // seconds (from workout duration)
	Priority      string
	Notes         string
	PacingProfile string // "positive", "negative", or "even"
	// RiegelComparisons holds percentage over/under Riegel-predicted time from historical races.
	RiegelComparisons []RiegelComparison
	// TrainingPhase from the current Stride plan (e.g. "base", "build", "taper").
	TrainingPhase string
	// WeeklyVolume from the most recent complete training week.
	WeeklyVolumeKm float64
	// PreviousRaces at similar distances with results.
	PreviousRaces []PastRace
}

// RiegelComparison holds a comparison between actual race time and Riegel-predicted time
// from a reference race.
type RiegelComparison struct {
	RefRaceName string
	RefDistance  float64
	RefTime     int // seconds
	PredictedS  float64
	ActualS     int
	DeltaPct    float64 // positive = slower than predicted, negative = faster
}

// PastRace is a previous race result at a similar distance.
type PastRace struct {
	Name      string
	Date      string
	DistanceM float64
	TimeS     int
	PacePerKm string
}

// isRaceWorkout returns true if the workout is classified as a race via
// race_id linkage or the ai:type:race tag.
func isRaceWorkout(w *Workout) bool {
	if w.RaceID != nil {
		return true
	}
	for _, tag := range w.Tags {
		if tag == "ai:type:race" {
			return true
		}
	}
	return false
}

// BuildRaceContext builds an enriched race context for AI prompt injection.
// Returns nil if the workout is not a race or no race data is available.
func BuildRaceContext(db *sql.DB, workout *Workout) *RaceContext {
	if !isRaceWorkout(workout) {
		return nil
	}

	rc := &RaceContext{
		ActualTime: workout.DurationSeconds,
		DistanceM:  workout.DistanceMeters,
	}

	// Fetch the linked race entry if available.
	if workout.RaceID != nil {
		race, err := getRaceByID(db, *workout.RaceID, workout.UserID)
		if err != nil {
			log.Printf("BuildRaceContext: failed to load race %d: %v", *workout.RaceID, err)
		} else if race != nil {
			rc.RaceName = race.name
			rc.RaceDate = race.date
			rc.DistanceM = race.distanceM
			rc.TargetTime = race.targetTime
			rc.Priority = race.priority
			rc.Notes = race.notes
		}
	}

	// Analyze pacing from laps.
	if len(workout.Laps) > 1 {
		rc.PacingProfile = classifyPacing(workout.Laps)
	}

	// Riegel comparisons from historical races.
	rc.RiegelComparisons = buildRiegelComparisons(db, workout)

	// Training phase from current Stride plan.
	rc.TrainingPhase = getCurrentTrainingPhase(db, workout.UserID)

	// Weekly volume from most recent complete week.
	rc.WeeklyVolumeKm = getRecentWeeklyVolume(db, workout.UserID)

	// Previous races at similar distances.
	rc.PreviousRaces = fetchSimilarRaces(db, workout.UserID, workout.DistanceMeters, workout.ID)

	return rc
}

// raceRow is a minimal race record for internal use to avoid importing stride.
type raceRow struct {
	name       string
	date       string
	distanceM  float64
	targetTime *int
	priority   string
	notes      string
}

// getRaceByID fetches a single race, decrypting name and notes.
func getRaceByID(db *sql.DB, raceID, userID int64) (*raceRow, error) {
	var name, date, priority, notes string
	var distanceM float64
	var targetTime *int
	err := db.QueryRow(`
		SELECT name, date, distance_m, target_time, priority, notes
		FROM stride_races
		WHERE id = ? AND user_id = ?
	`, raceID, userID).Scan(&name, &date, &distanceM, &targetTime, &priority, &notes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decName, err := encryption.DecryptField(name)
	if err != nil {
		return nil, fmt.Errorf("decrypt race name: %w", err)
	}
	decNotes, err := encryption.DecryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("decrypt race notes: %w", err)
	}
	return &raceRow{
		name:       decName,
		date:       date,
		distanceM:  distanceM,
		targetTime: targetTime,
		priority:   priority,
		notes:      decNotes,
	}, nil
}

// classifyPacing analyzes lap split times to determine the pacing strategy.
// Returns "negative" (getting faster), "positive" (slowing down), or "even".
func classifyPacing(laps []Lap) string {
	if len(laps) < 2 {
		return "even"
	}

	// Use pace (sec/km) for laps that have distance. Skip laps with no pace data.
	var paces []float64
	for _, lap := range laps {
		if lap.AvgPaceSecPerKm > 0 && lap.DistanceMeters > 100 {
			paces = append(paces, lap.AvgPaceSecPerKm)
		}
	}
	if len(paces) < 2 {
		return "even"
	}

	// Compare first half average vs second half average.
	mid := len(paces) / 2
	var firstSum, secondSum float64
	for _, p := range paces[:mid] {
		firstSum += p
	}
	for _, p := range paces[mid:] {
		secondSum += p
	}
	firstAvg := firstSum / float64(mid)
	secondAvg := secondSum / float64(len(paces)-mid)

	// >3% difference threshold
	diff := (secondAvg - firstAvg) / firstAvg
	if diff > 0.03 {
		return "positive" // slowing down
	}
	if diff < -0.03 {
		return "negative" // getting faster
	}
	return "even"
}

// riegelCompare returns the percentage difference between actual race time and
// Riegel-predicted time from a reference effort. Positive = slower than predicted.
func riegelCompare(actualTimeS int, actualDistM, refTimeS float64, refDistM float64) float64 {
	predicted := riegelPredict(refTimeS, refDistM, actualDistM)
	if predicted <= 0 {
		return 0
	}
	return (float64(actualTimeS) - predicted) / predicted * 100
}

// buildRiegelComparisons compares this race result against historical race results
// at different distances using the Riegel formula.
func buildRiegelComparisons(db *sql.DB, workout *Workout) []RiegelComparison {
	rows, err := db.Query(`
		SELECT sr.name, sr.distance_m, sr.result_time
		FROM stride_races sr
		JOIN workouts w ON w.race_id = sr.id AND w.user_id = sr.user_id
		WHERE sr.user_id = ?
		  AND sr.result_time IS NOT NULL
		  AND sr.result_time > 0
		  AND w.id != ?
		ORDER BY w.started_at DESC
		LIMIT 10
	`, workout.UserID, workout.ID)
	if err != nil {
		log.Printf("BuildRaceContext: riegel comparisons query: %v", err)
		return nil
	}
	defer rows.Close()

	var comps []RiegelComparison
	for rows.Next() {
		var encName string
		var distM float64
		var resultTime int
		if err := rows.Scan(&encName, &distM, &resultTime); err != nil {
			log.Printf("BuildRaceContext: scan riegel row: %v", err)
			continue
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			log.Printf("BuildRaceContext: decrypt race name: %v", err)
			name = "Unknown race"
		}
		// Skip same-distance races (Riegel isn't meaningful for same distance).
		distRatio := distM / workout.DistanceMeters
		if distRatio > 0.9 && distRatio < 1.1 {
			continue
		}
		delta := riegelCompare(workout.DurationSeconds, workout.DistanceMeters, float64(resultTime), distM)
		comps = append(comps, RiegelComparison{
			RefRaceName: name,
			RefDistance:  distM,
			RefTime:     resultTime,
			PredictedS:  riegelPredict(float64(resultTime), distM, workout.DistanceMeters),
			ActualS:     workout.DurationSeconds,
			DeltaPct:    delta,
		})
	}
	return comps
}

// getCurrentTrainingPhase returns the phase from the current Stride plan, or empty string.
func getCurrentTrainingPhase(db *sql.DB, userID int64) string {
	today := time.Now().UTC().Format("2006-01-02")
	var phase string
	err := db.QueryRow(`
		SELECT phase FROM stride_plans
		WHERE user_id = ? AND week_start <= ? AND week_end >= ?
		ORDER BY week_start DESC LIMIT 1
	`, userID, today, today).Scan(&phase)
	if err != nil {
		return ""
	}
	return phase
}

// getRecentWeeklyVolume returns the total distance in km from the most recent
// complete training week.
func getRecentWeeklyVolume(db *sql.DB, userID int64) float64 {
	// Find Monday of the current week to exclude it.
	now := time.Now().UTC()
	weekday := int(now.Weekday())
	daysBack := (weekday - 1 + 7) % 7
	currentMonday := now.AddDate(0, 0, -daysBack)
	prevMonday := currentMonday.AddDate(0, 0, -7)
	prevSunday := currentMonday.AddDate(0, 0, -1)

	var totalDist float64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(distance_meters), 0)
		FROM workouts
		WHERE user_id = ?
		  AND date(started_at) >= ?
		  AND date(started_at) <= ?
	`, userID, prevMonday.Format("2006-01-02"), prevSunday.Format("2006-01-02")).Scan(&totalDist)
	if err != nil {
		return 0
	}
	return totalDist / 1000
}

// fetchSimilarRaces returns past races at similar distances (within ±30%) that
// have a result time, excluding the current workout.
func fetchSimilarRaces(db *sql.DB, userID int64, distanceM float64, excludeWorkoutID int64) []PastRace {
	minDist := distanceM * 0.7
	maxDist := distanceM * 1.3

	rows, err := db.Query(`
		SELECT sr.name, sr.date, sr.distance_m, sr.result_time
		FROM stride_races sr
		JOIN workouts w ON w.race_id = sr.id AND w.user_id = sr.user_id
		WHERE sr.user_id = ?
		  AND sr.result_time IS NOT NULL
		  AND sr.result_time > 0
		  AND sr.distance_m >= ? AND sr.distance_m <= ?
		  AND w.id != ?
		ORDER BY sr.date DESC
		LIMIT 5
	`, userID, minDist, maxDist, excludeWorkoutID)
	if err != nil {
		log.Printf("BuildRaceContext: fetch similar races: %v", err)
		return nil
	}
	defer rows.Close()

	var races []PastRace
	for rows.Next() {
		var encName, date string
		var distM float64
		var timeS int
		if err := rows.Scan(&encName, &date, &distM, &timeS); err != nil {
			log.Printf("BuildRaceContext: scan similar race: %v", err)
			continue
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			name = "Unknown race"
		}
		pacePerKm := float64(timeS) / (distM / 1000)
		races = append(races, PastRace{
			Name:      name,
			Date:      date,
			DistanceM: distM,
			TimeS:     timeS,
			PacePerKm: formatPacePerKm(pacePerKm),
		})
	}
	return races
}

// FormatRacePromptSection formats a RaceContext into a prompt section for Claude.
func FormatRacePromptSection(rc *RaceContext) string {
	if rc == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n--- RACE ANALYSIS CONTEXT ---\n")
	sb.WriteString("This workout is a RACE. Provide race-specific analysis.\n\n")

	// Race details
	if rc.RaceName != "" {
		fmt.Fprintf(&sb, "Race: %s\n", rc.RaceName)
	}
	if rc.RaceDate != "" {
		fmt.Fprintf(&sb, "Race Date: %s\n", rc.RaceDate)
	}
	fmt.Fprintf(&sb, "Distance: %.2f km\n", rc.DistanceM/1000)
	fmt.Fprintf(&sb, "Priority: %s-race\n", rc.Priority)

	// Target vs actual
	if rc.TargetTime != nil && *rc.TargetTime > 0 {
		target := *rc.TargetTime
		fmt.Fprintf(&sb, "Target Time: %s\n", formatDurationSecs(target))
		fmt.Fprintf(&sb, "Actual Time: %s\n", formatDurationSecs(rc.ActualTime))
		diffSec := rc.ActualTime - target
		diffPct := float64(diffSec) / float64(target) * 100
		if diffSec > 0 {
			fmt.Fprintf(&sb, "Result: %s SLOWER than target (+%.1f%%)\n", formatDurationSecs(diffSec), diffPct)
		} else if diffSec < 0 {
			fmt.Fprintf(&sb, "Result: %s FASTER than target (%.1f%%)\n", formatDurationSecs(-diffSec), diffPct)
		} else {
			sb.WriteString("Result: Exactly on target\n")
		}
	}

	// Pacing analysis
	if rc.PacingProfile != "" {
		var pacingDesc string
		switch rc.PacingProfile {
		case "negative":
			pacingDesc = "Negative split (got faster) — strong finish, good pacing discipline"
		case "positive":
			pacingDesc = "Positive split (slowed down) — may indicate too-aggressive start or fading"
		case "even":
			pacingDesc = "Even pacing — consistent effort throughout"
		}
		fmt.Fprintf(&sb, "Pacing: %s\n", pacingDesc)
	}

	// Riegel comparisons
	if len(rc.RiegelComparisons) > 0 {
		sb.WriteString("\nRiegel Formula Comparison (predicted vs actual):\n")
		sb.WriteString("| Reference Race | Ref Distance | Predicted | Actual | Delta |\n")
		sb.WriteString("|----------------|-------------|-----------|--------|-------|\n")
		for _, c := range rc.RiegelComparisons {
			sign := "+"
			if c.DeltaPct < 0 {
				sign = ""
			}
			fmt.Fprintf(&sb, "| %s | %.1f km | %s | %s | %s%.1f%% |\n",
				c.RefRaceName, c.RefDistance/1000,
				formatDurationSecs(int(math.Round(c.PredictedS))),
				formatDurationSecs(c.ActualS),
				sign, c.DeltaPct)
		}
		sb.WriteString("Negative delta = outperformed prediction (good). Positive = underperformed.\n")
	}

	// Training block context
	if rc.TrainingPhase != "" || rc.WeeklyVolumeKm > 0 {
		sb.WriteString("\nTraining Block Context:\n")
		if rc.TrainingPhase != "" {
			fmt.Fprintf(&sb, "- Current Phase: %s\n", rc.TrainingPhase)
			if strings.Contains(strings.ToLower(rc.TrainingPhase), "taper") {
				sb.WriteString("  (Athlete was in taper — assess if freshness benefited performance)\n")
			}
		}
		if rc.WeeklyVolumeKm > 0 {
			fmt.Fprintf(&sb, "- Recent Weekly Volume: %.1f km\n", rc.WeeklyVolumeKm)
		}
	}

	if rc.Notes != "" {
		fmt.Fprintf(&sb, "\nAthlete's Race Notes: %s\n", rc.Notes)
	}

	// Previous races at similar distance
	if len(rc.PreviousRaces) > 0 {
		sb.WriteString("\nPrevious Races at Similar Distance:\n")
		sb.WriteString("| Race | Date | Distance | Time | Pace/km |\n")
		sb.WriteString("|------|------|----------|------|----------|\n")
		for _, r := range rc.PreviousRaces {
			fmt.Fprintf(&sb, "| %s | %s | %.1f km | %s | %s |\n",
				r.Name, r.Date, r.DistanceM/1000,
				formatDurationSecs(r.TimeS), r.PacePerKm)
		}
	}

	// Instructions for Claude
	sb.WriteString("\n--- RACE ANALYSIS INSTRUCTIONS ---\n")
	sb.WriteString(`In addition to the standard analysis fields, your response MUST address these four areas in the relevant fields:

1. **Race Execution Analysis** (in pacing_analysis): Evaluate the pacing strategy, split analysis, and how well the race was executed tactically. Was the start too aggressive? Did the athlete fade or finish strong?

2. **Goal Assessment** (in effort_summary): Compare actual vs target time. Was the goal realistic? What factors contributed to meeting or missing the target?

3. **Training Effectiveness** (in threshold_context): Based on the training block phase, weekly volume, and Riegel comparisons, assess whether the training prepared the athlete well for this race distance and effort.

4. **Next-Race Recommendations** (in suggestions): Provide specific, actionable recommendations for the next race — pacing adjustments, training focus areas, race-day strategy improvements.
`)

	return sb.String()
}
