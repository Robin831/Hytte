package skywatch

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetPlanetPositions(t *testing.T) {
	// Use a known date for reproducible results.
	date := time.Date(2024, 3, 15, 22, 0, 0, 0, time.UTC)
	planets := GetPlanetPositions(date, DefaultLat, DefaultLon)

	if len(planets) != 5 {
		t.Fatalf("expected 5 planets, got %d", len(planets))
	}

	expectedNames := []PlanetName{Mercury, Venus, Mars, Jupiter, Saturn}
	for i, p := range planets {
		if p.Name != expectedNames[i] {
			t.Errorf("planet[%d]: expected %s, got %s", i, expectedNames[i], p.Name)
		}
		// Altitude should be in [-90, 90]
		if p.Altitude < -90 || p.Altitude > 90 {
			t.Errorf("%s: altitude %v out of range [-90, 90]", p.Name, p.Altitude)
		}
		// Azimuth should be in [0, 360)
		if p.Azimuth < 0 || p.Azimuth >= 360 {
			t.Errorf("%s: azimuth %v out of range [0, 360)", p.Name, p.Azimuth)
		}
		// Direction should be a valid compass direction
		validDirs := map[string]bool{"N": true, "NE": true, "E": true, "SE": true, "S": true, "SW": true, "W": true, "NW": true}
		if !validDirs[p.Direction] {
			t.Errorf("%s: invalid direction %q", p.Name, p.Direction)
		}
		// Status should be one of the valid values
		validStatuses := map[string]bool{"visible_now": true, "rises_at": true, "not_visible": true}
		if !validStatuses[p.Status] {
			t.Errorf("%s: invalid status %q", p.Name, p.Status)
		}
		// Elongation should be in [0, 180]
		if p.Elongation < 0 || p.Elongation > 180 {
			t.Errorf("%s: elongation %v out of range [0, 180]", p.Name, p.Elongation)
		}
	}
}

func TestCompassDirection(t *testing.T) {
	tests := []struct {
		azimuth float64
		want    string
	}{
		{0, "N"},
		{22.4, "N"},
		{22.5, "NE"},
		{45, "NE"},
		{90, "E"},
		{135, "SE"},
		{180, "S"},
		{225, "SW"},
		{270, "W"},
		{315, "NW"},
		{337.5, "N"},
		{359, "N"},
	}
	for _, tt := range tests {
		got := compassDirection(tt.azimuth)
		if got != tt.want {
			t.Errorf("compassDirection(%v) = %q, want %q", tt.azimuth, got, tt.want)
		}
	}
}

func TestSolveKepler(t *testing.T) {
	// For e=0, E should equal M
	E := solveKepler(90, 0)
	expected := 90 * deg2rad
	if math.Abs(E-expected) > 1e-10 {
		t.Errorf("Kepler e=0: got %v, want %v", E, expected)
	}

	// For small eccentricity, E should be close to M
	E = solveKepler(45, 0.01)
	M := 45 * deg2rad
	if math.Abs(E-M) > 0.02 {
		t.Errorf("Kepler e=0.01: E too far from M: %v vs %v", E, M)
	}
}

func TestNormalizeAngle(t *testing.T) {
	tests := []struct {
		input, want float64
	}{
		{0, 0},
		{360, 0},
		{-90, 270},
		{720, 0},
		{450, 90},
		{-180, 180},
	}
	for _, tt := range tests {
		got := normalizeAngle(tt.input)
		if math.Abs(got-tt.want) > 1e-10 {
			t.Errorf("normalizeAngle(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPlanetVisibilityConsistency(t *testing.T) {
	// If a planet is visible, it must be above horizon
	date := time.Date(2024, 8, 15, 23, 0, 0, 0, time.UTC)
	planets := GetPlanetPositions(date, DefaultLat, DefaultLon)

	for _, p := range planets {
		if p.Visible && p.Altitude <= 0 {
			t.Errorf("%s: marked visible but altitude is %v", p.Name, p.Altitude)
		}
		if p.Status == "visible_now" && !p.Visible {
			t.Errorf("%s: status is visible_now but visible flag is false", p.Name)
		}
		if p.Status == "rises_at" && p.RiseTime == nil {
			t.Errorf("%s: status is rises_at but rise_time is nil", p.Name)
		}
	}
}

func TestNowHandlerIncludesPlanets(t *testing.T) {
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

	if len(resp.Planets) != 5 {
		t.Errorf("expected 5 planets, got %d", len(resp.Planets))
	}

	// Verify planet names are in order
	expectedNames := []PlanetName{Mercury, Venus, Mars, Jupiter, Saturn}
	for i, p := range resp.Planets {
		if p.Name != expectedNames[i] {
			t.Errorf("planet[%d]: expected %s, got %s", i, expectedNames[i], p.Name)
		}
	}
}

func TestPlanetMagnitudeRanges(t *testing.T) {
	// Venus should be bright (negative magnitude), Saturn dimmer
	date := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	planets := GetPlanetPositions(date, DefaultLat, DefaultLon)

	for _, p := range planets {
		// Magnitudes for naked-eye planets should be in roughly [-5, 6]
		if p.Magnitude < -5 || p.Magnitude > 6 {
			t.Errorf("%s: magnitude %v seems out of expected range [-5, 6]", p.Name, p.Magnitude)
		}
	}
}

func TestAngularSeparation(t *testing.T) {
	tests := []struct {
		lon1, lon2, want float64
	}{
		{0, 90, 90},
		{350, 10, 20},
		{0, 180, 180},
		{90, 90, 0},
		{10, 350, 20},
	}
	for _, tt := range tests {
		got := angularSeparation(tt.lon1, tt.lon2)
		if math.Abs(got-tt.want) > 1e-10 {
			t.Errorf("angularSeparation(%v, %v) = %v, want %v", tt.lon1, tt.lon2, got, tt.want)
		}
	}
}
