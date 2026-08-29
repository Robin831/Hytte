package stride

// The AdjustWeek prompt. Where the weekly generator (generate.go) plans a week
// from scratch, this prompt adjusts ONE week of an existing macro block: the
// block states the intent, the elapsed weeks state the trajectory in summary,
// and the last fortnight states the immediate past in detail.
//
// Everything the athlete wrote or measured is reused verbatim from the weekly
// builder's section renderers, so the two prompts never drift apart. Everything
// derived is summarised in Go so the whole prompt stays around 7-8k tokens.
//
// Deliberately omitted: raw workouts and their laps (a week is adjusted from
// adherence and load, not from lap tables), the block's full 26-week table (the
// macro prompt's job, and the elapsed weeks say the same thing in fewer
// tokens), chat transcripts, and the legacy goal_race_* preferences — Stride's
// goal comes from the block's own goal revision, never from those.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/hrzones"
	"github.com/Robin831/Hytte/internal/training"
)

// ErrNoMacroWeek reports that no active macro block covers the week being
// adjusted. It is a routing signal rather than a failure: the caller falls back
// to the legacy weekly prompt so the athlete always gets a week, even when
// macro generation failed or Stride was only just enabled.
var ErrNoMacroWeek = errors.New("stride: no active macro week covers the requested week")

// adjustRaceLookaheadWeeks bounds the race calendar the adjust prompt carries.
// A week is adjusted against the races close enough to shape it; the block
// already accounts for everything further out.
const adjustRaceLookaheadWeeks = 6

// adjustACRTrendWeeks is how many weekly ACR points the fitness signals show.
// Four weeks is one mesocycle — enough to read a direction, short enough that
// the direction is about this block.
const adjustACRTrendWeeks = 4

// adjustRecentAdjustmentWeeks is how many previous weeks' adjustment summaries
// the coach sees. They exist so it does not oscillate: cutting a week that the
// previous week already cut is a different decision from cutting a fresh one.
const adjustRecentAdjustmentWeeks = 4

// adjustEvaluationWindowDays matches the weekly generator's evaluation window,
// so both prompts read the same "immediate past".
const adjustEvaluationWindowDays = 14

// adjustHalfMarathonRule restates the standing priority in one sentence. The
// full doctrine lives in macroHalfMarathonRule and is spent on the block; a
// week only has to be judged against it.
const adjustHalfMarathonRule = `## Half-Marathon Priority
Improving half-marathon performance is always the main priority: judge every adjustment you make to this week by whether it still serves the block's half-marathon goal.
`

// adjustmentRules is the contract for deviating from the macro week. The macro
// week's spec is the default answer; these are the only grounds for departing
// from it, and the only things the coach may propose but not do.
const adjustmentRules = `## Adjustment Rules
You are adjusting ONE week of an existing macro block, not planning a block. The macro week's spec below is the default answer: return a week that meets its target distance, its session count and its key sessions unless the evidence in this prompt says otherwise.

Reduce volume and/or intensity below the macro week's target when ANY of the following holds:
- ACR is above 1.3;
- two or more key sessions were missed across the elapsed weeks of this block (see Block Progress);
- an "overtraining", "injury_risk" or "hr_too_high" flag appears in the recent evaluations;
- an athlete note reports illness.
Name the trigger that fired in "adjustment.summary".

You MAY add up to 10% to the macro week's target distance when the athlete was fully compliant with the previous week AND ACR is below 0.8. Never add more than 10%, and never exceed the athlete's weekly distance cap.

NEVER change the week's phase. The phase is fixed by the macro block and nothing in this prompt overrides it.

You may PROPOSE a phase change or a goal change, but you may not apply one. Every proposal must carry a reason: put a phase-change proposal in "adjustment.summary", and a target-time proposal in "adjustment.goal_update" together with its own reason. A proposal without a reason is discarded.

Set "adjustment.deviates" to true whenever the week you return departs from the macro week's target distance, session count or key sessions, and false when it follows the spec.
`

// adjustOutputContract describes the envelope the adjust prompt must answer
// with: the week itself plus what the coach did to it. The day objects inside
// "week" are the same shape the weekly generator returns, so dayPlanSchemaFields
// is shared rather than restated.
const adjustOutputContract = `## Output Format
Return ONLY a single JSON object. No markdown, no explanation, no code fences.

{
  "week": array — exactly 7 day objects, one per date of the requested week, in ascending date order
  "adjustment": {
    "deviates": boolean — true when the returned week departs from the macro week's spec, false when it follows it
    "summary": string — 1-3 sentences on how this week departs from the macro week and why, naming the evidence. Next week's coach reads this, so make it concrete.
    "goal_update": object or null — a proposed change to the block's target half-marathon time, null when you are not proposing one:
      {
        "target_hm_time_s": integer — the proposed target half-marathon time in SECONDS
        "reason": string — why the target should move
      }
  }
}

` + dayPlanSchemaFields + `

Example output structure:
{"week":[{"date":"2026-04-06","rest_day":false,"session":{"warmup":"15 min easy jog + 4x100m strides","main_set":"6x1000m (or 6x4:30) at 4:28-4:32/km (13.2-13.4 km/h), 60s recovery jog","cooldown":"10 min easy jog","strides":"","target_hr_cap":165,"description":"Threshold intervals — the macro week's key session.","library_id":3}},{"date":"2026-04-07","rest_day":true}],"adjustment":{"deviates":false,"summary":"Followed the macro week's 62 km across 5 sessions: ACR 1.05, no flags, last week fully compliant.","goal_update":null}}
`

// adjustInstructions is the static instruction block every adjust prompt opens
// with: the coaching philosophy, the standing half-marathon priority, the rules
// for departing from the macro week, and the output envelope. Callers use this
// rather than concatenating the parts themselves, the same way the weekly path
// uses weeklyInstructions.
const adjustInstructions = bakkenPhilosophy + "\n\n" +
	adjustHalfMarathonRule + "\n" +
	adjustmentRules + "\n" +
	adjustOutputContract

// adjustPromptContext is what buildAdjustPrompt hands back: the prompt plus the
// context the caller has to persist alongside Claude's answer. The macro week
// and the notes are returned rather than re-read, so the rows written after the
// call are exactly the rows the prompt was rendered from.
type adjustPromptContext struct {
	Prompt    string
	MacroPlan *MacroPlan
	MacroWeek MacroWeek
	Notes     []Note
	// CurrentGoal is the block's goal in the revision the prompt showed the
	// coach. saveWeeklyPlan clamps a proposed goal_update against it, so it has
	// to be the same number the model was asked to revise.
	CurrentGoal MacroGoal
}

// buildAdjustPrompt assembles the full AdjustWeek prompt for the week starting
// at weekStart (a Monday) and ending at weekEnd, both YYYY-MM-DD.
//
// Returns ErrNoMacroWeek when no active block covers the week, which is the
// caller's signal to fall back to the legacy weekly prompt. Every other input
// is optional: an athlete with no predictions, no VO2max history and no elapsed
// weeks still gets a complete prompt with "n/a" where the data would be.
func buildAdjustPrompt(ctx context.Context, db *sql.DB, userID int64, weekStart, weekEnd string) (*adjustPromptContext, error) {
	block, err := GetActiveMacroPlan(ctx, db, userID, weekStart)
	if err != nil {
		return nil, fmt.Errorf("load active macro plan: %w", err)
	}
	if block == nil {
		return nil, ErrNoMacroWeek
	}
	target, ok := macroWeekAt(block, weekStart)
	if !ok {
		return nil, ErrNoMacroWeek
	}

	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return nil, fmt.Errorf("load preferences: %w", err)
	}

	var sb strings.Builder

	sb.WriteString(adjustInstructions)
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "## Adjust Request\nAdjust and materialise the training week of %s to %s.\n\n", weekStart, weekEnd)

	// The plan's intent: the block goal, where this week sits in it, and the
	// spec this week is contracted to deliver.
	goal, revisionLabel := currentBlockGoal(ctx, db, userID, block)
	sb.WriteString(renderMacroPlanBlock(block, target, goal, revisionLabel))

	// The trajectory, in summary: one row per elapsed week of the block.
	sb.WriteString(buildBlockProgressTable(ctx, db, userID, block, weekStart))

	// Where the athlete's fitness and load actually are right now.
	sb.WriteString(buildFitnessSignals(db, userID, block, time.Now().UTC()))

	// What the coach already did to the previous weeks, so it does not oscillate.
	adjustments, err := buildRecentAdjustments(ctx, db, userID, weekStart)
	if err != nil {
		return nil, err
	}
	sb.WriteString(adjustments)

	// Hard limits the week must respect.
	sb.WriteString(renderUserConstraints(prefs["stride_available_days"], prefs["stride_weekly_distance_cap"]))

	// Athlete profile, with the legacy goal-race preferences stripped: the goal
	// comes from the block's goal revision, never from goal_race_*.
	sb.WriteString(renderProfileSection(stripGoalRaceSection(training.BuildUserProfileBlock(db, userID))))

	// Athlete-measured treadmill calibration; authoritative over the generic
	// belt-speed and indoor-HR defaults in the instructions above.
	sb.WriteString(renderTreadmillCalibration(treadmillCalibrationFromPrefs(prefs)))

	// Current fitness estimate, so session paces are anchored to today.
	racePrediction, err := training.GetLatestRacePrediction(db, userID)
	if err != nil {
		log.Printf("stride: load race prediction for user %d: %v", userID, err)
		racePrediction = nil
	}
	sb.WriteString(renderRacePredictionSection(racePrediction))

	// The workout library. Unlike the weekly generator, this prompt knows the
	// block, so the "suitable for the current training block" rule is rendered
	// with a concrete value instead of an abstraction.
	if err := SeedReferenceWorkout(ctx, db, userID); err != nil {
		log.Printf("stride: seed reference workout for user %d: %v", userID, err)
	}
	libraryWorkouts, err := ListLibraryWorkouts(ctx, db, userID, false)
	if err != nil {
		log.Printf("stride: load workout library for user %d: %v", userID, err)
		libraryWorkouts = nil
	}
	sb.WriteString(renderWorkoutLibrarySection(libraryWorkouts, libraryBlockPhrase(target.Phase)))

	// Races close enough to shape this week.
	races, err := ListRaces(db, userID)
	if err != nil {
		return nil, fmt.Errorf("list races: %w", err)
	}
	sb.WriteString(renderUpcomingRacesSection(racesWithin(races, weekStart, adjustRaceLookaheadWeeks)))

	// The athlete's own words, and the previous week in full detail.
	notes, err := listUnconsumedNotes(ctx, db, userID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	sb.WriteString(renderAthleteNotesSection(notes))

	prevPlanJSON, prevPlanModel, prevPlanCreatedAt, err := loadPreviousPlan(ctx, db, userID, weekStart)
	if err != nil {
		log.Printf("stride: load previous plan for user %d: %v", userID, err)
		prevPlanJSON = ""
	}
	sb.WriteString(renderPreviousPlanSection(prevPlanJSON, prevPlanModel, prevPlanCreatedAt))

	evalSince := time.Now().UTC().AddDate(0, 0, -adjustEvaluationWindowDays)
	evaluations, err := listRecentEvaluations(ctx, db, userID, evalSince)
	if err != nil {
		log.Printf("stride: load recent evaluations for user %d: %v", userID, err)
		evaluations = nil
	}
	sb.WriteString(renderEvaluationsSection(evaluations))

	// The athlete's standing instructions, last so they override.
	sb.WriteString(renderCustomPromptSection(decryptCustomPrompt(prefs["stride_custom_prompt"])))

	sb.WriteString("Return the adjusted week now as the JSON object described above. Output ONLY that object, no other text.\n")

	return &adjustPromptContext{
		Prompt:      sb.String(),
		MacroPlan:   block,
		MacroWeek:   target,
		Notes:       notes,
		CurrentGoal: goal,
	}, nil
}

// racesWithin filters the calendar down to unfinished races between today and
// weeks weeks after weekStart. Races between today and the target week still
// matter — they constrain how the week may be loaded — so the window opens at
// whichever of the two comes first.
func racesWithin(races []Race, weekStart string, weeks int) []Race {
	from := time.Now().UTC().Format(dateLayout)
	if weekStart < from {
		from = weekStart
	}
	start, err := parseWeekDate(weekStart)
	if err != nil {
		log.Printf("stride: parse week start %q for race window: %v", weekStart, err)
		return nil
	}
	through := start.AddDate(0, 0, 7*weeks).Format(dateLayout)

	var out []Race
	for _, r := range races {
		if r.ResultTime != nil || r.Date < from || r.Date > through {
			continue
		}
		out = append(out, r)
	}
	return out
}

// libraryBlockForPhase maps a macro week's phase onto the workout library's
// block taxonomy (base|build|peak|taper — see validBlocks). The library has no
// entry for the 'race' and 'recovery' phases, so those take the nearest
// neighbour: a race week is run off the taper in front of it, and a recovery
// week trains like base. An unknown phase maps to "", meaning "cannot say".
func libraryBlockForPhase(phase string) string {
	switch phase {
	case MacroPhaseBase, MacroPhaseBuild, MacroPhasePeak, MacroPhaseTaper:
		return phase
	case MacroPhaseRace:
		return MacroPhaseTaper
	case MacroPhaseRecovery:
		return MacroPhaseBase
	default:
		return ""
	}
}

// libraryBlockPhrase instantiates libraryRulesTemplate's "suitable for ..."
// clause with the target week's actual block, so the rule is a test the coach
// can apply rather than an abstraction. Falls back to the weekly generator's
// wording when the phase is outside the taxonomy.
func libraryBlockPhrase(phase string) string {
	block := libraryBlockForPhase(phase)
	if block == "" {
		return libraryBlockUnknown
	}
	return fmt.Sprintf("the current training block, which for this week is %q — an entry is suitable when its listed blocks include %s, or when it lists none (blocks: any)", block, block)
}

// blockWeekAdherence is what one elapsed week's stride plan says about
// adherence: how many sessions it prescribed, how many of them the coach's
// evaluations matched to a logged workout, how many warning flags those
// evaluations raised, and whether the week was evaluated at all.
type blockWeekAdherence struct {
	planned   int
	completed int
	flags     int
	hasPlan   bool
	evaluated bool
}

// blockAdherence is the per-week fold together with what is actually known
// about it. The two reads behind it fail independently: plansKnown says the
// stride plans were read, so hasPlan and planned mean something; evalsKnown
// says the evaluations were folded in, so evaluated, completed and flags mean
// something. A false flag makes the table render those columns as unknown,
// because a failed read must never render as a fact about the athlete.
type blockAdherence struct {
	byWeek     map[string]blockWeekAdherence
	plansKnown bool
	evalsKnown bool
}

// blockCellUnknown marks a cell whose input could not be read. It is
// deliberately distinct from "--" (no plan was generated) and "?" (the week was
// never evaluated), both of which are facts about the athlete's training.
const blockCellUnknown = "n/a"

// buildBlockProgressTable renders one row per elapsed week of the block:
// planned versus actual distance, planned versus completed sessions, threshold
// minutes and flag count. It is the block's trajectory in the fewest tokens
// that still show whether the athlete is keeping up with the plan.
//
// Only weeks that started before the target week are shown; the weeks still
// ahead are the block's business, not this adjustment's.
//
// Every derived input degrades rather than aborting the prompt, but degrading
// is not zeroing: a failed lookup renders its cells as `n/a` and is named in a
// note above the table. A zero here would read as "the athlete trained nothing"
// and a "--" as "no plan was ever generated", and the coach cuts the week on
// either — so a query failure must never be able to say them.
func buildBlockProgressTable(ctx context.Context, db *sql.DB, userID int64, block *MacroPlan, weekStart string) string {
	var sb strings.Builder
	sb.WriteString("## Block Progress (elapsed weeks of this block)\n")

	var elapsed []MacroWeek
	for _, w := range block.Weeks {
		if w.WeekStart < weekStart {
			elapsed = append(elapsed, w)
		}
	}
	if len(elapsed) == 0 {
		sb.WriteString("No week of this block has elapsed yet — the target week is where the block starts.\n\n")
		return sb.String()
	}

	ranges := make([]weekRange, 0, len(elapsed))
	weekStarts := make([]string, 0, len(elapsed))
	for _, w := range elapsed {
		end := shiftDays(w.WeekStart, 6)
		if end == "" {
			continue
		}
		ranges = append(ranges, weekRange{start: w.WeekStart, end: end})
		weekStarts = append(weekStarts, w.WeekStart)
	}

	distByWeek, err := computeWeeksDistanceMeters(db, userID, ranges)
	distKnown := err == nil
	if err != nil {
		log.Printf("stride: compute block distance for user %d: %v", userID, err)
	}

	// Zones need the athlete's HR boundaries. Without them — or without the
	// workouts to bucket — threshold minutes are unknown, not zero.
	zoneBoundaries, zoneErr := hrzones.GetUserZones(db, userID)
	if zoneErr != nil {
		log.Printf("stride: load HR zones for user %d: %v", userID, zoneErr)
	}
	zoneByWeek, zoneComputeErr := computeWeeksZoneSeconds(db, userID, ranges, zoneBoundaries)
	if zoneComputeErr != nil {
		log.Printf("stride: compute block zone seconds for user %d: %v", userID, zoneComputeErr)
	}
	zonesUnknownReason := ""
	switch {
	case zoneErr != nil || zoneComputeErr != nil:
		zonesUnknownReason = "the workout or HR-zone query failed"
	case len(zoneBoundaries) == 0:
		zonesUnknownReason = "this athlete has no HR zones configured"
	}
	zonesKnown := zonesUnknownReason == ""

	// The loader hands back whatever it did read, so a failure that only hits
	// the evaluations still leaves the plan columns usable.
	adherence, err := loadBlockWeekAdherence(ctx, db, userID, weekStarts)
	if err != nil {
		log.Printf("stride: load block adherence for user %d: %v", userID, err)
	}

	sb.WriteString("Planned km and planned sessions are the macro week's targets. Actual km is logged volume across ALL sports, so read it as an upper bound on running.\n")
	sb.WriteString("Done counts the plan's sessions that the coach's evaluations matched to a logged workout; `?` means that week was never evaluated at all and `--` means no plan was ever generated for it — neither is the same as missed training.\n")
	sb.WriteString("Thr min is the total minutes of workouts whose AVERAGE heart rate fell in zones 3-4, not time-in-zone. Flags counts the warning flags raised in that week's evaluations.\n")
	sb.WriteString(blockProgressUnavailableNote(distKnown, zonesUnknownReason, adherence))
	sb.WriteString("\n")
	sb.WriteString("| Week | Seq | Phase | Planned km | Actual km | Planned sessions | Done | Thr min | Flags |\n")
	sb.WriteString("|------|-----|-------|------------|-----------|------------------|------|---------|-------|\n")
	for _, w := range elapsed {
		a := adherence.byWeek[w.WeekStart]
		var done, flags string
		switch {
		case !adherence.plansKnown:
			// The plans could not be read, so nothing is known about the week:
			// "--" would claim no plan existed and "0" would claim the athlete
			// completed nothing.
			done, flags = blockCellUnknown, blockCellUnknown
		case !a.hasPlan:
			// No stride plan was ever generated for that week; the legend
			// above tells the coach what "--" means.
			done, flags = "--", "0"
		case !adherence.evalsKnown:
			done, flags = blockCellUnknown, blockCellUnknown
		case !a.evaluated:
			done, flags = "?", "0"
		default:
			done, flags = fmt.Sprintf("%d", a.completed), fmt.Sprintf("%d", a.flags)
		}
		planned := fmt.Sprintf("%d", w.TargetSessions)
		if adherence.plansKnown && a.hasPlan && a.planned != w.TargetSessions {
			// The plan that actually ran may have prescribed a different count
			// from the macro target; show both rather than hide the deviation.
			planned = fmt.Sprintf("%d (plan %d)", w.TargetSessions, a.planned)
		}
		actual := blockCellUnknown
		if distKnown {
			actual = fmt.Sprintf("%.1f", distByWeek[w.WeekStart]/1000)
		}
		thrMin := blockCellUnknown
		if zonesKnown {
			zones := zoneByWeek[w.WeekStart]
			thrMin = fmt.Sprintf("%d", zones[1]/60)
		}
		fmt.Fprintf(&sb, "| %s | %d | %s | %.1f | %s | %s | %s | %s | %s |\n",
			w.WeekStart, w.Seq, w.Phase, w.TargetKm, actual,
			planned, done, thrMin, flags)
	}
	sb.WriteString("\n")
	return sb.String()
}

// blockProgressUnavailableNote names the inputs that could not be read and why,
// so the coach knows an `n/a` cell is a missing lookup and not a training
// absence. It returns "" when every input is known, which is the normal case.
func blockProgressUnavailableNote(distKnown bool, zonesUnknownReason string, adherence blockAdherence) string {
	var missing []string
	if !distKnown {
		missing = append(missing, "Actual km (the workout query failed)")
	}
	if zonesUnknownReason != "" {
		missing = append(missing, "Thr min ("+zonesUnknownReason+")")
	}
	switch {
	case !adherence.plansKnown:
		missing = append(missing, "Done, Flags and the plan's own session count (the plan query failed)")
	case !adherence.evalsKnown:
		missing = append(missing, "Done and Flags (the evaluation query failed)")
	}
	if len(missing) == 0 {
		return ""
	}
	return "DATA UNAVAILABLE: " + strings.Join(missing, "; ") + ". Those cells say `" + blockCellUnknown +
		"`, which is a lookup this prompt could not make and NOT a fact about the athlete: do not read `" +
		blockCellUnknown + "` as missed training, as an unplanned week or as zero volume, and never let it trigger a reduction.\n"
}

// shiftDays returns the date n days after date, or "" when date is unparseable.
func shiftDays(date string, n int) string {
	d, err := parseWeekDate(date)
	if err != nil {
		return ""
	}
	return d.AddDate(0, 0, n).Format(dateLayout)
}

// loadBlockWeekAdherence reads the stride plans for the given weeks and folds
// their evaluations into per-week adherence counts.
//
// It always returns what it managed to read, together with flags saying which
// of the two reads succeeded, so a caller that hits an error on the evaluations
// still keeps the plan counts instead of discarding a populated map.
//
// Evaluations are de-duplicated by workout within a plan, and only compliant or
// partial evaluations of a real prescription count as done — the same reading
// GetPlanHistory applies, so the two never disagree about the same week.
func loadBlockWeekAdherence(ctx context.Context, db *sql.DB, userID int64, weekStarts []string) (blockAdherence, error) {
	out := make(map[string]blockWeekAdherence, len(weekStarts))
	// Nothing read yet: until the plan query returns, every column behind it is
	// unknown rather than empty.
	result := blockAdherence{byWeek: out}
	if len(weekStarts) == 0 {
		result.plansKnown, result.evalsKnown = true, true
		return result, nil
	}

	args := make([]any, 0, len(weekStarts)+1)
	args = append(args, userID)
	for _, w := range weekStarts {
		args = append(args, w)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, week_start, plan_json
		FROM stride_plans
		WHERE user_id = ? AND week_start IN (`+placeholders(len(weekStarts))+`)
	`, args...)
	if err != nil {
		return result, fmt.Errorf("query block plans: %w", err)
	}
	defer rows.Close()

	weekByPlan := map[int64]string{}
	for rows.Next() {
		var planID int64
		var week, planJSON string
		if err := rows.Scan(&planID, &week, &planJSON); err != nil {
			return result, fmt.Errorf("scan block plan: %w", err)
		}
		var days []DayPlan
		if err := json.Unmarshal([]byte(planJSON), &days); err != nil {
			log.Printf("stride: decode plan_json for plan %d: %v", planID, err)
			continue
		}
		a := out[week]
		a.hasPlan = true
		for _, d := range days {
			if !d.RestDay && d.Session != nil {
				a.planned++
			}
		}
		out[week] = a
		weekByPlan[planID] = week
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	// The plans are fully read: hasPlan and planned are now facts.
	result.plansKnown = true
	if len(weekByPlan) == 0 {
		// No plan means no evaluation can exist, so the evaluation columns are
		// known to be empty rather than unread.
		result.evalsKnown = true
		return result, nil
	}

	planIDs := make([]any, 0, len(weekByPlan)+1)
	planIDs = append(planIDs, userID)
	for id := range weekByPlan {
		planIDs = append(planIDs, id)
	}
	evalRows, err := db.QueryContext(ctx, `
		SELECT plan_id, workout_id, eval_json
		FROM stride_evaluations
		WHERE user_id = ? AND plan_id IN (`+placeholders(len(weekByPlan))+`)
	`, planIDs...)
	if err != nil {
		return result, fmt.Errorf("query block evaluations: %w", err)
	}
	defer evalRows.Close()

	seenWorkout := map[int64]map[int64]bool{}
	for evalRows.Next() {
		var planID int64
		var workoutID sql.NullInt64
		var encJSON string
		if err := evalRows.Scan(&planID, &workoutID, &encJSON); err != nil {
			return result, fmt.Errorf("scan block evaluation: %w", err)
		}
		week, ok := weekByPlan[planID]
		if !ok {
			continue
		}
		if workoutID.Valid {
			if seenWorkout[planID] == nil {
				seenWorkout[planID] = map[int64]bool{}
			}
			if seenWorkout[planID][workoutID.Int64] {
				continue
			}
			seenWorkout[planID][workoutID.Int64] = true
		}
		decJSON, err := encryption.DecryptField(encJSON)
		if err != nil {
			log.Printf("stride: decrypt eval_json for user %d: %v; skipping", userID, err)
			continue
		}
		var eval Evaluation
		if err := json.Unmarshal([]byte(decJSON), &eval); err != nil {
			log.Printf("stride: unmarshal eval_json for user %d: %v; skipping", userID, err)
			continue
		}
		a := out[week]
		a.evaluated = true
		a.flags += len(eval.Flags)
		if (eval.Compliance == "compliant" || eval.Compliance == "partial") && eval.PlannedType != "none" {
			a.completed++
		}
		out[week] = a
	}
	if err := evalRows.Err(); err != nil {
		// The plan columns survive: only the evaluation fold is incomplete.
		return result, fmt.Errorf("iterate block evaluations: %w", err)
	}
	result.evalsKnown = true
	return result, nil
}

// placeholders returns "?, ?, ..." for an IN clause of n values.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// buildFitnessSignals renders where the athlete's load and fitness actually
// are: ACR now with a four-week trend, the derived training status, and the
// half-marathon prediction and VO2max both now and at the block's start, so the
// coach can see whether the block is working.
//
// Every signal degrades to "n/a" rather than to an error: a new athlete has
// none of this data and must still get a usable prompt.
func buildFitnessSignals(db *sql.DB, userID int64, block *MacroPlan, now time.Time) string {
	var sb strings.Builder
	sb.WriteString("## Fitness Signals\n")

	acr, acute, chronic, err := training.ComputeACR(db, userID, now)
	if err != nil {
		log.Printf("stride: compute ACR for user %d: %v", userID, err)
		acr = nil
	}
	if acr == nil {
		sb.WriteString("- ACR now: n/a (insufficient data)\n")
	} else {
		fmt.Fprintf(&sb, "- ACR now: %.2f (acute=%.1f, chronic=%.1f) — the optimal window is 0.8-1.3\n", *acr, acute, chronic)
	}

	trend, err := training.ComputeACRTrend(db, userID, now, adjustACRTrendWeeks)
	if err != nil {
		log.Printf("stride: compute ACR trend for user %d: %v", userID, err)
		trend = nil
	}
	if len(trend) == 0 {
		fmt.Fprintf(&sb, "- ACR last %d weeks: n/a\n", adjustACRTrendWeeks)
	} else {
		parts := make([]string, 0, len(trend))
		for _, p := range trend {
			if p.ACR == nil {
				parts = append(parts, p.Date+" n/a")
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %.2f", p.Date, *p.ACR))
		}
		fmt.Fprintf(&sb, "- ACR last %d weeks (oldest first): %s\n", adjustACRTrendWeeks, strings.Join(parts, ", "))
	}

	loads, err := training.GetWeeklyLoads(db, userID, 8)
	if err != nil {
		log.Printf("stride: load weekly loads for user %d: %v", userID, err)
		loads = nil
	}
	fmt.Fprintf(&sb, "- Training status: %s\n", training.ClassifyTrainingStatus(loads, acr))

	sb.WriteString(renderHMPredictionSignal(db, userID, block.StartWeek))
	sb.WriteString(renderVO2maxSignal(db, userID, block.StartWeek))
	sb.WriteString("\n")
	return sb.String()
}

// hmPredictionSnapshots is how many stored race-prediction snapshots are read
// to find the one current at the block's start. The prediction is refreshed
// weekly, so this comfortably covers a 26-week block and the weeks before it.
const hmPredictionSnapshots = 60

// renderHMPredictionSignal renders the predicted half-marathon time now against
// the prediction that was current when the block started — the honest reading
// of whether the block is moving the athlete toward the goal.
func renderHMPredictionSignal(db *sql.DB, userID int64, blockStart string) string {
	history, err := training.GetRacePredictionHistory(db, userID, hmPredictionSnapshots)
	if err != nil {
		log.Printf("stride: load race prediction history for user %d: %v", userID, err)
		history = nil
	}
	// training.GetRacePredictionHistory orders by created_at DESC, so this
	// slice is NEWEST first — the opposite of GetVO2maxHistory below. Read the
	// latest off the front, and the block-start snapshot as the first entry
	// scanning forward (backwards in time) that is not after the block started.
	nowSecs := halfMarathonSeconds(firstOrNil(history))
	var atStart *training.StoredRacePrediction
	for i := range history {
		if extractDate(history[i].CreatedAt) <= blockStart {
			atStart = &history[i]
			break
		}
	}
	startSecs := halfMarathonSeconds(atStart)

	if nowSecs <= 0 {
		return "- Half-marathon prediction: n/a\n"
	}
	if startSecs <= 0 {
		return fmt.Sprintf("- Half-marathon prediction: %s (no prediction recorded at block start)\n", formatRaceTime(nowSecs))
	}
	return fmt.Sprintf("- Half-marathon prediction: %s (block start %s, %s)\n",
		formatRaceTime(nowSecs), formatRaceTime(startSecs), signedDuration(nowSecs-startSecs))
}

// firstOrNil returns a pointer to the first snapshot, or nil for an empty slice.
func firstOrNil(history []training.StoredRacePrediction) *training.StoredRacePrediction {
	if len(history) == 0 {
		return nil
	}
	return &history[0]
}

// halfMarathonSeconds pulls the half-marathon entry out of a prediction
// snapshot, or 0 when the snapshot is missing or has no half-marathon row.
func halfMarathonSeconds(p *training.StoredRacePrediction) int {
	if p == nil {
		return 0
	}
	for _, pred := range p.Predictions {
		if strings.EqualFold(pred.Distance, "Half Marathon") {
			return pred.TimeSeconds
		}
	}
	return 0
}

// signedDuration renders a difference in seconds with an explicit sign, so a
// faster prediction reads as "-1:52" rather than as an unlabelled number.
func signedDuration(delta int) string {
	if delta == 0 {
		return "no change"
	}
	sign := "+"
	if delta < 0 {
		sign = "-"
		delta = -delta
	}
	return sign + formatRaceTime(delta)
}

// vo2maxHistoryDepth is how many VO2max estimates are read to find the one
// current at the block's start. Estimates are per qualifying workout, so a
// 26-week block plus its run-up fits well inside this.
const vo2maxHistoryDepth = 400

// renderVO2maxSignal renders the latest VO2max estimate against the last one
// recorded before the block started.
func renderVO2maxSignal(db *sql.DB, userID int64, blockStart string) string {
	history, err := training.GetVO2maxHistory(db, userID, vo2maxHistoryDepth)
	if err != nil {
		log.Printf("stride: load VO2max history for user %d: %v", userID, err)
		history = nil
	}
	if len(history) == 0 {
		return "- VO2max: n/a\n"
	}
	// training.GetVO2maxHistory re-sorts its LIMITed window by estimated_at
	// ASC, so this slice is OLDEST first — the opposite of the prediction
	// history above. The latest estimate is therefore the last element, and
	// the block-start estimate is the LAST entry not after the block started,
	// i.e. the one that was current on the block's first day.
	latest := history[len(history)-1].VO2max
	var atStart float64
	for _, e := range history {
		if extractDate(e.EstimatedAt) <= blockStart {
			atStart = e.VO2max
		}
	}
	if atStart <= 0 {
		return fmt.Sprintf("- VO2max: %.1f (no estimate recorded at block start)\n", latest)
	}
	return fmt.Sprintf("- VO2max: %.1f (block start %.1f, %+.1f)\n", latest, atStart, latest-atStart)
}

// buildRecentAdjustments renders the adjustment summaries the coach wrote for
// the previous weeks, newest first. They exist so the coach does not oscillate:
// cutting a week that last week already cut is a different decision from
// cutting a fresh one.
func buildRecentAdjustments(ctx context.Context, db *sql.DB, userID int64, weekStart string) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Recent Adjustments (last %d weeks)\n", adjustRecentAdjustmentWeeks)

	// An unparseable week start means no window can be computed. Every other
	// helper in this file treats that as "no data" rather than as a failure, so
	// this one does too — the section renders empty instead of killing the
	// prompt.
	from := shiftWeek(weekStart, -adjustRecentAdjustmentWeeks)
	if from == "" {
		log.Printf("stride: parse week start %q for recent adjustments", weekStart)
		sb.WriteString("No adjustments recorded for the previous weeks.\n\n")
		return sb.String(), nil
	}
	// adjustment_summary is TEXT NOT NULL DEFAULT '' (see db.go), but every
	// plan written by the legacy weekly path leaves it empty and COALESCE keeps
	// a future nullable column from turning a blank summary into a failed
	// prompt build.
	rows, err := db.QueryContext(ctx, `
		SELECT week_start, COALESCE(adjustment_summary, '')
		FROM stride_plans
		WHERE user_id = ? AND week_start >= ? AND week_start < ?
		ORDER BY week_start DESC
	`, userID, from, weekStart)
	if err != nil {
		return "", fmt.Errorf("query recent adjustments: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var week, stored string
		if err := rows.Scan(&week, &stored); err != nil {
			return "", fmt.Errorf("scan recent adjustment: %w", err)
		}
		summary, err := encryption.DecryptField(stored)
		if err != nil {
			log.Printf("stride: decrypt adjustment_summary for user %d week %s: %v; skipping", userID, week, err)
			continue
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			continue
		}
		fmt.Fprintf(&sb, "- [%s] %s\n", week, summary)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		sb.WriteString("No adjustments recorded for the previous weeks.\n")
	}
	sb.WriteString("\n")
	return sb.String(), nil
}
