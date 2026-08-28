package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
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

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(*calls))
	}
	wantStart, _ := upcomingWeek()
	if got := (*calls)[0]; got.startWeek != wantStart || got.mode != MacroModeManual || got.userID != 1 {
		t.Fatalf("generated %+v, want user 1 / %s / manual", got, wantStart)
	}
}

func TestGenerateMacroPlanHandler_StrideNotEnabled(t *testing.T) {
	db := setupTestDB(t)
	stubMacroGenerate(t, ErrStrideNotEnabled)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGenerateMacroPlanHandler_OverlapIsConflict(t *testing.T) {
	db := setupTestDB(t)
	stubMacroGenerate(t, ErrOverlappingMacroPlan)

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
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

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
	rec := httptest.NewRecorder()
	GenerateMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeMacroView(t, rec)
	if view.Plan == nil || view.Plan.ID == previous.ID {
		t.Fatalf("plan = %+v, want a block other than the replaced %d", view.Plan, previous.ID)
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

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/extend", nil), 1)
	rec := httptest.NewRecorder()
	ExtendMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(*calls))
	}
	// end_week + 7d: the block runs MacroBlockWeeks weeks from thisMonday, so
	// the extension starts the Monday after its last one.
	wantStart := mondayAfter(thisMonday, MacroBlockWeeks)
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

	req := withUser(httptest.NewRequest("POST", "/api/stride/macro/extend", nil), 1)
	rec := httptest.NewRecorder()
	ExtendMacroPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
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

// Two simultaneous regenerations for one athlete must produce one generation
// and one 409, never two Claude calls.
func TestGenerateMacroPlanHandler_ConcurrentPostsSerialise(t *testing.T) {
	db := setupTestDB(t)

	var mu sync.Mutex
	started := 0
	orig := generateMacroPlanFunc
	blocked := make(chan struct{})
	generateMacroPlanFunc = func(_ context.Context, _ *sql.DB, userID int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
		mu.Lock()
		started++
		mu.Unlock()
		<-blocked
		return &MacroPlan{UserID: userID, StartWeek: startWeek, GeneratedBy: string(mode)}, nil
	}
	t.Cleanup(func() { generateMacroPlanFunc = orig })

	handler := GenerateMacroPlanHandler(db)
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			req := withUser(httptest.NewRequest("POST", "/api/stride/macro/generate", nil), 1)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes <- rec.Code
		}()
	}

	// Whichever request takes the lock is parked inside the generation seam, so
	// the first answer can only be the other one's 409 — the two are provably
	// overlapping rather than merely sequential.
	if first := <-codes; first != http.StatusConflict {
		t.Fatalf("first answer = %d, want 409 while the other request holds the lock", first)
	}
	close(blocked)
	if second := <-codes; second != http.StatusCreated {
		t.Fatalf("second answer = %d, want 201", second)
	}

	mu.Lock()
	defer mu.Unlock()
	if started != 1 {
		t.Fatalf("generations started = %d, want 1", started)
	}
}
