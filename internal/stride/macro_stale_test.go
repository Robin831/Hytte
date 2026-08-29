package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// staleReasonOf reads a block's stale_reason straight from the table, so the
// assertions do not depend on the store's decryption path.
func staleReasonOf(t *testing.T, db *sql.DB, planID int64) string {
	t.Helper()
	var reason string
	if err := db.QueryRow(`SELECT stale_reason FROM stride_macro_plans WHERE id = ?`, planID).Scan(&reason); err != nil {
		t.Fatalf("read stale_reason for plan %d: %v", planID, err)
	}
	return reason
}

// countMacroPlans is the "nothing was regenerated" assertion: a race edit must
// never add a block.
func countMacroPlans(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_macro_plans`).Scan(&n); err != nil {
		t.Fatalf("count macro plans: %v", err)
	}
	return n
}

// seedActiveBlock stores the sample 26-week block for the user and returns it.
func seedActiveBlock(t *testing.T, db *sql.DB, userID int64) *MacroPlan {
	t.Helper()
	plan, weeks := sampleMacroPlan(userID)
	if err := CreateMacroPlan(context.Background(), db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	return plan
}

func createRaceViaHandler(t *testing.T, db *sql.DB, userID int64, payload string) Race {
	t.Helper()
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), userID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create race: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Race Race `json:"race"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode created race: %v", err)
	}
	return body.Race
}

func updateRaceViaHandler(t *testing.T, db *sql.DB, userID, raceID int64, payload string) {
	t.Helper()
	idStr := strconv.FormatInt(raceID, 10)
	req := withUser(httptest.NewRequest("PUT", "/api/stride/races/"+idStr, strings.NewReader(payload)), userID)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateRaceHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update race: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func deleteRaceViaHandler(t *testing.T, db *sql.DB, userID, raceID int64) {
	t.Helper()
	idStr := strconv.FormatInt(raceID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/stride/races/"+idStr, nil), userID)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteRaceHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete race: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRaceStaleWeeksSnapsDedupesAndSkipsEmpty(t *testing.T) {
	// 2026-09-06 is a Sunday, so it belongs to the week starting 2026-08-31 —
	// the same week as the Monday itself, hence one key rather than two.
	weeks, err := raceStaleWeeks([]string{"2026-09-06", "", "2026-08-31", "2026-09-12"})
	if err != nil {
		t.Fatalf("raceStaleWeeks: %v", err)
	}
	want := []string{"2026-08-31", "2026-09-07"}
	if len(weeks) != len(want) {
		t.Fatalf("weeks = %v, want %v", weeks, want)
	}
	for i := range want {
		if weeks[i] != want[i] {
			t.Fatalf("weeks = %v, want %v", weeks, want)
		}
	}
}

func TestRaceStaleWeeksRejectsMalformedDate(t *testing.T) {
	if _, err := raceStaleWeeks([]string{"not-a-date"}); err == nil {
		t.Fatal("expected an error for a malformed race date")
	}
}

func TestCreateRaceInsideHorizonMarksBlockStale(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)
	before := countMacroPlans(t, db)

	// Week 11 of the block, comfortably inside the horizon.
	createRaceViaHandler(t, db, 1, `{"name":"Oslo Half","date":"`+mondayAfter(testBlockStart, 10)+`","distance_m":21097,"priority":"A"}`)

	if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
	if after := countMacroPlans(t, db); after != before {
		t.Fatalf("macro plan count = %d, want %d — a race edit must never regenerate", after, before)
	}
}

func TestCreateRaceOutsideHorizonLeavesBlockFresh(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	// One week past the last week of the block, and one week before it starts.
	createRaceViaHandler(t, db, 1, `{"name":"Late Race","date":"`+mondayAfter(testBlockStart, 26)+`","distance_m":21097,"priority":"A"}`)
	createRaceViaHandler(t, db, 1, `{"name":"Early Race","date":"`+mondayAfter(testBlockStart, -1)+`","distance_m":10000,"priority":"B"}`)

	if got := staleReasonOf(t, db, plan.ID); got != "" {
		t.Fatalf("stale_reason = %q, want empty", got)
	}
}

func TestCreateRaceOnTheHorizonEdgesMarksBlockStale(t *testing.T) {
	// end_week is the Monday of the final week, so the horizon runs six days
	// past it — both ends of that range still belong to the block.
	lastMonday, err := time.Parse("2006-01-02", mondayAfter(testBlockStart, 25))
	if err != nil {
		t.Fatalf("parse last week: %v", err)
	}
	lastSunday := lastMonday.AddDate(0, 0, 6).Format("2006-01-02")

	for _, tc := range []struct {
		name string
		date string
	}{
		{"first day of the block", testBlockStart},
		{"last day of the block", lastSunday},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			plan := seedActiveBlock(t, db, 1)

			createRaceViaHandler(t, db, 1, `{"name":"Edge","date":"`+tc.date+`","distance_m":21097,"priority":"A"}`)

			if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
				t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
			}
		})
	}
}

func TestUpdateRaceMovingItIntoTheHorizonMarksBlockStale(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	outside := mondayAfter(testBlockStart, 30)
	race := createRaceViaHandler(t, db, 1, `{"name":"Drifting Race","date":"`+outside+`","distance_m":21097,"priority":"B"}`)
	if got := staleReasonOf(t, db, plan.ID); got != "" {
		t.Fatalf("stale_reason after the create = %q, want empty", got)
	}

	inside := mondayAfter(testBlockStart, 12)
	updateRaceViaHandler(t, db, 1, race.ID, `{"name":"Drifting Race","date":"`+inside+`","distance_m":21097,"priority":"A"}`)

	if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
}

func TestUpdateRaceMovingItOutOfTheHorizonMarksBlockStale(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	inside := mondayAfter(testBlockStart, 12)
	race := createRaceViaHandler(t, db, 1, `{"name":"Anchor","date":"`+inside+`","distance_m":21097,"priority":"A"}`)
	if err := SetMacroPlanStale(context.Background(), db, plan.ID, 1, ""); err != nil {
		t.Fatalf("clear stale flag: %v", err)
	}

	outside := mondayAfter(testBlockStart, 40)
	updateRaceViaHandler(t, db, 1, race.ID, `{"name":"Anchor","date":"`+outside+`","distance_m":21097,"priority":"A"}`)

	if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
}

func TestUpdateRaceEntirelyOutsideTheHorizonLeavesBlockFresh(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	race := createRaceViaHandler(t, db, 1, `{"name":"Far Away","date":"`+mondayAfter(testBlockStart, 30)+`","distance_m":21097,"priority":"B"}`)
	updateRaceViaHandler(t, db, 1, race.ID, `{"name":"Far Away","date":"`+mondayAfter(testBlockStart, 32)+`","distance_m":21097,"priority":"A"}`)

	if got := staleReasonOf(t, db, plan.ID); got != "" {
		t.Fatalf("stale_reason = %q, want empty", got)
	}
}

func TestDeleteRaceInsideHorizonMarksBlockStale(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	race := createRaceViaHandler(t, db, 1, `{"name":"Cancelled","date":"`+mondayAfter(testBlockStart, 8)+`","distance_m":21097,"priority":"A"}`)
	if err := SetMacroPlanStale(context.Background(), db, plan.ID, 1, ""); err != nil {
		t.Fatalf("clear stale flag: %v", err)
	}

	deleteRaceViaHandler(t, db, 1, race.ID)

	if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
}

func TestDeleteRaceOutsideHorizonLeavesBlockFresh(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)

	race := createRaceViaHandler(t, db, 1, `{"name":"Someday","date":"`+mondayAfter(testBlockStart, 30)+`","distance_m":10000,"priority":"C"}`)
	deleteRaceViaHandler(t, db, 1, race.ID)

	if got := staleReasonOf(t, db, plan.ID); got != "" {
		t.Fatalf("stale_reason = %q, want empty", got)
	}
}

func TestRaceChangeWithNoActiveBlockIsNotAnError(t *testing.T) {
	db := setupTestDB(t)

	race := createRaceViaHandler(t, db, 1, `{"name":"Solo","date":"`+mondayAfter(testBlockStart, 4)+`","distance_m":21097,"priority":"A"}`)
	updateRaceViaHandler(t, db, 1, race.ID, `{"name":"Solo","date":"`+mondayAfter(testBlockStart, 5)+`","distance_m":21097,"priority":"A"}`)
	deleteRaceViaHandler(t, db, 1, race.ID)

	if n := countMacroPlans(t, db); n != 0 {
		t.Fatalf("macro plan count = %d, want 0 — a race edit must never generate a block", n)
	}
}

func TestRaceChangeOnlyMarksTheOwningAthletesBlock(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	mine := seedActiveBlock(t, db, 1)
	theirs := seedActiveBlock(t, db, 2)

	createRaceViaHandler(t, db, 1, `{"name":"Mine","date":"`+mondayAfter(testBlockStart, 10)+`","distance_m":21097,"priority":"A"}`)

	if got := staleReasonOf(t, db, mine.ID); got != MacroStaleRacesChanged {
		t.Fatalf("own block stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
	if got := staleReasonOf(t, db, theirs.ID); got != "" {
		t.Fatalf("other athlete's block stale_reason = %q, want empty", got)
	}
}

func TestMarkMacroPlansStaleForRacesSkipsSupersededBlocks(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	plan := seedActiveBlock(t, db, 1)
	if err := SupersedeMacroPlan(ctx, db, plan.ID, 1); err != nil {
		t.Fatalf("SupersedeMacroPlan: %v", err)
	}

	if err := MarkMacroPlansStaleForRaces(ctx, db, 1, mondayAfter(testBlockStart, 10)); err != nil {
		t.Fatalf("MarkMacroPlansStaleForRaces: %v", err)
	}

	if got := staleReasonOf(t, db, plan.ID); got != "" {
		t.Fatalf("stale_reason = %q, want empty — a retired block has nothing to invalidate", got)
	}
}

func TestMarkMacroPlansStaleForRacesIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	plan := seedActiveBlock(t, db, 1)
	date := mondayAfter(testBlockStart, 3)

	for i := 0; i < 2; i++ {
		if err := MarkMacroPlansStaleForRaces(ctx, db, 1, date, date); err != nil {
			t.Fatalf("MarkMacroPlansStaleForRaces (pass %d): %v", i, err)
		}
	}
	if got := staleReasonOf(t, db, plan.ID); got != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got, MacroStaleRacesChanged)
	}
}

func TestCurrentMacroPlanResponseCarriesStaleReason(t *testing.T) {
	db := setupTestDB(t)
	plan := seedActiveBlock(t, db, 1)
	if err := SetMacroPlanStale(context.Background(), db, plan.ID, 1, MacroStaleRacesChanged); err != nil {
		t.Fatalf("SetMacroPlanStale: %v", err)
	}

	view, err := buildMacroPlanView(context.Background(), db, mustActiveBlock(t, db, 1))
	if err != nil {
		t.Fatalf("buildMacroPlanView: %v", err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var decoded struct {
		Plan struct {
			StaleReason string `json:"stale_reason"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if decoded.Plan.StaleReason != MacroStaleRacesChanged {
		t.Fatalf("payload stale_reason = %q, want %q", decoded.Plan.StaleReason, MacroStaleRacesChanged)
	}
}

func mustActiveBlock(t *testing.T, db *sql.DB, userID int64) *MacroPlan {
	t.Helper()
	plan, err := GetActiveMacroPlan(context.Background(), db, userID, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if plan == nil {
		t.Fatal("expected an active macro plan")
	}
	return plan
}
