package training

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// Race distances in metres.
const (
	raceDistance5K           = 5000.0
	raceDistance10K          = 10000.0
	raceDistanceHalfMarathon = 21097.5
	raceDistanceMarathon     = 42195.0
)

// RacePrediction holds a predicted finish time for a single race distance.
type RacePrediction struct {
	Distance      string  `json:"distance"`
	DistanceM     float64 `json:"distance_m"`
	PredictedTime string  `json:"predicted_time"`
	PacePerKm     string  `json:"pace_per_km"`
}

// RacePredictions holds all race time predictions derived from a reference effort.
type RacePredictions struct {
	RefDistance  string           `json:"ref_distance"`
	RefTime      string           `json:"ref_time"`
	RefWorkoutID *int64           `json:"ref_workout_id,omitempty"`
	Method       string           `json:"method"`
	Predictions  []RacePrediction `json:"predictions"`
}

// riegelPredict applies the Riegel formula: T2 = T1 * (D2/D1)^1.06.
func riegelPredict(t1Seconds, d1Metres, d2Metres float64) float64 {
	return t1Seconds * math.Pow(d2Metres/d1Metres, 1.06)
}

// formatRaceTime formats a duration in seconds as H:MM:SS (≥1 hour) or MM:SS.
func formatRaceTime(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatPacePerKm formats a pace value in seconds-per-km as M:SS.
func formatPacePerKm(secPerKm float64) string {
	total := int(math.Round(secPerKm))
	m := total / 60
	s := total % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

// PredictRaceTimes generates 5K/10K/Half Marathon/Marathon predictions using the
// Riegel formula (T2 = T1*(D2/D1)^1.06).
//
// recentThresholdPaceSecPerKm is treated as half-marathon race pace, which is a
// common approximation of threshold pace for club runners.
//
// recentVO2max is accepted for API symmetry with the VO2max sub-task and reserved
// for a future VDOT-based prediction path; pass 0 to use the pace-only path.
//
// Returns nil when thresholdPaceSecPerKm is zero or negative.
func PredictRaceTimes(recentVO2max float64, recentThresholdPaceSecPerKm float64) *RacePredictions {
	if recentThresholdPaceSecPerKm <= 0 {
		return nil
	}

	// Threshold pace ≈ half-marathon race pace.
	refDistM := raceDistanceHalfMarathon
	refTimeS := recentThresholdPaceSecPerKm * (refDistM / 1000.0)

	type raceSpec struct {
		name string
		m    float64
	}
	races := []raceSpec{
		{"5K", raceDistance5K},
		{"10K", raceDistance10K},
		{"Half Marathon", raceDistanceHalfMarathon},
		{"Marathon", raceDistanceMarathon},
	}

	predictions := make([]RacePrediction, 0, len(races))
	for _, race := range races {
		t := riegelPredict(refTimeS, refDistM, race.m)
		tSec := int(math.Round(t))
		predictions = append(predictions, RacePrediction{
			Distance:      race.name,
			DistanceM:     race.m,
			PredictedTime: formatRaceTime(tSec),
			PacePerKm:     formatPacePerKm(t / (race.m / 1000.0)),
		})
	}

	return &RacePredictions{
		RefDistance: "Half Marathon",
		RefTime:     formatRaceTime(int(math.Round(refTimeS))),
		Method:      "threshold_pace",
		Predictions: predictions,
	}
}

// FindBestThresholdWorkout returns the fastest threshold or tempo running workout
// from the last 3 months for the given user. It first looks for workouts tagged
// "ai:type:tempo" or "ai:type:threshold", then falls back to the fastest sustained
// running effort (≥20 min) in that window.
//
// Returns (nil, sql.ErrNoRows) when no suitable workout is found.
func FindBestThresholdWorkout(db *sql.DB, userID int64) (*Workout, error) {
	since := time.Now().UTC().AddDate(0, -3, 0).Format(time.RFC3339)

	// Prefer workouts explicitly tagged as tempo or threshold.
	row := db.QueryRow(`
		SELECT DISTINCT w.id, w.user_id, w.sport, w.duration_seconds,
		       w.distance_meters, w.avg_pace_sec_per_km, w.started_at
		FROM workouts w
		JOIN workout_tags wt ON wt.workout_id = w.id
		WHERE w.user_id = ?
		  AND w.sport = 'running'
		  AND w.started_at >= ?
		  AND w.avg_pace_sec_per_km IS NOT NULL
		  AND w.avg_pace_sec_per_km > 0
		  AND w.duration_seconds >= 1200
		  AND (wt.tag LIKE 'ai:type:tempo%' OR wt.tag LIKE 'ai:type:threshold%')
		ORDER BY w.avg_pace_sec_per_km ASC
		LIMIT 1`,
		userID, since,
	)

	w, err := scanThresholdWorkout(row)
	if err == nil {
		return w, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Fallback: fastest sustained running workout of at least 20 minutes.
	row = db.QueryRow(`
		SELECT id, user_id, sport, duration_seconds, distance_meters,
		       avg_pace_sec_per_km, started_at
		FROM workouts
		WHERE user_id = ?
		  AND sport = 'running'
		  AND started_at >= ?
		  AND avg_pace_sec_per_km IS NOT NULL
		  AND avg_pace_sec_per_km > 0
		  AND duration_seconds >= 1200
		ORDER BY avg_pace_sec_per_km ASC
		LIMIT 1`,
		userID, since,
	)

	return scanThresholdWorkout(row)
}

// scanThresholdWorkout scans a minimal Workout from a single DB row.
func scanThresholdWorkout(row *sql.Row) (*Workout, error) {
	var w Workout
	err := row.Scan(
		&w.ID,
		&w.UserID,
		&w.Sport,
		&w.DurationSeconds,
		&w.DistanceMeters,
		&w.AvgPaceSecPerKm,
		&w.StartedAt,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}
