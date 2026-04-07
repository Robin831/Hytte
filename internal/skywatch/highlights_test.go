package skywatch

import (
	"math"
	"testing"
	"time"
)

func TestAngularSepAltAz(t *testing.T) {
	tests := []struct {
		name     string
		alt1     float64
		az1      float64
		alt2     float64
		az2      float64
		wantDeg  float64
		tolerance float64
	}{
		{
			name:     "same position",
			alt1:     45, az1: 180,
			alt2:     45, az2: 180,
			wantDeg:  0,
			tolerance: 0.01,
		},
		{
			name:     "opposite azimuth at horizon",
			alt1:     0, az1: 0,
			alt2:     0, az2: 180,
			wantDeg:  180,
			tolerance: 0.01,
		},
		{
			name:     "zenith to horizon",
			alt1:     90, az1: 0,
			alt2:     0, az2: 0,
			wantDeg:  90,
			tolerance: 0.01,
		},
		{
			name:     "small separation",
			alt1:     45, az1: 100,
			alt2:     47, az2: 101,
			wantDeg:  2.12,
			tolerance: 0.1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := angularSepAltAz(tc.alt1, tc.az1, tc.alt2, tc.az2)
			if math.Abs(got-tc.wantDeg) > tc.tolerance {
				t.Errorf("angularSepAltAz(%v, %v, %v, %v) = %v, want %v (±%v)",
					tc.alt1, tc.az1, tc.alt2, tc.az2, got, tc.wantDeg, tc.tolerance)
			}
		})
	}
}

func TestFormatDeg(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{3.14159, "3.1"},
		{0.05, "0.1"},
		{10.0, "10.0"},
	}
	for _, tc := range tests {
		got := formatDeg(tc.input)
		if got != tc.want {
			t.Errorf("formatDeg(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPlanetNameKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Venus", "venus"},
		{"Mars", "mars"},
		{"Jupiter", "jupiter"},
	}
	for _, tc := range tests {
		got := planetNameKey(tc.input)
		if got != tc.want {
			t.Errorf("planetNameKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGetTonightHighlightsReturnsSlice(t *testing.T) {
	// Bergen, Norway on a known date.
	date := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	highlights := GetTonightHighlights(date, DefaultLat, DefaultLon)

	// Should return a non-nil slice (may be empty if no events).
	if highlights == nil {
		t.Error("GetTonightHighlights returned nil, want non-nil slice")
	}

	// Verify all highlights have required fields.
	for i, h := range highlights {
		if h.Type == "" {
			t.Errorf("highlight[%d] has empty Type", i)
		}
		if h.Key == "" {
			t.Errorf("highlight[%d] has empty Key", i)
		}
		if h.Params == nil {
			t.Errorf("highlight[%d] has nil Params", i)
		}

		switch h.Type {
		case "moon_conjunction":
			if h.Params["planetKey"] == "" {
				t.Errorf("highlight[%d] moon_conjunction missing planetKey", i)
			}
			if h.Params["degrees"] == "" {
				t.Errorf("highlight[%d] moon_conjunction missing degrees", i)
			}
		case "planet_conjunction":
			if h.Params["planet1Key"] == "" || h.Params["planet2Key"] == "" {
				t.Errorf("highlight[%d] planet_conjunction missing planet keys", i)
			}
		case "opposition":
			if h.Params["planetKey"] == "" {
				t.Errorf("highlight[%d] opposition missing planetKey", i)
			}
		case "bright_planet":
			if h.Params["planetKey"] == "" || h.Params["direction"] == "" {
				t.Errorf("highlight[%d] bright_planet missing fields", i)
			}
		default:
			t.Errorf("highlight[%d] has unknown type %q", i, h.Type)
		}
	}
}

func TestGetTonightHighlightsValidKeys(t *testing.T) {
	validKeys := map[string]bool{
		"highlights.moonNearPlanet":    true,
		"highlights.planetConjunction": true,
		"highlights.opposition":        true,
		"highlights.brightPlanet":      true,
	}

	validPlanetKeys := map[string]bool{
		"mercury": true,
		"venus":   true,
		"mars":    true,
		"jupiter": true,
		"saturn":  true,
	}

	// Test across a few different dates to get varied results.
	dates := []time.Time{
		time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC),
	}

	for _, date := range dates {
		highlights := GetTonightHighlights(date, DefaultLat, DefaultLon)
		for i, h := range highlights {
			if !validKeys[h.Key] {
				t.Errorf("date=%s highlight[%d] has invalid key %q", date.Format("2006-01-02"), i, h.Key)
			}
			// Check planet keys are valid.
			if pk := h.Params["planetKey"]; pk != "" && !validPlanetKeys[pk] {
				t.Errorf("date=%s highlight[%d] has invalid planetKey %q", date.Format("2006-01-02"), i, pk)
			}
			if pk := h.Params["planet1Key"]; pk != "" && !validPlanetKeys[pk] {
				t.Errorf("date=%s highlight[%d] has invalid planet1Key %q", date.Format("2006-01-02"), i, pk)
			}
			if pk := h.Params["planet2Key"]; pk != "" && !validPlanetKeys[pk] {
				t.Errorf("date=%s highlight[%d] has invalid planet2Key %q", date.Format("2006-01-02"), i, pk)
			}
		}
	}
}
