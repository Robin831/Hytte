package skywatch

import (
	"encoding/json"
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
func parseCoords(r *http.Request) (float64, float64) {
	lat := DefaultLat
	lon := DefaultLon

	if v := r.URL.Query().Get("lat"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			lat = parsed
		}
	}
	if v := r.URL.Query().Get("lon"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			lon = parsed
		}
	}
	return lat, lon
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
		now := time.Now()
		lat, lon := parseCoords(r)

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
		days := 30
		if v := r.URL.Query().Get("days"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 365 {
				days = parsed
			}
		}

		now := time.Now()
		lat, lon := parseCoords(r)
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
