package kiosk

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/netatmo"
	"github.com/Robin831/Hytte/internal/transit"
	"github.com/Robin831/Hytte/internal/weather"
)

// SunTimes holds today's sunrise and sunset times for the kiosk location.
// Kind is "normal", "polarDay", or "polarNight".
type SunTimes struct {
	Kind    string `json:"kind"`
	Sunrise string `json:"sunrise,omitempty"` // RFC3339, present when Kind == "normal"
	Sunset  string `json:"sunset,omitempty"`  // RFC3339, present when Kind == "normal"
}

// KioskData is the aggregated response returned by GET /api/kiosk/data.
type KioskData struct {
	Transit   []transit.StopDepartures `json:"transit"`
	Outdoor   *netatmo.OutdoorReadings `json:"outdoor,omitempty"`
	Forecast  json.RawMessage          `json:"forecast,omitempty"`
	Sun       *SunTimes                `json:"sun,omitempty"`
	FetchedAt time.Time                `json:"fetched_at"`
}

// DataHandler returns the aggregated kiosk data endpoint handler.
// It reads stop IDs, location, and Netatmo user from the KioskConfig injected
// by KioskAuth, then fans out to transit, Netatmo, weather, and sun time
// sources concurrently.
func DataHandler(db *sql.DB, transitSvc *transit.Service, netatmoClient *netatmo.Client, weatherSvc *weather.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := GetKioskConfig(r.Context())

		stopIDs := extractStringSlice(cfg, "stop_ids")
		lat, hasLat := extractFloat(cfg, "lat")
		lon, hasLon := extractFloat(cfg, "lon")
		locationName, _ := cfg["location"].(string)
		netatmoUserID, _ := extractInt64(cfg, "netatmo_user_id")

		var (
			mu     sync.Mutex
			wg     sync.WaitGroup
			result KioskData
		)
		result.FetchedAt = time.Now()
		result.Transit = []transit.StopDepartures{}

		// --- Transit departures ---
		if len(stopIDs) > 0 && transitSvc != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stops := make([]transit.StopDepartures, 0, len(stopIDs))
				for _, id := range stopIDs {
					stopName, departures, err := transitSvc.FetchDepartures(r.Context(), id, 10)
					if err != nil {
						continue
					}
					stops = append(stops, transit.StopDepartures{
						StopID:     id,
						StopName:   stopName,
						Departures: departures,
					})
				}
				mu.Lock()
				result.Transit = stops
				mu.Unlock()
			}()
		}

		// --- Netatmo outdoor readings ---
		if netatmoUserID > 0 && netatmoClient != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				readings, err := netatmoClient.GetStationsData(r.Context(), netatmoUserID)
				if err != nil {
					return
				}
				mu.Lock()
				result.Outdoor = readings.Outdoor
				mu.Unlock()
			}()
		}

		// --- Resolve location for weather and sun ---
		var loc *weather.Location
		if hasLat && hasLon {
			l := weather.Location{Name: locationName, Lat: lat, Lon: lon}
			loc = &l
		} else if locationName != "" {
			if l, ok := weather.NorwegianLocations[locationName]; ok {
				loc = &l
			}
		}

		// --- Weather forecast ---
		if loc != nil && weatherSvc != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				data, err := weatherSvc.FetchForecast(*loc)
				if err != nil {
					return
				}
				mu.Lock()
				result.Forecast = json.RawMessage(data)
				mu.Unlock()
			}()
		}

		// --- Sun times (computed from lat/lon) ---
		if loc != nil {
			capturedLoc := *loc
			wg.Add(1)
			go func() {
				defer wg.Done()
				sun := computeSunTimes(capturedLoc.Lat, capturedLoc.Lon, time.Now())
				mu.Lock()
				result.Sun = sun
				mu.Unlock()
			}()
		}

		wg.Wait()

		writeJSON(w, http.StatusOK, result)
	}
}

// computeSunTimes computes sunrise and sunset for the given lat/lon and local date.
// Uses the NOAA simplified algorithm — accurate to within ~1 minute for most locations.
// Returns a SunTimes with Kind "polarDay" or "polarNight" for extreme latitudes.
func computeSunTimes(lat, lon float64, t time.Time) *SunTimes {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		oslo = time.UTC
	}
	local := t.In(oslo)
	year, month, day := local.Date()
	// Build startOfDay as local Oslo midnight to avoid selecting the wrong UTC
	// midnight for dates near midnight (including DST transitions).
	localMidnight := time.Date(year, month, day, 0, 0, 0, 0, oslo)
	startOfDay := localMidnight.In(time.UTC)

	// Days since J2000.0 (2000-01-01 12:00 UTC = Unix 946728000s = 10957.5 days)
	n := float64(startOfDay.Unix())/86400.0 - 10957.5

	// Mean longitude and anomaly (degrees)
	L := mod360(280.46 + 0.9856474*n)
	g := mod360(357.528 + 0.9856003*n)
	gRad := g * math.Pi / 180.0

	// Ecliptic longitude (degrees)
	lambda := L + 1.915*math.Sin(gRad) + 0.02*math.Sin(2*gRad)
	lambdaRad := lambda * math.Pi / 180.0

	// Obliquity of the ecliptic (degrees)
	epsilon := 23.439 - 0.0000004*n
	epsilonRad := epsilon * math.Pi / 180.0

	// Sun's declination
	sinDec := math.Sin(epsilonRad) * math.Sin(lambdaRad)
	dec := math.Asin(sinDec)

	// Hour angle for sunrise/sunset (-0.8333° accounts for atmospheric refraction)
	latRad := lat * math.Pi / 180.0
	cosH := (math.Sin(-0.8333*math.Pi/180.0) - math.Sin(latRad)*sinDec) /
		(math.Cos(latRad) * math.Cos(dec))

	if cosH > 1 {
		return &SunTimes{Kind: "polarNight"}
	}
	if cosH < -1 {
		return &SunTimes{Kind: "polarDay"}
	}

	H := math.Acos(cosH) * 180.0 / math.Pi

	// Equation of time (minutes). Use day-of-year to avoid leap-year drift.
	dayOfYear := float64(local.YearDay())
	B := (360.0 / 365.0) * (dayOfYear - 81.0) * math.Pi / 180.0
	eot := 9.87*math.Sin(2*B) - 7.53*math.Cos(B) - 1.5*math.Sin(B)

	// Solar noon in minutes from midnight UTC
	solarNoonUTC := 720 - 4*lon - eot

	sunriseMin := solarNoonUTC - H*4
	sunsetMin := solarNoonUTC + H*4

	// Convert computed UTC instants back to the local timezone before formatting
	// so the RFC3339 strings reflect local time (e.g. "+01:00") rather than "Z".
	sunrise := startOfDay.Add(time.Duration(sunriseMin * float64(time.Minute))).In(oslo)
	sunset := startOfDay.Add(time.Duration(sunsetMin * float64(time.Minute))).In(oslo)

	return &SunTimes{
		Kind:    "normal",
		Sunrise: sunrise.Format(time.RFC3339),
		Sunset:  sunset.Format(time.RFC3339),
	}
}

func mod360(v float64) float64 {
	return math.Mod(math.Mod(v, 360)+360, 360)
}

// extractStringSlice reads a string slice from a KioskConfig map entry.
// JSON unmarshalling produces []any for arrays, so both []string and []any are handled.
func extractStringSlice(cfg KioskConfig, key string) []string {
	v, ok := cfg[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// extractFloat reads a float64 from a KioskConfig map entry.
// JSON numbers unmarshal as float64, but integer types are also accepted.
func extractFloat(cfg KioskConfig, key string) (float64, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// extractInt64 reads an int64 from a KioskConfig map entry.
// JSON numbers unmarshal as float64; fractional or out-of-range values are rejected.
func extractInt64(cfg KioskConfig, key string) (int64, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case float64:
		if val != math.Trunc(val) {
			return 0, false
		}
		if val > math.MaxInt64 || val < math.MinInt64 {
			return 0, false
		}
		return int64(val), true
	case int64:
		return val, true
	case int:
		return int64(val), true
	case json.Number:
		i, err := val.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}
