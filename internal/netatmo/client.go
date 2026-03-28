package netatmo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// maxCacheEntries caps the number of users tracked in the cache. When the limit
// is reached, expired entries are evicted before adding a new one.
const maxCacheEntries = 1000

const (
	stationsDataURL = "https://api.netatmo.com/api/getstationsdata"
	cacheTTL        = 5 * time.Minute
)

// Module type identifiers returned by the Netatmo API.
const (
	moduleTypeIndoor  = "NAMain"
	moduleTypeOutdoor = "NAModule1"
	moduleTypeWind    = "NAModule2"
)

// IndoorReadings holds sensor data from the indoor base station.
type IndoorReadings struct {
	Temperature float64
	Humidity    int
	CO2         int
	Noise       int
	Pressure    float64
}

// OutdoorReadings holds sensor data from an outdoor module.
type OutdoorReadings struct {
	Temperature float64
	Humidity    int
}

// WindReadings holds sensor data from a wind gauge module.
type WindReadings struct {
	Speed     float64 // km/h
	Gust      float64 // km/h
	Direction int     // degrees (0–360)
}

// ModuleReadings aggregates readings from all discovered station modules.
// Nil pointers indicate that module type was not found in the API response.
type ModuleReadings struct {
	Indoor    *IndoorReadings
	Outdoor   *OutdoorReadings
	Wind      *WindReadings
	FetchedAt time.Time
}

// cacheEntry holds a cached result for a single user.
type cacheEntry struct {
	readings  *ModuleReadings
	fetchedAt time.Time
}

// Client fetches station data from the Netatmo API and caches results per user.
type Client struct {
	oauth      *OAuthClient
	db         *sql.DB
	httpClient *http.Client

	mu    sync.Mutex
	cache map[int64]cacheEntry
}

// NewClient creates a new Netatmo API client.
func NewClient(oauth *OAuthClient, db *sql.DB) *Client {
	return &Client{
		oauth:      oauth,
		db:         db,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		cache:      make(map[int64]cacheEntry),
	}
}

// GetStationsData returns station readings for the given user, using a
// 5-minute in-memory cache to avoid hammering the Netatmo API.
func (c *Client) GetStationsData(ctx context.Context, userID int64) (*ModuleReadings, error) {
	c.mu.Lock()
	if entry, ok := c.cache[userID]; ok && time.Since(entry.fetchedAt) < cacheTTL {
		result := entry.readings
		c.mu.Unlock()
		return result, nil
	}
	c.mu.Unlock()

	accessToken, err := c.oauth.GetAccessToken(ctx, c.db, userID)
	if err != nil {
		return nil, fmt.Errorf("netatmo: get access token: %w", err)
	}
	if accessToken == "" {
		return nil, fmt.Errorf("netatmo: no token stored for user %d", userID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stationsDataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("netatmo: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netatmo: stations data request: %w", err)
	}
	defer resp.Body.Close()

	const maxBody int64 = 512 * 1024
	lr := &io.LimitedReader{R: resp.Body, N: maxBody + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("netatmo: read response: %w", err)
	}
	if int64(len(body)) > maxBody {
		return nil, fmt.Errorf("netatmo: stations data response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("netatmo: stations data HTTP %d: %s", resp.StatusCode, string(body))
	}

	readings, err := parseStationsData(body)
	if err != nil {
		return nil, err
	}
	readings.FetchedAt = time.Now()

	c.mu.Lock()
	// Double-check: another goroutine may have populated the cache while we fetched.
	if entry, ok := c.cache[userID]; ok && time.Since(entry.fetchedAt) < cacheTTL {
		c.mu.Unlock()
		return entry.readings, nil
	}
	// Evict expired entries before growing the map past the limit.
	if len(c.cache) >= maxCacheEntries {
		now := time.Now()
		for id, e := range c.cache {
			if now.Sub(e.fetchedAt) >= cacheTTL {
				delete(c.cache, id)
			}
		}
	}
	c.cache[userID] = cacheEntry{readings: readings, fetchedAt: readings.FetchedAt}
	c.mu.Unlock()

	return readings, nil
}

// --- Netatmo API response structs ---

type stationsDataResponse struct {
	Body struct {
		Devices []device `json:"devices"`
	} `json:"body"`
	Status string `json:"status"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type device struct {
	Type          string        `json:"type"`
	DashboardData dashboardData `json:"dashboard_data"`
	Modules       []module      `json:"modules"`
}

type module struct {
	Type          string        `json:"type"`
	DashboardData dashboardData `json:"dashboard_data"`
}

// dashboardData uses pointers so we can detect missing fields from the API.
type dashboardData struct {
	// Indoor / base station fields
	Temperature *float64 `json:"Temperature"`
	Humidity    *int     `json:"Humidity"`
	CO2         *int     `json:"CO2"`
	Noise       *int     `json:"Noise"`
	Pressure    *float64 `json:"Pressure"`

	// Wind module fields
	WindStrength *float64 `json:"WindStrength"`
	WindGust     *float64 `json:"WindGust"`
	WindAngle    *int     `json:"WindAngle"`
}

func parseStationsData(body []byte) (*ModuleReadings, error) {
	var apiResp stationsDataResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("netatmo: decode stations data: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("netatmo API error %d: %s", apiResp.Error.Code, apiResp.Error.Message)
	}

	readings := &ModuleReadings{}

	for _, dev := range apiResp.Body.Devices {
		if dev.Type == moduleTypeIndoor {
			readings.Indoor = parseIndoor(dev.DashboardData)

			for _, mod := range dev.Modules {
				switch mod.Type {
				case moduleTypeOutdoor:
					if readings.Outdoor == nil {
						readings.Outdoor = parseOutdoor(mod.DashboardData)
					}
				case moduleTypeWind:
					if readings.Wind == nil {
						readings.Wind = parseWind(mod.DashboardData)
					}
				}
			}
			// Use the first NAMain device found.
			break
		}
	}

	return readings, nil
}

func parseIndoor(d dashboardData) *IndoorReadings {
	if d.Temperature == nil {
		return nil
	}
	r := &IndoorReadings{Temperature: *d.Temperature}
	if d.Humidity != nil {
		r.Humidity = *d.Humidity
	}
	if d.CO2 != nil {
		r.CO2 = *d.CO2
	}
	if d.Noise != nil {
		r.Noise = *d.Noise
	}
	if d.Pressure != nil {
		r.Pressure = *d.Pressure
	}
	return r
}

func parseOutdoor(d dashboardData) *OutdoorReadings {
	if d.Temperature == nil {
		return nil
	}
	r := &OutdoorReadings{Temperature: *d.Temperature}
	if d.Humidity != nil {
		r.Humidity = *d.Humidity
	}
	return r
}

func parseWind(d dashboardData) *WindReadings {
	if d.WindStrength == nil {
		return nil
	}
	r := &WindReadings{Speed: *d.WindStrength}
	if d.WindGust != nil {
		r.Gust = *d.WindGust
	}
	if d.WindAngle != nil {
		r.Direction = *d.WindAngle
	}
	return r
}
