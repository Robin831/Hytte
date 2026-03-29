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

// olympiatoppenPcts defines the default Olympiatoppen-inspired 5-zone model as
// fractions of max HR. These are independent of the threshold-based lactate zones
// in internal/lactate/zones.go and act as canonical defaults when no custom zones
// or lactate test data are available.
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
		if err := validateZoneBoundaries(zones); err != nil {
			return nil, fmt.Errorf("invalid zone_boundaries: %w", err)
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

// ZoneName returns the canonical display name for a zone number (1-based, 1–5).
// Returns an empty string for out-of-range zone numbers.
func ZoneName(zone int) string {
	names := [5]string{"Recovery", "Aerobic", "Moderate", "Threshold", "VO2max"}
	if zone < 1 || zone > 5 {
		return ""
	}
	return names[zone-1]
}

// validateZoneBoundaries checks that zones 1–5 are each present exactly once,
// that min_bpm < max_bpm for each zone, and that boundaries are monotonically
// increasing (each zone's min_bpm >= the previous zone's max_bpm).
func validateZoneBoundaries(zones []ZoneBoundary) error {
	if len(zones) != 5 {
		return fmt.Errorf("must contain exactly 5 zones, got %d", len(zones))
	}
	seen := make(map[int]bool, 5)
	for _, z := range zones {
		if z.Zone < 1 || z.Zone > 5 {
			return fmt.Errorf("zone number %d out of range 1–5", z.Zone)
		}
		if seen[z.Zone] {
			return fmt.Errorf("zone %d appears more than once", z.Zone)
		}
		seen[z.Zone] = true
		if z.MinBPM < 0 || z.MaxBPM <= z.MinBPM {
			return fmt.Errorf("zone %d: max_bpm (%d) must be greater than min_bpm (%d)", z.Zone, z.MaxBPM, z.MinBPM)
		}
	}
	for zn := 1; zn <= 5; zn++ {
		if !seen[zn] {
			return fmt.Errorf("zone %d is missing", zn)
		}
	}
	// Build ordered slice and check monotonic boundaries.
	ordered := make([]ZoneBoundary, 5)
	for _, z := range zones {
		ordered[z.Zone-1] = z
	}
	for i := 1; i < 5; i++ {
		if ordered[i].MinBPM < ordered[i-1].MaxBPM {
			return fmt.Errorf("zone %d min_bpm (%d) must be >= zone %d max_bpm (%d)", i+1, ordered[i].MinBPM, i, ordered[i-1].MaxBPM)
		}
	}
	return nil
}
