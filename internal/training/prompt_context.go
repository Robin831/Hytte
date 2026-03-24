package training

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/lactate"
)

// UserTrainingProfile holds a user's training profile block (for prompt injection) and key
// parsed values derived from a single preferences load.
type UserTrainingProfile struct {
	Block       string
	ThresholdHR int
}

// BuildUserTrainingProfile loads user preferences once and returns the full profile.
// Use this in handlers so that ThresholdHR is available without a second DB round-trip.
func BuildUserTrainingProfile(db *sql.DB, userID int64) UserTrainingProfile {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("BuildUserTrainingProfile: failed to load preferences for user %d: %v", userID, err)
		return UserTrainingProfile{}
	}
	block, thresholdHR := buildUserProfileFromPrefs(prefs, db, userID)
	return UserTrainingProfile{Block: block, ThresholdHR: thresholdHR}
}

// BuildUserProfileBlock builds a structured text block with the user's personal
// training profile for injection into AI prompts. Returns an empty string if no
// useful profile data is available.
func BuildUserProfileBlock(db *sql.DB, userID int64) string {
	return BuildUserTrainingProfile(db, userID).Block
}

// buildUserProfileFromPrefs is the internal implementation that accepts already-loaded prefs.
// Returns (block, thresholdHR).
func buildUserProfileFromPrefs(prefs map[string]string, db *sql.DB, userID int64) (string, int) {
	// Parse preference values.
	maxHR := parseIntPref(prefs, "max_hr")
	restingHR := parseIntPref(prefs, "resting_hr")
	thresholdHR := parseIntPref(prefs, "threshold_hr")
	thresholdPace := parseIntPref(prefs, "threshold_pace") // sec/km
	easyPaceMin := parseIntPref(prefs, "easy_pace_min")    // sec/km
	easyPaceMax := parseIntPref(prefs, "easy_pace_max")    // sec/km

	// Try to load zones from the most recent lactate test.
	var zonesResult *lactate.ZonesResult
	var zonesSource string
	var thresholdHRSource string

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

	// Nothing useful to show — omit the block entirely.
	if maxHR == 0 && thresholdHR == 0 && zonesResult == nil {
		return "", 0
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

	if zonesResult != nil && len(zonesResult.Zones) > 0 {
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

	return sb.String(), thresholdHR
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
// including weekly training summaries, similar past workouts, and recent trends.
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

	if len(summaries) == 0 && len(groups) == 0 {
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

	return sb.String()
}

// writeWeeklySummarySection formats the last nWeeks of weekly training data as a table.
func writeWeeklySummarySection(sb *strings.Builder, summaries []WeeklySummary, nWeeks int) {
	limit := nWeeks
	if limit > len(summaries) {
		limit = len(summaries)
	}

	sb.WriteString("Weekly Training Summary:\n")
	sb.WriteString("| Week | Duration | Distance | Workouts | Avg HR |\n")
	sb.WriteString("|------|----------|----------|----------|--------|\n")

	for _, s := range summaries[:limit] {
		hrStr := "--"
		if s.AvgHeartRate > 0 {
			hrStr = fmt.Sprintf("%.0f", s.AvgHeartRate)
		}
		distStr := fmt.Sprintf("%.1f km", s.TotalDistance/1000)
		fmt.Fprintf(sb, "| %s | %s | %s | %d | %s |\n",
			s.WeekStart, formatDurationSecs(s.TotalDuration), distStr, s.WorkoutCount, hrStr)
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
	// summaries are DESC (most recent first)
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
