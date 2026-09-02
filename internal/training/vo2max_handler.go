package training

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
)

// vo2maxTrendMinChange is how far the fitted line must move, end to end across
// the window, before a direction is reported instead of "stable". Expressing
// the threshold as a total change rather than a per-step slope keeps it
// meaningful at any window length: a per-step threshold tuned for a handful of
// estimates would demand more than vo2maxNoisySpread across a full window, so
// no run tight enough to be readable could ever register a direction.
const vo2maxTrendMinChange = 1.0

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
// vo2maxSummaryWindow estimates, and a trend derived from those same estimates.
func GetVO2maxHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		history, err := GetVO2maxHistory(db, user.ID, 50)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load VO2max history"})
			return
		}

		// "latest" is no longer read by the trends card (which renders the
		// median and range instead); it is kept in the response because it is
		// part of the published API shape other clients may still read.
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
// the min-to-max spread reaches vo2maxNoisySpread the trend is reported as
// "noisy" rather than a direction. Otherwise the trend is a linear regression
// over the same estimates, returning "improving", "declining" or "stable".
//
// The window, the median/range arithmetic (vo2maxStats) and the noise threshold
// are shared with the race predictor's narrative (formatVO2maxSummary, which is
// fed the same most-recent vo2maxSummaryWindow estimates), so the trends card
// and the race prediction rendered on the same page report the same numbers and
// agree about whether the estimates are noise. The handler loads a longer
// history for the chart, so it is narrowed here first.
//
// Returns "stable" with a nil summary when there is nothing usable to report,
// and "stable" with a summary when there is a single estimate.
func computeVO2maxTrend(history []VO2maxEstimate) (string, *vo2maxSummary) {
	// history is chronological (ASC), so the newest estimates are at the end.
	if len(history) > vo2maxSummaryWindow {
		history = history[len(history)-vo2maxSummaryWindow:]
	}

	kept, median, lo, hi := vo2maxStats(history)
	if len(kept) == 0 {
		return "stable", nil
	}

	summary := &vo2maxSummary{
		Count:  len(kept),
		Median: median,
		Low:    lo,
		High:   hi,
		Spread: hi - lo,
		Noisy:  hi-lo >= vo2maxNoisySpread,
	}

	if len(kept) < 2 {
		return "stable", summary
	}
	if summary.Noisy {
		return "noisy", summary
	}

	// Linear regression: slope = (n*Σxy - Σx*Σy) / (n*Σx² - (Σx)²)
	nf := float64(len(kept))
	var sumX, sumY, sumXY, sumX2 float64
	for i, e := range kept {
		x := float64(i)
		sumX += x
		sumY += e.VO2max
		sumXY += x * e.VO2max
		sumX2 += x * x
	}

	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return "stable", summary
	}
	slope := (nf*sumXY - sumX*sumY) / denom

	// vo2maxTrendMinChange across the window, converted to a per-step slope.
	threshold := vo2maxTrendMinChange / (nf - 1)
	switch {
	case slope > threshold:
		return "improving", summary
	case slope < -threshold:
		return "declining", summary
	default:
		return "stable", summary
	}
}
