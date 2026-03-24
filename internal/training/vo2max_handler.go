package training

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
)

// trendWindow is the number of recent estimates used to compute the VO2max trend.
const trendWindow = 5

// GetVO2maxHandler handles GET /api/training/vo2max.
// Returns the user's VO2max history and a trend derived from the last trendWindow estimates.
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

		trend := computeVO2maxTrend(history)

		writeJSON(w, http.StatusOK, map[string]any{
			"history": history,
			"latest":  latest,
			"trend":   trend,
		})
	}
}

// computeVO2maxTrend derives a trend string from the most recent estimates.
// It uses a simple linear regression slope over the last trendWindow estimates
// (in chronological order) and returns "improving", "declining", or "stable".
// Returns "stable" when there are fewer than 2 data points.
func computeVO2maxTrend(history []VO2maxEstimate) string {
	n := len(history)
	if n < 2 {
		return "stable"
	}

	// Use the last trendWindow estimates; history is already chronological (ASC).
	window := history
	if n > trendWindow {
		window = history[n-trendWindow:]
	}

	// Linear regression: slope = (n*Σxy - Σx*Σy) / (n*Σx² - (Σx)²)
	nf := float64(len(window))
	var sumX, sumY, sumXY, sumX2 float64
	for i, e := range window {
		x := float64(i)
		y := e.VO2max
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return "stable"
	}
	slope := (nf*sumXY - sumX*sumY) / denom

	// Threshold: 0.3 mL/kg/min per step is meaningful change.
	const threshold = 0.3
	switch {
	case slope > threshold:
		return "improving"
	case slope < -threshold:
		return "declining"
	default:
		return "stable"
	}
}
