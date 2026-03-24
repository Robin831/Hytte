package training

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestBuildClassificationPrompt(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 3065,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 598, DistanceMeters: 1450, AvgHeartRate: 132, AvgPaceSecPerKm: 412},
			{LapNumber: 2, DurationSeconds: 360, DistanceMeters: 1237, AvgHeartRate: 151, AvgPaceSecPerKm: 290},
			{LapNumber: 3, DurationSeconds: 60, DistanceMeters: 150, AvgHeartRate: 140, AvgPaceSecPerKm: 400},
		},
	}

	prompt := BuildClassificationPrompt(w, "")

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Should contain sport.
	if !strings.Contains(prompt, "running") {
		t.Error("prompt should contain sport")
	}
	// Should contain lap table.
	if !strings.Contains(prompt, "| 1 |") {
		t.Error("prompt should contain lap data")
	}
	// Should contain response format instruction.
	if !strings.Contains(prompt, "JSON object") {
		t.Error("prompt should mention JSON response format")
	}
}

func TestBuildClassificationPrompt_WithProfile(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 3065,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
	}

	profile := "User Profile:\n- Max HR: 195 bpm\n- Threshold HR: 172 bpm (from lactate test)\n"
	prompt := BuildClassificationPrompt(w, profile)

	if !strings.Contains(prompt, "Max HR: 195") {
		t.Error("prompt should contain user profile block")
	}
	if !strings.Contains(prompt, "running") {
		t.Error("prompt should still contain sport after profile block")
	}
}

func TestBuildClassificationPrompt_IncludesConfidenceSchema(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 3065,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
	}

	prompt := BuildClassificationPrompt(w, "")

	if !strings.Contains(prompt, "confidence_score") {
		t.Error("classification prompt should include confidence_score in schema")
	}
	if !strings.Contains(prompt, "confidence_note") {
		t.Error("classification prompt should include confidence_note in schema")
	}
}

func TestBuildClassificationPrompt_SingleLap(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 145, AvgPaceSecPerKm: 360},
		},
	}

	prompt := BuildClassificationPrompt(w, "")
	// Single lap should NOT include the lap table.
	if strings.Contains(prompt, "| # |") {
		t.Error("single lap workout should not include lap table")
	}
}

func TestParseClaudeResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTag   string
		wantSum   string
		wantType  string
		wantTitle string
	}{
		{
			name:      "valid JSON",
			input:     `{"type": "intervals", "tag": "6x6min (r1m)", "summary": "6 intervals of 6 minutes", "title": "Threshold Intervals"}`,
			wantTag:   "6x6min (r1m)",
			wantSum:   "6 intervals of 6 minutes",
			wantType:  "intervals",
			wantTitle: "Threshold Intervals",
		},
		{
			name:      "markdown code fence",
			input:     "```json\n{\"type\": \"easy_run\", \"tag\": \"10k easy\", \"summary\": \"Easy 10k run\", \"title\": \"Easy Run\"}\n```",
			wantTag:   "10k easy",
			wantSum:   "Easy 10k run",
			wantType:  "easy_run",
			wantTitle: "Easy Run",
		},
		{
			name:      "invalid JSON fallback",
			input:     "This is just text",
			wantTag:   "",
			wantSum:   "This is just text",
			wantType:  "",
			wantTitle: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, summary, typ, title := parseClaudeResponse(tt.input)
			if tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", tag, tt.wantTag)
			}
			if summary != tt.wantSum {
				t.Errorf("summary = %q, want %q", summary, tt.wantSum)
			}
			if typ != tt.wantType {
				t.Errorf("type = %q, want %q", typ, tt.wantType)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}

func TestAnalysisCRUD(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout to attach analysis to.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Initially no analysis should exist.
	_, err = GetAnalysis(database, 1, 1, "tag")
	if err == nil {
		t.Fatal("expected error for missing analysis")
	}

	// Upsert an analysis.
	a := &WorkoutAnalysis{
		UserID:       1,
		WorkoutID:    1,
		AnalysisType: "tag",
		Model:        "claude-sonnet-4-6",
		Prompt:       "test prompt",
		ResponseJSON: `{"type":"intervals","tag":"6x6min","summary":"test","title":"Threshold Intervals"}`,
		Tags:         "6x6min,intervals",
		Summary:      "test summary",
		Title:        "Threshold Intervals",
	}
	if err := UpsertAnalysis(database, a); err != nil {
		t.Fatalf("upsert analysis: %v", err)
	}

	// Read back.
	got, err := GetAnalysis(database, 1, 1, "tag")
	if err != nil {
		t.Fatalf("get analysis: %v", err)
	}
	if got.Tags != "6x6min,intervals" {
		t.Errorf("tags = %q, want %q", got.Tags, "6x6min,intervals")
	}
	if got.Summary != "test summary" {
		t.Errorf("summary = %q, want %q", got.Summary, "test summary")
	}
	if got.Title != "Threshold Intervals" {
		t.Errorf("title = %q, want %q", got.Title, "Threshold Intervals")
	}

	// Upsert again (should replace).
	a.Summary = "updated summary"
	a.Title = "Updated Title"
	if err := UpsertAnalysis(database, a); err != nil {
		t.Fatalf("upsert analysis again: %v", err)
	}
	got, err = GetAnalysis(database, 1, 1, "tag")
	if err != nil {
		t.Fatalf("get analysis after update: %v", err)
	}
	if got.Summary != "updated summary" {
		t.Errorf("summary after upsert = %q, want %q", got.Summary, "updated summary")
	}
	if got.Title != "Updated Title" {
		t.Errorf("title after upsert = %q, want %q", got.Title, "Updated Title")
	}

	// Delete.
	if err := DeleteAnalysis(database, 1, 1, "tag"); err != nil {
		t.Fatalf("delete analysis: %v", err)
	}
	_, err = GetAnalysis(database, 1, 1, "tag")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestAddAITags(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Add a manual tag first.
	_, err = database.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (1, 'manual-tag')`)
	if err != nil {
		t.Fatalf("add manual tag: %v", err)
	}

	// Add AI tags.
	if err := AddAITags(database, 1, 1, []string{"6x6min", "intervals"}); err != nil {
		t.Fatalf("add AI tags: %v", err)
	}

	tags, err := getTags(database, 1)
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}

	// Should have manual tag + 2 AI tags.
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}

	// Verify AI tags have prefix.
	hasAI := 0
	for _, tag := range tags {
		if strings.HasPrefix(tag, "ai:") {
			hasAI++
		}
	}
	if hasAI != 2 {
		t.Errorf("expected 2 ai: tags, got %d", hasAI)
	}
}

func TestGetAnalysisHandler_NotAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/1/analysis", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	GetAnalysisHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestGetAnalysisHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withAdminUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/999/analysis", nil), 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()

	GetAnalysisHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetAnalysisHandler_Success(t *testing.T) {
	database := setupTestDB(t)

	// Create workout and analysis.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	a := &WorkoutAnalysis{
		UserID:       1,
		WorkoutID:    1,
		AnalysisType: "tag",
		Model:        "claude-sonnet-4-6",
		Prompt:       "test",
		ResponseJSON: `{"type":"intervals"}`,
		Tags:         "intervals",
		Summary:      "test summary",
	}
	if err := UpsertAnalysis(database, a); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := withAdminUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/1/analysis", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	GetAnalysisHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatal("expected analysis object")
	}
	if analysis["summary"] != "test summary" {
		t.Errorf("summary = %v, want 'test summary'", analysis["summary"])
	}
}

func TestDeleteAnalysisHandler_NotAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/training/workouts/1/analysis", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	DeleteAnalysisHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAnalyzeHandler_NotAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/analyze", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AnalyzeHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAnalyzeHandler_CachedResult(t *testing.T) {
	database := setupTestDB(t)

	// Create workout and pre-existing analysis.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	a := &WorkoutAnalysis{
		UserID:       1,
		WorkoutID:    1,
		AnalysisType: "tag",
		Model:        "claude-sonnet-4-6",
		Prompt:       "test",
		ResponseJSON: `{"type":"intervals","tag":"6x6min","summary":"cached"}`,
		Tags:         "6x6min,intervals",
		Summary:      "cached summary",
	}
	if err := UpsertAnalysis(database, a); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/analyze", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AnalyzeHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["cached"] != true {
		t.Errorf("expected cached=true, got %v", resp["cached"])
	}
	analysis := resp["analysis"].(map[string]any)
	if analysis["summary"] != "cached summary" {
		t.Errorf("summary = %v, want 'cached summary'", analysis["summary"])
	}
}

func TestAnalyzeHandler_RunsClaudeOnCacheMiss(t *testing.T) {
	database := setupTestDB(t)

	// Create workout with laps.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds, distance_meters)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 3600, 10000)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Set up Claude config via preferences.
	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_model", "claude-sonnet-4-6"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	// Mock RunPrompt.
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		return `{"type": "easy_run", "tag": "10k easy", "summary": "Easy 10k run", "title": "Easy Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/analyze", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AnalyzeHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["cached"] != false {
		t.Errorf("expected cached=false, got %v", resp["cached"])
	}
	analysis := resp["analysis"].(map[string]any)
	if analysis["summary"] != "Easy 10k run" {
		t.Errorf("summary = %v, want 'Easy 10k run'", analysis["summary"])
	}

	// Verify analysis was persisted with title.
	got, err := GetAnalysis(database, 1, 1, "tag")
	if err != nil {
		t.Fatalf("get analysis: %v", err)
	}
	if got.Summary != "Easy 10k run" {
		t.Errorf("persisted summary = %q, want 'Easy 10k run'", got.Summary)
	}
	if got.Title != "Easy Run" {
		t.Errorf("persisted title = %q, want 'Easy Run'", got.Title)
	}

	// Verify workout title was NOT overwritten — the existing title 'Test Run' has
	// no title_source tracking (legacy empty string), and since it's non-empty we
	// leave it alone to avoid silently replacing user-edited legacy titles.
	var workoutTitle, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&workoutTitle, &titleSource)
	if err != nil {
		t.Fatalf("query workout title: %v", err)
	}
	if workoutTitle != "Test Run" {
		t.Errorf("workout title = %q, want 'Test Run' (legacy non-empty titles are preserved)", workoutTitle)
	}
	if titleSource != "" {
		t.Errorf("title_source = %q, want '' (unchanged)", titleSource)
	}
}

func TestSetAITitle(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with no title yet (empty title, empty title_source).
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', '', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// SetAITitle should set the title when title_source is empty and title is empty.
	if err := SetAITitle(database, 1, 1, "Easy Run"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&title, &titleSource)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Easy Run" {
		t.Errorf("title = %q, want %q", title, "Easy Run")
	}
	if titleSource != "ai" {
		t.Errorf("title_source = %q, want %q", titleSource, "ai")
	}
}

func TestSetAITitle_UpdatesExistingAITitle(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with a previous AI-set title.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, title_source, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Old AI Title', 'ai', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// SetAITitle should overwrite a previously AI-set title.
	if err := SetAITitle(database, 1, 1, "Easy Run"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&title, &titleSource)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Easy Run" {
		t.Errorf("title = %q, want %q", title, "Easy Run")
	}
	if titleSource != "ai" {
		t.Errorf("title_source = %q, want %q", titleSource, "ai")
	}
}

func TestSetAITitle_PreservesLegacyNonEmptyTitle(t *testing.T) {
	database := setupTestDB(t)

	// Create a legacy workout with a non-empty title but no title_source tracking.
	// We treat these as potentially user-edited (pre-title_source schema), so AI
	// must NOT overwrite them.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Garmin Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := SetAITitle(database, 1, 1, "Easy Run"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&title, &titleSource)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Garmin Run" {
		t.Errorf("title = %q, want %q (legacy non-empty titles must be preserved)", title, "Garmin Run")
	}
	if titleSource != "" {
		t.Errorf("title_source = %q, want '' (unchanged)", titleSource)
	}
}

func TestSetAITitle_OverwritesDeviceTitle(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with title_source = 'device' (from .fit import).
	// AI should be able to overwrite device-generated titles.
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, title_source, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Running 2024-01-01 10:00', 'device', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := SetAITitle(database, 1, 1, "Easy Morning Run"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&title, &titleSource)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Easy Morning Run" {
		t.Errorf("title = %q, want %q", title, "Easy Morning Run")
	}
	if titleSource != "ai" {
		t.Errorf("title_source = %q, want %q", titleSource, "ai")
	}
}

func TestSetAITitle_PreservesUserTitle(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with title_source = 'user' (manually edited).
	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, title_source, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'My Custom Title', 'user', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// SetAITitle should NOT overwrite a user-set title.
	if err := SetAITitle(database, 1, 1, "AI Title"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title, titleSource string
	err = database.QueryRow(`SELECT title, title_source FROM workouts WHERE id = 1`).Scan(&title, &titleSource)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "My Custom Title" {
		t.Errorf("title = %q, want %q", title, "My Custom Title")
	}
	if titleSource != "user" {
		t.Errorf("title_source = %q, want %q", titleSource, "user")
	}
}

func TestSetAITitle_WrongUser(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Original', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// User 2 should not be able to update user 1's workout title.
	if err := SetAITitle(database, 1, 2, "Hacked Title"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title string
	err = database.QueryRow(`SELECT title FROM workouts WHERE id = 1`).Scan(&title)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Original" {
		t.Errorf("title = %q, want %q — wrong user was able to update title", title, "Original")
	}
}

func TestSetAITitle_EmptyTitle(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Original', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Empty title should be a no-op.
	if err := SetAITitle(database, 1, 1, ""); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title string
	err = database.QueryRow(`SELECT title FROM workouts WHERE id = 1`).Scan(&title)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Original" {
		t.Errorf("title = %q, want %q — empty title should not overwrite", title, "Original")
	}
}

func TestSetAITitle_OverwritesPreviousAITitle(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, title_source, started_at, created_at, fit_file_hash)
		VALUES (1, 1, 'running', 'Old AI Title', 'ai', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1')`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// A new AI title should overwrite a previous AI title.
	if err := SetAITitle(database, 1, 1, "New AI Title"); err != nil {
		t.Fatalf("SetAITitle: %v", err)
	}

	var title string
	err = database.QueryRow(`SELECT title FROM workouts WHERE id = 1`).Scan(&title)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "New AI Title" {
		t.Errorf("title = %q, want %q", title, "New AI Title")
	}
}

func TestRunClaudeAnalysis_WhenEnabled(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 1800)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	called := make(chan struct{}, 1)
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		called <- struct{}{}
		return `{"type":"easy_run","tag":"easy","summary":"Easy run","title":"Easy Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	if err := RunClaudeAnalysis(context.Background(), database, 1, 1); err != nil {
		t.Fatalf("RunClaudeAnalysis: %v", err)
	}

	select {
	case <-called:
		// success: prompt was called
	default:
		t.Fatal("expected runPromptFunc to be called when Claude is enabled")
	}
}

func TestRunClaudeAnalysis_WhenDisabled(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 1800)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Claude not enabled (no preference set).
	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	err = RunClaudeAnalysis(context.Background(), database, 1, 1)
	if !errors.Is(err, ErrClaudeNotEnabled) {
		t.Fatalf("expected ErrClaudeNotEnabled, got %v", err)
	}
	if called {
		t.Fatal("expected runPromptFunc NOT to be called when Claude is disabled")
	}
}

func TestAnalyzeHandler_ClaudeError(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds, distance_meters)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 3600, 10000)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	// Mock RunPrompt to return error.
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		return "", fmt.Errorf("CLI not found")
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	req := withAdminUser(httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/analyze", nil), 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AnalyzeHandler(database)(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

