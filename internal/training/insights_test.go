package training

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	prompt := buildInsightsPrompt(w)

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

func TestBuildInsightsPrompt_LongDuration(t *testing.T) {
	// Workouts > 1 hour should format as h:mm:ss, not 150:00.
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-02-21T10:00:00Z",
		DurationSeconds: 9000, // 2h30m
		DistanceMeters:  30000,
	}

	prompt := buildInsightsPrompt(w)

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
