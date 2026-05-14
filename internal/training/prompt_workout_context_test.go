package training

import (
	"strings"
	"testing"
)

func TestFormatWorkoutContextNote_PostWorkoutFraming(t *testing.T) {
	ctx := &WorkoutContext{
		Surface:   "Treadmill",
		RunType:   "long",
		HRSource:  "chest",
		FeelNotes: "Legs heavy in last 10 min",
		SpeedPlan: []SpeedSegment{
			{Kind: "steady", SpeedKmph: 10.0, DurationSec: 1800, Repeats: 1},
		},
	}

	got := FormatWorkoutContextNote(ctx)

	if !strings.Contains(got, "Runner's post-workout report —") {
		t.Errorf("expected post-workout framing prefix, got %q", got)
	}
	if !strings.Contains(got, "Executed splits:") {
		t.Errorf("expected 'Executed splits:' prefix for speed plan, got %q", got)
	}
	if strings.Contains(got, "Plan:") {
		t.Errorf("output must not contain the 'Plan:' prefix (would mislead the evaluator), got %q", got)
	}
	if !strings.Contains(got, "Feel notes: Legs heavy") {
		t.Errorf("expected feel notes to be preserved, got %q", got)
	}
	if !strings.Contains(got, "surface=Treadmill") {
		t.Errorf("expected surface metadata, got %q", got)
	}
	if !strings.Contains(got, "run_type=long") {
		t.Errorf("expected run_type metadata, got %q", got)
	}
}

func TestFormatWorkoutContextNote_OnlySpeedPlan(t *testing.T) {
	ctx := &WorkoutContext{
		SpeedPlan: []SpeedSegment{
			{Kind: "work", SpeedKmph: 14.0, DurationSec: 300, Repeats: 4},
		},
	}

	got := FormatWorkoutContextNote(ctx)

	if !strings.Contains(got, "Runner's post-workout report —") {
		t.Errorf("expected post-workout framing prefix when only speed plan is set, got %q", got)
	}
	if !strings.Contains(got, "Executed splits:") {
		t.Errorf("expected 'Executed splits:' prefix, got %q", got)
	}
	if strings.Contains(got, "Plan:") {
		t.Errorf("output must not contain the 'Plan:' prefix, got %q", got)
	}
}

func TestFormatWorkoutContextNote_OnlyFeelNotes(t *testing.T) {
	ctx := &WorkoutContext{FeelNotes: "Great session"}
	got := FormatWorkoutContextNote(ctx)
	if !strings.Contains(got, "Runner's post-workout report —") {
		t.Errorf("expected post-workout framing prefix, got %q", got)
	}
	if !strings.Contains(got, "Feel notes: Great session") {
		t.Errorf("expected feel notes content, got %q", got)
	}
}

func TestFormatWorkoutContextNote_NilReturnsEmpty(t *testing.T) {
	if got := FormatWorkoutContextNote(nil); got != "" {
		t.Errorf("expected empty string for nil context, got %q", got)
	}
}

func TestFormatWorkoutContextNote_EmptyReturnsEmpty(t *testing.T) {
	ctx := &WorkoutContext{}
	if got := FormatWorkoutContextNote(ctx); got != "" {
		t.Errorf("expected empty string when no fields populated, got %q", got)
	}
}
