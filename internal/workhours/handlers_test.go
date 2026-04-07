package workhours

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// withUser attaches a user to the request context.
func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

// withChiParam adds a chi URL parameter to the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

var testUser = &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

// jsonBody encodes v as JSON and returns a bytes.Buffer.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return &buf
}

// --- DayGetHandler ---

func TestDayGetHandler_NoEntry(t *testing.T) {
	db := setupTestDB(t)
	handler := DayGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/day?date=2026-03-01", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["day"] != nil {
		t.Errorf("expected null day, got %v", body["day"])
	}
}

func TestDayGetHandler_WithEntry(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertDay(db, 1, "2026-03-10", true, "notes"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := DayGetHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/workhours/day?date=2026-03-10", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["day"] == nil {
		t.Error("expected day, got nil")
	}
	if body["summary"] == nil {
		t.Error("expected summary, got nil")
	}
}

func TestDayGetHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	handler := DayGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/day?date=not-a-date", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- DayPutHandler ---

func TestDayPutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := DayPutHandler(db)

	body := jsonBody(t, map[string]any{
		"date":  "2026-03-15",
		"lunch": true,
		"notes": "test",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/day", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["day"] == nil {
		t.Error("expected day in response")
	}
}

func TestDayPutHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	handler := DayPutHandler(db)

	req := withUser(httptest.NewRequest("PUT", "/api/workhours/day", bytes.NewBufferString("not json")), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDayPutHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	handler := DayPutHandler(db)

	body := jsonBody(t, map[string]any{"date": "2026-99-99"})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/day", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- DayDeleteHandler ---

func TestDayDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertDay(db, 1, "2026-03-20", false, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := DayDeleteHandler(db)
	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day?date=2026-03-20", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestDayDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := DayDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day?date=2026-01-01", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDayDeleteHandler_MissingDate(t *testing.T) {
	db := setupTestDB(t)
	handler := DayDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- SessionAddHandler ---

func TestSessionAddHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := SessionAddHandler(db)
	body := jsonBody(t, map[string]any{
		"day_id":     day.ID,
		"start_time": "09:00",
		"end_time":   "17:00",
		"sort_order": 0,
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/session", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var session WorkSession
	if err := json.NewDecoder(rec.Body).Decode(&session); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if session.StartTime != "09:00" {
		t.Errorf("start_time: got %q", session.StartTime)
	}
}

func TestSessionAddHandler_InvalidTime(t *testing.T) {
	db := setupTestDB(t)
	handler := SessionAddHandler(db)

	body := jsonBody(t, map[string]any{
		"day_id":     1,
		"start_time": "25:00",
		"end_time":   "17:00",
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/session", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSessionAddHandler_DayNotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := SessionAddHandler(db)

	body := jsonBody(t, map[string]any{
		"day_id":     999,
		"start_time": "09:00",
		"end_time":   "17:00",
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/session", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- SessionDeleteHandler ---

func TestSessionDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := SessionDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day/session/999", nil), testUser)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSessionDeleteHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	handler := SessionDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day/session/abc", nil), testUser)
	req = withChiParam(req, "id", "abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- DeductionAddHandler ---

func TestDeductionAddHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := DeductionAddHandler(db)
	body := jsonBody(t, map[string]any{
		"day_id":  day.ID,
		"name":    "Kindergarten",
		"minutes": 15,
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/deduction", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var d WorkDeduction
	if err := json.NewDecoder(rec.Body).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Name != "Kindergarten" {
		t.Errorf("name: got %q", d.Name)
	}
}

func TestDeductionAddHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)
	handler := DeductionAddHandler(db)

	body := jsonBody(t, map[string]any{"day_id": 1, "name": "", "minutes": 15})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/deduction", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeductionAddHandler_InvalidMinutes(t *testing.T) {
	db := setupTestDB(t)
	handler := DeductionAddHandler(db)

	body := jsonBody(t, map[string]any{"day_id": 1, "name": "Gym", "minutes": 0})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/deduction", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- PresetsListHandler ---

func TestPresetsListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetsListHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/presets", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Presets []WorkDeductionPreset `json:"presets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Presets) != 0 {
		t.Errorf("expected empty presets, got %d", len(body.Presets))
	}
}

// --- PresetCreateHandler ---

func TestPresetCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetCreateHandler(db)

	body := jsonBody(t, map[string]any{
		"name":            "Doctor",
		"default_minutes": 60,
		"icon":            "stethoscope",
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/presets", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var p WorkDeductionPreset
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "Doctor" {
		t.Errorf("name: got %q", p.Name)
	}
}

func TestPresetCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetCreateHandler(db)

	body := jsonBody(t, map[string]any{"name": "", "default_minutes": 30, "icon": "clock"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/presets", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPresetCreateHandler_InvalidMinutes(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetCreateHandler(db)

	body := jsonBody(t, map[string]any{"name": "Gym", "default_minutes": 0})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/presets", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- PresetDeleteHandler ---

func TestPresetDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/presets/999", nil), testUser)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- WeekSummaryHandler ---

func TestWeekSummaryHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := WeekSummaryHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/summary/week?date=2026-03-23", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["week_start"] == nil {
		t.Error("expected week_start in response")
	}
}

func TestWeekSummaryHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	handler := WeekSummaryHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/summary/week?date=bad", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- FlexPoolHandler ---

func TestFlexPoolHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := FlexPoolHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/flex", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["flex"]; !ok {
		t.Error("expected flex in response")
	}
}

// --- FlexResetHandler ---

func TestFlexResetHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := FlexResetHandler(db)

	req := withUser(httptest.NewRequest("POST", "/api/workhours/flex/reset", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["reset_date"] == "" {
		t.Error("expected reset_date in response")
	}
}

// --- SessionUpdateHandler ---

func TestSessionUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	session, err := AddSession(db, day.ID, 1, "09:00", "17:00", 0, false)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	handler := SessionUpdateHandler(db)
	body := jsonBody(t, map[string]any{
		"start_time": "10:00",
		"end_time":   "18:00",
		"sort_order": 0,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/day/session/1", body), testUser)
	req = withChiParam(req, "id", fmt.Sprintf("%d", session.ID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionUpdateHandler_EndNotAfterStart(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	session, err := AddSession(db, day.ID, 1, "09:00", "17:00", 0, false)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	handler := SessionUpdateHandler(db)
	body := jsonBody(t, map[string]any{
		"start_time": "17:00",
		"end_time":   "09:00",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/day/session/1", body), testUser)
	req = withChiParam(req, "id", fmt.Sprintf("%d", session.ID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionAddHandler_EndNotAfterStart(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := SessionAddHandler(db)
	body := jsonBody(t, map[string]any{
		"day_id":     day.ID,
		"start_time": "17:00",
		"end_time":   "09:00",
	})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/day/session", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- DeductionDeleteHandler ---

func TestDeductionDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-10", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ded, err := AddDeduction(db, day.ID, 1, "Lunch", 30, nil)
	if err != nil {
		t.Fatalf("add deduction: %v", err)
	}

	handler := DeductionDeleteHandler(db)
	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day/deduction/1", nil), testUser)
	req = withChiParam(req, "id", fmt.Sprintf("%d", ded.ID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeductionDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := DeductionDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/day/deduction/999", nil), testUser)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- PresetUpdateHandler ---

func TestPresetUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	preset, err := CreatePreset(db, 1, "Doctor", 60, "stethoscope")
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}

	handler := PresetUpdateHandler(db)
	body := jsonBody(t, map[string]any{
		"name":            "Hospital",
		"default_minutes": 90,
		"icon":            "stethoscope",
		"active":          true,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/presets/1", body), testUser)
	req = withChiParam(req, "id", fmt.Sprintf("%d", preset.ID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated WorkDeductionPreset
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.Name != "Hospital" {
		t.Errorf("expected name Hospital, got %s", updated.Name)
	}
	if updated.DefaultMinutes != 90 {
		t.Errorf("expected 90 minutes, got %d", updated.DefaultMinutes)
	}
}

func TestPresetUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := PresetUpdateHandler(db)

	body := jsonBody(t, map[string]any{
		"name":            "X",
		"default_minutes": 30,
		"icon":            "clock",
		"active":          true,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/presets/999", body), testUser)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- MonthSummaryHandler ---

func TestMonthSummaryHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := MonthSummaryHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/summary/month?month=2026-03", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["month"] == nil {
		t.Error("expected month in response")
	}
}

func TestMonthSummaryHandler_InvalidFormat(t *testing.T) {
	db := setupTestDB(t)
	handler := MonthSummaryHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/summary/month?month=bad", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- LeaveDayListHandler ---

func TestLeaveDayListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayListHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/leave?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		LeaveDays []LeaveDay   `json:"leave_days"`
		Balance   LeaveBalance `json:"balance"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.LeaveDays) != 0 {
		t.Errorf("expected empty leave_days, got %d", len(body.LeaveDays))
	}
	if body.Balance.VacationAllowance != 25 {
		t.Errorf("expected default allowance 25, got %d", body.Balance.VacationAllowance)
	}
}

func TestLeaveDayListHandler_WithDays(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertLeaveDay(db, 1, "2026-07-01", LeaveTypeVacation, "beach"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := LeaveDayListHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/workhours/leave?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		LeaveDays []LeaveDay   `json:"leave_days"`
		Balance   LeaveBalance `json:"balance"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.LeaveDays) != 1 {
		t.Fatalf("expected 1 leave day, got %d", len(body.LeaveDays))
	}
	if body.Balance.VacationUsed != 1 {
		t.Errorf("expected vacation_used=1, got %d", body.Balance.VacationUsed)
	}
}

func TestLeaveDayListHandler_InvalidYear(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayListHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/leave?year=bad", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- LeaveDayPutHandler ---

func TestLeaveDayPutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayPutHandler(db)

	body := jsonBody(t, map[string]any{
		"date":       "2026-07-15",
		"leave_type": "vacation",
		"note":       "holiday",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/leave", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var ld LeaveDay
	if err := json.NewDecoder(rec.Body).Decode(&ld); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ld.Date != "2026-07-15" {
		t.Errorf("date: got %q", ld.Date)
	}
	if ld.LeaveType != LeaveTypeVacation {
		t.Errorf("leave_type: got %q", ld.LeaveType)
	}
}

func TestLeaveDayPutHandler_InvalidLeaveType(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayPutHandler(db)

	body := jsonBody(t, map[string]any{
		"date":       "2026-07-15",
		"leave_type": "holiday",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/leave", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLeaveDayPutHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayPutHandler(db)

	body := jsonBody(t, map[string]any{
		"date":       "not-a-date",
		"leave_type": "sick",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/leave", body), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLeaveDayPutHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayPutHandler(db)

	req := withUser(httptest.NewRequest("PUT", "/api/workhours/leave", bytes.NewBufferString("not json")), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- LeaveDayDeleteHandler ---

func TestLeaveDayDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertLeaveDay(db, 1, "2026-07-20", LeaveTypeSick, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := LeaveDayDeleteHandler(db)
	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/leave?date=2026-07-20", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestLeaveDayDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/leave?date=2026-07-01", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLeaveDayDeleteHandler_MissingDate(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveDayDeleteHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/leave", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- LeaveBalanceHandler ---

func TestLeaveBalanceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertLeaveDay(db, 1, "2026-07-01", LeaveTypeVacation, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	handler := LeaveBalanceHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/workhours/leave/balance?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var balance LeaveBalance
	if err := json.NewDecoder(rec.Body).Decode(&balance); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if balance.VacationUsed != 1 {
		t.Errorf("expected vacation_used=1, got %d", balance.VacationUsed)
	}
	if balance.VacationAllowance != 25 {
		t.Errorf("expected default allowance 25, got %d", balance.VacationAllowance)
	}
}

func TestLeaveBalanceHandler_InvalidYear(t *testing.T) {
	db := setupTestDB(t)
	handler := LeaveBalanceHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/leave/balance?year=xyz", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- PunchInHandler ---

func TestPunchInHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchInHandler(db)

	body := jsonBody(t, map[string]any{"date": "2026-03-30", "start_time": "08:00"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-in", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var s OpenSession
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.StartTime != "08:00" {
		t.Errorf("StartTime: got %q, want %q", s.StartTime, "08:00")
	}
	if s.Date != "2026-03-30" {
		t.Errorf("Date: got %q, want %q", s.Date, "2026-03-30")
	}
}

func TestPunchInHandler_InvalidTime(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchInHandler(db)

	body := jsonBody(t, map[string]any{"date": "2026-03-30", "start_time": "not-a-time"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-in", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPunchInHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchInHandler(db)

	body := jsonBody(t, map[string]any{"date": "not-a-date", "start_time": "08:00"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-in", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- GetPunchSessionHandler ---

func TestGetPunchSessionHandler_NoSession(t *testing.T) {
	db := setupTestDB(t)
	handler := GetPunchSessionHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/punch-session", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["session"] != nil {
		t.Errorf("expected null session, got %v", body["session"])
	}
}

func TestGetPunchSessionHandler_WithSession(t *testing.T) {
	db := setupTestDB(t)
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:30"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := GetPunchSessionHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/workhours/punch-session", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Session *OpenSession `json:"session"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session == nil {
		t.Fatal("expected session, got nil")
	}
	if body.Session.StartTime != "08:30" {
		t.Errorf("StartTime: got %q, want %q", body.Session.StartTime, "08:30")
	}
}

// --- DeletePunchSessionHandler ---

func TestDeletePunchSessionHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := DeletePunchSessionHandler(db)

	req := withUser(httptest.NewRequest("DELETE", "/api/workhours/punch-session", nil), testUser)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	s, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession: %v", err)
	}
	if s != nil {
		t.Error("expected session to be deleted")
	}
}

// --- PunchOutHandler ---

func TestPunchOutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := PunchOutHandler(db)

	body := jsonBody(t, map[string]any{"end_time": "16:00"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-out", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["day"] == nil {
		t.Error("expected day in response, got nil")
	}

	// Open session should be gone.
	s, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession: %v", err)
	}
	if s != nil {
		t.Error("expected open session to be deleted after punch-out")
	}
}

func TestPunchOutHandler_NoActiveSession(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchOutHandler(db)

	body := jsonBody(t, map[string]any{"end_time": "16:00"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-out", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPunchOutHandler_EndBeforeStart(t *testing.T) {
	db := setupTestDB(t)
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "10:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := PunchOutHandler(db)

	body := jsonBody(t, map[string]any{"end_time": "09:00"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-out", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPunchOutHandler_InvalidEndTime(t *testing.T) {
	db := setupTestDB(t)
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := PunchOutHandler(db)

	body := jsonBody(t, map[string]any{"end_time": "not-a-time"})
	req := withUser(httptest.NewRequest("POST", "/api/workhours/punch-out", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- PunchEditHandler ---

func TestPunchEditHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	// Use a past date so the future-time guard never applies regardless of wall clock.
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := PunchEditHandler(db)

	// Use a start_time guaranteed to be valid for the past-date session.
	wantTime := "07:30"
	body := jsonBody(t, map[string]any{"start_time": wantTime})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/punch/edit", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var s OpenSession
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.StartTime != wantTime {
		t.Errorf("StartTime: got %q, want %q", s.StartTime, wantTime)
	}
}

func TestPunchEditHandler_NoSession(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchEditHandler(db)

	body := jsonBody(t, map[string]any{"start_time": "07:30"})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/punch/edit", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPunchEditHandler_InvalidTime(t *testing.T) {
	db := setupTestDB(t)
	handler := PunchEditHandler(db)

	body := jsonBody(t, map[string]any{"start_time": "not-a-time"})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/punch/edit", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPunchEditHandler_FutureTime(t *testing.T) {
	db := setupTestDB(t)
	// Use today's date so the future-time guard is active.
	today := time.Now().Format("2006-01-02")
	if _, err := CreateOpenSession(db, 1, today, "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	handler := PunchEditHandler(db)

	// Round up to the next full minute and add a larger buffer so the
	// formatted HH:MM value remains strictly in the future for the handler.
	now := time.Now()
	futureTime := now.Truncate(time.Minute).Add(11 * time.Minute).Format("15:04")
	body := jsonBody(t, map[string]any{"start_time": futureTime})
	req := withUser(httptest.NewRequest("PUT", "/api/workhours/punch/edit", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for future start_time, got %d: %s", rec.Code, rec.Body.String())
	}
}

