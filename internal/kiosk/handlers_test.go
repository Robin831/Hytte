package kiosk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/transit"
)

// injectConfig returns a copy of r with a KioskConfig injected into its context.
func injectConfig(r *http.Request, cfg KioskConfig) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), kioskConfigKey, cfg))
}

func TestDataHandler_TransitIsEmptyArrayWhenNoStops(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	req = injectConfig(req, KioskConfig{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Transit json.RawMessage `json:"transit"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Must be "[]" not "null".
	if string(body.Transit) != "[]" {
		t.Errorf("expected transit to be [] when no stops configured, got %s", string(body.Transit))
	}
}

func TestDataHandler_TransitIsEmptyArrayWhenServiceNil(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	req = injectConfig(req, KioskConfig{"stop_ids": []any{"NSR:StopPlace:12345"}})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Transit []transit.StopDepartures `json:"transit"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Transit == nil {
		t.Error("expected non-nil transit slice when service is nil, got nil")
	}
	if len(body.Transit) != 0 {
		t.Errorf("expected empty transit when service is nil, got %d entries", len(body.Transit))
	}
}

func TestDataHandler_NoConfigInContext_Returns200(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	// No KioskConfig injected — handler should not panic.
	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDataHandler_FetchedAtIsPresent(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	req = injectConfig(req, KioskConfig{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.FetchedAt.IsZero() {
		t.Error("expected fetched_at to be set")
	}
}

func TestDataHandler_SunTimesComputedFromLatLon(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	req = injectConfig(req, KioskConfig{
		"lat": float64(59.9139),
		"lon": float64(10.7522),
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Sun *SunTimes `json:"sun"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Sun == nil {
		t.Fatal("expected sun times to be present when lat/lon configured")
	}
	if body.Sun.Kind != "normal" {
		t.Errorf("expected sun kind 'normal' for Oslo, got %q", body.Sun.Kind)
	}
	if body.Sun.Sunrise == "" {
		t.Error("expected sunrise to be set for normal sun kind")
	}
	if body.Sun.Sunset == "" {
		t.Error("expected sunset to be set for normal sun kind")
	}
}

func TestDataHandler_NoSunTimesWithoutLocation(t *testing.T) {
	handler := DataHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/kiosk/data", nil)
	req = injectConfig(req, KioskConfig{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		Sun *SunTimes `json:"sun"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Sun != nil {
		t.Errorf("expected sun to be absent when no location configured, got %+v", body.Sun)
	}
}

func TestComputeSunTimes_PolarNight(t *testing.T) {
	// 90°N at winter solstice — should be polar night.
	winterSolstice := time.Date(2024, 12, 21, 12, 0, 0, 0, time.UTC)
	sun := computeSunTimes(90.0, 0.0, winterSolstice)
	if sun == nil {
		t.Fatal("expected non-nil result")
	}
	if sun.Kind != "polarNight" {
		t.Errorf("expected polarNight at north pole in winter, got %q", sun.Kind)
	}
}

func TestComputeSunTimes_PolarDay(t *testing.T) {
	// 90°N at summer solstice — should be polar day.
	summerSolstice := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	sun := computeSunTimes(90.0, 0.0, summerSolstice)
	if sun == nil {
		t.Fatal("expected non-nil result")
	}
	if sun.Kind != "polarDay" {
		t.Errorf("expected polarDay at north pole in summer, got %q", sun.Kind)
	}
}

func TestComputeSunTimes_NormalDay_SunriseBeforeSunset(t *testing.T) {
	// Oslo in spring — normal day, sunrise before sunset.
	t1 := time.Date(2024, 4, 15, 12, 0, 0, 0, time.UTC)
	sun := computeSunTimes(59.9139, 10.7522, t1)
	if sun == nil {
		t.Fatal("expected non-nil result")
	}
	if sun.Kind != "normal" {
		t.Fatalf("expected normal sun kind, got %q", sun.Kind)
	}
	sunrise, err := time.Parse(time.RFC3339, sun.Sunrise)
	if err != nil {
		t.Fatalf("parse sunrise: %v", err)
	}
	sunset, err := time.Parse(time.RFC3339, sun.Sunset)
	if err != nil {
		t.Fatalf("parse sunset: %v", err)
	}
	if !sunrise.Before(sunset) {
		t.Errorf("expected sunrise (%v) before sunset (%v)", sunrise, sunset)
	}
}
