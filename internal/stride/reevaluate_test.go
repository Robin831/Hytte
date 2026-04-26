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

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// reevalSetup wires the in-memory DB with the user_preferences rows ReEvaluateDate
// reads via training.LoadClaudeConfig. Pass claudeEnabled=false to simulate the
// disabled branch.
func reevalSetup(t *testing.T, db *sql.DB, claudeEnabled bool) {
	t.Helper()
	enabled := "false"
	if claudeEnabled {
		enabled = "true"
	}
	rows := []struct{ key, value string }{
		{"stride_enabled", "true"},
		{"claude_enabled", enabled},
		{"claude_cli_path", "/usr/bin/claude"},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, r.key, r.value); err != nil {
			t.Fatalf("insert pref %s: %v", r.key, err)
		}
	}
}

// stubRunPrompt swaps the Claude callable for a test fixture and registers
// cleanup to restore the original.
func stubRunPrompt(t *testing.T, fn func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error)) {
	t.Helper()
	orig := runPromptFunc
	runPromptFunc = fn
	t.Cleanup(func() { runPromptFunc = orig })
}

// --- ReEvaluateDate ---

func TestReEvaluateDate_ReplacesExistingWorkoutEvaluation(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "easy run", Description: "Easy"}},
	})
	planID := insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (200, 1, 'running', ?, 'hash-200', ?)
	`, date+"T07:00:00Z", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Pre-existing evaluation that the re-run should replace.
	stale := &Evaluation{Compliance: "missed", Notes: "STALE", Flags: []string{}}
	if err := storeEvaluation(context.Background(), db, 1, 200, planID, stale); err != nil {
		t.Fatalf("seed stale eval: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"FRESH","flags":[],"adjustments":""}`, nil
	})

	produced, err := ReEvaluateDate(context.Background(), db, nil, 1, date)
	if err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}
	if produced != 1 {
		t.Errorf("produced = %d, want 1", produced)
	}

	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 (stale should be replaced)", len(records))
	}
	if records[0].Eval.Notes != "FRESH" {
		t.Errorf("notes = %q, want FRESH", records[0].Eval.Notes)
	}
	if records[0].Eval.Compliance != "compliant" {
		t.Errorf("compliance = %q, want compliant", records[0].Eval.Compliance)
	}
}

func TestReEvaluateDate_PreservesExistingOnClaudeFailure(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-09"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "threshold", Description: "Threshold"}},
	})
	planID := insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (210, 1, 'running', ?, 'hash-210', ?)
	`, date+"T07:00:00Z", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Seed a prior coach evaluation that we MUST not lose if Claude fails.
	original := &Evaluation{Compliance: "compliant", Notes: "PRIOR-COACH-OUTPUT", Flags: []string{}}
	if err := storeEvaluation(context.Background(), db, 1, 210, planID, original); err != nil {
		t.Fatalf("seed original: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", errors.New("claude is down")
	})

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, date); err == nil {
		t.Fatal("expected error when Claude fails")
	}

	// Verify the prior coach evaluation is untouched — that's the regression
	// the warden flagged.
	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 (original must be preserved)", len(records))
	}
	if records[0].Eval.Notes != "PRIOR-COACH-OUTPUT" {
		t.Errorf("notes = %q, want PRIOR-COACH-OUTPUT (original lost)", records[0].Eval.Notes)
	}
}

func TestReEvaluateDate_PreservesExistingOnClaudeFailure_RestDay(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-10"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: true},
	})
	planID := insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	// Seed an existing rest-day evaluation.
	original := &Evaluation{Compliance: "rest_day", Notes: "PRIOR-REST", Flags: []string{}, Date: date}
	if err := storeEvaluationForDate(context.Background(), db, 1, planID, original); err != nil {
		t.Fatalf("seed original: %v", err)
	}

	// Add an unconsumed note so ReEvaluateDate calls Claude (notes drive AI path).
	if _, err := CreateNote(db, 1, nil, "felt great today, took a walk", date, ""); err != nil {
		t.Fatalf("create note: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", errors.New("claude is down")
	})

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, date); err == nil {
		t.Fatal("expected error when Claude fails")
	}

	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].Eval.Notes != "PRIOR-REST" {
		t.Errorf("notes = %q, want PRIOR-REST (original rest-day eval was wiped)", records[0].Eval.Notes)
	}

	// Notes must remain unconsumed since the re-run failed.
	var consumedAt sql.NullString
	if err := db.QueryRow(`SELECT consumed_at FROM stride_notes WHERE user_id = 1`).Scan(&consumedAt); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if consumedAt.Valid {
		t.Errorf("note must not be consumed when re-eval fails, got %v", consumedAt.String)
	}
}

func TestReEvaluateDate_NoStridePlanReturnsSentinel(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Fatal("Claude should not be called when there is no plan")
		return "", nil
	})

	_, err := ReEvaluateDate(context.Background(), db, nil, 1, "2026-04-08")
	if !errors.Is(err, ErrNoStridePlan) {
		t.Errorf("expected ErrNoStridePlan, got %v", err)
	}
}

func TestReEvaluateDate_ClaudeDisabledReturnsSentinel(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, false)

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Fatal("Claude should not be called when disabled")
		return "", nil
	})

	_, err := ReEvaluateDate(context.Background(), db, nil, 1, "2026-04-08")
	if !errors.Is(err, training.ErrClaudeNotEnabled) {
		t.Errorf("expected ErrClaudeNotEnabled, got %v", err)
	}
}

func TestReEvaluateDate_MarksNotesConsumedManual(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "easy run"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (220, 1, 'running', ?, 'hash-220', ?)
	`, date+"T07:00:00Z", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	note, err := CreateNote(db, 1, nil, "the coach got it wrong, this was easy not threshold", date, "")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var capturedPrompt string
	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
		capturedPrompt = prompt
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"OK","flags":[],"adjustments":""}`, nil
	})

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, date); err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}

	if !strings.Contains(capturedPrompt, "the coach got it wrong") {
		t.Error("expected note content to be passed to Claude")
	}

	var consumedBy sql.NullString
	if err := db.QueryRow(`SELECT consumed_by FROM stride_notes WHERE id = ?`, note.ID).Scan(&consumedBy); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if !consumedBy.Valid || consumedBy.String != "manual" {
		t.Errorf("consumed_by = %v, want 'manual'", consumedBy)
	}
}

func TestReEvaluateDate_NoWorkoutMissedSession(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "4x10min", Description: "Threshold"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	// No notes, no Claude call expected for the template path.
	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Fatal("Claude should not be called when there are no notes for the missed session")
		return "", nil
	})

	produced, err := ReEvaluateDate(context.Background(), db, nil, 1, date)
	if err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}
	if produced != 1 {
		t.Errorf("produced = %d, want 1 (template-driven missed-session eval)", produced)
	}

	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].Eval.Compliance != "missed" {
		t.Errorf("compliance = %q, want missed", records[0].Eval.Compliance)
	}
}

func TestReEvaluateDate_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, "not-a-date"); err == nil {
		t.Error("expected error for invalid date")
	}
}

// --- ReEvaluateDayHandler ---

func TestReEvaluateDayHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "easy"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (300, 1, 'running', ?, ?, ?)
	`, date+"T07:00:00Z", "hash-300", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"good","flags":[],"adjustments":""}`, nil
	})

	req := withChiParam(withUser(httptest.NewRequest(http.MethodPost, "/api/stride/days/"+date+"/reevaluate", nil), 1), "date", date)
	rec := httptest.NewRecorder()
	ReEvaluateDayHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Evaluated int    `json:"evaluated"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Evaluated != 1 {
		t.Errorf("evaluated = %d, want 1", body.Evaluated)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestReEvaluateDayHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	req := withChiParam(withUser(httptest.NewRequest(http.MethodPost, "/api/stride/days/bogus/reevaluate", nil), 1), "date", "bogus")
	rec := httptest.NewRecorder()
	ReEvaluateDayHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid date, got %d", rec.Code)
	}
}

func TestReEvaluateDayHandler_NoPlanReturns404(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Fatal("Claude should not be called when there is no plan")
		return "", nil
	})

	date := "2026-04-08"
	req := withChiParam(withUser(httptest.NewRequest(http.MethodPost, "/api/stride/days/"+date+"/reevaluate", nil), 1), "date", date)
	rec := httptest.NewRecorder()
	ReEvaluateDayHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no plan covers date, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReEvaluateDayHandler_ClaudeDisabledReturns400(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, false)

	date := "2026-04-08"
	req := withChiParam(withUser(httptest.NewRequest(http.MethodPost, "/api/stride/days/"+date+"/reevaluate", nil), 1), "date", date)
	rec := httptest.NewRecorder()
	ReEvaluateDayHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when Claude disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReEvaluateDayHandler_ClaudeFailureReturns500(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "easy"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (310, 1, 'running', ?, ?, ?)
	`, date+"T07:00:00Z", "hash-310", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", errors.New("claude crashed")
	})

	req := withChiParam(withUser(httptest.NewRequest(http.MethodPost, "/api/stride/days/"+date+"/reevaluate", nil), 1), "date", date)
	rec := httptest.NewRecorder()
	ReEvaluateDayHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- buildDateEval ---

func TestBuildDateEval_RestDayTemplate(t *testing.T) {
	plan := Plan{Plan: json.RawMessage(`[{"date":"2026-04-08","rest_day":true}]`)}
	eval, err := buildDateEval(context.Background(), nil, plan, "2026-04-08", nil)
	if err != nil {
		t.Fatalf("buildDateEval: %v", err)
	}
	if eval == nil {
		t.Fatal("expected rest-day eval, got nil")
	}
	if eval.Compliance != "rest_day" {
		t.Errorf("compliance = %q, want rest_day", eval.Compliance)
	}
}

func TestBuildDateEval_MissedSessionTemplate(t *testing.T) {
	plan := Plan{Plan: json.RawMessage(`[{"date":"2026-04-08","rest_day":false,"session":{"main_set":"4x10","description":"Threshold"}}]`)}
	eval, err := buildDateEval(context.Background(), nil, plan, "2026-04-08", nil)
	if err != nil {
		t.Fatalf("buildDateEval: %v", err)
	}
	if eval == nil {
		t.Fatal("expected missed-session eval, got nil")
	}
	if eval.Compliance != "missed" {
		t.Errorf("compliance = %q, want missed", eval.Compliance)
	}
}

func TestBuildDateEval_UnknownDateReturnsNil(t *testing.T) {
	plan := Plan{Plan: json.RawMessage(`[{"date":"2026-04-08","rest_day":true}]`)}
	eval, err := buildDateEval(context.Background(), nil, plan, "2026-04-09", nil)
	if err != nil {
		t.Fatalf("buildDateEval: %v", err)
	}
	if eval != nil {
		t.Errorf("expected nil for date not in plan, got %+v", eval)
	}
}

func TestBuildDateEval_RestDayWithNotesUsesClaude(t *testing.T) {
	cfg := &training.ClaudeConfig{Enabled: true, Model: "test"}
	plan := Plan{Plan: json.RawMessage(`[{"date":"2026-04-08","rest_day":true}]`)}
	notes := []Note{{ID: 1, TargetDate: "2026-04-08", Content: "took an active recovery walk"}}

	called := false
	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		called = true
		return `{"planned_type":"rest","actual_type":"rest","compliance":"rest_day","notes":"AI-CONTEXT","flags":[],"adjustments":""}`, nil
	})

	eval, err := buildDateEval(context.Background(), cfg, plan, "2026-04-08", notes)
	if err != nil {
		t.Fatalf("buildDateEval: %v", err)
	}
	if !called {
		t.Error("expected Claude to be called for rest day with notes")
	}
	if eval.Notes != "AI-CONTEXT" {
		t.Errorf("notes = %q, want AI-CONTEXT", eval.Notes)
	}
}

// Sanity check: re-evaluating with no existing rows should still insert the new
// evaluation (no precondition that prior data exists).
func TestReEvaluateDate_NoExistingRowsStillInserts(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: true},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		t.Fatal("Claude should not be called for rest day with no notes")
		return "", nil
	})

	produced, err := ReEvaluateDate(context.Background(), db, nil, 1, date)
	if err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}
	if produced != 1 {
		t.Errorf("produced = %d, want 1", produced)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

// Verify a successful re-eval also clears stale rest-day rows for the same date.
func TestReEvaluateDate_ReplacesExistingDateOnlyEvaluation(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: true},
	})
	planID := insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	// Seed a stale rest-day evaluation for the date.
	stale := &Evaluation{Compliance: "rest_day", Notes: "STALE", Flags: []string{}, Date: date}
	if err := storeEvaluationForDate(context.Background(), db, 1, planID, stale); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	// Add a note so Claude is invoked (with notes path).
	if _, err := CreateNote(db, 1, nil, "did some yoga", date, ""); err != nil {
		t.Fatalf("create note: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"rest","actual_type":"rest","compliance":"rest_day","notes":"FRESH","flags":[],"adjustments":"","date":"` + date + `"}`, nil
	})

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, date); err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}

	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 (stale rest-day eval should be replaced)", len(records))
	}
	if records[0].Eval.Notes != "FRESH" {
		t.Errorf("notes = %q, want FRESH", records[0].Eval.Notes)
	}
}

// Ensure the new ciphertext for a successful re-eval is decryptable round-trip.
func TestReEvaluateDate_StoredCiphertextRoundTrips(t *testing.T) {
	db := setupTestDB(t)
	reevalSetup(t, db, true)

	date := "2026-04-08"
	weekStart, weekEnd := "2026-04-06", "2026-04-12"
	planJSON, _ := json.Marshal([]DayPlan{
		{Date: date, RestDay: false, Session: &Session{MainSet: "easy"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (250, 1, 'running', ?, 'hash-250', ?)
	`, date+"T07:00:00Z", date+"T08:00:00Z"); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	stubRunPrompt(t, func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"good","flags":[],"adjustments":""}`, nil
	})

	if _, err := ReEvaluateDate(context.Background(), db, nil, 1, date); err != nil {
		t.Fatalf("ReEvaluateDate: %v", err)
	}

	var raw string
	if err := db.QueryRow(`SELECT eval_json FROM stride_evaluations WHERE workout_id = 250`).Scan(&raw); err != nil {
		t.Fatalf("query eval: %v", err)
	}
	if strings.HasPrefix(raw, "{") {
		t.Fatal("eval_json should be encrypted at rest")
	}
	if _, err := encryption.DecryptField(raw); err != nil {
		t.Errorf("decrypt round-trip: %v", err)
	}
}
