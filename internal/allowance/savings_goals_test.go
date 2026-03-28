package allowance

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ---- Savings goal storage tests ----

func TestCreateAndGetSavingsGoal(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "New bike", 500.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if goal.Name != "New bike" {
		t.Errorf("got name %q, want %q", goal.Name, "New bike")
	}
	if goal.TargetAmount != 500.0 {
		t.Errorf("got target %v, want 500", goal.TargetAmount)
	}
	if goal.CurrentAmount != 0 {
		t.Errorf("got current %v, want 0", goal.CurrentAmount)
	}
	if goal.Deadline != nil {
		t.Errorf("expected nil deadline, got %v", goal.Deadline)
	}

	goals, err := GetSavingsGoals(db, 1, 2)
	if err != nil {
		t.Fatalf("get goals: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("got %d goals, want 1", len(goals))
	}
	if goals[0].Name != "New bike" {
		t.Errorf("got name %q, want %q", goals[0].Name, "New bike")
	}
}

func TestCreateSavingsGoalWithDeadline(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	deadline := "2026-12-31"
	goal, err := CreateSavingsGoal(db, 1, 2, "Vacation", 1000.0, &deadline)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if goal.Deadline == nil || *goal.Deadline != deadline {
		t.Errorf("got deadline %v, want %q", goal.Deadline, deadline)
	}
}

func TestUpdateSavingsGoal(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Lego set", 200.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	updated, err := UpdateSavingsGoal(db, goal.ID, 1, 2, "Lego set deluxe", 300.0, 50.0, nil)
	if err != nil {
		t.Fatalf("update goal: %v", err)
	}
	if updated.Name != "Lego set deluxe" {
		t.Errorf("got name %q, want %q", updated.Name, "Lego set deluxe")
	}
	if updated.TargetAmount != 300.0 {
		t.Errorf("got target %v, want 300", updated.TargetAmount)
	}
	if updated.CurrentAmount != 50.0 {
		t.Errorf("got current %v, want 50", updated.CurrentAmount)
	}
}

func TestUpdateSavingsGoalNotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	_, err := UpdateSavingsGoal(db, 9999, 1, 2, "X", 100.0, 0, nil)
	if err != ErrGoalNotFound {
		t.Errorf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestDeleteSavingsGoal(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Skateboard", 400.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	if err := DeleteSavingsGoal(db, goal.ID, 1, 2); err != nil {
		t.Fatalf("delete goal: %v", err)
	}

	goals, err := GetSavingsGoals(db, 1, 2)
	if err != nil {
		t.Fatalf("get goals after delete: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("expected 0 goals after delete, got %d", len(goals))
	}
}

func TestDeleteSavingsGoalNotFound(t *testing.T) {
	db := setupTestDB(t)
	if err := DeleteSavingsGoal(db, 9999, 1, 2); err != ErrGoalNotFound {
		t.Errorf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestWeeksRemainingComputed(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Insert 2 paid payouts so avgWeeklyEarnings returns > 0
	for _, week := range []string{"2026-01-06", "2026-01-13"} {
		if _, err := UpsertPayout(db, 1, 2, week, 100, 0, 100); err != nil {
			t.Fatalf("upsert payout: %v", err)
		}
		payoutID := mustGetPayoutID(t, db, 1, 2, week)
		if _, err := MarkPayoutPaid(db, payoutID, 1); err != nil {
			t.Fatalf("mark paid: %v", err)
		}
	}

	goal, err := CreateSavingsGoal(db, 1, 2, "Console", 400.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	// avg = 100/week, remaining = 400 → 4 weeks
	if goal.WeeksRemaining == nil {
		t.Fatal("expected WeeksRemaining to be set, got nil")
	}
	if *goal.WeeksRemaining != 4.0 {
		t.Errorf("got weeks_remaining %v, want 4.0", *goal.WeeksRemaining)
	}
}

func mustGetPayoutID(t *testing.T, db *sql.DB, parentID, childID int64, weekStart string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow("SELECT id FROM allowance_payouts WHERE parent_id=? AND child_id=? AND week_start=?",
		parentID, childID, weekStart).Scan(&id)
	if err != nil {
		t.Fatalf("get payout id: %v", err)
	}
	return id
}

// ---- Handler tests ----

func TestMyGoalsHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := CreateSavingsGoal(db, 1, 2, "Toy", 100.0, nil); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/goals", nil), testChild)
	w := httptest.NewRecorder()
	MyGoalsHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Goals []SavingsGoal `json:"goals"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Goals) != 1 {
		t.Errorf("got %d goals, want 1", len(resp.Goals))
	}
	if resp.Goals[0].Name != "Toy" {
		t.Errorf("got goal name %q, want %q", resp.Goals[0].Name, "Toy")
	}
}

func TestCreateMyGoalHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/goals", map[string]any{
		"name":          "New headphones",
		"target_amount": 250.0,
	}), testChild)
	w := httptest.NewRecorder()
	CreateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var goal SavingsGoal
	decode(t, w.Body.Bytes(), &goal)
	if goal.Name != "New headphones" {
		t.Errorf("got %q, want %q", goal.Name, "New headphones")
	}
	if goal.TargetAmount != 250.0 {
		t.Errorf("got target %v, want 250", goal.TargetAmount)
	}
}

func TestCreateMyGoalHandlerMissingName(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/goals", map[string]any{
		"target_amount": 100.0,
	}), testChild)
	w := httptest.NewRecorder()
	CreateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestListChildGoalsHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := CreateSavingsGoal(db, 1, 2, "Puzzle", 80.0, nil); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	r := withChiParam(withUser(newRequest(http.MethodGet, "/api/allowance/children/2/goals", nil), testParent), "id", "2")
	w := httptest.NewRecorder()
	ListChildGoalsHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Goals []SavingsGoal `json:"goals"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Goals) != 1 {
		t.Errorf("got %d goals, want 1", len(resp.Goals))
	}
}

func TestCreateChildGoalHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParam(withUser(newRequest(http.MethodPost, "/api/allowance/children/2/goals", map[string]any{
		"name":          "iPad",
		"target_amount": 1200.0,
	}), testParent), "id", "2")
	w := httptest.NewRecorder()
	CreateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var goal SavingsGoal
	if err := json.Unmarshal(w.Body.Bytes(), &goal); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if goal.Name != "iPad" {
		t.Errorf("got %q, want %q", goal.Name, "iPad")
	}
}

func TestDeleteChildGoalHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Scooter", 600.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	r := withChiParams(withUser(newRequest(http.MethodDelete, "/", nil), testParent), map[string]string{
		"id":     "2",
		"goalId": strconv.FormatInt(goal.ID, 10),
	})
	w := httptest.NewRecorder()
	DeleteChildGoalHandler(db)(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMyGoalHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Guitar", 300.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	r := withChiParam(withUser(newRequest(http.MethodPut, "/api/allowance/my/goals/"+strconv.FormatInt(goal.ID, 10), map[string]any{
		"current_amount": 150.0,
	}), testChild), "id", strconv.FormatInt(goal.ID, 10))
	w := httptest.NewRecorder()
	UpdateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var updated SavingsGoal
	decode(t, w.Body.Bytes(), &updated)
	if updated.CurrentAmount != 150.0 {
		t.Errorf("got current_amount %v, want 150", updated.CurrentAmount)
	}
}

func TestUpdateMyGoalHandlerNegativeAmount(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Shoes", 100.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	r := withChiParam(withUser(newRequest(http.MethodPut, "/api/allowance/my/goals/"+strconv.FormatInt(goal.ID, 10), map[string]any{
		"current_amount": -10.0,
	}), testChild), "id", strconv.FormatInt(goal.ID, 10))
	w := httptest.NewRecorder()
	UpdateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestUpdateMyGoalHandlerNotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParam(withUser(newRequest(http.MethodPut, "/api/allowance/my/goals/9999", map[string]any{
		"current_amount": 50.0,
	}), testChild), "id", "9999")
	w := httptest.NewRecorder()
	UpdateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

func TestUpdateMyGoalHandlerInvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Book", 50.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	goalIDStr := strconv.FormatInt(goal.ID, 10)
	raw := httptest.NewRequest(http.MethodPut, "/api/allowance/my/goals/"+goalIDStr, strings.NewReader("not-json"))
	raw.Header.Set("Content-Type", "application/json")
	r := withChiParam(withUser(raw, testChild), "id", goalIDStr)
	w := httptest.NewRecorder()
	UpdateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

// ---- Additional validation path tests (comment 15) ----

func TestCreateChildGoalHandlerInvalidChildID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParam(withUser(newRequest(http.MethodPost, "/api/allowance/children/bad/goals", map[string]any{
		"name":          "Toy",
		"target_amount": 100.0,
	}), testParent), "id", "bad")
	w := httptest.NewRecorder()
	CreateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestCreateChildGoalHandlerBlankName(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParam(withUser(newRequest(http.MethodPost, "/api/allowance/children/2/goals", map[string]any{
		"name":          "   ",
		"target_amount": 100.0,
	}), testParent), "id", "2")
	w := httptest.NewRecorder()
	CreateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateChildGoalHandlerInvalidDeadline(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParam(withUser(newRequest(http.MethodPost, "/api/allowance/children/2/goals", map[string]any{
		"name":          "Console",
		"target_amount": 300.0,
		"deadline":      "not-a-date",
	}), testParent), "id", "2")
	w := httptest.NewRecorder()
	CreateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateMyGoalHandlerBlankName(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/goals", map[string]any{
		"name":          "   ",
		"target_amount": 50.0,
	}), testChild)
	w := httptest.NewRecorder()
	CreateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateMyGoalHandlerInvalidDeadline(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/goals", map[string]any{
		"name":          "Bike",
		"target_amount": 200.0,
		"deadline":      "bad-date",
	}), testChild)
	w := httptest.NewRecorder()
	CreateMyGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateChildGoalHandlerInvalidChildID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParams(withUser(newRequest(http.MethodPut, "/api/allowance/children/bad/goals/1", map[string]any{
		"name":          "X",
		"target_amount": 100.0,
	}), testParent), map[string]string{"id": "bad", "goalId": "1"})
	w := httptest.NewRecorder()
	UpdateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestUpdateChildGoalHandlerInvalidGoalID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParams(withUser(newRequest(http.MethodPut, "/api/allowance/children/2/goals/bad", map[string]any{
		"name":          "X",
		"target_amount": 100.0,
	}), testParent), map[string]string{"id": "2", "goalId": "bad"})
	w := httptest.NewRecorder()
	UpdateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestUpdateChildGoalHandlerBlankName(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParams(withUser(newRequest(http.MethodPut, "/api/allowance/children/2/goals/1", map[string]any{
		"name":          "   ",
		"target_amount": 100.0,
	}), testParent), map[string]string{"id": "2", "goalId": "1"})
	w := httptest.NewRecorder()
	UpdateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateChildGoalHandlerInvalidDeadline(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Bike", 300.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	goalIDStr := strconv.FormatInt(goal.ID, 10)
	r := withChiParams(withUser(newRequest(http.MethodPut, "/api/allowance/children/2/goals/"+goalIDStr, map[string]any{
		"name":          "Bike",
		"target_amount": 300.0,
		"deadline":      "not-a-date",
	}), testParent), map[string]string{"id": "2", "goalId": goalIDStr})
	w := httptest.NewRecorder()
	UpdateChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteChildGoalHandlerInvalidChildID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParams(withUser(newRequest(http.MethodDelete, "/", nil), testParent), map[string]string{
		"id":     "bad",
		"goalId": "1",
	})
	w := httptest.NewRecorder()
	DeleteChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestDeleteChildGoalHandlerInvalidGoalID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	r := withChiParams(withUser(newRequest(http.MethodDelete, "/", nil), testParent), map[string]string{
		"id":     "2",
		"goalId": "bad",
	})
	w := httptest.NewRecorder()
	DeleteChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestDeleteChildGoalHandlerChildNotLinked(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// child ID 999 is not linked to parent 1
	r := withChiParams(withUser(newRequest(http.MethodDelete, "/", nil), testParent), map[string]string{
		"id":     "999",
		"goalId": "1",
	})
	w := httptest.NewRecorder()
	DeleteChildGoalHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestGetSavingsGoalByID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	goal, err := CreateSavingsGoal(db, 1, 2, "Camera", 800.0, nil)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	fetched, err := GetSavingsGoalByID(db, goal.ID, 1, 2)
	if err != nil {
		t.Fatalf("GetSavingsGoalByID: %v", err)
	}
	if fetched.Name != "Camera" {
		t.Errorf("got name %q, want %q", fetched.Name, "Camera")
	}

	// Wrong child_id returns not found.
	_, err = GetSavingsGoalByID(db, goal.ID, 1, 999)
	if err != ErrGoalNotFound {
		t.Errorf("expected ErrGoalNotFound for wrong child_id, got %v", err)
	}
}
