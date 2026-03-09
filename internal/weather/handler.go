package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// cachedResponse holds a cached MET API response.
type cachedResponse struct {
	data      []byte
	expires   time.Time
	fetchedAt time.Time
}

var (
	cache   = make(map[string]*cachedResponse)
	cacheMu sync.RWMutex
)

// ForecastHandler returns weather forecast data for a given Norwegian location.
// Query params: location (city name, default "Oslo")
func ForecastHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationName := r.URL.Query().Get("location")
		if locationName == "" {
			locationName = "Oslo"
		}

		loc, ok := NorwegianLocations[locationName]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("unknown location: %s", locationName),
			})
			return
		}

		data, err := fetchForecast(loc)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "failed to fetch weather data",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

// LocationsHandler returns the list of available locations.
func LocationsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locations := make([]Location, 0, len(NorwegianLocations))
		for _, loc := range NorwegianLocations {
			locations = append(locations, loc)
		}
		writeJSON(w, http.StatusOK, map[string]any{"locations": locations})
	}
}

// fetchForecast retrieves the forecast from MET Norway, using an in-memory cache.
func fetchForecast(loc Location) ([]byte, error) {
	cacheKey := fmt.Sprintf("%.4f,%.4f", loc.Lat, loc.Lon)

	cacheMu.RLock()
	cached, ok := cache[cacheKey]
	cacheMu.RUnlock()

	if ok && time.Now().Before(cached.expires) {
		return cached.data, nil
	}

	url := fmt.Sprintf(
		"https://api.met.no/weatherapi/locationforecast/2.0/compact?lat=%.4f&lon=%.4f",
		loc.Lat, loc.Lon,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// MET Norway requires a descriptive User-Agent with contact info.
	req.Header.Set("User-Agent", "Hytte/1.0 github.com/Robin831/Hytte")

	// Forward If-Modified-Since if we have a cached response.
	if cached != nil {
		req.Header.Set("If-Modified-Since", cached.fetchedAt.UTC().Format(http.TimeFormat))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// If fetch fails but we have stale cache, return it.
		if cached != nil {
			return cached.data, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	// 304 Not Modified — extend cache.
	if resp.StatusCode == http.StatusNotModified && cached != nil {
		cacheMu.Lock()
		cached.expires = time.Now().Add(30 * time.Minute)
		cacheMu.Unlock()
		return cached.data, nil
	}

	if resp.StatusCode != http.StatusOK {
		if cached != nil {
			return cached.data, nil
		}
		return nil, fmt.Errorf("MET API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Cache for 30 minutes (MET asks clients to respect Expires header, but
	// 30 min is a safe default for compact forecasts).
	cacheMu.Lock()
	cache[cacheKey] = &cachedResponse{
		data:      body,
		expires:   time.Now().Add(30 * time.Minute),
		fetchedAt: time.Now(),
	}
	cacheMu.Unlock()

	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
