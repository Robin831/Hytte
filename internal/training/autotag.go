package training

import (
	"fmt"
	"math"
	"sort"
)

// GenerateAutoTags analyzes a parsed workout's lap structure and returns
// auto-generated tags describing the interval pattern (e.g. "auto:6x6m (r1m)")
// and certain non-interval properties such as "auto:treadmill". It returns
// nil only when no auto-tags are generated at all.
func GenerateAutoTags(pw *ParsedWorkout) []string {
	var tags []string

	// Tag indoor/treadmill workouts.
	if !pw.HasGPS {
		tags = append(tags, "auto:treadmill")
	}
	if pw.SubSport == "treadmill" || pw.SubSport == "indoor_running" {
		// Ensure treadmill tag exists even if HasGPS detection missed it.
		hasTreadmill := false
		for _, t := range tags {
			if t == "auto:treadmill" {
				hasTreadmill = true
				break
			}
		}
		if !hasTreadmill {
			tags = append(tags, "auto:treadmill")
		}
	}

	if len(pw.Laps) < 3 {
		// Need at least 3 laps for an interval pattern (work, rest, work).
		return tags
	}

	// Try alternating work/rest pattern first; only fall back to uniform repeats
	// if no alternating pattern is found (keeps the two detections mutually exclusive).
	if tag := detectAlternatingPattern(pw); tag != "" {
		tags = append(tags, "auto:"+tag)
	} else if tag := detectUniformRepeats(pw); tag != "" {
		tags = append(tags, "auto:"+tag)
	}

	if len(tags) == 0 {
		return nil
	}
	return tags
}

// trimOutlierLaps removes leading and trailing laps whose duration deviates
// more than 50% from both the Q1 and Q3 reference durations. Using two
// reference points handles alternating work/rest patterns where laps cluster
// around two distinct durations — only true warmup/cooldown outliers that are
// far from both clusters get trimmed.
func trimOutlierLaps(laps []ParsedLap) []ParsedLap {
	if len(laps) < 3 {
		return laps
	}

	// Compute Q1 and Q3 reference durations.
	durations := make([]float64, len(laps))
	for i, l := range laps {
		durations[i] = l.DurationSeconds
	}
	sort.Float64s(durations)
	n := len(durations)
	q1 := durations[n/4]
	q3 := durations[3*n/4]
	if q1 == 0 && q3 == 0 {
		return laps
	}

	const tolerance = 0.50

	isOutlier := func(d float64) bool {
		// A lap is an outlier only if it deviates >50% from BOTH reference points.
		farFromQ1 := q1 == 0 || math.Abs(d-q1)/q1 > tolerance
		farFromQ3 := q3 == 0 || math.Abs(d-q3)/q3 > tolerance
		return farFromQ1 && farFromQ3
	}

	// Trim leading outliers.
	start := 0
	for start < len(laps) && isOutlier(laps[start].DurationSeconds) {
		start++
	}

	// Trim trailing outliers.
	end := len(laps)
	for end > start && isOutlier(laps[end-1].DurationSeconds) {
		end--
	}

	trimmed := laps[start:end]
	if len(trimmed) < 3 {
		return laps // Don't trim if it would leave too few laps.
	}
	return trimmed
}

// detectAlternatingPattern checks for work/rest/work/rest... structure.
// Splits laps into even-indexed and odd-indexed groups, checks consistency,
// then determines which group is work vs rest by pace (distance sports) or duration.
func detectAlternatingPattern(pw *ParsedWorkout) string {
	laps := trimOutlierLaps(pw.Laps)
	if len(laps) < 3 {
		return ""
	}

	// Split into two alternating groups.
	var group1, group2 []ParsedLap
	for i, lap := range laps {
		if i%2 == 0 {
			group1 = append(group1, lap)
		} else {
			group2 = append(group2, lap)
		}
	}

	// Need at least 2 in each group to avoid low-signal "1x…" tags.
	if len(group1) < 2 || len(group2) < 2 {
		return ""
	}

	// Check consistency within each group (15% tolerance).
	if !lapsConsistent(group1, 0.15) || !lapsConsistent(group2, 0.15) {
		return ""
	}

	avg1 := avgDuration(group1)
	avg2 := avgDuration(group2)

	// Determine which group is work and which is rest.
	var workLaps, restLaps []ParsedLap

	if isDistanceSport(pw.Sport) && avgDistance(group1) > 0 && avgDistance(group2) > 0 {
		// For distance sports, use pace (m/s) to identify work intervals.
		pace1 := avgDistance(group1) / avg1
		pace2 := avgDistance(group2) / avg2
		paceRatio := pace1 / pace2
		if paceRatio > 0.8 && paceRatio < 1.25 {
			return "" // Paces too similar — not clearly work/rest.
		}
		if pace1 > pace2 {
			workLaps, restLaps = group1, group2
		} else {
			workLaps, restLaps = group2, group1
		}
	} else {
		// For non-distance sports, longer duration = work.
		ratio := avg1 / avg2
		if ratio > 0.7 && ratio < 1.4 {
			return "" // Too similar — not clearly work/rest.
		}
		if avg1 > avg2 {
			workLaps, restLaps = group1, group2
		} else {
			workLaps, restLaps = group2, group1
		}
		// For true non-distance sports (e.g. strength) we have no pace signal to
		// validate which group is work vs rest. Guard against inverted patterns
		// (e.g. 30s hard / 2m easy tagged as "Nx2m (r30s)"):
		if !isDistanceSport(pw.Sport) {
			avgRestDur := avgDuration(restLaps)
			avgWorkDur := avgDuration(workLaps)
			// Bail if rest somehow exceeds work (defensive; guards future logic changes).
			if avgRestDur > avgWorkDur {
				return ""
			}
			// Bail when work is >3× rest and rest is a real interval (>=30s) —
			// signals a likely inverted pattern.
			if avgWorkDur/avgRestDur > 3.0 && avgRestDur >= 30 {
				return ""
			}
		}
	}

	return formatIntervalTag(pw, workLaps, restLaps)
}

// detectUniformRepeats checks if all laps have similar duration (uniform intervals
// without distinct rest periods, e.g. track repeats with walk-back recovery not
// recorded as separate laps).
func detectUniformRepeats(pw *ParsedWorkout) string {
	laps := trimOutlierLaps(pw.Laps)
	if len(laps) < 3 {
		return ""
	}

	if !lapsConsistent(laps, 0.15) {
		return ""
	}

	// Do not generate an interval tag for auto-lapped steady-pace runs.
	// COROS and similar watches create a lap every 1km automatically, which would
	// otherwise be tagged as "10x1km" even though the run had no interval structure.
	if isDistanceSport(pw.Sport) && isAutoLap(laps) {
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

	workStr := formatDuration(avgWorkDur)

	// For distance-based sports, prefer distance format when it's a recognizable
	// distance and the duration isn't a clean minute value (e.g. "400m" over "1m30s",
	// but keep "6m" instead of "1200m").
	if isDistanceSport(pw.Sport) {
		avgDist := avgDistance(workLaps)
		if avgDist > 0 && distancesConsistent(workLaps, 0.15) {
			distStr := formatDistance(avgDist)
			durClean := int(math.Round(avgWorkDur))%60 == 0
			if distStr != "" && !durClean {
				workStr = distStr
			}
		}
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

// isAutoLap returns true when the laps appear to be automatic device-generated lap
// boundaries (e.g. a watch auto-lapping every 1km) rather than manually pressed lap
// buttons marking interval boundaries. Two signals are used:
//
//  1. FIT LapTrigger field: if any lap was manually triggered, this is a structured
//     workout; if all laps were distance-triggered, it is auto-lap.
//  2. Pace coefficient of variation fallback: when trigger data is absent, a very low
//     CV (< 4%) indicates a steady-pace run, not a structured interval session.
func isAutoLap(laps []ParsedLap) bool {
	if len(laps) == 0 {
		return false
	}

	// Explicit manual trigger on any lap means structured intervals.
	for _, lap := range laps {
		if lap.LapTrigger == "manual" {
			return false
		}
	}

	// All laps distance-triggered → device auto-lap confirmed.
	allDistance := true
	for _, lap := range laps {
		if lap.LapTrigger != "distance" {
			allDistance = false
			break
		}
	}
	if allDistance {
		return true
	}

	// No trigger data available — fall back to speed CV.
	// Steady-speed runs have very low CV; interval sessions have meaningful variation.
	return lapSpeedCVBelow(laps, 0.04)
}

// lapSpeedCVBelow returns true when the coefficient of variation of lap speed is below
// the given threshold. Laps without valid speed/distance data are skipped.
func lapSpeedCVBelow(laps []ParsedLap, threshold float64) bool {
	var speeds []float64
	for _, lap := range laps {
		var speed float64
		if lap.AvgSpeedMPerS > 0 {
			speed = lap.AvgSpeedMPerS
		} else if lap.DistanceMeters > 0 && lap.DurationSeconds > 0 {
			speed = lap.DistanceMeters / lap.DurationSeconds
		}
		if speed > 0 {
			speeds = append(speeds, speed)
		}
	}
	if len(speeds) < 2 {
		return false
	}

	var sum float64
	for _, s := range speeds {
		sum += s
	}
	mean := sum / float64(len(speeds))
	if mean == 0 {
		return false
	}

	var varSum float64
	for _, s := range speeds {
		diff := s - mean
		varSum += diff * diff
	}
	cv := math.Sqrt(varSum/float64(len(speeds))) / mean
	return cv < threshold
}
