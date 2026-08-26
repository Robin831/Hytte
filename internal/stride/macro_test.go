package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// stubMacroPrompt replaces the Claude seam with a canned answer and returns a
// pointer to the prompt the call was made with, so a test can assert on what
// the coach was actually shown.
func stubMacroPrompt(t *testing.T, response string) *string {
	t.Helper()
	var captured string
	orig := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
		captured = prompt
		return response, nil
	}
	t.Cleanup(func() { runPromptFunc = orig })
	return &captured
}

// enableMacroGeneration switches on the two gates GenerateMacroPlan checks and
// sets the weekly distance cap the validator fixture is built against.
func enableMacroGeneration(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	setPref(t, db, userID, "stride_enabled", "true")
	setPref(t, db, userID, "claude_enabled", "true")
	setPref(t, db, userID, "claude_model", "claude-opus-4-6")
	setPref(t, db, userID, "stride_weekly_distance_cap", "90")
}

// rebaseMacroPlanFixture moves a validMacroPlan fixture — the plan, its
// mesocycles and the race calendar it was planned against — onto a different
// block start, so the same known-good block can be generated for any Monday.
func rebaseMacroPlanFixture(t *testing.T, plan *MacroPlanResponse, in *MacroValidationContext, newStart string) {
	t.Helper()

	from, err := parseMondayWeek(in.StartWeek)
	if err != nil {
		t.Fatalf("parse fixture start week: %v", err)
	}
	to, err := parseMondayWeek(newStart)
	if err != nil {
		t.Fatalf("parse new start week: %v", err)
	}
	shift := func(date string) string {
		d, err := parseWeekDate(date)
		if err != nil {
			t.Fatalf("parse fixture date %q: %v", date, err)
		}
		return d.Add(to.Sub(from)).Format(dateLayout)
	}

	for i := range plan.Weeks {
		plan.Weeks[i].WeekStart = shift(plan.Weeks[i].WeekStart)
	}
	for i := range plan.Mesocycles {
		plan.Mesocycles[i].StartWeek = shift(plan.Mesocycles[i].StartWeek)
	}
	for i := range in.Races {
		in.Races[i].Date = shift(in.Races[i].Date)
	}
	in.StartWeek = newStart
}

// seedMacroFixtureRaces writes the fixture's race calendar with the exact ids
// the plan pins to its weeks, so verifyMacroReferences sees races the user owns
// and validateMacroRaceWeekIDs sees them in the weeks that claim them.
func seedMacroFixtureRaces(t *testing.T, db *sql.DB, userID int64, races []Race) {
	t.Helper()
	for _, r := range races {
		if _, err := db.Exec(`
			INSERT INTO stride_races (id, user_id, name, date, distance_m, priority, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.ID, userID, r.Name, r.Date, r.DistanceM, r.Priority, "2026-01-01T00:00:00Z",
		); err != nil {
			t.Fatalf("insert race %d: %v", r.ID, err)
		}
	}
}

// macroFixtureJSON renders a fixture plan the way the coach would return it.
func macroFixtureJSON(t *testing.T, plan *MacroPlanResponse) string {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal fixture plan: %v", err)
	}
	return string(raw)
}

// macroFixtureJSONIndented renders a fixture plan across many lines, the way a
// model that pretty-prints its answer would. Fence handling is line-based, so a
// one-line answer would not exercise it.
func macroFixtureJSONIndented(t *testing.T, plan *MacroPlanResponse) string {
	t.Helper()
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture plan: %v", err)
	}
	return string(raw)
}

// setupMacroGeneration builds a database with the fixture block's calendar in
// place, ready for a GenerateMacroPlan call starting at startWeek.
func setupMacroGeneration(t *testing.T, startWeek string) (*sql.DB, *MacroPlanResponse, MacroValidationContext) {
	t.Helper()
	db := setupTestDB(t)
	enableMacroGeneration(t, db, 1)

	plan, in := validMacroPlan(t)
	rebaseMacroPlanFixture(t, plan, &in, startWeek)
	seedMacroFixtureRaces(t, db, 1, in.Races)
	return db, plan, in
}

// countMacroRows reports how many plan, week and goal-revision rows the user
// has. It is the "nothing was written" assertion for the rejection paths.
func countMacroRows(t *testing.T, db *sql.DB, userID int64) (plans, weeks, revisions int) {
	t.Helper()
	for _, q := range []struct {
		table string
		into  *int
	}{
		{"stride_macro_plans", &plans},
		{"stride_macro_weeks", &weeks},
		{"stride_goal_revisions", &revisions},
	} {
		if err := db.QueryRow("SELECT COUNT(*) FROM "+q.table+" WHERE user_id = ?", userID).Scan(q.into); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
	}
	return plans, weeks, revisions
}

func TestGenerateMacroPlanPersistsValidPlan(t *testing.T) {
	ctx := context.Background()
	start := macroTestStartWeek
	db, fixture, in := setupMacroGeneration(t, start)

	prompt := stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, start, MacroModeScheduled)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}

	// The prompt is the instruction block paired with the athlete inputs.
	if !strings.Contains(*prompt, macroInstructions) {
		t.Error("prompt does not carry the macro instruction block")
	}
	if !strings.Contains(*prompt, "Mode: scheduled") {
		t.Error("prompt does not state the generation mode")
	}

	if plan.ID == 0 {
		t.Fatal("returned plan has no id")
	}
	if plan.StartWeek != start {
		t.Errorf("start_week = %q, want %q", plan.StartWeek, start)
	}
	if want := mondayAfter(start, MacroBlockWeeks-1); plan.EndWeek != want {
		t.Errorf("end_week = %q, want %q", plan.EndWeek, want)
	}
	if plan.Status != MacroPlanStatusActive {
		t.Errorf("status = %q, want active", plan.Status)
	}
	if plan.GeneratedBy != MacroGeneratedByScheduled {
		t.Errorf("generated_by = %q, want scheduled", plan.GeneratedBy)
	}
	if plan.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want claude-opus-4-6", plan.Model)
	}
	if plan.PreviousPlanID != nil {
		t.Errorf("previous_plan_id = %d, want nil for a first block", *plan.PreviousPlanID)
	}
	if plan.Goal.TargetHMTimeS != fixture.Goal.TargetHMTimeS {
		t.Errorf("goal target_hm_time_s = %d, want %d", plan.Goal.TargetHMTimeS, fixture.Goal.TargetHMTimeS)
	}
	if len(plan.Periodisation) != len(fixture.Mesocycles) {
		t.Fatalf("periodisation has %d mesocycles, want %d", len(plan.Periodisation), len(fixture.Mesocycles))
	}
	if len(plan.Weeks) != MacroBlockWeeks {
		t.Fatalf("plan has %d weeks, want %d", len(plan.Weeks), MacroBlockWeeks)
	}

	// The rows, read back through the store so the encrypted blobs are
	// exercised in both directions.
	stored, err := GetActiveMacroPlan(ctx, db, 1, start)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if stored == nil || stored.ID != plan.ID {
		t.Fatalf("active plan = %+v, want the generated one", stored)
	}
	if stored.Prompt != *prompt {
		t.Error("stored prompt does not match the prompt the model was given")
	}
	if stored.Response != macroFixtureJSON(t, fixture) {
		t.Error("stored response does not match the model's answer")
	}
	if len(stored.Weeks) != MacroBlockWeeks {
		t.Fatalf("stored plan has %d weeks, want %d", len(stored.Weeks), MacroBlockWeeks)
	}
	for i, w := range stored.Weeks {
		want := fixture.Weeks[i]
		if w.WeekStart != want.WeekStart || w.Seq != want.Seq {
			t.Fatalf("week %d = (%s, seq %d), want (%s, seq %d)", i, w.WeekStart, w.Seq, want.WeekStart, want.Seq)
		}
		if w.Phase != want.Phase || w.LoadLevel != want.LoadLevel || w.Mesocycle != want.Mesocycle {
			t.Errorf("week %d = (%s, %s, %s), want (%s, %s, %s)",
				i, w.Phase, w.LoadLevel, w.Mesocycle, want.Phase, want.LoadLevel, want.Mesocycle)
		}
		if w.TargetKm != want.TargetKm || w.TargetSessions != want.TargetSessions {
			t.Errorf("week %d volume = (%.1f km, %d sessions), want (%.1f km, %d)",
				i, w.TargetKm, w.TargetSessions, want.TargetKm, want.TargetSessions)
		}
		if w.Intent != want.Intent {
			t.Errorf("week %d intent = %q, want %q", i, w.Intent, want.Intent)
		}
		if len(w.KeySessions) != len(want.KeySessions) {
			t.Errorf("week %d has %d key sessions, want %d", i, len(w.KeySessions), len(want.KeySessions))
		}
		if w.Status != MacroWeekStatusPlanned {
			t.Errorf("week %d status = %q, want planned", i, w.Status)
		}
		switch {
		case want.RaceID == nil && w.RaceID != nil:
			t.Errorf("week %d race_id = %d, want nil", i, *w.RaceID)
		case want.RaceID != nil && (w.RaceID == nil || *w.RaceID != *want.RaceID):
			t.Errorf("week %d race_id = %v, want %d", i, w.RaceID, *want.RaceID)
		}
	}

	// Exactly one goal revision, the block's 'initial' one.
	revisions, err := ListGoalRevisions(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("goal revisions = %d, want 1", len(revisions))
	}
	rev := revisions[0]
	if rev.Source != GoalRevisionSourceInitial {
		t.Errorf("goal revision source = %q, want initial", rev.Source)
	}
	if rev.WeekStart != start {
		t.Errorf("goal revision week_start = %q, want %q", rev.WeekStart, start)
	}
	if rev.Goal.TargetHMTimeS != fixture.Goal.TargetHMTimeS {
		t.Errorf("goal revision target_hm_time_s = %d, want %d", rev.Goal.TargetHMTimeS, fixture.Goal.TargetHMTimeS)
	}
	if rev.Goal.AnchorRaceID == nil || *rev.Goal.AnchorRaceID != in.Races[0].ID {
		t.Errorf("goal revision anchor_race_id = %v, want %d", rev.Goal.AnchorRaceID, in.Races[0].ID)
	}
	if !strings.Contains(rev.Reason, start) {
		t.Errorf("goal revision reason = %q, want it to name the block start", rev.Reason)
	}
}

// TestGenerateMacroPlanRejectsInvalidPlans walks one mutation per validator
// rule. Every one must surface as an error from GenerateMacroPlan and leave the
// database exactly as it was — validation runs before CreateMacroPlan's
// transaction, so a rejected block writes no plan, no weeks and no goal
// revision.
func TestGenerateMacroPlanRejectsInvalidPlans(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(plan *MacroPlanResponse)
		want   string
	}{
		{
			name:   "wrong number of weeks",
			mutate: func(p *MacroPlanResponse) { p.Weeks = p.Weeks[:MacroBlockWeeks-1] },
			want:   "expected exactly 26 weeks",
		},
		{
			name:   "week does not start on a Monday",
			mutate: func(p *MacroPlanResponse) { p.Weeks[3].WeekStart = macroTestWeekDate(3, 1) },
			want:   "every week must start on a Monday",
		},
		{
			name:   "week_start is not a date",
			mutate: func(p *MacroPlanResponse) { p.Weeks[2].WeekStart = "week three" },
			want:   `week_start "week three" is not a YYYY-MM-DD date`,
		},
		{
			name:   "weeks are not contiguous",
			mutate: func(p *MacroPlanResponse) { p.Weeks[10].WeekStart = mondayAfter(macroTestStartWeek, 11) },
			want:   "contiguous Mondays",
		},
		{
			name:   "seq out of order",
			mutate: func(p *MacroPlanResponse) { p.Weeks[7].Seq = 9 },
			want:   "seq is 9, expected 8",
		},
		{
			name:   "unknown phase",
			mutate: func(p *MacroPlanResponse) { p.Weeks[2].Phase = "sharpening" },
			want:   `phase "sharpening" is not one of`,
		},
		{
			name:   "unknown load level",
			mutate: func(p *MacroPlanResponse) { p.Weeks[2].LoadLevel = "hard" },
			want:   `load_level "hard" is not one of`,
		},
		{
			name:   "week names a mesocycle that does not exist",
			mutate: func(p *MacroPlanResponse) { p.Weeks[5].Mesocycle = "Base 9" },
			want:   "is not one of the returned mesocycles",
		},
		{
			name:   "mesocycles leave a gap",
			mutate: func(p *MacroPlanResponse) { p.Mesocycles[0].Weeks = 3 },
			want:   "not covered by any mesocycle",
		},
		{
			name:   "week exceeds the weekly distance cap",
			mutate: func(p *MacroPlanResponse) { p.Weeks[18].TargetKm = 95 },
			want:   "exceeds the athlete's weekly distance cap",
		},
		{
			name:   "volume ramps more than 10 percent",
			mutate: func(p *MacroPlanResponse) { p.Weeks[1].TargetKm = 70 },
			want:   "more than +10%",
		},
		{
			name:   "A race week is not a race week",
			mutate: func(p *MacroPlanResponse) { p.Weeks[MacroBlockWeeks-1].Phase = MacroPhasePeak },
			want:   `expected "race"`,
		},
		{
			name:   "the week before an A race does not taper",
			mutate: func(p *MacroPlanResponse) { p.Weeks[MacroBlockWeeks-2].Phase = MacroPhaseBuild },
			want:   `expected "taper"`,
		},
		{
			name:   "a 10 km race gets a taper week",
			mutate: func(p *MacroPlanResponse) { p.Weeks[17].Phase = MacroPhaseTaper },
			want:   "never get a taper week",
		},
		{
			name:   "race week does not name its race",
			mutate: func(p *MacroPlanResponse) { p.Weeks[17].RaceID = nil },
			want:   "the week contains",
		},
		{
			name: "week names a race the block does not contain",
			mutate: func(p *MacroPlanResponse) {
				stranger := int64(99)
				p.Weeks[4].RaceID = &stranger
			},
			want: "no week of this block contains race 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
			tt.mutate(fixture)
			stubMacroPrompt(t, macroFixtureJSON(t, fixture))

			plan, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeScheduled)
			if err == nil {
				t.Fatal("expected the invalid plan to be rejected")
			}
			if plan != nil {
				t.Error("a rejected plan must not be returned")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if plans, weeks, revisions := countMacroRows(t, db, 1); plans+weeks+revisions != 0 {
				t.Errorf("rejected plan wrote rows: %d plans, %d weeks, %d revisions", plans, weeks, revisions)
			}
		})
	}
}

func TestGenerateMacroPlanExtensionModeSeedsFromPreviousBlock(t *testing.T) {
	ctx := context.Background()

	// The block being continued: the store fixture, which starts at
	// testBlockStart and runs the full 26 weeks.
	db := setupTestDB(t)
	enableMacroGeneration(t, db, 1)
	previous, previousWeeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, previous, previousWeeks, "Initial"); err != nil {
		t.Fatalf("create previous block: %v", err)
	}

	// The extension starts the week after the previous block's last week.
	start := mondayAfter(previous.EndWeek, 1)
	fixture, in := validMacroPlan(t)
	rebaseMacroPlanFixture(t, fixture, &in, start)
	seedMacroFixtureRaces(t, db, 1, in.Races)
	prompt := stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, start, MacroModeExtension)
	if err != nil {
		t.Fatalf("GenerateMacroPlan(extension): %v", err)
	}

	// The prompt carries the block being continued.
	if !strings.Contains(*prompt, "## Previous Block") {
		t.Error("extension prompt does not embed the previous block")
	}
	if !strings.Contains(*prompt, "Mode: extension") {
		t.Error("extension prompt does not state the mode")
	}
	if !strings.Contains(*prompt, previous.Goal.Statement) {
		t.Error("extension prompt does not carry the previous goal")
	}
	if !strings.Contains(*prompt, "threshold density") {
		t.Error("extension prompt does not carry the previous periodisation")
	}
	if !strings.Contains(*prompt, previous.StartWeek+" to "+previous.EndWeek) {
		t.Error("extension prompt does not state the horizon being continued")
	}

	// The new block picks up where the old one ended and records the lineage.
	if plan.GeneratedBy != MacroGeneratedByExtension {
		t.Errorf("generated_by = %q, want extension", plan.GeneratedBy)
	}
	if plan.PreviousPlanID == nil || *plan.PreviousPlanID != previous.ID {
		t.Fatalf("previous_plan_id = %v, want %d", plan.PreviousPlanID, previous.ID)
	}
	if plan.StartWeek != mondayAfter(previous.EndWeek, 1) {
		t.Errorf("start_week = %q, want the week after the previous block's %q", plan.StartWeek, previous.EndWeek)
	}
	if plan.Weeks[0].Seq != 1 || plan.Weeks[MacroBlockWeeks-1].Seq != MacroBlockWeeks {
		t.Error("extension block is not numbered 1..26 within itself")
	}
	if plan.Weeks[0].WeekStart != plan.StartWeek {
		t.Errorf("first week starts %q, want %q", plan.Weeks[0].WeekStart, plan.StartWeek)
	}

	// The block it continues is retired in the same write.
	retired, err := GetMacroPlanByID(ctx, db, previous.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if retired.Status != MacroPlanStatusSuperseded {
		t.Errorf("previous block status = %q, want superseded", retired.Status)
	}
}

func TestGenerateMacroPlanExtensionWithoutPreviousBlock(t *testing.T) {
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
	called := false
	orig := runPromptFunc
	runPromptFunc = func(context.Context, *training.ClaudeConfig, string) (string, error) {
		called = true
		return macroFixtureJSON(t, fixture), nil
	}
	t.Cleanup(func() { runPromptFunc = orig })

	if _, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeExtension); !errors.Is(err, ErrNoPreviousMacroPlan) {
		t.Fatalf("error = %v, want ErrNoPreviousMacroPlan", err)
	}
	if called {
		t.Error("an extension with nothing to continue must fail before the Claude call")
	}
}

// TestGenerateMacroPlanNoRaceBlock covers the standing half-marathon rule's
// other half: with an empty race calendar the block is still a half-marathon
// development block with a concrete target time, judged against the race
// prediction model, and it ends without a race week or a time trial.
func TestGenerateMacroPlanNoRaceBlock(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	enableMacroGeneration(t, db, 1)

	fixture, in := validMacroPlan(t)
	rebaseMacroPlanFixture(t, fixture, &in, macroTestStartWeek)

	// Strip the calendar out of the answer: no anchor race, no race ids, no
	// race week. The final week stays a taper week of its own mesocycle.
	fixture.Goal.AnchorRaceID = nil
	fixture.Goal.Benchmark = "3 x 3 km at threshold, 90 s float, in week 24"
	fixture.Goal.Statement = "Run 1:22:00 for the half marathon on current form"
	fixture.Goal.Rationale = "The race prediction model has the athlete at 1:25; 1:22 is reachable over 26 weeks."
	for i := range fixture.Weeks {
		fixture.Weeks[i].RaceID = nil
		if fixture.Weeks[i].Phase == MacroPhaseRace {
			fixture.Weeks[i].Phase = MacroPhaseTaper
		}
	}
	// No races are seeded, so the calendar the validator sees is empty too.

	stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeManual)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}

	if plan.GeneratedBy != MacroGeneratedByManual {
		t.Errorf("generated_by = %q, want manual", plan.GeneratedBy)
	}
	if plan.Goal.AnchorRaceID != nil {
		t.Errorf("anchor_race_id = %d, want nil for a block with no race", *plan.Goal.AnchorRaceID)
	}
	if plan.Goal.TargetHMTimeS != fixture.Goal.TargetHMTimeS {
		t.Errorf("target_hm_time_s = %d, want %d", plan.Goal.TargetHMTimeS, fixture.Goal.TargetHMTimeS)
	}
	for _, w := range plan.Weeks {
		if w.Phase == MacroPhaseRace {
			t.Errorf("week %s is a race week, but the block has no race", w.WeekStart)
		}
		if w.RaceID != nil {
			t.Errorf("week %s pins race %d, but the block has no race", w.WeekStart, *w.RaceID)
		}
	}

	// The goal history records the target time, so a later revision can be
	// compared against what the block set out to do.
	revisions, err := ListGoalRevisions(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("goal revisions = %d, want 1", len(revisions))
	}
	if revisions[0].Goal.TargetHMTimeS != fixture.Goal.TargetHMTimeS {
		t.Errorf("goal revision target_hm_time_s = %d, want %d",
			revisions[0].Goal.TargetHMTimeS, fixture.Goal.TargetHMTimeS)
	}
	if revisions[0].Goal.AnchorRaceID != nil {
		t.Errorf("goal revision anchor_race_id = %d, want nil", *revisions[0].Goal.AnchorRaceID)
	}
	if revisions[0].Goal.Benchmark != fixture.Goal.Benchmark {
		t.Errorf("goal revision benchmark = %q, want %q", revisions[0].Goal.Benchmark, fixture.Goal.Benchmark)
	}
}

func TestGenerateMacroPlanRejectsUnknownMode(t *testing.T) {
	db := setupTestDB(t)
	enableMacroGeneration(t, db, 1)

	_, err := GenerateMacroPlan(context.Background(), db, 1, time.Now().UTC().Format(dateLayout), MacroMode("initial"))
	if err == nil || !strings.Contains(err.Error(), `invalid macro mode "initial"`) {
		t.Fatalf("error = %v, want an invalid-mode error", err)
	}
}

func TestGenerateMacroPlanSnapsStartWeekToMonday(t *testing.T) {
	ctx := context.Background()
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
	prompt := stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	// A Thursday inside the fixture's first week: the block must still start on
	// that week's Monday rather than being rejected.
	monday, err := parseMondayWeek(macroTestStartWeek)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	plan, err := GenerateMacroPlan(ctx, db, 1, monday.AddDate(0, 0, 3).Format(dateLayout), MacroModeScheduled)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}
	if plan.StartWeek != macroTestStartWeek {
		t.Errorf("start_week = %q, want %q", plan.StartWeek, macroTestStartWeek)
	}
	if !strings.Contains(*prompt, "Block start week (Monday): "+macroTestStartWeek) {
		t.Error("prompt does not ask for the snapped start week")
	}
}

func TestGenerateMacroPlanMalformedResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{"not JSON at all", "Sorry, I cannot plan that.", "unmarshal macro plan JSON"},
		{"truncated JSON", `{"goal":{"primary_focus":"half`, "unmarshal macro plan JSON"},
		{"an array instead of an object", `[{"week_start":"2026-01-05"}]`, "unmarshal macro plan JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _, _ := setupMacroGeneration(t, macroTestStartWeek)
			stubMacroPrompt(t, tt.response)

			if _, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeScheduled); err == nil {
				t.Fatal("expected a parse error")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if plans, weeks, revisions := countMacroRows(t, db, 1); plans+weeks+revisions != 0 {
				t.Errorf("malformed response wrote rows: %d plans, %d weeks, %d revisions", plans, weeks, revisions)
			}
		})
	}
}

// A fenced answer is off-contract but not wrong, so it is unwrapped rather than
// rejected — the same tolerance parsePlanResponse gives the weekly plan. The
// half-fenced forms matter as much as the well-formed one: an answer that opens
// a fence and is cut off before closing it still ends in real JSON, and
// dropping its last line would discard the closing brace.
func TestGenerateMacroPlanStripsCodeFences(t *testing.T) {
	tests := []struct {
		name      string
		wrap      func(body string) string
		multiline bool
	}{
		{name: "tagged fence", wrap: func(b string) string { return "```json\n" + b + "\n```" }, multiline: true},
		{name: "bare fence", wrap: func(b string) string { return "```\n" + b + "\n```" }, multiline: true},
		// A truncated answer: the fence opens, the JSON runs to the end and no
		// closing fence ever arrives. Its last line is content, not a fence.
		{name: "opening fence only", wrap: func(b string) string { return "```json\n" + b }, multiline: true},
		{name: "single line", wrap: func(b string) string { return "```json " + b + "```" }},
		{name: "trailing whitespace", wrap: func(b string) string { return "\n```json\n" + b + "\n```\n\n" }, multiline: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
			body := macroFixtureJSON(t, fixture)
			if tt.multiline {
				body = macroFixtureJSONIndented(t, fixture)
			}
			stubMacroPrompt(t, tt.wrap(body))

			plan, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeScheduled)
			if err != nil {
				t.Fatalf("GenerateMacroPlan: %v", err)
			}
			if len(plan.Weeks) != MacroBlockWeeks {
				t.Fatalf("plan has %d weeks, want %d", len(plan.Weeks), MacroBlockWeeks)
			}
		})
	}
}

// The model Stride plans a block on is the athlete's when they have chosen one,
// and strideDefaultModel when they have not. The fallback cannot be written as
// `if cfg.Model == ""`: training.LoadClaudeConfig substitutes its own,
// cheaper package default first, so the branch would never fire.
func TestGenerateMacroPlanModelSelection(t *testing.T) {
	tests := []struct {
		name string
		pref string
		want string
	}{
		{"no model chosen falls back to the Stride default", "", strideDefaultModel},
		{"the athlete's choice is respected", "claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
			// setupMacroGeneration pins a model; this test is about what
			// happens without one, so clear it first.
			setPref(t, db, 1, "claude_model", tt.pref)

			var usedModel string
			orig := runPromptFunc
			runPromptFunc = func(_ context.Context, cfg *training.ClaudeConfig, _ string) (string, error) {
				usedModel = cfg.Model
				return macroFixtureJSON(t, fixture), nil
			}
			t.Cleanup(func() { runPromptFunc = orig })

			plan, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeScheduled)
			if err != nil {
				t.Fatalf("GenerateMacroPlan: %v", err)
			}
			if usedModel != tt.want {
				t.Errorf("block generated on model %q, want %q", usedModel, tt.want)
			}
			if plan.Model != tt.want {
				t.Errorf("persisted model = %q, want %q", plan.Model, tt.want)
			}
		})
	}
}

// The prompt carries the athlete's history, race calendar and goal, and the
// response carries the block built from them — the same class of data CLAUDE.md
// requires the analysis feature to encrypt. CreateMacroPlan encrypts both; this
// pins that a generated block never lands in SQLite as plaintext.
func TestGenerateMacroPlanStoresPromptAndResponseEncrypted(t *testing.T) {
	ctx := context.Background()
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
	prompt := stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeScheduled)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}

	var storedPrompt, storedResponse, storedGoal string
	if err := db.QueryRow(`SELECT prompt, response, goal_json FROM stride_macro_plans WHERE id = ?`, plan.ID).
		Scan(&storedPrompt, &storedResponse, &storedGoal); err != nil {
		t.Fatalf("read raw plan row: %v", err)
	}
	for name, raw := range map[string]string{
		"prompt":    storedPrompt,
		"response":  storedResponse,
		"goal_json": storedGoal,
	} {
		if raw == "" {
			t.Errorf("%s is empty", name)
		}
		if strings.Contains(raw, "Planning Request") || strings.Contains(raw, "target_hm_time_s") ||
			strings.Contains(raw, fixture.Goal.Statement) {
			t.Errorf("%s stored as plaintext: %q", name, raw)
		}
	}

	// And it decrypts back to exactly what the model was shown and answered.
	stored, err := GetMacroPlanByID(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if stored.Prompt != *prompt {
		t.Error("decrypted prompt does not match the prompt the model was given")
	}
	if stored.Response != macroFixtureJSON(t, fixture) {
		t.Error("decrypted response does not match the model's answer")
	}
}

// rivalMacroBlock builds an unsaved 26-week block starting at start, standing
// in for a block a second generation commits while the first is still waiting
// on Claude.
func rivalMacroBlock(t *testing.T, userID int64, start string) (*MacroPlan, []MacroWeek) {
	t.Helper()
	from, err := parseWeekDate(testBlockStart)
	if err != nil {
		t.Fatalf("parse fixture start: %v", err)
	}
	to, err := parseWeekDate(start)
	if err != nil {
		t.Fatalf("parse rival start: %v", err)
	}
	shift := func(date string) string {
		d, err := parseWeekDate(date)
		if err != nil {
			t.Fatalf("parse fixture date %q: %v", date, err)
		}
		return d.Add(to.Sub(from)).Format(dateLayout)
	}

	plan, weeks := sampleMacroPlan(userID)
	plan.StartWeek = shift(plan.StartWeek)
	plan.EndWeek = shift(plan.EndWeek)
	for i := range plan.Periodisation {
		plan.Periodisation[i].StartWeek = shift(plan.Periodisation[i].StartWeek)
	}
	for i := range weeks {
		weeks[i].WeekStart = shift(weeks[i].WeekStart)
	}
	return plan, weeks
}

// The block a generation follows is resolved before a Claude call that can take
// five minutes, so by the time the answer comes back another generation may
// have replaced it. Persisting anyway would leave the athlete with two active
// blocks prescribing different things for the same weeks — CreateMacroPlan
// re-checks inside its own transaction and rejects the late writer instead.
func TestGenerateMacroPlanRejectsBlockOvertakenByAConcurrentGeneration(t *testing.T) {
	ctx := context.Background()
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)

	// The rival starts a week later, so it overlaps this block's horizon
	// without colliding on the (user_id, start_week) unique index — the index
	// alone would not catch it.
	rivalStart := mondayAfter(macroTestStartWeek, 1)
	orig := runPromptFunc
	runPromptFunc = func(context.Context, *training.ClaudeConfig, string) (string, error) {
		rival, rivalWeeks := rivalMacroBlock(t, 1, rivalStart)
		if err := CreateMacroPlan(ctx, db, rival, rivalWeeks, "Racing block"); err != nil {
			t.Fatalf("create racing block: %v", err)
		}
		return macroFixtureJSON(t, fixture), nil
	}
	t.Cleanup(func() { runPromptFunc = orig })

	plan, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeScheduled)
	if !errors.Is(err, ErrOverlappingMacroPlan) {
		t.Fatalf("error = %v, want ErrOverlappingMacroPlan", err)
	}
	if plan != nil {
		t.Error("a rejected plan must not be returned")
	}

	// The rival is the one active block, and the losing generation left no
	// half-written rows behind.
	active, err := GetActiveMacroPlan(ctx, db, 1, rivalStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if active == nil || active.StartWeek != rivalStart {
		t.Fatalf("active plan = %+v, want the block that won the race", active)
	}
	plans, weeks, revisions := countMacroRows(t, db, 1)
	if plans != 1 || weeks != MacroBlockWeeks || revisions != 1 {
		t.Errorf("rows after the rejected write: %d plans, %d weeks, %d revisions", plans, weeks, revisions)
	}
}

func TestGenerateMacroPlanRequiresStrideAndClaude(t *testing.T) {
	t.Run("stride disabled", func(t *testing.T) {
		db := setupTestDB(t)
		setPref(t, db, 1, "claude_enabled", "true")
		if _, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeScheduled); !errors.Is(err, ErrStrideNotEnabled) {
			t.Fatalf("error = %v, want ErrStrideNotEnabled", err)
		}
	})

	t.Run("claude disabled", func(t *testing.T) {
		db := setupTestDB(t)
		setPref(t, db, 1, "stride_enabled", "true")
		if _, err := GenerateMacroPlan(context.Background(), db, 1, macroTestStartWeek, MacroModeScheduled); !errors.Is(err, training.ErrClaudeNotEnabled) {
			t.Fatalf("error = %v, want ErrClaudeNotEnabled", err)
		}
	})
}

// A regeneration of the same horizon retires the block it replaces in the same
// write, so the athlete is never left with two active blocks on one Monday.
func TestGenerateMacroPlanManualModeReplacesActiveBlock(t *testing.T) {
	ctx := context.Background()
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)
	stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	first, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeScheduled)
	if err != nil {
		t.Fatalf("first GenerateMacroPlan: %v", err)
	}

	second, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeManual)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if second.PreviousPlanID == nil || *second.PreviousPlanID != first.ID {
		t.Fatalf("previous_plan_id = %v, want %d", second.PreviousPlanID, first.ID)
	}
	if second.GeneratedBy != MacroGeneratedByManual {
		t.Errorf("generated_by = %q, want manual", second.GeneratedBy)
	}

	retired, err := GetMacroPlanByID(ctx, db, first.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if retired.Status != MacroPlanStatusSuperseded {
		t.Errorf("replaced block status = %q, want superseded", retired.Status)
	}

	active, err := GetActiveMacroPlan(ctx, db, 1, macroTestStartWeek)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if active == nil || active.ID != second.ID {
		t.Fatalf("active plan = %+v, want the regenerated one", active)
	}
}

// An active block that starts *after* the new one but inside its 26-week
// horizon is nobody's lineage parent — resolving only the block covering the
// start week would never name it, so it would never be demoted and the overlap
// check would reject every generation after the (up to 300s) Claude call. It is
// a reachable shape, not just a race: a scheduled extension starts at the
// running block's EndWeek+1w and retires that block on the way in, leaving the
// athlete's only active block starting in the future.
func TestGenerateMacroPlanReplacesBlockStartingInsideTheHorizon(t *testing.T) {
	ctx := context.Background()
	db, fixture, _ := setupMacroGeneration(t, macroTestStartWeek)

	aheadStart := mondayAfter(macroTestStartWeek, 4)
	ahead, aheadWeeks := rivalMacroBlock(t, 1, aheadStart)
	if err := CreateMacroPlan(ctx, db, ahead, aheadWeeks, "Scheduled ahead"); err != nil {
		t.Fatalf("create the block starting inside the horizon: %v", err)
	}

	stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, macroTestStartWeek, MacroModeManual)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}
	if plan.PreviousPlanID == nil || *plan.PreviousPlanID != ahead.ID {
		t.Errorf("previous_plan_id = %v, want the block it replaced (%d)", plan.PreviousPlanID, ahead.ID)
	}

	retired, err := GetMacroPlanByID(ctx, db, ahead.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if retired.Status != MacroPlanStatusSuperseded {
		t.Errorf("replaced block status = %q, want superseded", retired.Status)
	}

	// The new block is the athlete's only plan for both its own start week and
	// the week the retired block used to start.
	for _, week := range []string{macroTestStartWeek, aheadStart} {
		active, err := GetActiveMacroPlan(ctx, db, 1, week)
		if err != nil {
			t.Fatalf("GetActiveMacroPlan(%s): %v", week, err)
		}
		if active == nil || active.ID != plan.ID {
			t.Fatalf("active plan for %s = %+v, want the generated one (%d)", week, active, plan.ID)
		}
	}
}

// A horizon can overlap more than one active block — an athlete with a block
// running now and another already planned for after it has two. Every one of
// them is retired by the write, not just the lineage parent, otherwise the
// leftover trips the overlap check the block was meant to resolve.
func TestGenerateMacroPlanRetiresEveryOverlappingBlock(t *testing.T) {
	ctx := context.Background()
	start := mondayAfter(macroTestStartWeek, 10)
	db, fixture, _ := setupMacroGeneration(t, start)

	// Two active blocks laid end to end: the first covers the new block's
	// opening weeks, the second its closing ones.
	running, runningWeeks := rivalMacroBlock(t, 1, macroTestStartWeek)
	if err := CreateMacroPlan(ctx, db, running, runningWeeks, "Running block"); err != nil {
		t.Fatalf("create the running block: %v", err)
	}
	next, nextWeeks := rivalMacroBlock(t, 1, mondayAfter(macroTestStartWeek, MacroBlockWeeks))
	if err := CreateMacroPlan(ctx, db, next, nextWeeks, "Block after it"); err != nil {
		t.Fatalf("create the block after it: %v", err)
	}

	stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	plan, err := GenerateMacroPlan(ctx, db, 1, start, MacroModeManual)
	if err != nil {
		t.Fatalf("GenerateMacroPlan: %v", err)
	}
	// The lineage parent is the block the new one starts inside.
	if plan.PreviousPlanID == nil || *plan.PreviousPlanID != running.ID {
		t.Errorf("previous_plan_id = %v, want the running block (%d)", plan.PreviousPlanID, running.ID)
	}

	for _, replaced := range []*MacroPlan{running, next} {
		retired, err := GetMacroPlanByID(ctx, db, replaced.ID, 1)
		if err != nil {
			t.Fatalf("GetMacroPlanByID(%d): %v", replaced.ID, err)
		}
		if retired.Status != MacroPlanStatusSuperseded {
			t.Errorf("block %d (%s) status = %q, want superseded", replaced.ID, replaced.StartWeek, retired.Status)
		}
	}

	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_macro_plans WHERE user_id = 1 AND status = ?`,
		MacroPlanStatusActive).Scan(&active); err != nil {
		t.Fatalf("count active plans: %v", err)
	}
	if active != 1 {
		t.Errorf("active plans = %d, want only the generated one", active)
	}
}

func TestParseWeeklyDistanceCap(t *testing.T) {
	tests := []struct {
		raw  string
		want float64
	}{
		{"", 0},
		{"70", 70},
		{" 82.5 ", 82.5},
		{"seventy", 0},
		{"-10", 0},
	}
	for _, tt := range tests {
		if got := parseWeeklyDistanceCap(tt.raw); got != tt.want {
			t.Errorf("parseWeeklyDistanceCap(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
