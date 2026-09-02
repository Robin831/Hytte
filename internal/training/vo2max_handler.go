package training

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"

	"github.com/Robin831/Hytte/internal/auth"
)

// trendWindow is the number of recent estimates used to compute the VO2max trend.
const trendWindow = 5

// vo2maxSummary describes the recent per-workout estimates the trend is derived
// from: a robust centre (median) plus the min-to-max range, so the UI can show
// how much scatter sits behind the number instead of a bare latest value.
//
// Noisy is set when Spread reaches vo2maxNoisySpread, which is the point where
// the run of estimates is scatter rather than fitness change — see the constant
// in prediction.go. When it is set the trend is suppressed ("noisy") because
// there is no slope worth reading.
type vo2maxSummary struct {
	Count  int     `json:"count"`
	Median float64 `json:"median"`
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
	Spread float64 `json:"spread"`
	Noisy  bool    `json:"noisy"`
}

// GetVO2maxHandler handles GET /api/training/vo2max.
// Returns the user's VO2max history, a median-plus-range summary of the last
// trendWindow estimates, and a trend derived from those same estimates.
func GetVO2maxHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		history, err := GetVO2maxHistory(db, user.ID, 50)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load VO2max history"})
			return
		}

		latest, err := GetLatestVO2max(db, user.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load latest VO2max"})
			return
		}

		trend, summary := computeVO2maxTrend(history)

		writeJSON(w, http.StatusOK, map[string]any{
			"history": history,
			"latest":  latest,
			"trend":   trend,
			"summary": summary,
		})
	}
}

// computeVO2maxTrend summarises the most recent estimates and derives a trend
// from them.
//
// Per-workout VO2max is an estimator, not a measurement: it reacts to terrain,
// heat, HR artefacts and pacing, so a run like 37-56 has no slope to read. When
// the min-to-max spread of the window reaches vo2maxNoisySpread the trend is
// reported as "noisy" rather than a direction — the same treatment the race
// predictor gives these estimates (see formatVO2maxSummary). Otherwise the
// trend is a linear regression slope over the window, returning "improving",
// "declining" or "stable".
//
// Returns "stable" with a nil summary when there is nothing usable to report,
// and "stable" with a summary when there is a single estimate.
func computeVO2maxTrend(history []VO2maxEstimate) (string, *vo2maxSummary) {
	vals := make([]float64, 0, len(history))
	for _, e := range history {
		if e.VO2max > 0 {
			vals = append(vals, e.VO2max)
		}
	}
	if len(vals) == 0 {
		return "stable", nil
	}

	// Use the last trendWindow estimates; history is already chronological (ASC).
	window := vals
	if len(window) > trendWindow {
		window = window[len(window)-trendWindow:]
	}

	sorted := append([]float64(nil), window...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	lo, hi := sorted[0], sorted[len(sorted)-1]
	summary := &vo2maxSummary{
		Count:  len(window),
		Median: median,
		Low:    lo,
		High:   hi,
		Spread: hi - lo,
		Noisy:  hi-lo >= vo2maxNoisySpread,
	}

	if len(window) < 2 {
		return "stable", summary
	}
	if summary.Noisy {
		return "noisy", summary
	}

	// Linear regression: slope = (n*Σxy - Σx*Σy) / (n*Σx² - (Σx)²)
	nf := float64(len(window))
	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range window {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return "stable", summary
	}
	slope := (nf*sumXY - sumX*sumY) / denom

	// Threshold: 0.3 mL/kg/min per step is meaningful change.
	const threshold = 0.3
	switch {
	case slope > threshold:
		return "improving", summary
	case slope < -threshold:
		return "declining", summary
	default:
		return "stable", summary
	}
}
