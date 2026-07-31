package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

func insertTestUserAndPlan(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO users (id, email, name, google_id) VALUES (1, 'a@x.no', 'A', 'g1')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, created_at)
		VALUES (1, '2026-07-27', '2026-08-02', 'base', '[]', ?)`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()
	return planID
}

func insertTestEval(t *testing.T, db *sql.DB, planID int64, workoutID *int64, eval Evaluation) int64 {
	t.Helper()
	wid := int64(0)
	if workoutID != nil {
		wid = *workoutID
	}
	if wid > 0 {
		if _, err := db.Exec(`INSERT OR IGNORE INTO workouts (id, user_id, sport, title, started_at, duration_seconds, distance_meters, avg_heart_rate, max_heart_rate, avg_pace_sec_per_km)
			VALUES (?, 1, 'running', 'Test run', '2026-07-29T10:00:00Z', 3600, 10000, 150, 170, 360)`, wid); err != nil {
			t.Fatalf("insert workout: %v", err)
		}
		if err := storeEvaluation(context.Background(), db, 1, wid, planID, &eval); err != nil {
			t.Fatalf("store eval: %v", err)
		}
	} else {
		// Date-based evaluation: insert directly with NULL workout_id.
		raw, _ := json.Marshal(eval)
		enc, err := encryption.EncryptField(string(raw))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
			VALUES (1, ?, NULL, ?, ?)`, planID, enc, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("insert date eval: %v", err)
		}
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM stride_evaluations ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("select eval id: %v", err)
	}
	return id
}

func TestReplyToEvaluationConversationalOnly(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestUserAndPlan(t, db)
	wid := int64(42)
	evalID := insertTestEval(t, db, planID, &wid, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "compliant",
		Notes: "Solid easy run.", Flags: []string{},
	})

	origFn := runPromptFunc
	var gotPrompt string
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
		gotPrompt = prompt
		return "Good question — the HR spike was likely cardiac drift, nothing to worry about.", nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true}
	reply, err := ReplyToEvaluation(context.Background(), db, cfg, 1, evalID, "Why was my HR high at the end?")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.UpdatedEval != nil {
		t.Errorf("expected no revision for conversational reply")
	}
	if len(reply.Messages) != 2 || reply.Messages[0].Role != "user" || reply.Messages[1].Role != "coach" {
		t.Fatalf("messages = %+v, want user+coach pair", reply.Messages)
	}
	if reply.Messages[1].EvalRevised {
		t.Errorf("coach message marked revised without a revision")
	}
	// Prompt carries the current eval and the guarded athlete message.
	if !strings.Contains(gotPrompt, `"compliance":"compliant"`) {
		t.Errorf("prompt missing current eval JSON:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "<user-data>Why was my HR high at the end?</user-data>") {
		t.Errorf("prompt missing guarded athlete message")
	}

	// Thread persisted and readable via the list path.
	rec, err := GetEvaluationByID(context.Background(), db, 1, evalID)
	if err != nil {
		t.Fatalf("get eval: %v", err)
	}
	workoutID, date := evalThreadKey(rec)
	msgs, err := listEvalMessages(context.Background(), db, 1, planID, workoutID, date)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("stored %d messages, want 2", len(msgs))
	}
}

func TestReplyToEvaluationAppliesRevisionInPlace(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestUserAndPlan(t, db)
	wid := int64(7)
	evalID := insertTestEval(t, db, planID, &wid, Evaluation{
		PlannedType: "threshold", ActualType: "easy", Compliance: "missed",
		Notes: "Looks like the intervals were skipped.", Flags: []string{"too_short"},
	})

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "Ah, treadmill lap data missing — with your report this was a completed threshold session. Updated.\n\n```json\n" +
			`{"planned_type": "threshold", "actual_type": "threshold", "compliance": "compliant", "notes": "Completed as planned on the treadmill.", "flags": [], "adjustments": "Proceed as planned.", "questions": []}` +
			"\n```", nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true}
	reply, err := ReplyToEvaluation(context.Background(), db, cfg, 1, evalID, "The treadmill lost laps — I did do the 5x6min blocks.")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.UpdatedEval == nil || reply.UpdatedEval.Compliance != "compliant" {
		t.Fatalf("updated eval = %+v, want compliant revision", reply.UpdatedEval)
	}
	if !strings.Contains(reply.Messages[1].Content, "treadmill lap data missing") {
		t.Errorf("coach reply lost its conversational text: %q", reply.Messages[1].Content)
	}
	if strings.Contains(reply.Messages[1].Content, "```") {
		t.Errorf("fenced JSON leaked into the stored coach message")
	}
	if !reply.Messages[1].EvalRevised {
		t.Errorf("coach message not marked as revising the eval")
	}

	// Revision applied in place: same row id, new content.
	rec, err := GetEvaluationByID(context.Background(), db, 1, evalID)
	if err != nil {
		t.Fatalf("get eval: %v", err)
	}
	if rec.Eval.Compliance != "compliant" || rec.Eval.ActualType != "threshold" {
		t.Errorf("stored eval not revised: %+v", rec.Eval)
	}
}

func TestReplyToEvaluationInvalidRevisionJSONIsConversational(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestUserAndPlan(t, db)
	wid := int64(9)
	evalID := insertTestEval(t, db, planID, &wid, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "partial", Notes: "n", Flags: []string{},
	})

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "Here's a thought.\n\n```json\n{\"compliance\": \"totally-fine\"}\n```", nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true}
	reply, err := ReplyToEvaluation(context.Background(), db, cfg, 1, evalID, "hm")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.UpdatedEval != nil {
		t.Errorf("invalid JSON must not produce a revision")
	}
	rec, _ := GetEvaluationByID(context.Background(), db, 1, evalID)
	if rec.Eval.Compliance != "partial" {
		t.Errorf("stored eval changed despite invalid revision: %+v", rec.Eval)
	}
}

func TestStoreEvaluationSeedsQuestionThread(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestUserAndPlan(t, db)

	eval := Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "partial",
		Notes: "HR unusually high.", Flags: []string{"hr_too_high"},
		Questions: []string{"Was the HR strap seated properly?", "Were you feeling ill?"},
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO workouts (id, user_id, sport, title, started_at) VALUES (55, 1, 'running', 'Q run', '2026-07-29T10:00:00Z')`); err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	if err := storeEvaluation(context.Background(), db, 1, 55, planID, &eval); err != nil {
		t.Fatalf("store: %v", err)
	}

	wid := int64(55)
	msgs, err := listEvalMessages(context.Background(), db, 1, planID, &wid, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "coach" {
		t.Fatalf("msgs = %+v, want one coach opener", msgs)
	}
	if !strings.Contains(msgs[0].Content, "HR strap") {
		t.Errorf("question content missing: %q", msgs[0].Content)
	}
}

func TestParseEvalResponseCapsQuestions(t *testing.T) {
	resp := `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"n","flags":[],"adjustments":"a","questions":["q1","q2","q3","q4"]}`
	eval, err := parseEvalResponse(resp)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(eval.Questions) != 2 {
		t.Errorf("questions = %v, want capped at 2", eval.Questions)
	}
}

func TestEvalThreadSurvivesReEvaluation(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestUserAndPlan(t, db)
	wid := int64(77)
	evalID := insertTestEval(t, db, planID, &wid, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "compliant", Notes: "n", Flags: []string{},
	})

	if err := insertEvalMessage(context.Background(), db, 1, planID, &wid, "", roleUser, "hello coach", false); err != nil {
		t.Fatalf("insert msg: %v", err)
	}

	// Simulate a re-evaluation: delete + reinsert the evaluation row (new id).
	if _, err := db.Exec(`DELETE FROM stride_evaluations WHERE id = ?`, evalID); err != nil {
		t.Fatalf("delete eval: %v", err)
	}
	newEvalID := insertTestEval(t, db, planID, &wid, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "partial", Notes: "n2", Flags: []string{},
	})
	// Note: SQLite may reuse the freed row id here; the thread key (workout
	// id) is what matters, not the evaluation row identity.

	rec, err := GetEvaluationByID(context.Background(), db, 1, newEvalID)
	if err != nil {
		t.Fatalf("get new eval: %v", err)
	}
	workoutID, date := evalThreadKey(rec)
	msgs, err := listEvalMessages(context.Background(), db, 1, planID, workoutID, date)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello coach" {
		t.Fatalf("thread lost across re-evaluation: %+v", msgs)
	}
}
