// Package hrzones provides a single source of truth for HR zone boundaries.
// It defines the default Olympiatoppen 5-zone model based on max HR percentage,
// and allows users to store custom zone boundaries in user_preferences.
package hrzones

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
)

// ZoneBoundary defines the heart rate boundaries for a single training zone.
type ZoneBoundary struct {
	Zone   int `json:"zone"`
	MinBPM int `json:"min_bpm"`
	MaxBPM int `json:"max_bpm"`
}

// olympiatoppenPcts defines the Olympiatoppen 5-zone model as fractions of max HR.
// These percentages are consistent with the lactate zone model in internal/lactate/zones.go
// and represent the canonical zone boundaries when no lactate test data is available.
var olympiatoppenPcts = []struct {
	minPct float64
	maxPct float64
}{
	{0.00, 0.60}, // Zone 1 - Recovery
	{0.60, 0.72}, // Zone 2 - Easy aerobic / endurance base
	{0.72, 0.82}, // Zone 3 - Moderate / high aerobic
	{0.82, 0.92}, // Zone 4 - Threshold
	{0.92, 1.00}, // Zone 5 - VO2max / high intensity
}

// GetDefaultZones computes the Olympiatoppen 5-zone HR boundaries from max HR.
// Returns nil if maxHR is zero or negative.
func GetDefaultZones(maxHR int) []ZoneBoundary {
	if maxHR <= 0 {
		return nil
	}
	zones := make([]ZoneBoundary, len(olympiatoppenPcts))
	for i, p := range olympiatoppenPcts {
		zones[i] = ZoneBoundary{
			Zone:   i + 1,
			MinBPM: int(math.Round(float64(maxHR) * p.minPct)),
			MaxBPM: int(math.Round(float64(maxHR) * p.maxPct)),
		}
	}
	return zones
}

// GetUserZones returns the HR zone boundaries for a user. It first checks for
// custom zone_boundaries stored in user_preferences (as a JSON array). If none
// are stored, it falls back to GetDefaultZones using the user's max_hr preference.
// Returns (nil, nil) if no zones can be computed (no custom zones and no max HR).
func GetUserZones(db *sql.DB, userID int64) ([]ZoneBoundary, error) {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return nil, fmt.Errorf("get user preferences: %w", err)
	}

	// Use custom zone boundaries if the user has set them.
	if raw, ok := prefs["zone_boundaries"]; ok && raw != "" {
		var zones []ZoneBoundary
		if err := json.Unmarshal([]byte(raw), &zones); err != nil {
			return nil, fmt.Errorf("parse zone_boundaries: %w", err)
		}
		return zones, nil
	}

	// Fall back to default zones computed from max HR.
	maxHR := 0
	if raw, ok := prefs["max_hr"]; ok && raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("parse max_hr preference: %w", err)
		}
		maxHR = parsed
	}
	return GetDefaultZones(maxHR), nil
}
