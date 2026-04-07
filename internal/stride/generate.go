package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// mariusBakkenInstructions contains the Marius Bakken threshold-dominant model
// coaching instructions injected verbatim into every plan generation prompt.
const mariusBakkenInstructions = `You are an expert running coach applying the Marius Bakken threshold-dominant training model, adapted for recreational runners doing 3-5 sessions per week.

## Marius Bakken Training Model (Recreational Adaptation)

### Core Philosophy
- Threshold work is the dominant training stimulus for recreational runners (3-5 sessions/week).
- This is NOT 80/20 polarized training — that model is for elite athletes doing 10+ sessions/week.
- Easy/recovery runs use Zone 1 ONLY (below ~70% max HR). Not Zone 2 — true easy running.
- VO2max-intensity work (Zone 5) is used sparingly: ONE hard session per week or every other week.

### Critical HR Rules
- **Threshold sessions**: target BELOW the user's threshold HR. If threshold HR is 166, target 158-165.
  NEVER on or above threshold HR. These should feel controlled and sustainable.
- **Easy/recovery runs**: HR must stay in Zone 1 (below the user's Zone 1 ceiling).
  If Zone 1 ceiling is 138, all easy running must stay below 138. True recovery pace.
- **Long runs**: Zone 1 for the majority. May include a progressive threshold finish in the last 20-30%.
- **Hard sessions (above threshold)**: ONLY the one designated hard session per 1-2 weeks.

### Weekly Structure (3-5 sessions)
**3 sessions/week**: 1 threshold, 1 easy, 1 long run (with optional threshold finish)
**4 sessions/week**: 1-2 threshold, 1 easy, 1 long run. Every other week add the hard session replacing one easy.
**5 sessions/week**: 2 threshold, 1 easy, 1 long run, 1 hard (or easy if not a hard week).
- Long run day: Sunday (default, respect user preference).
- Rest days between hard efforts.

### Threshold Pace Definition
- Threshold pace = the pace you can sustain for approximately 60 minutes in a race.
- Corresponds to lactate threshold (approximately 4 mmol/L blood lactate).
- HR target: BELOW the user's threshold HR from their profile. If threshold HR is 166, target 158-165.
- Use the user's threshold pace from their profile as the reference.

### Session Templates
**Threshold Intervals (standard)**:
- Warmup: 15-20 min Zone 1 jog + 4x100m strides
- Main set: 6x6min at BELOW threshold pace/HR, 2min recovery jog
- Cooldown: 10-15 min Zone 1 jog
- Alternative formats: 6-8x1000m, 3-4x3000m, 2x4000m — always below threshold HR

**Hard Session (above threshold, max 1 per 1-2 weeks)**:
- Examples: 30-45s hard + 15s rest x 20-40 reps, or hill intervals
- The ONLY session where HR goes above threshold
- Skip if legs feel heavy from recent threshold work

**Easy Recovery**:
- 45-60 min at Zone 1 ONLY. HR must stay below Zone 1 ceiling.
- Optional: 4-6x20s strides at the end for neuromuscular activation

**Long Run**:
- 75-120 min starting at Zone 1 easy pace
- Optional progressive finish: last 20-30% at threshold effort (below threshold HR)

### Strides
- 4-6x20s at ~4:00/km pace (fast but relaxed), full recovery jog between
- Used after easy runs only, never after threshold sessions

### Load Management
- Increase weekly distance by no more than 10% per week
- After 3 weeks of build, include 1 deload week (60-70% of peak volume)
- If ACR ratio > 1.3, reduce intensity and/or volume for the coming week
- If ACR ratio < 0.8, athlete may be undertraining — can increase load

### Race Preparation
- Within 3 weeks of an A-race: shift to race-specific intervals, reduce volume 20-30%
- Taper: final 2 weeks reduce volume by 40-50%, maintain some intensity
- B/C-races: no taper, treat as quality training session

## Output Format
Return ONLY a JSON array of day objects for the requested week. No markdown, no explanation, no code fences.

Each day object must have:
- "date": "YYYY-MM-DD" (the calendar date)
- "rest_day": true (for complete rest, no session object needed)
OR
- "rest_day": false and "session": { ... }

Each session object must have:
- "warmup": string (warmup description, empty string if none)
- "main_set": string (main workout description)
- "cooldown": string (cooldown description, empty string if none)
- "strides": string (strides description, empty string if none)
- "target_hr_cap": integer (max HR for this session in bpm, 0 if not applicable)
- "description": string (1-2 sentence summary of the session purpose)

Example output structure:
[
  {"date":"2026-04-06","rest_day":false,"session":{"warmup":"15 min easy jog + 4x100m strides","main_set":"6x1000m at threshold pace, 60s recovery jog","cooldown":"10 min easy jog","strides":"","target_hr_cap":165,"description":"Threshold intervals to develop lactate threshold fitness. Core Marius Bakken session."}},
  {"date":"2026-04-07","rest_day":true}
]
`

// DayPlan represents a single day in a generated weekly training plan.
type DayPlan struct {
	Date    string   `json:"date"`
	RestDay bool     `json:"rest_day"`
	Session *Session `json:"session,omitempty"`
}

// Session holds the structured components of a training session.
type Session struct {
	Warmup      string `json:"warmup"`
	MainSet     string `json:"main_set"`
	Cooldown    string `json:"cooldown"`
	Strides     string `json:"strides"`
	TargetHRCap int    `json:"target_hr_cap"`
	Description string `json:"description"`
}

// runPromptFunc is the function used to call Claude. Override in tests.
var runPromptFunc = func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
	return training.RunPrompt(ctx, cfg, prompt)
}

// currentWeek returns the ISO date strings for the current week's Monday (week_start)
// and the following Sunday (week_end). If today is Monday, returns today.
func currentWeek() (weekStart, weekEnd string) {
	today := time.Now().UTC()
	weekday := int(today.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6

	daysBack := (weekday - 1 + 7) % 7 // Monday=0, Tuesday=1, ..., Sunday=6
	monday := today.AddDate(0, 0, -daysBack)
	sunday := monday.AddDate(0, 0, 6)

	const dateFmt = "2006-01-02"
	return monday.Format(dateFmt), sunday.Format(dateFmt)
}

// GeneratePlan generates a weekly training plan for the given user using Claude AI.
// It queries training context from the DB, builds a prompt with Marius Bakken
// threshold-dominant model instructions, calls Claude, and stores the result in
// stride_plans. Returns nil if stride is not enabled for the user.
// weekMode controls the target week: "current" for the current week, "next" (default)
// for the upcoming week.
func GeneratePlan(ctx context.Context, db *sql.DB, userID int64, weekMode string) error {
	// Load user preferences.
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}

	// Stride must be explicitly enabled.
	if prefs["stride_enabled"] != "true" {
		return nil
	}

	// Load Claude config (picks up claude_model and claude_enabled preferences).
	claudeCfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil {
		return fmt.Errorf("load Claude config: %w", err)
	}
	if !claudeCfg.Enabled {
		return training.ErrClaudeNotEnabled
	}

	// Override model to claude-opus if unset or if user explicitly chose opus.
	if claudeCfg.Model == "" {
		claudeCfg.Model = "claude-opus-4-6"
	}

	// Determine the week to plan.
	var weekStart, weekEnd string
	if weekMode == "current" {
		weekStart, weekEnd = currentWeek()
	} else {
		weekStart, weekEnd = upcomingWeek()
	}

	// Query stride races — filter to upcoming, unfinished races only.
	allRaces, err := ListRaces(db, userID)
	if err != nil {
		return fmt.Errorf("list races: %w", err)
	}
	var races []Race
	today := time.Now().UTC().Format("2006-01-02")
	for _, r := range allRaces {
		if r.Date >= today && r.ResultTime == nil {
			races = append(races, r)
		}
	}

	// Query stride notes (all, most recent first).
	notes, err := listAllNotes(ctx, db, userID)
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}

	// Read optional custom prompt appended to the plan generation request.
	// Decrypt it because the preference is stored encrypted at rest.
	customPrompt := prefs["stride_custom_prompt"]
	if customPrompt != "" {
		if dec, err := encryption.DecryptField(customPrompt); err != nil {
			log.Printf("stride: failed to decrypt stride_custom_prompt, skipping: %v", err)
			customPrompt = ""
		} else {
			customPrompt = dec
		}
	}

	// User training constraints.
	availableDays := prefs["stride_available_days"] // e.g. "5" or comma-separated list
	weeklyDistanceCap := prefs["stride_weekly_distance_cap"] // km, e.g. "70"

	// Compute current ACR to inform load recommendations.
	acr, acute, chronic, acrErr := training.ComputeACR(db, userID, time.Now().UTC())
	if acrErr != nil {
		// Non-fatal: log and proceed without ACR data.
		log.Printf("stride: compute ACR for user %d: %v", userID, acrErr)
		acr = nil
	}

	// Load last 8 weekly summaries for volume context.
	allSummaries, err := training.WeeklySummaries(db, userID)
	if err != nil {
		return fmt.Errorf("load weekly summaries: %w", err)
	}
	recentSummaries := allSummaries
	if len(recentSummaries) > 8 {
		recentSummaries = recentSummaries[:8]
	}

	// Load the previous week's plan if one exists.
	prevPlanJSON, prevPlanModel, prevPlanCreatedAt, err := loadPreviousPlan(ctx, db, userID, weekStart)
	if err != nil {
		// Non-fatal: log and continue without previous plan context.
		log.Printf("stride: load previous plan for user %d: %v", userID, err)
		prevPlanJSON = ""
	}

	// Build the user training profile block.
	profileBlock := training.BuildUserProfileBlock(db, userID)

	// Assemble the full prompt.
	prompt := buildGeneratePrompt(
		weekStart, weekEnd,
		profileBlock,
		races, notes,
		acr, acute, chronic,
		recentSummaries,
		prevPlanJSON, prevPlanModel, prevPlanCreatedAt,
		availableDays, weeklyDistanceCap,
		customPrompt,
	)

	// Call Claude.
	response, err := runPromptFunc(ctx, claudeCfg, prompt)
	if err != nil {
		return fmt.Errorf("Claude prompt: %w", err)
	}

	// Parse the JSON response into the plan schema.
	plan, err := parsePlanResponse(response, weekStart, weekEnd)
	if err != nil {
		return fmt.Errorf("parse plan response: %w", err)
	}

	// Re-marshal the validated plan to canonical JSON for storage.
	planBytes, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	// Encrypt sensitive fields before DB storage.
	encPrompt, err := encryption.EncryptField(prompt)
	if err != nil {
		return fmt.Errorf("encrypt prompt: %w", err)
	}
	encResponse, err := encryption.EncryptField(response)
	if err != nil {
		return fmt.Errorf("encrypt response: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Upsert into stride_plans (unique on user_id + week_start).
	_, err = db.ExecContext(ctx, `
		INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, prompt, response, model, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, week_start) DO UPDATE SET
			week_end   = excluded.week_end,
			plan_json  = excluded.plan_json,
			prompt     = excluded.prompt,
			response   = excluded.response,
			model      = excluded.model,
			created_at = excluded.created_at
	`, userID, weekStart, weekEnd, "", string(planBytes), encPrompt, encResponse, claudeCfg.Model, now)
	if err != nil {
		return fmt.Errorf("insert stride plan: %w", err)
	}

	return nil
}

// upcomingWeek returns the ISO date strings for the next Monday (week_start)
// and the following Sunday (week_end). If today is Monday, returns today.
func upcomingWeek() (weekStart, weekEnd string) {
	today := time.Now().UTC()
	weekday := int(today.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6

	var daysUntilMonday int
	if weekday == 0 {
		daysUntilMonday = 1 // Sunday → next day is Monday
	} else if weekday == 1 {
		daysUntilMonday = 0 // today is Monday
	} else {
		daysUntilMonday = 8 - weekday // Tuesday..Saturday → next Monday
	}

	monday := today.AddDate(0, 0, daysUntilMonday)
	sunday := monday.AddDate(0, 0, 6)

	const dateFmt = "2006-01-02"
	return monday.Format(dateFmt), sunday.Format(dateFmt)
}

// listAllNotes returns all stride notes for a user, most recent first, limit 20.
func listAllNotes(ctx context.Context, db *sql.DB, userID int64) ([]Note, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, plan_id, content, created_at
		FROM stride_notes
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT 20
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.CreatedAt); err != nil {
			return nil, err
		}
		if n.Content, err = encryption.DecryptField(n.Content); err != nil {
			return nil, fmt.Errorf("decrypt note content: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// loadPreviousPlan returns the plan_json, model, and created_at of the most
// recent stride plan before the given week_start. Returns empty strings if none.
func loadPreviousPlan(ctx context.Context, db *sql.DB, userID int64, weekStart string) (planJSON, model, createdAt string, err error) {
	row := db.QueryRowContext(ctx, `
		SELECT plan_json, model, created_at
		FROM stride_plans
		WHERE user_id = ? AND week_start < ?
		ORDER BY week_start DESC
		LIMIT 1
	`, userID, weekStart)

	err = row.Scan(&planJSON, &model, &createdAt)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return planJSON, model, createdAt, err
}

// buildGeneratePrompt assembles the full prompt string for Claude plan generation.
func buildGeneratePrompt(
	weekStart, weekEnd string,
	profileBlock string,
	races []Race,
	notes []Note,
	acr *float64, acute, chronic float64,
	summaries []training.WeeklySummary,
	prevPlanJSON, prevPlanModel, prevPlanCreatedAt string,
	availableDays, weeklyDistanceCap string,
	customPrompt string,
) string {
	var sb strings.Builder

	sb.WriteString(mariusBakkenInstructions)
	sb.WriteString("\n\n")

	// Target week.
	fmt.Fprintf(&sb, "## Plan Request\nGenerate a 7-day training plan for the week of %s to %s.\n\n", weekStart, weekEnd)

	// User training constraints.
	sb.WriteString("## User Constraints\n")
	if availableDays != "" {
		fmt.Fprintf(&sb, "- Training days per week: %s\n", availableDays)
	} else {
		sb.WriteString("- Training days per week: 5 (default)\n")
	}
	if weeklyDistanceCap != "" {
		fmt.Fprintf(&sb, "- Weekly distance cap: %s km\n", weeklyDistanceCap)
	}
	sb.WriteString("\n")

	// User profile (HR zones, threshold, goal race, etc.).
	if profileBlock != "" {
		sb.WriteString("## User Profile\n")
		sb.WriteString(profileBlock)
		sb.WriteString("\n")
	}

	// ACR / training load status.
	sb.WriteString("## Current Training Load (ACR)\n")
	if acr != nil {
		ratio := *acr
		var status string
		switch {
		case ratio > 1.5:
			status = "HIGH INJURY RISK — acute load far exceeds chronic baseline. Reduce volume and intensity."
		case ratio > 1.3:
			status = "Elevated — above the optimal 0.8–1.3 window. Ease off slightly."
		case ratio < 0.8:
			status = "Low — below chronic baseline. Athlete may be undertraining."
		default:
			status = "Optimal (0.8–1.3 window)."
		}
		fmt.Fprintf(&sb, "- ACR: %.2f (acute=%.1f, chronic=%.1f) — %s\n", ratio, acute, chronic, status)
	} else {
		sb.WriteString("- ACR: insufficient data\n")
	}
	sb.WriteString("\n")

	// Recent weekly volume.
	if len(summaries) > 0 {
		sb.WriteString("## Recent Training Volume (last 8 weeks)\n")
		sb.WriteString("| Week | Duration | Distance | Workouts | Avg HR |\n")
		sb.WriteString("|------|----------|----------|----------|--------|\n")
		for _, s := range summaries {
			hrStr := "--"
			if s.AvgHeartRate > 0 {
				hrStr = fmt.Sprintf("%.0f", s.AvgHeartRate)
			}
			distStr := fmt.Sprintf("%.1f km", s.TotalDistance/1000)
			fmt.Fprintf(&sb, "| %s | %s | %s | %d | %s |\n",
				s.WeekStart, formatDurationSecs(s.TotalDuration), distStr, s.WorkoutCount, hrStr)
		}
		sb.WriteString("\n")
	}

	// Upcoming races.
	if len(races) > 0 {
		sb.WriteString("## Upcoming Races\n")
		for _, r := range races {
			paceInfo := ""
			if r.TargetTime != nil && r.DistanceM > 0 {
				paceSecPerKm := float64(*r.TargetTime) / (r.DistanceM / 1000)
				paceMin := int(paceSecPerKm) / 60
				paceSec := int(paceSecPerKm) % 60
				paceInfo = fmt.Sprintf(", target pace: %d:%02d/km", paceMin, paceSec)
			}
			targetStr := ""
			if r.TargetTime != nil {
				h, m, s := secondsToHMS(*r.TargetTime)
				if h > 0 {
					targetStr = fmt.Sprintf(", target: %dh%02dm%02ds", h, m, s)
				} else {
					targetStr = fmt.Sprintf(", target: %dm%02ds", m, s)
				}
			}
			fmt.Fprintf(&sb, "- %s on %s (%.1f km, priority %s%s%s)\n",
				r.Name, r.Date, r.DistanceM/1000, r.Priority, targetStr, paceInfo)
			if r.Notes != "" {
				fmt.Fprintf(&sb, "  Notes: %s\n", r.Notes)
			}
		}
		sb.WriteString("\n")
	}

	// Athlete notes.
	if len(notes) > 0 {
		sb.WriteString("## Athlete Notes\n")
		for _, n := range notes {
			date := n.CreatedAt
			if len(date) > 10 {
				date = date[:10]
			}
			fmt.Fprintf(&sb, "- [%s] %s\n", date, n.Content)
		}
		sb.WriteString("\n")
	}

	// Previous week's plan for continuity.
	if prevPlanJSON != "" {
		sb.WriteString("## Previous Week's Plan\n")
		if prevPlanCreatedAt != "" && len(prevPlanCreatedAt) > 10 {
			fmt.Fprintf(&sb, "Generated: %s, Model: %s\n", prevPlanCreatedAt[:10], prevPlanModel)
		}
		sb.WriteString("```json\n")
		sb.WriteString(prevPlanJSON)
		sb.WriteString("\n```\n\n")
	}

	// User's custom prompt additions.
	if customPrompt != "" {
		sb.WriteString("## Additional Instructions\n")
		sb.WriteString(customPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Generate the 7-day plan now as a JSON array. Output ONLY the JSON array, no other text.\n")

	return sb.String()
}

// parsePlanResponse strips optional markdown fences and unmarshals the Claude
// response into a validated []DayPlan slice. weekStart and weekEnd are used to
// verify the response covers exactly the requested 7-day window with no duplicates.
func parsePlanResponse(response, weekStart, weekEnd string) ([]DayPlan, error) {
	response = strings.TrimSpace(response)

	// Strip markdown code fences if present.
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var plan []DayPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan JSON: %w", err)
	}

	if len(plan) != 7 {
		return nil, fmt.Errorf("plan must have exactly 7 days, got %d", len(plan))
	}

	// Build the set of expected dates (weekStart inclusive through weekEnd inclusive).
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return nil, fmt.Errorf("invalid weekStart %q: %w", weekStart, err)
	}
	expectedDates := make(map[string]bool, 7)
	for i := 0; i < 7; i++ {
		expectedDates[start.AddDate(0, 0, i).Format("2006-01-02")] = true
	}

	seenDates := make(map[string]bool, 7)
	for i, day := range plan {
		if day.Date == "" {
			return nil, fmt.Errorf("day %d missing date", i)
		}
		if !expectedDates[day.Date] {
			return nil, fmt.Errorf("day %d has unexpected date %s (not in week %s..%s)", i, day.Date, weekStart, weekEnd)
		}
		if seenDates[day.Date] {
			return nil, fmt.Errorf("duplicate date %s in plan", day.Date)
		}
		seenDates[day.Date] = true

		if !day.RestDay && day.Session == nil {
			return nil, fmt.Errorf("day %d (%s): not a rest day but has no session", i, day.Date)
		}
		if day.RestDay && day.Session != nil {
			// Tolerate rest_day=true with an empty session — strip the session.
			plan[i].Session = nil
		}
	}

	// Confirm all expected dates were present.
	for d := range expectedDates {
		if !seenDates[d] {
			return nil, fmt.Errorf("plan is missing date %s", d)
		}
	}

	return plan, nil
}

// formatDurationSecs formats a duration in seconds as "Hh Mm" or "Mm" for display.
func formatDurationSecs(secs int) string {
	h := secs / 3600
	m := (secs % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// secondsToHMS decomposes a duration in seconds to hours, minutes, seconds.
func secondsToHMS(secs int) (h, m, s int) {
	h = secs / 3600
	m = (secs % 3600) / 60
	s = secs % 60
	return
}
