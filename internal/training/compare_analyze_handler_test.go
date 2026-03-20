package training

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareAnalyzeHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/analyze?a=1&b=2", nil)
	req = withUser(req, 1) // non-admin
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCompareAnalyzeHandler_MissingParams(t *testing.T) {
	db := setupTestDB(t)

	tests := []struct {
		name string
		url  string
	}{
		{"missing both", "/api/training/compare/analyze"},
		{"missing b", "/api/training/compare/analyze?a=1"},
		{"missing a", "/api/training/compare/analyze?b=2"},
		{"invalid a", "/api/training/compare/analyze?a=abc&b=2"},
		{"invalid b", "/api/training/compare/analyze?a=1&b=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.url, nil)
			req = withAdminUser(req, 1)
			w := httptest.NewRecorder()

			CompareAnalyzeHandler(db)(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCompareAnalyzeHandler_SameWorkout(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/analyze?a=1&b=1", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for same workout, got %d", w.Code)
	}
}

func TestCompareAnalyzeHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/analyze?a=999&b=998", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompareAnalyzeHandler_CacheHit(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 310, 290)

	// Pre-populate cache.
	analysis := &ComparisonAnalysis{
		Summary:      "Cached comparison",
		Strengths:    []string{"Lower HR"},
		Weaknesses:   []string{"Slower pace"},
		Observations: []string{"Similar structure"},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "claude-sonnet-4-6", "test prompt", "2026-03-19T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	url := fmt.Sprintf("/api/training/compare/analyze?a=%d&b=%d", idA, idB)
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var cached CachedComparisonAnalysis
	if err := json.Unmarshal(resp["analysis"], &cached); err != nil {
		t.Fatalf("unmarshal analysis: %v", err)
	}
	if cached.Summary != "Cached comparison" {
		t.Errorf("expected cached summary, got: %s", cached.Summary)
	}
	if !cached.Cached {
		t.Error("expected cached=true for cache hit")
	}
}

func TestCompareAnalyzeHandler_CacheMiss(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 310, 290)

	// Mock Claude response.
	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		return `{"summary":"Fresh comparison","strengths":["Better HR"],"weaknesses":["Slower"],"observations":["Same route"]}`, nil
	}

	// Enable Claude for the test user.
	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', 'claude')`)
	if err != nil {
		t.Fatal(err)
	}

	url := fmt.Sprintf("/api/training/compare/analyze?a=%d&b=%d", idA, idB)
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	var result CachedComparisonAnalysis
	if err := json.Unmarshal(resp["analysis"], &result); err != nil {
		t.Fatalf("unmarshal analysis: %v", err)
	}
	if result.Cached {
		t.Error("expected cached=false on first request")
	}
	if result.Summary != "Fresh comparison" {
		t.Errorf("unexpected summary: %s", result.Summary)
	}
	if result.WorkoutIDA != idA || result.WorkoutIDB != idB {
		t.Errorf("unexpected workout IDs: %d, %d", result.WorkoutIDA, result.WorkoutIDB)
	}

	// Second request should be a cache hit.
	req2 := httptest.NewRequest(http.MethodPost, url, nil)
	req2 = withAdminUser(req2, 1)
	w2 := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("cache hit: expected 200, got %d", w2.Code)
	}
	var resp2 map[string]json.RawMessage
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal response2: %v", err)
	}
	var result2 CachedComparisonAnalysis
	if err := json.Unmarshal(resp2["analysis"], &result2); err != nil {
		t.Fatalf("unmarshal analysis2: %v", err)
	}
	if !result2.Cached {
		t.Error("cache hit: expected cached=true on second request")
	}
}

func TestCompareAnalyzeHandler_ForceBypassesCache(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 310, 290)

	// Pre-populate cache with old content.
	cached := &ComparisonAnalysis{
		Summary:      "Old cached summary",
		Strengths:    []string{"Old strength"},
		Weaknesses:   []string{"Old weakness"},
		Observations: []string{"Old observation"},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, cached, "claude-sonnet-4-6", "test prompt", "2026-03-19T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	// Mock Claude response with fresh content.
	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	called := false
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		called = true
		return `{"summary":"Force-refreshed summary","strengths":["New strength"],"weaknesses":["New weakness"],"observations":["New observation"]}`, nil
	}

	// Enable Claude for the test user.
	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', 'claude')`)
	if err != nil {
		t.Fatal(err)
	}

	url := fmt.Sprintf("/api/training/compare/analyze?a=%d&b=%d&force=1", idA, idB)
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	CompareAnalyzeHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !called {
		t.Error("expected runPromptFunc to be called when force=1, but it was not")
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	var result CachedComparisonAnalysis
	if err := json.Unmarshal(resp["analysis"], &result); err != nil {
		t.Fatalf("unmarshal analysis: %v", err)
	}
	if result.Cached {
		t.Error("expected cached=false when force=1 bypasses cache")
	}
	if result.Summary != "Force-refreshed summary" {
		t.Errorf("expected fresh summary, got: %s", result.Summary)
	}
}

func TestBuildComparisonAnalysisPrompt(t *testing.T) {
	wA := &Workout{
		Sport:           "running",
		Title:           "Morning Run",
		StartedAt:       "2026-03-10T08:00:00Z",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    155,
		MaxHeartRate:    175,
		AvgPaceSecPerKm: 360,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 360, DistanceMeters: 1000, AvgHeartRate: 150, MaxHeartRate: 165, AvgPaceSecPerKm: 360},
			{LapNumber: 2, DurationSeconds: 355, DistanceMeters: 1000, AvgHeartRate: 158, MaxHeartRate: 170, AvgPaceSecPerKm: 355},
		},
	}
	wB := &Workout{
		Sport:           "running",
		Title:           "Evening Run",
		StartedAt:       "2026-03-17T18:00:00Z",
		DurationSeconds: 1750,
		DistanceMeters:  5100,
		AvgHeartRate:    152,
		MaxHeartRate:    172,
		AvgPaceSecPerKm: 343,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 350, DistanceMeters: 1000, AvgHeartRate: 148, MaxHeartRate: 162, AvgPaceSecPerKm: 350},
			{LapNumber: 2, DurationSeconds: 340, DistanceMeters: 1000, AvgHeartRate: 155, MaxHeartRate: 168, AvgPaceSecPerKm: 340},
		},
	}

	comparison := &ComparisonResult{
		Compatible: true,
		LapDeltas: []LapDelta{
			{LapNumber: 1, LapNumberA: 1, LapNumberB: 1, AvgHRA: 150, AvgHRB: 148, HRDelta: -2, PaceA: 360, PaceB: 350, PaceDelta: -10},
			{LapNumber: 2, LapNumberA: 2, LapNumberB: 2, AvgHRA: 158, AvgHRB: 155, HRDelta: -3, PaceA: 355, PaceB: 340, PaceDelta: -15},
		},
		Summary: &ComparisonSummary{AvgHRDelta: -2.5, AvgPaceDelta: -12.5, Verdict: "improving — faster pace at similar HR"},
	}

	prompt := buildComparisonAnalysisPrompt(wA, wB, comparison)

	checks := []struct {
		label string
		want  string
	}{
		{"workout A header", "=== Workout A ==="},
		{"workout B header", "=== Workout B ==="},
		{"sport", "running"},
		{"title A", "Morning Run"},
		{"title B", "Evening Run"},
		{"lap table header", "Lap-by-Lap Comparison"},
		{"HR data", "150"},
		{"verdict", "improving"},
		{"JSON structure", `"summary"`},
	}
	for _, c := range checks {
		if !contains(prompt, c.want) {
			t.Errorf("prompt missing %s (%q)", c.label, c.want)
		}
	}
}

func TestBuildComparisonAnalysisPrompt_Incompatible(t *testing.T) {
	wA := &Workout{Sport: "running", StartedAt: "2026-03-10T08:00:00Z", DurationSeconds: 1800, DistanceMeters: 5000}
	wB := &Workout{Sport: "running", StartedAt: "2026-03-17T18:00:00Z", DurationSeconds: 1800, DistanceMeters: 5000}

	comparison := &ComparisonResult{
		Compatible: false,
		Reason:     "different number of laps",
	}

	prompt := buildComparisonAnalysisPrompt(wA, wB, comparison)

	if !contains(prompt, "not structurally compatible") {
		t.Error("prompt should mention incompatibility")
	}
	if !contains(prompt, "different number of laps") {
		t.Error("prompt should include the reason")
	}
}

func TestParseComparisonAnalysisResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "plain JSON",
			input: `{"summary":"Good improvement","strengths":["Lower HR"],"weaknesses":["Consistency"],"observations":["Similar route"]}`,
		},
		{
			name:  "markdown fenced",
			input: "```json\n" + `{"summary":"Fenced","strengths":[],"weaknesses":[],"observations":[]}` + "\n```",
		},
		{
			name:  "with preamble text",
			input: "Here is the analysis:\n" + `{"summary":"With preamble","strengths":[],"weaknesses":[],"observations":[]}`,
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis, err := parseComparisonAnalysisResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if analysis.Summary == "" {
				t.Error("summary should not be empty")
			}
		})
	}
}

func TestParseComparisonAnalysisResponse_NilSlices(t *testing.T) {
	raw := `{"summary":"Test"}`
	analysis, err := parseComparisonAnalysisResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Strengths == nil {
		t.Error("strengths should be [] not nil")
	}
	if analysis.Weaknesses == nil {
		t.Error("weaknesses should be [] not nil")
	}
	if analysis.Observations == nil {
		t.Error("observations should be [] not nil")
	}
}

func TestFormatPace(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{360, "6:00"},
		{305, "5:05"},
		{0, "--:--"},
		{-1, "--:--"},
	}
	for _, tt := range tests {
		got := formatPace(tt.input)
		if got != tt.want {
			t.Errorf("formatPace(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatPaceDelta(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{15, "+0:15"},
		{-10, "-0:10"},
		{0, "+0:00"},
		{65, "+1:05"},
	}
	for _, tt := range tests {
		got := formatPaceDelta(tt.input)
		if got != tt.want {
			t.Errorf("formatPaceDelta(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
