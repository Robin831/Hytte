package lactate

import (
	"math"
	"testing"
)

const floatTol = 1e-6

func TestCalculateZonesOlympiatoppen(t *testing.T) {
	result := CalculateZones(ZoneSystemOlympiatoppen, 14.0, 180, 0)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.System != ZoneSystemOlympiatoppen {
		t.Errorf("expected system olympiatoppen, got %s", result.System)
	}
	if len(result.Zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(result.Zones))
	}

	// Zone 1 should start at 0 speed
	if result.Zones[0].MinSpeedKmh != 0 {
		t.Errorf("zone 1 min speed should be 0, got %f", result.Zones[0].MinSpeedKmh)
	}
	// Zone 1 max speed = 14.0 * 0.72 = 10.08
	if math.Abs(result.Zones[0].MaxSpeedKmh-10.08) > floatTol {
		t.Errorf("zone 1 max speed should be 10.08, got %f", result.Zones[0].MaxSpeedKmh)
	}
	// Zone 1 max HR = 180 * 0.72 = 129.6 → 130
	if result.Zones[0].MaxHR != 130 {
		t.Errorf("zone 1 max HR should be 130, got %d", result.Zones[0].MaxHR)
	}

	// Zone 4 should be threshold zone
	if result.Zones[3].Zone != 4 {
		t.Errorf("expected zone 4, got %d", result.Zones[3].Zone)
	}
	if result.Zones[3].LactateFrom != 4.0 {
		t.Errorf("zone 4 lactate from should be 4.0, got %f", result.Zones[3].LactateFrom)
	}

	// Without max HR, MaxHR field should be 0
	if result.MaxHR != 0 {
		t.Errorf("expected MaxHR 0 when not provided, got %d", result.MaxHR)
	}
}

func TestCalculateZonesNorwegian(t *testing.T) {
	result := CalculateZones(ZoneSystemNorwegian, 14.0, 180, 0)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.System != ZoneSystemNorwegian {
		t.Errorf("expected system norwegian, got %s", result.System)
	}
	if len(result.Zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(result.Zones))
	}
	// Norwegian zone 4 name contains "Terskel"
	if result.Zones[3].Name != "Sone 4 - Terskel" {
		t.Errorf("zone 4 name should be 'Sone 4 - Terskel', got %s", result.Zones[3].Name)
	}
}

func TestCalculateZonesWithMaxHR(t *testing.T) {
	// Bug report scenario: threshold HR 154, max HR 191
	result := CalculateZones(ZoneSystemOlympiatoppen, 14.0, 154, 191)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.MaxHR != 191 {
		t.Errorf("expected MaxHR 191, got %d", result.MaxHR)
	}

	// Zone 5 max HR should be max HR (191), not threshold HR
	zone5 := result.Zones[4]
	if zone5.MaxHR != 191 {
		t.Errorf("zone 5 max HR should be 191 (max HR), got %d", zone5.MaxHR)
	}

	// Zone 5 min HR should be above threshold HR (near threshold)
	if zone5.MinHR <= 154 {
		t.Errorf("zone 5 min HR should be above threshold 154, got %d", zone5.MinHR)
	}

	// Zone 1 max HR should be higher than without max HR
	// Without max HR: 154 * 0.72 = 111
	// With max HR: 154 * 0.72/0.92 ≈ 120
	zone1 := result.Zones[0]
	if zone1.MaxHR < 115 {
		t.Errorf("zone 1 max HR with max HR 191 should be > 115, got %d", zone1.MaxHR)
	}

	// Zone 4 max HR should be near threshold HR
	zone4 := result.Zones[3]
	if zone4.MaxHR != 154 {
		t.Errorf("zone 4 max HR should be threshold HR 154, got %d", zone4.MaxHR)
	}
}

func TestCalculateZonesWithMaxHRBugReport(t *testing.T) {
	// From bug report: max HR 191, threshold HR 154.
	// With max HR scaling:
	//   zone 1 max HR = 0.72/0.92 * 154 ≈ 120
	//   zone 5 min HR = thresholdHR+1 = 155, zone 5 max HR = maxHR = 191
	result := CalculateZones(ZoneSystemOlympiatoppen, 14.0, 154, 191)

	zone5 := result.Zones[4]
	// Zone 5 should span from just above threshold HR to max HR
	if zone5.MaxHR != 191 {
		t.Errorf("zone 5 max HR should be 191, got %d", zone5.MaxHR)
	}
	if zone5.MinHR <= 154 {
		t.Errorf("zone 5 min HR (%d) should be above threshold HR (154)", zone5.MinHR)
	}

	zone1 := result.Zones[0]
	// Zone 1 max HR: 0.72/0.92 * 154 ≈ 120
	if zone1.MaxHR == 0 {
		t.Errorf("zone 1 max HR should be non-zero")
	}

	// Zones should be monotonically increasing
	for i := 1; i < len(result.Zones); i++ {
		if result.Zones[i].MinHR < result.Zones[i-1].MinHR {
			t.Errorf("zone %d min HR (%d) should be >= zone %d min HR (%d)",
				result.Zones[i].Zone, result.Zones[i].MinHR,
				result.Zones[i-1].Zone, result.Zones[i-1].MinHR)
		}
	}
}

func TestCalculateZonesMaxHRIgnoredWhenLowerThanThreshold(t *testing.T) {
	// If maxHR <= thresholdHR, it should be ignored
	result := CalculateZones(ZoneSystemOlympiatoppen, 14.0, 180, 170)
	if result.MaxHR != 0 {
		t.Errorf("MaxHR should be 0 when maxHR <= thresholdHR, got %d", result.MaxHR)
	}
	// Should behave like no max HR
	if result.Zones[4].MaxHR != 180 {
		t.Errorf("zone 5 max HR should be 180 (threshold) when maxHR invalid, got %d", result.Zones[4].MaxHR)
	}
}

func TestClassifyLactate(t *testing.T) {
	tests := []struct {
		lactate   float64
		threshold float64
		want      TrafficLight
	}{
		{1.0, 4.0, TrafficGreen},  // 25% of threshold
		{1.9, 4.0, TrafficGreen},  // just under 50%
		{2.0, 4.0, TrafficYellow}, // exactly 50%
		{3.5, 4.0, TrafficYellow}, // between 50% and 100%
		{4.0, 4.0, TrafficRed},    // at threshold
		{6.0, 4.0, TrafficRed},    // above threshold
	}

	for _, tc := range tests {
		light, _ := ClassifyLactate(tc.lactate, tc.threshold)
		if light != tc.want {
			t.Errorf("ClassifyLactate(%f, %f) = %s, want %s", tc.lactate, tc.threshold, light, tc.want)
		}
	}
}

func TestClassifyLactateDefaultThreshold(t *testing.T) {
	// With threshold <= 0, should default to 4.0
	light, _ := ClassifyLactate(1.0, 0)
	if light != TrafficGreen {
		t.Errorf("expected green for lactate 1.0 with default threshold, got %s", light)
	}
}

func TestClassifyStages(t *testing.T) {
	stages := []Stage{
		{StageNumber: 1, SpeedKmh: 10.0, LactateMmol: 1.0},
		{StageNumber: 2, SpeedKmh: 12.0, LactateMmol: 2.5},
		{StageNumber: 3, SpeedKmh: 14.0, LactateMmol: 5.0},
	}

	results := ClassifyStages(stages, 4.0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Light != TrafficGreen {
		t.Errorf("stage 1 should be green, got %s", results[0].Light)
	}
	if results[1].Light != TrafficYellow {
		t.Errorf("stage 2 should be yellow, got %s", results[1].Light)
	}
	if results[2].Light != TrafficRed {
		t.Errorf("stage 3 should be red, got %s", results[2].Light)
	}
}
