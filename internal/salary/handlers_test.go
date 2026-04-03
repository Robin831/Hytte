package salary

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

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
