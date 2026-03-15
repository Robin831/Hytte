package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
