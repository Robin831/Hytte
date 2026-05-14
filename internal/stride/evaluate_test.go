package stride

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

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

// --- appendWorkoutContextNote ---

func TestAppendWorkoutContextNote_FramesAsPostWorkoutReport(t *testing.T) {
	db := setupTestDB(t)

	const workoutID = int64(600)
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (?, 1, 'running', '2026-04-08T07:00:00Z', 'append-ctx-hash', '2026-04-08T08:00:00Z')
	`, workoutID); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	feelEnc, err := encryption.EncryptField("Tough but controlled")
	if err != nil {
		t.Fatalf("encrypt feel: %v", err)
	}
	// Encode the speed plan with a non-empty entry so summarizeSpeedPlan emits
	// the new "Executed splits:" label and we can assert against it.
	planEnc, err := encryption.EncryptField(`[{"kind":"steady","speed_kmph":10,"duration_sec":1800,"repeats":1,"same_as_previous":false}]`)
	if err != nil {
		t.Fatalf("encrypt plan: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO workout_context (workout_id, surface, run_type, hr_source, feel_notes, speed_plan, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, '')`,
		workoutID, "Treadmill", "long", "chest", feelEnc, planEnc,
	); err != nil {
		t.Fatalf("insert workout_context: %v", err)
	}

	workout := training.Workout{ID: workoutID, UserID: 1, Sport: "running", StartedAt: "2026-04-08T07:00:00Z"}
	got := appendWorkoutContextNote(db, workout, "2026-04-08", nil)

	if len(got) != 1 {
		t.Fatalf("expected exactly one synthetic note appended, got %d", len(got))
	}
	note := got[0]
	if !strings.Contains(note.Content, "Runner's post-workout report") {
		t.Errorf("note content missing post-workout framing, got %q", note.Content)
	}
	if !strings.Contains(note.Content, "Executed splits:") {
		t.Errorf("note content missing 'Executed splits:' prefix, got %q", note.Content)
	}
	if strings.Contains(note.Content, "Plan:") {
		t.Errorf("note content must not include 'Plan:' prefix (misleads evaluator), got %q", note.Content)
	}
	if !strings.Contains(note.Content, "Feel notes: Tough but controlled") {
		t.Errorf("note content missing feel notes, got %q", note.Content)
	}
	if note.Scope != NoteScopeNightly {
		t.Errorf("expected nightly scope on synthetic note, got %q", note.Scope)
	}
	if note.TargetDate != "2026-04-08" {
		t.Errorf("expected target_date=2026-04-08, got %q", note.TargetDate)
	}
}

func TestAppendWorkoutContextNote_NoContextLeavesNotesUnchanged(t *testing.T) {
	db := setupTestDB(t)

	const workoutID = int64(601)
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (?, 1, 'running', '2026-04-09T07:00:00Z', 'append-ctx-none', '2026-04-09T08:00:00Z')
	`, workoutID); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	original := []Note{{ID: 1, Content: "Pre-existing note", TargetDate: "2026-04-09"}}
	workout := training.Workout{ID: workoutID, UserID: 1, Sport: "running"}
	got := appendWorkoutContextNote(db, workout, "2026-04-09", original)

	if len(got) != 1 {
		t.Fatalf("expected notes unchanged when no context row exists, got %d", len(got))
	}
	if got[0].Content != "Pre-existing note" {
		t.Errorf("expected original note preserved, got %q", got[0].Content)
	}
}

