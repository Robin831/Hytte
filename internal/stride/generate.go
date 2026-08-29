package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// dayPlanSchemaFields describes the JSON fields for each day object and its
// session in the weekly training plan. Shared between plan generation and chat
// prompts so that both stay in sync when the schema changes.
const dayPlanSchemaFields = `Each day object:
- "date": string — "YYYY-MM-DD"
- "rest_day": boolean — true for complete rest (no session needed), false otherwise
- "session": object (required when rest_day is false):
  - "warmup": string — warmup description (empty string if none)
  - "main_set": string — main workout description. For interval/rep sessions with a target pace, give both units per the Workout Description Formatting rules.
  - "cooldown": string — cooldown description (empty string if none)
  - "strides": string — strides description (empty string if none)
  - "target_hr_cap": integer — max HR for this session in bpm (0 if not applicable)
  - "description": string — 1-2 sentence summary of the session purpose
  - "library_id": integer — the id of the Workout Library entry this session is based on; omit (or 0) for sessions not taken from the library`

// workoutFormatGuidance instructs the model to express interval/rep sessions in
// both distance and time, and target pace in both min/km and km/h, so sessions
// transfer cleanly to a treadmill. It also warns that a belt speed is NOT the
// same number as an outdoor pace target, mirroring the treadmill caveat the
// evaluation prompt already gives (see buildEvalPrompt). Shared between
// plan generation and chat editing so both produce the same format.
const workoutFormatGuidance = `## Workout Description Formatting
When a main set has intervals or reps with a target pace, make it treadmill-friendly by giving BOTH units:
- Distance AND its time equivalent: "4x2000m (or 4x9min)". Compute the time from distance × pace.
- Pace in min/km AND speed in km/h: "at 4:28-4:32/km (13.2-13.4 km/h)". Speed = 60 / pace_in_min_per_km; a faster pace (lower min/km) is a higher km/h, so list the km/h range ascending.
- Full example: "4x2000m (or 4x9min) at 4:28-4:32/km (13.2-13.4 km/h)".
- For time-based blocks (e.g. 6x6min) add the distance equivalent the same way: "6x6min (or 6x~1350m) at 4:25-4:30/km (13.3-13.6 km/h)".
Round time equivalents to the nearest half-minute and km/h to one decimal. Continuous easy/recovery/long runs described by time or HR don't need this dual-unit treatment.

### Treadmill speeds are NOT the same number as outdoor speeds
The km/h figure derived from an outdoor pace target is NOT a belt setting. Never present it as one. Two independent effects make the belt number lower:
1. **Measurement.** Indoors there is no GPS, so the watch estimates distance from wrist or foot-pod accelerometry. That estimate is driven largely by cadence rather than by belt speed, so its error is NOT a fixed percentage — for the same runner it can be near-zero at one belt speed and very large at another, because cadence barely changes while the belt does. Treat indoor watch pace and distance as unusable rather than as a number to be corrected: never quote a watch under-read percentage, never derive one from the data in this prompt, and never convert a watch-reported indoor pace into a belt speed. An interval prescribed as "1000m" indoors is measured by a sensor that does not agree with the belt.
2. **Physiology.** A treadmill removes the self-generated airflow that evaporates sweat outdoors, so heat accumulates, plasma and stroke volume fall, and HR climbs for the same mechanical work. The same effort therefore costs more HR indoors, and HR drift over a session is markedly higher.

Consequences for how you write sessions:
- Prescribe treadmill intervals by TIME and BELT SPEED, never by watch distance. Write "4x5min at belt 12.0-12.2 km/h", not "4x1000m at 12.5 km/h".
- When a session may be run either indoors or out, give the two prescriptions SEPARATELY and label them — an outdoor pace/distance target, and a distinct belt-speed/time target. Do not present one number as serving both.
- If a "` + treadmillCalibrationTitle + `" section appears later in this prompt, its numbers are authoritative: use them verbatim and do not adjust, recompute, or second-guess them. Only when no such section is present may you fall back to the rough default that a matched-HR belt speed sits a few percent below the outdoor km/h figure — and you must label that as an unverified starting estimate, never as this athlete's measured offset.
- **Outdoor-derived zone ceilings do NOT transfer indoors for continuous runs.** An easy/long-run HR cap set from outdoor running is too strict on a belt, because the same mechanical work costs more HR indoors; enforcing it forces the belt down to a speed that no longer trains anything. Use the athlete's calibrated indoor HR offset when one is given; otherwise govern continuous/steady indoor runs by (i) cardiac drift across the run, (ii) an absolute max HR ceiling for the session, and (iii) a nasal-breathing / conversational check — and allow roughly 3-8 bpm above the outdoor zone ceiling before treating the effort as too hard. **Interval sessions are the exception: the work-rep HR ceiling applies indoors unchanged.**
- **Judge indoor HR by the shape of the curve, not the average alone.** A step up over the first ~10-15 minutes that then plateaus is thermal load equilibrating — that is acceptable, and the answer is cooling (fan, fluids), not a slower belt. A continuous rise that never levels off means the effort is genuinely too hard — drop the belt speed. Use this shape as the tiebreaker whenever indoor HR sits above the outdoor zone ceiling.
- State explicitly that on a treadmill the athlete should judge the session by HR and belt speed and ignore the watch's pace and distance readouts.
- Recommend a fan directed at the torso for any indoor threshold or long session, and a chest strap rather than wrist HR — indoors HR is the only reliable signal, so it must not also be noisy.`

// bakkenPhilosophy holds the Marius Bakken threshold-dominant coaching model:
// core philosophy, HR rules, session templates, strides and load management. It
// carries no output contract of its own; weeklyInstructions pairs it with
// weeklyOutputFormat.
const bakkenPhilosophy = `You are an expert running coach applying the Marius Bakken threshold-dominant training model, adapted for recreational runners doing 3-5 sessions per week.

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
- Main set: 6x6min (or 6x~1500m) at BELOW threshold pace/HR, 2min recovery jog
- Cooldown: 10-15 min Zone 1 jog
- Alternative formats: 6-8x1000m, 3-4x3000m, 2x4000m — always below threshold HR
- Express every interval main set in both distance and time, and pace in both min/km and km/h (see Workout Description Formatting).

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

` + workoutFormatGuidance

// weeklyOutputFormat describes the JSON contract for a 7-day plan: the day
// object schema and an example array. weeklyInstructions appends it to
// bakkenPhilosophy after a blank line.
const weeklyOutputFormat = `## Output Format
Return ONLY a JSON array of day objects for the requested week. No markdown, no explanation, no code fences.

` + dayPlanSchemaFields + `

Example output structure:
[
  {"date":"2026-04-06","rest_day":false,"session":{"warmup":"15 min easy jog + 4x100m strides","main_set":"6x1000m (or 6x4:30) at 4:28-4:32/km (13.2-13.4 km/h), 60s recovery jog","cooldown":"10 min easy jog","strides":"","target_hr_cap":165,"description":"Threshold intervals to develop lactate threshold fitness. Core Marius Bakken session."}},
  {"date":"2026-04-07","rest_day":true}
]
`

// weeklyInstructions is the instruction block the weekly generation prompt opens
// with: the coaching philosophy followed by the 7-day output contract, separated
// by a blank line. Callers should use this rather than concatenating the two
// constants themselves, so the separator lives in one place.
const weeklyInstructions = bakkenPhilosophy + "\n\n" + weeklyOutputFormat

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
	// LibraryID names the workout-library entry this session was taken from,
	// when the coach used one. It is what makes library usage counts exact
	// instead of inferred from text matching.
	LibraryID int64 `json:"library_id,omitempty"`
}

// runPromptFunc is the function used to call Claude. Override in tests.
var runPromptFunc = func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
	return training.RunPrompt(ctx, cfg, prompt)
}

// strideDefaultModel is the model Stride's planning calls fall back to when the
// athlete has not chosen one. Both the weekly generator and GenerateMacroPlan
// read it, so the block is never planned on a cheaper model than its weeks.
const strideDefaultModel = "claude-opus-4-6"

// applyStrideModelDefault pins cfg to strideDefaultModel when the athlete has
// not chosen a model of their own.
//
// It keys off the raw claude_model preference rather than cfg.Model on purpose:
// training.LoadClaudeConfig has already substituted its own package-wide
// default ("claude-sonnet-4-6") by the time the config reaches a caller, so an
// `if cfg.Model == ""` check here can never fire and Stride would silently plan
// on the cheaper model instead of the one this constant names.
func applyStrideModelDefault(prefs map[string]string, cfg *training.ClaudeConfig) {
	if strings.TrimSpace(prefs["claude_model"]) == "" {
		cfg.Model = strideDefaultModel
	}
}

// stripCodeFence unwraps a markdown code fence a model wrapped its JSON answer
// in. Both planning contracts say "no code fences", but a model that adds them
// anyway is answering correctly in the wrong wrapper and is not worth a retry.
//
// The closing fence is only dropped when the last line actually is one. A
// truncated answer opens a fence and then runs out mid-JSON, and dropping its
// last line unconditionally would throw away real content — typically the
// closing brace — turning a recoverable answer into "unexpected end of JSON
// input". A single-line answer (```json {...}```) has no line to drop, so the
// fence markers are trimmed off the ends instead.
func stripCodeFence(response string) string {
	trimmed := strings.TrimSpace(response)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) == 1 {
		one := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "```"), "```"))
		// On one line the opening fence's language tag ("json") sits in front
		// of the payload rather than on a line of its own, so skip to where the
		// JSON actually starts. A tagless fence already starts there.
		if i := strings.IndexAny(one, "{["); i > 0 {
			one = one[i:]
		}
		return strings.TrimSpace(one)
	}

	body := lines[1:]
	if n := len(body); n > 0 && strings.HasPrefix(strings.TrimSpace(body[n-1]), "```") {
		body = body[:n-1]
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
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

// decryptCustomPrompt decrypts a raw stride_custom_prompt preference value.
// The preference is stored encrypted at rest, so callers must never use the raw
// value. A decrypt failure is logged and degrades to an empty string so plan
// generation and chat both keep working without the custom instructions rather
// than failing outright.
func decryptCustomPrompt(raw string) string {
	if raw == "" {
		return ""
	}
	dec, err := encryption.DecryptField(raw)
	if err != nil {
		log.Printf("stride: failed to decrypt stride_custom_prompt, skipping: %v", err)
		return ""
	}
	return dec
}

// loadCustomPrompt reads the athlete's stride_custom_prompt preference and
// returns it decrypted. Any failure (preference lookup or decrypt) is logged and
// yields an empty string.
func loadCustomPrompt(db *sql.DB, userID int64) string {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("stride: load preferences for user %d: %v", userID, err)
		return ""
	}
	return decryptCustomPrompt(prefs["stride_custom_prompt"])
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

	// Override the model to strideDefaultModel unless the athlete picked one.
	applyStrideModelDefault(prefs, claudeCfg)

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

	// Query unconsumed stride notes for plan context.
	notes, err := listUnconsumedNotes(ctx, db, userID)
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}

	// Query completed race results linked to workouts.
	raceHistory, err := listRaceResults(ctx, db, userID)
	if err != nil {
		return fmt.Errorf("list race results: %w", err)
	}

	// Read optional custom prompt appended to the plan generation request.
	// Decrypt it because the preference is stored encrypted at rest.
	customPrompt := decryptCustomPrompt(prefs["stride_custom_prompt"])

	// Athlete-measured treadmill calibration, persisted across weeks so the coach
	// never has to re-derive belt-speed and indoor-HR offsets — and so it cannot
	// invent a figure the athlete's own data contradicts.
	treadmillCalibration := treadmillCalibrationFromPrefs(prefs)

	// User training constraints.
	availableDays := prefs["stride_available_days"]          // e.g. "5" or comma-separated list
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

	// Load nightly evaluations from the past 14 days so the coach can react to
	// per-session adherence and any flags raised during nightly evaluation. The
	// 14-day window covers the previous full week plus a buffer for late evals.
	evalSince := time.Now().UTC().AddDate(0, 0, -14)
	evaluations, err := listRecentEvaluations(ctx, db, userID, evalSince)
	if err != nil {
		// Non-fatal: log and continue without recent evaluations.
		log.Printf("stride: load recent evaluations for user %d: %v", userID, err)
		evaluations = nil
	}

	// Build the user training profile block.
	profileBlock := training.BuildUserProfileBlock(db, userID)

	// Latest race-prediction snapshot: the weekly cron refreshes it right
	// before calling GeneratePlan, so the coach paces sessions off this week's
	// honest fitness estimate instead of flying blind. Non-fatal when absent.
	racePrediction, err := training.GetLatestRacePrediction(db, userID)
	if err != nil {
		log.Printf("stride: load race prediction for user %d: %v", userID, err)
		racePrediction = nil
	}

	// The workout library: curated sessions (with usage recency and ratings)
	// the coach rotates through instead of free-generating the same intervals
	// every week. Seeded with the 6x6min reference on first use so the weekly
	// benchmark exists even before the user curates anything.
	if err := SeedReferenceWorkout(ctx, db, userID); err != nil {
		log.Printf("stride: seed reference workout for user %d: %v", userID, err)
	}
	libraryWorkouts, err := ListLibraryWorkouts(ctx, db, userID, false)
	if err != nil {
		log.Printf("stride: load workout library for user %d: %v", userID, err)
		libraryWorkouts = nil
	}

	// Assemble the full prompt.
	prompt := buildGeneratePrompt(
		weekStart, weekEnd,
		profileBlock,
		races, notes,
		raceHistory,
		acr, acute, chronic,
		recentSummaries,
		prevPlanJSON, prevPlanModel, prevPlanCreatedAt,
		evaluations,
		availableDays, weeklyDistanceCap,
		treadmillCalibration,
		customPrompt,
		racePrediction,
		libraryWorkouts,
	)

	// Call Claude.
	response, err := runPromptFunc(ctx, claudeCfg, prompt)
	if err != nil {
		return fmt.Errorf("Claude prompt: %w", err)
	}

	// Parse the response. The weekly contract asks for a bare array of 7 day
	// objects; parsePlanEnvelope also accepts the AdjustWeek envelope, so a
	// model answering in the richer shape has its week and its adjustment
	// summary read rather than thrown away. Only the summary: this path has no
	// macro block, so an envelope's goal_update is always rejected by the clamp
	// below (MacroPlanID stays zero) — goal proposals reach the athlete's
	// history through the block-aware path, never through here.
	env, err := parsePlanEnvelope(response, weekStart, weekEnd)
	if err != nil {
		return fmt.Errorf("parse plan response: %w", err)
	}

	// This path plans without a macro block, so there is no phase to copy down
	// and no macro week to mark materialised, and a goal proposal has no goal
	// history to append to. The block-aware caller that fills those in is wired
	// separately; saveWeeklyPlan is the same write either way.
	return saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:    userID,
		WeekStart: weekStart,
		WeekEnd:   weekEnd,
		Envelope:  env,
		Prompt:    prompt,
		Response:  response,
		Model:     claudeCfg.Model,
		Notes:     notes,
	})
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

// listUnconsumedNotes returns stride notes for a user that have not yet been
// consumed by any process and that are routed to the weekly plan generator
// (scope IN ('any','weekly')). Results are ordered most recent first with a
// safety limit of 200.
func listUnconsumedNotes(ctx context.Context, db *sql.DB, userID int64) ([]Note, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, plan_id, content, target_date, scope, created_at
		FROM stride_notes
		WHERE user_id = ? AND consumed_at IS NULL AND scope IN ('any','weekly')
		ORDER BY created_at DESC
		LIMIT 200
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.TargetDate, &n.Scope, &n.CreatedAt); err != nil {
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

// RaceResult holds a completed race linked to a workout for prompt context.
type RaceResult struct {
	Name      string
	Date      string
	DistanceM float64
	TimeSecs  int
	Priority  string
}

// EvaluationRow is a single nightly evaluation joined with its workout (when
// present) for prompt rendering. Date is the YYYY-MM-DD of the workout, or of
// the eval itself for rest-day / missed-session entries.
type EvaluationRow struct {
	WorkoutID *int64
	Date      string
	Sport     string
	DistanceM float64
	Eval      Evaluation
}

// listRecentEvaluations returns evaluation rows for the user whose effective
// date (workout started_at date, or eval.Date for rest-day / missed-session
// entries) is at or after since, ordered by that date ascending with a stable
// tiebreak on the eval row id.
//
// Filtering and ordering are done in Go rather than SQL because rest-day evals
// have created_at set to the nightly job run time (D+1 T03:00) while their
// effective date is eval.Date (D). Using created_at in the SQL WHERE/ORDER
// would produce a window boundary that is off by one day and an ordering that
// does not match the dates rendered in the prompt.
//
// The SQL pre-filter uses a 2-day buffer to ensure no rest-day evals are
// dropped before the Go post-filter can evaluate their effective date.
func listRecentEvaluations(ctx context.Context, db *sql.DB, userID int64, since time.Time) ([]EvaluationRow, error) {
	sinceDate := since.UTC().Format("2006-01-02")
	// 2-day buffer so rest-day evals (created_at = eval.Date+1 T03:00) are not
	// dropped by the SQL pre-filter before the Go date check below.
	sqlSince := since.UTC().AddDate(0, 0, -2).Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.workout_id, e.eval_json, e.created_at,
		       NULLIF(w.started_at, ''), w.sport, w.distance_meters
		FROM stride_evaluations e
		LEFT JOIN workouts w ON w.id = e.workout_id AND w.user_id = e.user_id
		WHERE e.user_id = ?
		  AND e.created_at >= ?
		LIMIT 200
	`, userID, sqlSince)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		row    EvaluationRow
		evalID int64
	}
	var candidates []candidate
	for rows.Next() {
		var (
			evalID    int64
			workoutID sql.NullInt64
			encJSON   string
			createdAt string
			startedAt sql.NullString
			sport     sql.NullString
			distanceM sql.NullFloat64
		)
		if err := rows.Scan(&evalID, &workoutID, &encJSON, &createdAt, &startedAt, &sport, &distanceM); err != nil {
			return nil, err
		}

		decJSON, derr := encryption.DecryptField(encJSON)
		if derr != nil {
			log.Printf("stride: decrypt eval_json for user %d: %v; skipping", userID, derr)
			continue
		}
		var eval Evaluation
		if err := json.Unmarshal([]byte(decJSON), &eval); err != nil {
			log.Printf("stride: unmarshal eval_json for user %d: %v; skipping", userID, err)
			continue
		}

		row := EvaluationRow{Eval: eval}
		if workoutID.Valid {
			id := workoutID.Int64
			row.WorkoutID = &id
		}
		switch {
		case startedAt.Valid && startedAt.String != "":
			row.Date = extractDate(startedAt.String)
		case eval.Date != "":
			row.Date = eval.Date
		default:
			row.Date = extractDate(createdAt)
		}
		if sport.Valid {
			row.Sport = sport.String
		}
		if distanceM.Valid {
			row.DistanceM = distanceM.Float64
		}
		candidates = append(candidates, candidate{row: row, evalID: evalID})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort and post-filter by the computed effective date so that the result
	// is ordered by the dates actually rendered in the prompt.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].row.Date != candidates[j].row.Date {
			return candidates[i].row.Date < candidates[j].row.Date
		}
		return candidates[i].evalID < candidates[j].evalID
	})
	var out []EvaluationRow
	for _, c := range candidates {
		if c.row.Date >= sinceDate {
			out = append(out, c.row)
		}
	}
	return out, nil
}

// renderEvaluationsSection formats recent evaluations into a Markdown section
// for inclusion in the plan generation prompt. Returns an empty string when
// the list is empty so the caller can omit the heading entirely.
func renderEvaluationsSection(evals []EvaluationRow) string {
	if len(evals) == 0 {
		return ""
	}

	const noteCap = 200

	var sb strings.Builder
	sb.WriteString("## Recent Workout Evaluations (last 14 days)\n")
	for _, r := range evals {
		sportLabel := "rest day"
		if r.WorkoutID != nil {
			sportLabel = r.Sport
			if sportLabel == "" {
				sportLabel = "workout"
			}
			if r.DistanceM > 0 {
				sportLabel = fmt.Sprintf("%s, %.1f km", sportLabel, r.DistanceM/1000)
			}
		}

		planned := r.Eval.PlannedType
		if planned == "" {
			planned = "unknown"
		}
		actual := r.Eval.ActualType
		if actual == "" {
			actual = "unknown"
		}
		compliance := r.Eval.Compliance
		if compliance == "" {
			compliance = "unknown"
		}

		fmt.Fprintf(&sb, "- [%s] %s — planned %s vs actual %s — compliance: %s",
			r.Date, sportLabel, planned, actual, compliance)
		if len(r.Eval.Flags) > 0 {
			fmt.Fprintf(&sb, " — flags: %s", strings.Join(r.Eval.Flags, ", "))
		}
		sb.WriteString("\n")
		if notes := truncate(strings.TrimSpace(r.Eval.Notes), noteCap); notes != "" {
			fmt.Fprintf(&sb, "  Notes: %s\n", notes)
		}
		if adj := truncate(strings.TrimSpace(r.Eval.Adjustments), noteCap); adj != "" {
			fmt.Fprintf(&sb, "  Adjustments: %s\n", adj)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// truncate returns s shortened to at most n runes, appending an ellipsis when
// truncation occurs. Empty input returns empty output.
func truncate(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// listRaceResults queries completed race results for the user that are linked
// to at least one workout.
func listRaceResults(ctx context.Context, db *sql.DB, userID int64) ([]RaceResult, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sr.name, sr.date, sr.distance_m, sr.result_time, sr.priority
		FROM stride_races sr
		WHERE sr.user_id = ?
		  AND sr.result_time IS NOT NULL
		  AND sr.result_time > 0
		  AND EXISTS (
			SELECT 1
			FROM workouts w
			WHERE w.race_id = sr.id
			  AND w.user_id = sr.user_id
		  )
		ORDER BY sr.date DESC
		LIMIT 20
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []RaceResult{}
	for rows.Next() {
		var r RaceResult
		var encName string
		if err := rows.Scan(&encName, &r.Date, &r.DistanceM, &r.TimeSecs, &r.Priority); err != nil {
			return nil, err
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			return nil, fmt.Errorf("decrypt race name: %w", err)
		}
		r.Name = name
		results = append(results, r)
	}
	return results, rows.Err()
}

// renderUserConstraints renders the athlete's hard training limits. Shared with
// the AdjustWeek prompt so both prompts state the same constraints in the same
// words; see adjust_prompt.go.
func renderUserConstraints(availableDays, weeklyDistanceCap string) string {
	var sb strings.Builder
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
	return sb.String()
}

// renderProfileSection wraps the athlete profile block (HR zones, threshold,
// paces) in its prompt heading. Empty input yields no section.
func renderProfileSection(profileBlock string) string {
	if profileBlock == "" {
		return ""
	}
	return "## User Profile\n" + profileBlock + "\n"
}

// renderRacePredictionSection renders the stored race-prediction snapshot: the
// coach's honest anchor for session paces (threshold ~ current half-marathon
// race pace) instead of guessing from raw workout history. Empty when no
// snapshot exists.
func renderRacePredictionSection(racePrediction *training.StoredRacePrediction) string {
	if racePrediction == nil || len(racePrediction.Predictions) == 0 {
		return ""
	}
	var sb strings.Builder
	asOf := racePrediction.CreatedAt
	if len(asOf) >= 10 {
		asOf = asOf[:10]
	}
	fmt.Fprintf(&sb, "## Current Fitness Estimate (race predictions, as of %s)\n", asOf)
	for _, p := range racePrediction.Predictions {
		fmt.Fprintf(&sb, "- %s: %s (%s/km", p.Distance, p.PredictedTime, p.PacePerKm)
		if p.Confidence != "" {
			fmt.Fprintf(&sb, ", confidence %s", p.Confidence)
		}
		sb.WriteString(")\n")
	}
	if racePrediction.Rationale != "" {
		fmt.Fprintf(&sb, "\nPrediction rationale: %s\n", racePrediction.Rationale)
	}
	sb.WriteString("\n")
	return sb.String()
}

// libraryRulesTemplate is the rotation contract that goes with the workout
// library listing. libraryRulesBlockToken names the training block a library
// entry has to suit: the weekly generator has no macro week to name one from
// and passes libraryBlockUnknown, while the AdjustWeek prompt substitutes the
// target week's actual block so the rule is instantiated rather than abstract.
//
// The token is substituted with strings.Replace, not Fprintf: this prose is
// prompt text that will keep growing, and a literal % added to it later (or a
// % inside the phrase) would otherwise corrupt the prompt with %!x(MISSING)
// noise that go vet cannot catch once the phrase is built elsewhere.
const libraryRulesTemplate = `
Library rules:
- The [WEEKLY REFERENCE] session MUST appear exactly once this week, with its structure unchanged (adjust target paces to current fitness only). It is the fixed benchmark the athlete tracks week over week.
- Draw the other quality sessions (threshold/hard/long-run variants) from the library when a suitable entry exists for {{BLOCK}} — and VARY them: do not schedule a library workout whose "last" week is within the past 3 weeks, unless nothing else fits the block. Prefer higher-rated and less-recently-used entries.
- When a session is taken from the library, set its "library_id" to the entry's id and keep the structure; you may tune paces/HR targets to current fitness.
- Easy runs and sessions with no suitable library entry are composed freely as usual (library_id omitted).

`

// libraryRulesBlockToken is the placeholder libraryRulesTemplate reserves for
// the training-block phrase.
const libraryRulesBlockToken = "{{BLOCK}}"

// libraryBlockUnknown is the abstract phrase libraryRulesTemplate falls back to
// when the prompt cannot name the block concretely.
const libraryBlockUnknown = "the current training block"

// renderWorkoutLibrarySection lists the curated sessions the coach rotates
// through, followed by the rotation rules. This is what breaks the "same
// intervals every week" loop: the reference session is the one fixed weekly
// benchmark, everything else must vary. blockPhrase instantiates the "suitable
// for ..." rule — pass libraryBlockUnknown when no concrete block is known.
func renderWorkoutLibrarySection(libraryWorkouts []LibraryWorkout, blockPhrase string) string {
	if len(libraryWorkouts) == 0 {
		return ""
	}
	if blockPhrase == "" {
		blockPhrase = libraryBlockUnknown
	}
	var sb strings.Builder
	sb.WriteString("## Workout Library\n")
	sb.WriteString("Curated sessions to draw quality days from. For each: id, name, type, suitable blocks, athlete rating (0=unrated..5), times used, last used week.\n\n")
	for _, lw := range libraryWorkouts {
		marker := ""
		if lw.IsReference {
			marker = " [WEEKLY REFERENCE]"
		}
		blocks := strings.Join(lw.Blocks, ",")
		if blocks == "" {
			blocks = "any"
		}
		lastUsed := lw.LastUsedAt
		if lastUsed == "" {
			lastUsed = "never"
		}
		fmt.Fprintf(&sb, "- id=%d%s %q (%s; blocks: %s; rating %d; used %dx; last %s)\n",
			lw.ID, marker, lw.Name, lw.WorkoutType, blocks, lw.Rating, lw.TimesUsed, lastUsed)
		fmt.Fprintf(&sb, "  warmup: %s | main set: %s | cooldown: %s", lw.Warmup, lw.MainSet, lw.Cooldown)
		if lw.Strides != "" {
			fmt.Fprintf(&sb, " | strides: %s", lw.Strides)
		}
		if lw.TargetHRCap != "" {
			fmt.Fprintf(&sb, " | HR cap: %s", lw.TargetHRCap)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(strings.Replace(libraryRulesTemplate, libraryRulesBlockToken, blockPhrase, 1))
	return sb.String()
}

// renderUpcomingRacesSection lists the races the plan has to respect, with
// target time and derived target pace.
func renderUpcomingRacesSection(races []Race) string {
	if len(races) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Upcoming Races\n")
	for _, r := range races {
		// formatRaceTime/formatPaceSecPerKm are shared with the macro
		// prompt so both prompts quote the same race in the same form.
		paceInfo := ""
		if r.TargetTime != nil && r.DistanceM > 0 {
			paceInfo = fmt.Sprintf(", target pace: %s/km", formatPaceSecPerKm(float64(*r.TargetTime)/(r.DistanceM/1000)))
		}
		targetStr := ""
		if r.TargetTime != nil {
			targetStr = fmt.Sprintf(", target: %s", formatRaceTime(*r.TargetTime))
		}
		fmt.Fprintf(&sb, "- %s on %s (%.1f km, priority %s%s%s)\n",
			r.Name, r.Date, r.DistanceM/1000, r.Priority, targetStr, paceInfo)
		if r.Notes != "" {
			fmt.Fprintf(&sb, "  Notes: %s\n", r.Notes)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderAthleteNotesSection renders the unconsumed stride notes the athlete
// wrote for the plan generator.
func renderAthleteNotesSection(notes []Note) string {
	if len(notes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Athlete Notes\n")
	for _, n := range notes {
		date := n.CreatedAt
		if len(date) > 10 {
			date = date[:10]
		}
		fmt.Fprintf(&sb, "- [%s] %s\n", date, n.Content)
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderPreviousPlanSection embeds the previous week's plan JSON verbatim, so
// the coach can carry structure forward instead of restarting every week.
func renderPreviousPlanSection(prevPlanJSON, prevPlanModel, prevPlanCreatedAt string) string {
	if prevPlanJSON == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Previous Week's Plan\n")
	if prevPlanCreatedAt != "" && len(prevPlanCreatedAt) > 10 {
		fmt.Fprintf(&sb, "Generated: %s, Model: %s\n", prevPlanCreatedAt[:10], prevPlanModel)
	}
	sb.WriteString("```json\n")
	sb.WriteString(prevPlanJSON)
	sb.WriteString("\n```\n\n")
	return sb.String()
}

// renderCustomPromptSection appends the athlete's own standing instructions.
func renderCustomPromptSection(customPrompt string) string {
	if customPrompt == "" {
		return ""
	}
	return "## Additional Instructions\n" + customPrompt + "\n\n"
}

// buildGeneratePrompt assembles the full prompt string for Claude plan generation.
func buildGeneratePrompt(
	weekStart, weekEnd string,
	profileBlock string,
	races []Race,
	notes []Note,
	raceHistory []RaceResult,
	acr *float64, acute, chronic float64,
	summaries []training.WeeklySummary,
	prevPlanJSON, prevPlanModel, prevPlanCreatedAt string,
	evaluations []EvaluationRow,
	availableDays, weeklyDistanceCap string,
	treadmillCalibration string,
	customPrompt string,
	racePrediction *training.StoredRacePrediction,
	libraryWorkouts []LibraryWorkout,
) string {
	var sb strings.Builder

	sb.WriteString(weeklyInstructions)
	sb.WriteString("\n\n")

	// Target week.
	fmt.Fprintf(&sb, "## Plan Request\nGenerate a 7-day training plan for the week of %s to %s.\n\n", weekStart, weekEnd)

	// User training constraints.
	sb.WriteString(renderUserConstraints(availableDays, weeklyDistanceCap))

	// User profile (HR zones, threshold, goal race, etc.).
	sb.WriteString(renderProfileSection(profileBlock))

	// Athlete-measured treadmill calibration. Overrides the generic belt-speed
	// and indoor-HR defaults in the instructions above.
	sb.WriteString(renderTreadmillCalibration(treadmillCalibration))

	// Current fitness estimate: the weekly race-prediction snapshot, refreshed
	// just before this plan generates.
	sb.WriteString(renderRacePredictionSection(racePrediction))

	// The workout library, with the rotation rules. The weekly generator has no
	// macro week to read a concrete block off, so the rule stays abstract here.
	sb.WriteString(renderWorkoutLibrarySection(libraryWorkouts, libraryBlockUnknown))

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
	sb.WriteString(renderUpcomingRacesSection(races))

	// Race history (completed races linked to workouts).
	if len(raceHistory) > 0 {
		sb.WriteString("## Race History\n")
		for _, r := range raceHistory {
			pace := formatPaceSecPerKm(float64(r.TimeSecs) / (r.DistanceM / 1000))
			fmt.Fprintf(&sb, "- %s on %s (%.1f km, %s, pace %s/km, priority %s)\n",
				r.Name, r.Date, r.DistanceM/1000, formatRaceTime(r.TimeSecs), pace, r.Priority)
		}
		sb.WriteString("\n")
	}

	// Athlete notes.
	sb.WriteString(renderAthleteNotesSection(notes))

	// Previous week's plan for continuity.
	sb.WriteString(renderPreviousPlanSection(prevPlanJSON, prevPlanModel, prevPlanCreatedAt))

	// Recent nightly evaluations from the previous ~2 weeks. Closes the planning
	// loop by surfacing per-session adherence, fatigue flags, and any
	// adjustments the coach already recommended.
	sb.WriteString(renderEvaluationsSection(evaluations))

	// User's custom prompt additions.
	sb.WriteString(renderCustomPromptSection(customPrompt))

	sb.WriteString("Generate the 7-day plan now as a JSON array. Output ONLY the JSON array, no other text.\n")

	return sb.String()
}

// parsePlanResponse strips optional markdown fences and unmarshals the Claude
// response into a validated []DayPlan slice. weekStart and weekEnd are used to
// verify the response covers exactly the requested 7-day window with no duplicates.
//
// This is the bare-array contract only, and is what the chat handler uses to
// validate a plan the athlete edited in conversation. Callers that may receive
// the AdjustWeek envelope go through parsePlanEnvelope / parseAdjustEnvelope
// (adjust.go), which validate the week with the same validatePlanDays below.
func parsePlanResponse(response, weekStart, weekEnd string) ([]DayPlan, error) {
	// Strip markdown code fences if present, the same way the macro plan
	// response is unwrapped — one heuristic, one place to fix it.
	response = stripCodeFence(response)

	var plan []DayPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan JSON: %w", err)
	}
	if err := validatePlanDays(plan, weekStart, weekEnd); err != nil {
		return nil, err
	}
	return plan, nil
}

// validatePlanDays checks that plan covers exactly the 7 dates of
// weekStart..weekEnd, once each, with a session on every non-rest day. It
// mutates plan in place only to strip the session off a day the coach marked
// rest_day=true, which is a tolerated inconsistency rather than an error.
//
// Shared by both response contracts so the bare array and the envelope's "week"
// can never diverge on what a valid week is.
func validatePlanDays(plan []DayPlan, weekStart, weekEnd string) error {
	if len(plan) != 7 {
		return fmt.Errorf("plan must have exactly 7 days, got %d", len(plan))
	}

	// Build the set of expected dates (weekStart inclusive through weekEnd inclusive).
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return fmt.Errorf("invalid weekStart %q: %w", weekStart, err)
	}
	expectedDates := make(map[string]bool, 7)
	for i := 0; i < 7; i++ {
		expectedDates[start.AddDate(0, 0, i).Format("2006-01-02")] = true
	}

	seenDates := make(map[string]bool, 7)
	for i, day := range plan {
		if day.Date == "" {
			return fmt.Errorf("day %d missing date", i)
		}
		if !expectedDates[day.Date] {
			return fmt.Errorf("day %d has unexpected date %s (not in week %s..%s)", i, day.Date, weekStart, weekEnd)
		}
		if seenDates[day.Date] {
			return fmt.Errorf("duplicate date %s in plan", day.Date)
		}
		seenDates[day.Date] = true

		if !day.RestDay && day.Session == nil {
			return fmt.Errorf("day %d (%s): not a rest day but has no session", i, day.Date)
		}
		if day.RestDay && day.Session != nil {
			// Tolerate rest_day=true with an empty session — strip the session.
			plan[i].Session = nil
		}
	}

	// Confirm all expected dates were present.
	for d := range expectedDates {
		if !seenDates[d] {
			return fmt.Errorf("plan is missing date %s", d)
		}
	}

	return nil
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
