package weather

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSearchHandler_MissingQuery(t *testing.T) {
	svc := newTestSearchService("http://unused")
	handler := svc.SearchHandler()

	req := httptest.NewRequest("GET", "/api/weather/search", nil)
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
		t.Error("expected error message")
	}
}

func TestSearchHandler_QueryTooLong(t *testing.T) {
	svc := newTestSearchService("http://unused")
	handler := svc.SearchHandler()

	longQuery := strings.Repeat("a", 101)
	req := httptest.NewRequest("GET", "/api/weather/search?q="+longQuery, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSearchHandler_Success(t *testing.T) {
	nominatimResp := `[
		{
			"display_name": "Geilo, Numedal, Viken, Norge",
			"lat": "60.5340",
			"lon": "8.2024",
			"address": {
				"city": "Geilo",
				"country": "Norge"
			}
		},
		{
			"display_name": "Rjukan, Notodden, Telemark, Norge",
			"lat": "59.8778",
			"lon": "8.5927",
			"address": {
				"town": "Rjukan",
				"country": "Norge"
			}
		}
	]`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Error("expected q param to be forwarded")
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(nominatimResp))
	}))
	defer mock.Close()

	svc := newTestSearchService(mock.URL)
	handler := svc.SearchHandler()

	req := httptest.NewRequest("GET", "/api/weather/search?q=Geilo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(body.Results))
	}
	if body.Results[0].Name != "Geilo" {
		t.Errorf("expected first result name=Geilo, got %s", body.Results[0].Name)
	}
	if body.Results[1].Name != "Rjukan" {
		t.Errorf("expected second result name=Rjukan, got %s", body.Results[1].Name)
	}
}

func TestSearchHandler_UpstreamError(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mock.Close()

	svc := newTestSearchService(mock.URL)
	handler := svc.SearchHandler()

	req := httptest.NewRequest("GET", "/api/weather/search?q=Oslo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d", rec.Code)
	}
}

func TestSearchHandler_InvalidJSON(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer mock.Close()

	svc := newTestSearchService(mock.URL)
	handler := svc.SearchHandler()

	req := httptest.NewRequest("GET", "/api/weather/search?q=Oslo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on invalid JSON, got %d", rec.Code)
	}
}

func TestForecastHandler_LatLon(t *testing.T) {
	mock := newMockMETServer(func(w http.ResponseWriter, r *http.Request) {
		// Verify lat/lon are forwarded to MET API.
		lat := r.URL.Query().Get("lat")
		lon := r.URL.Query().Get("lon")
		if lat == "" || lon == "" {
			t.Error("expected lat and lon query params")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeMETResponse()))
	})
	defer mock.Close()

	svc := newTestService(mock.URL)
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?lat=60.1234&lon=10.5678&location=CustomPlace", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestForecastHandler_InvalidLat(t *testing.T) {
	svc := newTestService("http://unused")
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?lat=abc&lon=10.0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestForecastHandler_LatOutOfRange(t *testing.T) {
	svc := newTestService("http://unused")
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?lat=91.0&lon=10.0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for lat out of range, got %d", rec.Code)
	}
}

func TestForecastHandler_InvalidLon(t *testing.T) {
	svc := newTestService("http://unused")
	handler := svc.ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?lat=60.0&lon=abc", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
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
