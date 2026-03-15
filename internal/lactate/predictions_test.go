package lactate

import (
	"math"
	"testing"
)

func TestPredictRaceTimes(t *testing.T) {
	// Threshold speed of 14 km/h (4:17/km) — typical trained runner
	predictions := PredictRaceTimes(14.0)
	if predictions == nil {
		t.Fatal("expected non-nil predictions")
	}
	if len(predictions) != len(StandardDistances) {
		t.Fatalf("expected %d predictions, got %d", len(StandardDistances), len(predictions))
	}

	// All times should be positive
	for _, p := range predictions {
		if p.TimeSeconds <= 0 {
			t.Errorf("prediction for %s has non-positive time: %f", p.Name, p.TimeSeconds)
		}
		if p.SpeedKmh <= 0 {
			t.Errorf("prediction for %s has non-positive speed: %f", p.Name, p.SpeedKmh)
		}
		if p.TimeFormatted == "" {
			t.Errorf("prediction for %s has empty formatted time", p.Name)
		}
		if p.PaceMinKm == "" {
			t.Errorf("prediction for %s has empty pace", p.Name)
		}
	}

	// 5K should be faster than 10K which should be faster than half marathon
	idx5k := 2  // "5K"
	idx10k := 3 // "10K"
	idxHM := 4  // "Half Marathon"
	if predictions[idx5k].TimeSeconds >= predictions[idx10k].TimeSeconds {
		t.Error("5K time should be less than 10K time")
	}
	if predictions[idx10k].TimeSeconds >= predictions[idxHM].TimeSeconds {
		t.Error("10K time should be less than half marathon time")
	}

	// Speed should decrease as distance increases (fatigue factor)
	if predictions[idx5k].SpeedKmh <= predictions[idxHM].SpeedKmh {
		t.Error("5K speed should be faster than half marathon speed")
	}
}

func TestPredictRaceTimesZeroSpeed(t *testing.T) {
	predictions := PredictRaceTimes(0)
	if predictions != nil {
		t.Error("expected nil predictions for zero speed")
	}
}

func TestPredictRaceTimesNegativeSpeed(t *testing.T) {
	predictions := PredictRaceTimes(-5.0)
	if predictions != nil {
		t.Error("expected nil predictions for negative speed")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{300, "5:00"},
		{3661, "1:01:01"},
		{5400, "1:30:00"},
		{1234, "20:34"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.seconds)
		if got != tc.want {
			t.Errorf("formatDuration(%f) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestFormatPace(t *testing.T) {
	got := formatPace(300) // 5:00/km
	if got != "5:00/km" {
		t.Errorf("formatPace(300) = %q, want %q", got, "5:00/km")
	}
}

func TestRiegelFormulaSanity(t *testing.T) {
	// At threshold speed of 15 km/h, 5K should take roughly:
	// ref distance = 15 km in 3600s
	// 5K time = 3600 * (5/15)^1.06
	predictions := PredictRaceTimes(15.0)
	fiveKTime := predictions[2].TimeSeconds // 5K
	expected := 3600.0 * math.Pow(5.0/15.0, riegelExponent)
	if math.Abs(fiveKTime-round2(expected)) > 1.0 {
		t.Errorf("5K time %f differs from expected %f by more than 1 second", fiveKTime, expected)
	}
}
