package kiosk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/netatmo"
	"github.com/Robin831/Hytte/internal/transit"
	"github.com/Robin831/Hytte/internal/weather"
)

// fakeTransit is a transitFetcher that counts calls and returns a fixed stop.
type fakeTransit struct {
	calls atomic.Int64
}

func (f *fakeTransit) FetchDepartures(ctx context.Context, stopID string, count int) (string, []transit.Departure, error) {
	f.calls.Add(1)
	return "Stop " + stopID, []transit.Departure{}, nil
}

// fakeWeather is a weatherFetcher that counts calls and can sleep to simulate a
// slow upstream.
type fakeWeather struct {
	calls atomic.Int64
	delay time.Duration
}

func (f *fakeWeather) FetchForecast(loc weather.Location) ([]byte, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return []byte(`{"forecast":true}`), nil
}

// fakeNetatmo is a netatmoFetcher that counts calls and returns fixed readings.
type fakeNetatmo struct {
	calls atomic.Int64
}

func (f *fakeNetatmo) GetStationsData(ctx context.Context, userID int64) (*netatmo.ModuleReadings, error) {
	f.calls.Add(1)
	return &netatmo.ModuleReadings{Outdoor: &netatmo.OutdoorReadings{}}, nil
}

// resetKioskCache clears the package-level cache so tests don't see each
// other's cached payloads.
func resetKioskCache() {
	kioskCache = NewTTLCache()
}

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

func TestDataHandler_CacheReuseAvoidsRefetch(t *testing.T) {
	resetKioskCache()

	transitSvc := &fakeTransit{}
	netatmoSvc := &fakeNetatmo{}
	weatherSvc := &fakeWeather{}
	handler := DataHandler(nil, transitSvc, netatmoSvc, weatherSvc)

	cfg := KioskConfig{
		"stop_ids":        []any{"NSR:StopPlace:1"},
		"location":        "Oslo",
		"netatmo_user_id": float64(42),
	}

	// Two identical requests within the TTL window.
	for i := 0; i < 2; i++ {
		req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	if got := transitSvc.calls.Load(); got != 1 {
		t.Errorf("expected transit fetched once across two cached requests, got %d", got)
	}
	if got := netatmoSvc.calls.Load(); got != 1 {
		t.Errorf("expected netatmo fetched once across two cached requests, got %d", got)
	}
	if got := weatherSvc.calls.Load(); got != 1 {
		t.Errorf("expected weather fetched once across two cached requests, got %d", got)
	}
}

func TestDataHandler_DistinctConfigTriggersFreshFetch(t *testing.T) {
	resetKioskCache()

	transitSvc := &fakeTransit{}
	handler := DataHandler(nil, transitSvc, nil, nil)

	cfgA := KioskConfig{"stop_ids": []any{"NSR:StopPlace:1"}}
	cfgB := KioskConfig{"stop_ids": []any{"NSR:StopPlace:2"}}

	for _, cfg := range []KioskConfig{cfgA, cfgB} {
		req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	}

	if got := transitSvc.calls.Load(); got != 2 {
		t.Errorf("expected two fetches for two distinct configs, got %d", got)
	}
}

func TestDataHandler_CacheHitPreservesFetchedAt(t *testing.T) {
	resetKioskCache()

	handler := DataHandler(nil, &fakeTransit{}, nil, nil)
	cfg := KioskConfig{"stop_ids": []any{"NSR:StopPlace:1"}}

	first := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, first)
	var body1 struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.NewDecoder(rec1.Body).Decode(&body1); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	// Small gap so a freshly-stamped FetchedAt would differ.
	time.Sleep(5 * time.Millisecond)

	second := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, second)
	var body2 struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	if !body1.FetchedAt.Equal(body2.FetchedAt) {
		t.Errorf("expected cache hit to preserve original FetchedAt %v, got %v", body1.FetchedAt, body2.FetchedAt)
	}
}

func TestDataHandler_CacheControlAndContentTypeHeaders(t *testing.T) {
	resetKioskCache()

	handler := DataHandler(nil, nil, nil, nil)
	req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), KioskConfig{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	wantCC := "private, max-age=" + strconv.Itoa(int(CacheTTL.Seconds()))
	if cc := rec.Header().Get("Cache-Control"); cc != wantCC {
		t.Errorf("expected Cache-Control %q, got %q", wantCC, cc)
	}
	if vary := rec.Header().Get("Vary"); vary != "Authorization" {
		t.Errorf("expected Vary: Authorization, got %q", vary)
	}
}

func TestDataHandler_SlowSourceYieldsPartialData(t *testing.T) {
	resetKioskCache()

	// Shorten the per-source timeout for the duration of this test.
	orig := perSourceTimeout
	perSourceTimeout = 50 * time.Millisecond
	defer func() { perSourceTimeout = orig }()

	// Weather sleeps well past the timeout; sun is computed locally (fast).
	weatherSvc := &fakeWeather{delay: 500 * time.Millisecond}
	handler := DataHandler(nil, nil, nil, weatherSvc)

	cfg := KioskConfig{
		"lat": float64(59.9139),
		"lon": float64(10.7522),
	}
	req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Latency must be bound by the timeout, not the 500ms slow upstream.
	if elapsed >= 400*time.Millisecond {
		t.Errorf("expected handler to return near the timeout bound, took %v", elapsed)
	}

	var body struct {
		Forecast json.RawMessage `json:"forecast"`
		Sun      *SunTimes       `json:"sun"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Forecast) != 0 {
		t.Errorf("expected forecast omitted when weather times out, got %s", body.Forecast)
	}
	if body.Sun == nil {
		t.Error("expected fast sun source to still populate despite slow weather")
	}
}
