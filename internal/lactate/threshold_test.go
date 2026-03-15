package lactate

import (
	"math"
	"testing"
)

// sampleStages returns a realistic set of lactate test stages.
func sampleStages() []Stage {
	return []Stage{
		{StageNumber: 1, SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{StageNumber: 2, SpeedKmh: 9.0, LactateMmol: 1.1, HeartRateBpm: 140},
		{StageNumber: 3, SpeedKmh: 10.0, LactateMmol: 1.3, HeartRateBpm: 150},
		{StageNumber: 4, SpeedKmh: 11.0, LactateMmol: 1.8, HeartRateBpm: 160},
		{StageNumber: 5, SpeedKmh: 12.0, LactateMmol: 2.5, HeartRateBpm: 168},
		{StageNumber: 6, SpeedKmh: 13.0, LactateMmol: 3.5, HeartRateBpm: 175},
		{StageNumber: 7, SpeedKmh: 14.0, LactateMmol: 5.0, HeartRateBpm: 182},
		{StageNumber: 8, SpeedKmh: 15.0, LactateMmol: 8.0, HeartRateBpm: 190},
	}
}

func TestCalculateThresholds_AllMethods(t *testing.T) {
	stages := sampleStages()
	results := CalculateThresholds(stages)

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	methods := []ThresholdMethod{MethodOBLA, MethodDmax, MethodModDmax, MethodLogLog, MethodExpDmax}
	for i, method := range methods {
		if results[i].Method != method {
			t.Errorf("result[%d]: expected method %s, got %s", i, method, results[i].Method)
		}
	}
}

func TestOBLA_Normal(t *testing.T) {
	pts := sortedPoints(sampleStages())
	result := calcOBLA(pts, 4.0)

	if !result.Valid {
		t.Fatalf("expected valid result, got invalid: %s", result.Reason)
	}
	// 4.0 mmol/L is between stages 6 (3.5) and 7 (5.0)
	// Linear interpolation: speed = 13 + (4.0-3.5)/(5.0-3.5) * (14-13) ≈ 13.33
	if result.SpeedKmh < 13.0 || result.SpeedKmh > 14.0 {
		t.Errorf("OBLA speed %.2f outside expected range [13, 14]", result.SpeedKmh)
	}
	if math.Abs(result.LactateMmol-4.0) > 0.01 {
		t.Errorf("OBLA lactate should be 4.0, got %.2f", result.LactateMmol)
	}
	if result.HeartRateBpm < 175 || result.HeartRateBpm > 182 {
		t.Errorf("OBLA HR %d outside expected range [175, 182]", result.HeartRateBpm)
	}
}

func TestOBLA_NeverReachesThreshold(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 1.5, HeartRateBpm: 140},
		{SpeedKmh: 10.0, LactateMmol: 2.0, HeartRateBpm: 150},
	}
	pts := sortedPoints(stages)
	result := calcOBLA(pts, 4.0)
	if result.Valid {
		t.Error("expected invalid when lactate never reaches 4.0")
	}
}

func TestOBLA_StartsAboveThreshold(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 5.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 6.0, HeartRateBpm: 140},
	}
	pts := sortedPoints(stages)
	result := calcOBLA(pts, 4.0)
	if result.Valid {
		t.Error("expected invalid when lactate starts above 4.0")
	}
}

func TestOBLA_TooFewStages(t *testing.T) {
	stages := []Stage{{SpeedKmh: 10.0, LactateMmol: 3.0, HeartRateBpm: 150}}
	pts := sortedPoints(stages)
	result := calcOBLA(pts, 4.0)
	if result.Valid {
		t.Error("expected invalid with only 1 stage")
	}
}

func TestDmax_Normal(t *testing.T) {
	pts := sortedPoints(sampleStages())
	result := calcDmax(pts)

	if !result.Valid {
		t.Fatalf("expected valid Dmax, got invalid: %s", result.Reason)
	}
	// Dmax threshold should be somewhere in the middle of the data range.
	if result.SpeedKmh < 10.0 || result.SpeedKmh > 14.0 {
		t.Errorf("Dmax speed %.2f outside expected range [10, 14]", result.SpeedKmh)
	}
	if result.LactateMmol < 1.0 || result.LactateMmol > 5.0 {
		t.Errorf("Dmax lactate %.2f outside expected range [1, 5]", result.LactateMmol)
	}
}

func TestDmax_TooFewStages(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 2.0, HeartRateBpm: 140},
		{SpeedKmh: 10.0, LactateMmol: 3.0, HeartRateBpm: 150},
	}
	pts := sortedPoints(stages)
	result := calcDmax(pts)
	if result.Valid {
		t.Error("expected invalid with only 3 stages for Dmax")
	}
}

func TestModDmax_Normal(t *testing.T) {
	pts := sortedPoints(sampleStages())
	result := calcModDmax(pts)

	if !result.Valid {
		t.Fatalf("expected valid ModDmax, got invalid: %s", result.Reason)
	}
	// ModDmax typically gives a slightly higher threshold speed than Dmax.
	if result.SpeedKmh < 10.0 || result.SpeedKmh > 15.0 {
		t.Errorf("ModDmax speed %.2f outside expected range [10, 15]", result.SpeedKmh)
	}
}

func TestModDmax_FlatLactate(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 1.0, HeartRateBpm: 140},
		{SpeedKmh: 10.0, LactateMmol: 1.0, HeartRateBpm: 150},
		{SpeedKmh: 11.0, LactateMmol: 1.0, HeartRateBpm: 160},
	}
	pts := sortedPoints(stages)
	result := calcModDmax(pts)
	if result.Valid {
		t.Error("expected invalid when lactate never rises 0.5 above baseline")
	}
}

func TestLogLog_Normal(t *testing.T) {
	pts := sortedPoints(sampleStages())
	result := calcLogLog(pts)

	if !result.Valid {
		t.Fatalf("expected valid Log-log, got invalid: %s", result.Reason)
	}
	if result.SpeedKmh < 8.0 || result.SpeedKmh > 15.0 {
		t.Errorf("Log-log speed %.2f outside data range", result.SpeedKmh)
	}
}

func TestLogLog_TooFewStages(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 2.0, HeartRateBpm: 140},
	}
	pts := sortedPoints(stages)
	result := calcLogLog(pts)
	if result.Valid {
		t.Error("expected invalid with < 4 stages")
	}
}

func TestExpDmax_Normal(t *testing.T) {
	pts := sortedPoints(sampleStages())
	result := calcExpDmax(pts)

	if !result.Valid {
		t.Fatalf("expected valid ExpDmax, got invalid: %s", result.Reason)
	}
	if result.SpeedKmh < 9.0 || result.SpeedKmh > 14.0 {
		t.Errorf("ExpDmax speed %.2f outside expected range [9, 14]", result.SpeedKmh)
	}
}

func TestExpDmax_TooFewStages(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
		{SpeedKmh: 9.0, LactateMmol: 2.0, HeartRateBpm: 140},
	}
	pts := sortedPoints(stages)
	result := calcExpDmax(pts)
	if result.Valid {
		t.Error("expected invalid with < 3 stages")
	}
}

func TestSortedPoints_OrderBySpeed(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 12.0, LactateMmol: 2.5},
		{SpeedKmh: 8.0, LactateMmol: 1.0},
		{SpeedKmh: 10.0, LactateMmol: 1.5},
	}
	pts := sortedPoints(stages)
	for i := 1; i < len(pts); i++ {
		if pts[i].speed <= pts[i-1].speed {
			t.Errorf("points not sorted: %.1f after %.1f", pts[i].speed, pts[i-1].speed)
		}
	}
}

func TestInterpolateHR(t *testing.T) {
	pts := []point{
		{speed: 10.0, hr: 150},
		{speed: 12.0, hr: 170},
	}
	hr := interpolateHR(pts, 11.0)
	if hr != 160 {
		t.Errorf("expected HR 160, got %d", hr)
	}
}

func TestFitPolynomial_Linear(t *testing.T) {
	// y = 2 + 3x
	x := []float64{1, 2, 3, 4}
	y := []float64{5, 8, 11, 14}
	coeffs := fitPolynomial(x, y, 1)
	if coeffs == nil {
		t.Fatal("fitPolynomial returned nil")
	}
	if math.Abs(coeffs[0]-2.0) > 0.01 || math.Abs(coeffs[1]-3.0) > 0.01 {
		t.Errorf("expected [2, 3], got [%.2f, %.2f]", coeffs[0], coeffs[1])
	}
}

func TestLinearRegression(t *testing.T) {
	x := []float64{1, 2, 3, 4}
	y := []float64{3, 5, 7, 9} // y = 1 + 2x
	intercept, slope := linearRegression(x, y)
	if math.Abs(intercept-1.0) > 0.01 || math.Abs(slope-2.0) > 0.01 {
		t.Errorf("expected intercept=1, slope=2; got intercept=%.2f, slope=%.2f", intercept, slope)
	}
}

func TestCalculateThresholds_EmptyStages(t *testing.T) {
	results := CalculateThresholds([]Stage{})
	for _, r := range results {
		if r.Valid {
			t.Errorf("method %s should be invalid with empty stages", r.Method)
		}
	}
}

func TestOBLA_ExactMatch(t *testing.T) {
	stages := []Stage{
		{SpeedKmh: 10.0, LactateMmol: 3.0, HeartRateBpm: 150},
		{SpeedKmh: 12.0, LactateMmol: 4.0, HeartRateBpm: 170},
	}
	pts := sortedPoints(stages)
	result := calcOBLA(pts, 4.0)
	if !result.Valid {
		t.Fatal("expected valid")
	}
	if math.Abs(result.SpeedKmh-12.0) > 0.01 {
		t.Errorf("expected speed 12.0, got %.2f", result.SpeedKmh)
	}
}
