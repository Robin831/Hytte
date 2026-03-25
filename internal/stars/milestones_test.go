package stars

import (
	"context"
	"testing"
	"time"
)

func TestDistanceMilestone_FirstKilometer(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	workoutID := insertWorkout(t, db, childID, 1800, 1500, 0, 0, 0)

	awards, err := checkDistanceMilestones(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 1500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range awards {
		if a.Reason == "first_kilometer" {
			found = true
			if a.Amount != 5 {
				t.Errorf("first_kilometer amount = %d, want 5", a.Amount)
			}
		}
	}
	if !found {
		t.Error("expected first_kilometer award for 1.5km workout")
	}
}

func TestDistanceMilestone_5KFinisher(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	workoutID := insertWorkout(t, db, childID, 1800, 5100, 0, 0, 0)

	awards, err := checkDistanceMilestones(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 5100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reasons := map[string]bool{}
	for _, a := range awards {
		reasons[a.Reason] = true
	}
	if !reasons["5k_finisher"] {
		t.Error("expected 5k_finisher")
	}
	if !reasons["first_kilometer"] {
		t.Error("expected first_kilometer alongside 5k_finisher")
	}
}

func TestDistanceMilestone_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Simulate first_kilometer already awarded.
	_, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (?, 5, 'first_kilometer', 'Test', NULL, ?)
	`, childID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert prior transaction: %v", err)
	}

	workoutID := insertWorkout(t, db, childID, 1800, 2000, 0, 0, 0)

	awards, err := checkDistanceMilestones(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 2000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range awards {
		if a.Reason == "first_kilometer" {
			t.Error("first_kilometer should NOT be awarded twice")
		}
	}
}

func TestCumulativeMilestone_CenturyClub(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Pre-existing workouts totaling 95km.
	for range 19 {
		insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)
	}

	// The new workout pushes total over 100km.
	workoutID := insertWorkout(t, db, childID, 1800, 6000, 0, 0, 0)

	awards, err := checkCumulativeMilestones(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 6000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range awards {
		if a.Reason == "century_club" {
			found = true
			if a.Amount != 25 {
				t.Errorf("century_club amount = %d, want 25", a.Amount)
			}
		}
	}
	if !found {
		t.Error("expected century_club award when crossing 100km")
	}
}

func TestCumulativeMilestone_NotYetCrossed(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	workoutID := insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)

	awards, err := checkCumulativeMilestones(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 5000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range awards {
		if a.Reason == "century_club" {
			t.Error("century_club should NOT fire for only 5km total")
		}
	}
}

func TestPersonalRecord_LongestRun(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Insert a previous shorter workout.
	insertWorkout(t, db, childID, 1800, 3000, 0, 0, 0)

	// New workout is longer.
	workoutID := insertWorkout(t, db, childID, 3600, 5000, 0, 0, 0)

	awards, err := checkPersonalRecords(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 5000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range awards {
		if a.Reason == "pr_longest_run" {
			found = true
		}
	}
	if !found {
		t.Error("expected pr_longest_run for new personal best distance")
	}
}

func TestPersonalRecord_NotPR(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Previous workout was longer.
	insertWorkout(t, db, childID, 3600, 10000, 0, 0, 0)

	workoutID := insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)

	awards, err := checkPersonalRecords(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 5000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range awards {
		if a.Reason == "pr_longest_run" {
			t.Error("pr_longest_run should NOT fire when not a PR")
		}
	}
}

func TestPersonalRecord_FirstWorkout(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// This is the user's first workout.
	workoutID := insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)

	awards, err := checkPersonalRecords(context.Background(), db, childID, WorkoutInput{
		ID:             workoutID,
		DistanceMeters: 5000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range awards {
		if a.Reason == "pr_longest_run" {
			found = true
		}
	}
	if !found {
		t.Error("expected pr_longest_run for first workout")
	}
}

// TestMilestoneOnlyOnce verifies a milestone doesn't fire twice via full EvaluateWorkout flow.
func TestMilestoneOnlyOnce(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// First workout >= 5km.
	w1ID := insertWorkout(t, db, childID, 1800, 5500, 0, 0, 0)
	_, err := EvaluateWorkout(context.Background(), db, childID, WorkoutInput{
		ID: w1ID, DurationSeconds: 1800, DistanceMeters: 5500,
	})
	if err != nil {
		t.Fatalf("first evaluate: %v", err)
	}

	// Count 5k_finisher awards after first workout.
	var count1 int
	db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = '5k_finisher'`, childID).Scan(&count1)
	if count1 != 1 {
		t.Errorf("expected 1 5k_finisher after first 5k workout, got %d", count1)
	}

	// Second workout >= 5km.
	w2ID := insertWorkout(t, db, childID, 1800, 6000, 0, 0, 0)
	_, err = EvaluateWorkout(context.Background(), db, childID, WorkoutInput{
		ID: w2ID, DurationSeconds: 1800, DistanceMeters: 6000,
	})
	if err != nil {
		t.Fatalf("second evaluate: %v", err)
	}

	var count2 int
	db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = '5k_finisher'`, childID).Scan(&count2)
	if count2 != 1 {
		t.Errorf("5k_finisher should only be awarded once, got %d", count2)
	}
}

