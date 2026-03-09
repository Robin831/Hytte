package weather

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	// Set up a mock MET API server.
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"Feature","properties":{"timeseries":[]}}`))
	}))
	defer mockAPI.Close()

	// We can't easily override the MET API URL in the handler, so test
	// that the handler returns a valid error or proxied response.
	// For unit testing, verify the handler accepts valid locations and
	// rejects invalid ones.
	handler := ForecastHandler()

	// Request with no location param should default to Oslo (may fail
	// reaching MET API, which returns 502 — that's expected in tests).
	req := httptest.NewRequest("GET", "/api/weather/forecast", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Either 200 (cached/reachable) or 502 (no network in CI) are acceptable.
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
		t.Errorf("expected 200 or 502, got %d", rec.Code)
	}
}

func TestForecastHandler_UnknownLocation(t *testing.T) {
	handler := ForecastHandler()

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
	handler := ForecastHandler()

	req := httptest.NewRequest("GET", "/api/weather/forecast?location=Bergen", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Bergen is a valid location; 200 or 502 depending on network.
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
		t.Errorf("expected 200 or 502 for Bergen, got %d", rec.Code)
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
