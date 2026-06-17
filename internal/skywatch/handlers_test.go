package skywatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTTLCacheHit(t *testing.T) {
	c := newTTLCache(time.Minute)
	c.set("k", []byte("value"))

	data, ok := c.get("k")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(data) != "value" {
		t.Errorf("got %q, want %q", data, "value")
	}
}

func TestTTLCacheMiss(t *testing.T) {
	c := newTTLCache(time.Minute)
	if _, ok := c.get("missing"); ok {
		t.Error("expected cache miss for unknown key")
	}
}

func TestTTLCacheExpiry(t *testing.T) {
	c := newTTLCache(time.Nanosecond)
	c.set("k", []byte("value"))
	// Ensure the TTL has elapsed.
	time.Sleep(time.Millisecond)

	if _, ok := c.get("k"); ok {
		t.Error("expected expired entry to be a miss")
	}
	// Expired entry should have been dropped lazily.
	c.mu.RLock()
	_, present := c.entries["k"]
	c.mu.RUnlock()
	if present {
		t.Error("expected expired entry to be removed on access")
	}
}

func TestTTLCacheKeySeparation(t *testing.T) {
	c := newTTLCache(time.Minute)
	c.set("a", []byte("A"))
	c.set("b", []byte("B"))

	if data, _ := c.get("a"); string(data) != "A" {
		t.Errorf("key a = %q, want A", data)
	}
	if data, _ := c.get("b"); string(data) != "B" {
		t.Errorf("key b = %q, want B", data)
	}
}

func nowKey(lat, lon float64, t time.Time) string {
	return fmt.Sprintf("%v|%v|%s", lat, lon, t.Format("2006-01-02"))
}

func TestNowHandlerCacheControl(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/skywatch/now?lat=11&lon=22", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=300")
	}

	var resp NowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Location.Lat != 11 || resp.Location.Lon != 22 {
		t.Errorf("location = %+v, want {11 22}", resp.Location)
	}
}

func TestNowHandlerServesFromCache(t *testing.T) {
	lat, lon := 31.0, 41.0
	date := "2026-01-15"
	key := fmt.Sprintf("%v|%v|%s", lat, lon, date)
	sentinel := []byte(`{"sentinel":"now"}`)
	nowCache.set(key, sentinel)

	req := httptest.NewRequest("GET", "/api/skywatch/now?lat=31&lon=41&date="+date, nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if got := w.Body.String(); got != string(sentinel) {
		t.Errorf("body = %q, want cached sentinel %q", got, sentinel)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=300")
	}
}

func TestNowHandlerKeySeparation(t *testing.T) {
	// Seed a sentinel for one coordinate; a different coordinate must miss it.
	seedLat, seedLon := 51.0, 61.0
	nowCache.set(nowKey(seedLat, seedLon, time.Now()), []byte(`{"sentinel":"seeded"}`))

	req := httptest.NewRequest("GET", "/api/skywatch/now?lat=52&lon=62", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if w.Body.String() == `{"sentinel":"seeded"}` {
		t.Error("different coordinates should not return another key's cached entry")
	}
	var resp NowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Location.Lat != 52 {
		t.Errorf("location lat = %v, want 52", resp.Location.Lat)
	}
}

func TestNowHandlerDefaultCoordsCached(t *testing.T) {
	date := "2026-01-15"
	key := fmt.Sprintf("%v|%v|%s", DefaultLat, DefaultLon, date)
	nowCache.mu.Lock()
	delete(nowCache.entries, key)
	nowCache.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/skywatch/now?date="+date, nil)
	w := httptest.NewRecorder()
	NowHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if _, ok := nowCache.get(key); !ok {
		t.Error("default-coords request should populate the cache")
	}
}

func TestMoonHandlerCacheControl(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/skywatch/moon?days=10&lat=12&lon=23", nil)
	w := httptest.NewRecorder()

	MoonCalendarHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=3600")
	}

	var resp struct {
		Days     int               `json:"days"`
		Calendar []MoonCalendarDay `json:"calendar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Days != 10 || len(resp.Calendar) != 10 {
		t.Errorf("days = %d, calendar len = %d, want 10/10", resp.Days, len(resp.Calendar))
	}
}

func TestMoonHandlerServesFromCache(t *testing.T) {
	lat, lon, days := 33.0, 44.0, 7

	// Call the handler once to discover the date it uses internally,
	// avoiding a flaky mismatch if the test runs across midnight.
	req1 := httptest.NewRequest("GET", "/api/skywatch/moon?days=7&lat=33&lon=44", nil)
	w1 := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w1, req1)

	var first struct {
		Calendar []struct {
			Date string `json:"date"`
		} `json:"calendar"`
	}
	if err := json.NewDecoder(w1.Body).Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	date := first.Calendar[0].Date

	key := fmt.Sprintf("%v|%v|%d|%s", lat, lon, days, date)
	sentinel := []byte(`{"sentinel":"moon"}`)
	moonCache.set(key, sentinel)

	req2 := httptest.NewRequest("GET", "/api/skywatch/moon?days=7&lat=33&lon=44", nil)
	w2 := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w2, req2)

	if got := w2.Body.String(); got != string(sentinel) {
		t.Errorf("body = %q, want cached sentinel %q", got, sentinel)
	}
	if got := w2.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=3600")
	}
}

func TestMoonHandlerKeySeparationByDays(t *testing.T) {
	// Different `days` values must not share a cache entry.
	lat, lon := 34.0, 45.0
	moonCache.set(fmt.Sprintf("%v|%v|%d|%s", lat, lon, 5, time.Now().Format("2006-01-02")), []byte(`{"sentinel":"days5"}`))

	req := httptest.NewRequest("GET", "/api/skywatch/moon?days=6&lat=34&lon=45", nil)
	w := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w, req)

	if w.Body.String() == `{"sentinel":"days5"}` {
		t.Error("different days value should not return another key's cached entry")
	}
}

func TestMoonHandlerDefaultDateStartsToday(t *testing.T) {
	before := time.Now().Format("2006-01-02")
	req := httptest.NewRequest("GET", "/api/skywatch/moon?days=3&lat=15&lon=25", nil)
	w := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w, req)
	after := time.Now().Format("2006-01-02")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Calendar []MoonCalendarDay `json:"calendar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Calendar) == 0 {
		t.Fatal("expected non-empty calendar")
	}
	got := resp.Calendar[0].Date
	if got != before && got != after {
		t.Errorf("first date = %q, want today (%q or %q)", got, before, after)
	}
}

func TestMoonHandlerHonorsDateParam(t *testing.T) {
	const date = "2026-03-10"
	req := httptest.NewRequest("GET", "/api/skywatch/moon?days=5&lat=16&lon=26&date="+date, nil)
	w := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Days     int               `json:"days"`
		Calendar []MoonCalendarDay `json:"calendar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Days != 5 || len(resp.Calendar) != 5 {
		t.Fatalf("days = %d, calendar len = %d, want 5/5", resp.Days, len(resp.Calendar))
	}
	if resp.Calendar[0].Date != date {
		t.Errorf("first date = %q, want %q", resp.Calendar[0].Date, date)
	}
	// Subsequent entries increment by one day from the requested date.
	start := time.Date(2026, 3, 10, 12, 0, 0, 0, time.Now().Location())
	for i, day := range resp.Calendar {
		want := start.AddDate(0, 0, i).Format("2006-01-02")
		if day.Date != want {
			t.Errorf("calendar[%d].Date = %q, want %q", i, day.Date, want)
		}
	}
}

func TestMoonHandlerInvalidDate(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/skywatch/moon?date=2026-13-99&lat=17&lon=27", nil)
	w := httptest.NewRecorder()
	MoonCalendarHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["error"] != "invalid date format, expected YYYY-MM-DD" {
		t.Errorf("error = %q, want %q", resp["error"], "invalid date format, expected YYYY-MM-DD")
	}
}

func TestAuroraHandlerCacheControl(t *testing.T) {
	mockObserved := `[
		["time_tag", "Kp"],
		["2026-04-07 12:00:00.000", 1.0]
	]`
	mockForecast := `[
		["time_tag", "Kp"],
		["2026-04-07 21:00:00.000", 2.0]
	]`

	svc := NewAuroraService()
	svc.cache["observed"] = &auroraCached{data: []byte(mockObserved), expires: time.Now().Add(time.Hour)}
	svc.cache["forecast"] = &auroraCached{data: []byte(mockForecast), expires: time.Now().Add(time.Hour)}

	req := httptest.NewRequest("GET", "/api/skywatch/aurora?lat=60.36&lon=5.24", nil)
	w := httptest.NewRecorder()

	svc.AuroraHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=900" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=900")
	}
}
