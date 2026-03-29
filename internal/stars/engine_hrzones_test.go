package stars

import (
	"testing"

	"github.com/Robin831/Hytte/internal/hrzones"
)

// sampleBoundaries returns a typical 5-zone boundary slice (maxHR ≈ 190).
func sampleBoundaries() []hrzones.ZoneBoundary {
	return []hrzones.ZoneBoundary{
		{Zone: 1, MinBPM: 0, MaxBPM: 114},
		{Zone: 2, MinBPM: 114, MaxBPM: 137},
		{Zone: 3, MinBPM: 137, MaxBPM: 156},
		{Zone: 4, MinBPM: 156, MaxBPM: 175},
		{Zone: 5, MinBPM: 175, MaxBPM: 190},
	}
}

func TestHRZoneFromBoundaries_Basic(t *testing.T) {
	zones := sampleBoundaries()
	tests := []struct {
		hr   int
		want int
	}{
		{0, 0},   // invalid HR
		{-10, 0}, // negative HR
		{50, 1},  // well within Z1
		{114, 2}, // exactly at Z2 min (min inclusive)
		{136, 2}, // just below Z3
		{137, 3}, // at Z3 min
		{155, 3}, // within Z3
		{156, 4}, // at Z4 min
		{174, 4}, // within Z4
		{175, 5}, // at Z5 min
		{189, 5}, // within Z5 (below max)
		{190, 5}, // at Z5 max — clamp to Z5
		{210, 5}, // above Z5 max — clamp to Z5
	}
	for _, tt := range tests {
		got := hrZoneFromBoundaries(tt.hr, zones)
		if got != tt.want {
			t.Errorf("hrZoneFromBoundaries(%d) = %d, want %d", tt.hr, got, tt.want)
		}
	}
}

func TestHRZoneFromBoundaries_EmptyZones(t *testing.T) {
	// Empty boundary slice should return 0 for any HR.
	if got := hrZoneFromBoundaries(150, nil); got != 0 {
		t.Errorf("expected 0 for nil zones, got %d", got)
	}
	if got := hrZoneFromBoundaries(150, []hrzones.ZoneBoundary{}); got != 0 {
		t.Errorf("expected 0 for empty zones, got %d", got)
	}
}

func TestComputeTimeInZonesFromBoundaries_Basic(t *testing.T) {
	zones := sampleBoundaries()
	// 60s in Z1 (HR=100), 60s in Z2 (HR=120), 60s in Z3 (HR=140)
	samples := []HRSample{
		{OffsetMs: 0, HeartRate: 100},     // Z1: covers 0→60 000 ms = 60s in Z1
		{OffsetMs: 60000, HeartRate: 120}, // Z2: covers 60→120 000 ms = 60s in Z2
		{OffsetMs: 120000, HeartRate: 140}, // Z3: covers 120→180 000 ms = 60s in Z3
		{OffsetMs: 180000, HeartRate: 0},  // sentinel — no interval after this
	}
	result := computeTimeInZonesFromBoundaries(samples, zones)
	if result[1] != 60 {
		t.Errorf("Z1 = %.0f, want 60", result[1])
	}
	if result[2] != 60 {
		t.Errorf("Z2 = %.0f, want 60", result[2])
	}
	if result[3] != 60 {
		t.Errorf("Z3 = %.0f, want 60", result[3])
	}
	if result[4] != 0 {
		t.Errorf("Z4 = %.0f, want 0", result[4])
	}
	if result[5] != 0 {
		t.Errorf("Z5 = %.0f, want 0", result[5])
	}
}

func TestComputeTimeInZonesFromBoundaries_ZeroHR(t *testing.T) {
	zones := sampleBoundaries()
	// Samples with zero HR should not be counted in any zone.
	samples := []HRSample{
		{OffsetMs: 0, HeartRate: 0},
		{OffsetMs: 30000, HeartRate: 120},
		{OffsetMs: 60000, HeartRate: 0},
	}
	result := computeTimeInZonesFromBoundaries(samples, zones)
	// Interval 0→30s has HR=0 → skipped.
	// Interval 30→60s has HR=120 → Z2.
	if result[1] != 0 {
		t.Errorf("Z1 = %.0f, want 0", result[1])
	}
	if result[2] != 30 {
		t.Errorf("Z2 = %.0f, want 30", result[2])
	}
}

func TestComputeTimeInZonesFromBoundaries_EmptySamples(t *testing.T) {
	zones := sampleBoundaries()
	// Fewer than 2 samples produces all-zero result.
	result := computeTimeInZonesFromBoundaries(nil, zones)
	for i := 0; i <= 5; i++ {
		if result[i] != 0 {
			t.Errorf("zone[%d] = %.0f, want 0 for nil samples", i, result[i])
		}
	}
	result = computeTimeInZonesFromBoundaries([]HRSample{{OffsetMs: 0, HeartRate: 150}}, zones)
	for i := 0; i <= 5; i++ {
		if result[i] != 0 {
			t.Errorf("zone[%d] = %.0f, want 0 for single sample", i, result[i])
		}
	}
}

func TestComputeTimeInZonesFromBoundaries_HighHRClampedToZ5(t *testing.T) {
	zones := sampleBoundaries()
	// HR above Z5 max should be clamped to Z5.
	samples := []HRSample{
		{OffsetMs: 0, HeartRate: 210}, // above Z5 max (190) — clamped to Z5
		{OffsetMs: 60000, HeartRate: 0},
	}
	result := computeTimeInZonesFromBoundaries(samples, zones)
	if result[5] != 60 {
		t.Errorf("Z5 = %.0f, want 60 for HR above max", result[5])
	}
}
