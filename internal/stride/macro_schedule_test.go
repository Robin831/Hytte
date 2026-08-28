package stride

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
)

// macroGenerateCall records one call into the GenerateMacroPlan seam, so a test
// can assert on *what* would have been generated without a Claude call.
type macroGenerateCall struct {
	userID    int64
	startWeek string
	mode      MacroMode
}

// stubMacroGenerate replaces the generation seam. Every call is recorded and
// answered with err; the returned pointer is the call log.
func stubMacroGenerate(t *testing.T, err error) *[]macroGenerateCall {
	t.Helper()
	calls := []macroGenerateCall{}
	orig := generateMacroPlanFunc
	generateMacroPlanFunc = func(_ context.Context, _ *sql.DB, userID int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
		calls = append(calls, macroGenerateCall{userID: userID, startWeek: startWeek, mode: mode})
		if err != nil {
			return nil, err
		}
		return &MacroPlan{UserID: userID, StartWeek: startWeek, GeneratedBy: string(mode)}, nil
	}
	t.Cleanup(func() { generateMacroPlanFunc = orig })
	return &calls
}

// insertMacroPlanSpan writes just the horizon columns of a block. EnsureMacroPlan
// only reads the span and the lineage id, so the AI-authored blobs are left
// empty — decryptJSON treats an empty column as "not set".
func insertMacroPlanSpan(t *testing.T, db *sql.DB, userID int64, startWeek string, weeks int, status string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO stride_macro_plans (user_id, start_week, end_week, status, model, generated_by, created_at)
		VALUES (?, ?, ?, ?, 'claude-opus-5', ?, '2026-01-01T00:00:00Z')`,
		userID, startWeek, mondayAfter(startWeek, weeks-1), status, MacroGeneratedByScheduled,
	)
	if err != nil {
		t.Fatalf("insert macro plan span: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("macro plan span last insert id: %v", err)
	}
	return id
}

func mustMonday(t *testing.T, date string) time.Time {
	t.Helper()
	monday, err := parseMondayWeek(date)
	if err != nil {
		t.Fatalf("parse Monday %q: %v", date, err)
	}
	return monday
}

func TestWeeksRemaining(t *testing.T) {
	const from = "2026-08-31"
	tests := []struct {
		name    string
		endWeek string
		want    int
	}{
		{"same week", from, 0},
		{"exactly the extension lead", mondayAfter(from, MacroExtensionLeadWeeks), MacroExtensionLeadWeeks},
		{"one week past the lead", mondayAfter(from, MacroExtensionLeadWeeks+1), MacroExtensionLeadWeeks + 1},
		{"full block", mondayAfter(from, MacroBlockWeeks-1), MacroBlockWeeks - 1},
		{"already ended", mondayAfter(from, -3), -3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weeksRemaining(mustMonday(t, from), mustMonday(t, tt.endWeek))
			if got != tt.want {
				t.Errorf("weeksRemaining(%s, %s) = %d, want %d", from, tt.endWeek, got, tt.want)
			}
		})
	}
}

// A first-time athlete has nothing to extend, so the horizon is opened with a
// scheduled block starting on the week the plan step is about to materialise.
func TestEnsureMacroPlanGeneratesInitialBlock(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)

	const nextMonday = "2026-08-31"
	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("generate calls = %d, want 1", len(*calls))
	}
	got := (*calls)[0]
	if got.startWeek != nextMonday {
		t.Errorf("start week = %q, want %q", got.startWeek, nextMonday)
	}
	if got.mode != MacroModeScheduled {
		t.Errorf("mode = %q, want %q", got.mode, MacroModeScheduled)
	}
}

// A superseded block is not a horizon: it must not suppress the initial
// generation the athlete otherwise has no plan from.
func TestEnsureMacroPlanIgnoresSupersededBlock(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)

	const nextMonday = "2026-08-31"
	insertMacroPlanSpan(t, db, 1, nextMonday, MacroBlockWeeks, MacroPlanStatusSuperseded)

	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("generate calls = %d, want 1", len(*calls))
	}
	if (*calls)[0].mode != MacroModeScheduled {
		t.Errorf("mode = %q, want %q", (*calls)[0].mode, MacroModeScheduled)
	}
}

// A fresh block has 25 weeks of runway; ensuring it every Monday must not cost
// a Claude call until the horizon actually runs low.
func TestEnsureMacroPlanNoOpWithRunway(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)

	const blockStart = "2026-08-31"
	insertMacroPlanSpan(t, db, 1, blockStart, MacroBlockWeeks, MacroPlanStatusActive)

	// End week is blockStart+25w, so this Monday leaves 9 weeks — one more than
	// the lead time.
	nextMonday := mondayAfter(blockStart, MacroBlockWeeks-1-MacroExtensionLeadWeeks-1)
	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("generate calls = %d, want 0 (%+v)", len(*calls), *calls)
	}
}

// Exactly MacroExtensionLeadWeeks weeks left is inside the window, not outside
// it — the boundary the whole schedule hinges on.
func TestEnsureMacroPlanExtendsAtLeadBoundary(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)

	const blockStart = "2026-08-31"
	planID := insertMacroPlanSpan(t, db, 1, blockStart, MacroBlockWeeks, MacroPlanStatusActive)
	endWeek := mondayAfter(blockStart, MacroBlockWeeks-1)

	nextMonday := mondayAfter(endWeek, -MacroExtensionLeadWeeks)
	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("generate calls = %d, want 1", len(*calls))
	}
	got := (*calls)[0]
	if want := mondayAfter(endWeek, 1); got.startWeek != want {
		t.Errorf("start week = %q, want %q (the Monday after plan %d ends)", got.startWeek, want, planID)
	}
	if got.mode != MacroModeExtension {
		t.Errorf("mode = %q, want %q", got.mode, MacroModeExtension)
	}
}

// The successor already exists — a restart or a second trigger on the same
// Monday must find it and spend nothing.
func TestEnsureMacroPlanNoOpWhenExtensionExists(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)

	const blockStart = "2026-08-31"
	insertMacroPlanSpan(t, db, 1, blockStart, MacroBlockWeeks, MacroPlanStatusActive)
	endWeek := mondayAfter(blockStart, MacroBlockWeeks-1)
	insertMacroPlanSpan(t, db, 1, mondayAfter(endWeek, 1), MacroBlockWeeks, MacroPlanStatusActive)

	nextMonday := mondayAfter(endWeek, -2)
	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("generate calls = %d, want 0 (%+v)", len(*calls), *calls)
	}
}

// The unique index on (user_id, start_week) WHERE status='active' is the
// backstop for two runs that pass the check-before-call at the same instant.
// It surfaces as ErrOverlappingMacroPlan and means "already done", not "failed".
func TestEnsureMacroPlanTreatsOverlapAsDone(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, fmt.Errorf("persist macro plan: %w", ErrOverlappingMacroPlan))

	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, "2026-08-31")); err != nil {
		t.Fatalf("EnsureMacroPlan should swallow an overlap, got: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("generate calls = %d, want 1 (no retry)", len(*calls))
	}
}

// Any other generation failure is the caller's to hear about — RunWeekly turns
// it into the "open Stride to retry" push.
func TestEnsureMacroPlanPropagatesGenerationError(t *testing.T) {
	db := setupTestDB(t)
	boom := errors.New("claude exploded")
	calls := stubMacroGenerate(t, boom)

	err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, "2026-08-31"))
	if !errors.Is(err, boom) {
		t.Fatalf("EnsureMacroPlan error = %v, want it to wrap %v", err, boom)
	}
	if len(*calls) != 1 {
		t.Fatalf("generate calls = %d, want 1 (no retry loop)", len(*calls))
	}
}

// Another athlete's block is not this athlete's horizon.
func TestEnsureMacroPlanScopedToUser(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	calls := stubMacroGenerate(t, nil)

	const nextMonday = "2026-08-31"
	insertMacroPlanSpan(t, db, 2, nextMonday, MacroBlockWeeks, MacroPlanStatusActive)

	if err := EnsureMacroPlan(context.Background(), db, 1, mustMonday(t, nextMonday)); err != nil {
		t.Fatalf("EnsureMacroPlan: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].userID != 1 {
		t.Fatalf("generate calls = %+v, want one call for user 1", *calls)
	}
}

// Two runs for one athlete must not overlap: the second waits for the first.
func TestLockUserSerialisesSameUser(t *testing.T) {
	const userID = int64(9001)
	var mu sync.Mutex
	inside := 0
	maxInside := 0

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer LockUser(userID)()

			mu.Lock()
			inside++
			if inside > maxInside {
				maxInside = inside
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if maxInside != 1 {
		t.Errorf("max concurrent holders = %d, want 1", maxInside)
	}
}

// Different athletes are independent: one long run must not stall everybody
// else's Monday.
func TestLockUserDoesNotBlockOtherUsers(t *testing.T) {
	held := LockUser(9002)
	defer held()

	done := make(chan struct{})
	go func() {
		defer close(done)
		LockUser(9003)()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("LockUser(9003) blocked while 9002 was held")
	}
}

// stubWeeklyRunSeams replaces the prediction refresh and the push sender, and
// returns the notifications the run emitted.
func stubWeeklyRunSeams(t *testing.T) *[]push.Notification {
	t.Helper()
	sent := []push.Notification{}

	origRefresh := refreshRacePredictionFunc
	refreshRacePredictionFunc = func(_ context.Context, _ *sql.DB, _ int64, _ *training.ClaudeConfig) (*training.StoredRacePrediction, error) {
		return nil, nil
	}
	t.Cleanup(func() { refreshRacePredictionFunc = origRefresh })

	origPush := sendPushFunc
	sendPushFunc = func(_ *sql.DB, _ int64, notif push.Notification) error {
		sent = append(sent, notif)
		return nil
	}
	t.Cleanup(func() { sendPushFunc = origPush })

	return &sent
}

// The happy path: the horizon is already long enough, the week is planned, and
// the athlete gets the ordinary "plan is ready" push.
func TestRunWeeklyHappyPath(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	stubMacroGenerate(t, nil)
	sent := stubWeeklyRunSeams(t)

	// A block covering the coming week with a full 26-week horizon leaves
	// EnsureMacroPlan nothing to do.
	insertMacroPlanSpan(t, db, 1, weekStart, MacroBlockWeeks, MacroPlanStatusActive)

	if err := RunWeekly(context.Background(), db, 1); err != nil {
		t.Fatalf("RunWeekly: %v", err)
	}

	var planCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1 AND week_start = ?", weekStart).Scan(&planCount); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if planCount != 1 {
		t.Errorf("stored plans for %s = %d, want 1", weekStart, planCount)
	}
	if len(*sent) != 1 {
		t.Fatalf("notifications = %d, want 1 (%+v)", len(*sent), *sent)
	}
	if (*sent)[0].Body != weeklyPlanReadyBody {
		t.Errorf("push body = %q, want %q", (*sent)[0].Body, weeklyPlanReadyBody)
	}
}

// A failed block generation must not cost the athlete their week: the weekly
// plan still generates, the run still succeeds, and the push is what tells them
// the long-term plan needs a manual retry.
func TestRunWeeklyMacroFailureStillPlansWeek(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	calls := stubMacroGenerate(t, errors.New("claude exploded"))
	sent := stubWeeklyRunSeams(t)

	if err := RunWeekly(context.Background(), db, 1); err != nil {
		t.Fatalf("RunWeekly should survive a macro failure, got: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("macro generate calls = %d, want 1 (no automatic retry)", len(*calls))
	}

	var planCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1 AND week_start = ?", weekStart).Scan(&planCount); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if planCount != 1 {
		t.Errorf("stored plans for %s = %d, want 1 — the weekly step must still run", weekStart, planCount)
	}

	if len(*sent) != 1 {
		t.Fatalf("notifications = %d, want 1 (%+v)", len(*sent), *sent)
	}
	if (*sent)[0].Body != macroFailedBody {
		t.Errorf("push body = %q, want %q", (*sent)[0].Body, macroFailedBody)
	}
}

// A failed weekly plan is the one failure that fails the run, and it must not
// tell the athlete a plan is waiting for them.
func TestRunWeeklyWeeklyFailureReturnsErrorAndSendsNoPush(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	stubMacroGenerate(t, nil)
	sent := stubWeeklyRunSeams(t)

	weekStart, _ := upcomingWeek()
	insertMacroPlanSpan(t, db, 1, weekStart, MacroBlockWeeks, MacroPlanStatusActive)

	boom := errors.New("claude timed out")
	origPrompt := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", boom
	}
	t.Cleanup(func() { runPromptFunc = origPrompt })

	if err := RunWeekly(context.Background(), db, 1); !errors.Is(err, boom) {
		t.Fatalf("RunWeekly error = %v, want it to wrap %v", err, boom)
	}
	if len(*sent) != 0 {
		t.Errorf("notifications = %+v, want none when the week could not be planned", *sent)
	}
}

// A request-driven caller must not park behind a multi-minute run: TryLockUser
// reports the lock as taken instead of waiting for it, and hands it over once
// the holder is done.
func TestTryLockUserFailsWhileHeldAndSucceedsAfter(t *testing.T) {
	release := LockUser(9004)

	if _, ok := TryLockUser(9004); ok {
		t.Fatal("TryLockUser succeeded while the lock was held")
	}
	if other, ok := TryLockUser(9005); !ok {
		t.Error("TryLockUser blocked on an unrelated user")
	} else {
		other()
	}

	release()

	got, ok := TryLockUser(9004)
	if !ok {
		t.Fatal("TryLockUser failed after the lock was released")
	}
	got()
}

// A macro step that fails because the athlete never enabled Stride or Claude is
// a configuration state, not a generation failure — pushing "open Stride and
// retry" would send them after something they cannot fix from there.
func TestRunWeeklyNotEnabledMacroErrorSkipsFailurePush(t *testing.T) {
	for name, macroErr := range map[string]error{
		"stride not enabled": ErrStrideNotEnabled,
		"claude not enabled": training.ErrClaudeNotEnabled,
	} {
		t.Run(name, func(t *testing.T) {
			db := extendedTestDB(t)
			enableStride(t, db, 1)
			weekStart, _ := upcomingWeek()
			mockClaude(t, weekStart)
			stubMacroGenerate(t, macroErr)
			sent := stubWeeklyRunSeams(t)

			if err := RunWeekly(context.Background(), db, 1); err != nil {
				t.Fatalf("RunWeekly: %v", err)
			}

			if len(*sent) != 1 {
				t.Fatalf("notifications = %d, want 1 (%+v)", len(*sent), *sent)
			}
			if (*sent)[0].Body != weeklyPlanReadyBody {
				t.Errorf("push body = %q, want the ordinary %q", (*sent)[0].Body, weeklyPlanReadyBody)
			}
		})
	}
}
