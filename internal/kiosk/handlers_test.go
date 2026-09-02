package kiosk

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// decodeDim runs the data handler with cfg and returns the decoded dim object.
func decodeDim(t *testing.T, cfg KioskConfig) *DimConfig {
	t.Helper()
	resetKioskCache()

	handler := DataHandler(nil, nil, nil, nil)
	req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Dim *DimConfig `json:"dim"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Dim
}

func TestDataHandler_DimAbsentWhenNotConfigured(t *testing.T) {
	if dim := decodeDim(t, KioskConfig{}); dim != nil {
		t.Errorf("expected dim omitted when no dim keys configured, got %+v", dim)
	}
}

func TestDataHandler_DimEnabledValues(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim := decodeDim(t, KioskConfig{"dim": tt.raw})
			if dim == nil {
				t.Fatalf("expected dim present for %v", tt.raw)
			}
			if dim.Enabled == nil {
				t.Fatalf("expected dim.enabled present for %v", tt.raw)
			}
			if *dim.Enabled != tt.want {
				t.Errorf("expected dim.enabled %v, got %v", tt.want, *dim.Enabled)
			}
		})
	}
}

func TestDataHandler_DimWindowEmittedWhenBothParse(t *testing.T) {
	dim := decodeDim(t, KioskConfig{"dim_start": "22:30", "dim_end": "06:00"})
	if dim == nil {
		t.Fatal("expected dim present when a valid window is configured")
	}
	if dim.Start != "22:30" {
		t.Errorf("expected start 22:30, got %q", dim.Start)
	}
	if dim.End != "06:00" {
		t.Errorf("expected end 06:00, got %q", dim.End)
	}
	if dim.Enabled != nil {
		t.Errorf("expected enabled absent when only a window is configured, got %v", *dim.Enabled)
	}
}

func TestDataHandler_DimWindowRequiresBothEnds(t *testing.T) {
	tests := []struct {
		name string
		cfg  KioskConfig
	}{
		{"start only", KioskConfig{"dim_start": "22:30"}},
		{"end only", KioskConfig{"dim_end": "06:00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if dim := decodeDim(t, tt.cfg); dim != nil {
				t.Errorf("expected dim omitted for a half-configured window, got %+v", dim)
			}
		})
	}
}

// Malformed dim values must degrade to the sun-driven default, never a 500.
func TestDataHandler_MalformedDimValuesFallBack(t *testing.T) {
	tests := []struct {
		name string
		cfg  KioskConfig
	}{
		{"unknown enum", KioskConfig{"dim": "maybe"}},
		{"auto is not a bool", KioskConfig{"dim": "auto"}},
		{"string true is not a bool", KioskConfig{"dim": "true"}},
		{"string false is not a bool", KioskConfig{"dim": "false"}},
		{"string on is not a bool", KioskConfig{"dim": "on"}},
		{"string 1 is not a bool", KioskConfig{"dim": "1"}},
		{"number instead of bool", KioskConfig{"dim": float64(5)}},
		{"object instead of bool", KioskConfig{"dim": map[string]any{"on": true}}},
		{"null", KioskConfig{"dim": nil}},
		{"garbage start", KioskConfig{"dim_start": "garbage", "dim_end": "06:00"}},
		{"hour out of range", KioskConfig{"dim_start": "25:00", "dim_end": "06:00"}},
		{"minute out of range", KioskConfig{"dim_start": "22:30", "dim_end": "12:60"}},
		{"missing separator", KioskConfig{"dim_start": "2230", "dim_end": "0600"}},
		{"non-digit", KioskConfig{"dim_start": "2a:30", "dim_end": "06:00"}},
		{"signed hour", KioskConfig{"dim_start": "+1:30", "dim_end": "06:00"}},
		{"unpadded", KioskConfig{"dim_start": "2:30", "dim_end": "06:00"}},
		{"start wrong type", KioskConfig{"dim_start": float64(22), "dim_end": "06:00"}},
		{"end wrong type", KioskConfig{"dim_start": "22:30", "dim_end": true}},
		{"empty strings", KioskConfig{"dim_start": "", "dim_end": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if dim := decodeDim(t, tt.cfg); dim != nil {
				t.Errorf("expected dim omitted for malformed config, got %+v", dim)
			}
		})
	}
}

// A dropped dim window is the one place the misconfiguration is visible: the
// client never receives the raw values, so without this log a wall-mounted
// screen would just quietly keep following the sun.
func TestBuildDimConfig_LogsDroppedWindow(t *testing.T) {
	tests := []struct {
		name string
		cfg  KioskConfig
		want []string
	}{
		{"unpadded hour", KioskConfig{"dim_start": "7:31", "dim_end": "06:01"}, []string{"7:31", "06:01"}},
		{"hour out of range", KioskConfig{"dim_start": "25:01", "dim_end": "06:02"}, []string{"25:01"}},
		{"start only", KioskConfig{"dim_start": "22:31"}, []string{"22:31"}},
		{"end only", KioskConfig{"dim_end": "06:03"}, []string{"06:03"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureLog(t, func() { buildDimConfig(tt.cfg) })
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected log to mention %q, got %q", want, out)
				}
			}
			if !strings.Contains(out, "dim window") {
				t.Errorf("expected a dim-window warning, got %q", out)
			}
		})
	}
}

// The warning is deduped: buildDimConfig runs on every kiosk poll, so an
// un-deduped line would be a permanent log flood rather than a diagnostic.
func TestBuildDimConfig_DroppedWindowLoggedOnce(t *testing.T) {
	cfg := KioskConfig{"dim_start": "9:44", "dim_end": "06:04"}
	if first := captureLog(t, func() { buildDimConfig(cfg) }); first == "" {
		t.Fatal("expected the first dropped window to be logged")
	}
	if again := captureLog(t, func() { buildDimConfig(cfg) }); again != "" {
		t.Errorf("expected the repeat to be deduped, got %q", again)
	}
}

// A usable window — and a config with no window at all — must stay silent.
func TestBuildDimConfig_NoLogWhenNothingIsDropped(t *testing.T) {
	for _, cfg := range []KioskConfig{
		{"dim_start": "21:07", "dim_end": "05:07"},
		{"dim": true},
		{},
	} {
		if out := captureLog(t, func() { buildDimConfig(cfg) }); out != "" {
			t.Errorf("expected no log for %v, got %q", cfg, out)
		}
	}
}

// captureLog redirects the standard logger for the duration of fn.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	fn()
	return buf.String()
}

func TestDataHandler_DimAndWindowTogether(t *testing.T) {
	dim := decodeDim(t, KioskConfig{
		"dim":       true,
		"dim_start": "23:00",
		"dim_end":   "07:15",
	})
	if dim == nil {
		t.Fatal("expected dim present")
	}
	if dim.Enabled == nil || !*dim.Enabled {
		t.Errorf("expected enabled true, got %v", dim.Enabled)
	}
	if dim.Start != "23:00" || dim.End != "07:15" {
		t.Errorf("expected window 23:00-07:15, got %q-%q", dim.Start, dim.End)
	}
}

// A malformed window must not suppress a valid dim flag, and vice versa.
func TestDataHandler_MalformedWindowKeepsEnabledFlag(t *testing.T) {
	dim := decodeDim(t, KioskConfig{"dim": false, "dim_start": "99:99", "dim_end": "06:00"})
	if dim == nil {
		t.Fatal("expected dim present when the enabled flag parses")
	}
	if dim.Enabled == nil || *dim.Enabled {
		t.Errorf("expected enabled false, got %v", dim.Enabled)
	}
	if dim.Start != "" || dim.End != "" {
		t.Errorf("expected window omitted, got %q-%q", dim.Start, dim.End)
	}
}

// Two tokens differing only in dim overrides share an upstream cache entry, but
// each response must still carry its own dim values.
func TestDataHandler_CachedPayloadCarriesPerTokenDim(t *testing.T) {
	resetKioskCache()

	transitSvc := &fakeTransit{}
	handler := DataHandler(nil, transitSvc, nil, nil)

	read := func(cfg KioskConfig) *DimConfig {
		req := injectConfig(httptest.NewRequest("GET", "/api/kiosk/data", nil), cfg)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body struct {
			Dim *DimConfig `json:"dim"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Dim
	}

	base := []any{"NSR:StopPlace:1"}
	first := read(KioskConfig{"stop_ids": base, "dim": true})
	second := read(KioskConfig{"stop_ids": base, "dim": false})
	// A token with no dim override must not inherit a previous token's dim from
	// the shared cache entry.
	third := read(KioskConfig{"stop_ids": base})

	if got := transitSvc.calls.Load(); got != 1 {
		t.Errorf("expected the second request to hit the cache, got %d transit calls", got)
	}
	if first == nil || first.Enabled == nil || !*first.Enabled {
		t.Errorf("expected first response enabled=true, got %+v", first)
	}
	if second == nil || second.Enabled == nil || *second.Enabled {
		t.Errorf("expected cached response to carry its own enabled=false, got %+v", second)
	}
	if third != nil {
		t.Errorf("expected a token without dim keys to stay dim-free, got %+v", third)
	}
}

func TestParseHHMM(t *testing.T) {
	valid := []string{"00:00", "23:59", "09:05", " 22:30 "}
	for _, s := range valid {
		if _, ok := parseHHMM(s); !ok {
			t.Errorf("expected %q to parse", s)
		}
	}
	invalid := []string{"", "24:00", "23:60", "1:00", "100:00", "aa:bb", "12-30", "12:3"}
	for _, s := range invalid {
		if got, ok := parseHHMM(s); ok {
			t.Errorf("expected %q to be rejected, got %q", s, got)
		}
	}
}
