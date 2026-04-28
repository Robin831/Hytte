package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// extendedTestDB creates a test DB with the extra tables needed by GeneratePlan
// (user_preferences, workouts, training_load) in addition to the core stride tables.
func extendedTestDB(t *testing.T) *sql.DB {
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
			sport            TEXT NOT NULL DEFAULT 'running',
			training_load    REAL,
			race_id          INTEGER
		);
	`)
	if err != nil {
		t.Fatalf("extend schema: %v", err)
	}
	return db
}

func TestGeneratePlan_DisabledReturnsNil(t *testing.T) {
	db := extendedTestDB(t)

	// stride_enabled is not set — GeneratePlan should be a no-op.
	err := GeneratePlan(context.Background(), db, 1, "next")
	if err != nil {
		t.Errorf("expected nil when stride disabled, got: %v", err)
	}

	// Confirm no plan was inserted.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1").Scan(&count); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 plans, got %d", count)
	}
}

func TestGeneratePlan_ClaudeNotEnabled(t *testing.T) {
	db := extendedTestDB(t)

	// Enable stride but leave claude_enabled unset (defaults to false).
	if _, err := db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_enabled', 'true')"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	err := GeneratePlan(context.Background(), db, 1, "next")
	if !errors.Is(err, training.ErrClaudeNotEnabled) {
		t.Errorf("expected ErrClaudeNotEnabled, got %v", err)
	}
}

func TestGeneratePlan_StoresPlan(t *testing.T) {
	db := extendedTestDB(t)

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

	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// Verify the plan row was inserted.
	var storedPlanJSON, storedModel, storedWeekStart, storedWeekEnd string
	if err := db.QueryRow(
		"SELECT plan_json, model, week_start, week_end FROM stride_plans WHERE user_id = 1",
	).Scan(&storedPlanJSON, &storedModel, &storedWeekStart, &storedWeekEnd); err != nil {
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
	if err := db.QueryRow(
		"SELECT prompt, response FROM stride_plans WHERE user_id = 1",
	).Scan(&storedPrompt, &storedResponse); err != nil {
		t.Fatalf("query encrypted fields: %v", err)
	}
	if storedPrompt != "" && storedPrompt[:4] != "enc:" {
		t.Errorf("prompt not encrypted: %q", storedPrompt[:min(20, len(storedPrompt))])
	}
	if storedResponse != "" && storedResponse[:4] != "enc:" {
		t.Errorf("response not encrypted: %q", storedResponse[:min(20, len(storedResponse))])
	}
}

func TestGeneratePlan_StoresPlan_Current(t *testing.T) {
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

	weekStart, weekEnd := currentWeek()
	planDays := buildMinimalPlan(weekStart)
	mockResponse, _ := json.Marshal(planDays)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return string(mockResponse), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	if err := GeneratePlan(context.Background(), db, 1, "current"); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	var storedWeekStart, storedWeekEnd string
	if err := db.QueryRow(
		"SELECT week_start, week_end FROM stride_plans WHERE user_id = 1",
	).Scan(&storedWeekStart, &storedWeekEnd); err != nil {
		t.Fatalf("query plan: %v", err)
	}
	if storedWeekStart != weekStart {
		t.Errorf("week_start = %q, want %q (currentWeek)", storedWeekStart, weekStart)
	}
	if storedWeekEnd != weekEnd {
		t.Errorf("week_end = %q, want %q (currentWeek)", storedWeekEnd, weekEnd)
	}
}

func TestGeneratePlan_DBError(t *testing.T) {
	db := extendedTestDB(t)

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

	if err := GeneratePlan(context.Background(), db, 1, "next"); err == nil {
		t.Error("expected error when stride_plans table is missing, got nil")
	}
}

func TestGeneratePlan_APIError(t *testing.T) {
	db := extendedTestDB(t)

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

	err := GeneratePlan(context.Background(), db, 1, "next")
	if err == nil {
		t.Error("expected error on API failure, got nil")
	}

	// No plan should be stored.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = 1").Scan(&count); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 plans after API failure, got %d", count)
	}
}

func TestParsePlanResponse_Valid(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()
	days := buildMinimalPlan(weekStart)
	raw, _ := json.Marshal(days)

	parsed, err := parsePlanResponse(string(raw), weekStart, weekEnd)
	if err != nil {
		t.Fatalf("parsePlanResponse: %v", err)
	}
	if len(parsed) != 7 {
		t.Errorf("len = %d, want 7", len(parsed))
	}
}

func TestParsePlanResponse_StripsCodeFences(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()
	days := buildMinimalPlan(weekStart)
	raw, _ := json.Marshal(days)

	fenced := "```json\n" + string(raw) + "\n```"
	parsed, err := parsePlanResponse(fenced, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("parsePlanResponse with fences: %v", err)
	}
	if len(parsed) != 7 {
		t.Errorf("len = %d, want 7", len(parsed))
	}
}

func TestParsePlanResponse_InvalidJSON(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()
	_, err := parsePlanResponse("not json at all", weekStart, weekEnd)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlanResponse_MissingDate(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()
	// Single-element plan fails the 7-day count check.
	raw := `[{"rest_day":true}]`
	_, err := parsePlanResponse(raw, weekStart, weekEnd)
	if err == nil {
		t.Error("expected error for plan with wrong number of days")
	}
}

func TestParsePlanResponse_NonRestDayWithoutSession(t *testing.T) {
	weekStart, weekEnd := upcomingWeek()
	// Single-element plan fails the 7-day count check.
	raw := `[{"date":"2026-04-06","rest_day":false}]`
	_, err := parsePlanResponse(raw, weekStart, weekEnd)
	if err == nil {
		t.Error("expected error for plan with wrong number of days")
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

func TestCurrentWeek_IsMonday(t *testing.T) {
	weekStart, weekEnd := currentWeek()

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

	// Current week Monday should be <= today.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if start.After(today) {
		t.Errorf("currentWeek start %s is after today %s", weekStart, today.Format("2006-01-02"))
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

func TestListRaceResults_Empty(t *testing.T) {
	db := setupTestDB(t)

	results, err := listRaceResults(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("listRaceResults: %v", err)
	}
	if results == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestListRaceResults_ReturnsLinkedRaces(t *testing.T) {
	db := setupTestDB(t)

	// Insert a race with a result_time.
	_, err := db.Exec(`INSERT INTO stride_races (id, user_id, name, date, distance_m, result_time, priority, created_at)
		VALUES (1, 1, 'Test 10K', '2026-03-15', 10000, 2400, 'A', '2026-03-15')`)
	if err != nil {
		t.Fatalf("insert race: %v", err)
	}

	// Insert a workout linked to that race.
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, started_at, duration_seconds, distance_meters, sport, race_id)
		VALUES (1, 1, '2026-03-15T08:00:00Z', 2400, 10000, 'running', 1)`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Insert a race without result_time (should not appear).
	_, err = db.Exec(`INSERT INTO stride_races (id, user_id, name, date, distance_m, priority, created_at)
		VALUES (2, 1, 'Future Race', '2026-06-01', 21097, 'A', '2026-03-01')`)
	if err != nil {
		t.Fatalf("insert future race: %v", err)
	}

	// Insert a workout linked to a race but for a different user (should not appear).
	_, err = db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@test.com', 'Other', 'g2')`)
	if err != nil {
		t.Fatalf("insert user2: %v", err)
	}
	_, err = db.Exec(`INSERT INTO stride_races (id, user_id, name, date, distance_m, result_time, priority, created_at)
		VALUES (3, 2, 'Other Race', '2026-03-10', 5000, 1200, 'B', '2026-03-10')`)
	if err != nil {
		t.Fatalf("insert other race: %v", err)
	}
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, started_at, duration_seconds, distance_meters, sport, race_id)
		VALUES (2, 2, '2026-03-10T08:00:00Z', 1200, 5000, 'running', 3)`)
	if err != nil {
		t.Fatalf("insert other workout: %v", err)
	}

	results, err := listRaceResults(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("listRaceResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "Test 10K" {
		t.Errorf("name = %q, want %q", results[0].Name, "Test 10K")
	}
	if results[0].TimeSecs != 2400 {
		t.Errorf("time = %d, want 2400", results[0].TimeSecs)
	}
	if results[0].DistanceM != 10000 {
		t.Errorf("distance = %f, want 10000", results[0].DistanceM)
	}
	if results[0].Priority != "A" {
		t.Errorf("priority = %q, want %q", results[0].Priority, "A")
	}
}

func TestBuildGeneratePrompt_RaceHistorySection(t *testing.T) {
	history := []RaceResult{
		{Name: "Spring 10K", Date: "2026-03-15", DistanceM: 10000, TimeSecs: 2400, Priority: "A"},
		{Name: "Park Run", Date: "2026-02-01", DistanceM: 5000, TimeSecs: 1185, Priority: "C"},
	}

	prompt := buildGeneratePrompt(
		"2026-04-06", "2026-04-12",
		"", nil, nil,
		history,
		nil, 0, 0,
		nil,
		"", "", "",
		nil,
		"", "",
		"",
	)

	if !strings.Contains(prompt, "## Race History") {
		t.Error("prompt missing Race History section")
	}
	if !strings.Contains(prompt, "Spring 10K") {
		t.Error("prompt missing race name 'Spring 10K'")
	}
	if !strings.Contains(prompt, "40m00s") {
		t.Error("prompt missing formatted time for 10K race")
	}
	if !strings.Contains(prompt, "4:00/km") {
		t.Error("prompt missing pace for 10K race")
	}
	if !strings.Contains(prompt, "Park Run") {
		t.Error("prompt missing race name 'Park Run'")
	}
}

func TestBuildGeneratePrompt_NoRaceHistoryWhenEmpty(t *testing.T) {
	prompt := buildGeneratePrompt(
		"2026-04-06", "2026-04-12",
		"", nil, nil,
		[]RaceResult{},
		nil, 0, 0,
		nil,
		"", "", "",
		nil,
		"", "",
		"",
	)

	if strings.Contains(prompt, "## Race History") {
		t.Error("prompt should not contain Race History when empty")
	}
}

// insertNote is a test helper that inserts a stride note (scope='any') and returns its ID.
func insertNote(t *testing.T, db *sql.DB, userID int64, content string) int64 {
	t.Helper()
	return insertNoteWithScope(t, db, userID, content, "any")
}

// insertNoteWithScope inserts a stride note with the given scope and returns its ID.
func insertNoteWithScope(t *testing.T, db *sql.DB, userID int64, content, scope string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		"INSERT INTO stride_notes (user_id, content, target_date, scope, created_at) VALUES (?, ?, '', ?, ?)",
		userID, content, scope, now,
	)
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insert note last insert id: %v", err)
	}
	return id
}

// mockClaude sets up runPromptFunc to return a valid 7-day plan for the given
// weekStart and registers cleanup with t.Cleanup to restore the original function.
func mockClaude(t *testing.T, weekStart string) {
	t.Helper()
	planDays := buildMinimalPlan(weekStart)
	mockResponse, _ := json.Marshal(planDays)

	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return string(mockResponse), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })
}

// enableStride sets up user preferences to enable stride and Claude.
func enableStride(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	prefs := []struct{ k, v string }{
		{"stride_enabled", "true"},
		{"claude_enabled", "true"},
		{"claude_model", "claude-opus-4-5"},
	}
	for _, p := range prefs {
		if _, err := db.Exec("INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)", userID, p.k, p.v); err != nil {
			t.Fatalf("set pref %s: %v", p.k, err)
		}
	}
}

func TestListUnconsumedNotes_OnlyUnconsumed(t *testing.T) {
	db := extendedTestDB(t)

	// Insert two unconsumed notes.
	insertNote(t, db, 1, "note-a")
	insertNote(t, db, 1, "note-b")

	// Insert a consumed note.
	id3 := insertNote(t, db, 1, "note-consumed")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec("UPDATE stride_notes SET consumed_at = ?, consumed_by = 'manual' WHERE id = ?", now, id3); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}

	notes, err := listUnconsumedNotes(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("listUnconsumedNotes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 unconsumed notes, got %d", len(notes))
	}
	for _, n := range notes {
		if n.Content == "note-consumed" {
			t.Error("consumed note should not appear in unconsumed list")
		}
	}
}

func TestListUnconsumedNotes_ExcludesNightlyConsumed(t *testing.T) {
	db := extendedTestDB(t)

	insertNote(t, db, 1, "note-active")

	// Simulate a note consumed by the nightly process.
	idNightly := insertNote(t, db, 1, "note-nightly")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec("UPDATE stride_notes SET consumed_at = ?, consumed_by = 'nightly' WHERE id = ?", now, idNightly); err != nil {
		t.Fatalf("mark nightly consumed: %v", err)
	}

	notes, err := listUnconsumedNotes(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("listUnconsumedNotes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 unconsumed note, got %d", len(notes))
	}
	if notes[0].Content != "note-active" {
		t.Errorf("expected note-active, got %q", notes[0].Content)
	}
}

func TestGeneratePlan_MarksNotesConsumed(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)

	id1 := insertNote(t, db, 1, "plan-note-1")
	id2 := insertNote(t, db, 1, "plan-note-2")

	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)

	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// Both notes should now be consumed with consumed_by='weekly'.
	for _, id := range []int64{id1, id2} {
		var consumedAt sql.NullString
		var consumedBy sql.NullString
		if err := db.QueryRow("SELECT consumed_at, consumed_by FROM stride_notes WHERE id = ?", id).Scan(&consumedAt, &consumedBy); err != nil {
			t.Fatalf("query note %d: %v", id, err)
		}
		if !consumedAt.Valid {
			t.Errorf("note %d: consumed_at should be set", id)
		}
		if !consumedBy.Valid || consumedBy.String != "weekly" {
			t.Errorf("note %d: consumed_by = %q, want %q", id, consumedBy.String, "weekly")
		}
	}
}

func TestGeneratePlan_FailedInsertRollsBackNoteConsumption(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)

	id1 := insertNote(t, db, 1, "rollback-note")

	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)

	// Drop stride_plans to trigger a DB error on insert.
	if _, err := db.Exec("DROP TABLE stride_plans"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	err := GeneratePlan(context.Background(), db, 1, "next")
	if err == nil {
		t.Fatal("expected error when stride_plans table is missing, got nil")
	}

	// Note should remain unconsumed because the transaction was rolled back.
	var consumedAt sql.NullString
	if err := db.QueryRow("SELECT consumed_at FROM stride_notes WHERE id = ?", id1).Scan(&consumedAt); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if consumedAt.Valid {
		t.Error("note should remain unconsumed after failed plan insert")
	}
}

func TestGeneratePlan_UnconsumedNotesSurviveAcrossRuns(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)

	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)

	// First run: one note gets consumed.
	insertNote(t, db, 1, "first-run-note")

	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("first GeneratePlan: %v", err)
	}

	// Add a new note after the first run.
	id2 := insertNote(t, db, 1, "second-run-note")

	// Second run: only the new note should be picked up.
	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("second GeneratePlan: %v", err)
	}

	// The second note should now be consumed.
	var consumedAt sql.NullString
	var consumedBy sql.NullString
	if err := db.QueryRow("SELECT consumed_at, consumed_by FROM stride_notes WHERE id = ?", id2).Scan(&consumedAt, &consumedBy); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if !consumedAt.Valid {
		t.Error("second note should be consumed after second run")
	}
	if !consumedBy.Valid || consumedBy.String != "weekly" {
		t.Errorf("consumed_by = %q, want %q", consumedBy.String, "weekly")
	}

	// Verify total: all notes should be consumed now.
	var unconsumed int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_notes WHERE user_id = 1 AND consumed_at IS NULL").Scan(&unconsumed); err != nil {
		t.Fatalf("count unconsumed: %v", err)
	}
	if unconsumed != 0 {
		t.Errorf("expected 0 unconsumed notes, got %d", unconsumed)
	}
}

// TestGeneratePlan_ScopeFiltering verifies that the weekly generation only
// consumes notes scoped 'any' or 'weekly' and leaves notes scoped 'nightly'
// untouched for the nightly evaluator.
func TestGeneratePlan_ScopeFiltering(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)

	weekStart, _ := upcomingWeek()
	mockClaude(t, weekStart)

	idAny := insertNoteWithScope(t, db, 1, "any-note", "any")
	idWeekly := insertNoteWithScope(t, db, 1, "weekly-note", "weekly")
	idNightly := insertNoteWithScope(t, db, 1, "nightly-note", "nightly")

	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// 'any' and 'weekly' notes should be consumed.
	for _, id := range []int64{idAny, idWeekly} {
		var consumedAt sql.NullString
		var consumedBy sql.NullString
		if err := db.QueryRow("SELECT consumed_at, consumed_by FROM stride_notes WHERE id = ?", id).Scan(&consumedAt, &consumedBy); err != nil {
			t.Fatalf("query note %d: %v", id, err)
		}
		if !consumedAt.Valid {
			t.Errorf("note %d should be consumed by weekly run", id)
		}
		if !consumedBy.Valid || consumedBy.String != "weekly" {
			t.Errorf("note %d consumed_by = %q, want 'weekly'", id, consumedBy.String)
		}
	}

	// 'nightly' note must remain unconsumed.
	var consumedAt sql.NullString
	if err := db.QueryRow("SELECT consumed_at FROM stride_notes WHERE id = ?", idNightly).Scan(&consumedAt); err != nil {
		t.Fatalf("query nightly note: %v", err)
	}
	if consumedAt.Valid {
		t.Errorf("nightly-scoped note must NOT be consumed by the weekly run, got consumed_at=%v", consumedAt.String)
	}
}

// insertEncryptedEvaluation inserts a stride_evaluations row with the given
// evaluation payload encrypted, returning the row id.
func insertEncryptedEvaluation(t *testing.T, db *sql.DB, userID, planID int64, workoutID *int64, eval Evaluation, createdAt string) int64 {
	t.Helper()
	bytes, err := json.Marshal(eval)
	if err != nil {
		t.Fatalf("marshal eval: %v", err)
	}
	enc, err := encryption.EncryptField(string(bytes))
	if err != nil {
		t.Fatalf("encrypt eval: %v", err)
	}
	var res sql.Result
	if workoutID != nil {
		res, err = db.Exec(`
			INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
			VALUES (?, ?, ?, ?, ?)`, userID, planID, *workoutID, enc, createdAt)
	} else {
		res, err = db.Exec(`
			INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
			VALUES (?, ?, NULL, ?, ?)`, userID, planID, enc, createdAt)
	}
	if err != nil {
		t.Fatalf("insert evaluation: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestListRecentEvaluations_FiltersAndOrders(t *testing.T) {
	db := extendedTestDB(t)

	planID := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -14)

	// Rest-day eval (workout_id IS NULL) seven days ago — should be returned
	// and, being older, come first in ascending order.
	restCreatedAt := now.AddDate(0, 0, -7).Format(time.RFC3339)
	insertEncryptedEvaluation(t, db, 1, planID, nil, Evaluation{
		PlannedType: "rest", ActualType: "rest",
		Compliance: "rest_day", Notes: "Rest day taken.",
		Date: now.AddDate(0, 0, -7).Format("2006-01-02"),
	}, restCreatedAt)

	// Workout eval three days ago — should be returned and come second.
	recentWorkoutStart := now.AddDate(0, 0, -3).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, distance_meters, fit_file_hash)
		VALUES (10, 1, 'running', ?, 8500, 'hash-recent')`, recentWorkoutStart); err != nil {
		t.Fatalf("insert recent workout: %v", err)
	}
	wid10 := int64(10)
	insertEncryptedEvaluation(t, db, 1, planID, &wid10, Evaluation{
		PlannedType: "threshold", ActualType: "threshold",
		Compliance: "compliant", Notes: "Solid threshold session.",
		Flags: []string{}, Adjustments: "Continue.",
	}, recentWorkoutStart)

	// Workout outside the window — should be filtered out.
	oldWorkoutStart := now.AddDate(0, 0, -30).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, distance_meters, fit_file_hash)
		VALUES (11, 1, 'running', ?, 5000, 'hash-old')`, oldWorkoutStart); err != nil {
		t.Fatalf("insert old workout: %v", err)
	}
	wid11 := int64(11)
	insertEncryptedEvaluation(t, db, 1, planID, &wid11, Evaluation{
		PlannedType: "easy", ActualType: "easy",
		Compliance: "compliant", Notes: "Old eval.",
	}, oldWorkoutStart)

	// Cross-user evaluation must not leak.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@x.com', 'Other', 'g2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
	otherPlanID := insertTestPlan(t, db, 2, "2026-04-07", "2026-04-13", `[]`)
	if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, distance_meters, fit_file_hash)
		VALUES (12, 2, 'running', ?, 7000, 'hash-other')`, recentWorkoutStart); err != nil {
		t.Fatalf("insert other-user workout: %v", err)
	}
	wid12 := int64(12)
	insertEncryptedEvaluation(t, db, 2, otherPlanID, &wid12, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "compliant",
	}, recentWorkoutStart)

	rows, err := listRecentEvaluations(context.Background(), db, 1, since)
	if err != nil {
		t.Fatalf("listRecentEvaluations: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows in window, got %d", len(rows))
	}
	// Ordered ascending by date, so rest-day (older) precedes the workout.
	if rows[0].WorkoutID != nil {
		t.Errorf("rows[0] should be the rest-day eval (workout_id nil), got %v", rows[0].WorkoutID)
	}
	if rows[0].Eval.Compliance != "rest_day" {
		t.Errorf("rows[0].Compliance = %q, want rest_day", rows[0].Eval.Compliance)
	}
	if rows[1].WorkoutID == nil || *rows[1].WorkoutID != 10 {
		t.Errorf("rows[1].WorkoutID = %v, want pointer to 10", rows[1].WorkoutID)
	}
	if rows[1].Sport != "running" {
		t.Errorf("rows[1].Sport = %q, want running", rows[1].Sport)
	}
	if rows[1].DistanceM != 8500 {
		t.Errorf("rows[1].DistanceM = %v, want 8500", rows[1].DistanceM)
	}
}

// TestListRecentEvaluations_CrossUserWorkoutJoin verifies that when a
// stride_evaluations row for user 1 references a workout_id that belongs to
// user 2, the JOIN does not pull in user 2's workout metadata. The evaluation
// row must still be returned (it belongs to user 1) but Sport and DistanceM
// must be zero-valued.
func TestListRecentEvaluations_CrossUserWorkoutJoin(t *testing.T) {
	db := extendedTestDB(t)

	// Set up two users.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@x.com', 'Other', 'g2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -14)

	// Insert a workout owned by user 2.
	workoutStart := now.AddDate(0, 0, -3).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, distance_meters, fit_file_hash)
		VALUES (20, 2, 'cycling', ?, 50000, 'hash-u2')`, workoutStart); err != nil {
		t.Fatalf("insert user-2 workout: %v", err)
	}

	// Insert an evaluation for user 1 that references user 2's workout id.
	planID := insertTestPlan(t, db, 1, "2026-04-14", "2026-04-20", `[]`)
	wid20 := int64(20)
	insertEncryptedEvaluation(t, db, 1, planID, &wid20, Evaluation{
		PlannedType: "easy", ActualType: "easy", Compliance: "compliant",
		Notes: "Cross-user workout reference.",
	}, workoutStart)

	rows, err := listRecentEvaluations(context.Background(), db, 1, since)
	if err != nil {
		t.Fatalf("listRecentEvaluations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for user 1, got %d", len(rows))
	}
	row := rows[0]
	// The evaluation belongs to user 1, so it should be returned.
	if row.WorkoutID == nil || *row.WorkoutID != 20 {
		t.Errorf("WorkoutID = %v, want pointer to 20", row.WorkoutID)
	}
	// The JOIN must not leak user 2's workout metadata.
	// Note: Date is still set via the e.created_at fallback — that is expected.
	if row.Sport != "" {
		t.Errorf("Sport = %q, want empty (cross-user join must not return other user's data)", row.Sport)
	}
	if row.DistanceM != 0 {
		t.Errorf("DistanceM = %v, want 0 (cross-user join must not return other user's data)", row.DistanceM)
	}
}

func TestRenderEvaluationsSection_Empty(t *testing.T) {
	if got := renderEvaluationsSection(nil); got != "" {
		t.Errorf("expected empty string for nil evals, got %q", got)
	}
	if got := renderEvaluationsSection([]EvaluationRow{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func TestBuildGeneratePrompt_EvaluationsSection(t *testing.T) {
	wid := int64(42)
	evals := []EvaluationRow{
		{
			WorkoutID: &wid,
			Date:      "2026-04-15",
			Sport:     "running",
			DistanceM: 12000,
			Eval: Evaluation{
				PlannedType: "threshold",
				ActualType:  "threshold",
				Compliance:  "compliant",
				Notes:       "Hit the splits cleanly with HR controlled.",
				Flags:       []string{"hr_too_high"},
				Adjustments: "Hold pace next session, monitor recovery HR.",
			},
		},
		{
			WorkoutID: nil,
			Date:      "2026-04-16",
			Eval: Evaluation{
				PlannedType: "rest",
				ActualType:  "rest",
				Compliance:  "rest_day",
				Notes:       "Rest day taken as planned.",
				Flags:       []string{},
				Adjustments: "",
				Date:        "2026-04-16",
			},
		},
	}

	prompt := buildGeneratePrompt(
		"2026-04-20", "2026-04-26",
		"", nil, nil,
		nil,
		nil, 0, 0,
		nil,
		"", "", "",
		evals,
		"", "",
		"",
	)

	if !strings.Contains(prompt, "## Recent Workout Evaluations (last 14 days)") {
		t.Error("prompt missing evaluations section heading")
	}
	if !strings.Contains(prompt, "[2026-04-15]") {
		t.Error("prompt missing workout eval date")
	}
	if !strings.Contains(prompt, "running, 12.0 km") {
		t.Error("prompt missing sport + distance label")
	}
	if !strings.Contains(prompt, "planned threshold vs actual threshold") {
		t.Error("prompt missing planned/actual rendering")
	}
	if !strings.Contains(prompt, "compliance: compliant") {
		t.Error("prompt missing compliance rendering")
	}
	if !strings.Contains(prompt, "flags: hr_too_high") {
		t.Error("prompt missing flag rendering")
	}
	if !strings.Contains(prompt, "Notes: Hit the splits cleanly") {
		t.Error("prompt missing notes rendering")
	}
	if !strings.Contains(prompt, "Adjustments: Hold pace") {
		t.Error("prompt missing adjustments rendering")
	}
	if !strings.Contains(prompt, "rest day") {
		t.Error("prompt missing rest day label for workout_id IS NULL row")
	}
	if !strings.Contains(prompt, "compliance: rest_day") {
		t.Error("prompt missing rest_day compliance rendering")
	}
}

func TestBuildGeneratePrompt_NoEvaluationsSectionWhenEmpty(t *testing.T) {
	prompt := buildGeneratePrompt(
		"2026-04-20", "2026-04-26",
		"", nil, nil,
		nil,
		nil, 0, 0,
		nil,
		"", "", "",
		nil,
		"", "",
		"",
	)

	if strings.Contains(prompt, "## Recent Workout Evaluations") {
		t.Error("prompt should not contain evaluations section when slice is empty")
	}
}

func TestRenderEvaluationsSection_TruncatesLongNotesAndAdjustments(t *testing.T) {
	longNotes := strings.Repeat("a", 250)
	longAdj := strings.Repeat("b", 300)
	wid := int64(7)
	evals := []EvaluationRow{
		{
			WorkoutID: &wid,
			Date:      "2026-04-15",
			Sport:     "running",
			DistanceM: 5000,
			Eval: Evaluation{
				PlannedType: "easy",
				ActualType:  "easy",
				Compliance:  "compliant",
				Notes:       longNotes,
				Adjustments: longAdj,
			},
		},
	}
	out := renderEvaluationsSection(evals)

	if strings.Contains(out, longNotes) {
		t.Error("notes should be truncated, but full untruncated string is present")
	}
	if strings.Contains(out, longAdj) {
		t.Error("adjustments should be truncated, but full untruncated string is present")
	}
	if !strings.Contains(out, strings.Repeat("a", 200)+"...") {
		t.Error("notes should be truncated to 200 chars with ellipsis")
	}
	if !strings.Contains(out, strings.Repeat("b", 200)+"...") {
		t.Error("adjustments should be truncated to 200 chars with ellipsis")
	}
}

func TestRenderEvaluationsSection_RestDayRendersWithoutSport(t *testing.T) {
	evals := []EvaluationRow{
		{
			WorkoutID: nil,
			Date:      "2026-04-16",
			Eval: Evaluation{
				PlannedType: "rest",
				ActualType:  "rest",
				Compliance:  "rest_day",
				Notes:       "Recovery day complete.",
				Date:        "2026-04-16",
			},
		},
	}
	out := renderEvaluationsSection(evals)

	if !strings.Contains(out, "[2026-04-16] rest day") {
		t.Errorf("rest-day eval should render with 'rest day' label, got %q", out)
	}
	if !strings.Contains(out, "Notes: Recovery day complete.") {
		t.Errorf("rest-day notes missing, got %q", out)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short string returned as is", "hello", 200, "hello"},
		{"exact length returned as is", "abcde", 5, "abcde"},
		{"longer string truncated with ellipsis", "abcdefghij", 5, "abcde..."},
		{"empty string returns empty", "", 5, ""},
		{"non-positive n returns input", "abcdef", 0, "abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.in, tc.n); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestGeneratePlan_PromptIncludesRecentEvaluations(t *testing.T) {
	db := extendedTestDB(t)
	enableStride(t, db, 1)

	// Seed a previous-week plan that listRecentEvaluations can join against.
	planID := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	// Insert a workout + evaluation inside the 14-day window.
	now := time.Now().UTC()
	workoutStart := now.AddDate(0, 0, -2).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, distance_meters, fit_file_hash)
		VALUES (50, 1, 'running', ?, 9000, 'hash-prompt-include')`, workoutStart); err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	wid := int64(50)
	insertEncryptedEvaluation(t, db, 1, planID, &wid, Evaluation{
		PlannedType: "easy", ActualType: "easy",
		Compliance: "compliant", Notes: "Easy run completed.",
		Flags: []string{}, Adjustments: "",
	}, workoutStart)

	weekStart, _ := upcomingWeek()
	planDays := buildMinimalPlan(weekStart)
	mockResponse, _ := json.Marshal(planDays)

	var capturedPrompt string
	origFn := runPromptFunc
	runPromptFunc = func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
		capturedPrompt = prompt
		return string(mockResponse), nil
	}
	t.Cleanup(func() { runPromptFunc = origFn })

	if err := GeneratePlan(context.Background(), db, 1, "next"); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if !strings.Contains(capturedPrompt, "## Recent Workout Evaluations (last 14 days)") {
		t.Error("expected evaluations section in generated prompt")
	}
	if !strings.Contains(capturedPrompt, "Notes: Easy run completed.") {
		t.Error("expected workout-eval notes in generated prompt")
	}
}
