package stride

import (
	"context"
	"testing"
)

func TestLibraryWorkoutCRUDAndSeed(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Seed on an empty library inserts the pinned 6x6min reference.
	if err := SeedReferenceWorkout(ctx, db, 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Idempotent: a second call must not duplicate.
	if err := SeedReferenceWorkout(ctx, db, 1); err != nil {
		t.Fatalf("seed twice: %v", err)
	}
	workouts, err := ListLibraryWorkouts(ctx, db, 1, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(workouts) != 1 {
		t.Fatalf("expected 1 seeded workout, got %d", len(workouts))
	}
	ref := workouts[0]
	if !ref.IsReference || ref.WorkoutType != "threshold" {
		t.Errorf("seeded reference wrong shape: %+v", ref)
	}
	if len(ref.Blocks) != 3 {
		t.Errorf("expected 3 blocks on the reference, got %v", ref.Blocks)
	}

	// Create a second workout; pinning it as reference must unpin the seed.
	w := &LibraryWorkout{
		Name:        "Hill sprints",
		WorkoutType: "hard",
		MainSet:     "10x30s uphill hard, walk-back recovery",
		Blocks:      []string{"build", "peak"},
		IsReference: true,
		Source:      "ai",
	}
	if msg := ValidateLibraryWorkout(w); msg != "" {
		t.Fatalf("validate: %s", msg)
	}
	if err := InsertLibraryWorkout(ctx, db, 1, w); err != nil {
		t.Fatalf("insert: %v", err)
	}
	workouts, _ = ListLibraryWorkouts(ctx, db, 1, false)
	refCount := 0
	for _, lw := range workouts {
		if lw.IsReference {
			refCount++
			if lw.ID != w.ID {
				t.Errorf("expected new workout to hold the reference pin, got %d", lw.ID)
			}
		}
	}
	if refCount != 1 {
		t.Errorf("exactly one reference expected, got %d", refCount)
	}

	// Usage recording bumps counters and stamps the plan week; duplicates in
	// one call count once.
	RecordLibraryUsage(ctx, db, 1, []int64{w.ID, w.ID}, "2026-08-17")
	workouts, _ = ListLibraryWorkouts(ctx, db, 1, false)
	for _, lw := range workouts {
		if lw.ID == w.ID {
			if lw.TimesUsed != 1 || lw.LastUsedAt != "2026-08-17" {
				t.Errorf("usage not recorded: %+v", lw)
			}
		}
	}

	// Update: archive hides the row from the default listing.
	w.Archived = true
	w.IsReference = false
	if err := UpdateLibraryWorkout(ctx, db, 1, w); err != nil {
		t.Fatalf("update: %v", err)
	}
	visible, _ := ListLibraryWorkouts(ctx, db, 1, false)
	all, _ := ListLibraryWorkouts(ctx, db, 1, true)
	if len(visible) != 1 || len(all) != 2 {
		t.Errorf("archive filter wrong: visible=%d all=%d", len(visible), len(all))
	}

	// Another user's library is invisible and unwritable.
	if lst, _ := ListLibraryWorkouts(ctx, db, 2, true); len(lst) != 0 {
		t.Errorf("cross-user leak: %d rows", len(lst))
	}
	if err := DeleteLibraryWorkout(ctx, db, 2, w.ID); err == nil {
		t.Error("expected cross-user delete to fail")
	}
	if err := DeleteLibraryWorkout(ctx, db, 1, w.ID); err != nil {
		t.Errorf("owner delete: %v", err)
	}
}

func TestValidateLibraryWorkout(t *testing.T) {
	cases := []struct {
		name string
		w    LibraryWorkout
		ok   bool
	}{
		{"valid", LibraryWorkout{Name: "x", MainSet: "y", WorkoutType: "easy", Blocks: []string{"base"}}, true},
		{"missing name", LibraryWorkout{MainSet: "y"}, false},
		{"missing main set", LibraryWorkout{Name: "x"}, false},
		{"bad type", LibraryWorkout{Name: "x", MainSet: "y", WorkoutType: "sprint"}, false},
		{"bad block", LibraryWorkout{Name: "x", MainSet: "y", Blocks: []string{"offseason"}}, false},
		{"bad rating", LibraryWorkout{Name: "x", MainSet: "y", Rating: 9}, false},
	}
	for _, c := range cases {
		msg := ValidateLibraryWorkout(&c.w)
		if c.ok && msg != "" {
			t.Errorf("%s: unexpected error %q", c.name, msg)
		}
		if !c.ok && msg == "" {
			t.Errorf("%s: expected validation error", c.name)
		}
	}
}
