package skywatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// NowResponse is the JSON response for GET /api/skywatch/now.
type NowResponse struct {
	Timestamp string   `json:"timestamp"`
	Location  Location `json:"location"`
	Moon      MoonInfo `json:"moon"`
	Sun       SunTimes `json:"sun"`
}

// MoonCalendarDay is one entry in the moon phase calendar.
type MoonCalendarDay struct {
	Date         string  `json:"date"`
	Phase        string  `json:"phase"`
	Illumination float64 `json:"illumination"`
	PhaseValue   float64 `json:"phase_value"`
}

// Location holds the coordinates used for calculations.
type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// parseCoords extracts lat/lon from query parameters, falling back to defaults.
// Both lat and lon must be provided together; if only one is given, an error is returned.
// Latitude must be in [-90, 90] and longitude in [-180, 180].
func parseCoords(r *http.Request) (float64, float64, error) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")

	// Neither provided — use defaults.
	if latStr == "" && lonStr == "" {
		return DefaultLat, DefaultLon, nil
	}

	// One provided without the other.
	if latStr == "" || lonStr == "" {
		return 0, 0, fmt.Errorf("both lat and lon must be provided together")
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lat value")
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lon value")
	}

	if lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("lat must be between -90 and 90")
	}
	if lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("lon must be between -180 and 180")
	}

	return lat, lon, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// NowHandler returns the current moon phase and sun times.
// GET /api/skywatch/now?lat=...&lon=...
func NowHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, lon, err := parseCoords(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		now := time.Now()
		resp := NowResponse{
			Timestamp: now.Format(time.RFC3339),
			Location:  Location{Lat: lat, Lon: lon},
			Moon:      GetMoonPhase(now, lat, lon),
			Sun:       GetSunTimes(now, lat, lon),
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// MoonCalendarHandler returns a moon phase calendar for the given number of days.
// GET /api/skywatch/moon?days=30&lat=...&lon=...
func MoonCalendarHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, lon, err := parseCoords(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		days := 30
		if v := r.URL.Query().Get("days"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 365 {
				days = parsed
			}
		}

		now := time.Now()
		calendar := make([]MoonCalendarDay, days)

		for i := range days {
			date := now.AddDate(0, 0, i)
			// Use noon for consistent daily calculations.
			noon := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, date.Location())
			illum := GetMoonPhase(noon, lat, lon)
			calendar[i] = MoonCalendarDay{
				Date:         date.Format("2006-01-02"),
				Phase:        illum.Phase,
				Illumination: illum.Illumination,
				PhaseValue:   illum.PhaseValue,
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"location": Location{Lat: lat, Lon: lon},
			"days":     days,
			"calendar": calendar,
		})
	}
}
