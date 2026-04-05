package stride

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// extendedTestDB creates a test DB with the extra tables needed by GeneratePlan
// (user_preferences, workouts) in addition to the core stride tables.
func extendedTestDB(t *testing.T) interface{ Close() error } {
	t.Helper()
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE IF NOT EXISTS workouts (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at       TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			distance_meters  REAL NOT NULL DEFAULT 0,
			avg_heart_rate   INTEGER NOT NULL DEFAULT 0,
			sport            TEXT NOT NULL DEFAULT 'running'
		);
		CREATE TABLE IF NOT EXISTS training_load (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id INTEGER REFERENCES workouts(id) ON DELETE CASCADE,
			date       TEXT NOT NULL,
			load       REAL NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("extend schema: %v", err)
	}
	return db
}

func TestGeneratePlan_DisabledReturnsNil(t *testing.T) {
	db := setupTestDB(t)
	// Extend with user_preferences table.
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		t.Fatalf("create user_preferences: %v", err)
	}

	// stride_enabled is not set — GeneratePlan should be a no-op.
	err = GeneratePlan(context.Background(), db, 1)
	if err != nil {
		t.Errorf("expected nil when stride disabled, got: %v", err)
	}

	// Confirm no plan was inserted.
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 plans, got %d", count)
	}
}

func TestGeneratePlan_ClaudeNotEnabled(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		t.Fatalf("create user_preferences: %v", err)
	}
	// Enable stride but leave claude_enabled unset (defaults to false).
	_, err = db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')")
	if err != nil {
		t.Fatalf("set preference: %v", err)
	}

	err = GeneratePlan(context.Background(), db, 1)
	if err == nil {
		t.Error("expected error when Claude is not enabled, got nil")
	}
}

func TestGeneratePlan_StoresPlan(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE IF NOT EXISTS workouts (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at       TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			distance_meters  REAL NOT NULL DEFAULT 0,
			avg_heart_rate   INTEGER NOT NULL DEFAULT 0,
			sport            TEXT NOT NULL DEFAULT 'running'
		);
		CREATE TABLE IF NOT EXISTS training_load (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id INTEGER REFERENCES workouts(id) ON DELETE CASCADE,
			date       TEXT NOT NULL,
			load       REAL NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("extend schema: %v", err)
	}

	// Enable stride and claude.
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

	// Build a minimal valid 7-day plan JSON that the mock Claude will return.
	weekStart, weekEnd := upcomingWeek()
	planDays := buildMinimalPlan(weekStart)
	mockResponse, _ := json.Marshal(planDays)

	// Override the runPromptFunc to return our canned response.
	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return string(mockResponse), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	err = GeneratePlan(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// Verify the plan row was inserted.
	var storedPlanJSON, storedModel, storedWeekStart, storedWeekEnd string
	err = db.QueryRow(
		"SELECT plan_json, model, week_start, week_end FROM stride_plans WHERE user_id = 1",
	).Scan(&storedPlanJSON, &storedModel, &storedWeekStart, &storedWeekEnd)
	if err != nil {
		t.Fatalf("query plan: %v", err)
	}

	if storedModel != "claude-opus-4-5" {
		t.Errorf("model = %q, want %q", storedModel, "claude-opus-4-5")
	}
	if storedWeekStart != weekStart {
		t.Errorf("week_start = %q, want %q", storedWeekStart, weekStart)
	}
	if storedWeekEnd != weekEnd {
		t.Errorf("week_end = %q, want %q", storedWeekEnd, weekEnd)
	}

	// Verify plan_json is valid JSON with the right number of days.
	var stored []DayPlan
	if err := json.Unmarshal([]byte(storedPlanJSON), &stored); err != nil {
		t.Fatalf("unmarshal stored plan: %v", err)
	}
	if len(stored) != 7 {
		t.Errorf("plan days = %d, want 7", len(stored))
	}

	// Verify the prompt and response columns are encrypted (prefixed with "enc:").
	var storedPrompt, storedResponse string
	err = db.QueryRow(
		"SELECT prompt, response FROM stride_plans WHERE user_id = 1",
	).Scan(&storedPrompt, &storedResponse)
	if err != nil {
		t.Fatalf("query encrypted fields: %v", err)
	}
	if storedPrompt != "" && storedPrompt[:4] != "enc:" {
		t.Errorf("prompt not encrypted: %q", storedPrompt[:min(20, len(storedPrompt))])
	}
	if storedResponse != "" && storedResponse[:4] != "enc:" {
		t.Errorf("response not encrypted: %q", storedResponse[:min(20, len(storedResponse))])
	}
}

func TestGeneratePlan_DBError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE IF NOT EXISTS workouts (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at       TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			distance_meters  REAL NOT NULL DEFAULT 0,
			avg_heart_rate   INTEGER NOT NULL DEFAULT 0,
			sport            TEXT NOT NULL DEFAULT 'running'
		);
		CREATE TABLE IF NOT EXISTS training_load (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id INTEGER REFERENCES workouts(id) ON DELETE CASCADE,
			date       TEXT NOT NULL,
			load       REAL NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("extend schema: %v", err)
	}

	prefs := []struct{ k, v string }{
		{"stride_enabled", "true"},
		{"claude_enabled", "true"},
		{"claude_model", "claude-opus-4-5"},
	}
	for _, p := range prefs {
		_, _ = db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)", p.k, p.v)
	}

	// Drop stride_plans to trigger a DB error on insert.
	if _, err := db.Exec("DROP TABLE stride_plans"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	weekStart, _ := upcomingWeek()
	planDays := buildMinimalPlan(weekStart)
	mockResponse, _ := json.Marshal(planDays)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return string(mockResponse), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	err = GeneratePlan(context.Background(), db, 1)
	if err == nil {
		t.Error("expected error when stride_plans table is missing, got nil")
	}
}

func TestGeneratePlan_APIError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE IF NOT EXISTS workouts (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at       TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			distance_meters  REAL NOT NULL DEFAULT 0,
			avg_heart_rate   INTEGER NOT NULL DEFAULT 0,
			sport            TEXT NOT NULL DEFAULT 'running'
		);
		CREATE TABLE IF NOT EXISTS training_load (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id INTEGER REFERENCES workouts(id) ON DELETE CASCADE,
			date       TEXT NOT NULL,
			load       REAL NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("extend schema: %v", err)
	}
	prefs := []struct{ k, v string }{
		{"stride_enabled", "true"},
		{"claude_enabled", "true"},
	}
	for _, p := range prefs {
		_, _ = db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)", p.k, p.v)
	}

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "", fmt.Errorf("API timeout")
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	err = GeneratePlan(context.Background(), db, 1)
	if err == nil {
		t.Error("expected error on API failure, got nil")
	}

	// No plan should be stored.
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 plans after API failure, got %d", count)
	}
}

func TestParsePlanResponse_Valid(t *testing.T) {
	weekStart, _ := upcomingWeek()
	days := buildMinimalPlan(weekStart)
	raw, _ := json.Marshal(days)

	parsed, err := parsePlanResponse(string(raw))
	if err != nil {
		t.Fatalf("parsePlanResponse: %v", err)
	}
	if len(parsed) != 7 {
		t.Errorf("len = %d, want 7", len(parsed))
	}
}

func TestParsePlanResponse_StripsCodeFences(t *testing.T) {
	weekStart, _ := upcomingWeek()
	days := buildMinimalPlan(weekStart)
	raw, _ := json.Marshal(days)

	fenced := "```json\n" + string(raw) + "\n```"
	parsed, err := parsePlanResponse(fenced)
	if err != nil {
		t.Fatalf("parsePlanResponse with fences: %v", err)
	}
	if len(parsed) != 7 {
		t.Errorf("len = %d, want 7", len(parsed))
	}
}

func TestParsePlanResponse_InvalidJSON(t *testing.T) {
	_, err := parsePlanResponse("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlanResponse_MissingDate(t *testing.T) {
	raw := `[{"rest_day":true}]`
	_, err := parsePlanResponse(raw)
	if err == nil {
		t.Error("expected error for missing date")
	}
}

func TestParsePlanResponse_NonRestDayWithoutSession(t *testing.T) {
	raw := `[{"date":"2026-04-06","rest_day":false}]`
	_, err := parsePlanResponse(raw)
	if err == nil {
		t.Error("expected error for non-rest day without session")
	}
}

func TestUpcomingWeek_IsMonday(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()

	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		t.Fatalf("parse week_start: %v", err)
	}
	if start.Weekday() != time.Monday {
		t.Errorf("week_start %s is %s, want Monday", weekStart, start.Weekday())
	}

	end, err := time.Parse("2006-01-02", weekEnd)
	if err != nil {
		t.Fatalf("parse week_end: %v", err)
	}
	diff := end.Sub(start)
	if diff != 6*24*time.Hour {
		t.Errorf("week span = %v, want 6 days", diff)
	}
}

// buildMinimalPlan creates a 7-day plan starting at weekStart for use in tests.
func buildMinimalPlan(weekStart string) []DayPlan {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		start = time.Now().UTC()
	}

	days := make([]DayPlan, 7)
	for i := 0; i < 7; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		if i == 2 || i == 4 || i == 6 { // Wed, Fri, Sun = rest
			days[i] = DayPlan{Date: date, RestDay: true}
		} else {
			days[i] = DayPlan{
				Date:    date,
				RestDay: false,
				Session: &Session{
					Warmup:      "15 min easy jog",
					MainSet:     "6x1000m at threshold pace, 60s recovery jog",
					Cooldown:    "10 min easy jog",
					Strides:     "",
					TargetHRCap: 165,
					Description: "Threshold intervals session.",
				},
			}
		}
	}
	return days
}

