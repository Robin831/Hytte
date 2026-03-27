package transit

import (
	"net/http"
	"time"
)

// newTestService creates a Service pointing at test servers instead of the real
// Entur APIs.
func newTestService(graphqlURL, geocoderURL string) *Service {
	return &Service{
		client:      &http.Client{Timeout: 5 * time.Second},
		graphqlURL:  graphqlURL,
		geocoderURL: geocoderURL,
		cache:       make(map[string]*departureCache),
	}
}
