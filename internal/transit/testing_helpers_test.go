package transit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

// setPerStopTimeout shortens the handler's per-stop deadline for the duration of
// a test so timeout behaviour can be exercised without waiting out the real one.
func setPerStopTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	previous := perStopTimeout
	perStopTimeout = d
	t.Cleanup(func() { perStopTimeout = previous })
}

// enturStub serves the Entur GraphQL endpoint, dispatching each request to the
// handler registered for the stop ID in its variables. This lets a test give
// each stop its own latency or failure mode.
func enturStub(t *testing.T, behavior map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			Variables struct {
				StopID string `json:"stopID"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode GraphQL body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler, ok := behavior[reqBody.Variables.StopID]
		if !ok {
			t.Errorf("unexpected stop ID in request: %q", reqBody.Variables.StopID)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		handler(w, r)
	}))
}

// departuresAfter returns a stub handler that waits before answering with a
// valid single-departure response. It bails out early when the client goes away
// so a hung stop doesn't hold the test server open past the test.
func departuresAfter(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeEnturResponse()))
	}
}

// emptyDepartures answers with a valid response that simply has no calls —
// a stop with no service right now, as opposed to a stop that failed.
func emptyDepartures() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data": {"stopPlace": {"name": "Quiet Stop", "estimatedCalls": []}}}`))
	}
}
