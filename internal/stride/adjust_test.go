package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

// envelopeTestWeekStart is the Monday every canned week in this file covers.
const (
	envelopeTestWeekStart = "2026-04-06"
	envelopeTestWeekEnd   = "2026-04-12"
)

// cannedWeekJSON renders the 7 day objects of envelopeTestWeekStart's week as
// the JSON array both response contracts carry: six training days and a Sunday
// rest day, with a library id on the Monday session.
func cannedWeekJSON(t *testing.T) string {
	t.Helper()

	days := make([]DayPlan, 0, 7)
	for i := 0; i < 7; i++ {
		date := addDays(t, envelopeTestWeekStart, i)
		if i == 6 {
			days = append(days, DayPlan{Date: date, RestDay: true})
			continue
		}
		session := &Session{
			Warmup:      "15 min easy jog",
			MainSet:     "6x6min at 4:28-4:32/km (13.2-13.4 km/h)",
			Cooldown:    "10 min easy jog",
			TargetHRCap: 165,
			Description: "Threshold intervals.",
		}
		if i == 0 {
			session.LibraryID = 3
		}
		days = append(days, DayPlan{Date: date, Session: session})
	}

	raw, err := json.Marshal(days)
	if err != nil {
		t.Fatalf("marshal canned week: %v", err)
	}
	return string(raw)
}

// addDays offsets a YYYY-MM-DD date by n days.
func addDays(t *testing.T, date string, n int) string {
	t.Helper()
	parsed, err := parseWeekDate(date)
	if err != nil {
		t.Fatalf("parse %q: %v", date, err)
	}
	return parsed.AddDate(0, 0, n).Format(dateLayout)
}

func TestParsePlanEnvelopeReadsEnvelope(t *testing.T) {
	response := `{"week":` + cannedWeekJSON(t) + `,"adjustment":{"deviates":true,` +
		`"summary":"  Cut the second threshold session: ACR 1.41.  ",` +
		`"goal_update":{"target_hm_time":5100,"reason":"the prediction moved out"}}}`

	env, err := parsePlanEnvelope(response, envelopeTestWeekStart, envelopeTestWeekEnd)
	if err != nil {
		t.Fatalf("parsePlanEnvelope: %v", err)
	}
	if len(env.Week) != 7 {
		t.Fatalf("week = %d days, want 7", len(env.Week))
	}
	if env.Week[0].Date != envelopeTestWeekStart {
		t.Errorf("first day = %q, want %q", env.Week[0].Date, envelopeTestWeekStart)
	}
	if !env.Week[6].RestDay {
		t.Error("last day should be a rest day")
	}
	if !env.Adjustment.Deviates {
		t.Error("deviates = false, want true")
	}
	if env.Adjustment.Summary != "Cut the second threshold session: ACR 1.41." {
		t.Errorf("summary = %q, want it trimmed", env.Adjustment.Summary)
	}
	if env.Adjustment.GoalUpdate == nil {
		t.Fatal("goal_update = nil, want the proposal")
	}
	if env.Adjustment.GoalUpdate.TargetHMTimeS != 5100 {
		t.Errorf("target_hm_time = %d, want 5100", env.Adjustment.GoalUpdate.TargetHMTimeS)
	}
	if env.Adjustment.GoalUpdate.Reason != "the prediction moved out" {
		t.Errorf("reason = %q", env.Adjustment.GoalUpdate.Reason)
	}
}

// The legacy weekly generator answers with a bare array and no envelope. It has
// to keep parsing after the envelope landed, because a plan generated without a
// macro block still goes through the same reader.
func TestParsePlanEnvelopeReadsLegacyBareArray(t *testing.T) {
	env, err := parsePlanEnvelope(cannedWeekJSON(t), envelopeTestWeekStart, envelopeTestWeekEnd)
	if err != nil {
		t.Fatalf("parsePlanEnvelope: %v", err)
	}
	if len(env.Week) != 7 {
		t.Fatalf("week = %d days, want 7", len(env.Week))
	}
	if env.Adjustment.Deviates {
		t.Error("deviates = true, want the zero adjustment for a bare array")
	}
	if env.Adjustment.Summary != "" {
		t.Errorf("summary = %q, want empty for a bare array", env.Adjustment.Summary)
	}
	if env.Adjustment.GoalUpdate != nil {
		t.Error("goal_update should be nil for a bare array")
	}
}

func TestParsePlanEnvelopeUnwrapsCodeFences(t *testing.T) {
	for name, response := range map[string]string{
		"envelope": "```json\n{\"week\":" + cannedWeekJSON(t) + ",\"adjustment\":{\"deviates\":false,\"summary\":\"followed the spec\",\"goal_update\":null}}\n```",
		"array":    "```json\n" + cannedWeekJSON(t) + "\n```",
	} {
		t.Run(name, func(t *testing.T) {
			env, err := parsePlanEnvelope(response, envelopeTestWeekStart, envelopeTestWeekEnd)
			if err != nil {
				t.Fatalf("parsePlanEnvelope: %v", err)
			}
			if len(env.Week) != 7 {
				t.Fatalf("week = %d days, want 7", len(env.Week))
			}
		})
	}
}

func TestParsePlanEnvelopeRejectsBadInput(t *testing.T) {
	tests := map[string]string{
		"empty":            "   ",
		"not JSON":         "I could not plan this week.",
		"short week":       `{"week":[{"date":"2026-04-06","rest_day":true}],"adjustment":{"deviates":false,"summary":""}}`,
		"wrong week":       `{"week":` + cannedWeekJSON(t) + `,"adjustment":{"deviates":false,"summary":""}}`,
		"truncated object": `{"week":[{"date":"2026-04-06"`,
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			weekStart, weekEnd := envelopeTestWeekStart, envelopeTestWeekEnd
			if name == "wrong week" {
				weekStart, weekEnd = "2026-04-13", "2026-04-19"
			}
			if _, err := parsePlanEnvelope(response, weekStart, weekEnd); err == nil {
				t.Fatal("parsePlanEnvelope = nil error, want a failure")
			}
		})
	}
}

// The clamp is the whole authority boundary: the weekly coach may correct
// drift, but changing the goal is the athlete's call.
func TestEvaluateGoalUpdateClamp(t *testing.T) {
	const current = 5000 // 1:23:20

	tests := []struct {
		name    string
		current int
		update  *GoalUpdate
		want    bool
	}{
		{"2% slower with a reason is applied", current, &GoalUpdate{TargetHMTimeS: 5100, Reason: "illness cost two weeks"}, true},
		{"2% faster with a reason is applied", current, &GoalUpdate{TargetHMTimeS: 4900, Reason: "threshold pace improved"}, true},
		{"exactly 3% is still within tolerance", current, &GoalUpdate{TargetHMTimeS: 5150, Reason: "prediction moved"}, true},
		{"5% slower is rejected", current, &GoalUpdate{TargetHMTimeS: 5250, Reason: "illness cost two weeks"}, false},
		{"5% faster is rejected", current, &GoalUpdate{TargetHMTimeS: 4750, Reason: "threshold pace improved"}, false},
		{"within tolerance but no reason is rejected", current, &GoalUpdate{TargetHMTimeS: 5100, Reason: "  "}, false},
		{"nil proposal is rejected", current, nil, false},
		{"non-positive target is rejected", current, &GoalUpdate{TargetHMTimeS: 0, Reason: "typo"}, false},
		{"no current target to measure against is rejected", 0, &GoalUpdate{TargetHMTimeS: 5100, Reason: "prediction moved"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateGoalUpdate(tc.current, tc.update)
			if got.Accepted != tc.want {
				t.Fatalf("accepted = %v, want %v (rejection: %s)", got.Accepted, tc.want, got.Rejection)
			}
			if !got.Accepted && got.Rejection == "" {
				t.Error("a rejection must say why, for the log line")
			}
			if got.Accepted && got.Rejection != "" {
				t.Errorf("accepted decision carries a rejection %q", got.Rejection)
			}
		})
	}
}

// saveWeeklyPlanFixture seeds a block whose third week is the one being
// materialised, plus one unconsumed weekly note, and returns the target week.
func saveWeeklyPlanFixture(t *testing.T, db *sql.DB) (*MacroPlan, MacroWeek, int64) {
	t.Helper()

	block := seedAdjustBlock(t, db, 1, mondayAfter(envelopeTestWeekStart, -2))
	target, ok := macroWeekAt(block, envelopeTestWeekStart)
	if !ok {
		t.Fatalf("block has no week starting %s", envelopeTestWeekStart)
	}
	noteID := insertNoteWithScope(t, db, 1, "left calf tight after Sunday", "weekly")
	return block, target, noteID
}

// envelopeWithGoalUpdate builds a parsed envelope for the canned week carrying
// the given summary and (optionally) a goal proposal.
func envelopeWithGoalUpdate(t *testing.T, summary string, update *GoalUpdate) PlanEnvelope {
	t.Helper()

	adjustment, err := json.Marshal(PlanAdjustment{Deviates: true, Summary: summary, GoalUpdate: update})
	if err != nil {
		t.Fatalf("marshal adjustment: %v", err)
	}
	env, err := parsePlanEnvelope(
		`{"week":`+cannedWeekJSON(t)+`,"adjustment":`+string(adjustment)+`}`,
		envelopeTestWeekStart, envelopeTestWeekEnd)
	if err != nil {
		t.Fatalf("parsePlanEnvelope: %v", err)
	}
	return env
}

// readSavedPlan returns the queryable provenance columns of the saved week,
// with adjustment_summary decrypted.
func readSavedPlan(t *testing.T, db *sql.DB, weekStart string) (phase string, macroWeekID sql.NullInt64, summary string) {
	t.Helper()

	var stored string
	err := db.QueryRow(`
		SELECT phase, macro_week_id, adjustment_summary
		FROM stride_plans WHERE user_id = 1 AND week_start = ?
	`, weekStart).Scan(&phase, &macroWeekID, &stored)
	if err != nil {
		t.Fatalf("read saved plan: %v", err)
	}
	if summary, err = encryption.DecryptField(stored); err != nil {
		t.Fatalf("decrypt adjustment_summary: %v", err)
	}
	return phase, macroWeekID, summary
}

func TestSaveWeeklyPlanMaterialisesMacroWeek(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	block, target, noteID := saveWeeklyPlanFixture(t, db)
	const summary = "Dropped the second threshold session: two key sessions missed."
	env := envelopeWithGoalUpdate(t, summary, &GoalUpdate{
		// 2% slower than the block's 5040s target — inside the clamp.
		TargetHMTimeS: 5141,
		Reason:        "the prediction slipped after the missed sessions",
	})

	err := saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:      1,
		WeekStart:   envelopeTestWeekStart,
		WeekEnd:     envelopeTestWeekEnd,
		Phase:       target.Phase,
		Envelope:    env,
		Prompt:      "the adjust prompt",
		Response:    "the raw response",
		Model:       "claude-opus-5",
		Notes:       []Note{{ID: noteID, UserID: 1}},
		MacroWeekID: &target.ID,
		MacroPlanID: block.ID,
		CurrentGoal: block.Goal,
	})
	if err != nil {
		t.Fatalf("saveWeeklyPlan: %v", err)
	}

	phase, macroWeekID, storedSummary := readSavedPlan(t, db, envelopeTestWeekStart)
	if phase != target.Phase || phase == "" {
		t.Errorf("phase = %q, want the macro week's %q", phase, target.Phase)
	}
	if !macroWeekID.Valid || macroWeekID.Int64 != target.ID {
		t.Errorf("macro_week_id = %v, want %d", macroWeekID, target.ID)
	}
	if storedSummary != summary {
		t.Errorf("adjustment_summary = %q, want %q", storedSummary, summary)
	}

	// The summary must be encrypted at rest, not stored as prose.
	var raw string
	if err := db.QueryRow(`SELECT adjustment_summary FROM stride_plans WHERE user_id = 1 AND week_start = ?`,
		envelopeTestWeekStart).Scan(&raw); err != nil {
		t.Fatalf("read raw adjustment_summary: %v", err)
	}
	if strings.Contains(raw, "threshold") {
		t.Error("adjustment_summary is stored in plaintext")
	}

	// The macro week has been turned into an actual plan.
	week, err := GetMacroWeek(ctx, db, 1, envelopeTestWeekStart)
	if err != nil {
		t.Fatalf("get macro week: %v", err)
	}
	if week.Status != MacroWeekStatusMaterialised {
		t.Errorf("macro week status = %q, want %q", week.Status, MacroWeekStatusMaterialised)
	}

	// The note the prompt was rendered from is consumed.
	var consumedBy sql.NullString
	if err := db.QueryRow(`SELECT consumed_by FROM stride_notes WHERE id = ?`, noteID).Scan(&consumedBy); err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !consumedBy.Valid || consumedBy.String != "weekly" {
		t.Errorf("note consumed_by = %v, want \"weekly\"", consumedBy)
	}

	// The in-tolerance goal update landed as one 'weekly' revision.
	revisions, err := ListGoalRevisions(ctx, db, block.ID, 1)
	if err != nil {
		t.Fatalf("list goal revisions: %v", err)
	}
	if len(revisions) != 2 {
		t.Fatalf("goal revisions = %d, want the initial one plus the weekly one", len(revisions))
	}
	latest := revisions[len(revisions)-1]
	if latest.Source != GoalRevisionSourceWeekly {
		t.Errorf("source = %q, want %q", latest.Source, GoalRevisionSourceWeekly)
	}
	if latest.Goal.TargetHMTimeS != 5141 {
		t.Errorf("target_hm_time_s = %d, want 5141", latest.Goal.TargetHMTimeS)
	}
	if latest.Goal.Statement != block.Goal.Statement {
		t.Errorf("statement = %q, want the block's own %q — only the target time may move",
			latest.Goal.Statement, block.Goal.Statement)
	}
	if latest.Reason != "the prediction slipped after the missed sessions" {
		t.Errorf("reason = %q", latest.Reason)
	}
	if latest.WeekStart != envelopeTestWeekStart {
		t.Errorf("week_start = %q, want %q", latest.WeekStart, envelopeTestWeekStart)
	}
}

// A proposal beyond +/-3% is not applied: it leaves no revision behind and
// survives only in the week's adjustment summary.
func TestSaveWeeklyPlanRejectsOutOfToleranceGoalUpdate(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	block, target, _ := saveWeeklyPlanFixture(t, db)
	const summary = "Proposing 1:20:00: the athlete has run three weeks well ahead of the block."
	env := envelopeWithGoalUpdate(t, summary, &GoalUpdate{
		// 5% faster than the block's 5040s target — the athlete's call, not the coach's.
		TargetHMTimeS: 4788,
		Reason:        "three weeks well ahead of the block",
	})

	err := saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:      1,
		WeekStart:   envelopeTestWeekStart,
		WeekEnd:     envelopeTestWeekEnd,
		Phase:       target.Phase,
		Envelope:    env,
		Model:       "claude-opus-5",
		MacroWeekID: &target.ID,
		MacroPlanID: block.ID,
		CurrentGoal: block.Goal,
	})
	if err != nil {
		t.Fatalf("saveWeeklyPlan: %v", err)
	}

	revisions, err := ListGoalRevisions(ctx, db, block.ID, 1)
	if err != nil {
		t.Fatalf("list goal revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("goal revisions = %d, want only the block's initial one", len(revisions))
	}
	if revisions[0].Source != GoalRevisionSourceInitial {
		t.Errorf("source = %q, want %q", revisions[0].Source, GoalRevisionSourceInitial)
	}

	// The proposal still reaches next week's coach through the summary.
	_, _, storedSummary := readSavedPlan(t, db, envelopeTestWeekStart)
	if storedSummary != summary {
		t.Errorf("adjustment_summary = %q, want %q", storedSummary, summary)
	}
}

// Re-planning the same week overwrites the row rather than inserting a second
// one, and rewrites every provenance column from the new generation.
func TestSaveWeeklyPlanUpsertsOnWeekStart(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, target, _ := saveWeeklyPlanFixture(t, db)

	first := weeklyPlanWrite{
		UserID:      1,
		WeekStart:   envelopeTestWeekStart,
		WeekEnd:     envelopeTestWeekEnd,
		Phase:       target.Phase,
		Envelope:    envelopeWithGoalUpdate(t, "first pass", nil),
		Model:       "claude-opus-5",
		MacroWeekID: &target.ID,
	}
	if err := saveWeeklyPlan(ctx, db, first); err != nil {
		t.Fatalf("first saveWeeklyPlan: %v", err)
	}

	second := first
	second.Envelope = envelopeWithGoalUpdate(t, "second pass", nil)
	if err := saveWeeklyPlan(ctx, db, second); err != nil {
		t.Fatalf("second saveWeeklyPlan: %v", err)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_plans WHERE user_id = 1 AND week_start = ?`,
		envelopeTestWeekStart).Scan(&rows); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if rows != 1 {
		t.Fatalf("stride_plans rows = %d, want 1", rows)
	}
	if _, _, summary := readSavedPlan(t, db, envelopeTestWeekStart); summary != "second pass" {
		t.Errorf("adjustment_summary = %q, want the re-plan's %q", summary, "second pass")
	}
}

// The legacy path carries no block, so it writes an empty phase, a null
// macro_week_id and an empty summary — and must not fail for lack of any of it.
func TestSaveWeeklyPlanWithoutMacroBlock(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	env, err := parsePlanEnvelope(cannedWeekJSON(t), envelopeTestWeekStart, envelopeTestWeekEnd)
	if err != nil {
		t.Fatalf("parsePlanEnvelope: %v", err)
	}
	noteID := insertNoteWithScope(t, db, 1, "slept badly all week", "weekly")

	saveErr := saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:    1,
		WeekStart: envelopeTestWeekStart,
		WeekEnd:   envelopeTestWeekEnd,
		Envelope:  env,
		Model:     "claude-opus-5",
		Notes:     []Note{{ID: noteID, UserID: 1}},
	})
	if saveErr != nil {
		t.Fatalf("saveWeeklyPlan: %v", saveErr)
	}

	phase, macroWeekID, summary := readSavedPlan(t, db, envelopeTestWeekStart)
	if phase != "" {
		t.Errorf("phase = %q, want empty without a block", phase)
	}
	if macroWeekID.Valid {
		t.Errorf("macro_week_id = %d, want NULL without a block", macroWeekID.Int64)
	}
	if summary != "" {
		t.Errorf("adjustment_summary = %q, want empty without a block", summary)
	}

	var consumedBy sql.NullString
	if err := db.QueryRow(`SELECT consumed_by FROM stride_notes WHERE id = ?`, noteID).Scan(&consumedBy); err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !consumedBy.Valid || consumedBy.String != "weekly" {
		t.Errorf("note consumed_by = %v, want \"weekly\"", consumedBy)
	}
}

// A macro week belonging to somebody else must roll the whole write back — the
// status flip is scoped to the owning user, and a plan is never stored against
// a block the athlete does not own.
func TestSaveWeeklyPlanRollsBackOnForeignMacroWeek(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	insertSecondUser(t, db)
	foreignBlock := seedAdjustBlock(t, db, 2, mondayAfter(envelopeTestWeekStart, -2))
	foreignWeek, ok := macroWeekAt(foreignBlock, envelopeTestWeekStart)
	if !ok {
		t.Fatalf("foreign block has no week starting %s", envelopeTestWeekStart)
	}

	env, err := parsePlanEnvelope(cannedWeekJSON(t), envelopeTestWeekStart, envelopeTestWeekEnd)
	if err != nil {
		t.Fatalf("parsePlanEnvelope: %v", err)
	}
	noteID := insertNoteWithScope(t, db, 1, "should survive the rollback", "weekly")

	saveErr := saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:      1,
		WeekStart:   envelopeTestWeekStart,
		WeekEnd:     envelopeTestWeekEnd,
		Envelope:    env,
		Model:       "claude-opus-5",
		Notes:       []Note{{ID: noteID, UserID: 1}},
		MacroWeekID: &foreignWeek.ID,
	})
	if !errors.Is(saveErr, ErrMacroWeekNotFound) {
		t.Fatalf("saveWeeklyPlan = %v, want ErrMacroWeekNotFound", saveErr)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_plans WHERE user_id = 1`).Scan(&rows); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if rows != 0 {
		t.Fatalf("stride_plans rows = %d, want the write rolled back", rows)
	}

	var consumedAt sql.NullString
	if err := db.QueryRow(`SELECT consumed_at FROM stride_notes WHERE id = ?`, noteID).Scan(&consumedAt); err != nil {
		t.Fatalf("read note: %v", err)
	}
	if consumedAt.Valid {
		t.Error("the note was consumed by a write that rolled back")
	}
}
