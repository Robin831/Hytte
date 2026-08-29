package stride

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// adjustBlockWeeks is the number of weeks the test block spans. It mirrors
// MacroBlockWeeks so the fixture is shaped like a real block.
const adjustBlockWeeks = MacroBlockWeeks

// adjustTestMesocycles is the periodisation the fixture block carries: 4-week
// segments, so the target week (seq 3) reads as "week 3 of 4".
var adjustTestMesocycles = []struct {
	name  string
	phase string
	weeks int
	focus string
}{
	{"Base 1", MacroPhaseBase, 4, "aerobic volume and threshold frequency"},
	{"Base 2", MacroPhaseBase, 4, "aerobic durability"},
	{"Build 1", MacroPhaseBuild, 4, "threshold density"},
	{"Build 2", MacroPhaseBuild, 4, "threshold volume at race specificity"},
	{"Peak", MacroPhasePeak, 4, "race-specific sharpening"},
	{"Taper", MacroPhaseTaper, 4, "shed volume, keep intensity"},
	{"Race", MacroPhaseRace, 2, "run the benchmark"},
}

// seedAdjustBlock creates an active 26-week block starting at blockStart, with
// a mesocycle every 4 weeks and one week per Monday.
func seedAdjustBlock(t *testing.T, db *sql.DB, userID int64, blockStart string) *MacroPlan {
	t.Helper()

	plan := &MacroPlan{
		UserID:    userID,
		StartWeek: blockStart,
		EndWeek:   mondayAfter(blockStart, adjustBlockWeeks-1),
		Status:    MacroPlanStatusActive,
		Goal: MacroGoal{
			PrimaryFocus:  "half_marathon",
			Statement:     "GOAL-STATEMENT-MARKER: run 1:24:00 for the half marathon",
			TargetHMTimeS: 5040,
			Benchmark:     "BENCHMARK-MARKER: 3 x 3 km at threshold",
			Rationale:     "RATIONALE-MARKER: the model predicts 1:27 today.",
		},
		Model:       "claude-opus-5",
		GeneratedBy: MacroGeneratedByScheduled,
	}

	var periodisation []Mesocycle
	weeks := make([]MacroWeek, 0, adjustBlockWeeks)
	seq := 0
	for _, m := range adjustTestMesocycles {
		periodisation = append(periodisation, Mesocycle{
			Name:      m.name,
			Phase:     m.phase,
			StartWeek: mondayAfter(blockStart, seq),
			Weeks:     m.weeks,
			Focus:     m.focus,
		})
		for i := 0; i < m.weeks; i++ {
			load := LoadLevelNormal
			if i == m.weeks-1 {
				load = LoadLevelDeload
			}
			weeks = append(weeks, MacroWeek{
				WeekStart:      mondayAfter(blockStart, seq),
				Seq:            seq + 1,
				Phase:          m.phase,
				Mesocycle:      m.name,
				LoadLevel:      load,
				TargetKm:       50 + float64(seq),
				TargetSessions: 5,
				KeySessions: []KeySession{
					{Type: "threshold", Focus: "KEY-SESSION-MARKER: controlled tempo"},
					{Type: "long_run", Focus: "aerobic durability"},
				},
				Intent: fmt.Sprintf("INTENT-MARKER-%d: hold the aerobic base.", seq+1),
				Status: MacroWeekStatusPlanned,
			})
			seq++
		}
	}
	plan.Periodisation = periodisation

	if err := CreateMacroPlan(context.Background(), db, plan, weeks, "initial block goal"); err != nil {
		t.Fatalf("create macro plan: %v", err)
	}
	return plan
}

// seedAdjustEvaluation inserts one evaluation with flags, encrypted the way the
// production writer does.
func seedAdjustEvaluation(t *testing.T, db *sql.DB, userID, planID int64, workoutID *int64, eval Evaluation) {
	t.Helper()
	insertEncryptedEvaluation(t, db, userID, planID, workoutID, eval, time.Now().UTC().Format(time.RFC3339))
}

// setAdjustmentSummary writes an encrypted adjustment summary onto a plan row.
func setAdjustmentSummary(t *testing.T, db *sql.DB, planID int64, summary string) {
	t.Helper()
	enc, err := encryption.EncryptField(summary)
	if err != nil {
		t.Fatalf("encrypt adjustment summary: %v", err)
	}
	if _, err := db.Exec("UPDATE stride_plans SET adjustment_summary = ? WHERE id = ?", enc, planID); err != nil {
		t.Fatalf("set adjustment summary: %v", err)
	}
}

// adjustFixture is the seeded world one adjust-prompt test runs against.
type adjustFixture struct {
	blockStart  string
	targetWeek  string
	targetEnd   string
	block       *MacroPlan
	elapsed     []string
	elapsedPlan []int64
}

// seedAdjustFixture builds an athlete two weeks into a block: a goal revision,
// two elapsed weeks with plans, workouts, evaluations and adjustment summaries,
// a race calendar, predictions, VO2max history and the preferences the prompt
// reads (including the goal_race_* ones it must never read).
func seedAdjustFixture(t *testing.T, db *sql.DB, userID int64) adjustFixture {
	t.Helper()
	ctx := context.Background()

	targetWeek, targetEnd := upcomingWeek()
	blockStart := mondayAfter(targetWeek, -2)
	block := seedAdjustBlock(t, db, userID, blockStart)

	setPref(t, db, userID, "max_hr", "185")
	setPref(t, db, userID, "threshold_hr", "166")
	setPref(t, db, userID, "threshold_pace", "268")
	setPref(t, db, userID, "stride_available_days", "5")
	setPref(t, db, userID, "stride_weekly_distance_cap", "70")
	setPref(t, db, userID, "stride_custom_prompt", "CUSTOM-PROMPT-MARKER: no hills on Tuesdays.")
	setPref(t, db, userID, treadmillCalibrationPref, "CALIBRATION-MARKER: belt offset 3%.")

	// The legacy goal-race preferences must never reach an adjust prompt.
	setPref(t, db, userID, "goal_race_name", "GOAL-RACE-PREF-MARKER")
	setPref(t, db, userID, "goal_race_date", "2027-05-01")
	setPref(t, db, userID, "goal_race_distance", "21097")
	setPref(t, db, userID, "goal_race_target_time", "5100")

	// A goal revision on top of the initial one, so the prompt has to pick the
	// current revision rather than the block's original goal.
	if err := AddGoalRevision(ctx, db, &GoalRevision{
		MacroPlanID: block.ID,
		UserID:      userID,
		WeekStart:   mondayAfter(blockStart, 1),
		Goal: MacroGoal{
			PrimaryFocus:  "half_marathon",
			Statement:     "REVISED-GOAL-MARKER: run 1:23:30 for the half marathon",
			TargetHMTimeS: 5010,
			Benchmark:     "REVISED-BENCHMARK-MARKER: 3 x 3 km at threshold",
			Rationale:     "REVISED-RATIONALE-MARKER: the first weeks went better than modelled.",
		},
		Reason: "prediction improved by 90s",
		Source: GoalRevisionSourceWeekly,
	}); err != nil {
		t.Fatalf("add goal revision: %v", err)
	}

	// Two elapsed weeks with real volume, plans and evaluations.
	elapsed := []string{blockStart, mondayAfter(blockStart, 1)}
	planIDs := make([]int64, 0, len(elapsed))
	for wi, week := range elapsed {
		monday, err := parseWeekDate(week)
		if err != nil {
			t.Fatalf("parse elapsed week: %v", err)
		}
		var workoutIDs []int64
		for day := 0; day < 3; day++ {
			workoutIDs = append(workoutIDs, seedWorkout(t, db, userID,
				monday.AddDate(0, 0, day).Format(dateLayout), 160, fmt.Sprintf("adjust-w%d-d%d", wi, day)))
		}
		planID := seedStridePlan(t, db, userID, monday, 5)
		planIDs = append(planIDs, planID)
		setAdjustmentSummary(t, db, planID, fmt.Sprintf("ADJUSTMENT-MARKER-%d: cut the second threshold day.", wi+1))

		for i, wid := range workoutIDs {
			id := wid
			eval := Evaluation{PlannedType: "threshold", ActualType: "threshold", Compliance: "compliant"}
			if wi == 1 && i == 0 {
				eval.Flags = []string{"hr_too_high", "overtraining"}
			}
			seedAdjustEvaluation(t, db, userID, planID, &id, eval)
		}
	}

	// Race calendar: one inside the 6-week window, one well beyond it.
	inWindow, err := parseWeekDate(targetWeek)
	if err != nil {
		t.Fatalf("parse target week: %v", err)
	}
	target := 5040
	if _, err := CreateRace(db, userID, "NEAR-RACE-MARKER", inWindow.AddDate(0, 0, 21).Format(dateLayout),
		21097.5, &target, "A", ""); err != nil {
		t.Fatalf("create near race: %v", err)
	}
	if _, err := CreateRace(db, userID, "FAR-RACE-MARKER", inWindow.AddDate(0, 0, 7*20).Format(dateLayout),
		10000, nil, "C", ""); err != nil {
		t.Fatalf("create far race: %v", err)
	}

	// Race predictions: one from before the block started, one from today.
	insertRacePrediction(t, db, userID, mondayAfter(blockStart, -1)+"T06:00:00Z", 5280)
	insertRacePrediction(t, db, userID, time.Now().UTC().Format(time.RFC3339), 5130)

	// VO2max history: one estimate from before the block, one from inside it.
	// Both workouts sit before the block start so they cannot disturb the
	// elapsed weeks' volume; only estimated_at decides which side of the block
	// start an estimate falls on.
	preBlockWorkout := seedWorkout(t, db, userID, mondayAfter(blockStart, -2), 150, "adjust-vo2-pre")
	insertVO2max(t, db, userID, preBlockWorkout, 52.4, mondayAfter(blockStart, -1)+"T06:00:00Z")
	nowWorkout := seedWorkout(t, db, userID, mondayAfter(blockStart, -1), 150, "adjust-vo2-now")
	insertVO2max(t, db, userID, nowWorkout, 54.1, mondayAfter(blockStart, 1)+"T06:00:00Z")

	// An unconsumed weekly note and a library entry.
	insertNoteWithScope(t, db, userID, "STRIDE-NOTE-MARKER: calf felt tight.", "weekly")
	if err := SeedReferenceWorkout(ctx, db, userID); err != nil {
		t.Fatalf("seed reference workout: %v", err)
	}

	return adjustFixture{
		blockStart:  blockStart,
		targetWeek:  targetWeek,
		targetEnd:   targetEnd,
		block:       block,
		elapsed:     elapsed,
		elapsedPlan: planIDs,
	}
}

// insertRacePrediction stores one prediction snapshot with the given
// half-marathon time. predictions_json tolerates plaintext on read.
func insertRacePrediction(t *testing.T, db *sql.DB, userID int64, createdAt string, hmSeconds int) {
	t.Helper()
	payload := fmt.Sprintf(
		`[{"distance":"Half Marathon","distance_m":21097.5,"time_seconds":%d,"predicted_time":%q,"pace_per_km":"4:00","confidence":"medium"}]`,
		hmSeconds, formatRaceTime(hmSeconds))
	if _, err := db.Exec(`
		INSERT INTO race_predictions (user_id, created_at, method, predictions_json, rationale, inputs_json)
		VALUES (?, ?, 'formula', ?, '', '')`, userID, createdAt, payload); err != nil {
		t.Fatalf("insert race prediction: %v", err)
	}
}

// insertVO2max stores one VO2max estimate for a workout.
func insertVO2max(t *testing.T, db *sql.DB, userID, workoutID int64, value float64, estimatedAt string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO vo2max_estimates (user_id, workout_id, vo2max, method, estimated_at)
		VALUES (?, ?, ?, 'hr_ratio', ?)`, userID, workoutID, value, estimatedAt); err != nil {
		t.Fatalf("insert vo2max estimate: %v", err)
	}
}

// promptRow returns the rendered table row for a week, so a test asserts on the
// whole row rather than a substring that could match another column.
func promptRow(t *testing.T, table, week string) string {
	t.Helper()
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| "+week+" |") {
			return line
		}
	}
	t.Fatalf("no table row for week %s\n%s", week, table)
	return ""
}

func TestBuildAdjustPromptSections(t *testing.T) {
	db := setupTestDB(t)
	fx := seedAdjustFixture(t, db, 1)

	got, err := buildAdjustPrompt(context.Background(), db, 1, fx.targetWeek, fx.targetEnd)
	if err != nil {
		t.Fatalf("buildAdjustPrompt: %v", err)
	}
	prompt := got.Prompt

	if got.MacroWeek.WeekStart != fx.targetWeek {
		t.Errorf("macro week = %q, want %q", got.MacroWeek.WeekStart, fx.targetWeek)
	}
	if got.MacroPlan == nil || got.MacroPlan.ID != fx.block.ID {
		t.Errorf("macro plan not returned for the week being adjusted")
	}
	if len(got.Notes) != 1 {
		t.Errorf("notes = %d, want the one unconsumed weekly note", len(got.Notes))
	}

	// Verbatim instruction blocks carried over from the weekly builder.
	for _, want := range []string{
		bakkenPhilosophy,
		workoutFormatGuidance,
		dayPlanSchemaFields,
		adjustmentRules,
		adjustHalfMarathonRule,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing a verbatim instruction block:\n%s", truncate(want, 120))
		}
	}

	// The macro block: goal in its current revision, mesocycle, target week and
	// both neighbours.
	for _, want := range []string{
		"## Macro Block",
		"### Block Goal (revision 2, set " + mondayAfter(fx.blockStart, 1) + ", source weekly)",
		"REVISED-GOAL-MARKER",
		"- Target half-marathon time: 1:23:30",
		"### Current mesocycle\nBase 1, week 3 of 4, focus aerobic volume and threshold frequency",
		"### Target week — " + fx.targetWeek + " (week 3 of 26)",
		"- Phase: base — NEVER change this",
		"- Target distance: 52.0 km",
		"- Target sessions: 5",
		"KEY-SESSION-MARKER",
		"INTENT-MARKER-3",
		"- Suitable training block for library selection: base",
		"### Previous macro week — " + mondayAfter(fx.targetWeek, -1),
		"### Next macro week — " + mondayAfter(fx.targetWeek, 1),
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("macro block is missing %q", want)
		}
	}
	// The superseded initial goal must not be quoted alongside the revision.
	if strings.Contains(prompt, "GOAL-STATEMENT-MARKER") {
		t.Error("prompt quotes the block's original goal instead of the current revision")
	}

	// Block progress: one row per elapsed week, none for the target week.
	if !strings.Contains(prompt, "## Block Progress (elapsed weeks of this block)") {
		t.Error("prompt is missing the block progress section")
	}
	for _, week := range fx.elapsed {
		row := promptRow(t, prompt, week)
		// 3 x 10 km logged, 3 x 60 min at HR 160 (zones 3-4), 5 planned
		// sessions of which 3 were evaluated as compliant.
		for _, want := range []string{"| 30.0 |", "| 180 |", "| 3 |"} {
			if !strings.Contains(row, want) {
				t.Errorf("progress row for %s missing %q\nrow: %s", week, want, row)
			}
		}
	}
	if strings.Contains(prompt, "| "+fx.targetWeek+" |") {
		t.Error("block progress includes the target week, which has not elapsed")
	}
	// The second elapsed week raised two flags; the first raised none.
	if row := promptRow(t, prompt, fx.elapsed[1]); !strings.HasSuffix(strings.TrimSpace(row), "| 2 |") {
		t.Errorf("flag count missing from row: %s", row)
	}
	if row := promptRow(t, prompt, fx.elapsed[0]); !strings.HasSuffix(strings.TrimSpace(row), "| 0 |") {
		t.Errorf("first elapsed week should report 0 flags: %s", row)
	}

	// Fitness signals, each with its label and its block-start comparison.
	for _, want := range []string{
		"## Fitness Signals",
		"- ACR now:",
		"- ACR last 4 weeks (oldest first):",
		"- Training status:",
		"- Half-marathon prediction: 1:25:30 (block start 1:28:00, -2:30)",
		"- VO2max: 54.1 (block start 52.4, +1.7)",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("fitness signals missing %q", want)
		}
	}

	// Recent adjustments, newest first.
	if !strings.Contains(prompt, "## Recent Adjustments (last 4 weeks)") {
		t.Error("prompt is missing the recent adjustments section")
	}
	first := strings.Index(prompt, "ADJUSTMENT-MARKER-2")
	second := strings.Index(prompt, "ADJUSTMENT-MARKER-1")
	if first < 0 || second < 0 {
		t.Fatalf("recent adjustments missing: %d %d", first, second)
	}
	if first > second {
		t.Error("recent adjustments are not newest first")
	}

	// Reused athlete context.
	for _, want := range []string{
		"## User Constraints",
		"- Weekly distance cap: 70 km",
		"## User Profile",
		treadmillCalibrationHeading,
		"CALIBRATION-MARKER",
		"## Workout Library",
		"NEAR-RACE-MARKER",
		"STRIDE-NOTE-MARKER",
		"CUSTOM-PROMPT-MARKER",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing reused athlete context %q", want)
		}
	}
	if strings.Contains(prompt, "FAR-RACE-MARKER") {
		t.Errorf("prompt carries a race %d weeks out, past the %d-week window", 20, adjustRaceLookaheadWeeks)
	}

	// The library rule is instantiated with the concrete block.
	if !strings.Contains(prompt, `the current training block, which for this week is "base"`) {
		t.Error("library rule was not instantiated with the target week's block")
	}
	if strings.Contains(prompt, "for the current training block — and VARY them") {
		t.Error("library rule still carries the abstract wording")
	}

	// The output envelope sub-task 3 parses.
	for _, want := range []string{`"week"`, `"adjustment"`, `"deviates"`, `"goal_update"`, `"target_hm_time"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("output contract missing %s", want)
		}
	}
}

func TestBuildAdjustPromptExcludesRawDataAndLegacyGoalRace(t *testing.T) {
	db := setupTestDB(t)
	fx := seedAdjustFixture(t, db, 1)

	got, err := buildAdjustPrompt(context.Background(), db, 1, fx.targetWeek, fx.targetEnd)
	if err != nil {
		t.Fatalf("buildAdjustPrompt: %v", err)
	}

	for _, forbidden := range []string{
		"goal_race_",            // the legacy preference keys
		"GOAL-RACE-PREF-MARKER", // and their rendered value
		"## Training History",   // the macro prompt's 26-week table
		"## Recent Lactate",     // block-level context, not a week's
		"lap ",                  // raw lap data
		"| Lap |",               //
		"avg_pace_sec_per_km",   // raw workout columns
		"## Chat",               // chat transcripts
	} {
		if strings.Contains(got.Prompt, forbidden) {
			t.Errorf("prompt contains %q, which the adjust prompt must exclude", forbidden)
		}
	}
}

func TestBuildAdjustPromptWithoutMacroWeek(t *testing.T) {
	db := setupTestDB(t)
	weekStart, weekEnd := upcomingWeek()

	// No block at all.
	if _, err := buildAdjustPrompt(context.Background(), db, 1, weekStart, weekEnd); !errors.Is(err, ErrNoMacroWeek) {
		t.Fatalf("no block: err = %v, want ErrNoMacroWeek", err)
	}

	// A block that covers the week but has no row for it: the horizon exists,
	// the week does not, and the caller still has to fall back.
	blockStart := mondayAfter(weekStart, -2)
	block := seedAdjustBlock(t, db, 1, blockStart)
	if _, err := db.Exec("DELETE FROM stride_macro_weeks WHERE macro_plan_id = ? AND week_start = ?",
		block.ID, weekStart); err != nil {
		t.Fatalf("delete macro week: %v", err)
	}
	if _, err := buildAdjustPrompt(context.Background(), db, 1, weekStart, weekEnd); !errors.Is(err, ErrNoMacroWeek) {
		t.Fatalf("missing week: err = %v, want ErrNoMacroWeek", err)
	}
}

// A brand-new athlete has a block and nothing else. Every derived section must
// degrade to "n/a" or an explicit "none" line rather than panicking or erroring.
func TestBuildAdjustPromptNewAthlete(t *testing.T) {
	db := setupTestDB(t)
	weekStart, weekEnd := upcomingWeek()
	seedAdjustBlock(t, db, 1, weekStart)

	got, err := buildAdjustPrompt(context.Background(), db, 1, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("buildAdjustPrompt: %v", err)
	}
	for _, want := range []string{
		"No week of this block has elapsed yet",
		"- ACR now: n/a (insufficient data)",
		"- Half-marathon prediction: n/a",
		"- VO2max: n/a",
		"No adjustments recorded for the previous weeks.",
		"### Previous macro week\nNone — the target week is the first week of the block.",
	} {
		if !strings.Contains(got.Prompt, want) {
			t.Errorf("new-athlete prompt missing %q", want)
		}
	}
}

// The last week of a block has no successor; the section must say so instead of
// silently disappearing.
func TestBuildAdjustPromptLastWeekOfBlock(t *testing.T) {
	db := setupTestDB(t)
	weekStart, weekEnd := upcomingWeek()
	blockStart := mondayAfter(weekStart, -(adjustBlockWeeks - 1))
	seedAdjustBlock(t, db, 1, blockStart)

	got, err := buildAdjustPrompt(context.Background(), db, 1, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("buildAdjustPrompt: %v", err)
	}
	if !strings.Contains(got.Prompt, "### Next macro week\nNone — the target week is the last week of the block.") {
		t.Error("last week of the block should say it has no successor")
	}
}

func TestAdjustPromptSizeBudget(t *testing.T) {
	db := setupTestDB(t)
	fx := seedAdjustFixture(t, db, 1)

	got, err := buildAdjustPrompt(context.Background(), db, 1, fx.targetWeek, fx.targetEnd)
	if err != nil {
		t.Fatalf("buildAdjustPrompt: %v", err)
	}

	// Ceilings, not targets: the prompt aims at ~7-8k tokens. These catch a
	// section that has stopped summarising (raw workouts, the 26-week table),
	// not normal drift.
	inputs := strings.TrimPrefix(got.Prompt, adjustInstructions)
	if n := approxTokens(inputs); n > 6000 {
		t.Errorf("adjust inputs ≈%d tokens, over the 6000 ceiling", n)
	}
	if n := approxTokens(got.Prompt); n > 12000 {
		t.Errorf("full adjust prompt ≈%d tokens, over the 12000 ceiling", n)
	}
	if n := approxTokens(got.Prompt); n < 1000 {
		t.Errorf("full adjust prompt ≈%d tokens — suspiciously small", n)
	}
	t.Logf("adjust prompt ≈%d tokens (inputs ≈%d)", approxTokens(got.Prompt), approxTokens(inputs))
}

func TestAdjustInstructionsCarryTheRules(t *testing.T) {
	for _, want := range []string{
		"ACR is above 1.3",
		"two or more key sessions were missed",
		`an "overtraining", "injury_risk" or "hr_too_high" flag`,
		"an athlete note reports illness",
		"add up to 10% to the macro week's target distance",
		"ACR is below 0.8",
		"NEVER change the week's phase",
		"You may PROPOSE a phase change or a goal change, but you may not apply one",
		"Improving half-marathon performance is always the main priority",
	} {
		if !strings.Contains(adjustInstructions, want) {
			t.Errorf("adjust instructions missing %q", want)
		}
	}
}

func TestDescribeMesocycle(t *testing.T) {
	block := &MacroPlan{
		Periodisation: []Mesocycle{
			{Name: "Base 1", StartWeek: "2026-08-31", Weeks: 4, Focus: "aerobic volume"},
			{Name: "Build 2", StartWeek: "2026-09-28", Weeks: 4, Focus: "threshold density"},
		},
	}
	tests := []struct {
		name string
		week MacroWeek
		want string
	}{
		{
			name: "named mesocycle mid-segment",
			week: MacroWeek{WeekStart: "2026-10-05", Mesocycle: "Build 2"},
			want: "Build 2, week 2 of 4, focus threshold density",
		},
		{
			name: "falls back to the covering date span when the name is unknown",
			week: MacroWeek{WeekStart: "2026-09-07", Mesocycle: "Renamed"},
			want: "Base 1, week 2 of 4, focus aerobic volume",
		},
		{
			name: "no mesocycle covers the week",
			week: MacroWeek{WeekStart: "2027-06-07", Mesocycle: "Gone"},
			want: "Not recorded for this block.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeMesocycle(block, tc.week); got != tc.want {
				t.Errorf("describeMesocycle = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLibraryBlockPhrase(t *testing.T) {
	tests := []struct {
		phase string
		want  string
	}{
		{MacroPhaseBase, "base"},
		{MacroPhaseBuild, "build"},
		{MacroPhasePeak, "peak"},
		{MacroPhaseTaper, "taper"},
		{MacroPhaseRace, "taper"},
		{MacroPhaseRecovery, "base"},
	}
	for _, tc := range tests {
		got := libraryBlockPhrase(tc.phase)
		if !strings.Contains(got, `is "`+tc.want+`"`) {
			t.Errorf("phase %q → %q, want it to name block %q", tc.phase, got, tc.want)
		}
		// Whatever the phrase says, it must name a block the library validates.
		if !validBlocks[libraryBlockForPhase(tc.phase)] {
			t.Errorf("phase %q maps to %q, which is not a library block", tc.phase, libraryBlockForPhase(tc.phase))
		}
	}
	if got := libraryBlockPhrase("nonsense"); got != libraryBlockUnknown {
		t.Errorf("unknown phase → %q, want the abstract wording", got)
	}
}

// A week the athlete never got a plan for reads as "--", not as a zero: no plan
// means nothing can be said about adherence.
func TestBlockProgressUnplannedAndUnevaluatedWeeks(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetWeek, _ := upcomingWeek()
	blockStart := mondayAfter(targetWeek, -2)
	block := seedAdjustBlock(t, db, 1, blockStart)

	// Week 1 gets a plan with no evaluations, week 2 gets nothing at all.
	monday, err := parseWeekDate(blockStart)
	if err != nil {
		t.Fatalf("parse block start: %v", err)
	}
	seedStridePlan(t, db, 1, monday, 4)

	table := buildBlockProgressTable(ctx, db, 1, block, targetWeek)
	if row := promptRow(t, table, blockStart); !strings.Contains(row, "| ? |") {
		t.Errorf("unevaluated week should report `?`, got: %s", row)
	}
	if row := promptRow(t, table, mondayAfter(blockStart, 1)); !strings.Contains(row, "| -- |") {
		t.Errorf("week without a plan should report `--`, got: %s", row)
	}
	// The plan prescribed 4 sessions where the macro week targets 5; both show.
	if row := promptRow(t, table, blockStart); !strings.Contains(row, "5 (plan 4)") {
		t.Errorf("row should show the macro target and the plan's own count, got: %s", row)
	}
}

func TestBuildRecentAdjustmentsWindowAndOrder(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetWeek, _ := upcomingWeek()

	for _, back := range []int{1, 4, 5} {
		monday, err := parseWeekDate(mondayAfter(targetWeek, -back))
		if err != nil {
			t.Fatalf("parse week: %v", err)
		}
		planID := seedStridePlan(t, db, 1, monday, 5)
		setAdjustmentSummary(t, db, planID, fmt.Sprintf("SUMMARY-%d-WEEKS-BACK", back))
	}

	section, err := buildRecentAdjustments(ctx, db, 1, targetWeek)
	if err != nil {
		t.Fatalf("buildRecentAdjustments: %v", err)
	}
	if !strings.Contains(section, "SUMMARY-1-WEEKS-BACK") {
		t.Error("the previous week's summary is missing")
	}
	if !strings.Contains(section, "SUMMARY-4-WEEKS-BACK") {
		t.Error("the oldest in-window summary is missing")
	}
	if strings.Contains(section, "SUMMARY-5-WEEKS-BACK") {
		t.Errorf("a summary from outside the %d-week window leaked in", adjustRecentAdjustmentWeeks)
	}
	if strings.Index(section, "SUMMARY-1-WEEKS-BACK") > strings.Index(section, "SUMMARY-4-WEEKS-BACK") {
		t.Error("recent adjustments are not newest first")
	}
}

// The refactor that extracted the library section for reuse must not have moved
// the weekly generator's wording: with no concrete block it stays abstract.
func TestGeneratePromptLibraryRuleStaysAbstract(t *testing.T) {
	library := []LibraryWorkout{{ID: 1, Name: "6x6min", WorkoutType: "threshold", MainSet: "6x6min", IsReference: true}}
	prompt := buildGeneratePrompt(
		"2026-09-07", "2026-09-13", "", nil, nil, nil, nil, 0, 0, nil,
		"", "", "", nil, "", "", "", "", nil, library,
	)
	if !strings.Contains(prompt, "a suitable entry exists for the current training block — and VARY them") {
		t.Error("the weekly generator's library rule wording changed")
	}
	if strings.Contains(prompt, "which for this week is") {
		t.Error("the weekly generator must not claim to know the block")
	}
}

// The adherence fold has three distinguishable outcomes the coach reads
// differently — no plan, a plan nobody evaluated, and a count of matched
// sessions — and the count itself is de-duplicated per workout so a
// re-evaluated session is not counted twice. Both IN-clause queries are
// exercised across two weeks, so a placeholder miscount would fail here too.
func TestLoadBlockWeekAdherenceFoldsEvaluations(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetWeek, _ := upcomingWeek()
	weekA := mondayAfter(targetWeek, -2)
	weekB := mondayAfter(targetWeek, -1)

	mondayA, err := parseWeekDate(weekA)
	if err != nil {
		t.Fatalf("parse week A: %v", err)
	}
	mondayB, err := parseWeekDate(weekB)
	if err != nil {
		t.Fatalf("parse week B: %v", err)
	}
	planA := seedStridePlan(t, db, 1, mondayA, 4)
	seedStridePlan(t, db, 1, mondayB, 3)

	w1 := seedWorkout(t, db, 1, weekA, 160, "adh-w1")
	w2 := seedWorkout(t, db, 1, mondayA.AddDate(0, 0, 1).Format(dateLayout), 150, "adh-w2")
	w3 := seedWorkout(t, db, 1, mondayA.AddDate(0, 0, 2).Format(dateLayout), 140, "adh-w3")

	// Two evaluations of the SAME workout: only the first is folded in, so the
	// second one's flags never reach the table either.
	seedAdjustEvaluation(t, db, 1, planA, &w1, Evaluation{
		PlannedType: "threshold", ActualType: "threshold", Compliance: "compliant",
		Flags: []string{"hr_too_high"},
	})
	seedAdjustEvaluation(t, db, 1, planA, &w1, Evaluation{
		PlannedType: "threshold", ActualType: "threshold", Compliance: "compliant",
		Flags: []string{"overtraining", "injury_risk"},
	})
	// A missed session is evaluated but not completed.
	seedAdjustEvaluation(t, db, 1, planA, &w2, Evaluation{
		PlannedType: "threshold", ActualType: "none", Compliance: "missed",
	})
	// An unplanned extra workout: nothing was prescribed, so it is not a
	// completed session however compliant the athlete was.
	seedAdjustEvaluation(t, db, 1, planA, &w3, Evaluation{
		PlannedType: "none", ActualType: "easy", Compliance: "compliant",
	})
	// Evaluations with no workout attached deliberately bypass the dedup set,
	// so each of these counts on its own.
	seedAdjustEvaluation(t, db, 1, planA, nil, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "compliant",
	})
	seedAdjustEvaluation(t, db, 1, planA, nil, Evaluation{
		PlannedType: "long_run", ActualType: "long_run", Compliance: "partial",
	})

	got, err := loadBlockWeekAdherence(ctx, db, 1, []string{weekA, weekB})
	if err != nil {
		t.Fatalf("loadBlockWeekAdherence: %v", err)
	}

	a := got[weekA]
	if !a.hasPlan || !a.evaluated {
		t.Fatalf("week A: hasPlan=%v evaluated=%v, want both true", a.hasPlan, a.evaluated)
	}
	if a.planned != 4 {
		t.Errorf("week A planned = %d, want 4", a.planned)
	}
	if a.completed != 3 {
		t.Errorf("week A completed = %d, want 3 (one deduped workout + two workout-less evaluations)", a.completed)
	}
	if a.flags != 1 {
		t.Errorf("week A flags = %d, want 1 — the duplicate evaluation's flags must not be counted", a.flags)
	}

	b := got[weekB]
	if !b.hasPlan {
		t.Error("week B has a plan and should say so")
	}
	if b.evaluated {
		t.Error("week B was never evaluated and must render `?`, not a count")
	}
	if b.completed != 0 || b.flags != 0 {
		t.Errorf("week B completed=%d flags=%d, want 0/0", b.completed, b.flags)
	}

	if _, ok := got[mondayAfter(targetWeek, -3)]; ok {
		t.Error("a week that was never asked for leaked into the result")
	}
}

// The two history signals read slices ordered in OPPOSITE directions:
// training.GetRacePredictionHistory returns newest first, GetVO2maxHistory
// oldest first. This pins both readings — "now" really is the latest estimate
// and "block start" really is the one current when the block began — so a
// change to either query's ORDER BY fails here instead of silently inverting
// the deltas the coach reads.
func TestFitnessSignalsReadHistoriesInTheirOwnOrder(t *testing.T) {
	db := setupTestDB(t)
	targetWeek, _ := upcomingWeek()
	blockStart := mondayAfter(targetWeek, -4)
	block := seedAdjustBlock(t, db, 1, blockStart)

	// Predictions: two before the block (the later of which is the block-start
	// reading) and one from today.
	insertRacePrediction(t, db, 1, mondayAfter(blockStart, -3)+"T06:00:00Z", 5400)
	insertRacePrediction(t, db, 1, mondayAfter(blockStart, -1)+"T06:00:00Z", 5280)
	insertRacePrediction(t, db, 1, mondayAfter(blockStart, 2)+"T06:00:00Z", 5130)

	// VO2max: same shape, so the block-start value is the last one recorded
	// before the block and the latest is the most recent overall.
	// One workout per estimate: vo2max_estimates is unique on (user, workout).
	for i, e := range []struct {
		value float64
		weeks int
	}{{50.0, -3}, {52.4, -1}, {54.1, 2}} {
		// The workouts all sit before the block so they cannot disturb any
		// week's volume; only estimated_at decides which side of the block
		// start an estimate falls on.
		w := seedWorkout(t, db, 1, mondayAfter(blockStart, -5), 150, fmt.Sprintf("order-vo2-%d", i))
		insertVO2max(t, db, 1, w, e.value, mondayAfter(blockStart, e.weeks)+"T06:00:00Z")
	}

	got := buildFitnessSignals(db, 1, block, time.Now().UTC())

	// 5130s is 1:25:30 and 5280s is 1:28:00, so the block is 2:30 to the good.
	wantHM := "- Half-marathon prediction: " + formatRaceTime(5130) +
		" (block start " + formatRaceTime(5280) + ", -" + formatRaceTime(150) + ")\n"
	if !strings.Contains(got, wantHM) {
		t.Errorf("half-marathon signal:\ngot  %q\nwant %q", got, wantHM)
	}
	if !strings.Contains(got, "- VO2max: 54.1 (block start 52.4, +1.7)\n") {
		t.Errorf("VO2max signal read the history in the wrong direction:\n%s", got)
	}
}

// The library rules are substituted by token, not by format string, so neither
// a percent sign in the block phrase nor one added to the rules prose later can
// leak %!x(MISSING) noise into a prompt sent to Claude.
func TestWorkoutLibrarySectionSubstitutesTokenNotFormat(t *testing.T) {
	library := []LibraryWorkout{{ID: 1, Name: "6x6min", WorkoutType: "threshold", MainSet: "6x6min", IsReference: true}}

	got := renderWorkoutLibrarySection(library, "the build block (within 10% of target)")
	if !strings.Contains(got, "a suitable entry exists for the build block (within 10% of target) — and VARY them") {
		t.Errorf("the block phrase was not substituted verbatim:\n%s", got)
	}
	for _, bad := range []string{"%!", libraryRulesBlockToken, "MISSING"} {
		if strings.Contains(got, bad) {
			t.Errorf("rendered library section contains %q:\n%s", bad, got)
		}
	}

	// An empty phrase still falls back to the abstract wording.
	if fallback := renderWorkoutLibrarySection(library, ""); !strings.Contains(fallback,
		"a suitable entry exists for "+libraryBlockUnknown+" — and VARY them") {
		t.Errorf("empty phrase did not fall back to the abstract wording:\n%s", fallback)
	}
}
