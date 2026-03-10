package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Location represents a Norwegian location with coordinates.
type Location struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// NorwegianLocations maps city names to their coordinates.
var NorwegianLocations = map[string]Location{
	"Oslo":          {Name: "Oslo", Lat: 59.9139, Lon: 10.7522},
	"Bergen":        {Name: "Bergen", Lat: 60.3913, Lon: 5.3221},
	"Trondheim":     {Name: "Trondheim", Lat: 63.4305, Lon: 10.3951},
	"Stavanger":     {Name: "Stavanger", Lat: 58.9700, Lon: 5.7331},
	"Tromsø":        {Name: "Tromsø", Lat: 69.6492, Lon: 18.9553},
	"Kristiansand":  {Name: "Kristiansand", Lat: 58.1599, Lon: 8.0182},
	"Drammen":       {Name: "Drammen", Lat: 59.7441, Lon: 10.2045},
	"Fredrikstad":   {Name: "Fredrikstad", Lat: 59.2181, Lon: 10.9298},
	"Bodø":          {Name: "Bodø", Lat: 67.2804, Lon: 14.4049},
	"Ålesund":       {Name: "Ålesund", Lat: 62.4722, Lon: 6.1495},
	"Lillehammer":   {Name: "Lillehammer", Lat: 61.1153, Lon: 10.4662},
	"Haugesund":     {Name: "Haugesund", Lat: 59.4138, Lon: 5.2680},
	"Molde":         {Name: "Molde", Lat: 62.7375, Lon: 7.1591},
	"Narvik":        {Name: "Narvik", Lat: 68.4385, Lon: 17.4272},
	"Alta":          {Name: "Alta", Lat: 69.9689, Lon: 23.2716},
}

const (
	metBaseURL     = "https://api.met.no/weatherapi/locationforecast/2.0/compact"
	metUserAgent   = "Hytte/1.0 github.com/Robin831/Hytte"
	cacheDuration  = 30 * time.Minute
	maxResponseSize = 2 << 20 // 2 MB
)

// cachedResponse holds a cached MET API response.
type cachedResponse struct {
	data      []byte
	expires   time.Time
	fetchedAt time.Time
}

// Service holds weather-related state (cache, HTTP client, base URL).
type Service struct {
	client  *http.Client
	baseURL string

	mu    sync.RWMutex
	cache map[string]*cachedResponse
}

// NewService creates a weather service with production defaults.
func NewService() *Service {
	return &Service{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: metBaseURL,
		cache:   make(map[string]*cachedResponse),
	}
}

// newTestService creates a weather service pointing at a test server.
func newTestService(baseURL string) *Service {
	return &Service{
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: baseURL,
		cache:   make(map[string]*cachedResponse),
	}
}

// ForecastHandler returns weather forecast data for a given Norwegian location.
// Query params: location (city name, default "Oslo")
func (s *Service) ForecastHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationName := r.URL.Query().Get("location")
		if locationName == "" {
			locationName = "Oslo"
		}

		loc, ok := NorwegianLocations[locationName]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "unknown location",
			})
			return
		}

		data, err := s.fetchForecast(loc)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "failed to fetch weather data",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// LocationsHandler returns the list of available locations sorted by name.
func LocationsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locations := make([]Location, 0, len(NorwegianLocations))
		for _, loc := range NorwegianLocations {
			locations = append(locations, loc)
		}
		sort.Slice(locations, func(i, j int) bool {
			return locations[i].Name < locations[j].Name
		})
		writeJSON(w, http.StatusOK, map[string]any{"locations": locations})
	}
}

// fetchForecast retrieves the forecast from MET Norway, using an in-memory cache.
func (s *Service) fetchForecast(loc Location) ([]byte, error) {
	cacheKey := fmt.Sprintf("%.4f,%.4f", loc.Lat, loc.Lon)

	// Hold RLock while checking expiry and copying fields to avoid data races.
	s.mu.RLock()
	cached, ok := s.cache[cacheKey]
	if ok && time.Now().Before(cached.expires) {
		data := cached.data
		s.mu.RUnlock()
		return data, nil
	}
	// Copy fields we need before releasing the lock.
	var hasCached bool
	var cachedFetchedAt time.Time
	var cachedData []byte
	if ok {
		hasCached = true
		cachedFetchedAt = cached.fetchedAt
		cachedData = cached.data
	}
	s.mu.RUnlock()

	url := fmt.Sprintf("%s?lat=%.4f&lon=%.4f", s.baseURL, loc.Lat, loc.Lon)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", metUserAgent)

	// Forward If-Modified-Since if we have a cached response.
	if hasCached {
		req.Header.Set("If-Modified-Since", cachedFetchedAt.UTC().Format(http.TimeFormat))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		// If fetch fails but we have stale cache, return it.
		if hasCached {
			return cachedData, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	// 304 Not Modified — extend cache.
	if resp.StatusCode == http.StatusNotModified && hasCached {
		s.mu.Lock()
		if c := s.cache[cacheKey]; c != nil {
			c.expires = time.Now().Add(cacheDuration)
		}
		s.mu.Unlock()
		return cachedData, nil
	}

	if resp.StatusCode != http.StatusOK {
		if hasCached {
			s.mu.Lock()
			if c := s.cache[cacheKey]; c != nil {
				c.expires = time.Now().Add(5 * time.Minute)
			}
			s.mu.Unlock()
			return cachedData, nil
		}
		return nil, fmt.Errorf("MET API returned status %d", resp.StatusCode)
	}

	limitedReader := &io.LimitedReader{
		R: resp.Body,
		N: maxResponseSize + 1,
	}
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxResponseSize {
		return nil, fmt.Errorf("response body too large (>%d bytes)", maxResponseSize)
	}

	s.mu.Lock()
	s.cache[cacheKey] = &cachedResponse{
		data:      body,
		expires:   time.Now().Add(cacheDuration),
		fetchedAt: time.Now(),
	}
	s.mu.Unlock()

	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
