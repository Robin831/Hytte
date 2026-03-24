package training

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestMetricsBackfillHandler_NoWorkouts(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/metrics/backfill", nil), 1)
	w := httptest.NewRecorder()
	MetricsBackfillHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated, ok := resp["updated"].(float64)
	if !ok {
		t.Fatalf("expected updated field, got %v", resp)
	}
	if updated != 0 {
		t.Errorf("expected 0 updated, got %v", updated)
	}
}

func TestMetricsBackfillHandler_UpdatesWorkoutsWithNullMetrics(t *testing.T) {
	db := setupTestDB(t)

	// Insert two workouts without metrics (distinct hashes to satisfy UNIQUE constraint).
	// Include avg/max HR so ComputeTrainingLoad returns a non-nil value.
	insertWorkoutWithHash := func(hash string) int64 {
		t.Helper()
		res, err := db.Exec(
			`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds,
			 distance_meters, avg_heart_rate, max_heart_rate, fit_file_hash)
			 VALUES (1, 'running', 'Backfill Workout', '2025-01-01T10:00:00Z', 3600, 10000, 150, 180, ?)`,
			hash,
		)
		if err != nil {
			t.Fatalf("insert workout %s: %v", hash, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}
		return id
	}
	id1 := insertWorkoutWithHash("backfillhash1")
	id2 := insertWorkoutWithHash("backfillhash2")

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/metrics/backfill", nil), 1)
	w := httptest.NewRecorder()
	MetricsBackfillHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated, ok := resp["updated"].(float64)
	if !ok {
		t.Fatalf("expected updated field, got %v", resp)
	}
	if updated != 2 {
		t.Errorf("expected 2 updated, got %v", updated)
	}

	// Verify training_load was written for both workouts.
	w1, err := GetByID(db, id1, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w1.TrainingLoad == nil {
		t.Error("expected TrainingLoad set after backfill for workout 1")
	}

	w2, err := GetByID(db, id2, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w2.TrainingLoad == nil {
		t.Error("expected TrainingLoad set after backfill for workout 2")
	}
}

func TestMetricsBackfillHandler_SkipsAlreadyComputedWorkouts(t *testing.T) {
	db := setupTestDB(t)

	id := insertMinimalWorkout(t, db, 1)

	// Pre-set training_load.
	existingTL := 42.0
	if err := UpdateMetrics(db, id, 1, &existingTL, nil, nil); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/metrics/backfill", nil), 1)
	w := httptest.NewRecorder()
	MetricsBackfillHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated, ok := resp["updated"].(float64)
	if !ok {
		t.Fatalf("expected numeric 'updated' field, got %T (%v) in response %v", resp["updated"], resp["updated"], resp)
	}
	if updated != 0 {
		t.Errorf("expected 0 updated (workout already has metrics), got %v", updated)
	}

	// Verify the existing value is unchanged.
	workout, err := GetByID(db, id, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if workout.TrainingLoad == nil || *workout.TrainingLoad != existingTL {
		t.Errorf("TrainingLoad changed: want %v, got %v", existingTL, workout.TrainingLoad)
	}
}

func TestMetricsBackfillHandler_RespectsMaxHRPreference(t *testing.T) {
	db := setupTestDB(t)

	// Insert workout with avg_heart_rate and max_heart_rate.
	_, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds,
		 distance_meters, avg_heart_rate, max_heart_rate, fit_file_hash)
		 VALUES (1, 'running', 'Pref HR Workout', '2025-01-01T10:00:00Z', 3600, 10000, 150, 175, 'prefhrhash')`,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	var workoutID int64
	if err := db.QueryRow(`SELECT id FROM workouts WHERE fit_file_hash = 'prefhrhash'`).Scan(&workoutID); err != nil {
		t.Fatalf("query workout id: %v", err)
	}

	// Set max_hr preference to override device-reported max HR.
	if err := auth.SetPreference(db, 1, "max_hr", "190"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/metrics/backfill", nil), 1)
	w := httptest.NewRecorder()
	MetricsBackfillHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	workout, err := GetByID(db, workoutID, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if workout.TrainingLoad == nil {
		t.Fatal("expected TrainingLoad set after backfill with max_hr pref")
	}

	// Compute expected value using the pref max_hr (190), not device max_hr (175).
	durationMin := float64(3600) / 60.0
	expectedTL := ComputeTrainingLoad(durationMin, 150, 190)
	if expectedTL == nil {
		t.Fatal("expected non-nil training load from ComputeTrainingLoad")
	}
	if *workout.TrainingLoad != *expectedTL {
		t.Errorf("TrainingLoad: want %v (using max_hr pref 190), got %v", *expectedTL, *workout.TrainingLoad)
	}
}

func TestMetricsBackfillHandler_OnlyUpdatesCurrentUser(t *testing.T) {
	db := setupTestDB(t)

	// Insert a second user.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	// Workout for user 1 (no metrics).
	insertMinimalWorkout(t, db, 1)

	// Workout for user 2 (no metrics) — should not be touched.
	_, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (2, 'cycling', 'Other User Workout', '2025-01-01T10:00:00Z', 1800, 5000, 'otheruserhash')`,
	)
	if err != nil {
		t.Fatalf("insert user2 workout: %v", err)
	}

	// Run backfill as user 1.
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/metrics/backfill", nil), 1)
	w := httptest.NewRecorder()
	MetricsBackfillHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated, ok := resp["updated"].(float64)
	if !ok {
		t.Fatalf("expected numeric 'updated' field, got %v", resp["updated"])
	}
	if updated != 1 {
		t.Errorf("expected 1 updated (only user 1's workout), got %v", updated)
	}

	// Verify user 2's workout is still missing training_load.
	var tl *float64
	if err := db.QueryRow(
		`SELECT training_load FROM workouts WHERE fit_file_hash = 'otheruserhash'`,
	).Scan(&tl); err != nil {
		t.Fatalf("scan user2 workout: %v", err)
	}
	if tl != nil {
		t.Errorf("expected user 2 workout still nil training_load, got %v", tl)
	}
}

// TestUploadMetrics_ComputeAndPersist verifies that metrics are computed from a
// created workout's samples and can be stored + retrieved correctly — covering the
// same code path triggered during FIT file upload.
func TestUploadMetrics_ComputeAndPersist(t *testing.T) {
	db := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		MaxHeartRate:    180,
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: 130, SpeedMPerS: 2.5},
			{OffsetMs: 900000, HeartRate: 145, SpeedMPerS: 2.6},
			{OffsetMs: 1800000, HeartRate: 155, SpeedMPerS: 2.7},
			{OffsetMs: 2700000, HeartRate: 165, SpeedMPerS: 2.8},
			{OffsetMs: 3600000, HeartRate: 170, SpeedMPerS: 2.5},
		},
	}

	workout, err := Create(db, 1, pw, "uploadmetricshash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate what UploadHandler does: compute and persist metrics.
	maxHR := workout.MaxHeartRate
	durationMin := float64(workout.DurationSeconds) / 60.0
	tl := ComputeTrainingLoad(durationMin, workout.AvgHeartRate, maxHR)

	var hrDrift, paceCV *float64
	if workout.Samples != nil {
		hrDrift = ComputeHRDrift(workout.Samples.Points, workout.DurationSeconds)
		paceCV = ComputePaceCV(workout.Samples.Points)
	}

	if err := UpdateMetrics(db, workout.ID, 1, tl, hrDrift, paceCV); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}

	// Retrieve and verify.
	updated, err := GetByID(db, workout.ID, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if updated.TrainingLoad == nil {
		t.Fatal("expected TrainingLoad set after upload metrics computation")
	}
	if *updated.TrainingLoad != *tl {
		t.Errorf("TrainingLoad: want %v, got %v", *tl, *updated.TrainingLoad)
	}
}

// TestUploadMetrics_MaxHRPreferenceOverride verifies that the user's max_hr pref
// overrides the device-reported value when computing training load on upload.
func TestUploadMetrics_MaxHRPreferenceOverride(t *testing.T) {
	db := setupTestDB(t)

	if err := auth.SetPreference(db, 1, "max_hr", "195"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    160,
		MaxHeartRate:    175, // device value — should be overridden by pref
	}

	workout, err := Create(db, 1, pw, "uploadmaxhrprefhash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate pref lookup + override (as UploadHandler does).
	prefs, err := auth.GetPreferences(db, 1)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	maxHR := workout.MaxHeartRate
	if maxHRStr, ok := prefs["max_hr"]; ok {
		if parsed, parseErr := strconv.Atoi(maxHRStr); parseErr == nil && parsed > 0 {
			maxHR = parsed
		}
	}

	durationMin := float64(workout.DurationSeconds) / 60.0
	tlWithPref := ComputeTrainingLoad(durationMin, workout.AvgHeartRate, maxHR)
	tlWithDevice := ComputeTrainingLoad(durationMin, workout.AvgHeartRate, workout.MaxHeartRate)

	if tlWithPref == nil || tlWithDevice == nil {
		t.Fatal("expected non-nil training load")
	}
	if *tlWithPref == *tlWithDevice {
		t.Error("expected different training load values when max_hr pref overrides device value")
	}

	// Persist and verify the pref-based value is stored.
	if err := UpdateMetrics(db, workout.ID, 1, tlWithPref, nil, nil); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}
	w, err := GetByID(db, workout.ID, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w.TrainingLoad == nil || *w.TrainingLoad != *tlWithPref {
		t.Errorf("expected TrainingLoad %v, got %v", *tlWithPref, w.TrainingLoad)
	}
}

