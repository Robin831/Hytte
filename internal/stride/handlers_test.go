package stride

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- Race handler tests ---

func TestListRacesHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/races", nil), 1)
	rec := httptest.NewRecorder()
	ListRacesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Races []Race `json:"races"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Races) != 0 {
		t.Errorf("expected 0 races, got %d", len(body.Races))
	}
}

func TestCreateRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Bergen City Marathon","date":"2026-10-18","distance_m":42195,"priority":"A","notes":"Goal race"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Race Race `json:"race"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Race.Name != "Bergen City Marathon" {
		t.Errorf("name = %q, want %q", body.Race.Name, "Bergen City Marathon")
	}
	if body.Race.Priority != "A" {
		t.Errorf("priority = %q, want %q", body.Race.Priority, "A")
	}
}

func TestCreateRaceHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateRaceHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date":"2026-10-18","distance_m":42195,"priority":"A"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateRaceHandler_InvalidPriority(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Race","date":"2026-10-18","distance_m":42195,"priority":"D"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Old Name", "2026-05-01", 10000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	payload := `{"name":"New Name","date":"2026-05-02","distance_m":21097,"priority":"B","notes":"updated"}`
	idStr := strconv.FormatInt(race.ID, 10)
	req := withUser(httptest.NewRequest("PUT", "/api/stride/races/"+idStr, strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Race Race `json:"race"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Race.Name != "New Name" {
		t.Errorf("name = %q, want %q", body.Race.Name, "New Name")
	}
}

func TestUpdateRaceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X","date":"2026-05-01","distance_m":5000,"priority":"C"}`
	req := withUser(httptest.NewRequest("PUT", "/api/stride/races/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Delete Me", "2026-05-01", 5000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(race.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/stride/races/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteRaceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/stride/races/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- Note handler tests ---

func TestListNotesHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/notes", nil), 1)
	rec := httptest.NewRecorder()
	ListNotesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Notes []Note `json:"notes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(body.Notes))
	}
}

func TestCreateNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"content":"Feeling strong this week"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/notes", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Note Note `json:"note"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Note.Content != "Feeling strong this week" {
		t.Errorf("content = %q, want %q", body.Note.Content, "Feeling strong this week")
	}
}

func TestCreateNoteHandler_EmptyContent(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"content":""}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/notes", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Delete me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/stride/notes/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/stride/notes/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- Plan handler tests ---

func TestListPlansHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans", nil), 1)
	rec := httptest.NewRecorder()
	ListPlansHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Plans []Plan `json:"plans"`
		Total int    `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 0 {
		t.Errorf("total = %d, want 0", body.Total)
	}
	if len(body.Plans) != 0 {
		t.Errorf("len(plans) = %d, want 0", len(body.Plans))
	}
}

func TestListPlansHandler_WithPlans(t *testing.T) {
	db := setupTestDB(t)
	insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)
	insertTestPlan(t, db, 1, "2026-04-14", "2026-04-20", `[]`)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans?limit=1&offset=0", nil), 1)
	rec := httptest.NewRecorder()
	ListPlansHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plans  []Plan `json:"plans"`
		Total  int    `json:"total"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 2 {
		t.Errorf("total = %d, want 2", body.Total)
	}
	if len(body.Plans) != 1 {
		t.Errorf("len(plans) = %d, want 1 (limited)", len(body.Plans))
	}
	if body.Limit != 1 {
		t.Errorf("limit = %d, want 1", body.Limit)
	}
}

func TestGetCurrentPlanHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetCurrentPlanHandler_Found(t *testing.T) {
	db := setupTestDB(t)
	// Insert a plan that covers today (use a wide range to avoid flakiness).
	insertTestPlan(t, db, 1, "2020-01-01", "2099-12-31", `[]`)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans/current", nil), 1)
	rec := httptest.NewRecorder()
	GetCurrentPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plan Plan `json:"plan"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Plan.WeekStart != "2020-01-01" {
		t.Errorf("plan.WeekStart = %q, want 2020-01-01", body.Plan.WeekStart)
	}
}

func TestGetPlanHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans/notanumber", nil), 1)
	req = withChiParam(req, "id", "notanumber")
	rec := httptest.NewRecorder()
	GetPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetPlanHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GetPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetPlanHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	id := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)
	idStr := strconv.FormatInt(id, 10)

	req := withUser(httptest.NewRequest("GET", "/api/stride/plans/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	GetPlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plan Plan `json:"plan"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Plan.ID != id {
		t.Errorf("plan.ID = %d, want %d", body.Plan.ID, id)
	}
}

func TestGeneratePlanHandler_StrideNotEnabled(t *testing.T) {
	// stride_enabled is not set → GeneratePlan is a no-op, no plan stored → 422.
	db := extendedTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/stride/plans/generate", nil), 1)
	rec := httptest.NewRecorder()
	GeneratePlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGeneratePlanHandler_ClaudeNotEnabled(t *testing.T) {
	// stride_enabled=true but claude_enabled=false → ErrClaudeNotEnabled → 400.
	db := extendedTestDB(t)
	if _, err := db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	req := withUser(httptest.NewRequest("POST", "/api/stride/plans/generate", nil), 1)
	rec := httptest.NewRecorder()
	GeneratePlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGeneratePlanHandler_Success(t *testing.T) {
	db := extendedTestDB(t)
	prefs := []struct{ k, v string }{
		{"stride_enabled", "true"},
		{"claude_enabled", "true"},
		{"claude_model", "claude-opus-4-5"},
	}
	for _, p := range prefs {
		if _, err := db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)", p.k, p.v); err != nil {
			t.Fatalf("set pref %s: %v", p.k, err)
		}
	}

	weekStart, _ := upcomingWeek()
	planDays := buildMinimalPlan(weekStart)
	mockJSON, _ := json.Marshal(planDays)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return string(mockJSON), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	req := withUser(httptest.NewRequest("POST", "/api/stride/plans/generate", nil), 1)
	rec := httptest.NewRecorder()
	GeneratePlanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plan Plan `json:"plan"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Plan.WeekStart != weekStart {
		t.Errorf("plan.WeekStart = %q, want %q", body.Plan.WeekStart, weekStart)
	}
}
