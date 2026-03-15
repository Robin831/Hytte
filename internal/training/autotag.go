package training

import (
	"fmt"
	"math"
)

// GenerateAutoTags analyzes a parsed workout's lap structure and returns
// auto-generated tags describing the interval pattern (e.g. "6x6m (r1m)").
// Returns nil if no recognizable interval pattern is detected.
func GenerateAutoTags(pw *ParsedWorkout) []string {
	if len(pw.Laps) < 3 {
		// Need at least 3 laps for an interval pattern (work, rest, work).
		return nil
	}

	// Try alternating work/rest pattern first.
	if tag := detectAlternatingPattern(pw); tag != "" {
		return []string{"auto:" + tag}
	}

	// Try uniform repeats (all laps similar duration, no distinct rest).
	if tag := detectUniformRepeats(pw); tag != "" {
		return []string{"auto:" + tag}
	}

	return nil
}

// detectAlternatingPattern checks for work/rest/work/rest... structure.
// Work laps are the odd-indexed (1st, 3rd, 5th...) and rest laps are even-indexed
// (2nd, 4th, 6th...), or vice versa — whichever produces the more consistent grouping.
func detectAlternatingPattern(pw *ParsedWorkout) string {
	laps := pw.Laps
	n := len(laps)
	if n < 3 {
		return ""
	}

	// Try both assignments: odd=work or odd=rest.
	for _, workOnOdd := range []bool{true, false} {
		var workLaps, restLaps []ParsedLap
		for i, lap := range laps {
			isOdd := i%2 == 0 // 0-indexed, so first lap is index 0 (odd position 1)
			if (isOdd && workOnOdd) || (!isOdd && !workOnOdd) {
				workLaps = append(workLaps, lap)
			} else {
				restLaps = append(restLaps, lap)
			}
		}

		// Need at least 2 work laps to form a pattern.
		if len(workLaps) < 2 {
			continue
		}

		// Rest laps might be one fewer than work laps (no trailing rest).
		if len(restLaps) == 0 {
			continue
		}

		// Check consistency within each group (15% tolerance).
		if !lapsConsistent(workLaps, 0.15) || !lapsConsistent(restLaps, 0.15) {
			continue
		}

		// Work laps should be meaningfully different from rest laps
		// (otherwise it's uniform repeats, not intervals).
		avgWork := avgDuration(workLaps)
		avgRest := avgDuration(restLaps)
		if avgWork > 0 && avgRest > 0 {
			ratio := avgWork / avgRest
			if ratio > 0.7 && ratio < 1.4 {
				// Too similar — not clearly work/rest.
				continue
			}
		}

		// Ensure work > rest (swap labels if needed).
		if avgWork < avgRest {
			continue // The other iteration will catch the swapped assignment.
		}

		return formatIntervalTag(pw, workLaps, restLaps)
	}

	return ""
}

// detectUniformRepeats checks if all laps have similar duration (uniform intervals
// without distinct rest periods, e.g. track repeats with walk-back recovery not
// recorded as separate laps).
func detectUniformRepeats(pw *ParsedWorkout) string {
	laps := pw.Laps
	if len(laps) < 3 {
		return ""
	}

	if !lapsConsistent(laps, 0.15) {
		return ""
	}

	count := len(laps)
	avgDur := avgDuration(laps)

	// For distance-based sports with consistent distances, prefer distance format.
	if isDistanceSport(pw.Sport) {
		avgDist := avgDistance(laps)
		if avgDist > 0 && distancesConsistent(laps, 0.15) {
			distStr := formatDistance(avgDist)
			if distStr != "" {
				return fmt.Sprintf("%dx%s", count, distStr)
			}
		}
	}

	durStr := formatDuration(avgDur)
	return fmt.Sprintf("%dx%s", count, durStr)
}

// lapsConsistent returns true if all lap durations are within tolerance of the mean.
func lapsConsistent(laps []ParsedLap, tolerance float64) bool {
	if len(laps) <= 1 {
		return true
	}
	avg := avgDuration(laps)
	if avg == 0 {
		return false
	}
	for _, lap := range laps {
		if math.Abs(lap.DurationSeconds-avg)/avg > tolerance {
			return false
		}
	}
	return true
}

// distancesConsistent returns true if all lap distances are within tolerance of the mean.
func distancesConsistent(laps []ParsedLap, tolerance float64) bool {
	if len(laps) <= 1 {
		return true
	}
	avg := avgDistance(laps)
	if avg == 0 {
		return false
	}
	for _, lap := range laps {
		if math.Abs(lap.DistanceMeters-avg)/avg > tolerance {
			return false
		}
	}
	return true
}

func avgDuration(laps []ParsedLap) float64 {
	if len(laps) == 0 {
		return 0
	}
	var sum float64
	for _, l := range laps {
		sum += l.DurationSeconds
	}
	return sum / float64(len(laps))
}

func avgDistance(laps []ParsedLap) float64 {
	if len(laps) == 0 {
		return 0
	}
	var sum float64
	for _, l := range laps {
		sum += l.DistanceMeters
	}
	return sum / float64(len(laps))
}

// formatIntervalTag produces the "NxDur (rRestDur)" tag string.
func formatIntervalTag(pw *ParsedWorkout, workLaps, restLaps []ParsedLap) string {
	count := len(workLaps)
	avgWorkDur := avgDuration(workLaps)
	avgRestDur := avgDuration(restLaps)

	// For distance-based sports, try distance format for work intervals.
	var workStr string
	if isDistanceSport(pw.Sport) {
		avgDist := avgDistance(workLaps)
		if avgDist > 0 && distancesConsistent(workLaps, 0.15) {
			workStr = formatDistance(avgDist)
		}
	}
	if workStr == "" {
		workStr = formatDuration(avgWorkDur)
	}

	tag := fmt.Sprintf("%dx%s", count, workStr)

	// Add rest duration if meaningful (> 5 seconds).
	if avgRestDur > 5 {
		restStr := formatDuration(avgRestDur)
		tag += fmt.Sprintf(" (r%s)", restStr)
	}

	return tag
}

// formatDuration formats seconds into a human-readable compact string.
// Examples: 45 -> "45s", 60 -> "1m", 90 -> "1m30s", 360 -> "6m".
func formatDuration(seconds float64) string {
	total := int(math.Round(seconds))
	if total <= 0 {
		return "0s"
	}
	m := total / 60
	s := total % 60

	// Round to nearest 5s for durations > 2 minutes to keep tags clean.
	if m >= 2 && s > 0 {
		rounded := int(math.Round(float64(total)/5.0)) * 5
		m = rounded / 60
		s = rounded % 60
	}

	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

// formatDistance formats meters into a compact distance string.
// Rounds to common track/road distances when close.
// Examples: 400 -> "400m", 1000 -> "1km", 1609 -> "1mi".
func formatDistance(meters float64) string {
	rounded := int(math.Round(meters))
	if rounded <= 0 {
		return ""
	}

	// Snap to common distances if within 5%.
	commonDistances := []struct {
		meters int
		label  string
	}{
		{200, "200m"},
		{400, "400m"},
		{600, "600m"},
		{800, "800m"},
		{1000, "1km"},
		{1200, "1200m"},
		{1500, "1500m"},
		{1600, "1mi"},
		{1609, "1mi"},
		{2000, "2km"},
		{3000, "3km"},
		{5000, "5km"},
	}
	for _, cd := range commonDistances {
		if math.Abs(float64(rounded-cd.meters))/float64(cd.meters) <= 0.05 {
			return cd.label
		}
	}

	// Generic formatting.
	if rounded >= 1000 && rounded%1000 == 0 {
		return fmt.Sprintf("%dkm", rounded/1000)
	}
	return fmt.Sprintf("%dm", rounded)
}

// isDistanceSport returns true for sports where distance-based intervals are common.
func isDistanceSport(sport string) bool {
	switch sport {
	case "running", "cycling", "swimming", "rowing", "cross_country_skiing":
		return true
	}
	return false
}
