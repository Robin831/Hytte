package skywatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMoonPhaseName(t *testing.T) {
	tests := []struct {
		phase float64
		want  string
	}{
		{0.0, "New Moon"},
		{0.1, "Waxing Crescent"},
		{0.25, "First Quarter"},
		{0.4, "Waxing Gibbous"},
		{0.5, "Full Moon"},
		{0.6, "Waning Gibbous"},
		{0.75, "Last Quarter"},
		{0.85, "Waning Crescent"},
		{0.95, "New Moon"},
		{1.0, "New Moon"},
	}
	for _, tt := range tests {
		got := MoonPhaseName(tt.phase)
		if got != tt.want {
			t.Errorf("MoonPhaseName(%v) = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestGetMoonPhase(t *testing.T) {
	// Use a known date: 2024-01-25 is roughly a full moon.
	date := time.Date(2024, 1, 25, 12, 0, 0, 0, time.UTC)
	info := GetMoonPhase(date, DefaultLat, DefaultLon)

	if info.Phase == "" {
		t.Error("expected non-empty phase name")
	}
	if info.Illumination < 0 || info.Illumination > 100 {
		t.Errorf("illumination %v out of range [0, 100]", info.Illumination)
	}
	if info.PhaseValue < 0 || info.PhaseValue > 1 {
		t.Errorf("phase value %v out of range [0, 1]", info.PhaseValue)
	}
}

func TestGetSunTimes(t *testing.T) {
	// Summer solstice in Bergen — sun definitely rises and sets.
	date := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	sun := GetSunTimes(date, DefaultLat, DefaultLon)

	if sun.Sunrise == nil {
		t.Error("expected non-nil sunrise")
	}
	if sun.Sunset == nil {
		t.Error("expected non-nil sunset")
	}
	if sun.SolarNoon == nil {
		t.Error("expected non-nil solar noon")
	}
	if sun.DayLength <= 0 {
		t.Errorf("expected positive day length, got %v", sun.DayLength)
	}
	// Bergen at solstice should have >18 hours of daylight.
	if sun.DayLength < 18 {
		t.Errorf("Bergen summer solstice should have >18h daylight, got %.1f", sun.DayLength)
	}
}

func TestGetSunTimesWinter(t *testing.T) {
	// Winter solstice in Bergen — short day.
	date := time.Date(2024, 12, 21, 12, 0, 0, 0, time.UTC)
	sun := GetSunTimes(date, DefaultLat, DefaultLon)

	if sun.DayLength <= 0 {
		t.Errorf("expected positive day length, got %v", sun.DayLength)
	}
	// Bergen at winter solstice should have <7 hours of daylight.
	if sun.DayLength > 7 {
		t.Errorf("Bergen winter solstice should have <7h daylight, got %.1f", sun.DayLength)
	}
}

func TestNowHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/now", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if resp.Location.Lat != DefaultLat || resp.Location.Lon != DefaultLon {
		t.Errorf("expected default location, got %+v", resp.Location)
	}
	if resp.Moon.Phase == "" {
		t.Error("expected non-empty moon phase")
	}
	if resp.Sun.Sunrise == nil {
		t.Error("expected non-nil sunrise")
	}
}

func TestNowHandlerCustomCoords(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/now?lat=59.9139&lon=10.7522", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Location.Lat != 59.9139 {
		t.Errorf("expected lat 59.9139, got %v", resp.Location.Lat)
	}
	if resp.Location.Lon != 10.7522 {
		t.Errorf("expected lon 10.7522, got %v", resp.Location.Lon)
	}
}

func TestMoonCalendarHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/moon?days=7", nil)
	w := httptest.NewRecorder()

	MoonCalendarHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Days     int               `json:"days"`
		Calendar []MoonCalendarDay `json:"calendar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Days != 7 {
		t.Errorf("expected 7 days, got %d", resp.Days)
	}
	if len(resp.Calendar) != 7 {
		t.Fatalf("expected 7 calendar entries, got %d", len(resp.Calendar))
	}
	for i, entry := range resp.Calendar {
		if entry.Date == "" {
			t.Errorf("calendar[%d]: empty date", i)
		}
		if entry.Phase == "" {
			t.Errorf("calendar[%d]: empty phase", i)
		}
		if entry.Illumination < 0 || entry.Illumination > 100 {
			t.Errorf("calendar[%d]: illumination %v out of range", i, entry.Illumination)
		}
	}
}

func TestMoonCalendarHandlerDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/moon", nil)
	w := httptest.NewRecorder()

	MoonCalendarHandler().ServeHTTP(w, req)

	var resp struct {
		Days     int               `json:"days"`
		Calendar []MoonCalendarDay `json:"calendar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Days != 30 {
		t.Errorf("expected default 30 days, got %d", resp.Days)
	}
	if len(resp.Calendar) != 30 {
		t.Errorf("expected 30 calendar entries, got %d", len(resp.Calendar))
	}
}

func TestMoonCalendarHandlerInvalidDays(t *testing.T) {
	// Invalid days parameter should fall back to 30.
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/moon?days=-5", nil)
	w := httptest.NewRecorder()

	MoonCalendarHandler().ServeHTTP(w, req)

	var resp struct {
		Days int `json:"days"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Days != 30 {
		t.Errorf("expected default 30 days for invalid input, got %d", resp.Days)
	}
}

func TestNowHandlerWithDate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/skywatch/now?date=2024-06-21", nil)
	w := httptest.NewRecorder()

	NowHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Timestamp should reflect the requested date (noon of 2024-06-21).
	ts, err := time.Parse(time.RFC3339, resp.Timestamp)
	if err != nil {
		t.Fatalf("failed to parse timestamp: %v", err)
	}
	if ts.Year() != 2024 || ts.Month() != 6 || ts.Day() != 21 {
		t.Errorf("expected date 2024-06-21, got %s", resp.Timestamp)
	}
	if ts.Hour() != 12 {
		t.Errorf("expected noon (12), got hour %d", ts.Hour())
	}
}

func TestNowHandlerInvalidDate(t *testing.T) {
	for _, bad := range []string{"not-a-date", "2024/06/21", "06-21-2024", "20240621"} {
		req := httptest.NewRequest(http.MethodGet, "/api/skywatch/now?date="+bad, nil)
		w := httptest.NewRecorder()

		NowHandler().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("date=%q: expected 400, got %d", bad, w.Code)
		}
		var errResp map[string]string
		if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
			t.Fatalf("date=%q: failed to decode error response: %v", bad, err)
		}
		if errResp["error"] == "" {
			t.Errorf("date=%q: expected non-empty error message", bad)
		}
	}
}

func TestParseCoordsValidation(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantErr    bool
		wantStatus int
	}{
		{"lat only", "?lat=60.0", true, http.StatusBadRequest},
		{"lon only", "?lon=5.0", true, http.StatusBadRequest},
		{"lat out of range high", "?lat=91&lon=5", true, http.StatusBadRequest},
		{"lat out of range low", "?lat=-91&lon=5", true, http.StatusBadRequest},
		{"lon out of range high", "?lat=60&lon=181", true, http.StatusBadRequest},
		{"lon out of range low", "?lat=60&lon=-181", true, http.StatusBadRequest},
		{"invalid lat format", "?lat=abc&lon=5", true, http.StatusBadRequest},
		{"invalid lon format", "?lat=60&lon=abc", true, http.StatusBadRequest},
		{"valid coords", "?lat=60&lon=5", false, http.StatusOK},
		{"no coords defaults", "", false, http.StatusOK},
		{"boundary lat 90", "?lat=90&lon=0", false, http.StatusOK},
		{"boundary lat -90", "?lat=-90&lon=0", false, http.StatusOK},
		{"boundary lon 180", "?lat=0&lon=180", false, http.StatusOK},
		{"boundary lon -180", "?lat=0&lon=-180", false, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/skywatch/now"+tt.query, nil)
			w := httptest.NewRecorder()
			NowHandler().ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantErr {
				var errResp map[string]string
				if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
					t.Fatalf("failed to decode error response: %v", err)
				}
				if errResp["error"] == "" {
					t.Error("expected non-empty error message")
				}
			}
		})
	}
}
