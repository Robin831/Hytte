package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// adjustPromptMarker appears only in the AdjustWeek prompt, and
// legacyPromptMarker only in the legacy whole-week one, so a test can say which
// of the two a call was built from without matching a whole instruction block.
const (
	adjustPromptMarker = "## Adjust Request"
	legacyPromptMarker = "Return ONLY a JSON array of day objects"
)

// stubAdjustWeek replaces the AdjustWeek seam and records the week each caller
// asked for. onCall runs in place of the real generation, so a test can make a
// plan appear the way a real one would; a nil onCall just succeeds.
func stubAdjustWeek(t *testing.T, onCall func(userID int64, weekStart string) error) *[]string {
	t.Helper()
	weeks := []string{}
	orig := adjustWeekFunc
	adjustWeekFunc = func(_ context.Context, _ *sql.DB, userID int64, weekStart string) error {
		weeks = append(weeks, weekStart)
		if onCall == nil {
			return nil
		}
		return onCall(userID, weekStart)
	}
	t.Cleanup(func() { adjustWeekFunc = orig })
	return &weeks
}

// stubPromptCapture records the prompt one Claude call was made with and
// answers it with response.
func stubPromptCapture(t *testing.T, response string) *string {
	t.Helper()
	var prompt string
	orig := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, p string) (string, error) {
		prompt = p
		return response, nil
	}
	t.Cleanup(func() { runPromptFunc = orig })
	return &prompt
}

// failIfClaudeCalled makes any Claude call fail the test, for the paths that
// must be rejected before a model is worth paying for.
func failIfClaudeCalled(t *testing.T) {
	t.Helper()
	orig := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Error("Claude was called; the request should have been rejected first")
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = orig })
}

// insertPlanForWeek writes a plan row directly, standing in for the write a real
// generation would have made.
func insertPlanForWeek(t *testing.T, db *sql.DB, userID int64, weekStart string) {
	t.Helper()
	planJSON, err := json.Marshal(buildMinimalPlan(weekStart))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, model, created_at)
		VALUES (?, ?, ?, ?, 'claude-opus-5', ?)`,
		userID, weekStart, shiftDays(weekStart, 6), string(planJSON),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert plan for %s: %v", weekStart, err)
	}
}

// countPlansForWeek returns how many plan rows exist for one athlete's week.
func countPlansForWeek(t *testing.T, db *sql.DB, userID int64, weekStart string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM stride_plans WHERE user_id = ? AND week_start = ?", userID, weekStart,
	).Scan(&n); err != nil {
		t.Fatalf("count plans for %s: %v", weekStart, err)
	}
	return n
}

// The handler owns the week=next|current → week_start mapping, and hands the
// resolved Monday to AdjustWeek. It then reads the written plan back by that
// same week_start, so both halves are asserted here: a mismatch would answer
// with a different week from the one generated.
func TestGeneratePlanHandlerRoutesResolvedWeekToAdjustWeek(t *testing.T) {
	currentStart, _ := currentWeek()
	nextStart, _ := upcomingWeek()

	tests := []struct {
		name     string
		query    string
		wantWeek string
	}{
		{"no week param defaults to next", "", nextStart},
		{"week=next", "?week=next", nextStart},
		{"week=current", "?week=current", currentStart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			weeks := stubAdjustWeek(t, func(userID int64, weekStart string) error {
				insertPlanForWeek(t, db, userID, weekStart)
				return nil
			})

			req := withUser(httptest.NewRequest("POST", "/api/stride/plans/generate"+tt.query, nil), 1)
			rec := httptest.NewRecorder()
			GeneratePlanHandler(db).ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
			}
			if len(*weeks) != 1 {
				t.Fatalf("AdjustWeek calls = %d, want 1 (%v)", len(*weeks), *weeks)
			}
			if got := (*weeks)[0]; got != tt.wantWeek {
				t.Errorf("AdjustWeek week_start = %q, want %q", got, tt.wantWeek)
			}

			var body struct {
				Plan Plan `json:"plan"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Plan.WeekStart != tt.wantWeek {
				t.Errorf("returned plan week_start = %q, want %q", body.Plan.WeekStart, tt.wantWeek)
			}
		})
	}
}

// A week value that is neither "current" nor "next" is rejected before anything
// is generated — the 400 must not cost a Claude call.
func TestGeneratePlanHandlerInvalidWeekSkipsAdjustWeek(t *testing.T) {
	db := setupTestDB(t)
	weeks := stubAdjustWeek(t, nil)

	req := withUser(httptest.NewRequest("POST", "/api/stride/plans/generate?week=bogus", nil), 1)
	rec := httptest.NewRecorder()
	GeneratePlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if len(*weeks) != 0 {
		t.Errorf("AdjustWeek calls = %d, want 0 (%v)", len(*weeks), *weeks)
	}
}

// The Monday job plans the week it just made sure the macro horizon covers: the
// upcoming Monday, not the one the run itself starts on.
func TestRunWeeklyAdjustsTheUpcomingWeek(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	stubMacroGenerate(t, nil)
	seams := stubWeeklyRunSeams(t, nil)
	insertMacroPlanSpan(t, db, 1, weekStart, MacroBlockWeeks, MacroPlanStatusActive)
	weeks := stubAdjustWeek(t, nil)

	if err := RunWeekly(context.Background(), db, 1); err != nil {
		t.Fatalf("RunWeekly: %v", err)
	}

	if len(*weeks) != 1 {
		t.Fatalf("AdjustWeek calls = %d, want 1 (%v)", len(*weeks), *weeks)
	}
	if got := (*weeks)[0]; got != weekStart {
		t.Errorf("AdjustWeek week_start = %q, want the upcoming Monday %q", got, weekStart)
	}
	// The push says what the run actually did to the week, so it is pinned to
	// the literal text rather than to the constant it is read from.
	if got := seams.onlyPush(t); got.Body != "Stride adjusted next week" {
		t.Errorf("push body = %q, want %q", got.Body, "Stride adjusted next week")
	}
}

// An athlete with no macro block — macro generation failed, or Stride was only
// just enabled — still gets a week: AdjustWeek falls back to the legacy
// whole-week prompt and writes the plan through the same store.
func TestAdjustWeekWithoutMacroWeekFallsBackToLegacy(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()

	response, err := json.Marshal(buildMinimalPlan(weekStart))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	prompt := stubPromptCapture(t, string(response))

	if err := AdjustWeek(context.Background(), db, 1, weekStart); err != nil {
		t.Fatalf("AdjustWeek: %v", err)
	}

	if strings.Contains(*prompt, adjustPromptMarker) {
		t.Error("the adjust prompt was used, but there is no macro week to adjust")
	}
	if !strings.Contains(*prompt, legacyPromptMarker) {
		t.Errorf("prompt is not the legacy whole-week one:\n%s", truncate(*prompt, 400))
	}

	var phase string
	var macroWeekID sql.NullInt64
	if err := db.QueryRow(
		"SELECT phase, macro_week_id FROM stride_plans WHERE user_id = 1 AND week_start = ?", weekStart,
	).Scan(&phase, &macroWeekID); err != nil {
		t.Fatalf("read stored plan: %v", err)
	}
	if phase != "" {
		t.Errorf("phase = %q, want empty — the legacy path has no block to copy one from", phase)
	}
	if macroWeekID.Valid {
		t.Errorf("macro_week_id = %d, want NULL", macroWeekID.Int64)
	}
}

// The block-aware path: the adjust prompt is used, the envelope's adjustment is
// stored alongside the week, the macro week is marked materialised, the notes
// the prompt was built from are consumed, and a goal proposal inside the +/-3%
// tolerance is recorded as a revision.
func TestAdjustWeekMaterialisesTheMacroWeek(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	fx := seedAdjustFixture(t, db, 1)
	enableStride(t, db, 1)

	target, err := GetMacroWeek(ctx, db, 1, fx.targetWeek)
	if err != nil {
		t.Fatalf("get macro week: %v", err)
	}

	const summary = "Cut 6 km against the spec: an overtraining flag on Tuesday and ACR at 1.4."
	// 4870s is within 3% of the fixture's REVISED target (5010s) but not of the
	// block's original one (5040s), so an accepted revision proves the clamp
	// measured from the goal the prompt actually showed the coach.
	const proposedTarget = 4870
	response, err := json.Marshal(PlanEnvelope{
		Week: buildMinimalPlan(fx.targetWeek),
		Adjustment: PlanAdjustment{
			Deviates:   true,
			Summary:    summary,
			GoalUpdate: &GoalUpdate{TargetHMTimeS: proposedTarget, Reason: "the prediction improved by 90s"},
		},
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	prompt := stubPromptCapture(t, string(response))

	if err := AdjustWeek(ctx, db, 1, fx.targetWeek); err != nil {
		t.Fatalf("AdjustWeek: %v", err)
	}

	if !strings.Contains(*prompt, adjustPromptMarker) {
		t.Errorf("prompt is not the adjust one:\n%s", truncate(*prompt, 400))
	}

	plan, err := getPlanByWeekStart(db, 1, fx.targetWeek)
	if err != nil {
		t.Fatalf("read stored plan: %v", err)
	}
	if plan.WeekEnd != fx.targetEnd {
		t.Errorf("week_end = %q, want %q", plan.WeekEnd, fx.targetEnd)
	}
	if plan.Phase != target.Phase {
		t.Errorf("phase = %q, want the macro week's %q", plan.Phase, target.Phase)
	}
	if plan.MacroWeekID == nil || *plan.MacroWeekID != target.ID {
		t.Errorf("macro_week_id = %v, want %d", plan.MacroWeekID, target.ID)
	}
	if plan.AdjustmentSummary != summary {
		t.Errorf("adjustment_summary = %q, want %q", plan.AdjustmentSummary, summary)
	}

	after, err := GetMacroWeek(ctx, db, 1, fx.targetWeek)
	if err != nil {
		t.Fatalf("re-read macro week: %v", err)
	}
	if after.Status != MacroWeekStatusMaterialised {
		t.Errorf("macro week status = %q, want %q", after.Status, MacroWeekStatusMaterialised)
	}

	notes, err := listUnconsumedNotes(ctx, db, 1)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("unconsumed notes = %d, want 0 — the prompt's notes are consumed by the same write", len(notes))
	}

	revisions, err := ListGoalRevisions(ctx, db, fx.block.ID, 1)
	if err != nil {
		t.Fatalf("list goal revisions: %v", err)
	}
	latest := revisions[len(revisions)-1]
	if latest.Goal.TargetHMTimeS != proposedTarget {
		t.Errorf("latest goal target = %ds, want %ds — the clamp must measure from the current revision, not the block's original goal",
			latest.Goal.TargetHMTimeS, proposedTarget)
	}
	if latest.Source != GoalRevisionSourceWeekly {
		t.Errorf("latest goal revision source = %q, want %q", latest.Source, GoalRevisionSourceWeekly)
	}
	if latest.WeekStart != fx.targetWeek {
		t.Errorf("latest goal revision week = %q, want %q", latest.WeekStart, fx.targetWeek)
	}
}

// The adjust prompt asked for the {week, adjustment} envelope, so a bare day
// array is a shape slip. Storing it would tell next week's coach the week was
// spec-conforming, so the whole write fails instead.
func TestAdjustWeekRejectsABareArrayFromTheAdjustPrompt(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	fx := seedAdjustFixture(t, db, 1)
	enableStride(t, db, 1)

	response, err := json.Marshal(buildMinimalPlan(fx.targetWeek))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	stubPromptCapture(t, string(response))

	err = AdjustWeek(ctx, db, 1, fx.targetWeek)
	if err == nil {
		t.Fatal("AdjustWeek accepted a bare day array under the envelope contract")
	}
	if !strings.Contains(err.Error(), "envelope") {
		t.Errorf("error = %v, want it to name the missing envelope", err)
	}
	if n := countPlansForWeek(t, db, 1, fx.targetWeek); n != 0 {
		t.Errorf("stored plans for %s = %d, want 0", fx.targetWeek, n)
	}
	after, err := GetMacroWeek(ctx, db, 1, fx.targetWeek)
	if err != nil {
		t.Fatalf("re-read macro week: %v", err)
	}
	if after.Status != MacroWeekStatusPlanned {
		t.Errorf("macro week status = %q, want it left %q", after.Status, MacroWeekStatusPlanned)
	}
}

// A non-Monday week start matches no macro week, no plan row and no weekly
// summary, so it is rejected before a Claude call rather than silently planning
// a week nothing else in Stride recognises.
func TestAdjustWeekRejectsANonMondayWeekStart(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	failIfClaudeCalled(t)

	weekStart, _ := upcomingWeek()
	tuesday := shiftDays(weekStart, 1)

	err := AdjustWeek(context.Background(), db, 1, tuesday)
	if err == nil {
		t.Fatal("AdjustWeek accepted a Tuesday week start")
	}
	if !strings.Contains(err.Error(), "Monday") {
		t.Errorf("error = %v, want it to say the week start is not a Monday", err)
	}
	if n := countPlansForWeek(t, db, 1, tuesday); n != 0 {
		t.Errorf("stored plans = %d, want 0", n)
	}
}

// Stride switched off is a configuration state, not a failure: nothing is
// generated, nothing is written, and the handler turns the missing plan into a
// 422 that names the setting.
func TestAdjustWeekWithStrideDisabledWritesNothing(t *testing.T) {
	db := extendedTestDB(t)
	failIfClaudeCalled(t)

	weekStart, _ := upcomingWeek()
	if err := AdjustWeek(context.Background(), db, 1, weekStart); err != nil {
		t.Fatalf("AdjustWeek with Stride disabled = %v, want nil", err)
	}
	if n := countPlansForWeek(t, db, 1, weekStart); n != 0 {
		t.Errorf("stored plans = %d, want 0", n)
	}
}

// A disabled Claude integration *is* returned as an error, because it names a
// setting the athlete can act on — GeneratePlanHandler answers 400 with it.
func TestAdjustWeekWithClaudeDisabledReturnsErrClaudeNotEnabled(t *testing.T) {
	db := extendedTestDB(t)
	failIfClaudeCalled(t)
	if _, err := db.Exec(
		"INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')",
	); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	weekStart, _ := upcomingWeek()
	if err := AdjustWeek(context.Background(), db, 1, weekStart); !errors.Is(err, training.ErrClaudeNotEnabled) {
		t.Fatalf("AdjustWeek = %v, want training.ErrClaudeNotEnabled", err)
	}
}
