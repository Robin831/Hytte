package stride

import (
	"context"
	"fmt"
	"strings"
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

// TestHistoryReplayBlock pins the fresh-session replay: recent messages are
// rendered, the current turn's message is excluded, and when older messages
// fall outside the bounds the block SAYS so — the silent-truncation failure
// mode (coach confidently wrong about what was said) must be impossible.
func TestHistoryReplayBlock(t *testing.T) {
	db := setupTestDB(t)
	// A plan to hang messages on.
	if _, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, phase, plan_json, model, created_at)
		VALUES (5, 1, '2026-08-17', '2026-08-23', '', '[]', 'm', '2026-08-16T00:00:00Z')`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	var lastID int64
	for i := 0; i < chatReplayMaxMessages+4; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		m, err := AddChatMessage(db, ChatMessage{PlanID: 5, UserID: 1, Role: role, Content: fmt.Sprintf("message number %d", i)})
		if err != nil {
			t.Fatalf("add message %d: %v", i, err)
		}
		lastID = m.ID
	}

	block := historyReplayBlock(db, 5, 1, lastID)
	if block == "" {
		t.Fatal("expected a replay block")
	}
	if strings.Contains(block, fmt.Sprintf("message number %d", chatReplayMaxMessages+3)) {
		t.Error("the excluded current message must not be replayed")
	}
	if !strings.Contains(block, fmt.Sprintf("message number %d", chatReplayMaxMessages+2)) {
		t.Error("the most recent prior message must be replayed")
	}
	// 15 prior messages, 12 replayed → 3 hidden, and the block must say so.
	if !strings.Contains(block, "3 earlier message(s)") {
		t.Errorf("hidden-message disclosure missing:\n%s", block[:200])
	}
	if !strings.Contains(block, "CANNOT see") {
		t.Error("partial-view warning missing")
	}
}
