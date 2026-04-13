package stride

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Robin831/Hytte/internal/training"
)

// BuildChatSystemPrompt assembles the system prompt that gives Claude coaching
// context for a real-time Stride chat conversation. It includes the current
// plan, athlete profile, evaluations, races, training load, and active notes
// — but NOT the full Marius Bakken generation instructions.
func BuildChatSystemPrompt(
	profile training.UserTrainingProfile,
	plan Plan,
	evaluations []EvaluationRecord,
	races []Race,
	acr *float64,
	acute, chronic float64,
	notes []Note,
) string {
	var b strings.Builder

	// 1. Role and capabilities
	b.WriteString(`You are an expert running coach using the Marius Bakken threshold-dominant model.
You are chatting with your athlete about their current training week.

You can:
- Answer questions about the plan, training load, pacing, and recovery
- Modify the weekly plan when asked (move workouts, swap sessions, adjust paces, add rest days)
- Give injury/fatigue advice grounded in the athlete's actual data

When modifying the plan, output the FULL updated 7-day plan as a fenced JSON block:
` + "```json\n" + `[{"date": "YYYY-MM-DD", "rest_day": false, "session": {...}}, ...]
` + "```\n" + `The JSON must follow the exact DayPlan schema below. Include ALL 7 days, not just the changed ones.
Only output plan JSON when you are actually making a change — not when just discussing.

### DayPlan Schema

Each day object:
- "date": string — "YYYY-MM-DD"
- "rest_day": boolean — true for complete rest (no session needed), false otherwise
- "session": object (required when rest_day is false):
  - "warmup": string — warmup description (empty string if none)
  - "main_set": string — main workout description
  - "cooldown": string — cooldown description (empty string if none)
  - "strides": string — strides description (empty string if none)
  - "target_hr_cap": integer — max HR for this session in bpm (0 if not applicable)
  - "description": string — 1-2 sentence summary of the session purpose
`)

	// 2. Current plan
	b.WriteString("\n## Current Weekly Plan\n\n")
	b.WriteString(fmt.Sprintf("Week: %s to %s | Phase: %s\n\n", plan.WeekStart, plan.WeekEnd, plan.Phase))

	var days []DayPlan
	if err := json.Unmarshal(plan.Plan, &days); err == nil {
		prettyPlan, err := json.MarshalIndent(days, "", "  ")
		if err == nil {
			b.WriteString("```json\n")
			b.Write(prettyPlan)
			b.WriteString("\n```\n")
		}
	} else {
		// Fallback: include the raw plan JSON
		b.WriteString("```json\n")
		b.Write(plan.Plan)
		b.WriteString("\n```\n")
	}

	// 3. Training profile
	if profile.Block != "" {
		b.WriteString("\n## Athlete Profile\n\n")
		b.WriteString(profile.Block)
		b.WriteString("\n")
	}

	// 4. This week's evaluations
	if len(evaluations) > 0 {
		b.WriteString("\n## Completed Sessions This Week\n\n")
		for _, er := range evaluations {
			e := er.Eval
			date := e.Date
			if date == "" {
				date = er.CreatedAt
			}
			line := fmt.Sprintf("- %s: %s — %s", date, e.PlannedType, e.Compliance)
			if e.Notes != "" {
				line += ". " + e.Notes
			}
			b.WriteString(line + "\n")
		}
	}

	// 5. Race calendar
	if len(races) > 0 {
		b.WriteString("\n## Upcoming Races\n\n")
		for _, r := range races {
			line := fmt.Sprintf("- %s: %s, %.0fm, priority %s", r.Date, r.Name, r.DistanceM, r.Priority)
			if r.TargetTime != nil {
				mins := *r.TargetTime / 60
				secs := *r.TargetTime % 60
				line += fmt.Sprintf(", target %d:%02d", mins, secs)
			}
			b.WriteString(line + "\n")
		}
	}

	// 6. Training load context
	b.WriteString("\n## Training Load\n\n")
	if acr != nil {
		b.WriteString(fmt.Sprintf("- ACR (acute:chronic ratio): %.2f\n", *acr))
	}
	b.WriteString(fmt.Sprintf("- Acute load: %.0f\n", acute))
	b.WriteString(fmt.Sprintf("- Chronic load: %.0f\n", chronic))

	// 7. Active notes
	if len(notes) > 0 {
		b.WriteString("\n## Athlete Notes\n\n")
		for _, n := range notes {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", n.TargetDate, n.Content))
		}
	}

	return b.String()
}
