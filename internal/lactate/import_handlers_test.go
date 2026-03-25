package lactate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

// setupImportTestDB creates an in-memory DB with all tables needed for import handler tests.
func setupImportTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-import-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE workouts (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			sport      TEXT NOT NULL DEFAULT '',
			title      TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE workout_laps (
			id                INTEGER PRIMARY KEY,
			workout_id        INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
			lap_number        INTEGER NOT NULL,
			start_offset_ms   INTEGER NOT NULL DEFAULT 0,
			duration_seconds  REAL NOT NULL DEFAULT 0,
			distance_meters   REAL NOT NULL DEFAULT 0,
			avg_heart_rate    INTEGER NOT NULL DEFAULT 0,
			max_heart_rate    INTEGER NOT NULL DEFAULT 0,
			avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
			avg_cadence       INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE workout_samples (
			workout_id INTEGER PRIMARY KEY REFERENCES workouts(id) ON DELETE CASCADE,
			data       TEXT NOT NULL
		);
		CREATE TABLE lactate_tests (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id          INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
			date                TEXT NOT NULL DEFAULT '',
			comment             TEXT NOT NULL DEFAULT '',
			protocol_type       TEXT NOT NULL DEFAULT 'standard',
			warmup_duration_min INTEGER NOT NULL DEFAULT 10,
			stage_duration_min  INTEGER NOT NULL DEFAULT 5,
			start_speed_kmh     REAL NOT NULL DEFAULT 11.5,
			speed_increment_kmh REAL NOT NULL DEFAULT 0.5,
			created_at          TEXT NOT NULL DEFAULT '',
			updated_at          TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE lactate_test_stages (
			id             INTEGER PRIMARY KEY,
			test_id        INTEGER NOT NULL REFERENCES lactate_tests(id) ON DELETE CASCADE,
			stage_number   INTEGER NOT NULL,
			speed_kmh      REAL NOT NULL,
			lactate_mmol   REAL NOT NULL,
			heart_rate_bpm INTEGER NOT NULL DEFAULT 0,
			rpe            INTEGER,
			notes          TEXT NOT NULL DEFAULT '',
			UNIQUE(test_id, stage_number)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(
		"INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

// insertTestWorkout inserts a workout row and returns its ID.
func insertTestWorkout(t *testing.T, db *sql.DB, userID int64, startedAt string) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO workouts (user_id, sport, title, started_at) VALUES (?, 'run', 'Test workout', ?)",
		userID, startedAt,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// insertTestLap inserts a lap row for a workout.
func insertTestLap(t *testing.T, db *sql.DB, workoutID int64, lapNum int, startMs int64, durSec, pace float64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO workout_laps (workout_id, lap_number, start_offset_ms, duration_seconds, avg_pace_sec_per_km)
		 VALUES (?, ?, ?, ?, ?)`,
		workoutID, lapNum, startMs, durSec, pace,
	)
	if err != nil {
		t.Fatalf("insert lap: %v", err)
	}
}

// insertTestSamples inserts JSON-encoded samples for a workout.
func insertTestSamples(t *testing.T, db *sql.DB, workoutID int64, samples []samplePoint) {
	t.Helper()
	data, err := json.Marshal(samples)
	if err != nil {
		t.Fatalf("marshal samples: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO workout_samples (workout_id, data) VALUES (?, ?)", workoutID, string(data),
	); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
}

const validLactateData = "10.5 1.8\n11.0 2.2\n11.5 3.1"

func TestPreviewFromWorkoutHandler_ParseError(t *testing.T) {
	db := setupImportTestDB(t)
	wid := insertTestWorkout(t, db, 1, "2026-03-14T09:00:00Z")

	body := fmt.Sprintf(`{"workout_id": %d, "lactate_data": "bad data here"}`, wid)
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/preview-from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PreviewFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["hint"] == "" {
		t.Error("expected hint field in parse error response")
	}
}

func TestPreviewFromWorkoutHandler_WorkoutNotFound(t *testing.T) {
	db := setupImportTestDB(t)

	body := `{"workout_id": 9999, "lactate_data": "10.5 1.8\n11.0 2.2"}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/preview-from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PreviewFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPreviewFromWorkoutHandler_NoLaps(t *testing.T) {
	db := setupImportTestDB(t)
	wid := insertTestWorkout(t, db, 1, "2026-03-14T09:00:00Z")
	// No laps inserted.

	body := fmt.Sprintf(`{"workout_id": %d, "lactate_data": %q}`, wid, validLactateData)
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/preview-from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PreviewFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Stages   []ProposedStage `json:"stages"`
		Warnings []string        `json:"warnings"`
		Method   string          `json:"method"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Stages) != 3 {
		t.Errorf("expected 3 stages, got %d", len(resp.Stages))
	}
	if len(resp.Warnings) == 0 {
		t.Error("expected at least one warning when no laps")
	}
	for _, s := range resp.Stages {
		if s.HeartRateBpm != 0 {
			t.Errorf("expected HR=0 when no laps, got %d at stage %d", s.HeartRateBpm, s.StageNumber)
		}
	}
}

func TestPreviewFromWorkoutHandler_HappyPath(t *testing.T) {
	db := setupImportTestDB(t)
	wid := insertTestWorkout(t, db, 1, "2026-03-14T09:00:00Z")

	// Warmup: 10 min = 600 s, pace ~= 3600/10.0 = 360 s/km
	insertTestLap(t, db, wid, 1, 0, 600, 360.0)
	// Stage 1: speed 10.5 km/h → pace = 3600/10.5 ≈ 342.86 s/km, 5 min = 300 s
	insertTestLap(t, db, wid, 2, 600_000, 300, 3600.0/10.5)
	// Stage 2: speed 11.0 km/h → pace = 3600/11.0 ≈ 327.27 s/km
	insertTestLap(t, db, wid, 3, 900_000, 300, 3600.0/11.0)
	// Stage 3: speed 11.5 km/h → pace = 3600/11.5 ≈ 313.04 s/km
	insertTestLap(t, db, wid, 4, 1_200_000, 300, 3600.0/11.5)

	// Insert HR samples — one sample in the last 30 s of each stage.
	insertTestSamples(t, db, wid, []samplePoint{
		{OffsetMs: 870_000, HeartRate: 135}, // last 30s of lap 2
		{OffsetMs: 1_170_000, HeartRate: 150}, // last 30s of lap 3
		{OffsetMs: 1_470_000, HeartRate: 163}, // last 30s of lap 4
	})

	body := fmt.Sprintf(
		`{"workout_id": %d, "lactate_data": %q, "warmup_duration_min": 10, "stage_duration_min": 5}`,
		wid, validLactateData,
	)
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/preview-from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PreviewFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Stages []ProposedStage `json:"stages"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(resp.Stages))
	}
	// Verify speed and lactate values came through correctly.
	if resp.Stages[0].SpeedKmh != 10.5 {
		t.Errorf("stage 1 speed: want 10.5, got %f", resp.Stages[0].SpeedKmh)
	}
	if resp.Stages[0].LactateMmol != 1.8 {
		t.Errorf("stage 1 lactate: want 1.8, got %f", resp.Stages[0].LactateMmol)
	}
	if resp.Stages[0].HeartRateBpm == 0 {
		t.Error("stage 1 HR should be non-zero")
	}
}

func TestImportFromWorkoutHandler_HappyPath(t *testing.T) {
	db := setupImportTestDB(t)
	wid := insertTestWorkout(t, db, 1, "2026-03-14T09:00:00Z")

	insertTestLap(t, db, wid, 1, 0, 600, 360.0)
	insertTestLap(t, db, wid, 2, 600_000, 300, 3600.0/10.5)
	insertTestLap(t, db, wid, 3, 900_000, 300, 3600.0/11.0)
	insertTestSamples(t, db, wid, []samplePoint{
		{OffsetMs: 870_000, HeartRate: 135},
		{OffsetMs: 1_170_000, HeartRate: 150},
	})

	body := fmt.Sprintf(
		`{"workout_id": %d, "lactate_data": "10.5 1.8\n11.0 2.2", "warmup_duration_min": 10, "stage_duration_min": 5}`,
		wid,
	)
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ImportFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Test Test `json:"test"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Test.ID == 0 {
		t.Error("expected persisted test to have an ID")
	}
	if resp.Test.WorkoutID == nil || *resp.Test.WorkoutID != wid {
		t.Errorf("expected workout_id=%d, got %v", wid, resp.Test.WorkoutID)
	}
	if len(resp.Test.Stages) != 2 {
		t.Errorf("expected 2 stages, got %d", len(resp.Test.Stages))
	}
	if resp.Test.Date != "2026-03-14" {
		t.Errorf("expected date=2026-03-14, got %s", resp.Test.Date)
	}
}

func TestImportFromWorkoutHandler_ParseError(t *testing.T) {
	db := setupImportTestDB(t)
	wid := insertTestWorkout(t, db, 1, "2026-03-14T09:00:00Z")

	body := fmt.Sprintf(`{"workout_id": %d, "lactate_data": "not valid data"}`, wid)
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ImportFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["hint"] == "" {
		t.Error("expected hint field in parse error response")
	}
}

func TestImportFromWorkoutHandler_MissingWorkoutID(t *testing.T) {
	db := setupImportTestDB(t)

	body := `{"lactate_data": "10.5 1.8\n11.0 2.2"}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests/from-workout", strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ImportFromWorkoutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
