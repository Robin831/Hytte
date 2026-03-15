package lactate

import (
	"fmt"
	"math"
)

// RaceDistance defines a standard race distance.
type RaceDistance struct {
	Name       string  `json:"name"`
	DistanceKm float64 `json:"distance_km"`
}

// StandardDistances lists common race distances for predictions.
var StandardDistances = []RaceDistance{
	{"1500m", 1.5},
	{"3000m", 3.0},
	{"5K", 5.0},
	{"10K", 10.0},
	{"Half Marathon", 21.0975},
	{"Marathon", 42.195},
}

// RacePrediction holds the predicted time for a race distance.
type RacePrediction struct {
	Name          string  `json:"name"`
	DistanceKm    float64 `json:"distance_km"`
	TimeSeconds   float64 `json:"time_seconds"`
	TimeFormatted string  `json:"time_formatted"`
	PaceMinKm     string  `json:"pace_min_km"`
	SpeedKmh      float64 `json:"speed_kmh"`
}

// PredictionsResult holds race predictions and the threshold used.
type PredictionsResult struct {
	ThresholdSpeed float64          `json:"threshold_speed_kmh"`
	Method         string           `json:"method"`
	Predictions    []RacePrediction `json:"predictions"`
}

// riegelExponent is the standard Riegel formula exponent for distance running.
const riegelExponent = 1.06

// PredictRaceTimes uses the lactate threshold speed to predict race times.
//
// The threshold speed (OBLA/4 mmol/L) approximates the pace sustainable for
// ~60 minutes. Using Riegel's formula: T2 = T1 * (D2/D1)^1.06
func PredictRaceTimes(thresholdSpeedKmh float64) []RacePrediction {
	if thresholdSpeedKmh <= 0 {
		return nil
	}

	// Reference: threshold speed ≈ 1-hour race effort
	refDistanceKm := thresholdSpeedKmh // distance covered in 1 hour at threshold
	refTimeSeconds := 3600.0           // 1 hour = 3600 seconds

	predictions := make([]RacePrediction, len(StandardDistances))
	for i, d := range StandardDistances {
		// Riegel formula
		timeSeconds := refTimeSeconds * math.Pow(d.DistanceKm/refDistanceKm, riegelExponent)
		avgSpeedKmh := d.DistanceKm / (timeSeconds / 3600.0)
		paceSecondsPerKm := timeSeconds / d.DistanceKm

		predictions[i] = RacePrediction{
			Name:          d.Name,
			DistanceKm:    d.DistanceKm,
			TimeSeconds:   round2(timeSeconds),
			TimeFormatted: formatDuration(timeSeconds),
			PaceMinKm:     formatPace(paceSecondsPerKm),
			SpeedKmh:      round2(avgSpeedKmh),
		}
	}

	return predictions
}

// formatDuration converts seconds to H:MM:SS or M:SS format.
func formatDuration(seconds float64) string {
	total := int(math.Round(seconds))
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatPace converts seconds per km to M:SS/km format.
func formatPace(secondsPerKm float64) string {
	total := int(math.Round(secondsPerKm))
	m := total / 60
	s := total % 60
	return fmt.Sprintf("%d:%02d/km", m, s)
}
