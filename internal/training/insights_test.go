package training

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildInsightsPrompt(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-02-21T10:00:00Z",
		DurationSeconds: 3065,
		DistanceMeters:  10000,
		AvgHeartRate:    155,
		MaxHeartRate:    178,
		AvgPaceSecPerKm: 306.5,
		AvgCadence:      182,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 360, DistanceMeters: 1000, AvgHeartRate: 150, MaxHeartRate: 160, AvgPaceSecPerKm: 360},
			{LapNumber: 2, DurationSeconds: 300, DistanceMeters: 1000, AvgHeartRate: 165, MaxHeartRate: 175, AvgPaceSecPerKm: 300},
		},
	}

	prompt := buildInsightsPrompt(w, "", false, nil, "", "")

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "running") {
		t.Error("prompt should contain sport")
	}
	if !contains(prompt, "10.00 km") {
		t.Error("prompt should contain formatted distance")
	}
	if !contains(prompt, "155") {
		t.Error("prompt should contain avg HR")
	}
	if !contains(prompt, "| 1 |") {
		t.Error("prompt should contain lap table")
	}
}

func TestBuildInsightsPrompt_WithProfile(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-02-21T10:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  12000,
		AvgHeartRate:    155,
	}

	profile := "User Profile:\n- Max HR: 195 bpm\n- Threshold HR: 172 bpm\n"
	zones := []ZoneDistribution{
		{Zone: 1, Name: "Recovery", DurationS: 600, Percentage: 16.7},
		{Zone: 2, Name: "Aerobic", DurationS: 1800, Percentage: 50.0},
		{Zone: 3, Name: "Tempo", DurationS: 1200, Percentage: 33.3},
	}

	prompt := buildInsightsPrompt(w, profile, false, zones, "", "")

	if !contains(prompt, "Max HR: 195") {
		t.Error("prompt should contain user profile block")
	}
	if !contains(prompt, "HR Zone Distribution") {
		t.Error("prompt should contain zone distribution table")
	}
	if !contains(prompt, "Recovery") {
		t.Error("prompt should contain zone names")
	}
	if !contains(prompt, "threshold_context") {
		t.Error("prompt should include threshold_context in JSON schema")
	}
}

func TestBuildInsightsPrompt_WithHistoricalContext(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-03-24T08:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  12000,
		AvgHeartRate:    152,
	}

	historicalContext := "=== Weekly Training Summary (last 4 weeks) ===\nWeek 2026-03-17: 3 workouts, 35.0 km, avg HR 150\n\n=== Similar Past Workouts ===\n1. 2026-03-10 running 12.0 km avg HR 155 pace 5:10/km\n"

	prompt := buildInsightsPrompt(w, "", false, nil, historicalContext, "")

	if !contains(prompt, "Weekly Training Summary") {
		t.Error("prompt should contain weekly training summary")
	}
	if !contains(prompt, "Similar Past Workouts") {
		t.Error("prompt should contain similar past workouts section")
	}
	if !contains(prompt, "trend_analysis") {
		t.Error("prompt should include trend_analysis in JSON schema when history is present")
	}
	if !contains(prompt, "fitness_direction") {
		t.Error("prompt should include fitness_direction field in trend_analysis schema")
	}
}

func TestBuildInsightsPrompt_IncludesConfidenceSchema(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-03-25T08:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
	}

	// Without historical context.
	prompt := buildInsightsPrompt(w, "", false, nil, "", "")
	if !contains(prompt, "confidence_score") {
		t.Error("prompt without history should include confidence_score in JSON schema")
	}
	if !contains(prompt, "confidence_note") {
		t.Error("prompt without history should include confidence_note in JSON schema")
	}

	// With historical context (different schema branch).
	hist := "=== Weekly Training Summary ===\n3 workouts, 35.0 km\n"
	promptWithHist := buildInsightsPrompt(w, "", false, nil, hist, "")
	if !contains(promptWithHist, "confidence_score") {
		t.Error("prompt with history should include confidence_score in JSON schema")
	}
	if !contains(promptWithHist, "confidence_note") {
		t.Error("prompt with history should include confidence_note in JSON schema")
	}
}

func TestParseInsightsResponse_ConfidenceFields(t *testing.T) {
	raw := `{
		"effort_summary": "Good aerobic run",
		"pacing_analysis": "Even pacing",
		"hr_zones": "Mostly zone 2",
		"observations": [],
		"suggestions": [],
		"confidence_score": 0.82,
		"confidence_note": "Good HR and pace data available"
	}`
	insights, err := parseInsightsResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const eps = 1e-9
	if math.Abs(insights.ConfidenceScore-0.82) > eps {
		t.Errorf("expected confidence_score 0.82, got %f", insights.ConfidenceScore)
	}
	if insights.ConfidenceNote != "Good HR and pace data available" {
		t.Errorf("unexpected confidence_note: %s", insights.ConfidenceNote)
	}

	// Zero confidence_score should be present in JSON (field is always serialized).
	rawNoConf := `{"effort_summary":"Run","pacing_analysis":"OK","hr_zones":"Z2","observations":[],"suggestions":[]}`
	insightsNoConf, err := parseInsightsResponse(rawNoConf)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(insightsNoConf)
	if !contains(string(data), `"confidence_score"`) {
		t.Error("confidence_score should always be present in JSON output")
	}
}

func TestBuildInsightsPrompt_LongDuration(t *testing.T) {
	// Workouts > 1 hour should format as h:mm:ss, not 150:00.
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-02-21T10:00:00Z",
		DurationSeconds: 9000, // 2h30m
		DistanceMeters:  30000,
	}

	prompt := buildInsightsPrompt(w, "", false, nil, "", "")

	if !contains(prompt, "2:30:00") {
		t.Errorf("expected 2:30:00 for 9000s duration, prompt: %s", prompt)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParseInsightsResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "plain JSON",
			input: `{
				"effort_summary": "Solid aerobic run",
				"pacing_analysis": "Even pacing",
				"hr_zones": "Mostly zone 2-3",
				"observations": ["Good consistency"],
				"suggestions": ["Try negative splits"]
			}`,
		},
		{
			name: "markdown fenced",
			input: "```json\n" + `{
				"effort_summary": "Easy run",
				"pacing_analysis": "Steady",
				"hr_zones": "Zone 2",
				"observations": [],
				"suggestions": []
			}` + "\n```",
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights, err := parseInsightsResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if insights.EffortSummary == "" {
				t.Error("effort_summary should not be empty")
			}
		})
	}
}

func TestParseInsightsResponse_NilSlices(t *testing.T) {
	// Observations/Suggestions omitted from JSON should become [] not null.
	raw := `{"effort_summary":"Good","pacing_analysis":"Even","hr_zones":"Z2"}`
	insights, err := parseInsightsResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if insights.Observations == nil {
		t.Error("observations should be [] not nil")
	}
	if insights.Suggestions == nil {
		t.Error("suggestions should be [] not nil")
	}
	// Verify JSON serialization produces [] not null.
	data, _ := json.Marshal(insights)
	if !contains(string(data), `"observations":[]`) {
		t.Errorf("expected observations:[], got %s", string(data))
	}
	if !contains(string(data), `"suggestions":[]`) {
		t.Errorf("expected suggestions:[], got %s", string(data))
	}
}

func TestParseInsightsResponse_TrendAnalysisNoNotableChanges(t *testing.T) {
	// trend_analysis present but notable_changes omitted/null should serialize as [].
	raw := `{
		"effort_summary": "Good",
		"pacing_analysis": "Even",
		"hr_zones": "Z2",
		"observations": [],
		"suggestions": [],
		"trend_analysis": {
			"fitness_direction": "improving",
			"comparison_to_recent": "Better than last week"
		}
	}`
	insights, err := parseInsightsResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if insights.TrendAnalysis == nil {
		t.Fatal("trend_analysis should not be nil")
	}
	if insights.TrendAnalysis.NotableChanges == nil {
		t.Error("notable_changes should be [] not nil")
	}
	data, _ := json.Marshal(insights)
	if !contains(string(data), `"notable_changes":[]`) {
		t.Errorf("expected notable_changes:[], got %s", string(data))
	}
}

func TestCacheRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	// Create a test workout (user already created by setupTestDB).
	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Test Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Initially no cache.
	cached, err := GetCachedInsights(db, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected nil for uncached workout")
	}

	// Save insights.
	insights := &TrainingInsights{
		EffortSummary:  "Good run",
		PacingAnalysis: "Even pacing",
		HRZones:        "Zone 2-3",
		Observations:   []string{"Nice and steady"},
		Suggestions:    []string{"Push harder next time"},
	}
	if err := SaveInsights(db, 1, 1, insights, "claude-sonnet-4-6", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	// Retrieve cached.
	cached, err = GetCachedInsights(db, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached insights")
	}
	if cached.EffortSummary != "Good run" {
		t.Errorf("unexpected effort_summary: %s", cached.EffortSummary)
	}
	if cached.Model != "claude-sonnet-4-6" {
		t.Errorf("unexpected model: %s", cached.Model)
	}
	if !cached.Cached {
		t.Error("expected cached=true")
	}
	if len(cached.Observations) != 1 || cached.Observations[0] != "Nice and steady" {
		t.Errorf("unexpected observations: %v", cached.Observations)
	}

	// Verify JSON round-trip.
	data, _ := json.Marshal(cached)
	var roundTrip CachedInsights
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.EffortSummary != "Good run" {
		t.Errorf("round-trip failed: %s", roundTrip.EffortSummary)
	}
}

func TestCacheRoundTrip_EmptySlices(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Test', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Save insights with empty (not nil) slices.
	insights := &TrainingInsights{
		EffortSummary:  "Good",
		PacingAnalysis: "Even",
		HRZones:        "Z2",
		Observations:   []string{},
		Suggestions:    []string{},
	}
	if err := SaveInsights(db, 1, 1, insights, "test-model", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	cached, err := GetCachedInsights(db, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached insights")
	}

	// After round-trip, slices must be [] not nil.
	if cached.Observations == nil {
		t.Error("observations should be [] not nil after cache round-trip")
	}
	if cached.Suggestions == nil {
		t.Error("suggestions should be [] not nil after cache round-trip")
	}

	// Verify JSON output.
	data, _ := json.Marshal(cached)
	s := string(data)
	if !contains(s, `"observations":[]`) {
		t.Errorf("expected observations:[], got %s", s)
	}
}

func TestCacheUserScoping(t *testing.T) {
	db := setupTestDB(t)

	// Create a second user and workouts for each.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Run 1', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	insights := &TrainingInsights{
		EffortSummary:  "User 1 insights",
		PacingAnalysis: "Even",
		HRZones:        "Zone 2",
		Observations:   []string{"obs"},
		Suggestions:    []string{"sug"},
	}
	if err := SaveInsights(db, 1, 1, insights, "claude-sonnet-4-6", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	// User 1 can see their own cached insights.
	cached, err := GetCachedInsights(db, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("user 1 should see own cached insights")
	}

	// User 2 cannot see user 1's cached insights.
	cached, err = GetCachedInsights(db, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("user 2 should not see user 1's cached insights")
	}
}

func TestRunInsightsAnalysis_CacheHit(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate the cache.
	insights := &TrainingInsights{
		EffortSummary:  "Pre-cached",
		PacingAnalysis: "Even",
		HRZones:        "Zone 2",
		Observations:   []string{},
		Suggestions:    []string{},
	}
	if err := SaveInsights(db, 1, 1, insights, "test-model", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	// RunInsightsAnalysis should return ErrInsightsAlreadyCached without calling Claude.
	err = RunInsightsAnalysis(context.Background(), db, 1, 1)
	if !errors.Is(err, ErrInsightsAlreadyCached) {
		t.Errorf("expected ErrInsightsAlreadyCached, got %v", err)
	}
}

func TestRunInsightsAnalysis_ClaudeDisabled(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// No claude_enabled preference → Claude is disabled.
	err = RunInsightsAnalysis(context.Background(), db, 1, 1)
	if !errors.Is(err, ErrClaudeNotEnabled) {
		t.Errorf("expected ErrClaudeNotEnabled, got %v", err)
	}
}

func TestRunInsightsAnalysis_WorkoutNotFound(t *testing.T) {
	db := setupTestDB(t)

	// Enable Claude so we get past the config check.
	for _, kv := range [][2]string{
		{"claude_enabled", "true"},
		{"claude_cli_path", "claude"},
		{"claude_model", "claude-sonnet-4-6"},
	} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}

	err := RunInsightsAnalysis(context.Background(), db, 999, 1)
	if err == nil {
		t.Fatal("expected error for non-existent workout")
	}
}

func TestRunInsightsAnalysis_FreshGeneration(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Enable Claude.
	for _, kv := range [][2]string{
		{"claude_enabled", "true"},
		{"claude_cli_path", "claude"},
		{"claude_model", "claude-sonnet-4-6"},
	} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}

	// Override the prompt runner to return valid JSON.
	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		return `{"effort_summary":"Generated","pacing_analysis":"Good","hr_zones":"Zone 2","observations":[],"suggestions":[]}`, nil
	}

	if err := RunInsightsAnalysis(context.Background(), db, 1, 1); err != nil {
		t.Fatalf("expected nil on fresh generation, got %v", err)
	}

	// Verify insights were persisted.
	cached, err := GetCachedInsights(db, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached insights after fresh generation")
	}
	if cached.EffortSummary != "Generated" {
		t.Errorf("unexpected effort_summary: %s", cached.EffortSummary)
	}

	// Second call should return ErrInsightsAlreadyCached.
	err = RunInsightsAnalysis(context.Background(), db, 1, 1)
	if !errors.Is(err, ErrInsightsAlreadyCached) {
		t.Errorf("second call: expected ErrInsightsAlreadyCached, got %v", err)
	}
}

func TestInsightsHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/insights", nil)
	req = withUser(req, 1) // non-admin
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestInsightsHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/abc/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "abc")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestInsightsHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/999/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestInsightsHandler_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	// Create a second admin user and a workout owned by user 1.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id, is_admin) VALUES (2, 'admin2@example.com', 'Admin2', 'google-2', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'User1 Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Admin user 2 tries to access user 1's workout — should get 404.
	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/insights", nil)
	req = withAdminUser(req, 2)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong user's workout, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInsightsHandler_CacheHit(t *testing.T) {
	db := setupTestDB(t)

	// Create workout and cache insights.
	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Test Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	insights := &TrainingInsights{
		EffortSummary:  "Cached result",
		PacingAnalysis: "Steady",
		HRZones:        "Zone 2",
		Observations:   []string{"obs"},
		Suggestions:    []string{"sug"},
	}
	if err := SaveInsights(db, 1, 1, insights, "claude-sonnet-4-6", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := resp["insights"]; !ok {
		t.Fatal("expected insights key in response")
	}

	var cached CachedInsights
	if err := json.Unmarshal(resp["insights"], &cached); err != nil {
		t.Fatalf("unmarshal insights: %v", err)
	}
	if cached.EffortSummary != "Cached result" {
		t.Errorf("expected cached effort_summary, got: %s", cached.EffortSummary)
	}
	if !cached.Cached {
		t.Error("expected cached=true for cache hit")
	}
}

func TestInsightsHandler_CacheMiss(t *testing.T) {
	db := setupTestDB(t)

	// Create a workout owned by the admin user.
	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Fresh Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Override runPromptFunc with a mock that returns valid JSON.
	orig := runPromptFunc
	t.Cleanup(func() { runPromptFunc = orig })
	runPromptFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, error) {
		return `{"effort_summary":"Fresh result","pacing_analysis":"Good","hr_zones":"Zone 2","observations":["obs"],"suggestions":["sug"]}`, nil
	}

	// Enable Claude for the test user via user_preferences.
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_cli_path', 'claude')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_model', 'claude-sonnet-4-6')`)
	if err != nil {
		t.Fatal(err)
	}

	// First request: cache miss — should call runPromptFunc, save, return cached=false.
	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	InsightsHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("cache miss: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	var result CachedInsights
	if err := json.Unmarshal(resp["insights"], &result); err != nil {
		t.Fatalf("unmarshal insights: %v", err)
	}
	if result.Cached {
		t.Error("cache miss: expected cached=false on first request")
	}
	if result.EffortSummary != "Fresh result" {
		t.Errorf("cache miss: unexpected effort_summary: %s", result.EffortSummary)
	}

	// Second request: should now be a cache hit (cached=true).
	req2 := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/insights", nil)
	req2 = withAdminUser(req2, 1)
	req2 = withChiParam(req2, "id", "1")
	w2 := httptest.NewRecorder()

	InsightsHandler(db)(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("cache hit: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 map[string]json.RawMessage
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal response2: %v", err)
	}
	var result2 CachedInsights
	if err := json.Unmarshal(resp2["insights"], &result2); err != nil {
		t.Fatalf("unmarshal insights2: %v", err)
	}
	if !result2.Cached {
		t.Error("cache hit: expected cached=true on second request")
	}
	if result2.EffortSummary != "Fresh result" {
		t.Errorf("cache hit: unexpected effort_summary: %s", result2.EffortSummary)
	}
}

func TestGetCachedInsightsHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/1/insights", nil)
	req = withUser(req, 1) // non-admin
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	GetCachedInsightsHandler(db)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestGetCachedInsightsHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/1/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	GetCachedInsightsHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no cache, got %d", w.Code)
	}
}

func TestGetCachedInsightsHandler_CacheHit(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Test Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	insights := &TrainingInsights{
		EffortSummary:  "Cached GET result",
		PacingAnalysis: "Steady",
		HRZones:        "Zone 2",
		RiskFlags:      []string{"high load"},
		Observations:   []string{"obs"},
		Suggestions:    []string{"sug"},
	}
	if err := SaveInsights(db, 1, 1, insights, "claude-sonnet-4-6", "2026-02-21T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/1/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	GetCachedInsightsHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := resp["insights"]; !ok {
		t.Fatal("expected insights key in response")
	}

	var cached CachedInsights
	if err := json.Unmarshal(resp["insights"], &cached); err != nil {
		t.Fatalf("unmarshal insights: %v", err)
	}
	if cached.EffortSummary != "Cached GET result" {
		t.Errorf("unexpected effort_summary: %s", cached.EffortSummary)
	}
	if !cached.Cached {
		t.Error("expected cached=true")
	}
	if len(cached.RiskFlags) != 1 || cached.RiskFlags[0] != "high load" {
		t.Errorf("unexpected risk_flags: %v", cached.RiskFlags)
	}
}

func TestGetCachedInsightsHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/abc/insights", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "abc")
	w := httptest.NewRecorder()

	GetCachedInsightsHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIsTreadmill(t *testing.T) {
	tests := []struct {
		name    string
		workout *Workout
		want    bool
	}{
		{
			name:    "sub_sport treadmill lowercase",
			workout: &Workout{Sport: "running", SubSport: "treadmill"},
			want:    true,
		},
		{
			name:    "sub_sport treadmill mixed case",
			workout: &Workout{Sport: "running", SubSport: "Treadmill"},
			want:    true,
		},
		{
			name:    "sport treadmill",
			workout: &Workout{Sport: "treadmill"},
			want:    true,
		},
		{
			name:    "tag treadmill",
			workout: &Workout{Sport: "running", Tags: []string{"easy", "treadmill"}},
			want:    true,
		},
		{
			name:    "tag treadmill mixed case",
			workout: &Workout{Sport: "running", Tags: []string{"TREADMILL"}},
			want:    true,
		},
		{
			name:    "auto-prefixed treadmill tag",
			workout: &Workout{Sport: "running", Tags: []string{"auto:treadmill"}},
			want:    true,
		},
		{
			name:    "ai-prefixed treadmill tag",
			workout: &Workout{Sport: "running", Tags: []string{"ai:treadmill"}},
			want:    true,
		},
		{
			name:    "outdoor running no treadmill",
			workout: &Workout{Sport: "running", SubSport: "trail"},
			want:    false,
		},
		{
			name:    "indoor cycling not treadmill",
			workout: &Workout{Sport: "cycling", SubSport: "indoor_cycling"},
			want:    false,
		},
		{
			name:    "empty workout",
			workout: &Workout{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTreadmill(tt.workout)
			if got != tt.want {
				t.Errorf("isTreadmill() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildInsightsPrompt_TreadmillCaveat(t *testing.T) {
	const caveat = "This is a treadmill workout. GPS-based pace data is unreliable"

	treadmillWorkout := &Workout{
		Sport:           "running",
		SubSport:        "treadmill",
		StartedAt:       "2026-03-29T08:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    155,
	}
	outdoorWorkout := &Workout{
		Sport:           "running",
		StartedAt:       "2026-03-29T08:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    155,
	}

	treadmillPrompt := buildInsightsPrompt(treadmillWorkout, "", false, nil, "", "")
	if !contains(treadmillPrompt, caveat) {
		t.Error("treadmill workout prompt should contain treadmill caveat")
	}

	outdoorPrompt := buildInsightsPrompt(outdoorWorkout, "", false, nil, "", "")
	if contains(outdoorPrompt, caveat) {
		t.Error("outdoor workout prompt should not contain treadmill caveat")
	}
}

func TestBuildInsightsPrompt_TreadmillCaveatAfterBasePrompt(t *testing.T) {
	// Caveat must appear after the user-editable base prompt block.
	const profile = "User Profile:\n- Max HR: 195 bpm\n"
	const caveat = "This is a treadmill workout. GPS-based pace data is unreliable"

	w := &Workout{
		Sport:           "running",
		SubSport:        "treadmill",
		StartedAt:       "2026-03-29T08:00:00Z",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
	}

	prompt := buildInsightsPrompt(w, profile, false, nil, "", "")

	profileIdx := strings.Index(prompt, "Max HR: 195")
	caveatIdx := strings.Index(prompt, caveat)

	if profileIdx < 0 {
		t.Fatal("prompt should contain user profile block")
	}
	if caveatIdx < 0 {
		t.Fatal("prompt should contain treadmill caveat")
	}
	if caveatIdx <= profileIdx {
		t.Error("treadmill caveat should appear after the user profile block")
	}
}

func TestBuildInsightsPrompt_TreadmillByTag(t *testing.T) {
	const caveat = "This is a treadmill workout. GPS-based pace data is unreliable"

	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-03-29T08:00:00Z",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		Tags:            []string{"easy", "treadmill"},
	}

	prompt := buildInsightsPrompt(w, "", false, nil, "", "")
	if !contains(prompt, caveat) {
		t.Error("workout with treadmill tag should include treadmill caveat in prompt")
	}
}

func TestBuildComparisonAnalysisPrompt_BothTreadmill(t *testing.T) {
	const caveat = "Both workouts are treadmill workouts. GPS-based pace data is unreliable"
	const mixedNote = "one of these workouts is a treadmill workout"

	wA := &Workout{Sport: "running", SubSport: "treadmill", StartedAt: "2026-03-10T08:00:00Z", DurationSeconds: 1800, DistanceMeters: 5000}
	wB := &Workout{Sport: "running", SubSport: "treadmill", StartedAt: "2026-03-17T18:00:00Z", DurationSeconds: 1750, DistanceMeters: 5100}

	prompt := buildComparisonAnalysisPrompt(wA, wB, nil, "", "", "")

	if !contains(prompt, caveat) {
		t.Error("both-treadmill comparison prompt should contain treadmill caveat")
	}
	if contains(prompt, mixedNote) {
		t.Error("both-treadmill comparison prompt should not contain the mixed caveat")
	}
}

func TestBuildComparisonAnalysisPrompt_OneTreadmillOneOutdoor(t *testing.T) {
	const mixedNote = "one of these workouts is a treadmill workout and the other is outdoors — pace comparison between them is not meaningful"
	const bothCaveat = "Both workouts are treadmill workouts"

	wTreadmill := &Workout{Sport: "running", SubSport: "treadmill", StartedAt: "2026-03-10T08:00:00Z", DurationSeconds: 1800, DistanceMeters: 5000}
	wOutdoor := &Workout{Sport: "running", StartedAt: "2026-03-17T18:00:00Z", DurationSeconds: 1750, DistanceMeters: 5100}

	prompt := buildComparisonAnalysisPrompt(wTreadmill, wOutdoor, nil, "", "", "")

	if !contains(prompt, mixedNote) {
		t.Error("mixed treadmill/outdoor comparison prompt should contain mixed caveat")
	}
	if contains(prompt, bothCaveat) {
		t.Error("mixed comparison prompt should not contain the both-treadmill caveat")
	}

	// Order reversed: outdoor A, treadmill B.
	promptReversed := buildComparisonAnalysisPrompt(wOutdoor, wTreadmill, nil, "", "", "")
	if !contains(promptReversed, mixedNote) {
		t.Error("reversed mixed comparison prompt should also contain mixed caveat")
	}
}

func TestBuildComparisonAnalysisPrompt_NeitherTreadmill(t *testing.T) {
	const caveat = "treadmill"

	wA := &Workout{Sport: "running", StartedAt: "2026-03-10T08:00:00Z", DurationSeconds: 1800, DistanceMeters: 5000}
	wB := &Workout{Sport: "running", StartedAt: "2026-03-17T18:00:00Z", DurationSeconds: 1750, DistanceMeters: 5100}

	prompt := buildComparisonAnalysisPrompt(wA, wB, nil, "", "", "")

	if contains(prompt, caveat) {
		t.Error("outdoor-only comparison prompt should not contain any treadmill caveat")
	}
}

