package training

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// ---------------------------------------------------------------------------
// GetTrainingLoadHandler
// ---------------------------------------------------------------------------

func TestGetTrainingLoadHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/load", nil), 1)
	w := httptest.NewRecorder()

	GetTrainingLoadHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	weeks, ok := resp["weeks"].([]any)
	if !ok {
		t.Fatal("expected weeks array")
	}
	if len(weeks) != 0 {
		t.Errorf("expected empty weeks, got %d", len(weeks))
	}
	if resp["status"] != string(StatusInsufficientData) {
		t.Errorf("status: want %s, got %v", StatusInsufficientData, resp["status"])
	}
}

func TestGetTrainingLoadHandler_WithData(t *testing.T) {
	db := setupTestDB(t)

	// Insert 5 weekly loads so ACR can be computed and status classified.
	for i := 0; i < 5; i++ {
		ws := time.Date(2026, 3, 17-i*7, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		wl := WeeklyLoad{
			UserID: 1, WeekStart: ws,
			EasyLoad: 80.0, HardLoad: 40.0, TotalLoad: 120.0,
			WorkoutCount: 3,
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		if err := UpsertWeeklyLoad(db, wl); err != nil {
			t.Fatalf("UpsertWeeklyLoad(%s): %v", ws, err)
		}
	}

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/load?weeks=4", nil), 1)
	w := httptest.NewRecorder()

	GetTrainingLoadHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	weeks, ok := resp["weeks"].([]any)
	if !ok {
		t.Fatal("expected weeks array")
	}
	// weeks param=4 should trim to 4 even though we have 5.
	if len(weeks) != 4 {
		t.Errorf("expected 4 weeks (trimmed), got %d", len(weeks))
	}
	if _, ok := resp["acute_load"]; !ok {
		t.Error("expected acute_load field")
	}
	if _, ok := resp["chronic_load"]; !ok {
		t.Error("expected chronic_load field")
	}
	if _, ok := resp["status"]; !ok {
		t.Error("expected status field")
	}
}

func TestGetTrainingLoadHandler_WeeksParamDefault(t *testing.T) {
	db := setupTestDB(t)

	// Insert 15 weekly loads.
	for i := 0; i < 15; i++ {
		ws := time.Date(2026, 3, 17-i*7, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		wl := WeeklyLoad{
			UserID: 1, WeekStart: ws, TotalLoad: 100.0,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := UpsertWeeklyLoad(db, wl); err != nil {
			t.Fatalf("UpsertWeeklyLoad: %v", err)
		}
	}

	// Default is 12 weeks.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/load", nil), 1)
	w := httptest.NewRecorder()
	GetTrainingLoadHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	weeks := resp["weeks"].([]any)
	if len(weeks) != 12 {
		t.Errorf("expected 12 weeks (default), got %d", len(weeks))
	}
}

func TestGetTrainingLoadHandler_InvalidWeeksParam_UsesDefault(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/load?weeks=bad", nil), 1)
	w := httptest.NewRecorder()
	GetTrainingLoadHandler(db)(w, req)

	// Should not error — bad param is silently ignored.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with bad weeks param, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// AnalyzeTrainingSummaryHandler
// ---------------------------------------------------------------------------

func TestAnalyzeTrainingSummaryHandler_NonAdmin(t *testing.T) {
	db := setupTestDB(t)

	body := `{"period":"week","date":"2026-03-17"}`
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w := httptest.NewRecorder()

	AnalyzeTrainingSummaryHandler(db)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}
}

func TestAnalyzeTrainingSummaryHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString("{bad json")), 1)
	w := httptest.NewRecorder()

	AnalyzeTrainingSummaryHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestAnalyzeTrainingSummaryHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)

	body := `{"period":"week","date":"not-a-date"}`
	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w := httptest.NewRecorder()

	AnalyzeTrainingSummaryHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date, got %d", w.Code)
	}
}

func TestAnalyzeTrainingSummaryHandler_ClaudeDisabled(t *testing.T) {
	db := setupTestDB(t)

	// Claude not enabled by default.
	body := `{"period":"week","date":"2026-03-17"}`
	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w := httptest.NewRecorder()

	AnalyzeTrainingSummaryHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when claude disabled, got %d", w.Code)
	}
}

func TestAnalyzeTrainingSummaryHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	if err := auth.SetPreference(db, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}
	if err := auth.SetPreference(db, 1, "claude_model", "claude-sonnet-4-6"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		return `{
			"overview": "Good training week",
			"key_insights": ["insight1"],
			"strengths": ["consistent"],
			"concerns": [],
			"recommendations": ["keep it up"],
			"risk_flags": []
		}`, nil
	}

	body := `{"period":"week","date":"2026-03-17"}`
	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w := httptest.NewRecorder()

	AnalyzeTrainingSummaryHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SummaryAnalysisResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Cached {
		t.Error("expected cached=false on fresh generation")
	}
	if resp.Analysis.Overview != "Good training week" {
		t.Errorf("overview: want 'Good training week', got %q", resp.Analysis.Overview)
	}
	if resp.Period != "week" {
		t.Errorf("period: want 'week', got %q", resp.Period)
	}
	if resp.PeriodStart != "2026-03-16" {
		t.Errorf("period_start: want '2026-03-16' (Monday of 2026-03-17 week), got %q", resp.PeriodStart)
	}
}

func TestAnalyzeTrainingSummaryHandler_CachedResponse(t *testing.T) {
	db := setupTestDB(t)

	if err := auth.SetPreference(db, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	promptCalls := 0
	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		promptCalls++
		return `{
			"overview": "Cached overview",
			"key_insights": [],
			"strengths": [],
			"concerns": [],
			"recommendations": [],
			"risk_flags": []
		}`, nil
	}

	body := `{"period":"week","date":"2026-03-17"}`

	// First request — generates and caches.
	req1 := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w1 := httptest.NewRecorder()
	AnalyzeTrainingSummaryHandler(db)(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second request — should use the cache.
	req2 := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w2 := httptest.NewRecorder()
	AnalyzeTrainingSummaryHandler(db)(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp SummaryAnalysisResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal second response: %v", err)
	}
	if !resp.Cached {
		t.Error("expected cached=true on second request")
	}
	if promptCalls != 1 {
		t.Errorf("expected runPromptFunc called once, got %d", promptCalls)
	}
}

func TestAnalyzeTrainingSummaryHandler_CachedLoadValuesRounded(t *testing.T) {
	db := setupTestDB(t)

	if err := auth.SetPreference(db, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		return `{"overview":"x","key_insights":[],"strengths":[],"concerns":[],"recommendations":[],"risk_flags":[]}`, nil
	}

	body := `{"period":"week","date":"2026-03-17"}`

	// First request generates and stores unrounded values in DB.
	req1 := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w1 := httptest.NewRecorder()
	AnalyzeTrainingSummaryHandler(db)(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request returns from cache — load values must be rounded the same way.
	req2 := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/summary/analyze", bytes.NewBufferString(body)), 1)
	w2 := httptest.NewRecorder()
	AnalyzeTrainingSummaryHandler(db)(w2, req2)

	var fresh, cached SummaryAnalysisResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &fresh); err != nil {
		t.Fatalf("unmarshal fresh: %v", err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &cached); err != nil {
		t.Fatalf("unmarshal cached: %v", err)
	}
	if fresh.AcuteLoad != cached.AcuteLoad {
		t.Errorf("AcuteLoad inconsistency: fresh=%v cached=%v", fresh.AcuteLoad, cached.AcuteLoad)
	}
	if fresh.ChronicLoad != cached.ChronicLoad {
		t.Errorf("ChronicLoad inconsistency: fresh=%v cached=%v", fresh.ChronicLoad, cached.ChronicLoad)
	}
}

// ---------------------------------------------------------------------------
// UpsertTrainingSummaryAnalysis / getTrainingSummaryByPeriod
// ---------------------------------------------------------------------------

func TestUpsertTrainingSummaryAnalysis_RoundTrip(t *testing.T) {
	db := setupTestDB(t)

	encPrompt, err := encryption.EncryptField("test prompt")
	if err != nil {
		t.Fatalf("encrypt prompt: %v", err)
	}
	encResp, err := encryption.EncryptField(`{"overview":"test"}`)
	if err != nil {
		t.Fatalf("encrypt response: %v", err)
	}

	acr := 1.1
	s := TrainingSummary{
		UserID:       1,
		Period:       "week",
		WeekStart:    "2026-03-16",
		Status:       StatusOptimal,
		ACR:          &acr,
		AcuteLoad:    110.0,
		ChronicLoad:  100.0,
		Prompt:       encPrompt,
		ResponseJSON: encResp,
		Model:        "claude-sonnet-4-6",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := UpsertTrainingSummaryAnalysis(db, s); err != nil {
		t.Fatalf("UpsertTrainingSummaryAnalysis: %v", err)
	}

	got, err := getTrainingSummaryByPeriod(db, 1, "week", "2026-03-16")
	if err != nil {
		t.Fatalf("getTrainingSummaryByPeriod: %v", err)
	}
	if got.Status != StatusOptimal {
		t.Errorf("Status: want %s, got %s", StatusOptimal, got.Status)
	}
	if got.ACR == nil || *got.ACR != acr {
		t.Errorf("ACR: want %v, got %v", acr, got.ACR)
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model: want 'claude-sonnet-4-6', got %s", got.Model)
	}

	// Verify encrypted fields decrypt correctly.
	plainPrompt, decErr := encryption.DecryptField(got.Prompt)
	if decErr != nil {
		t.Fatalf("decrypt prompt: %v", decErr)
	}
	if plainPrompt != "test prompt" {
		t.Errorf("decrypted prompt: want 'test prompt', got %q", plainPrompt)
	}
	plainResp, decErr := encryption.DecryptField(got.ResponseJSON)
	if decErr != nil {
		t.Fatalf("decrypt response: %v", decErr)
	}
	if plainResp != `{"overview":"test"}` {
		t.Errorf("decrypted response: want '{\"overview\":\"test\"}', got %q", plainResp)
	}
}

func TestGetTrainingSummaryByPeriod_NotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := getTrainingSummaryByPeriod(db, 1, "week", "2026-03-16")
	if err == nil {
		t.Fatal("expected error for missing row, got nil")
	}
}

func TestUpsertTrainingSummaryAnalysis_UpdatesExistingRow(t *testing.T) {
	db := setupTestDB(t)

	ws := "2026-03-16"
	first := TrainingSummary{
		UserID: 1, Period: "week", WeekStart: ws,
		Status: StatusIncreasing, AcuteLoad: 120.0, ChronicLoad: 100.0,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertTrainingSummaryAnalysis(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := TrainingSummary{
		UserID: 1, Period: "week", WeekStart: ws,
		Status: StatusOptimal, AcuteLoad: 100.0, ChronicLoad: 100.0,
		Model:     "claude-sonnet-4-6",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertTrainingSummaryAnalysis(db, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := getTrainingSummaryByPeriod(db, 1, "week", ws)
	if err != nil {
		t.Fatalf("getTrainingSummaryByPeriod: %v", err)
	}
	if got.Status != StatusOptimal {
		t.Errorf("Status after update: want %s, got %s", StatusOptimal, got.Status)
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model after update: want 'claude-sonnet-4-6', got %s", got.Model)
	}
}

// ---------------------------------------------------------------------------
// computePeriodStart
// ---------------------------------------------------------------------------

func TestComputePeriodStart_Week(t *testing.T) {
	tests := []struct {
		date      string
		wantStart string
	}{
		{"2026-03-17", "2026-03-16"}, // Tuesday → Monday
		{"2026-03-16", "2026-03-16"}, // Monday → same day
		{"2026-03-22", "2026-03-16"}, // Sunday → previous Monday
	}
	for _, tt := range tests {
		got, err := computePeriodStart("week", tt.date)
		if err != nil {
			t.Errorf("computePeriodStart(week, %s): %v", tt.date, err)
			continue
		}
		if got.Format("2006-01-02") != tt.wantStart {
			t.Errorf("computePeriodStart(week, %s) = %s, want %s", tt.date, got.Format("2006-01-02"), tt.wantStart)
		}
	}
}

func TestComputePeriodStart_Month(t *testing.T) {
	got, err := computePeriodStart("month", "2026-03-17")
	if err != nil {
		t.Fatalf("computePeriodStart: %v", err)
	}
	if got.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("month period start: want 2026-03-01, got %s", got.Format("2006-01-02"))
	}
}

func TestComputePeriodStart_InvalidPeriod(t *testing.T) {
	_, err := computePeriodStart("quarter", "2026-03-17")
	if err == nil {
		t.Fatal("expected error for unsupported period")
	}
}

func TestComputePeriodStart_InvalidDate(t *testing.T) {
	_, err := computePeriodStart("week", "not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}
