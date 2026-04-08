package skywatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// NOAA SWPC planetary Kp index — 3-day forecast in JSON.
	noaaKpForecastURL = "https://services.swpc.noaa.gov/products/noaa-planetary-k-index-forecast.json"
	// NOAA SWPC estimated Kp — recent observed values.
	noaaKpObservedURL = "https://services.swpc.noaa.gov/products/noaa-planetary-k-index.json"

	auroraCacheDuration = 15 * time.Minute
	auroraMaxResponse   = 512 << 10 // 512 KB
	noaaUserAgent       = "Hytte/1.0 github.com/Robin831/Hytte"
)

// AuroraService fetches and caches NOAA Kp index data.
type AuroraService struct {
	client *http.Client

	mu    sync.RWMutex
	cache map[string]*auroraCached

	group singleflight.Group
}

type auroraCached struct {
	data    []byte
	expires time.Time
}

// NewAuroraService creates a new aurora forecast service.
func NewAuroraService() *AuroraService {
	return &AuroraService{
		client: &http.Client{Timeout: 10 * time.Second},
		cache:  make(map[string]*auroraCached),
	}
}

// AuroraForecast is the JSON response for GET /api/skywatch/aurora.
type AuroraForecast struct {
	CurrentKp     *float64       `json:"current_kp"`
	MaxKpTonight  *float64       `json:"max_kp_tonight"`
	Probability   string         `json:"probability"`
	BestTime      string         `json:"best_time"`
	BestDirection string         `json:"best_direction"`
	Entries       []KpEntry      `json:"entries"`
	Location      AuroraLocation `json:"location"`
}

// AuroraLocation holds the observer's geomagnetic context.
type AuroraLocation struct {
	Lat            float64 `json:"lat"`
	Lon            float64 `json:"lon"`
	GeomagneticLat float64 `json:"geomagnetic_lat"`
	MinKpForAurora float64 `json:"min_kp_for_aurora"`
}

// KpEntry is a single Kp index data point.
type KpEntry struct {
	TimeTag string  `json:"time_tag"`
	Kp      float64 `json:"kp"`
	Source  string  `json:"source"`
}

// AuroraHandler returns the current aurora forecast.
// GET /api/skywatch/aurora?lat=...&lon=...
func (s *AuroraService) AuroraHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, lon, err := parseCoords(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		forecast, err := s.getAuroraForecast(r.Context(), lat, lon)
		if err != nil {
			log.Printf("skywatch: aurora forecast error: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch aurora data"})
			return
		}

		writeJSON(w, http.StatusOK, forecast)
	}
}

func (s *AuroraService) getAuroraForecast(ctx context.Context, lat, lon float64) (*AuroraForecast, error) {
	observedData, err := s.fetchCached(ctx, "observed", noaaKpObservedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch observed: %w", err)
	}

	forecastData, err := s.fetchCached(ctx, "forecast", noaaKpForecastURL)
	if err != nil {
		return nil, fmt.Errorf("fetch forecast: %w", err)
	}

	observed, err := parseKpData(observedData)
	if err != nil {
		return nil, fmt.Errorf("parse observed: %w", err)
	}

	forecasted, err := parseKpData(forecastData)
	if err != nil {
		return nil, fmt.Errorf("parse forecast: %w", err)
	}

	return buildAuroraForecast(observed, forecasted, lat, lon), nil
}

func (s *AuroraService) fetchCached(ctx context.Context, key, url string) ([]byte, error) {
	val, err, _ := s.group.Do(key, func() (interface{}, error) {
		s.mu.RLock()
		if c, ok := s.cache[key]; ok && time.Now().Before(c.expires) {
			data := c.data
			s.mu.RUnlock()
			return data, nil
		}
		s.mu.RUnlock()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", noaaUserAgent)

		resp, err := s.client.Do(req)
		if err != nil {
			if data := s.fallbackStaleCache(key); data != nil {
				return data, nil
			}
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			if data := s.fallbackStaleCache(key); data != nil {
				return data, nil
			}
			return nil, fmt.Errorf("NOAA API returned status %d", resp.StatusCode)
		}

		lr := &io.LimitedReader{R: resp.Body, N: auroraMaxResponse + 1}
		body, err := io.ReadAll(lr)
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > auroraMaxResponse {
			return nil, fmt.Errorf("NOAA response too large (>%d bytes)", auroraMaxResponse)
		}

		s.mu.Lock()
		s.cache[key] = &auroraCached{data: body, expires: time.Now().Add(auroraCacheDuration)}
		s.mu.Unlock()

		return body, nil
	})

	if err != nil {
		return nil, err
	}
	return val.([]byte), nil
}

// fallbackStaleCache returns stale cached data if available, extending the TTL
// to avoid hammering a failing upstream on every subsequent request.
func (s *AuroraService) fallbackStaleCache(key string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[key]; ok {
		c.expires = time.Now().Add(auroraCacheDuration)
		return c.data
	}
	return nil
}

// parseKpData parses NOAA Kp JSON arrays. The first row is a header; skip it.
func parseKpData(data []byte) ([]KpEntry, error) {
	var raw [][]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	if len(raw) < 2 {
		return nil, fmt.Errorf("insufficient data rows")
	}

	var entries []KpEntry
	for _, row := range raw[1:] {
		if len(row) < 2 {
			continue
		}

		var timeTag string
		if err := json.Unmarshal(row[0], &timeTag); err != nil {
			continue
		}

		var kp float64
		if err := json.Unmarshal(row[1], &kp); err != nil {
			// Try as string (some endpoints return Kp as "2.33").
			var kpStr string
			if err2 := json.Unmarshal(row[1], &kpStr); err2 != nil {
				continue
			}
			if _, err2 := fmt.Sscanf(kpStr, "%f", &kp); err2 != nil {
				continue
			}
		}

		source := "observed"
		if len(row) >= 3 {
			var src string
			if err := json.Unmarshal(row[2], &src); err == nil && src != "" {
				source = src
			}
		}

		entries = append(entries, KpEntry{
			TimeTag: timeTag,
			Kp:      kp,
			Source:  source,
		})
	}

	return entries, nil
}

// buildAuroraForecast computes the aurora probability from Kp data and location.
func buildAuroraForecast(observed, forecasted []KpEntry, lat, lon float64) *AuroraForecast {
	geomagLat := ApproximateGeomagneticLat(lat, lon)
	minKp := MinKpForAurora(geomagLat)

	// Current Kp: last observed value.
	var currentKp *float64
	if len(observed) > 0 {
		last := observed[len(observed)-1].Kp
		currentKp = &last
	}

	// Tonight's forecast window: 18:00 – 06:00 in observer's approximate local time.
	// Approximate timezone from longitude (1 hour per 15 degrees).
	utcNow := time.Now().UTC()
	offsetSec := int(math.Round(lon/15)) * 3600
	localZone := time.FixedZone("obs", offsetSec)
	localNow := utcNow.In(localZone)

	tonightStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 18, 0, 0, 0, localZone)
	tomorrowMorning := tonightStart.Add(12 * time.Hour)

	// If it's already past midnight local, shift window back.
	if localNow.Hour() < 6 {
		tonightStart = tonightStart.Add(-24 * time.Hour)
		tomorrowMorning = tomorrowMorning.Add(-24 * time.Hour)
	}

	// Convert window to UTC for comparison with NOAA timestamps.
	tonightStart = tonightStart.UTC()
	tomorrowMorning = tomorrowMorning.UTC()

	var maxKp float64
	var maxKpSet bool
	var tonightEntries []KpEntry

	for _, e := range forecasted {
		t, err := time.Parse("2006-01-02 15:04:05.000", e.TimeTag)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", e.TimeTag)
			if err != nil {
				continue
			}
		}
		if !t.Before(tonightStart) && t.Before(tomorrowMorning) {
			tonightEntries = append(tonightEntries, e)
			if !maxKpSet || e.Kp > maxKp {
				maxKp = e.Kp
				maxKpSet = true
			}
		}
	}

	// Use current observed Kp if we're in the viewing window and have no forecast.
	if !maxKpSet && currentKp != nil {
		h := localNow.Hour()
		if h >= 18 || h < 6 {
			maxKp = *currentKp
			maxKpSet = true
		}
	}

	var maxKpPtr *float64
	if maxKpSet {
		maxKpPtr = &maxKp
	}

	probability := ClassifyProbability(maxKp, minKp, maxKpSet)

	// Build combined entries: last few observed + tonight forecast.
	var entries []KpEntry
	if len(observed) > 8 {
		entries = append(entries, observed[len(observed)-8:]...)
	} else {
		entries = append(entries, observed...)
	}
	entries = append(entries, tonightEntries...)

	bestDirection := "N"
	if geomagLat < 0 {
		bestDirection = "S"
	}

	return &AuroraForecast{
		CurrentKp:     currentKp,
		MaxKpTonight:  maxKpPtr,
		Probability:   probability,
		BestTime:      "23:00–02:00",
		BestDirection: bestDirection,
		Entries:       entries,
		Location: AuroraLocation{
			Lat:            lat,
			Lon:            lon,
			GeomagneticLat: math.Round(geomagLat*10) / 10,
			MinKpForAurora: minKp,
		},
	}
}

// ClassifyProbability returns "unlikely", "possible", or "likely" based on
// the maximum expected Kp and the minimum Kp needed at the observer's location.
func ClassifyProbability(maxKp, minKp float64, hasData bool) string {
	if !hasData {
		return "unknown"
	}
	switch {
	case maxKp >= minKp:
		return "likely"
	case maxKp >= minKp-1:
		return "possible"
	default:
		return "unlikely"
	}
}

// ApproximateGeomagneticLat converts geographic to approximate geomagnetic latitude
// using a centered dipole model (geomagnetic pole at ~80.5N, 72.6W).
func ApproximateGeomagneticLat(lat, lon float64) float64 {
	const (
		poleLat = 80.5  // Geomagnetic north pole latitude
		poleLon = -72.6 // Geomagnetic north pole longitude
	)

	latR := lat * math.Pi / 180
	lonR := lon * math.Pi / 180
	poleLatR := poleLat * math.Pi / 180
	poleLonR := poleLon * math.Pi / 180

	// Spherical law of cosines for angular distance from geomagnetic pole.
	cosTheta := math.Sin(latR)*math.Sin(poleLatR) + math.Cos(latR)*math.Cos(poleLatR)*math.Cos(lonR-poleLonR)
	cosTheta = math.Max(-1, math.Min(1, cosTheta))

	// Geomagnetic colatitude → latitude.
	colatitude := math.Acos(cosTheta) * 180 / math.Pi
	return 90 - colatitude
}

// MinKpForAurora returns the minimum Kp index needed to see aurora at the given
// geomagnetic latitude. Based on empirical NOAA auroral oval data.
func MinKpForAurora(geomagLat float64) float64 {
	absLat := math.Abs(geomagLat)

	switch {
	case absLat >= 67:
		return 1
	case absLat >= 65:
		return 2
	case absLat >= 63:
		return 3
	case absLat >= 60:
		return 4
	case absLat >= 57:
		return 5
	case absLat >= 55:
		return 6
	case absLat >= 52:
		return 7
	case absLat >= 50:
		return 8
	default:
		return 9
	}
}
