package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
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
	metBaseURL          = "https://api.met.no/weatherapi/locationforecast/2.0/compact"
	metUserAgent        = "Hytte/1.0 github.com/Robin831/Hytte"
	cacheDuration       = 30 * time.Minute
	maxResponseSize     = 2 << 20 // 2 MB
	maxNominatimSize    = 512 << 10 // 512 KB — more than enough for 5 results
)

// cachedResponse holds a cached MET API response.
type cachedResponse struct {
	data      []byte
	expires   time.Time
	fetchedAt time.Time
}

// Service holds weather-related state (cache, HTTP client, base URLs).
type Service struct {
	client       *http.Client
	baseURL      string
	nominatimURL string

	mu    sync.RWMutex
	cache map[string]*cachedResponse

	requestGroup singleflight.Group
}

// NewService creates a weather service with production defaults.
func NewService() *Service {
	return &Service{
		client:       &http.Client{Timeout: 10 * time.Second},
		baseURL:      metBaseURL,
		nominatimURL: nominatimBaseURL,
		cache:        make(map[string]*cachedResponse),
	}
}

// newTestService creates a weather service pointing at a test MET server.
func newTestService(baseURL string) *Service {
	return &Service{
		client:       &http.Client{Timeout: 5 * time.Second},
		baseURL:      baseURL,
		nominatimURL: nominatimBaseURL,
		cache:        make(map[string]*cachedResponse),
	}
}

// newTestSearchService creates a weather service pointing at a test Nominatim server.
func newTestSearchService(nominatimURL string) *Service {
	return &Service{
		client:       &http.Client{Timeout: 5 * time.Second},
		baseURL:      metBaseURL,
		nominatimURL: nominatimURL,
		cache:        make(map[string]*cachedResponse),
	}
}

// ForecastHandler returns weather forecast data for a given location.
// Query params: location (city name, default "Oslo"), or lat + lon for arbitrary coordinates.
// When lat and lon are provided, they take precedence over the location name lookup.
func (s *Service) ForecastHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		latStr := r.URL.Query().Get("lat")
		lonStr := r.URL.Query().Get("lon")
		locationName := r.URL.Query().Get("location")

		var loc Location

		if latStr != "" && lonStr != "" {
			lat, err := strconv.ParseFloat(latStr, 64)
			if err != nil || lat < -90 || lat > 90 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "invalid latitude",
				})
				return
			}
			lon, err := strconv.ParseFloat(lonStr, 64)
			if err != nil || lon < -180 || lon > 180 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "invalid longitude",
				})
				return
			}
			loc = Location{Name: locationName, Lat: lat, Lon: lon}
		} else if latStr != "" || lonStr != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "lat and lon must be provided together",
			})
			return
		} else {
			if locationName == "" {
				locationName = "Oslo"
			}
			var ok bool
			loc, ok = NorwegianLocations[locationName]
			if !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "unknown location",
				})
				return
			}
		}

		data, err := s.fetchForecastWithStampedeProtection(loc)
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

const nominatimBaseURL = "https://nominatim.openstreetmap.org/search"

// nominatimResult matches the JSON returned by Nominatim's search endpoint.
type nominatimResult struct {
	DisplayName string          `json:"display_name"`
	Lat         string          `json:"lat"`
	Lon         string          `json:"lon"`
	Address     nominatimAddress `json:"address"`
}

type nominatimAddress struct {
	Hamlet       string `json:"hamlet"`
	Suburb       string `json:"suburb"`
	Neighbourhood string `json:"neighbourhood"`
	Quarter      string `json:"quarter"`
	Village      string `json:"village"`
	Town         string `json:"town"`
	City         string `json:"city"`
	Municipality string `json:"municipality"`
	County       string `json:"county"`
	Country      string `json:"country"`
}

// SearchResult is returned to the frontend for each Nominatim hit.
type SearchResult struct {
	Name    string `json:"name"`
	Context string `json:"context,omitempty"`
	Country string `json:"country"`
	Lat     string `json:"lat"`
	Lon     string `json:"lon"`
}

// SearchHandler handles free-text location searches via Nominatim.
// Query params: q (search query, required, max 100 chars)
func (s *Service) SearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "q parameter is required",
			})
			return
		}
		if len(q) > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "q parameter must not exceed 100 characters",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		searchURL := s.nominatimURL + "?q=" + url.QueryEscape(q) + "&format=json&limit=5&addressdetails=1"
		req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to build search request",
			})
			return
		}
		req.Header.Set("User-Agent", metUserAgent)

		resp, err := s.client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "location search failed",
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("Nominatim returned status %d", resp.StatusCode),
			})
			return
		}

		limitedReader := &io.LimitedReader{
			R: resp.Body,
			N: maxNominatimSize + 1,
		}
		body, err := io.ReadAll(limitedReader)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "failed to read search results",
			})
			return
		}
		if int64(len(body)) > maxNominatimSize {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "search response too large",
			})
			return
		}

		var raw []nominatimResult
		if err := json.Unmarshal(body, &raw); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "failed to parse search results",
			})
			return
		}

		results := make([]SearchResult, 0, len(raw))
		for _, item := range raw {
			// Use the most specific available place name as the primary label.
			name := item.Address.Hamlet
			if name == "" {
				name = item.Address.Suburb
			}
			if name == "" {
				name = item.Address.Neighbourhood
			}
			if name == "" {
				name = item.Address.Quarter
			}
			if name == "" {
				name = item.Address.Village
			}
			if name == "" {
				name = item.Address.Town
			}
			if name == "" {
				name = item.Address.City
			}
			if name == "" {
				name = item.Address.County
			}
			if name == "" {
				name = item.DisplayName
			}

			// Build context from municipality + county, omitting duplicates of the primary name.
			municipality := item.Address.Municipality
			if municipality == "" {
				municipality = item.Address.City
			}
			if municipality == "" {
				municipality = item.Address.Town
			}
			county := item.Address.County
			var contextParts []string
			if municipality != "" && municipality != name {
				contextParts = append(contextParts, municipality)
			}
			if county != "" && county != municipality && county != name {
				contextParts = append(contextParts, county)
			}

			results = append(results, SearchResult{
				Name:    name,
				Context: strings.Join(contextParts, ", "),
				Country: item.Address.Country,
				Lat:     item.Lat,
				Lon:     item.Lon,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"results": results})
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

// FetchForecast returns the raw JSON forecast bytes for the given location.
// It uses the same cache and stampede protection as ForecastHandler.
func (s *Service) FetchForecast(loc Location) ([]byte, error) {
	return s.fetchForecastWithStampedeProtection(loc)
}

// fetchForecastWithStampedeProtection wraps fetchForecast to prevent cache stampedes.
func (s *Service) fetchForecastWithStampedeProtection(loc Location) ([]byte, error) {
	latStr := strconv.FormatFloat(loc.Lat, 'f', -1, 64)
	lonStr := strconv.FormatFloat(loc.Lon, 'f', -1, 64)
	cacheKey := latStr + "," + lonStr

	val, err, _ := s.requestGroup.Do(cacheKey, func() (interface{}, error) {
		return s.fetchForecast(loc, cacheKey)
	})
	
	if err != nil {
		return nil, err
	}
	return val.([]byte), nil
}

// fetchForecast retrieves the forecast from MET Norway, using an in-memory cache.
func (s *Service) fetchForecast(loc Location, cacheKey string) ([]byte, error) {
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

	url := fmt.Sprintf("%s?lat=%s&lon=%s", s.baseURL, strconv.FormatFloat(loc.Lat, 'f', -1, 64), strconv.FormatFloat(loc.Lon, 'f', -1, 64))

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
