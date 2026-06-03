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

func TestSunHandlerSuccess(t *testing.T) {
	svc := NewService()
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
	svc := NewService()

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

func TestSunHandlerValidation(t *testing.T) {
	svc := NewService()
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
