package training

import (
	"regexp"
	"time"
)

// intervalTagRe matches auto-generated interval tags such as "auto:6x6m (r1m)" or "auto:10x400m".
var intervalTagRe = regexp.MustCompile(`^auto:\d+x`)

// VO2maxEstimate holds a single VO2max estimation derived from a workout.
type VO2maxEstimate struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"user_id"`
	WorkoutID   int64   `json:"workout_id"`
	VO2max      float64 `json:"vo2max"`
	Method      string  `json:"method"` // "daniels" or "hr_ratio"
	EstimatedAt string  `json:"estimated_at"`
}

// EstimateVO2max attempts to estimate VO2max from a steady-state workout.
//
// It applies the Daniels/Gilbert velocity-based formula as the primary method, using
// heart rate reserve (Swain et al.) to derive the fraction of VO2max. When restingHR
// is nil the formula falls back to a direct %HRmax approximation.
//
// When pace data is absent or insufficient, the Uth HR-ratio formula
// (VO2max = 15.3 × HRmax/HRrest) is used as a fallback — this requires restingHR.
//
// Returns nil, nil when the workout should be skipped (intervals detected, duration
// under 15 minutes, or HR data insufficient for any method).
func EstimateVO2max(w *Workout, restingHR *int) (*VO2maxEstimate, error) {
	// Skip workouts shorter than 15 minutes.
	if w.DurationSeconds < 15*60 {
		return nil, nil
	}

	// Skip interval and hill repeat workouts detected by the auto-tagger.
	for _, tag := range w.Tags {
		if intervalTagRe.MatchString(tag) {
			return nil, nil
		}
	}

	// Resolve max HR: prefer workout max, fall back to package-level default.
	maxHR := w.MaxHeartRate
	if maxHR <= 0 {
		maxHR = defaultMaxHR
	}

	// Primary: Daniels/Gilbert velocity-based estimation.
	if w.DistanceMeters > 0 && w.DurationSeconds > 0 && w.AvgHeartRate > 0 {
		if est := danielsEstimate(w, maxHR, restingHR); est != nil {
			return est, nil
		}
	}

	// Fallback: Uth HR-ratio formula (requires resting HR).
	if restingHR != nil && *restingHR > 0 && maxHR > 0 {
		vo2max := 15.3 * float64(maxHR) / float64(*restingHR)
		if vo2max >= 20 && vo2max <= 85 {
			estimatedAt := w.StartedAt
			if estimatedAt == "" {
				estimatedAt = time.Now().UTC().Format(time.RFC3339)
			}
			return &VO2maxEstimate{
				UserID:      w.UserID,
				WorkoutID:   w.ID,
				VO2max:      roundVO2max(vo2max),
				Method:      "hr_ratio",
				EstimatedAt: estimatedAt,
			}, nil
		}
	}

	return nil, nil
}

// danielsEstimate applies the Daniels/Gilbert submaximal formula.
// Returns nil when the result falls outside a physiologically plausible range.
func danielsEstimate(w *Workout, maxHR int, restingHR *int) *VO2maxEstimate {
	// Velocity in m/min.
	v := (w.DistanceMeters / float64(w.DurationSeconds)) * 60

	// Oxygen cost at velocity (Daniels 2005, adapted from Gilbert & Daniels).
	vo2AtPace := -4.60 + 0.182258*v + 0.000104*v*v
	if vo2AtPace <= 0 {
		return nil
	}

	// Fraction of VO2max (%VO2max / 100).
	var pctVO2max float64
	if restingHR != nil && *restingHR > 0 && maxHR > *restingHR {
		// Swain et al. (1994): %VO2max = 1.54 × %HRR − 0.54
		pctHRR := float64(w.AvgHeartRate-*restingHR) / float64(maxHR-*restingHR)
		pctVO2max = 1.54*pctHRR - 0.54
	} else {
		// Direct %HRmax approximation when resting HR is unknown.
		pctVO2max = float64(w.AvgHeartRate) / float64(maxHR)
	}

	// Guard against out-of-range HR values producing nonsensical results.
	if pctVO2max <= 0.3 || pctVO2max > 1.0 {
		return nil
	}

	vo2max := vo2AtPace / pctVO2max

	// Plausibility check: typical human range is roughly 20–85 mL/kg/min.
	if vo2max < 20 || vo2max > 85 {
		return nil
	}

	estimatedAt := w.StartedAt
	if estimatedAt == "" {
		estimatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return &VO2maxEstimate{
		UserID:      w.UserID,
		WorkoutID:   w.ID,
		VO2max:      roundVO2max(vo2max),
		Method:      "daniels",
		EstimatedAt: estimatedAt,
	}
}

// roundVO2max rounds a VO2max value to one decimal place.
func roundVO2max(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
