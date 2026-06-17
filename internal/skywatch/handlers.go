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
	Timestamp  string       `json:"timestamp"`
	Location   Location     `json:"location"`
	Moon       MoonInfo     `json:"moon"`
	Sun        SunTimes     `json:"sun"`
	Planets    []PlanetInfo `json:"planets"`
	Highlights []Highlight  `json:"highlights"`
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

// Response cache TTLs and their matching Cache-Control max-age values.
const (
	nowCacheTTL  = 5 * time.Minute
	moonCacheTTL = time.Hour
)

var (
	// nowCache caches NowHandler responses keyed by lat/lon/date.
	nowCache = newTTLCache(nowCacheTTL)
	// moonCache caches MoonCalendarHandler responses keyed by lat/lon/days.
	moonCache = newTTLCache(moonCacheTTL)
)

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

// writeCachedJSON writes pre-serialized JSON bytes with a matching
// Cache-Control: public, max-age=<ttl> header.
func writeCachedJSON(w http.ResponseWriter, data []byte, ttl time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// NowHandler returns the current moon phase and sun times.
// GET /api/skywatch/now?lat=...&lon=...&date=YYYY-MM-DD
// The optional date parameter allows fetching data for a specific date (defaults to today).
func NowHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, lon, err := parseCoords(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		t := time.Now()
		if dateStr := r.URL.Query().Get("date"); dateStr != "" {
			parsed, err := time.ParseInLocation("2006-01-02", dateStr, t.Location())
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
				return
			}
			// Use noon of the requested date for consistent calculations.
			t = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 12, 0, 0, 0, t.Location())
		}

		// Cache key incorporates all inputs that affect output. The no-date path
		// resolves to today's date so repeat visits within the TTL are cached too.
		key := fmt.Sprintf("%v|%v|%s", lat, lon, t.Format("2006-01-02"))
		if data, ok := nowCache.get(key); ok {
			writeCachedJSON(w, data, nowCacheTTL)
			return
		}

		// Compute planet positions once to avoid redundant rise/set calculations.
		planets := GetPlanetPositions(t, lat, lon)
		highlights := GetTonightHighlights(t, lat, lon, planets)
		if highlights == nil {
			highlights = []Highlight{}
		}

		resp := NowResponse{
			Timestamp:  t.Format(time.RFC3339),
			Location:   Location{Lat: lat, Lon: lon},
			Moon:       GetMoonPhase(t, lat, lon),
			Sun:        GetSunTimes(t, lat, lon),
			Planets:    planets,
			Highlights: highlights,
		}

		data, err := json.Marshal(resp)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode response"})
			return
		}
		nowCache.set(key, data)
		writeCachedJSON(w, data, nowCacheTTL)
	}
}

// MoonCalendarHandler returns a moon phase calendar for the given number of days.
// GET /api/skywatch/moon?days=30&lat=...&lon=...&date=YYYY-MM-DD
// The optional date parameter sets the first day of the calendar (defaults to today).
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

		start := time.Now()
		if dateStr := r.URL.Query().Get("date"); dateStr != "" {
			parsed, err := time.ParseInLocation("2006-01-02", dateStr, start.Location())
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
				return
			}
			// Use noon of the requested date for consistent calculations.
			start = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 12, 0, 0, 0, start.Location())
		}

		// Cache key incorporates all inputs that affect output. The calendar is
		// anchored on the start day, so include the date to avoid serving a stale run.
		key := fmt.Sprintf("%v|%v|%d|%s", lat, lon, days, start.Format("2006-01-02"))
		if data, ok := moonCache.get(key); ok {
			writeCachedJSON(w, data, moonCacheTTL)
			return
		}

		calendar := make([]MoonCalendarDay, days)

		for i := range days {
			date := start.AddDate(0, 0, i)
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

		data, err := json.Marshal(map[string]any{
			"location": Location{Lat: lat, Lon: lon},
			"days":     days,
			"calendar": calendar,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode response"})
			return
		}
		moonCache.set(key, data)
		writeCachedJSON(w, data, moonCacheTTL)
	}
}
