package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
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
	if len(w.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(w.Tags))
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
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Tag != "6x6" {
		t.Fatalf("expected tag '6x6', got %s", groups[0].Tag)
	}
	if len(groups[0].Workouts) != 1 {
		t.Fatalf("expected 1 workout in group, got %d", len(groups[0].Workouts))
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

	zones, err := GetZoneDistribution(database, workout.ID, 1, 180)
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
