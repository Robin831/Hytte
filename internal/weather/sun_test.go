package weather

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Oslo coordinates, used for mid-latitude sanity checks.
const (
	osloLat = 59.9139
	osloLon = 10.7522
)

func newFixedClockService(t time.Time) *Service {
	svc := NewService()
	svc.now = func() time.Time { return t }
	return svc
}

func TestComputeSunDataMidLatitude(t *testing.T) {
	// Spring equinox 2026 — day length should be close to 12 hours everywhere.
	date := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	sun := ComputeSunData(osloLat, osloLon, date)

	if sun.PolarDay || sun.PolarNight {
		t.Fatalf("expected normal day at Oslo equinox, got polarDay=%v polarNight=%v", sun.PolarDay, sun.PolarNight)
	}
	if sun.Sunrise == nil || sun.Sunset == nil {
		t.Fatal("expected non-nil sunrise and sunset")
	}
	if !sun.Sunset.After(*sun.Sunrise) {
		t.Fatalf("expected sunset (%v) after sunrise (%v)", sun.Sunset, sun.Sunrise)
	}

	// Both events should fall on the requested calendar date (UTC).
	if y, m, d := sun.Sunrise.UTC().Date(); y != 2026 || m != time.March || d != 20 {
		t.Errorf("sunrise on unexpected date: %v", sun.Sunrise)
	}

	// Near the equinox the day length is ~12h; allow a generous tolerance for
	// the equation-of-time and refraction offsets.
	const wantSeconds = 12 * 3600
	const tolerance = 45 * 60 // 45 minutes
	if diff := sun.DaylightSeconds - wantSeconds; diff < -tolerance || diff > tolerance {
		t.Errorf("daylight %ds not within tolerance of 12h", sun.DaylightSeconds)
	}
}

func TestComputeSunDataDaylightMatchesInterval(t *testing.T) {
	// Daylight length must equal the sunset-minus-sunrise interval.
	date := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	sun := ComputeSunData(osloLat, osloLon, date)

	if sun.Sunrise == nil || sun.Sunset == nil {
		t.Fatal("expected non-nil sunrise and sunset at Oslo in June")
	}
	want := int(sun.Sunset.Sub(*sun.Sunrise).Round(time.Second).Seconds())
	if sun.DaylightSeconds != want {
		t.Errorf("DaylightSeconds=%d, want %d (sunset-sunrise)", sun.DaylightSeconds, want)
	}

	// Oslo in midsummer has long days — clearly more than 12 hours.
	if sun.DaylightSeconds <= 12*3600 {
		t.Errorf("expected long midsummer day in Oslo, got %ds", sun.DaylightSeconds)
	}
}

func TestComputeSunDataPolarDay(t *testing.T) {
	// Tromsø (well north of the Arctic Circle) has the midnight sun in June.
	const tromsoLat = 69.6492
	const tromsoLon = 18.9553
	date := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	sun := ComputeSunData(tromsoLat, tromsoLon, date)

	if !sun.PolarDay {
		t.Fatalf("expected polar day (midnight sun) at Tromsø in June, got %+v", sun)
	}
	if sun.PolarNight {
		t.Error("polar day and polar night must be mutually exclusive")
	}
	if sun.Sunrise != nil || sun.Sunset != nil {
		t.Error("expected nil sunrise/sunset on polar day")
	}
	if sun.DaylightSeconds != 24*3600 {
		t.Errorf("expected 24h of daylight on polar day, got %ds", sun.DaylightSeconds)
	}
}

func TestComputeSunDataPolarNight(t *testing.T) {
	// Tromsø has polar night around the winter solstice.
	const tromsoLat = 69.6492
	const tromsoLon = 18.9553
	date := time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC)
	sun := ComputeSunData(tromsoLat, tromsoLon, date)

	if !sun.PolarNight {
		t.Fatalf("expected polar night at Tromsø in December, got %+v", sun)
	}
	if sun.PolarDay {
		t.Error("polar day and polar night must be mutually exclusive")
	}
	if sun.Sunrise != nil || sun.Sunset != nil {
		t.Error("expected nil sunrise/sunset on polar night")
	}
	if sun.DaylightSeconds != 0 {
		t.Errorf("expected 0s of daylight on polar night, got %ds", sun.DaylightSeconds)
	}
}

func TestComputeSunDataExactPole(t *testing.T) {
	summer := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	winter := time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		lat       float64
		date      time.Time
		wantPolar string
	}{
		{"north pole summer", 90, summer, "day"},
		{"north pole winter", 90, winter, "night"},
		{"south pole summer", -90, winter, "day"},
		{"south pole winter", -90, summer, "night"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sun := ComputeSunData(tc.lat, 0, tc.date)
			if tc.wantPolar == "day" {
				if !sun.PolarDay {
					t.Errorf("expected polar day, got %+v", sun)
				}
				if sun.DaylightSeconds != 24*3600 {
					t.Errorf("expected 24h daylight, got %ds", sun.DaylightSeconds)
				}
			} else {
				if !sun.PolarNight {
					t.Errorf("expected polar night, got %+v", sun)
				}
				if sun.DaylightSeconds != 0 {
					t.Errorf("expected 0s daylight, got %ds", sun.DaylightSeconds)
				}
			}
			if sun.Sunrise != nil || sun.Sunset != nil {
				t.Error("expected nil sunrise/sunset for polar condition")
			}
		})
	}
}

func TestSunCacheEviction(t *testing.T) {
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	svc := newFixedClockService(fixed)

	for i := range maxSunCacheEntries + 10 {
		lat := float64(i) * 0.01
		svc.sunDataCached(lat, 10.0)
	}

	svc.sunMu.RLock()
	n := len(svc.sunCache)
	svc.sunMu.RUnlock()

	if n > maxSunCacheEntries {
		t.Errorf("cache should be bounded to %d entries, got %d", maxSunCacheEntries, n)
	}
}

func TestSunHandlerSuccess(t *testing.T) {
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	svc := newFixedClockService(fixed)
	handler := svc.SunHandler()

	req := httptest.NewRequest("GET", "/api/weather/sun?lat=59.9139&lon=10.7522", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp SunResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Oslo is never in polar day/night, so we expect concrete sun times unless
	// the daylight reflects a polar condition — either way, no NaN/garbage.
	if resp.PolarDay && resp.PolarNight {
		t.Error("polarDay and polarNight cannot both be true")
	}
	if resp.DaylightSeconds < 0 || resp.DaylightSeconds > 24*3600 {
		t.Errorf("daylightSeconds out of range: %d", resp.DaylightSeconds)
	}
}

func TestSunHandlerCaches(t *testing.T) {
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	svc := newFixedClockService(fixed)

	// First call computes and stores in the cache.
	first := svc.sunDataCached(osloLat, osloLon)

	svc.sunMu.RLock()
	n := len(svc.sunCache)
	svc.sunMu.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 cached entry after first call, got %d", n)
	}

	// Second call for the same location/date should reuse the cache and not add
	// a new entry.
	second := svc.sunDataCached(osloLat, osloLon)
	svc.sunMu.RLock()
	n = len(svc.sunCache)
	svc.sunMu.RUnlock()
	if n != 1 {
		t.Errorf("expected cache to be reused, got %d entries", n)
	}
	if first.DaylightSeconds != second.DaylightSeconds {
		t.Errorf("cached result differs: %d vs %d", first.DaylightSeconds, second.DaylightSeconds)
	}
}

func TestSunCacheInvalidatesOnDayChange(t *testing.T) {
	day1 := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	clock := day1
	svc := NewService()
	svc.now = func() time.Time { return clock }

	first := svc.sunDataCached(osloLat, osloLon)

	svc.sunMu.RLock()
	n := len(svc.sunCache)
	svc.sunMu.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 cached entry, got %d", n)
	}

	clock = day1.Add(24 * time.Hour)
	second := svc.sunDataCached(osloLat, osloLon)

	if first.DaylightSeconds == second.DaylightSeconds {
		t.Log("daylight seconds happen to match across days — acceptable but unusual")
	}

	svc.sunMu.RLock()
	n = len(svc.sunCache)
	svc.sunMu.RUnlock()
	if n != 1 {
		t.Errorf("expected old entry replaced, got %d entries", n)
	}
}

func TestSunHandlerValidation(t *testing.T) {
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	svc := newFixedClockService(fixed)
	handler := svc.SunHandler()

	cases := []struct {
		name  string
		query string
	}{
		{"missing both", "/api/weather/sun"},
		{"missing lon", "/api/weather/sun?lat=59.9"},
		{"invalid lat", "/api/weather/sun?lat=abc&lon=10"},
		{"lat out of range", "/api/weather/sun?lat=120&lon=10"},
		{"lon out of range", "/api/weather/sun?lat=59&lon=200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.query, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})
	}
}
