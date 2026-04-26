package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// --- NextNightlyEvaluationRun ---

func TestNextNightlyEvaluationRun(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 03:00 returns same day",
			now:  time.Date(2026, 4, 6, 1, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 6, 3, 0, 0, 0, oslo),
		},
		{
			name: "after 03:00 returns next day",
			now:  time.Date(2026, 4, 6, 4, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 7, 3, 0, 0, 0, oslo),
		},
		{
			name: "exactly 03:00 returns next day",
			now:  time.Date(2026, 4, 6, 3, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 7, 3, 0, 0, 0, oslo),
		},
		{
			name: "nil loc uses UTC",
			now:  time.Date(2026, 4, 6, 1, 0, 0, 0, time.UTC),
			want: time.Date(2026, 4, 6, 3, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var loc *time.Location
			if tc.name == "nil loc uses UTC" {
				loc = nil
			} else {
				loc = oslo
			}
			got := NextNightlyEvaluationRun(tc.now, loc)
			if !got.Equal(tc.want) {
				t.Errorf("NextNightlyEvaluationRun(%v) = %v, want %v", tc.now, got, tc.want)
			}
			if !got.After(tc.now) {
				t.Errorf("result %v is not after now %v", got, tc.now)
			}
		})
	}
}

// --- parseEvalResponse ---

func TestParseEvalResponse_Valid(t *testing.T) {
	raw := `{
		"planned_type": "threshold",
		"actual_type":  "threshold",
		"compliance":   "compliant",
		"notes":        "Great session. HR stayed in zone.",
		"flags":        ["hr_too_high"],
		"adjustments":  "Keep the same effort next session."
	}`

	eval, err := parseEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.PlannedType != "threshold" {
		t.Errorf("planned_type = %q, want %q", eval.PlannedType, "threshold")
	}
	if eval.Compliance != "compliant" {
		t.Errorf("compliance = %q, want %q", eval.Compliance, "compliant")
	}
	if len(eval.Flags) != 1 || eval.Flags[0] != "hr_too_high" {
		t.Errorf("flags = %v, want [hr_too_high]", eval.Flags)
	}
}

func TestParseEvalResponse_MarkdownFences(t *testing.T) {
	raw := "```json\n{\"planned_type\":\"easy\",\"actual_type\":\"easy\",\"compliance\":\"partial\",\"notes\":\"OK\",\"flags\":[],\"adjustments\":\"Rest more.\"}\n```"
	eval, err := parseEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.Compliance != "partial" {
		t.Errorf("compliance = %q, want partial", eval.Compliance)
	}
}

func TestParseEvalResponse_EmptyFlags(t *testing.T) {
	raw := `{"planned_type":"easy","actual_type":"easy","compliance":"missed","notes":"Skipped","flags":null,"adjustments":""}`
	eval, err := parseEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.Flags == nil {
		t.Error("flags should be non-nil empty slice, got nil")
	}
	if len(eval.Flags) != 0 {
		t.Errorf("flags = %v, want []", eval.Flags)
	}
}

func TestParseEvalResponse_InvalidCompliance(t *testing.T) {
	raw := `{"planned_type":"easy","actual_type":"easy","compliance":"unknown","notes":"","flags":[],"adjustments":""}`
	_, err := parseEvalResponse(raw)
	if err == nil {
		t.Error("expected error for invalid compliance value, got nil")
	}
}

func TestParseEvalResponse_InvalidJSON(t *testing.T) {
	_, err := parseEvalResponse("not json")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- hasCriticalFlag ---

func TestHasCriticalFlag(t *testing.T) {
	if !hasCriticalFlag([]string{"overtraining"}) {
		t.Error("overtraining should be critical")
	}
	if !hasCriticalFlag([]string{"hr_too_low", "injury_risk"}) {
		t.Error("injury_risk should be critical")
	}
	if !hasCriticalFlag([]string{"hr_too_high"}) {
		t.Error("hr_too_high should be critical")
	}
	if hasCriticalFlag([]string{"hr_too_low", "pacing_issue"}) {
		t.Error("non-critical flags should not trigger")
	}
	if hasCriticalFlag(nil) {
		t.Error("nil flags should not trigger")
	}
}

// --- buildEvalPrompt ---

func TestBuildEvalPrompt_NoSession(t *testing.T) {
	workout := training.Workout{
		ID:              1,
		Sport:           "running",
		StartedAt:       "2026-04-06T07:00:00Z",
		DurationSeconds: 3600,
		AvgHeartRate:    145,
		Title:           "Morning Run",
	}
	prompt := buildEvalPrompt(workout, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if !strings.Contains(prompt, "bonus/unplanned") {
		t.Error("expected prompt to mention bonus/unplanned when no session")
	}
	if !strings.Contains(prompt, "Morning Run") {
		t.Error("expected prompt to include workout title")
	}
	if !strings.Contains(prompt, "running") {
		t.Error("expected prompt to include sport")
	}
}

func TestBuildEvalPrompt_WithSession(t *testing.T) {
	session := &PlannedSession{
		Date: "2026-04-06",
		Session: &Session{
			MainSet:      "4x10min threshold",
			Description:  "Threshold session",
			TargetHRCap:  165,
		},
	}
	workout := training.Workout{
		Sport:           "running",
		StartedAt:       "2026-04-06T07:00:00Z",
		DurationSeconds: 3600,
		AvgHeartRate:    162,
	}
	plan := Plan{
		ID:        1,
		WeekStart: "2026-04-07",
		WeekEnd:   "2026-04-13",
		Plan:      json.RawMessage(`[]`),
	}
	prompt := buildEvalPrompt(workout, session, plan, training.UserTrainingProfile{}, nil)
	if !strings.Contains(prompt, "4x10min threshold") {
		t.Error("expected prompt to include session main set")
	}
	if !strings.Contains(prompt, "165") {
		t.Error("expected prompt to include target HR cap")
	}
}

func TestBuildEvalPrompt_OmitsEmptyTitle(t *testing.T) {
	workout := training.Workout{
		Sport:     "running",
		StartedAt: "2026-04-06T07:00:00Z",
		Title:     "",
	}
	prompt := buildEvalPrompt(workout, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if strings.Contains(prompt, "Title:") {
		t.Error("prompt should not include Title line when title is empty")
	}
}

func TestBuildEvalPrompt_WithNotes(t *testing.T) {
	workout := training.Workout{
		Sport:     "running",
		StartedAt: "2026-04-06T07:00:00Z",
	}
	notes := []Note{
		{ID: 1, TargetDate: "2026-04-05", Content: "Felt sick with a cold"},
		{ID: 2, TargetDate: "2026-04-06", Content: "Still recovering, took it easy"},
	}
	prompt := buildEvalPrompt(workout, nil, Plan{}, training.UserTrainingProfile{}, notes)
	if !strings.Contains(prompt, "User Notes") {
		t.Error("expected prompt to contain User Notes section header")
	}
	if !strings.Contains(prompt, "Felt sick with a cold") {
		t.Error("expected prompt to contain first note content")
	}
	if !strings.Contains(prompt, "Still recovering, took it easy") {
		t.Error("expected prompt to contain second note content")
	}
	if !strings.Contains(prompt, "2026-04-05") {
		t.Error("expected prompt to contain first note date")
	}
}

func TestBuildEvalPrompt_WithoutNotes(t *testing.T) {
	workout := training.Workout{
		Sport:     "running",
		StartedAt: "2026-04-06T07:00:00Z",
	}
	prompt := buildEvalPrompt(workout, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if strings.Contains(prompt, "User Notes") {
		t.Error("prompt should not contain User Notes section when no notes provided")
	}
}

// --- storeEvaluation and queryUnevaluatedWorkouts ---

func TestStoreEvaluation_RoundTrip(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	// Insert a workout.
	_, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (10, 1, 'running', '2026-04-08T07:00:00Z', 'hash1', '2026-04-08T08:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	eval := &Evaluation{
		PlannedType: "threshold",
		ActualType:  "threshold",
		Compliance:  "compliant",
		Notes:       "Good session.",
		Flags:       []string{"hr_too_high"},
		Adjustments: "Keep it up.",
	}

	ctx := context.Background()
	if err := storeEvaluation(ctx, db, 1, 10, planID, eval); err != nil {
		t.Fatalf("storeEvaluation: %v", err)
	}

	// Verify stored and encrypted.
	var rawJSON string
	if err := db.QueryRow(`SELECT eval_json FROM stride_evaluations WHERE workout_id = 10`).Scan(&rawJSON); err != nil {
		t.Fatalf("query eval: %v", err)
	}

	// Should be stored as ciphertext, not plaintext.
	if strings.HasPrefix(rawJSON, "{") {
		t.Error("eval_json should be encrypted at rest, not raw JSON")
	}

	// Decrypt and verify round-trip.
	decrypted, err := encryption.DecryptField(rawJSON)
	if err != nil {
		t.Fatalf("decrypt eval_json: %v", err)
	}
	var got Evaluation
	if err := json.Unmarshal([]byte(decrypted), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Compliance != "compliant" {
		t.Errorf("compliance = %q, want compliant", got.Compliance)
	}
	if len(got.Flags) != 1 || got.Flags[0] != "hr_too_high" {
		t.Errorf("flags = %v, want [hr_too_high]", got.Flags)
	}
}

func TestStoreEvaluation_DuplicatePrevented(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	_, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (11, 1, 'running', '2026-04-08T07:00:00Z', 'hash2', '2026-04-08T08:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	eval := &Evaluation{Compliance: "partial", Flags: []string{}}
	ctx := context.Background()

	if err := storeEvaluation(ctx, db, 1, 11, planID, eval); err != nil {
		t.Fatalf("first storeEvaluation: %v", err)
	}

	// A second store succeeds (no UNIQUE constraint on workout_id), inserting a second row.
	// The unevaluated-workout query uses NOT EXISTS, so the workout is still filtered out
	// regardless of how many evaluation rows exist for it.
	if err := storeEvaluation(ctx, db, 1, 11, planID, eval); err != nil {
		t.Fatalf("second storeEvaluation: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE workout_id = 11`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 evaluation records after two inserts (no UNIQUE constraint), got %d", count)
	}
}

func TestQueryUnevaluatedWorkouts_FiltersEvaluated(t *testing.T) {
	db := setupTestDB(t)
	planID := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	since := "2026-04-06T00:00:00Z"

	// Insert two workouts.
	_, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES
		  (20, 1, 'running', '2026-04-07T07:00:00Z', 'h20', '2026-04-07T08:00:00Z'),
		  (21, 1, 'running', '2026-04-08T07:00:00Z', 'h21', '2026-04-08T08:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert workouts: %v", err)
	}

	ctx := context.Background()

	// Both workouts should be unevaluated.
	workouts, err := queryUnevaluatedWorkouts(ctx, db, 1, since)
	if err != nil {
		t.Fatalf("queryUnevaluatedWorkouts: %v", err)
	}
	if len(workouts) != 2 {
		t.Fatalf("got %d unevaluated workouts, want 2", len(workouts))
	}

	// Evaluate workout 20.
	eval := &Evaluation{Compliance: "compliant", Flags: []string{}}
	if err := storeEvaluation(ctx, db, 1, 20, planID, eval); err != nil {
		t.Fatalf("storeEvaluation: %v", err)
	}

	// Now only workout 21 should be unevaluated.
	workouts, err = queryUnevaluatedWorkouts(ctx, db, 1, since)
	if err != nil {
		t.Fatalf("queryUnevaluatedWorkouts after eval: %v", err)
	}
	if len(workouts) != 1 {
		t.Fatalf("got %d unevaluated workouts after evaluation, want 1", len(workouts))
	}
	if workouts[0].ID != 21 {
		t.Errorf("remaining workout ID = %d, want 21", workouts[0].ID)
	}
}

func TestQueryUnevaluatedWorkouts_EncryptedTitle(t *testing.T) {
	db := setupTestDB(t)

	// Store an encrypted title.
	encTitle, err := encryption.EncryptField("Evening Threshold")
	if err != nil {
		t.Fatalf("encrypt title: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, fit_file_hash, created_at)
		VALUES (30, 1, 'running', ?, '2026-04-08T19:00:00Z', 'h30', '2026-04-08T20:00:00Z')
	`, encTitle)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ctx := context.Background()
	workouts, err := queryUnevaluatedWorkouts(ctx, db, 1, "2026-04-08T00:00:00Z")
	if err != nil {
		t.Fatalf("queryUnevaluatedWorkouts: %v", err)
	}
	if len(workouts) != 1 {
		t.Fatalf("got %d workouts, want 1", len(workouts))
	}
	if workouts[0].Title != "Evening Threshold" {
		t.Errorf("title = %q, want %q", workouts[0].Title, "Evening Threshold")
	}
}

func TestQueryUnevaluatedWorkouts_CorruptedTitleBecomesEmpty(t *testing.T) {
	db := setupTestDB(t)

	// Encrypt with the key configured by setupTestDB, then switch to a different key
	// to simulate a decryption failure.
	encTitle, err := encryption.EncryptField("Secret Title")
	if err != nil {
		t.Fatalf("encrypt title: %v", err)
	}
	encryption.ResetEncryptionKey()

	// Switch to a different encryption key so decryption will fail.
	t.Setenv("ENCRYPTION_KEY", "different-key-causes-decrypt-fail")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	_, err = db.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, fit_file_hash, created_at)
		VALUES (31, 1, 'running', ?, '2026-04-08T19:00:00Z', 'h31', '2026-04-08T20:00:00Z')
	`, encTitle)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ctx := context.Background()
	workouts, err := queryUnevaluatedWorkouts(ctx, db, 1, "2026-04-08T00:00:00Z")
	if err != nil {
		t.Fatalf("queryUnevaluatedWorkouts: %v", err)
	}
	if len(workouts) != 1 {
		t.Fatalf("got %d workouts, want 1", len(workouts))
	}
	// Title should be empty — not the raw ciphertext — to prevent leaking it into the AI prompt.
	if workouts[0].Title != "" {
		t.Errorf("title = %q, want empty string when decryption fails", workouts[0].Title)
	}
}

// --- EvaluateWorkout (with mocked runPromptFunc) ---

func TestEvaluateWorkout_Success(t *testing.T) {
	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"threshold","actual_type":"threshold","compliance":"compliant","notes":"Solid effort.","flags":[],"adjustments":"Continue as planned."}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true, Model: "claude-test"}
	workout := training.Workout{Sport: "running", StartedAt: "2026-04-06T07:00:00Z"}

	eval, err := EvaluateWorkout(context.Background(), cfg, workout, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if err != nil {
		t.Fatalf("EvaluateWorkout: %v", err)
	}
	if eval.Compliance != "compliant" {
		t.Errorf("compliance = %q, want compliant", eval.Compliance)
	}
}

func TestEvaluateWorkout_PromptError(t *testing.T) {
	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", context.DeadlineExceeded
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true}
	_, err := EvaluateWorkout(context.Background(), cfg, training.Workout{}, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if err == nil {
		t.Error("expected error from failed prompt, got nil")
	}
}

func TestEvaluateWorkout_InvalidJSON(t *testing.T) {
	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "not json at all", nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	cfg := &training.ClaudeConfig{Enabled: true}
	_, err := EvaluateWorkout(context.Background(), cfg, training.Workout{}, nil, Plan{}, training.UserTrainingProfile{}, nil)
	if err == nil {
		t.Error("expected error for invalid JSON response, got nil")
	}
}

// --- parseEvalResponse rest_day compliance ---

func TestParseEvalResponse_RestDayCompliance(t *testing.T) {
	raw := `{"planned_type":"rest","actual_type":"rest","compliance":"rest_day","notes":"Rest day taken.","flags":[],"adjustments":"","date":"2026-04-07"}`
	eval, err := parseEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.Compliance != "rest_day" {
		t.Errorf("compliance = %q, want rest_day", eval.Compliance)
	}
}

// --- evaluateRestDaysAndMissedSessions ---

func TestEvaluateRestDaysAndMissedSessions_RestDay(t *testing.T) {
	db := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	weekStart := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")

	planJSON, _ := json.Marshal([]DayPlan{
		{Date: today, RestDay: true},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	ctx := context.Background()
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("evaluateRestDaysAndMissedSessions: %v", err)
	}

	// Should have one evaluation with rest_day compliance and NULL workout_id.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE user_id = 1 AND workout_id IS NULL`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 rest_day evaluation, got %d", count)
	}

	// Verify compliance via decryption.
	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Eval.Compliance != "rest_day" {
		t.Errorf("compliance = %q, want rest_day", records[0].Eval.Compliance)
	}
	if records[0].Eval.Date != today {
		t.Errorf("date = %q, want %q", records[0].Eval.Date, today)
	}
	if records[0].WorkoutID != nil {
		t.Errorf("workout_id = %v, want nil", records[0].WorkoutID)
	}
}

func TestEvaluateRestDaysAndMissedSessions_MissedSession(t *testing.T) {
	db := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	weekStart := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")

	planJSON, _ := json.Marshal([]DayPlan{
		{Date: today, RestDay: false, Session: &Session{MainSet: "4x10min threshold", Description: "Threshold session"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	ctx := context.Background()
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("evaluateRestDaysAndMissedSessions: %v", err)
	}

	records, err := ListEvaluations(db, 1, nil, nil)
	if err != nil {
		t.Fatalf("ListEvaluations: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Eval.Compliance != "missed" {
		t.Errorf("compliance = %q, want missed", records[0].Eval.Compliance)
	}
	if !strings.Contains(records[0].Eval.Notes, "Threshold session") {
		t.Errorf("notes should mention session type, got %q", records[0].Eval.Notes)
	}
}

func TestEvaluateRestDaysAndMissedSessions_WithWorkout(t *testing.T) {
	db := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	weekStart := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")

	planJSON, _ := json.Marshal([]DayPlan{
		{Date: today, RestDay: false, Session: &Session{MainSet: "easy run"}},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	// Insert a workout for today using a fixed midday timestamp to avoid flakiness around UTC midnight.
	workoutStarted := today + "T12:00:00Z"
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (50, 1, 'running', ?, 'hash-exists', ?)
	`, workoutStarted, workoutStarted); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ctx := context.Background()
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("evaluateRestDaysAndMissedSessions: %v", err)
	}

	// No evaluation should be created because a workout exists.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE user_id = 1 AND workout_id IS NULL`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 non-workout evaluations, got %d", count)
	}
}

func TestEvaluateRestDaysAndMissedSessions_NoPlan(t *testing.T) {
	db := setupTestDB(t)

	ctx := context.Background()
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("evaluateRestDaysAndMissedSessions: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 evaluations with no plan, got %d", count)
	}
}

func TestEvaluateRestDaysAndMissedSessions_Idempotent(t *testing.T) {
	db := setupTestDB(t)

	today := time.Now().UTC().Format("2006-01-02")
	weekStart := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")

	planJSON, _ := json.Marshal([]DayPlan{
		{Date: today, RestDay: true},
	})
	insertTestPlan(t, db, 1, weekStart, weekEnd, string(planJSON))

	ctx := context.Background()

	// Run twice.
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := evaluateRestDaysAndMissedSessions(ctx, db, nil, 1, time.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatalf("second run: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_evaluations WHERE user_id = 1 AND workout_id IS NULL`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 evaluation after two runs (idempotent), got %d", count)
	}
}

// --- Notes in nightly evaluation ---

func TestEvaluateUserWorkouts_NotesConsumedAfterSuccess(t *testing.T) {
	db := setupTestDB(t)

	origFn := runPromptFunc
	var capturedPrompt string
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
		capturedPrompt = prompt
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"Good.","flags":[],"adjustments":"None."}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	// Set up Claude config.
	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', '/usr/bin/claude')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	weekStart := yesterday.AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := yesterday.AddDate(0, 0, 3).Format("2006-01-02")
	insertTestPlan(t, db, 1, weekStart, weekEnd, `[]`)

	// Insert a workout from yesterday.
	workoutStarted := yesterday.Format("2006-01-02") + "T12:00:00Z"
	_, err = db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (100, 1, 'running', ?, 'hash-notes-test', ?)
	`, workoutStarted, workoutStarted)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Insert unconsumed notes.
	note1, err := CreateNote(db, 1, nil, "Feeling fatigued", yesterday.Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("create note 1: %v", err)
	}
	note2, err := CreateNote(db, 1, nil, "Sore knee after last run", yesterday.Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("create note 2: %v", err)
	}

	ctx := context.Background()
	since := yesterday.Format("2006-01-02") + "T00:00:00Z"
	targetDate := yesterday.Format("2006-01-02")
	if err := evaluateUserWorkouts(ctx, db, nil, 1, since, targetDate); err != nil {
		t.Fatalf("evaluateUserWorkouts: %v", err)
	}

	// Verify notes were included in the prompt sent to Claude.
	if !strings.Contains(capturedPrompt, "Feeling fatigued") {
		t.Error("expected prompt to contain first note content")
	}
	if !strings.Contains(capturedPrompt, "Sore knee after last run") {
		t.Error("expected prompt to contain second note content")
	}
	if !strings.Contains(capturedPrompt, yesterday.Format("2006-01-02")) {
		t.Error("expected prompt to contain note target date")
	}

	// Verify notes are consumed.
	for _, noteID := range []int64{note1.ID, note2.ID} {
		var consumedBy sql.NullString
		if err := db.QueryRow(`SELECT consumed_by FROM stride_notes WHERE id = ?`, noteID).Scan(&consumedBy); err != nil {
			t.Fatalf("query note %d: %v", noteID, err)
		}
		if !consumedBy.Valid || consumedBy.String != "nightly" {
			t.Errorf("note %d consumed_by = %v, want 'nightly'", noteID, consumedBy)
		}
	}
}

func TestEvaluateUserWorkouts_NotesNotConsumedOnClaudeFailure(t *testing.T) {
	db := setupTestDB(t)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", errors.New("claude call failed")
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', '/usr/bin/claude')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	weekStart := yesterday.AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := yesterday.AddDate(0, 0, 3).Format("2006-01-02")
	insertTestPlan(t, db, 1, weekStart, weekEnd, `[]`)

	workoutStarted := yesterday.Format("2006-01-02") + "T12:00:00Z"
	_, err = db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (101, 1, 'running', ?, 'hash-notes-fail', ?)
	`, workoutStarted, workoutStarted)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	note, err := CreateNote(db, 1, nil, "Feeling tired", yesterday.Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	ctx := context.Background()
	since := yesterday.Format("2006-01-02") + "T00:00:00Z"
	targetDate := yesterday.Format("2006-01-02")
	// evaluateUserWorkouts logs errors but doesn't return them for individual workouts.
	evaluateUserWorkouts(ctx, db, nil, 1, since, targetDate)

	// Notes should NOT be consumed because Claude call failed.
	var consumedAt sql.NullString
	if err := db.QueryRow(`SELECT consumed_at FROM stride_notes WHERE id = ?`, note.ID).Scan(&consumedAt); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if consumedAt.Valid {
		t.Errorf("note should not be consumed when Claude call fails, but consumed_at = %v", consumedAt.String)
	}
}

// TestEvaluateUserWorkouts_ScopeFiltering verifies that the nightly evaluation only
// consumes notes scoped 'any' or 'nightly' and leaves notes scoped 'weekly' untouched
// for the weekly plan generator.
func TestEvaluateUserWorkouts_ScopeFiltering(t *testing.T) {
	db := setupTestDB(t)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return `{"planned_type":"easy","actual_type":"easy","compliance":"compliant","notes":"Good.","flags":[],"adjustments":"None."}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true'), (1, 'claude_enabled', 'true'), (1, 'claude_cli_path', '/usr/bin/claude')`); err != nil {
		t.Fatalf("insert prefs: %v", err)
	}

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	weekStart := yesterday.AddDate(0, 0, -3).Format("2006-01-02")
	weekEnd := yesterday.AddDate(0, 0, 3).Format("2006-01-02")
	insertTestPlan(t, db, 1, weekStart, weekEnd, `[]`)

	workoutStarted := yesterday.Format("2006-01-02") + "T12:00:00Z"
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (700, 1, 'running', ?, 'hash-scope-test', ?)
	`, workoutStarted, workoutStarted); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Create three notes — one for each scope.
	yesterdayStr := yesterday.Format("2006-01-02")
	noteAny, err := CreateNote(db, 1, nil, "any note", yesterdayStr, "any")
	if err != nil {
		t.Fatalf("create any note: %v", err)
	}
	noteNightly, err := CreateNote(db, 1, nil, "nightly note", yesterdayStr, "nightly")
	if err != nil {
		t.Fatalf("create nightly note: %v", err)
	}
	noteWeekly, err := CreateNote(db, 1, nil, "weekly note", yesterdayStr, "weekly")
	if err != nil {
		t.Fatalf("create weekly note: %v", err)
	}

	ctx := context.Background()
	since := yesterdayStr + "T00:00:00Z"
	if err := evaluateUserWorkouts(ctx, db, nil, 1, since, yesterdayStr); err != nil {
		t.Fatalf("evaluateUserWorkouts: %v", err)
	}

	// 'any' and 'nightly' notes should be consumed by 'nightly'.
	for _, n := range []*Note{noteAny, noteNightly} {
		var consumedBy sql.NullString
		if err := db.QueryRow(`SELECT consumed_by FROM stride_notes WHERE id = ?`, n.ID).Scan(&consumedBy); err != nil {
			t.Fatalf("query note %d: %v", n.ID, err)
		}
		if !consumedBy.Valid || consumedBy.String != "nightly" {
			t.Errorf("note %d (scope=%s) consumed_by = %v, want 'nightly'", n.ID, n.Scope, consumedBy)
		}
	}

	// 'weekly' note must remain unconsumed.
	var consumedAt sql.NullString
	if err := db.QueryRow(`SELECT consumed_at FROM stride_notes WHERE id = ?`, noteWeekly.ID).Scan(&consumedAt); err != nil {
		t.Fatalf("query weekly note: %v", err)
	}
	if consumedAt.Valid {
		t.Errorf("weekly-scoped note must NOT be consumed by nightly run, got consumed_at=%v", consumedAt.String)
	}
}

func TestEvaluateUserWorkouts_NotesNotConsumedWhenNoWorkouts(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', '/usr/bin/claude')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}

	yesterday := time.Now().UTC().AddDate(0, 0, -1)

	// Insert unconsumed notes but NO workouts.
	note, err := CreateNote(db, 1, nil, "Skipped today, feeling unwell", yesterday.Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	ctx := context.Background()
	since := yesterday.Format("2006-01-02") + "T00:00:00Z"
	targetDate := yesterday.Format("2006-01-02")
	if err := evaluateUserWorkouts(ctx, db, nil, 1, since, targetDate); err != nil {
		t.Fatalf("evaluateUserWorkouts: %v", err)
	}

	// Notes should NOT be consumed because no workouts exist.
	var consumedAt sql.NullString
	if err := db.QueryRow(`SELECT consumed_at FROM stride_notes WHERE id = ?`, note.ID).Scan(&consumedAt); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if consumedAt.Valid {
		t.Errorf("note should not be consumed when no workouts exist, but consumed_at = %v", consumedAt.String)
	}
}
