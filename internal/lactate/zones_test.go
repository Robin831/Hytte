package lactate

import (
	"math"
	"testing"
)

const floatTol = 1e-9

func TestCalculateZonesOlympiatoppen(t *testing.T) {
	result := CalculateZones(ZoneSystemOlympiatoppen, 14.0, 180)
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
}

func TestCalculateZonesNorwegian(t *testing.T) {
	result := CalculateZones(ZoneSystemNorwegian, 14.0, 180)
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
