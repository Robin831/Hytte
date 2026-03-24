package lactate

import (
	"testing"
)

// ---- ParseLactateInput tests ----

func TestParseLactateInput_DotDecimalSpaceSeparated(t *testing.T) {
	pairs, err := ParseLactateInput("10.5 2.3\n11.0 2.8\n11.5 3.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
	assertPair(t, pairs[2], 11.5, 3.5)
}

func TestParseLactateInput_NorwegianCommaSpaceSeparated(t *testing.T) {
	pairs, err := ParseLactateInput("10,5 2,3\n11,0 2,8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
}

func TestParseLactateInput_SlashSeparated(t *testing.T) {
	pairs, err := ParseLactateInput("10.5/2.3\n11.0/2.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
}

func TestParseLactateInput_NorwegianCommaSlashSeparated(t *testing.T) {
	pairs, err := ParseLactateInput("10,5/2,3\n11,0/2,8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
}

func TestParseLactateInput_FourTokenNorwegianComma(t *testing.T) {
	// "10,5,2,3" is Norwegian format: speed=10.5, lactate=2.3
	pairs, err := ParseLactateInput("10,5,2,3\n11,0,2,8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
}

func TestParseLactateInput_CommaSeparatedDotDecimal(t *testing.T) {
	pairs, err := ParseLactateInput("10.5,2.3\n11.0,2.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPair(t, pairs[0], 10.5, 2.3)
	assertPair(t, pairs[1], 11.0, 2.8)
}

func TestParseLactateInput_BlankLinesIgnored(t *testing.T) {
	pairs, err := ParseLactateInput("\n10.5 2.3\n\n11.0 2.8\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
}

func TestParseLactateInput_TooFewPairs(t *testing.T) {
	_, err := ParseLactateInput("10.5 2.3")
	if err == nil {
		t.Fatal("expected error for single pair, got nil")
	}
}

func TestParseLactateInput_NonIncreasingSpeed(t *testing.T) {
	_, err := ParseLactateInput("11.0 2.3\n10.5 2.8")
	if err == nil {
		t.Fatal("expected error for non-increasing speeds, got nil")
	}
}

func TestParseLactateInput_EqualSpeeds(t *testing.T) {
	_, err := ParseLactateInput("10.5 2.3\n10.5 2.8")
	if err == nil {
		t.Fatal("expected error for equal speeds, got nil")
	}
}

func TestParseLactateInput_SpeedTooLow(t *testing.T) {
	_, err := ParseLactateInput("4.0 2.3\n5.0 2.8")
	if err == nil {
		t.Fatal("expected error for speed below 5 km/h, got nil")
	}
}

func TestParseLactateInput_SpeedTooHigh(t *testing.T) {
	_, err := ParseLactateInput("24.0 2.3\n26.0 2.8")
	if err == nil {
		t.Fatal("expected error for speed above 25 km/h, got nil")
	}
}

func TestParseLactateInput_NegativeLactate(t *testing.T) {
	_, err := ParseLactateInput("10.5 -1.0\n11.0 2.8")
	if err == nil {
		t.Fatal("expected error for negative lactate, got nil")
	}
}

func TestParseLactateInput_ZeroLactate(t *testing.T) {
	_, err := ParseLactateInput("10.5 0\n11.0 2.8")
	if err == nil {
		t.Fatal("expected error for zero lactate, got nil")
	}
}

// ---- ExtractStageHR tests ----

// makeImportLap creates an ImportLap for testing.
func makeImportLap(lapNum int, startOffsetMs int64, durationSeconds float64, avgPaceSecPerKm float64) ImportLap {
	return ImportLap{
		LapNumber:       lapNum,
		StartOffsetMs:   startOffsetMs,
		DurationSeconds: durationSeconds,
		AvgPaceSecPerKm: avgPaceSecPerKm,
	}
}

// makeImportSamples creates a simple HR time-series with constant HR per lap.
func makeImportSamples(laps []ImportLap, hrPerLap []int) []ImportSample {
	var samples []ImportSample
	for i, lap := range laps {
		hr := 0
		if i < len(hrPerLap) {
			hr = hrPerLap[i]
		}
		// Emit one sample per second across the lap.
		for s := 0; s < int(lap.DurationSeconds); s++ {
			samples = append(samples, ImportSample{
				OffsetMs:  lap.StartOffsetMs + int64(s)*1000,
				HeartRate: hr,
			})
		}
	}
	return samples
}

func TestExtractStageHR_SpeedMatch(t *testing.T) {
	// Lap 1: warmup ~10 min at 6 km/h (pace=600 s/km)
	// Lap 2: stage 1 at 10 km/h (pace=360 s/km)
	// Lap 3: stage 2 at ~11 km/h (pace=327.3 s/km → 3600/327.3 ≈ 11 km/h)
	laps := []ImportLap{
		makeImportLap(1, 0, 600, 600),       // warmup 6 km/h
		makeImportLap(2, 600000, 300, 360),  // stage 10 km/h
		makeImportLap(3, 900000, 300, 327.3), // stage ~11 km/h
	}

	hrPerLap := []int{120, 145, 158}
	samples := makeImportSamples(laps, hrPerLap)

	pairs := []SpeedLactatePair{
		{SpeedKmh: 10.0, LactateMmol: 2.1},
		{SpeedKmh: 11.0, LactateMmol: 2.8},
	}

	opts := ImportOptions{HRWindowSeconds: 30}
	result, err := ExtractStageHR(laps, samples, pairs, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Method != "speed" {
		t.Errorf("expected method=speed, got %q", result.Method)
	}
	if len(result.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(result.Stages))
	}
	if result.Stages[0].HeartRateBpm != 145 {
		t.Errorf("stage 1 HR: expected 145, got %d", result.Stages[0].HeartRateBpm)
	}
	if result.Stages[1].HeartRateBpm != 158 {
		t.Errorf("stage 2 HR: expected 158, got %d", result.Stages[1].HeartRateBpm)
	}
}

func TestExtractStageHR_DurationFallback(t *testing.T) {
	// All laps have pace=0 so speed matching will produce no matches.
	laps := []ImportLap{
		makeImportLap(1, 0, 600, 0),         // warmup 10 min
		makeImportLap(2, 600000, 300, 0),    // stage 1
		makeImportLap(3, 900000, 300, 0),    // stage 2
	}

	hrPerLap := []int{120, 140, 155}
	samples := makeImportSamples(laps, hrPerLap)

	pairs := []SpeedLactatePair{
		{SpeedKmh: 10.0, LactateMmol: 2.1},
		{SpeedKmh: 11.0, LactateMmol: 2.8},
	}

	opts := ImportOptions{
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		HRWindowSeconds:   30,
	}
	result, err := ExtractStageHR(laps, samples, pairs, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Method != "duration" {
		t.Errorf("expected method=duration, got %q", result.Method)
	}
	if len(result.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(result.Stages))
	}
	// Stages should map to laps 2 and 3 (after warmup skip).
	if result.Stages[0].LapNumber != 2 {
		t.Errorf("stage 1 lap: expected 2, got %d", result.Stages[0].LapNumber)
	}
	if result.Stages[1].LapNumber != 3 {
		t.Errorf("stage 2 lap: expected 3, got %d", result.Stages[1].LapNumber)
	}
}

func TestExtractStageHR_SpeedToleranceEnforced(t *testing.T) {
	// Lap speed is 10.8 km/h (pace=333.3 s/km → 3600/333.3 ≈ 10.8 km/h).
	// Pair speed is 10.0 km/h → diff ≈ 0.8 km/h > 0.4 km/h tolerance.
	// Speed match must NOT succeed; the result should fall back to duration.
	laps := []ImportLap{
		makeImportLap(1, 0, 300, 333.3), // ~10.8 km/h — too far from 10.0 km/h
		makeImportLap(2, 300000, 300, 327.3), // ~11.0 km/h — matches pair 2
	}
	samples := makeImportSamples(laps, []int{140, 155})

	pairs := []SpeedLactatePair{
		{SpeedKmh: 10.0, LactateMmol: 2.1},
		{SpeedKmh: 11.0, LactateMmol: 2.8},
	}

	opts := ImportOptions{HRWindowSeconds: 30}
	result, err := ExtractStageHR(laps, samples, pairs, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Speed matching finds only 1 out of 2 pairs → falls back to duration method.
	if result.Method != "duration" {
		t.Errorf("expected method=duration (speed tolerance not met for pair 1), got %q", result.Method)
	}
}

func TestExtractStageHR_StageDurationMinFiltersShortLaps(t *testing.T) {
	// After warmup, lap 2 is a short auto-lap (30 s < StageDurationMin=5 min),
	// and laps 3 and 4 are real 5-min stages. Only laps 3 and 4 should be used.
	laps := []ImportLap{
		makeImportLap(1, 0, 600, 0),          // warmup 10 min
		makeImportLap(2, 600000, 30, 0),      // short auto-lap (30 s) — must be skipped
		makeImportLap(3, 630000, 300, 0),     // stage 1 (5 min)
		makeImportLap(4, 930000, 300, 0),     // stage 2 (5 min)
	}
	samples := makeImportSamples(laps, []int{120, 130, 145, 158})

	pairs := []SpeedLactatePair{
		{SpeedKmh: 10.0, LactateMmol: 2.1},
		{SpeedKmh: 11.0, LactateMmol: 2.8},
	}

	opts := ImportOptions{
		WarmupDurationMin: 10,
		StageDurationMin:  5, // 5-min minimum — lap 2 (30 s) must be excluded
		HRWindowSeconds:   30,
	}
	result, err := ExtractStageHR(laps, samples, pairs, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Method != "duration" {
		t.Errorf("expected method=duration, got %q", result.Method)
	}
	if len(result.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(result.Stages))
	}
	// Stages must map to laps 3 and 4, not laps 2 and 3.
	if result.Stages[0].LapNumber != 3 {
		t.Errorf("stage 1 lap: expected 3, got %d (short lap was not filtered)", result.Stages[0].LapNumber)
	}
	if result.Stages[1].LapNumber != 4 {
		t.Errorf("stage 2 lap: expected 4, got %d", result.Stages[1].LapNumber)
	}
}

func TestExtractStageHR_NoLaps(t *testing.T) {
	_, err := ExtractStageHR(
		nil,
		[]ImportSample{{OffsetMs: 0, HeartRate: 150}},
		[]SpeedLactatePair{{10, 2.0}, {11, 2.5}},
		ImportOptions{},
	)
	if err == nil {
		t.Fatal("expected error for empty laps, got nil")
	}
}

func TestExtractStageHR_NoSamples(t *testing.T) {
	laps := []ImportLap{
		makeImportLap(1, 0, 300, 360),
		makeImportLap(2, 300000, 300, 327),
	}
	_, err := ExtractStageHR(laps, nil, []SpeedLactatePair{{10, 2.0}, {11, 2.5}}, ImportOptions{})
	if err == nil {
		t.Fatal("expected error for empty samples, got nil")
	}
}

func TestExtractStageHR_HRWindowDefaultsTo30(t *testing.T) {
	// opts.HRWindowSeconds = 0 should default to 30.
	laps := []ImportLap{
		makeImportLap(1, 0, 300, 360),
		makeImportLap(2, 300000, 300, 327.3),
	}
	samples := makeImportSamples(laps, []int{145, 158})

	pairs := []SpeedLactatePair{
		{SpeedKmh: 10.0, LactateMmol: 2.1},
		{SpeedKmh: 11.0, LactateMmol: 2.8},
	}

	result, err := ExtractStageHR(laps, samples, pairs, ImportOptions{HRWindowSeconds: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(result.Stages))
	}
}

// assertPair checks that a SpeedLactatePair matches the expected values within a small tolerance.
func assertPair(t *testing.T, p SpeedLactatePair, wantSpeed, wantLactate float64) {
	t.Helper()
	const eps = 1e-9
	if diff := p.SpeedKmh - wantSpeed; diff < -eps || diff > eps {
		t.Errorf("speed: expected %.4f, got %.4f", wantSpeed, p.SpeedKmh)
	}
	if diff := p.LactateMmol - wantLactate; diff < -eps || diff > eps {
		t.Errorf("lactate: expected %.4f, got %.4f", wantLactate, p.LactateMmol)
	}
}
