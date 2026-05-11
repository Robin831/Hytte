package training

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

func createWorkoutForUser(t *testing.T, database *sql.DB, userID int64, hash string) int64 {
	t.Helper()
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}
	workout, err := Create(database, userID, pw, hash)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return workout.ID
}

func putContext(t *testing.T, database *sql.DB, userID, workoutID int64, body WorkoutContext) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", bytes.NewReader(raw))
	req = withUser(req, userID)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	PutWorkoutContextHandler(database)(w, req)
	return w
}

func getContext(t *testing.T, database *sql.DB, userID, workoutID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", nil)
	req = withUser(req, userID)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	GetWorkoutContextHandler(database)(w, req)
	return w
}

func decodeContext(t *testing.T, body []byte) WorkoutContext {
	t.Helper()
	var wrapper struct {
		Context WorkoutContext `json:"context"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		t.Fatalf("unmarshal context: %v (body=%s)", err, string(body))
	}
	return wrapper.Context
}

func TestPutWorkoutContext_CreatesAndDecryptsRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-create-hash")

	body := WorkoutContext{
		Surface:   "trail",
		RunType:   "tempo",
		HRSource:  "chest_strap",
		FeelNotes: "Felt strong on the climbs, legs heavy late.",
		SpeedPlan: []SpeedSegment{
			{Kind: "warmup", SpeedKmph: 9.0, DurationSec: 600, Repeats: 1},
			{Kind: "work", SpeedKmph: 14.5, DurationSec: 180, Repeats: 6},
			{Kind: "recovery", SpeedKmph: 8.0, DurationSec: 90, Repeats: 6, SameAsPrevious: true},
		},
	}

	w := putContext(t, database, 1, workoutID, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	saved := decodeContext(t, w.Body.Bytes())
	if saved.WorkoutID != workoutID {
		t.Fatalf("expected workout_id=%d, got %d", workoutID, saved.WorkoutID)
	}
	if saved.FeelNotes != body.FeelNotes {
		t.Fatalf("feel_notes round-trip failed: %q vs %q", saved.FeelNotes, body.FeelNotes)
	}
	if len(saved.SpeedPlan) != len(body.SpeedPlan) {
		t.Fatalf("speed_plan length mismatch: got %d, want %d", len(saved.SpeedPlan), len(body.SpeedPlan))
	}
	if saved.SpeedPlan[1].Kind != "work" || saved.SpeedPlan[1].SpeedKmph != 14.5 {
		t.Fatalf("speed_plan[1] mismatch: %+v", saved.SpeedPlan[1])
	}
	if !saved.SpeedPlan[2].SameAsPrevious {
		t.Fatalf("expected same_as_previous=true on segment 2, got %+v", saved.SpeedPlan[2])
	}

	// GET should return the same data.
	gw := getContext(t, database, 1, workoutID)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	got := decodeContext(t, gw.Body.Bytes())
	if got.FeelNotes != body.FeelNotes {
		t.Fatalf("GET feel_notes mismatch: %q vs %q", got.FeelNotes, body.FeelNotes)
	}

	// Verify ciphertext at rest — the raw DB value must not contain the plaintext.
	var feelEnc, planEnc string
	err := database.QueryRow(`SELECT feel_notes, speed_plan FROM workout_context WHERE workout_id = ?`, workoutID).Scan(&feelEnc, &planEnc)
	if err != nil {
		t.Fatalf("query encrypted columns: %v", err)
	}
	if strings.Contains(feelEnc, body.FeelNotes) {
		t.Fatalf("feel_notes stored in plaintext: %s", feelEnc)
	}
	if !strings.HasPrefix(feelEnc, "enc:") {
		t.Fatalf("expected feel_notes to be encrypted (enc: prefix), got %q", feelEnc)
	}
	if !strings.HasPrefix(planEnc, "enc:") {
		t.Fatalf("expected speed_plan to be encrypted (enc: prefix), got %q", planEnc)
	}
	if strings.Contains(planEnc, "warmup") || strings.Contains(planEnc, "tempo") {
		t.Fatalf("speed_plan stored in plaintext: %s", planEnc)
	}
}

func TestPutWorkoutContext_UpdatesExistingRow(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-update-hash")

	first := WorkoutContext{
		Surface:   "road",
		RunType:   "easy",
		HRSource:  "wrist",
		FeelNotes: "First note",
		SpeedPlan: []SpeedSegment{{Kind: "steady", SpeedKmph: 10.0, DurationSec: 1800, Repeats: 1}},
	}
	if w := putContext(t, database, 1, workoutID, first); w.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d", w.Code)
	}

	second := WorkoutContext{
		Surface:   "trail",
		RunType:   "long",
		HRSource:  "chest_strap",
		FeelNotes: "Updated note",
		SpeedPlan: []SpeedSegment{{Kind: "steady", SpeedKmph: 9.5, DurationSec: 3600, Repeats: 1}},
	}
	w := putContext(t, database, 1, workoutID, second)
	if w.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	gw := getContext(t, database, 1, workoutID)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	got := decodeContext(t, gw.Body.Bytes())
	if got.Surface != "trail" || got.RunType != "long" || got.HRSource != "chest_strap" {
		t.Fatalf("plain fields not updated: %+v", got)
	}
	if got.FeelNotes != "Updated note" {
		t.Fatalf("feel_notes not updated: %q", got.FeelNotes)
	}
	if len(got.SpeedPlan) != 1 || got.SpeedPlan[0].SpeedKmph != 9.5 || got.SpeedPlan[0].DurationSec != 3600 {
		t.Fatalf("speed_plan not updated: %+v", got.SpeedPlan)
	}

	// Confirm only one row exists.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM workout_context WHERE workout_id = ?`, workoutID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 context row after update, got %d", count)
	}
}

func TestGetWorkoutContext_MissingContextReturns404(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-missing-hash")

	w := getContext(t, database, 1, workoutID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing context, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetWorkoutContext_DifferentUserGets404(t *testing.T) {
	database := setupTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	workoutID := createWorkoutForUser(t, database, 1, "ctx-cross-user-hash")

	// User 1 saves context.
	body := WorkoutContext{Surface: "road", RunType: "easy", HRSource: "wrist", FeelNotes: "private"}
	if w := putContext(t, database, 1, workoutID, body); w.Code != http.StatusOK {
		t.Fatalf("PUT (user 1): expected 200, got %d", w.Code)
	}

	// User 2 must not be able to read or write that workout.
	if w := getContext(t, database, 2, workoutID); w.Code != http.StatusNotFound {
		t.Fatalf("GET (user 2): expected 404, got %d", w.Code)
	}
	if w := putContext(t, database, 2, workoutID, body); w.Code != http.StatusNotFound {
		t.Fatalf("PUT (user 2): expected 404, got %d", w.Code)
	}
}

func TestPutWorkoutContext_InvalidJSON(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-invalid-json-hash")

	req := httptest.NewRequest(http.MethodPut, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", strings.NewReader("{not json"))
	req = withUser(req, 1)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	PutWorkoutContextHandler(database)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- scheduleAnalysisAfterContextSave ---

func setWorkoutAnalysisStatus(t *testing.T, database *sql.DB, workoutID int64, status string) {
	t.Helper()
	if _, err := database.Exec(`UPDATE workouts SET analysis_status = ? WHERE id = ?`, status, workoutID); err != nil {
		t.Fatalf("set analysis_status %q for workout %d: %v", status, workoutID, err)
	}
}

func getWorkoutAnalysisStatus(t *testing.T, database *sql.DB, workoutID int64) string {
	t.Helper()
	var status string
	if err := database.QueryRow(`SELECT analysis_status FROM workouts WHERE id = ?`, workoutID).Scan(&status); err != nil {
		t.Fatalf("get analysis_status for workout %d: %v", workoutID, err)
	}
	return status
}

// TestScheduleAnalysisAfterContextSave_Fires_WhenStatusEmpty verifies that
// Claude analysis is triggered (and status is claimed atomically) when the
// workout has never been analysed (analysis_status='').
func TestScheduleAnalysisAfterContextSave_Fires_WhenStatusEmpty(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-empty-hash")
	seedWorkoutContext(t, database, workoutID)
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	called := make(chan struct{}, 1)
	origFunc := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return `{"type":"easy_run","tag":"easy","summary":"Test","title":"Test Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleAnalysisAfterContextSave(database, 1, true, workoutID)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: analysis did not fire within 2s for status=''")
	}
}

// TestScheduleAnalysisAfterContextSave_Fires_WhenStatusFailed verifies that
// Claude analysis is re-triggered when the previous run failed.
func TestScheduleAnalysisAfterContextSave_Fires_WhenStatusFailed(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-failed-hash")
	setWorkoutAnalysisStatus(t, database, workoutID, "failed")
	seedWorkoutContext(t, database, workoutID)
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	called := make(chan struct{}, 1)
	origFunc := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return `{"type":"easy_run","tag":"easy","summary":"Test","title":"Test Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleAnalysisAfterContextSave(database, 1, true, workoutID)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: analysis did not fire within 2s for status='failed'")
	}
}

// TestScheduleAnalysisAfterContextSave_Skips_WhenStatusPending verifies that
// the atomic UPDATE returns 0 rows (no goroutine spawned) when analysis is
// already in progress.
func TestScheduleAnalysisAfterContextSave_Skips_WhenStatusPending(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-pending-hash")
	setWorkoutAnalysisStatus(t, database, workoutID, "pending")

	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleAnalysisAfterContextSave(database, 1, true, workoutID)
	time.Sleep(150 * time.Millisecond)

	if called {
		t.Fatal("expected no Claude call when analysis_status='pending'")
	}
	if status := getWorkoutAnalysisStatus(t, database, workoutID); status != "pending" {
		t.Errorf("status should remain 'pending', got %q", status)
	}
}

// TestScheduleAnalysisAfterContextSave_Skips_WhenStatusCompleted verifies that
// a completed analysis is not re-run.
func TestScheduleAnalysisAfterContextSave_Skips_WhenStatusCompleted(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-completed-hash")
	setWorkoutAnalysisStatus(t, database, workoutID, "completed")

	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleAnalysisAfterContextSave(database, 1, true, workoutID)
	time.Sleep(150 * time.Millisecond)

	if called {
		t.Fatal("expected no Claude call when analysis_status='completed'")
	}
	if status := getWorkoutAnalysisStatus(t, database, workoutID); status != "completed" {
		t.Errorf("status should remain 'completed', got %q", status)
	}
}

// TestScheduleAnalysisAfterContextSave_Skips_WhenNotAdmin verifies that the
// function is a no-op for non-admin users.
func TestScheduleAnalysisAfterContextSave_Skips_WhenNotAdmin(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-nonadmin-hash")

	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleAnalysisAfterContextSave(database, 1, false, workoutID)
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("expected no Claude call for non-admin user")
	}
}

// TestScheduleAnalysisAfterContextSave_SetsStatusPendingBeforeRun verifies that
// the atomic UPDATE claims 'pending' synchronously (before the goroutine body
// executes), so a second concurrent call sees the updated status and does not
// enqueue a duplicate run.
func TestScheduleAnalysisAfterContextSave_SetsStatusPendingBeforeRun(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "saa-claim-hash")
	seedWorkoutContext(t, database, workoutID)
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	origFunc := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		defer wg.Done()
		<-release
		return `{"type":"easy_run","tag":"easy","summary":"Test","title":"Test Run"}`, nil
	}
	t.Cleanup(func() {
		close(release)
		wg.Wait()
		runPromptFunc = origFunc
	})

	scheduleAnalysisAfterContextSave(database, 1, true, workoutID)

	// The atomic UPDATE is synchronous — status must be 'pending' before the
	// goroutine finishes and flips it to 'completed'.
	if status := getWorkoutAnalysisStatus(t, database, workoutID); status != "pending" {
		t.Errorf("expected status='pending' immediately after scheduling, got %q", status)
	}
}

// --- scheduleStrideEvalAfterContextSave ---

// setStrideEvalHook swaps OnContextSavedReevaluateStride for the duration of a
// test and restores the previous value (typically nil) on cleanup. Returning
// the swap helper keeps each test focused on its assertions.
func setStrideEvalHook(t *testing.T, hook func(context.Context, *sql.DB, *http.Client, int64, string) (int, error)) {
	t.Helper()
	orig := OnContextSavedReevaluateStride
	OnContextSavedReevaluateStride = hook
	t.Cleanup(func() { OnContextSavedReevaluateStride = orig })
}

// TestScheduleStrideEvalAfterContextSave_Fires verifies the trigger spawns a
// re-evaluation when both stride_enabled and Claude are on, and that the date
// passed to the hook is the workout's started_at converted to Europe/Oslo —
// not the UTC date. 2024-06-15T22:30:00Z is still 2024-06-15 in UTC but
// 2024-06-16 in CEST, so a UTC-only path would pass the wrong date.
func TestScheduleStrideEvalAfterContextSave_Fires(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "stride-fires-hash")
	if _, err := database.Exec(`UPDATE workouts SET started_at = ? WHERE id = ?`,
		"2024-06-15T22:30:00Z", workoutID); err != nil {
		t.Fatalf("update started_at: %v", err)
	}
	if err := auth.SetPreference(database, 1, "stride_enabled", "true"); err != nil {
		t.Fatalf("set stride_enabled: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set claude_enabled: %v", err)
	}

	type capture struct {
		userID int64
		date   string
	}
	captured := make(chan capture, 1)
	setStrideEvalHook(t, func(_ context.Context, _ *sql.DB, _ *http.Client, userID int64, date string) (int, error) {
		captured <- capture{userID: userID, date: date}
		return 1, nil
	})

	scheduleStrideEvalAfterContextSave(database, 1, workoutID)

	select {
	case c := <-captured:
		if c.userID != 1 {
			t.Errorf("user_id: got %d, want 1", c.userID)
		}
		if c.date != "2024-06-16" {
			t.Errorf("date: got %q, want %q (Europe/Oslo)", c.date, "2024-06-16")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: stride re-evaluation did not fire within 2s")
	}
}

// TestScheduleStrideEvalAfterContextSave_Skips_WhenStrideNotEnabled verifies
// the trigger is a no-op when the user has not opted in to Stride, even if
// Claude is enabled.
func TestScheduleStrideEvalAfterContextSave_Skips_WhenStrideNotEnabled(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "stride-no-stride-hash")
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set claude_enabled: %v", err)
	}

	called := false
	setStrideEvalHook(t, func(_ context.Context, _ *sql.DB, _ *http.Client, _ int64, _ string) (int, error) {
		called = true
		return 0, nil
	})

	scheduleStrideEvalAfterContextSave(database, 1, workoutID)
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("expected no stride evaluation when stride_enabled is false")
	}
}

// TestScheduleStrideEvalAfterContextSave_Skips_WhenClaudeNotEnabled verifies
// the trigger is a no-op when Claude is disabled, matching the nightly cron's
// gating behavior.
func TestScheduleStrideEvalAfterContextSave_Skips_WhenClaudeNotEnabled(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "stride-no-claude-hash")
	if err := auth.SetPreference(database, 1, "stride_enabled", "true"); err != nil {
		t.Fatalf("set stride_enabled: %v", err)
	}

	called := false
	setStrideEvalHook(t, func(_ context.Context, _ *sql.DB, _ *http.Client, _ int64, _ string) (int, error) {
		called = true
		return 0, nil
	})

	scheduleStrideEvalAfterContextSave(database, 1, workoutID)
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("expected no stride evaluation when Claude is not enabled")
	}
}

// TestScheduleStrideEvalAfterContextSave_NoOpWhenHookUnset verifies the
// trigger is safe to call when no hook is registered (e.g. unit tests that
// don't go through the router wiring).
func TestScheduleStrideEvalAfterContextSave_NoOpWhenHookUnset(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "stride-no-hook-hash")
	if err := auth.SetPreference(database, 1, "stride_enabled", "true"); err != nil {
		t.Fatalf("set stride_enabled: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set claude_enabled: %v", err)
	}
	setStrideEvalHook(t, nil)

	// Should not panic or block.
	scheduleStrideEvalAfterContextSave(database, 1, workoutID)
}

// TestPutWorkoutContext_StrideHookFailure_StillReturns200 verifies the HTTP
// save succeeds even if the Stride re-evaluation hook returns an error —
// evaluation runs in a detached goroutine and failures must be logged, not
// propagated.
func TestPutWorkoutContext_StrideHookFailure_StillReturns200(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "stride-failure-hash")
	if err := auth.SetPreference(database, 1, "stride_enabled", "true"); err != nil {
		t.Fatalf("set stride_enabled: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set claude_enabled: %v", err)
	}

	hookCalled := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	setStrideEvalHook(t, func(_ context.Context, _ *sql.DB, _ *http.Client, _ int64, _ string) (int, error) {
		defer wg.Done()
		select {
		case hookCalled <- struct{}{}:
		default:
		}
		return 0, errors.New("simulated evaluator failure")
	})

	body := WorkoutContext{
		Surface:   "road",
		RunType:   "easy",
		HRSource:  "wrist",
		FeelNotes: "Stride trigger failure-path test",
	}
	w := putContext(t, database, 1, workoutID, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from PUT despite hook failure, got %d (body=%s)", w.Code, w.Body.String())
	}

	select {
	case <-hookCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: stride hook never fired")
	}
	wg.Wait()
}
