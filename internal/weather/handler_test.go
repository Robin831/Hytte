package weather

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeMETResponse returns a minimal valid MET API compact forecast JSON.
func fakeMETResponse() string {
	return `{
		"type": "Feature",
		"properties": {
			"timeseries": [
				{
					"time": "2026-03-09T12:00:00Z",
					"data": {
						"instant": {
							"details": {
								"air_temperature": 5.2,
								"wind_speed": 3.1,
								"relative_humidity": 72.0,
								"air_pressure_at_sea_level": 1013.0
							}
						},
						"next_1_hours": {
							"summary": {"symbol_code": "cloudy"},
							"details": {"precipitation_amount": 0.0}
						}
					}
				}
			]
		}
	}`
}

func newMockMETServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestLocationsHandler(t *testing.T) {
	handler := LocationsHandler()

	req := httptest.NewRequest("GET", "/api/weather/locations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Locations []Location `json:"locations"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Locations) != len(NorwegianLocations) {
		t.Errorf("expected %d locations, got %d", len(NorwegianLocations), len(body.Locations))
	}
}

func TestForecastHandler_DefaultLocation(t *testing.T) {
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeMETResponse()))
	})
	defer mock.Close()

	svc := newTestService(mock.URL)
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["type"] != "Feature" {
		t.Errorf("expected type=Feature, got %v", body["type"])
	}
}

func TestForecastHandler_UnknownLocation(t *testing.T) {
	svc := newTestService("http://unused")
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?location=Atlantis", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestForecastHandler_ValidLocation(t *testing.T) {
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request has correct query params for Bergen.
		lat := r.URL.Query().Get("lat")
		if lat == "" {
			t.Error("expected lat query param")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeMETResponse()))
	})
	defer mock.Close()

	svc := newTestService(mock.URL)
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?location=Bergen", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for Bergen, got %d", rec.Code)
	}
}

func TestForecastHandler_UpstreamError(t *testing.T) {
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mock.Close()

	svc := newTestService(mock.URL)
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?location=Oslo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d", rec.Code)
	}
}

func TestForecastHandler_304NotModified(t *testing.T) {
	callCount := 0
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fakeMETResponse()))
			return
		}
		// Second request: return 304.
		w.WriteHeader(http.StatusNotModified)
	})
	defer mock.Close()

	svc := newTestService(mock.URL)

	// First call: populate cache.
	loc := NorwegianLocations["Oslo"]
	data1, err := svc.fetchForecast(loc)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Expire the cache so the next call fetches again.
	svc.mu.Lock()
	for _, v := range svc.cache {
		v.expires = v.expires.Add(-1 * time.Hour)
	}
	svc.mu.Unlock()

	// Second call: should get 304 and return cached data.
	data2, err := svc.fetchForecast(loc)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("expected cached data to be returned on 304")
	}
	if callCount != 2 {
		t.Errorf("expected 2 upstream calls, got %d", callCount)
	}
}

func TestForecastHandler_CacheHit(t *testing.T) {
	callCount := 0
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeMETResponse()))
	})
	defer mock.Close()

	svc := newTestService(mock.URL)
	loc := NorwegianLocations["Oslo"]

	// First call: cache miss.
	_, err := svc.fetchForecast(loc)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Second call: should be served from cache (no upstream call).
	_, err = svc.fetchForecast(loc)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 upstream call (cache hit), got %d", callCount)
	}
}

func TestNorwegianLocations_HasExpectedCities(t *testing.T) {
	expectedCities := []string{"Oslo", "Bergen", "Trondheim", "Tromsø", "Stavanger"}
	for _, city := range expectedCities {
		if _, ok := NorwegianLocations[city]; !ok {
			t.Errorf("NorwegianLocations missing %s", city)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["hello"] != "world" {
		t.Errorf("expected world, got %s", body["hello"])
	}
}

func TestForecastHandler_ErrorMessageDoesNotLeakInput(t *testing.T) {
	svc := newTestService("http://unused")
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?location=<script>alert(1)</script>", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Error message should NOT contain the user-supplied input.
	if body["error"] != "unknown location" {
		t.Errorf("error message should be generic, got: %s", body["error"])
	}
}
