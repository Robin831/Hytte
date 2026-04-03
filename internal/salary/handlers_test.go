package salary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Robin831/Hytte/internal/auth"
)

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

var testUser = &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

func withUser(r *http.Request, u *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), u))
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return &buf
}

// --- ConfigGetHandler ---

func TestConfigGetHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	h := ConfigGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/config", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestConfigGetHandler_Found(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	h := ConfigGetHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/config", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp ConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BaseSalary != 60000 {
		t.Errorf("BaseSalary = %v, want 60000", resp.BaseSalary)
	}
	if len(resp.CommissionTiers) != 4 {
		t.Errorf("len(CommissionTiers) = %d, want 4", len(resp.CommissionTiers))
	}
}

// --- ConfigPutHandler ---

func TestConfigPutHandler_Create(t *testing.T) {
	db := setupTestDB(t)
	h := ConfigPutHandler(db)

	body := jsonBody(t, map[string]any{
		"base_salary":    55000,
		"hourly_rate":    600,
		"standard_hours": 7.5,
		"currency":       "NOK",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/salary/config", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BaseSalary != 55000 {
		t.Errorf("BaseSalary = %v, want 55000", resp.BaseSalary)
	}
	// Default tiers should be seeded on first create.
	if len(resp.CommissionTiers) != 4 {
		t.Errorf("len(CommissionTiers) = %d, want 4 (default seed)", len(resp.CommissionTiers))
	}
}

func TestConfigPutHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)
	h := ConfigPutHandler(db)

	req := withUser(httptest.NewRequest("PUT", "/api/salary/config", bytes.NewBufferString("not-json")), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestConfigPutHandler_WithCustomTiers(t *testing.T) {
	db := setupTestDB(t)
	h := ConfigPutHandler(db)

	body := jsonBody(t, map[string]any{
		"base_salary": 50000,
		"hourly_rate": 500,
		"currency":    "NOK",
		"commission_tiers": []map[string]any{
			{"floor": 0, "ceiling": 50000, "rate": 0},
			{"floor": 50000, "ceiling": 0, "rate": 0.25},
		},
	})
	req := withUser(httptest.NewRequest("PUT", "/api/salary/config", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.CommissionTiers) != 2 {
		t.Errorf("len(CommissionTiers) = %d, want 2", len(resp.CommissionTiers))
	}
}

// --- EstimateCurrentHandler ---

func TestEstimateCurrentHandler_NoConfig(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateCurrentHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/current", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestEstimateCurrentHandler_WithConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	h := EstimateCurrentHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/current", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EstimateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WorkingDays <= 0 {
		t.Errorf("WorkingDays = %d, want > 0", resp.WorkingDays)
	}
}

// --- EstimateMonthHandler ---

func TestEstimateMonthHandler_MissingParam(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateMonthHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestEstimateMonthHandler_InvalidFormat(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateMonthHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month?month=not-a-month", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestEstimateMonthHandler_NoConfig(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateMonthHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month?month=2026-03", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestEstimateMonthHandler_WithConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	h := EstimateMonthHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month?month=2026-03", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EstimateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Month != "2026-03" {
		t.Errorf("Month = %q, want 2026-03", resp.Month)
	}
}

func TestEstimateMonthHandler_UsesConfigEffectiveForRequestedMonth(t *testing.T) {
	db := setupTestDB(t)

	olderCfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, olderCfg); err != nil {
		t.Fatalf("SaveConfig olderCfg: %v", err)
	}
	if err := SeedDefaultTiers(db, olderCfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers olderCfg: %v", err)
	}

	h := EstimateMonthHandler(db)

	firstReq := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month?month=2026-03", nil), testUser)
	firstRec := httptest.NewRecorder()
	h.ServeHTTP(firstRec, firstReq)

	if firstRec.Code != http.StatusOK {
		t.Fatalf("first estimate expected 200, got %d: %s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp map[string]any
	if err := json.NewDecoder(bytes.NewReader(firstRec.Body.Bytes())).Decode(&firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	// Add a newer config that should NOT apply for 2026-03.
	newerCfg := &Config{
		UserID: 1, BaseSalary: 90000, HourlyRate: 900,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-06-01",
	}
	if err := SaveConfig(db, newerCfg); err != nil {
		t.Fatalf("SaveConfig newerCfg: %v", err)
	}
	if err := SeedDefaultTiers(db, newerCfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers newerCfg: %v", err)
	}

	secondReq := withUser(httptest.NewRequest("GET", "/api/salary/estimate/month?month=2026-03", nil), testUser)
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusOK {
		t.Fatalf("second estimate expected 200, got %d: %s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp map[string]any
	if err := json.NewDecoder(bytes.NewReader(secondRec.Body.Bytes())).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	firstJSON, _ := json.Marshal(firstResp)
	secondJSON, _ := json.Marshal(secondResp)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected past-month estimate to keep using the config effective for 2026-03; before=%s after=%s", firstJSON, secondJSON)
	}
}

// --- AbsenceCostHandler ---

func TestAbsenceCostHandler_NoConfig(t *testing.T) {
	db := setupTestDB(t)
	h := AbsenceCostHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/absence-cost", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAbsenceCostHandler_WithConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := AbsenceCostHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/absence-cost", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AbsenceCostResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Currency != "NOK" {
		t.Errorf("Currency = %q, want NOK", resp.Currency)
	}
	if resp.WorkingDays <= 0 {
		t.Errorf("WorkingDays = %d, want > 0", resp.WorkingDays)
	}
	if resp.CostPerDay <= 0 {
		t.Errorf("CostPerDay = %v, want > 0", resp.CostPerDay)
	}
}

// --- EstimateYearHandler ---

func TestEstimateYearHandler_InvalidYear(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateYearHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/year?year=not-a-year", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestEstimateYearHandler_NoConfig(t *testing.T) {
	db := setupTestDB(t)
	h := EstimateYearHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/year?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp YearEstimateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Year != 2026 {
		t.Errorf("Year = %d, want 2026", resp.Year)
	}
	if len(resp.Months) != 12 {
		t.Errorf("len(Months) = %d, want 12", len(resp.Months))
	}
}

func TestEstimateYearHandler_WithConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	h := EstimateYearHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/year?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp YearEstimateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Year != 2026 {
		t.Errorf("Year = %d, want 2026", resp.Year)
	}
	if len(resp.Months) != 12 {
		t.Fatalf("len(Months) = %d, want 12", len(resp.Months))
	}
	// All months should have working_days > 0.
	for _, mp := range resp.Months {
		if mp.WorkingDays <= 0 {
			t.Errorf("month %s: WorkingDays = %d, want > 0", mp.Month, mp.WorkingDays)
		}
	}
}

func TestEstimateYearHandler_WithConfirmedRecord(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Insert a confirmed (non-estimate) record for January.
	r := &Record{
		UserID: 1, Month: "2026-01", WorkingDays: 21, HoursWorked: 157.5,
		BaseAmount: 60000, Gross: 62000, Tax: 18600, Net: 43400, IsEstimate: false,
	}
	if err := SaveRecord(db, r); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	h := EstimateYearHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/estimate/year?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp YearEstimateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// January should be marked as actual (not estimate).
	janIdx := -1
	for i, mp := range resp.Months {
		if mp.Month == "2026-01" {
			janIdx = i
			break
		}
	}
	if janIdx < 0 {
		t.Fatal("month 2026-01 not found in response")
	}
	if resp.Months[janIdx].IsEstimate {
		t.Error("2026-01: IsEstimate should be false (confirmed record)")
	}
}

// --- RecordsGetHandler ---

func TestRecordsGetHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/records?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var records []Record
	if err := json.NewDecoder(rec.Body).Decode(&records); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(records))
	}
}

func TestRecordsGetHandler_InvalidYear(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/records?year=bad", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecordsGetHandler_WithRecords(t *testing.T) {
	db := setupTestDB(t)

	for _, month := range []string{"2026-01", "2026-02"} {
		r := &Record{UserID: 1, Month: month, WorkingDays: 20, IsEstimate: false}
		if err := SaveRecord(db, r); err != nil {
			t.Fatalf("SaveRecord %s: %v", month, err)
		}
	}

	h := RecordsGetHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/records?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var records []Record
	if err := json.NewDecoder(rec.Body).Decode(&records); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("len(records) = %d, want 2", len(records))
	}
}

// --- RecordsPutHandler ---

func TestRecordsPutHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsPutHandler(db)

	req := withUser(withChiParam(httptest.NewRequest("PUT", "/api/salary/records/not-a-month", nil), "month", "not-a-month"), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecordsPutHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsPutHandler(db)

	req := withUser(withChiParam(
		httptest.NewRequest("PUT", "/api/salary/records/2026-03", bytes.NewBufferString("not-json")),
		"month", "2026-03",
	), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecordsPutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsPutHandler(db)

	body := jsonBody(t, RecordPutRequest{
		HoursWorked: 157.5, BillableHours: 150, BaseAmount: 60000,
		Gross: 63000, Tax: 18900, Net: 44100,
	})
	req := withUser(withChiParam(
		httptest.NewRequest("PUT", "/api/salary/records/2026-03", body),
		"month", "2026-03",
	), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var r Record
	if err := json.NewDecoder(rec.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Month != "2026-03" {
		t.Errorf("Month = %q, want 2026-03", r.Month)
	}
	if r.HoursWorked != 157.5 {
		t.Errorf("HoursWorked = %v, want 157.5", r.HoursWorked)
	}
	if r.IsEstimate {
		t.Error("IsEstimate should be false for PUT record")
	}
}

// --- RecordsConfirmHandler ---

func TestRecordsConfirmHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsConfirmHandler(db)

	req := withUser(withChiParam(
		httptest.NewRequest("POST", "/api/salary/records/bad/confirm", nil),
		"month", "bad",
	), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecordsConfirmHandler_NoConfig(t *testing.T) {
	db := setupTestDB(t)
	h := RecordsConfirmHandler(db)

	prevMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
	req := withUser(withChiParam(
		httptest.NewRequest("POST", "/api/salary/records/"+prevMonth+"/confirm", nil),
		"month", prevMonth,
	), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRecordsConfirmHandler_WithConfig(t *testing.T) {
	db := setupTestDB(t)

	prev := time.Now().AddDate(0, -1, 0)
	prevMonth := prev.Format("2006-01")
	effectiveFrom := fmt.Sprintf("%d-01-01", prev.Year())

	cfg := &Config{
		UserID: 1, BaseSalary: 60000, HourlyRate: 500,
		StandardHours: 7.5, Currency: "NOK", EffectiveFrom: effectiveFrom,
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	h := RecordsConfirmHandler(db)
	req := withUser(withChiParam(
		httptest.NewRequest("POST", "/api/salary/records/"+prevMonth+"/confirm", nil),
		"month", prevMonth,
	), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var r Record
	if err := json.NewDecoder(rec.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Month != prevMonth {
		t.Errorf("Month = %q, want %s", r.Month, prevMonth)
	}
	if r.IsEstimate {
		t.Error("IsEstimate should be false for confirmed record")
	}

	// Verify the record was persisted.
	records, err := GetRecords(db, 1, int64(prev.Year()))
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	found := false
	for _, saved := range records {
		if saved.Month == prevMonth && !saved.IsEstimate {
			found = true
			break
		}
	}
	if !found {
		t.Error("confirmed record not found in DB with is_estimate=false")
	}
}

// --- TaxTableGetHandler ---

func TestTaxTableGetHandler_SeedsDefaults(t *testing.T) {
	db := setupTestDB(t)
	h := TaxTableGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/tax-table?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TaxTableResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Year != 2026 {
		t.Errorf("Year = %d, want 2026", resp.Year)
	}
	if len(resp.Brackets) == 0 {
		t.Error("expected default brackets to be seeded, got empty")
	}
}

func TestTaxTableGetHandler_InvalidYear(t *testing.T) {
	db := setupTestDB(t)
	h := TaxTableGetHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/tax-table?year=1800", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- TaxTablePutHandler ---

func TestTaxTablePutHandler_SavesAndReturns(t *testing.T) {
	db := setupTestDB(t)
	h := TaxTablePutHandler(db)

	body := jsonBody(t, TaxTablePutRequest{
		Year: 2026,
		Brackets: []TaxBracket{
			{IncomeFrom: 0, IncomeTo: 300000, Rate: 0.22},
			{IncomeFrom: 300000, IncomeTo: 0, Rate: 0.35},
		},
	})
	req := withUser(httptest.NewRequest("PUT", "/api/salary/tax-table", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TaxTableResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Brackets) != 2 {
		t.Errorf("len(Brackets) = %d, want 2", len(resp.Brackets))
	}
}

func TestTaxTablePutHandler_EmptyBrackets(t *testing.T) {
	db := setupTestDB(t)
	h := TaxTablePutHandler(db)

	body := jsonBody(t, TaxTablePutRequest{Year: 2026, Brackets: []TaxBracket{}})
	req := withUser(httptest.NewRequest("PUT", "/api/salary/tax-table", body), testUser)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- VacationHandler ---

func TestVacationHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	h := VacationHandler(db)

	req := withUser(httptest.NewRequest("GET", "/api/salary/vacation?year=2026", nil), testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp VacationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DaysAllowance != 25 {
		t.Errorf("DaysAllowance = %d, want 25", resp.DaysAllowance)
	}
	if resp.DaysUsed != 0 {
		t.Errorf("DaysUsed = %d, want 0", resp.DaysUsed)
	}
	if resp.DaysRemaining != 25 {
		t.Errorf("DaysRemaining = %d, want 25", resp.DaysRemaining)
	}
	if resp.FeriepengerPct != 10.2 {
		t.Errorf("FeriepengerPct = %v, want 10.2", resp.FeriepengerPct)
	}
}

func TestVacationHandler_WithVacationDays(t *testing.T) {
	db := setupTestDB(t)

	// Save a confirmed record with 5 vacation days and some gross.
	rec := &Record{
		UserID:       1,
		Month:        "2026-02",
		WorkingDays:  20,
		HoursWorked:  150,
		BillableHours: 150,
		Gross:        80000,
		Net:          55000,
		VacationDays: 5,
		IsEstimate:   false,
	}
	if err := SaveRecord(db, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	h := VacationHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/salary/vacation?year=2026", nil), testUser)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var v VacationResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.DaysUsed != 5 {
		t.Errorf("DaysUsed = %d, want 5", v.DaysUsed)
	}
	if v.DaysRemaining != 20 {
		t.Errorf("DaysRemaining = %d, want 20", v.DaysRemaining)
	}
	wantAccrued := 80000 * 0.102
	if v.FeriepengerAccrued != wantAccrued {
		t.Errorf("FeriepengerAccrued = %v, want %v", v.FeriepengerAccrued, wantAccrued)
	}
}
