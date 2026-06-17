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
	key := nowKey(lat, lon, time.Now())
	sentinel := []byte(`{"sentinel":"now"}`)
	nowCache.set(key, sentinel)

	req := httptest.NewRequest("GET", "/api/skywatch/now?lat=31&lon=41", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	// Receiving the sentinel bytes back proves the handler served from cache
	// rather than recomputing planet/moon/sun positions.
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
	// Clear any prior entry for the default-coords key.
	key := nowKey(DefaultLat, DefaultLon, time.Now())
	nowCache.mu.Lock()
	delete(nowCache.entries, key)
	nowCache.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/skywatch/now", nil)
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
	key := fmt.Sprintf("%v|%v|%d|%s", lat, lon, days, time.Now().Format("2006-01-02"))
	sentinel := []byte(`{"sentinel":"moon"}`)
	moonCache.set(key, sentinel)

	req := httptest.NewRequest("GET", "/api/skywatch/moon?days=7&lat=33&lon=44", nil)
	w := httptest.NewRecorder()

	MoonCalendarHandler().ServeHTTP(w, req)

	if got := w.Body.String(); got != string(sentinel) {
		t.Errorf("body = %q, want cached sentinel %q", got, sentinel)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=3600" {
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
