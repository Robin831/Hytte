package training

import (
	"database/sql"
	"testing"
)

func TestUpdateAnalysisStatus(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "statushash1")
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// Initial status should be empty.
	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if w.AnalysisStatus != "" {
		t.Fatalf("expected empty initial status, got %q", w.AnalysisStatus)
	}

	// Set to pending.
	if err := UpdateAnalysisStatus(database, workout.ID, 1, "pending"); err != nil {
		t.Fatalf("set pending: %v", err)
	}
	w, err = GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get after pending: %v", err)
	}
	if w.AnalysisStatus != "pending" {
		t.Fatalf("expected pending, got %q", w.AnalysisStatus)
	}

	// Set to completed.
	if err := UpdateAnalysisStatus(database, workout.ID, 1, "completed"); err != nil {
		t.Fatalf("set completed: %v", err)
	}
	w, err = GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get after completed: %v", err)
	}
	if w.AnalysisStatus != "completed" {
		t.Fatalf("expected completed, got %q", w.AnalysisStatus)
	}

	// Set to failed.
	if err := UpdateAnalysisStatus(database, workout.ID, 1, "failed"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	w, err = GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get after failed: %v", err)
	}
	if w.AnalysisStatus != "failed" {
		t.Fatalf("expected failed, got %q", w.AnalysisStatus)
	}
}

func TestUpdateAnalysisStatus_WrongUser(t *testing.T) {
	database := setupTestDB(t)

	// Create a second user.
	_, err := database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`)
	if err != nil {
		t.Fatalf("create user 2: %v", err)
	}

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "statushash2")
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	// User 2 should not be able to update user 1's workout.
	err = UpdateAnalysisStatus(database, workout.ID, 2, "pending")
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for wrong user, got %v", err)
	}

	// Status should remain unchanged.
	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if w.AnalysisStatus != "" {
		t.Fatalf("expected empty status unchanged, got %q", w.AnalysisStatus)
	}
}

func TestUpdateAnalysisStatus_NonExistentWorkout(t *testing.T) {
	database := setupTestDB(t)

	err := UpdateAnalysisStatus(database, 99999, 1, "pending")
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for nonexistent workout, got %v", err)
	}
}

func TestAnalysisStatusInList(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "statushash3")
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := UpdateAnalysisStatus(database, workout.ID, 1, "pending"); err != nil {
		t.Fatalf("set pending: %v", err)
	}

	workouts, err := List(database, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(workouts) != 1 {
		t.Fatalf("expected 1 workout, got %d", len(workouts))
	}
	if workouts[0].AnalysisStatus != "pending" {
		t.Fatalf("expected pending in list, got %q", workouts[0].AnalysisStatus)
	}
}
