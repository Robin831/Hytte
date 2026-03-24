package training

import (
	"database/sql"
	"testing"
	"time"
)

// insertMinimalWorkout inserts a bare-bones workout for metrics tests, returning its ID.
func insertMinimalWorkout(t *testing.T, db_ *sql.DB, userID int64) int64 {
	t.Helper()
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, "running", "Test Workout", time.Now().UTC().Format(time.RFC3339), 3600, "metricshash1",
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func TestWorkoutMetrics_NewWorkoutHasNilFields(t *testing.T) {
	db := setupTestDB(t)
	id := insertMinimalWorkout(t, db, 1)

	w, err := GetByID(db, id, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w.TrainingLoad != nil {
		t.Errorf("expected TrainingLoad nil, got %v", w.TrainingLoad)
	}
	if w.HRDriftPct != nil {
		t.Errorf("expected HRDriftPct nil, got %v", w.HRDriftPct)
	}
	if w.PaceCVPct != nil {
		t.Errorf("expected PaceCVPct nil, got %v", w.PaceCVPct)
	}
}

func TestWorkoutMetrics_UpdateAndRetrieveViaGetByID(t *testing.T) {
	db := setupTestDB(t)
	id := insertMinimalWorkout(t, db, 1)

	tl := 85.5
	hr := 3.2
	pc := 1.7
	if err := UpdateMetrics(db, id, 1, &tl, &hr, &pc); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}

	w, err := GetByID(db, id, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w.TrainingLoad == nil || *w.TrainingLoad != tl {
		t.Errorf("TrainingLoad: want %v, got %v", tl, w.TrainingLoad)
	}
	if w.HRDriftPct == nil || *w.HRDriftPct != hr {
		t.Errorf("HRDriftPct: want %v, got %v", hr, w.HRDriftPct)
	}
	if w.PaceCVPct == nil || *w.PaceCVPct != pc {
		t.Errorf("PaceCVPct: want %v, got %v", pc, w.PaceCVPct)
	}
}

func TestWorkoutMetrics_UpdateAndRetrieveViaList(t *testing.T) {
	db := setupTestDB(t)
	id := insertMinimalWorkout(t, db, 1)

	tl := 100.0
	if err := UpdateMetrics(db, id, 1, &tl, nil, nil); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}

	workouts, err := List(db, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *Workout
	for i := range workouts {
		if workouts[i].ID == id {
			found = &workouts[i]
			break
		}
	}
	if found == nil {
		t.Fatal("workout not found in List")
	}
	if found.TrainingLoad == nil || *found.TrainingLoad != tl {
		t.Errorf("TrainingLoad: want %v, got %v", tl, found.TrainingLoad)
	}
	if found.HRDriftPct != nil {
		t.Errorf("expected HRDriftPct nil, got %v", found.HRDriftPct)
	}
	if found.PaceCVPct != nil {
		t.Errorf("expected PaceCVPct nil, got %v", found.PaceCVPct)
	}
}

func TestWorkoutMetrics_UpdateMetrics_WrongUser(t *testing.T) {
	db := setupTestDB(t)
	id := insertMinimalWorkout(t, db, 1)

	tl := 50.0
	err := UpdateMetrics(db, id, 999, &tl, nil, nil)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows for wrong user, got %v", err)
	}
}

func TestWorkoutMetrics_ClearFields(t *testing.T) {
	db := setupTestDB(t)
	id := insertMinimalWorkout(t, db, 1)

	tl := 75.0
	hr := 2.5
	pc := 1.1
	if err := UpdateMetrics(db, id, 1, &tl, &hr, &pc); err != nil {
		t.Fatalf("UpdateMetrics (set): %v", err)
	}

	// Clear all fields by passing nil.
	if err := UpdateMetrics(db, id, 1, nil, nil, nil); err != nil {
		t.Fatalf("UpdateMetrics (clear): %v", err)
	}

	w, err := GetByID(db, id, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if w.TrainingLoad != nil {
		t.Errorf("expected TrainingLoad nil after clear, got %v", w.TrainingLoad)
	}
	if w.HRDriftPct != nil {
		t.Errorf("expected HRDriftPct nil after clear, got %v", w.HRDriftPct)
	}
	if w.PaceCVPct != nil {
		t.Errorf("expected PaceCVPct nil after clear, got %v", w.PaceCVPct)
	}
}
