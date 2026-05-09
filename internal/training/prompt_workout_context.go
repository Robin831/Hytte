package training

import (
	"fmt"
	"strings"
)

// writeWorkoutContextBlock injects user-entered workout context (surface, run
// type, HR data source, free-text feel notes) into a prompt builder. It is a
// no-op when the context is empty across all surfaced fields.
func writeWorkoutContextBlock(sb *strings.Builder, ctx *WorkoutContext) {
	if ctx == nil {
		return
	}
	hasField := ctx.Surface != "" || ctx.RunType != "" || ctx.HRSource != "" || strings.TrimSpace(ctx.FeelNotes) != ""
	if !hasField {
		return
	}
	sb.WriteString("Workout Context (user-reported):\n")
	if ctx.Surface != "" {
		fmt.Fprintf(sb, "- Surface: %s\n", ctx.Surface)
	}
	if ctx.RunType != "" {
		fmt.Fprintf(sb, "- Run type: %s\n", ctx.RunType)
	}
	if ctx.HRSource != "" {
		fmt.Fprintf(sb, "- HR source: %s\n", ctx.HRSource)
	}
	if notes := strings.TrimSpace(ctx.FeelNotes); notes != "" {
		fmt.Fprintf(sb, "- Feel notes: %s\n", notes)
	}
	sb.WriteString("\n")
}

// resolvedSegment is one expanded entry from a SpeedSegment plan. Repeats and
// SameAsPrevious flags are unrolled so the prompt — and any per-lap override —
// can index a flat list.
type resolvedSegment struct {
	Kind        string
	SpeedKmph   float64
	DurationSec int
}

// workoutCtxOrNil safely extracts the speed plan from a possibly-nil context.
func workoutCtxOrNil(ctx *WorkoutContext) []SpeedSegment {
	if ctx == nil {
		return nil
	}
	return ctx.SpeedPlan
}

// resolveSpeedPlan expands a SpeedSegment slice into a flat list of repeats.
// SameAsPrevious copies the previous resolved segment's speed/kind when fields
// on the current segment are zero — letting the UI compress repeated blocks
// without losing data for the prompt builder. Repeats < 1 are treated as 1.
func resolveSpeedPlan(plan []SpeedSegment) []resolvedSegment {
	if len(plan) == 0 {
		return nil
	}
	out := make([]resolvedSegment, 0, len(plan))
	var prev *resolvedSegment
	for _, seg := range plan {
		base := resolvedSegment{
			Kind:        seg.Kind,
			SpeedKmph:   seg.SpeedKmph,
			DurationSec: seg.DurationSec,
		}
		if seg.SameAsPrevious && prev != nil {
			if base.Kind == "" {
				base.Kind = prev.Kind
			}
			if base.SpeedKmph == 0 {
				base.SpeedKmph = prev.SpeedKmph
			}
			if base.DurationSec == 0 {
				base.DurationSec = prev.DurationSec
			}
		}
		repeats := seg.Repeats
		if repeats < 1 {
			repeats = 1
		}
		for i := 0; i < repeats; i++ {
			out = append(out, base)
		}
		// Track the most-recent resolved segment so chained SameAsPrevious
		// segments inherit from the prior reps rather than the original entry.
		copyOfBase := base
		prev = &copyOfBase
	}
	return out
}

// writeResolvedSpeedPlan formats the resolved plan as a markdown table for prompt injection.
func writeResolvedSpeedPlan(sb *strings.Builder, plan []resolvedSegment) {
	if len(plan) == 0 {
		return
	}
	sb.WriteString("\nPlanned Speed Structure (treadmill — overrides device pace):\n")
	sb.WriteString("| # | Kind | Speed | Pace/km | Duration |\n")
	sb.WriteString("|---|------|-------|---------|----------|\n")
	for i, seg := range plan {
		paceSec := paceFromSpeedKmph(seg.SpeedKmph)
		fmt.Fprintf(sb, "| %d | %s | %.2f km/h | %s | %s |\n",
			i+1,
			seg.Kind,
			seg.SpeedKmph,
			formatPromptPace(paceSec),
			formatPromptDuration(seg.DurationSec),
		)
	}
}

// lookupResolvedPace returns the planned pace (sec/km) for a 1-based lap index
// into the resolved plan, or 0 when out of range or speed is zero.
func lookupResolvedPace(plan []resolvedSegment, lapIndex int) float64 {
	if lapIndex < 0 || lapIndex >= len(plan) {
		return 0
	}
	return paceFromSpeedKmph(plan[lapIndex].SpeedKmph)
}

// paceFromSpeedKmph converts km/h to sec/km. Returns 0 for non-positive speed.
func paceFromSpeedKmph(speedKmph float64) float64 {
	if speedKmph <= 0 {
		return 0
	}
	return 3600.0 / speedKmph
}

// isTreadmillSurface returns true when the user-reported surface indicates a
// treadmill — case-insensitive match for "treadmill" or "indoor". Matching
// loosely lets the UI surface alternative labels (e.g. "indoor_track") while
// still triggering the device-pace override.
func isTreadmillSurface(surface string) bool {
	s := strings.ToLower(strings.TrimSpace(surface))
	if s == "" {
		return false
	}
	return s == "treadmill" || strings.Contains(s, "treadmill") || strings.Contains(s, "indoor")
}

// FormatWorkoutContextNote renders the user-reported context as a single
// stride note string (feel notes + structured plan summary). Returns empty
// when there is nothing useful to surface. Used by the nightly stride
// evaluation to fold per-workout context into the notes that go to Claude.
func FormatWorkoutContextNote(ctx *WorkoutContext) string {
	if ctx == nil {
		return ""
	}
	var parts []string
	if notes := strings.TrimSpace(ctx.FeelNotes); notes != "" {
		parts = append(parts, "Feel notes: "+notes)
	}
	if summary := summarizeSpeedPlan(ctx.SpeedPlan); summary != "" {
		parts = append(parts, "Plan: "+summary)
	}
	if ctx.Surface != "" || ctx.RunType != "" || ctx.HRSource != "" {
		var meta []string
		if ctx.Surface != "" {
			meta = append(meta, "surface="+ctx.Surface)
		}
		if ctx.RunType != "" {
			meta = append(meta, "run_type="+ctx.RunType)
		}
		if ctx.HRSource != "" {
			meta = append(meta, "hr_source="+ctx.HRSource)
		}
		if len(meta) > 0 {
			parts = append(parts, "Context: "+strings.Join(meta, ", "))
		}
	}
	return strings.Join(parts, " | ")
}

// summarizeSpeedPlan condenses a SpeedSegment plan into a one-line summary,
// keeping the original repeats/same_as_previous semantics so the summary
// matches what the user entered.
func summarizeSpeedPlan(plan []SpeedSegment) string {
	if len(plan) == 0 {
		return ""
	}
	parts := make([]string, 0, len(plan))
	var prev *SpeedSegment
	for i := range plan {
		seg := plan[i]
		effective := seg
		if seg.SameAsPrevious && prev != nil {
			if effective.Kind == "" {
				effective.Kind = prev.Kind
			}
			if effective.SpeedKmph == 0 {
				effective.SpeedKmph = prev.SpeedKmph
			}
			if effective.DurationSec == 0 {
				effective.DurationSec = prev.DurationSec
			}
		}
		repeats := effective.Repeats
		if repeats < 1 {
			repeats = 1
		}
		repeatPrefix := ""
		if repeats > 1 {
			repeatPrefix = fmt.Sprintf("%dx ", repeats)
		}
		parts = append(parts, fmt.Sprintf("%s%s @ %.1fkm/h for %s",
			repeatPrefix,
			effective.Kind,
			effective.SpeedKmph,
			formatPromptDuration(effective.DurationSec),
		))
		copyOfEffective := effective
		prev = &copyOfEffective
	}
	return strings.Join(parts, "; ")
}
