package training

import (
	"database/sql"
	"fmt"
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

// insertWorkoutWithStartedAt inserts a workout with a specific started_at timestamp,
// returning its ID. Used for time-range-sensitive tests like GetWorkoutTypeDistribution.
func insertWorkoutWithStartedAt(t *testing.T, db_ *sql.DB, userID int64, startedAt string) int64 {
	t.Helper()
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, "running", "Test Workout", startedAt, 3600, fmt.Sprintf("hash-%d-%s", userID, startedAt),
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

func TestGetWorkoutTypeDistribution_Empty(t *testing.T) {
	db := setupTestDB(t)

	dist, err := GetWorkoutTypeDistribution(db, 1, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dist) != 0 {
		t.Errorf("expected empty distribution, got %v", dist)
	}
}

func TestGetWorkoutTypeDistribution_CountsAITags(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -3).Format(time.RFC3339)
	wid1 := insertWorkoutWithStartedAt(t, db, 1, recent)
	wid2 := insertWorkoutWithStartedAt(t, db, 1, now.AddDate(0, 0, -1).Format(time.RFC3339))
	wid3 := insertWorkoutWithStartedAt(t, db, 1, now.AddDate(0, 0, -2).Format(time.RFC3339))

	for _, tc := range []struct {
		wid int64
		tag string
	}{
		{wid1, "ai:type:easy"},
		{wid2, "ai:type:easy"},
		{wid3, "ai:type:tempo"},
	} {
		if _, err := db.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (?, ?)`, tc.wid, tc.tag); err != nil {
			t.Fatalf("insert tag: %v", err)
		}
	}

	dist, err := GetWorkoutTypeDistribution(db, 1, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dist["ai:type:easy"] != 2 {
		t.Errorf("expected ai:type:easy=2, got %d", dist["ai:type:easy"])
	}
	if dist["ai:type:tempo"] != 1 {
		t.Errorf("expected ai:type:tempo=1, got %d", dist["ai:type:tempo"])
	}
}

func TestGetWorkoutTypeDistribution_ExcludesAutoTags(t *testing.T) {
	db := setupTestDB(t)

	recent := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	wid := insertWorkoutWithStartedAt(t, db, 1, recent)
	for _, tag := range []string{"auto:treadmill", "user-tag", "ai:threshold", "ai:type:threshold"} {
		if _, err := db.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (?, ?)`, wid, tag); err != nil {
			t.Fatalf("insert tag: %v", err)
		}
	}

	dist, err := GetWorkoutTypeDistribution(db, 1, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dist) != 1 {
		t.Errorf("expected only ai:type:threshold in distribution, got %v", dist)
	}
	if dist["ai:type:threshold"] != 1 {
		t.Errorf("expected ai:type:threshold=1, got %d", dist["ai:type:threshold"])
	}
}

func TestGetWorkoutTypeDistribution_ExcludesOldWorkouts(t *testing.T) {
	db := setupTestDB(t)

	old := time.Now().UTC().AddDate(0, 0, -60).Format(time.RFC3339)
	recent := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	widOld := insertWorkoutWithStartedAt(t, db, 1, old)
	widRecent := insertWorkoutWithStartedAt(t, db, 1, recent)

	for _, tc := range []struct {
		wid int64
		tag string
	}{
		{widOld, "ai:type:easy"},
		{widRecent, "ai:type:tempo"},
	} {
		if _, err := db.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (?, ?)`, tc.wid, tc.tag); err != nil {
			t.Fatalf("insert tag: %v", err)
		}
	}

	// 4-week window excludes the 60-day-old workout.
	dist, err := GetWorkoutTypeDistribution(db, 1, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dist["ai:type:easy"] != 0 {
		t.Errorf("expected ai:type:easy excluded (old workout), got %d", dist["ai:type:easy"])
	}
	if dist["ai:type:tempo"] != 1 {
		t.Errorf("expected ai:type:tempo=1, got %d", dist["ai:type:tempo"])
	}
}

func TestGetWorkoutTypeDistribution_UserScoped(t *testing.T) {
	db := setupTestDB(t)

	// Create a second user.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')`); err != nil {
		t.Fatal(err)
	}

	recent := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	wid1 := insertWorkoutWithStartedAt(t, db, 1, recent)
	wid2 := insertWorkoutWithStartedAt(t, db, 2, recent)
	for _, tc := range []struct {
		wid int64
		tag string
	}{
		{wid1, "ai:type:easy"},
		{wid2, "ai:type:tempo"},
	} {
		if _, err := db.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (?, ?)`, tc.wid, tc.tag); err != nil {
			t.Fatalf("insert tag: %v", err)
		}
	}

	dist1, err := GetWorkoutTypeDistribution(db, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if dist1["ai:type:easy"] != 1 || dist1["ai:type:tempo"] != 0 {
		t.Errorf("user 1 distribution wrong: %v", dist1)
	}

	dist2, err := GetWorkoutTypeDistribution(db, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if dist2["ai:type:tempo"] != 1 || dist2["ai:type:easy"] != 0 {
		t.Errorf("user 2 distribution wrong: %v", dist2)
	}
}

func TestGetWorkoutTypeDistribution_WeeksClampedToMin1(t *testing.T) {
	db := setupTestDB(t)

	// weeks=0 should be treated as weeks=1 (no panic, no error).
	dist, err := GetWorkoutTypeDistribution(db, 1, 0)
	if err != nil {
		t.Fatalf("unexpected error with weeks=0: %v", err)
	}
	if dist == nil {
		t.Error("expected non-nil map even with weeks=0")
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
