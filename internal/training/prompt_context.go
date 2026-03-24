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
				zonesSource = "from lactate test"
			}
			if thresholdPace == 0 && best.SpeedKmh > 0 {
				// Convert speed (km/h) to pace (sec/km): 3600 / speed.
				thresholdPace = int(math.Round(3600.0 / best.SpeedKmh))
			}
			zonesResult = lactate.CalculateZones(lactate.ZoneSystemOlympiatoppen, best.SpeedKmh, best.HeartRateBpm, maxHR)
			if zonesSource == "" {
				zonesSource = "from lactate test"
			}
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
		if zonesSource != "" {
			fmt.Fprintf(&sb, "- Threshold HR: %d bpm (%s)\n", thresholdHR, zonesSource)
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
