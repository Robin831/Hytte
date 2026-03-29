package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/hrzones"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-training-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	// SQLite :memory: databases are per-connection; limit to 1 to avoid "no such table" races.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return database
}

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withAdminUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test", IsAdmin: true}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListHandler_Empty(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts", nil), 1)
	w := httptest.NewRecorder()
	ListHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	workouts, ok := resp["workouts"].([]any)
	if !ok {
		t.Fatal("expected workouts array")
	}
	if len(workouts) != 0 {
		t.Fatalf("expected empty list, got %d", len(workouts))
	}
}

func TestCreateAndGet(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		MaxHeartRate:    175,
		Laps: []ParsedLap{
			{DurationSeconds: 600, DistanceMeters: 1000, AvgHeartRate: 145, MaxHeartRate: 160, AvgSpeedMPerS: 1.67},
			{DurationSeconds: 600, DistanceMeters: 1000, AvgHeartRate: 155, MaxHeartRate: 170, AvgSpeedMPerS: 1.67},
		},
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: 120, SpeedMPerS: 2.5},
			{OffsetMs: 1000, HeartRate: 140, SpeedMPerS: 2.8},
			{OffsetMs: 2000, HeartRate: 150, SpeedMPerS: 3.0},
		},
	}

	workout, err := Create(database, 1, pw, "abc123hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if workout.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if workout.Sport != "running" {
		t.Fatalf("expected running, got %s", workout.Sport)
	}
	if len(workout.Laps) != 2 {
		t.Fatalf("expected 2 laps, got %d", len(workout.Laps))
	}
	if workout.Samples == nil || len(workout.Samples.Points) != 3 {
		t.Fatal("expected 3 sample points")
	}

	// Test duplicate detection.
	exists, err := HashExists(database, 1, "abc123hash")
	if err != nil {
		t.Fatalf("hash exists: %v", err)
	}
	if !exists {
		t.Fatal("expected hash to exist")
	}

	// Test delete.
	if err := Delete(database, workout.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = GetByID(database, workout.ID, 1)
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestUpdateTags(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "taghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := UpdateTags(database, workout.ID, 1, []string{"6x6", "intervals"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// 2 manual tags + auto:treadmill (no GPS in test workout)
	if len(w.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(w.Tags), w.Tags)
	}
}

func TestCreateAutoTags(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with a clear alternating interval pattern (work/rest).
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		MaxHeartRate:    175,
		Laps: []ParsedLap{
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
		},
	}

	workout, err := Create(database, 1, pw, "autotaghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	hasAutoTag := false
	for _, tag := range workout.Tags {
		if strings.HasPrefix(tag, "auto:") {
			hasAutoTag = true
			break
		}
	}
	if !hasAutoTag {
		t.Fatalf("expected auto-tag on workout with interval pattern, got tags: %v", workout.Tags)
	}
}

func TestUpdateTagsPreservesAutoTags(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		Laps: []ParsedLap{
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
		},
	}

	workout, err := Create(database, 1, pw, "preservehash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	initialAutoTags := 0
	for _, tag := range workout.Tags {
		if strings.HasPrefix(tag, "auto:") {
			initialAutoTags++
		}
	}
	if initialAutoTags == 0 {
		t.Fatal("expected auto-tags on workout with interval pattern")
	}

	// Update manual tags — auto-tags should be preserved.
	if err := UpdateTags(database, workout.ID, 1, []string{"my-tag", "intervals"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	autoCount := 0
	manualCount := 0
	for _, tag := range w.Tags {
		if strings.HasPrefix(tag, "auto:") {
			autoCount++
		} else {
			manualCount++
		}
	}
	if autoCount != initialAutoTags {
		t.Fatalf("expected %d auto-tags preserved, got %d (tags: %v)", initialAutoTags, autoCount, w.Tags)
	}
	if manualCount != 2 {
		t.Fatalf("expected 2 manual tags, got %d (tags: %v)", manualCount, w.Tags)
	}
}

func TestUpdateTagsFiltersAutoPrefix(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "filterhash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// User tries to submit tags with "auto:" prefix — should be filtered out.
	if err := UpdateTags(database, workout.ID, 1, []string{"auto:fake", "legit-tag"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	for _, tag := range w.Tags {
		if tag == "auto:fake" {
			t.Fatal("auto:fake should have been filtered out from user input")
		}
	}
	found := false
	for _, tag := range w.Tags {
		if tag == "legit-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'legit-tag' in tags, got %v", w.Tags)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/training/workouts/999", nil), 1)
	req = withChiParam(req, "id", "999")

	w := httptest.NewRecorder()
	DeleteHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/999", nil), 1)
	req = withChiParam(req, "id", "999")

	w := httptest.NewRecorder()
	GetHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUploadHandler_NoFiles(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/upload", nil), 1)
	w := httptest.NewRecorder()
	UploadHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_MissingParams(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=999&b=998", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCompareHandler_Success(t *testing.T) {
	database := setupTestDB(t)

	pw1 := &ParsedWorkout{
		Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 150,
		Laps: []ParsedLap{{DurationSeconds: 600, DistanceMeters: 1000, AvgSpeedMPerS: 2.0}},
	}
	pw2 := &ParsedWorkout{
		Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 148,
		Laps: []ParsedLap{{DurationSeconds: 600, DistanceMeters: 1000, AvgSpeedMPerS: 2.0}},
	}
	w1, err := Create(database, 1, pw1, "cmph1")
	if err != nil {
		t.Fatalf("create w1: %v", err)
	}
	w2, err := Create(database, 1, pw2, "cmph2")
	if err != nil {
		t.Fatalf("create w2: %v", err)
	}

	url := fmt.Sprintf("/api/training/compare?a=%d&b=%d", w1.ID, w2.ID)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestZonesHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/999/zones", nil), 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()
	ZonesHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestZonesHandler_NoSamples(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 150}
	workout, err := Create(database, 1, pw, "znoshash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	url := fmt.Sprintf("/api/training/workouts/%d/zones", workout.ID)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	req = withChiParam(req, "id", strconv.FormatInt(workout.ID, 10))
	w := httptest.NewRecorder()
	ZonesHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestWeeklySummaries(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150}
	_, err := Create(database, 1, pw, "wkhash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	summaries, err := WeeklySummaries(database, 1)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].WorkoutCount != 1 {
		t.Fatalf("expected 1 workout count, got %d", summaries[0].WorkoutCount)
	}
	if summaries[0].TotalDuration != 3600 {
		t.Fatalf("expected total duration 3600, got %d", summaries[0].TotalDuration)
	}
}

func TestWeeklySummaries_IncludesNoHRWorkouts(t *testing.T) {
	database := setupTestDB(t)

	// Workout without HR data should still count toward weekly totals.
	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 0}
	_, err := Create(database, 1, pw, "nohrwk")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	summaries, err := WeeklySummaries(database, 1)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary (no-HR workout counted), got %d", len(summaries))
	}
	if summaries[0].WorkoutCount != 1 {
		t.Fatalf("expected workout_count=1, got %d", summaries[0].WorkoutCount)
	}
}

func TestGetProgression(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150}
	workout, err := Create(database, 1, pw, "proghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := UpdateTags(database, workout.ID, 1, []string{"6x6"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	groups, err := GetProgression(database, 1)
	if err != nil {
		t.Fatalf("progression: %v", err)
	}
	// May have 2 groups: auto:treadmill (from no-GPS test workout) + 6x6 (manual tag).
	var found bool
	for _, g := range groups {
		if g.Tag == "6x6" {
			found = true
			if len(g.Workouts) != 1 {
				t.Fatalf("expected 1 workout in '6x6' group, got %d", len(g.Workouts))
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected '6x6' group in progression, got %v", groups)
	}
}

func TestCompareHandler_LapSelection_Valid(t *testing.T) {
	database := setupTestDB(t)

	// Workout A: 3 laps; workout B: 4 laps — incompatible in auto mode.
	idA := insertTestWorkoutWithHR(t, database, 1, "running",
		[]int{150, 160, 155}, []float64{300, 300, 300})
	idB := insertTestWorkoutWithHR(t, database, 1, "running",
		[]int{148, 158, 153, 140}, []float64{300, 300, 300, 300})

	url := fmt.Sprintf("/api/training/compare?a=%d&b=%d&laps_a=0,1&laps_b=0,1", idA, idB)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cmp, ok := resp["comparison"].(map[string]any)
	if !ok {
		t.Fatal("expected comparison object")
	}
	if cmp["compatible"] != true {
		t.Errorf("expected compatible=true, got %v", cmp["compatible"])
	}
}

func TestCompareHandler_LapSelection_InvalidIntegers(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=0,x&laps_b=0,1", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_LapSelection_OnlyOneProvided(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=0,1", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when only laps_a provided, got %d", w.Code)
	}
}

func TestCompareHandler_LapSelection_EmptyParams(t *testing.T) {
	database := setupTestDB(t)

	// Both params present but empty — should be rejected, not silently fall back to auto mode.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=&laps_b=", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty laps params, got %d", w.Code)
	}
}

func TestGetZoneDistribution(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150,
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: 130, SpeedMPerS: 3.0},
			{OffsetMs: 1000, HeartRate: 155, SpeedMPerS: 3.0},
			{OffsetMs: 2000, HeartRate: 170, SpeedMPerS: 3.0},
		},
	}
	workout, err := Create(database, 1, pw, "zonehash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	zones, err := GetZoneDistribution(database, workout.ID, 1, hrzones.GetDefaultZones(220))
	if err != nil {
		t.Fatalf("zones: %v", err)
	}
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}
	var total float64
	for _, z := range zones {
		total += z.Percentage
	}
	if total < 99 || total > 101 {
		t.Fatalf("expected ~100%% total percentage, got %.1f", total)
	}
}

// TestScheduleBackgroundAnalysis_AdminEnabled_Fires verifies that
// scheduleBackgroundAnalysis triggers RunClaudeAnalysis for an admin user
// with the claude_ai feature and claude_enabled config set.
func TestScheduleBackgroundAnalysis_AdminEnabled_Fires(t *testing.T) {
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
		select {
		case called <- struct{}{}:
		default:
		}
		return `{"type":"easy_run","tag":"easy","summary":"Easy run","title":"Easy Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	// Admin users automatically have claude_ai feature enabled.
	scheduleBackgroundAnalysis(database, 1, true, []Workout{{ID: 1}})

	select {
	case <-called:
		// success: background analysis fired
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: background analysis did not fire within 2s")
	}
}

// TestScheduleBackgroundAnalysis_NonAdmin_DoesNotFire verifies that
// scheduleBackgroundAnalysis does NOT trigger for non-admin users.
func TestScheduleBackgroundAnalysis_NonAdmin_DoesNotFire(t *testing.T) {
	database := setupTestDB(t)

	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleBackgroundAnalysis(database, 1, false, []Workout{{ID: 1}})

	// Allow time for any spurious goroutines.
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("expected no analysis triggered for non-admin user")
	}
}

// --- ACRTrendHandler ---

func TestACRTrendHandler_DefaultWeeks(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array, got %T", resp["acr_trend"])
	}
	// Default is 26 weeks.
	if len(trend) != 26 {
		t.Errorf("expected 26 points by default, got %d", len(trend))
	}
}

func TestACRTrendHandler_CustomWeeks(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=4", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array")
	}
	if len(trend) != 4 {
		t.Errorf("expected 4 points for ?weeks=4, got %d", len(trend))
	}
}

func TestACRTrendHandler_FallsBackToDefaultWhenWeeksOverLimit(t *testing.T) {
	database := setupTestDB(t)

	// weeks=200 is above the 104 limit — handler falls back to the default (26).
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=200", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array")
	}
	// Invalid value falls back to default (26).
	if len(trend) != 26 {
		t.Errorf("expected 26 points when weeks=200 (over limit), got %d", len(trend))
	}
}

func TestACRTrendHandler_PointsContainDateField(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=2", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok || len(trend) == 0 {
		t.Fatal("expected non-empty acr_trend array")
	}

	first, ok := trend[0].(map[string]any)
	if !ok {
		t.Fatalf("expected point to be a map, got %T", trend[0])
	}
	if _, has := first["date"]; !has {
		t.Errorf("expected 'date' field in ACR trend point, got keys: %v", first)
	}
}
