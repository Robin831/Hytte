package training

import (
	"testing"
)

func TestGenerateAutoTags_TooFewLaps(t *testing.T) {
	pw := &ParsedWorkout{Sport: "running", Laps: []ParsedLap{
		{DurationSeconds: 360},
		{DurationSeconds: 60},
	}}
	tags := GenerateAutoTags(pw)
	if tags != nil {
		t.Errorf("expected nil for <3 laps, got %v", tags)
	}
}

func TestGenerateAutoTags_AlternatingWorkRest(t *testing.T) {
	// 6x6min with 1min rest: work, rest, work, rest, ...
	var laps []ParsedLap
	for i := range 6 {
		laps = append(laps, ParsedLap{DurationSeconds: 360, DistanceMeters: 1200})
		if i < 5 {
			laps = append(laps, ParsedLap{DurationSeconds: 60, DistanceMeters: 150})
		}
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:6x6m (r1m)"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_AlternatingWorkRest_Seconds(t *testing.T) {
	// 20x45s with 15s rest.
	var laps []ParsedLap
	for i := range 20 {
		laps = append(laps, ParsedLap{DurationSeconds: 45})
		if i < 19 {
			laps = append(laps, ParsedLap{DurationSeconds: 15})
		}
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:20x45s (r15s)"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_DistanceBased(t *testing.T) {
	// 8x400m with 200m jog rest.
	var laps []ParsedLap
	for i := range 8 {
		laps = append(laps, ParsedLap{DurationSeconds: 90, DistanceMeters: 400})
		if i < 7 {
			laps = append(laps, ParsedLap{DurationSeconds: 120, DistanceMeters: 200})
		}
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:8x400m (r2m)"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_UniformRepeats(t *testing.T) {
	// 5 laps of ~3min each (no distinct rest).
	laps := []ParsedLap{
		{DurationSeconds: 180},
		{DurationSeconds: 185},
		{DurationSeconds: 178},
		{DurationSeconds: 182},
		{DurationSeconds: 176},
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:5x3m"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_UniformRepeats_Distance(t *testing.T) {
	// 8x1km repeats with consistent distance.
	laps := make([]ParsedLap, 8)
	for i := range laps {
		laps[i] = ParsedLap{DurationSeconds: 240, DistanceMeters: 1000}
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:8x1km"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_NoPattern(t *testing.T) {
	// Wildly varying laps — no pattern.
	laps := []ParsedLap{
		{DurationSeconds: 60},
		{DurationSeconds: 300},
		{DurationSeconds: 120},
		{DurationSeconds: 500},
		{DurationSeconds: 30},
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if tags != nil {
		t.Errorf("expected nil for inconsistent laps, got %v", tags)
	}
}

func TestGenerateAutoTags_NonDistanceSport(t *testing.T) {
	// Strength training with uniform laps — should use duration, not distance.
	laps := make([]ParsedLap, 4)
	for i := range laps {
		laps[i] = ParsedLap{DurationSeconds: 30, DistanceMeters: 0}
	}

	pw := &ParsedWorkout{Sport: "strength", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:4x30s"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "0s"},
		{30, "30s"},
		{45, "45s"},
		{60, "1m"},
		{90, "1m30s"},
		{120, "2m"},
		{360, "6m"},
		{375, "6m15s"}, // rounds to 6m15s (375/5=75 exact)
	}
	for _, tt := range tests {
		got := formatDuration(tt.seconds)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestFormatDistance(t *testing.T) {
	tests := []struct {
		meters float64
		want   string
	}{
		{0, ""},
		{200, "200m"},
		{395, "400m"},   // within 5% of 400
		{400, "400m"},
		{800, "800m"},
		{1000, "1km"},
		{1605, "1mi"},   // within 5% of 1609
		{2000, "2km"},
		{750, "750m"},   // not near a common distance
	}
	for _, tt := range tests {
		got := formatDistance(tt.meters)
		if got != tt.want {
			t.Errorf("formatDistance(%v) = %q, want %q", tt.meters, got, tt.want)
		}
	}
}

func TestGenerateAutoTags_SingleLap(t *testing.T) {
	pw := &ParsedWorkout{Sport: "running", Laps: []ParsedLap{
		{DurationSeconds: 1800},
	}}
	tags := GenerateAutoTags(pw)
	if tags != nil {
		t.Errorf("expected nil for single lap, got %v", tags)
	}
}

func TestGenerateAutoTags_ShortRestSkipped(t *testing.T) {
	// Work/rest pattern where rest is <= 5s — rest portion should be omitted from tag.
	var laps []ParsedLap
	for i := range 4 {
		laps = append(laps, ParsedLap{DurationSeconds: 120})
		if i < 3 {
			laps = append(laps, ParsedLap{DurationSeconds: 3})
		}
	}

	pw := &ParsedWorkout{Sport: "running", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:4x2m"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}

func TestGenerateAutoTags_SlightVariation(t *testing.T) {
	// 4x~5min with ~1min rest, slight natural variation.
	laps := []ParsedLap{
		{DurationSeconds: 295},
		{DurationSeconds: 58},
		{DurationSeconds: 305},
		{DurationSeconds: 62},
		{DurationSeconds: 298},
		{DurationSeconds: 60},
		{DurationSeconds: 302},
	}

	pw := &ParsedWorkout{Sport: "cycling", Laps: laps}
	tags := GenerateAutoTags(pw)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", tags)
	}
	expected := "auto:4x5m (r1m)"
	if tags[0] != expected {
		t.Errorf("expected %q, got %q", expected, tags[0])
	}
}
