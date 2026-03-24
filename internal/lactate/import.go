package lactate

import (
	"fmt"
	"strconv"
	"strings"
)

// SpeedLactatePair is a parsed speed/lactate data point from pasted input.
type SpeedLactatePair struct {
	SpeedKmh    float64 `json:"speed_kmh"`
	LactateMmol float64 `json:"lactate_mmol"`
}

// ImportOptions configures how workout lap data is matched to lactate pairs.
type ImportOptions struct {
	// WarmupDurationMin is the expected warmup duration to skip when matching by duration.
	WarmupDurationMin int
	// StageDurationMin is the expected stage duration for duration-based matching.
	StageDurationMin int
	// HRWindowSeconds is the trailing window (seconds) at the end of each stage
	// used to average the heart rate.
	HRWindowSeconds int
}

// ProposedStage is a matched stage ready to be reviewed before saving.
type ProposedStage struct {
	StageNumber  int     `json:"stage_number"`
	SpeedKmh     float64 `json:"speed_kmh"`
	LactateMmol  float64 `json:"lactate_mmol"`
	HeartRateBpm int     `json:"heart_rate_bpm"`
	LapNumber    int     `json:"lap_number"`
}

// ImportResult holds the outcome of extracting stage data from a workout.
type ImportResult struct {
	Stages   []ProposedStage `json:"stages"`
	Warnings []string        `json:"warnings"`
	// Method is "speed" when laps were matched by pace, "duration" when matched
	// by expected stage duration after skipping warmup.
	Method string `json:"method"`
}

// ImportLap contains the lap fields required for HR extraction.  Callers
// populate this from training.Lap to avoid a package import cycle (the
// training package already imports lactate for threshold context).
type ImportLap struct {
	LapNumber       int
	StartOffsetMs   int64
	DurationSeconds float64
	// AvgPaceSecPerKm is used to derive average speed (3600 / pace = km/h).
	// Zero means speed is unknown.
	AvgPaceSecPerKm float64
}

// ImportSample is a single time-series HR observation required for extraction.
type ImportSample struct {
	OffsetMs  int64
	HeartRate int
}

// ParseLactateInput parses pasted text into speed/lactate pairs.
//
// Supported formats (one pair per line):
//   - "10.5 2.3"    — space-separated, dot decimal
//   - "10,5 2,3"    — space-separated, Norwegian comma decimal
//   - "10.5/2.3"    — slash-separated
//   - "10,5/2,3"    — slash-separated, Norwegian comma decimal
//   - "10,5,2,3"    — four-token comma format (Norwegian: speed=10.5, lactate=2.3)
//   - "10.5,2.3"    — comma-separated, dot decimal
//
// Validation rules:
//   - At least 2 pairs required.
//   - Speeds must be in the range 5–25 km/h.
//   - Lactate values must be positive (> 0).
//   - Speeds must be strictly increasing across pairs.
func ParseLactateInput(text string) ([]SpeedLactatePair, error) {
	var pairs []SpeedLactatePair

	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		speed, lactate, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("invalid line %q: %w", line, err)
		}

		pairs = append(pairs, SpeedLactatePair{SpeedKmh: speed, LactateMmol: lactate})
	}

	if len(pairs) < 2 {
		return nil, fmt.Errorf("need at least 2 pairs, got %d", len(pairs))
	}

	for i, p := range pairs {
		if p.SpeedKmh < 5 || p.SpeedKmh > 25 {
			return nil, fmt.Errorf("speed %.2f km/h at index %d is outside valid range 5–25 km/h", p.SpeedKmh, i)
		}
		if p.LactateMmol <= 0 {
			return nil, fmt.Errorf("lactate %.2f at index %d must be positive", p.LactateMmol, i)
		}
	}

	for i := 1; i < len(pairs); i++ {
		if pairs[i].SpeedKmh <= pairs[i-1].SpeedKmh {
			return nil, fmt.Errorf(
				"speeds must be strictly increasing: %.2f followed by %.2f at index %d",
				pairs[i-1].SpeedKmh, pairs[i].SpeedKmh, i,
			)
		}
	}

	return pairs, nil
}

// parseLine parses a single text line into a (speed, lactate) pair.
func parseLine(line string) (float64, float64, error) {
	var tokens []string

	switch {
	case strings.Contains(line, "/"):
		// Slash separator: "10.5/2.3" or "10,5/2,3"
		parts := strings.SplitN(line, "/", 2)
		tokens = []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}

	case len(strings.Fields(line)) >= 2:
		// Whitespace separator: "10.5 2.3" or "10,5 2,3"
		fields := strings.Fields(line)
		tokens = []string{fields[0], fields[1]}

	default:
		// Comma-only: "10.5,2.3" or "10,5,2,3" (Norwegian four-token)
		parts := strings.Split(line, ",")
		switch len(parts) {
		case 2:
			// "10.5,2.3"
			tokens = parts
		case 4:
			// Norwegian format: "10,5,2,3" → speed=10.5, lactate=2.3
			tokens = []string{
				parts[0] + "." + parts[1],
				parts[2] + "." + parts[3],
			}
		default:
			return 0, 0, fmt.Errorf("cannot parse %q: unexpected number of comma-separated tokens (%d)", line, len(parts))
		}
	}

	speed, err := parseDecimal(tokens[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse speed %q: %w", tokens[0], err)
	}
	lactate, err := parseDecimal(tokens[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse lactate %q: %w", tokens[1], err)
	}

	return speed, lactate, nil
}

// parseDecimal parses a floating-point number, accepting both dot and comma as
// the decimal separator (Norwegian locale convention).
func parseDecimal(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

// speedMatchTolerance is the maximum km/h difference for a speed-based lap match.
const speedMatchTolerance = 0.4

// lapSpeedKmh converts a lap's average pace (sec/km) to km/h.
func lapSpeedKmh(lap ImportLap) float64 {
	if lap.AvgPaceSecPerKm <= 0 {
		return 0
	}
	return 3600.0 / lap.AvgPaceSecPerKm
}

// avgHRInWindow returns the average heart rate for samples that fall within
// [windowStartMs, windowEndMs] (both inclusive).  Returns 0 if no samples
// with a valid HR exist in the window.
func avgHRInWindow(samples []ImportSample, windowStartMs, windowEndMs int64) int {
	sum, count := 0, 0
	for _, s := range samples {
		if s.OffsetMs >= windowStartMs && s.OffsetMs <= windowEndMs && s.HeartRate > 0 {
			sum += s.HeartRate
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

// ExtractStageHR matches parsed lactate pairs to workout laps and extracts the
// average heart rate over the last HRWindowSeconds of each matched lap.
//
// Laps and samples use the ImportLap/ImportSample types (mirrors of
// training.Lap/training.Sample) to avoid a package import cycle.
//
// Matching strategy:
//  1. Speed-based (primary): each pair is matched to a lap whose average speed
//     is within speedMatchTolerance km/h of the pair's speed.
//  2. Duration-based (fallback): when speed matching finds fewer matches than
//     pairs, laps are matched in order after skipping the warmup.
func ExtractStageHR(laps []ImportLap, samples []ImportSample, pairs []SpeedLactatePair, opts ImportOptions) (*ImportResult, error) {
	if len(laps) == 0 {
		return nil, fmt.Errorf("workout has no laps")
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("workout has no time-series samples")
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no lactate pairs provided")
	}

	if opts.HRWindowSeconds <= 0 {
		opts.HRWindowSeconds = 30
	}
	hrWindowMs := int64(opts.HRWindowSeconds) * 1000

	// Attempt speed-based matching first.
	stages, warnings, method := matchBySpeed(laps, samples, pairs, hrWindowMs)
	if len(stages) < len(pairs) {
		// Speed matching did not find all pairs; fall back to duration ordering.
		stages, warnings, method = matchByDuration(laps, samples, pairs, opts, hrWindowMs)
	}

	return &ImportResult{
		Stages:   stages,
		Warnings: warnings,
		Method:   method,
	}, nil
}

// matchBySpeed attempts to match each pair to a lap with a similar average speed.
func matchBySpeed(
	laps []ImportLap,
	samples []ImportSample,
	pairs []SpeedLactatePair,
	hrWindowMs int64,
) ([]ProposedStage, []string, string) {
	var stages []ProposedStage
	var warnings []string
	usedLaps := make(map[int]bool)

	for i, pair := range pairs {
		bestIdx := -1
		bestDiff := speedMatchTolerance + 1 // start above tolerance

		for j, lap := range laps {
			if usedLaps[j] {
				continue
			}
			diff := pair.SpeedKmh - lapSpeedKmh(lap)
			if diff < 0 {
				diff = -diff
			}
			if diff < bestDiff {
				bestDiff = diff
				bestIdx = j
			}
		}

		if bestIdx < 0 || bestDiff > speedMatchTolerance {
			warnings = append(warnings, fmt.Sprintf(
				"no lap matched speed %.2f km/h for pair %d (tolerance %.1f km/h)",
				pair.SpeedKmh, i+1, speedMatchTolerance,
			))
			continue
		}

		usedLaps[bestIdx] = true
		lap := laps[bestIdx]
		endMs := lap.StartOffsetMs + int64(lap.DurationSeconds*1000)
		startMs := endMs - hrWindowMs
		if startMs < lap.StartOffsetMs {
			startMs = lap.StartOffsetMs
		}

		hr := avgHRInWindow(samples, startMs, endMs)
		if hr == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"no HR samples found in last %d s of lap %d for pair %d",
				hrWindowMs/1000, laps[bestIdx].LapNumber, i+1,
			))
		}

		stages = append(stages, ProposedStage{
			StageNumber:  i + 1,
			SpeedKmh:     pair.SpeedKmh,
			LactateMmol:  pair.LactateMmol,
			HeartRateBpm: hr,
			LapNumber:    lap.LapNumber,
		})
	}

	return stages, warnings, "speed"
}

// matchByDuration matches pairs to laps in order, skipping warmup laps based
// on cumulative elapsed duration.
func matchByDuration(
	laps []ImportLap,
	samples []ImportSample,
	pairs []SpeedLactatePair,
	opts ImportOptions,
	hrWindowMs int64,
) ([]ProposedStage, []string, string) {
	var stages []ProposedStage
	var warnings []string

	// Skip laps whose cumulative duration falls within the warmup window.
	// Also skip laps shorter than StageDurationMin (if set) — these are
	// transition or auto-laps, not genuine test stages.
	warmupMs := int64(opts.WarmupDurationMin) * 60 * 1000
	stageDurationMs := int64(opts.StageDurationMin) * 60 * 1000
	var stageLaps []ImportLap
	var elapsed int64
	for _, lap := range laps {
		if elapsed >= warmupMs {
			lapMs := int64(lap.DurationSeconds * 1000)
			if stageDurationMs <= 0 || lapMs >= stageDurationMs {
				stageLaps = append(stageLaps, lap)
			}
		}
		elapsed += int64(lap.DurationSeconds * 1000)
	}

	if len(stageLaps) < len(pairs) {
		warnings = append(warnings, fmt.Sprintf(
			"only %d laps remain after warmup skip, but %d pairs were provided; using available laps",
			len(stageLaps), len(pairs),
		))
	}

	limit := len(pairs)
	if len(stageLaps) < limit {
		limit = len(stageLaps)
	}

	for i := 0; i < limit; i++ {
		pair := pairs[i]
		lap := stageLaps[i]

		endMs := lap.StartOffsetMs + int64(lap.DurationSeconds*1000)
		startMs := endMs - hrWindowMs
		if startMs < lap.StartOffsetMs {
			startMs = lap.StartOffsetMs
		}

		hr := avgHRInWindow(samples, startMs, endMs)
		if hr == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"no HR samples found in last %d s of lap %d for pair %d",
				hrWindowMs/1000, lap.LapNumber, i+1,
			))
		}

		stages = append(stages, ProposedStage{
			StageNumber:  i + 1,
			SpeedKmh:     pair.SpeedKmh,
			LactateMmol:  pair.LactateMmol,
			HeartRateBpm: hr,
			LapNumber:    lap.LapNumber,
		})
	}

	return stages, warnings, "duration"
}
