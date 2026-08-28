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

// weeklyRunSeams records what a run did through RunWeekly's two external seams:
// which athletes the race-prediction refresh ran for, and what was pushed.
type weeklyRunSeams struct {
	refreshUsers []int64
	pushes       []push.Notification
	// order records the run's Claude-backed steps in the order they fired. The
	// refresh records itself here; a test that cares about the ordering contract
	// wraps runPromptFunc to append "plan" — see
	// TestRunWeeklyRefreshesPredictionBeforePlanning.
	order []string
}

// stubWeeklyRunSeams replaces the prediction refresh and the push sender, so a
// weekly run neither predicts through Claude nor talks to a push service.
// refreshErr is what the refresh answers with, so a test can assert the run
// survives a snapshot that could not be refreshed.
func stubWeeklyRunSeams(t *testing.T, refreshErr error) *weeklyRunSeams {
	t.Helper()
	seams := &weeklyRunSeams{}

	origRefresh := refreshRacePredictionFunc
	refreshRacePredictionFunc = func(_ context.Context, _ *sql.DB, userID int64, _ *training.ClaudeConfig) (*training.StoredRacePrediction, error) {
		seams.refreshUsers = append(seams.refreshUsers, userID)
		seams.order = append(seams.order, "refresh")
		return nil, refreshErr
	}
	t.Cleanup(func() { refreshRacePredictionFunc = origRefresh })

	origPush := sendPushFunc
	sendPushFunc = func(_ *sql.DB, _ int64, notif push.Notification) error {
		seams.pushes = append(seams.pushes, notif)
		return nil
	}
	t.Cleanup(func() { sendPushFunc = origPush })

	return seams
}

// onlyPush returns the single notification the run emitted, failing when the run
// sent none or more than one: every run ends in exactly one.
func (s *weeklyRunSeams) onlyPush(t *testing.T) push.Notification {
	t.Helper()
	if len(s.pushes) != 1 {
		t.Fatalf("notifications = %d, want exactly 1 (%+v)", len(s.pushes), s.pushes)
	}
	return s.pushes[0]
}

// The happy path: the horizon is already long enough, the week is planned, and
// the athlete gets the ordinary "plan is ready" push.
func TestRunWeeklyHappyPath(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	stubMacroGenerate(t, nil)
	seams := stubWeeklyRunSeams(t, nil)

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
	if got := seams.onlyPush(t); got.Body != weeklyPlanReadyBody || got.Tag != weeklyPlanTag {
		t.Errorf("push = %q/%q, want %q/%q", got.Body, got.Tag, weeklyPlanReadyBody, weeklyPlanTag)
	}
}

// The prediction snapshot is refreshed for this athlete, and it happens *before*
// the plan generates: the coach prompt reads the stored snapshot, so a refresh
// that ran after it would price the week off last week's fitness estimate.
func TestRunWeeklyRefreshesPredictionBeforePlanning(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	stubMacroGenerate(t, nil)
	seams := stubWeeklyRunSeams(t, nil)
	insertMacroPlanSpan(t, db, 1, weekStart, MacroBlockWeeks, MacroPlanStatusActive)

	planPrompt := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		seams.order = append(seams.order, "plan")
		return planPrompt(ctx, cfg, prompt)
	}
	t.Cleanup(func() { runPromptFunc = planPrompt })

	if err := RunWeekly(context.Background(), db, 1); err != nil {
		t.Fatalf("RunWeekly: %v", err)
	}

	if len(seams.refreshUsers) != 1 || seams.refreshUsers[0] != 1 {
		t.Fatalf("prediction refreshed for %v, want exactly [1]", seams.refreshUsers)
	}
	if len(seams.order) != 2 || seams.order[0] != "refresh" || seams.order[1] != "plan" {
		t.Errorf("step order = %v, want [refresh plan]", seams.order)
	}
}

// The refresh is advisory input: when it fails, last week's snapshot stands and
// the run still plans the week and sends the ordinary push.
func TestRunWeeklyToleratesPredictionRefreshFailure(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	stubMacroGenerate(t, nil)
	seams := stubWeeklyRunSeams(t, errors.New("prediction claude exploded"))
	insertMacroPlanSpan(t, db, 1, weekStart, MacroBlockWeeks, MacroPlanStatusActive)

	if err := RunWeekly(context.Background(), db, 1); err != nil {
		t.Fatalf("RunWeekly must survive a failed prediction refresh, got: %v", err)
	}

	if len(seams.refreshUsers) != 1 {
		t.Fatalf("prediction refresh calls = %d, want 1", len(seams.refreshUsers))
	}
	var planCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1 AND week_start = ?", weekStart).Scan(&planCount); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if planCount != 1 {
		t.Errorf("stored plans for %s = %d, want 1 — the week is planned regardless", weekStart, planCount)
	}
	if got := seams.onlyPush(t); got.Body != weeklyPlanReadyBody {
		t.Errorf("push body = %q, want the ordinary %q", got.Body, weeklyPlanReadyBody)
	}
}

// The run holds the athlete's lock from start to finish, which is the whole
// point of the guard: while it runs, a manual trigger must see the lock taken
// and answer 409 rather than spend a second Claude call on the same week.
func TestRunWeeklyHoldsUserLockForTheWholeRun(t *testing.T) {
	// User 1 is the seeded athlete; the lock map is per-process, and the
	// package's tests are sequential, so this run owns it.
	const userID int64 = 1
	db := extendedTestDB(t)
	enableStride(t, db, userID)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	stubWeeklyRunSeams(t, nil)

	// No macro block exists, so the run parks inside the macro step until the
	// test lets it go — a stand-in for the multi-minute Claude call.
	started := make(chan struct{})
	finish := make(chan struct{})
	origMacro := generateMacroPlanFunc
	generateMacroPlanFunc = func(_ context.Context, _ *sql.DB, uid int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
		close(started)
		<-finish
		return &MacroPlan{UserID: uid, StartWeek: startWeek, GeneratedBy: string(mode)}, nil
	}
	t.Cleanup(func() { generateMacroPlanFunc = origMacro })

	done := make(chan error, 1)
	go func() { done <- RunWeekly(context.Background(), db, userID) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("RunWeekly never reached the macro step")
	}

	if release, ok := TryLockUser(userID); ok {
		release()
		close(finish)
		<-done
		t.Fatal("the athlete's lock was free mid-run — a manual trigger could generate the same week a second time")
	}

	close(finish)
	if err := <-done; err != nil {
		t.Fatalf("RunWeekly: %v", err)
	}

	release, ok := TryLockUser(userID)
	if !ok {
		t.Fatal("RunWeekly did not release the athlete's lock")
	}
	release()
}

// A failed block generation must not cost the athlete their week: the weekly
// plan still generates, the run still succeeds, and the push both tells them the
// week is ready and that the long-term plan needs a manual retry.
func TestRunWeeklyMacroFailureStillPlansWeek(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)
	calls := stubMacroGenerate(t, errors.New("claude exploded"))
	seams := stubWeeklyRunSeams(t, nil)

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

	got := seams.onlyPush(t)
	if got.Body != macroFailedPlanReadyBody {
		t.Errorf("push body = %q, want %q — the week that did generate must not be reported as lost", got.Body, macroFailedPlanReadyBody)
	}
	if got.Tag != macroFailedTag {
		t.Errorf("push tag = %q, want %q", got.Tag, macroFailedTag)
	}
}

// A failed weekly plan is the one failure that fails the run. It must not tell
// the athlete a plan is waiting for them — and it must not say nothing either,
// or they open Stride on Monday to last week's plan with no explanation.
func TestRunWeeklyWeeklyFailureReturnsErrorAndPushesFailure(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	stubMacroGenerate(t, nil)
	seams := stubWeeklyRunSeams(t, nil)

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
	got := seams.onlyPush(t)
	if got.Body != weeklyPlanFailedBody {
		t.Errorf("push body = %q, want %q", got.Body, weeklyPlanFailedBody)
	}
	if got.Tag != weeklyPlanTag {
		t.Errorf("push tag = %q, want %q", got.Tag, weeklyPlanTag)
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
			seams := stubWeeklyRunSeams(t, nil)

			if err := RunWeekly(context.Background(), db, 1); err != nil {
				t.Fatalf("RunWeekly: %v", err)
			}

			if got := seams.onlyPush(t); got.Body != weeklyPlanReadyBody {
				t.Errorf("push body = %q, want the ordinary %q", got.Body, weeklyPlanReadyBody)
			}
		})
	}
}

// The worst run an athlete can have: no long-term block *and* no week. They
// still get the actionable message — the macro failure is what they have to
// press Regenerate for — and the run still reports the plan failure to the cron.
func TestRunWeeklyBothStepsFailPushesMacroFailure(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)
	stubMacroGenerate(t, errors.New("macro claude exploded"))
	seams := stubWeeklyRunSeams(t, nil)

	boom := errors.New("plan claude timed out")
	origPrompt := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", boom
	}
	t.Cleanup(func() { runPromptFunc = origPrompt })

	if err := RunWeekly(context.Background(), db, 1); !errors.Is(err, boom) {
		t.Fatalf("RunWeekly error = %v, want it to wrap %v", err, boom)
	}

	got := seams.onlyPush(t)
	if got.Body != macroFailedBody {
		t.Errorf("push body = %q, want %q — the macro failure is the one to act on", got.Body, macroFailedBody)
	}
	if got.Tag != macroFailedTag {
		t.Errorf("push tag = %q, want %q", got.Tag, macroFailedTag)
	}
}

// The extension decision must not hinge on both operands being exact UTC
// midnights. Truncating the difference used to drop a whole week for any skew —
// an hour short of eight weeks read as seven and bought an extension block a
// Monday early; an hour long skipped the extension for a week.
func TestWeeksRemainingRoundsSkewedOperands(t *testing.T) {
	from := mustMonday(t, "2026-08-31")
	end := mustMonday(t, mondayAfter("2026-08-31", MacroExtensionLeadWeeks))

	for name, skew := range map[string]time.Duration{
		"an hour short":     -time.Hour,
		"an hour long":      time.Hour,
		"a DST day short":   -23 * time.Hour,
		"a DST day long":    25 * time.Hour,
		"non-UTC midnights": -2 * time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			if got := weeksRemaining(from, end.Add(skew)); got != MacroExtensionLeadWeeks {
				t.Errorf("weeksRemaining with %s skew = %d, want %d", name, got, MacroExtensionLeadWeeks)
			}
		})
	}
}
