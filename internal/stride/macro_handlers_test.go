package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// insertMacroBlock writes a complete block — plan row, week rows and the
// block's 'initial' goal revision — through CreateMacroPlan, so the handler
// tests read back exactly what a real generation stores.
func insertMacroBlock(t *testing.T, db *sql.DB, userID int64, startWeek string, weeks int, status string) *MacroPlan {
	t.Helper()

	plan := &MacroPlan{
		UserID:    userID,
		StartWeek: startWeek,
		EndWeek:   mondayAfter(startWeek, weeks-1),
		Status:    status,
		Goal: MacroGoal{
			PrimaryFocus:  "half_marathon",
			Statement:     "Run 1:24:00 for the half marathon",
			TargetHMTimeS: 5040,
			Benchmark:     "3 x 3 km at threshold",
			Rationale:     "The prediction model says 1:27 today.",
		},
		Periodisation: []Mesocycle{
			{Name: "Base 1", Phase: MacroPhaseBase, StartWeek: startWeek, Weeks: weeks, Focus: "aerobic volume"},
		},
		Model:       "claude-opus-5",
		GeneratedBy: MacroGeneratedByScheduled,
	}

	rows := make([]MacroWeek, weeks)
	for i := range rows {
		rows[i] = MacroWeek{
			WeekStart:      mondayAfter(startWeek, i),
			Seq:            i + 1,
			Phase:          MacroPhaseBase,
			Mesocycle:      "Base 1",
			LoadLevel:      LoadLevelNormal,
			TargetKm:       60,
			TargetSessions: 5,
			KeySessions:    []KeySession{{Type: "threshold", Focus: "3 x 3 km"}},
			Intent:         "aerobic base",
			Status:         MacroWeekStatusPlanned,
		}
	}

	if err := CreateMacroPlan(context.Background(), db, plan, rows, "Initial goal for the block."); err != nil {
		t.Fatalf("create macro block starting %s: %v", startWeek, err)
	}
	return plan
}

// decodeMacroView reads a macro endpoint's response body.
func decodeMacroView(t *testing.T, rec *httptest.ResponseRecorder) MacroPlanView {
	t.Helper()
	var view MacroPlanView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("decode macro plan view: %v", err)
	}
	return view
}

// insertSecondUser adds user 2 so ownership scoping can be exercised.
func insertSecondUser(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
}

// subscribeMacroEvents opens a training-hub subscription for userID before a
// POST is made, so the outcome event of the background generation the POST
// starts cannot be missed. Unsubscribed on cleanup.
func subscribeMacroEvents(t *testing.T, userID int64) *training.Subscriber {
	t.Helper()
	sub := training.DefaultHub().Subscribe(userID)
	t.Cleanup(func() { training.DefaultHub().Unsubscribe(userID, sub) })
	return sub
}

// awaitMacroEvent blocks until the background generation publishes its
// outcome and returns it. Waiting is not optional: the goroutine holds the
// athlete's lock until it publishes, and userLocks is process-wide, so a test
// that returned early would leave the next test's POST answering 409.
func awaitMacroEvent(t *testing.T, sub *training.Subscriber) training.Event {
	t.Helper()
	select {
	case evt := <-sub.Events():
		return evt
	case <-time.After(10 * time.Second):
		t.Fatal("no macro outcome event within 10s")
		return training.Event{}
	}
}

// decodeMacroAccepted reads a POST macro endpoint's 202 body.
func decodeMacroAccepted(t *testing.T, rec *httptest.ResponseRecorder) MacroAccepted {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var body MacroAccepted
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode macro accepted body: %v", err)
	}
	return body
}

// --- GET /api/stride/macro/current ---

func TestGetCurrentMacroPlanHandler_NoBlock(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCurrentMacroPlanHandler_ReturnsBlockWeeksAndGoalHistory(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	plan := insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)

	// A second revision so "current" is provably the newest, not the first.
	rev := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      1,
		WeekStart:   mondayAfter(thisMonday, 4),
		Goal:        MacroGoal{PrimaryFocus: "half_marathon", Statement: "Run 1:23:00", TargetHMTimeS: 4980},
		Reason:      "Threshold work is landing ahead of schedule.",
		Source:      GoalRevisionSourceWeekly,
	}
	if err := AddGoalRevision(context.Background(), db, rev); err != nil {
		t.Fatalf("add goal revision: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeMacroView(t, rec)
	if view.Plan == nil || view.Plan.ID != plan.ID {
		t.Fatalf("plan = %+v, want id %d", view.Plan, plan.ID)
	}
	if len(view.Weeks) != MacroBlockWeeks {
		t.Fatalf("weeks = %d, want %d", len(view.Weeks), MacroBlockWeeks)
	}
	if view.Weeks[0].WeekStart != thisMonday {
		t.Errorf("first week = %q, want %q", view.Weeks[0].WeekStart, thisMonday)
	}
	// The weeks are serialised once, at the top level.
	if len(view.Plan.Weeks) != 0 {
		t.Errorf("plan.weeks = %d entries, want them only at the top level", len(view.Plan.Weeks))
	}
	if len(view.Revisions) != 2 {
		t.Fatalf("revisions = %d, want 2", len(view.Revisions))
	}
	if view.CurrentGoalRevision == nil || view.CurrentGoalRevision.ID != rev.ID {
		t.Fatalf("current revision = %+v, want id %d", view.CurrentGoalRevision, rev.ID)
	}
	if view.CurrentGoalRevision.Goal.TargetHMTimeS != 4980 {
		t.Errorf("current goal target = %d, want 4980", view.CurrentGoalRevision.Goal.TargetHMTimeS)
	}
}

func TestGetCurrentMacroPlanHandler_IgnoresSupersededBlock(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusSuperseded)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a superseded block, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- GET /api/stride/macro/{id} ---

func TestGetMacroPlanHandler_OwnedBlock(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	// A superseded block is still readable by id — that is how the athlete
	// looks back at what a Regenerate replaced.
	plan := insertMacroBlock(t, db, 1, thisMonday, 4, MacroPlanStatusSuperseded)

	idStr := strconv.FormatInt(plan.ID, 10)
	req := withChiParam(withUser(httptest.NewRequest("GET", "/api/stride/macro/"+idStr, nil), 1), "id", idStr)
	rec := httptest.NewRecorder()
	GetMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeMacroView(t, rec)
	if view.Plan == nil || view.Plan.ID != plan.ID {
		t.Fatalf("plan = %+v, want id %d", view.Plan, plan.ID)
	}
	if len(view.Weeks) != 4 {
		t.Errorf("weeks = %d, want 4", len(view.Weeks))
	}
	if view.CurrentGoalRevision == nil {
		t.Error("current_goal_revision = nil, want the block's initial revision")
	}
}

func TestGetMacroPlanHandler_OtherUsersBlockIs404(t *testing.T) {
	db := setupTestDB(t)
	insertSecondUser(t, db)
	thisMonday, _ := currentWeek()
	plan := insertMacroBlock(t, db, 2, thisMonday, 4, MacroPlanStatusActive)

	idStr := strconv.FormatInt(plan.ID, 10)
	req := withChiParam(withUser(httptest.NewRequest("GET", "/api/stride/macro/"+idStr, nil), 1), "id", idStr)
	rec := httptest.NewRecorder()
	GetMacroPlanHandler(db).ServeHTTP(rec, req)

	// 404, not 403: the endpoint must not confirm that somebody else's id exists.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetMacroPlanHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := withChiParam(withUser(httptest.NewRequest("GET", "/api/stride/macro/abc", nil), 1), "id", "abc")
	rec := httptest.NewRecorder()
	GetMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- POST /api/stride/macro/generate ---

func TestGenerateMacroPlanHandler_StartsAtUpcomingMonday(t *testing.T) {
	db := setupTestDB(t)
	calls := stubMacroGenerate(t, nil)
	sub := subscribeMacroEvents(t, 1)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)

	wantStart, _ := upcomingWeek()
	accepted := decodeMacroAccepted(t, rec)
	if accepted.Status != "generating" || accepted.Action != "generate" || accepted.StartWeek != wantStart {
		t.Fatalf("accepted = %+v, want generating / generate / %s", accepted, wantStart)
	}

	evt := awaitMacroEvent(t, sub)
	if evt.Type != training.EventStrideMacroReady || evt.Action != "generate" {
		t.Fatalf("event = %+v, want %s for generate", evt, training.EventStrideMacroReady)
	}
	if len(*calls) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(*calls))
	}
	if got := (*calls)[0]; got.startWeek != wantStart || got.mode != MacroModeManual || got.userID != 1 {
		t.Fatalf("generated %+v, want user 1 / %s / manual", got, wantStart)
	}
}

// The outcome is published for the athlete who asked, not for everyone.
func TestGenerateMacroPlanHandler_EventIsScopedToTheAthlete(t *testing.T) {
	db := setupTestDB(t)
	insertSecondUser(t, db)
	stubMacroGenerate(t, nil)
	mine := subscribeMacroEvents(t, 1)
	theirs := subscribeMacroEvents(t, 2)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)
	decodeMacroAccepted(t, rec)

	awaitMacroEvent(t, mine)
	select {
	case evt := <-theirs.Events():
		t.Fatalf("user 2 received %+v for user 1's generation", evt)
	default:
	}
}

// A generation that fails after the 202 reports why through the failed event,
// with the message the athlete can act on where there is one.
func TestGenerateMacroPlanHandler_FailureIsPublished(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantMsg string
	}{
		{"stride not enabled", ErrStrideNotEnabled, ErrStrideNotEnabled.Error()},
		{"claude not enabled", training.ErrClaudeNotEnabled, training.ErrClaudeNotEnabled.Error()},
		{"overlap", ErrOverlappingMacroPlan, "another macro block was created while this one was generating — reload and try again"},
		{"foreign reference", ErrForeignReference, "the generated block references a race or workout that is not yours"},
		{"timeout", context.DeadlineExceeded, "macro block generation timed out — try again"},
		{"anything else", errors.New("claude CLI error: signal: killed"), "failed to generate macro block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			stubMacroGenerate(t, tc.err)
			sub := subscribeMacroEvents(t, 1)

			req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
			rec := httptest.NewRecorder()
			GenerateMacroPlanHandler(db).ServeHTTP(rec, req)
			decodeMacroAccepted(t, rec)

			evt := awaitMacroEvent(t, sub)
			if evt.Type != training.EventStrideMacroFailed {
				t.Fatalf("event type = %q, want %s", evt.Type, training.EventStrideMacroFailed)
			}
			if evt.Action != "generate" || evt.Error != tc.wantMsg {
				t.Fatalf("event = %+v, want generate / %q", evt, tc.wantMsg)
			}
		})
	}
}

// The lock a background generation holds is released when it ends, success or
// failure, so the athlete's next POST is not a 409 for the rest of the process.
func TestGenerateMacroPlanHandler_ReleasesTheLockWhenDone(t *testing.T) {
	db := setupTestDB(t)
	stubMacroGenerate(t, errors.New("boom"))
	sub := subscribeMacroEvents(t, 1)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)
	decodeMacroAccepted(t, rec)
	awaitMacroEvent(t, sub)

	release, ok := TryLockUser(1)
	if !ok {
		t.Fatal("lock still held after the generation published its outcome")
	}
	release()
}

// A real end-to-end regeneration: the previous block is retired, the new one
// takes over, and the week already materialised into a stride_plans row is left
// exactly as the athlete has it.
func TestGenerateMacroPlanHandler_SupersedesAndLeavesMaterialisedWeeks(t *testing.T) {
	startWeek, _ := upcomingWeek()
	db, fixture, _ := setupMacroGeneration(t, startWeek)
	stubMacroPrompt(t, macroFixtureJSON(t, fixture))

	previous := insertMacroBlock(t, db, 1, startWeek, MacroBlockWeeks, MacroPlanStatusActive)

	// The week in progress, already turned into a 7-day plan.
	thisMonday, thisSunday := currentWeek()
	if _, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, model, created_at)
		VALUES (1, ?, ?, '{"days":[]}', 'claude-opus-5', '2026-01-01T00:00:00Z')`,
		thisMonday, thisSunday); err != nil {
		t.Fatalf("insert materialised week: %v", err)
	}

	sub := subscribeMacroEvents(t, 1)
	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)
	decodeMacroAccepted(t, rec)

	evt := awaitMacroEvent(t, sub)
	if evt.Type != training.EventStrideMacroReady {
		t.Fatalf("event = %+v, want %s", evt, training.EventStrideMacroReady)
	}
	if evt.MacroPlanID == 0 || evt.MacroPlanID == previous.ID {
		t.Fatalf("event macro_plan_id = %d, want a block other than the replaced %d", evt.MacroPlanID, previous.ID)
	}

	// What the page re-reads after the event.
	getReq := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	getRec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /macro/current, got %d: %s", getRec.Code, getRec.Body.String())
	}
	view := decodeMacroView(t, getRec)
	if view.Plan == nil || view.Plan.ID != evt.MacroPlanID {
		t.Fatalf("plan = %+v, want the block %d the event announced", view.Plan, evt.MacroPlanID)
	}
	if view.Plan.GeneratedBy != MacroGeneratedByManual {
		t.Errorf("generated_by = %q, want manual", view.Plan.GeneratedBy)
	}
	if view.Plan.StartWeek != startWeek {
		t.Errorf("start_week = %q, want %q", view.Plan.StartWeek, startWeek)
	}
	if len(view.Weeks) != MacroBlockWeeks {
		t.Errorf("weeks = %d, want %d", len(view.Weeks), MacroBlockWeeks)
	}
	if view.CurrentGoalRevision == nil {
		t.Error("current_goal_revision = nil, want the new block's initial revision")
	}

	retired, err := GetMacroPlanByID(context.Background(), db, previous.ID, 1)
	if err != nil {
		t.Fatalf("load replaced block: %v", err)
	}
	if retired.Status != MacroPlanStatusSuperseded {
		t.Errorf("replaced block status = %q, want superseded", retired.Status)
	}

	// The materialised week is untouched — same row, same plan_json.
	var planJSON string
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1").Scan(&count); err != nil {
		t.Fatalf("count stride_plans: %v", err)
	}
	if count != 1 {
		t.Fatalf("stride_plans rows = %d, want the one materialised week", count)
	}
	if err := db.QueryRow("SELECT plan_json FROM stride_plans WHERE user_id = 1 AND week_start = ?", thisMonday).
		Scan(&planJSON); err != nil {
		t.Fatalf("read materialised week: %v", err)
	}
	if planJSON != `{"days":[]}` {
		t.Errorf("plan_json = %q, want it untouched", planJSON)
	}
}

// --- POST /api/stride/macro/extend ---

func TestExtendMacroPlanHandler_StartsAfterTheLastActiveBlock(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)
	calls := stubMacroGenerate(t, nil)

	sub := subscribeMacroEvents(t, 1)
	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/extend", nil), 1)
	rec := httptest.NewRecorder()
	ExtendMacroPlanHandler(db).ServeHTTP(rec, req)

	// end_week + 7d: the block runs MacroBlockWeeks weeks from thisMonday, so
	// the extension starts the Monday after its last one.
	wantStart := mondayAfter(thisMonday, MacroBlockWeeks)
	accepted := decodeMacroAccepted(t, rec)
	if accepted.Action != "extend" || accepted.StartWeek != wantStart {
		t.Fatalf("accepted = %+v, want extend / %s", accepted, wantStart)
	}
	evt := awaitMacroEvent(t, sub)
	if evt.Type != training.EventStrideMacroReady || evt.Action != "extend" {
		t.Fatalf("event = %+v, want %s for extend", evt, training.EventStrideMacroReady)
	}
	if len(*calls) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(*calls))
	}
	if got := (*calls)[0]; got.startWeek != wantStart || got.mode != MacroModeExtension {
		t.Fatalf("generated %+v, want %s / extension", got, wantStart)
	}
}

// An extension already queued ahead of the running block must not be
// regenerated away — the new one goes behind it.
func TestExtendMacroPlanHandler_StacksBehindAQueuedExtension(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)
	insertMacroBlock(t, db, 1, mondayAfter(thisMonday, MacroBlockWeeks), MacroBlockWeeks, MacroPlanStatusActive)
	calls := stubMacroGenerate(t, nil)

	sub := subscribeMacroEvents(t, 1)
	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/extend", nil), 1)
	rec := httptest.NewRecorder()
	ExtendMacroPlanHandler(db).ServeHTTP(rec, req)
	decodeMacroAccepted(t, rec)
	awaitMacroEvent(t, sub)

	wantStart := mondayAfter(thisMonday, 2*MacroBlockWeeks)
	if got := (*calls)[0]; got.startWeek != wantStart {
		t.Fatalf("start week = %q, want %q", got.startWeek, wantStart)
	}
}

func TestExtendMacroPlanHandler_NoActiveBlockIsConflict(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	// Active, but its horizon ended before this week — there is nothing left to
	// continue, so the athlete wants /macro/generate.
	insertMacroBlock(t, db, 1, mondayAfter(thisMonday, -30), MacroBlockWeeks, MacroPlanStatusActive)
	calls := stubMacroGenerate(t, nil)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/extend", nil), 1)
	rec := httptest.NewRecorder()
	ExtendMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("generation calls = %d, want none", len(*calls))
	}
}

// --- the shared per-user lock ---

func TestMacroPostHandlersShareThePerUserLock(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)

	// Hold the athlete's lock the way a Monday run does, then confirm both POST
	// endpoints refuse rather than starting a second Claude call.
	release := LockUser(1)
	defer release()

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"generate", GenerateMacroPlanHandler(db)},
		{"extend", ExtendMacroPlanHandler(db)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := withUser(httptest.NewRequest("POST", "/api/stride/macro/"+tc.name, nil), 1)
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusConflict {
				t.Fatalf("expected 409 while the lock is held, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A second POST while the background generation is still running must be a
// 409, never a second Claude call — the lock outlives the 202 that started it.
func TestGenerateMacroPlanHandler_SecondPostWhileGeneratingIsConflict(t *testing.T) {
	db := setupTestDB(t)

	var mu sync.Mutex
	started := 0
	orig := generateMacroPlanFunc
	entered := make(chan struct{}, 1)
	blocked := make(chan struct{})
	generateMacroPlanFunc = func(_ context.Context, _ *sql.DB, userID int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
		mu.Lock()
		started++
		mu.Unlock()
		entered <- struct{}{}
		<-blocked
		return &MacroPlan{UserID: userID, StartWeek: startWeek, GeneratedBy: string(mode)}, nil
	}
	t.Cleanup(func() { generateMacroPlanFunc = orig })
	sub := subscribeMacroEvents(t, 1)

	handler := GenerateMacroPlanHandler(db)
	post := func() int {
		req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if first := post(); first != http.StatusAccepted {
		t.Fatalf("first answer = %d, want 202", first)
	}
	// The background run is parked inside the generation seam, holding the
	// lock, when the second request arrives.
	<-entered
	if second := post(); second != http.StatusConflict {
		t.Fatalf("second answer = %d, want 409 while the generation is running", second)
	}

	close(blocked)
	awaitMacroEvent(t, sub)

	mu.Lock()
	defer mu.Unlock()
	if started != 1 {
		t.Fatalf("generations started = %d, want 1", started)
	}
}

// --- has_next_block ---

func TestGetCurrentMacroPlanHandler_HasNextBlockFalseWithoutSuccessor(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if view := decodeMacroView(t, rec); view.HasNextBlock {
		t.Error("has_next_block = true with only one block")
	}
}

func TestGetCurrentMacroPlanHandler_HasNextBlockWithQueuedExtension(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)
	// The extension starts the Monday after the first block's last week.
	insertMacroBlock(t, db, 1, mondayAfter(thisMonday, MacroBlockWeeks), MacroBlockWeeks, MacroPlanStatusActive)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if view := decodeMacroView(t, rec); !view.HasNextBlock {
		t.Error("has_next_block = false with an extension already queued")
	}
}

// A superseded successor is not a horizon: it was replaced, so the athlete
// still has nothing planned past the current block and Extend must stay live.
func TestGetCurrentMacroPlanHandler_HasNextBlockIgnoresSupersededSuccessor(t *testing.T) {
	db := setupTestDB(t)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)
	insertMacroBlock(t, db, 1, mondayAfter(thisMonday, MacroBlockWeeks), MacroBlockWeeks, MacroPlanStatusSuperseded)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if view := decodeMacroView(t, rec); view.HasNextBlock {
		t.Error("has_next_block = true for a superseded successor")
	}
}

// Another athlete's block over the same weeks must not count as this one's
// horizon — the successor lookup is scoped by user like every other read.
func TestGetCurrentMacroPlanHandler_HasNextBlockIsScopedByUser(t *testing.T) {
	db := setupTestDB(t)
	insertSecondUser(t, db)
	thisMonday, _ := currentWeek()
	insertMacroBlock(t, db, 1, thisMonday, MacroBlockWeeks, MacroPlanStatusActive)
	insertMacroBlock(t, db, 2, mondayAfter(thisMonday, MacroBlockWeeks), MacroBlockWeeks, MacroPlanStatusActive)

	req := withUser(httptest.NewRequest("GET", "/api/stride/macro/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if view := decodeMacroView(t, rec); view.HasNextBlock {
		t.Error("has_next_block = true for another athlete's block")
	}
}
