package transit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	enturGraphQLURL  = "https://api.entur.io/journey-planner/v3/graphql"
	enturGeocoderURL = "https://api.entur.io/geocoder/v1/autocomplete"
	enturClientName  = "hytte-transit"

	departureCacheTTL  = 30 * time.Second
	maxResponseSize    = 512 << 10 // 512 KB
	maxGeocoderSize    = 256 << 10 // 256 KB
	numberOfDepartures = 10
)

// departureCache holds a cached list of departures for a stop.
type departureCache struct {
	data    []Departure
	expires time.Time
}

// Service holds the Entur API client and its in-memory cache.
type Service struct {
	client *http.Client

	mu    sync.RWMutex
	cache map[string]*departureCache
}

// NewService creates a new Entur transit service.
func NewService() *Service {
	return &Service{
		client: &http.Client{Timeout: 10 * time.Second},
		cache:  make(map[string]*departureCache),
	}
}

// enturGraphQLQuery is the GraphQL query for real-time departures.
const enturGraphQLQuery = `
query StopDepartures($stopID: String!, $count: Int!) {
  stopPlace(id: $stopID) {
    name
    estimatedCalls(numberOfDepartures: $count, omitNonBoarding: true) {
      expectedDepartureTime
      aimedDepartureTime
      destinationDisplay {
        frontText
      }
      serviceJourney {
        line {
          publicCode
        }
        quay {
          publicCode
        }
      }
      realtime
    }
  }
}
`

// enturResponse mirrors the GraphQL response structure.
type enturResponse struct {
	Data struct {
		StopPlace *struct {
			Name           string `json:"name"`
			EstimatedCalls []struct {
				ExpectedDepartureTime string `json:"expectedDepartureTime"`
				AimedDepartureTime    string `json:"aimedDepartureTime"`
				DestinationDisplay    struct {
					FrontText string `json:"frontText"`
				} `json:"destinationDisplay"`
				ServiceJourney struct {
					Line struct {
						PublicCode string `json:"publicCode"`
					} `json:"line"`
					Quay *struct {
						PublicCode string `json:"publicCode"`
					} `json:"quay"`
				} `json:"serviceJourney"`
				Realtime bool `json:"realtime"`
			} `json:"estimatedCalls"`
		} `json:"stopPlace"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// FetchDepartures retrieves upcoming departures for a stop from Entur, using a
// 30-second in-memory cache to reduce API calls.
func (s *Service) FetchDepartures(ctx context.Context, stopID string) (string, []Departure, error) {
	// Check cache first.
	s.mu.RLock()
	if c, ok := s.cache[stopID]; ok && time.Now().Before(c.expires) {
		data := c.data
		s.mu.RUnlock()
		// We need the stop name; return empty string — callers tolerate this when cached.
		return "", data, nil
	}
	s.mu.RUnlock()

	body, err := json.Marshal(map[string]any{
		"query": enturGraphQLQuery,
		"variables": map[string]any{
			"stopID": stopID,
			"count":  numberOfDepartures,
		},
	})
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, enturGraphQLURL, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ET-Client-Name", enturClientName)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("entur returned status %d", resp.StatusCode)
	}

	lr := &io.LimitedReader{R: resp.Body, N: maxResponseSize + 1}
	respBody, err := io.ReadAll(lr)
	if err != nil {
		return "", nil, err
	}
	if int64(len(respBody)) > maxResponseSize {
		return "", nil, fmt.Errorf("entur response too large")
	}

	var gqlResp enturResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return "", nil, err
	}

	if len(gqlResp.Errors) > 0 {
		return "", nil, fmt.Errorf("entur GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if gqlResp.Data.StopPlace == nil {
		return "", nil, fmt.Errorf("stop %q not found", stopID)
	}

	stopName := gqlResp.Data.StopPlace.Name

	departures := make([]Departure, 0, len(gqlResp.Data.StopPlace.EstimatedCalls))
	for _, call := range gqlResp.Data.StopPlace.EstimatedCalls {
		expected, err := time.Parse(time.RFC3339, call.ExpectedDepartureTime)
		if err != nil {
			continue
		}
		aimed, err := time.Parse(time.RFC3339, call.AimedDepartureTime)
		if err != nil {
			aimed = expected
		}

		delaySeconds := expected.Sub(aimed).Seconds()
		delayMinutes := int(delaySeconds / 60)

		platform := ""
		if call.ServiceJourney.Quay != nil {
			platform = call.ServiceJourney.Quay.PublicCode
		}

		departures = append(departures, Departure{
			Line:          call.ServiceJourney.Line.PublicCode,
			Destination:   call.DestinationDisplay.FrontText,
			DepartureTime: expected,
			IsRealtime:    call.Realtime,
			Platform:      platform,
			DelayMinutes:  delayMinutes,
		})
	}

	// Store in cache.
	s.mu.Lock()
	s.cache[stopID] = &departureCache{
		data:    departures,
		expires: time.Now().Add(departureCacheTTL),
	}
	s.mu.Unlock()

	return stopName, departures, nil
}

// GeocoderResult is a stop returned by the Entur geocoder.
type GeocoderResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// enturGeocoderResponse mirrors the Entur geocoder response.
type enturGeocoderResponse struct {
	Features []struct {
		Properties struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Name  string `json:"name"`
		} `json:"properties"`
	} `json:"features"`
}

// SearchStops queries the Entur geocoder for stops matching the given text.
func (s *Service) SearchStops(ctx context.Context, query string) ([]GeocoderResult, error) {
	params := url.Values{}
	params.Set("text", query)
	params.Set("lang", "no")
	params.Set("size", "10")
	params.Set("layers", "venue")
	params.Set("categories", "onstreetBus,busStation,onstreetTram,tramStation,railStation,metroStation,ferryStop")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		enturGeocoderURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("ET-Client-Name", enturClientName)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("entur geocoder returned status %d", resp.StatusCode)
	}

	lr := &io.LimitedReader{R: resp.Body, N: maxGeocoderSize + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxGeocoderSize {
		return nil, fmt.Errorf("geocoder response too large")
	}

	var geoResp enturGeocoderResponse
	if err := json.Unmarshal(body, &geoResp); err != nil {
		return nil, err
	}

	results := make([]GeocoderResult, 0, len(geoResp.Features))
	for _, f := range geoResp.Features {
		name := f.Properties.Label
		if name == "" {
			name = f.Properties.Name
		}
		results = append(results, GeocoderResult{
			ID:   f.Properties.ID,
			Name: name,
		})
	}
	return results, nil
}
