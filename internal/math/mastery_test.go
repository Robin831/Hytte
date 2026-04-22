package math

import (
	"context"
	"testing"
)

func TestMasteryAggregatesCounts(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Same fact 3×4=12 attempted three times: correct, wrong, correct.
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 1000); err != nil {
		t.Fatalf("attempt 1: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 11, 2000); err != nil {
		t.Fatalf("attempt 2: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 3000); err != nil {
		t.Fatalf("attempt 3: %v", err)
	}
	// One division attempt 12÷4=3 (correct).
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 12, 4, OpDivide, 3, 500); err != nil {
		t.Fatalf("attempt 4: %v", err)
	}

	mastery, err := svc.Mastery(ctx, 1)
	if err != nil {
		t.Fatalf("Mastery: %v", err)
	}
	multStats, ok := mastery[FactKey{A: 3, B: 4, Op: OpMultiply}]
	if !ok {
		t.Fatal("missing 3×4 stats")
	}
	if multStats.Count != 3 {
		t.Errorf("Count=%d, want 3", multStats.Count)
	}
	if multStats.CorrectCount != 2 {
		t.Errorf("CorrectCount=%d, want 2", multStats.CorrectCount)
	}
	wantAvg := float64(1000+2000+3000) / 3.0
	if multStats.AvgMs != wantAvg {
		t.Errorf("AvgMs=%v, want %v", multStats.AvgMs, wantAvg)
	}
	if len(multStats.Last5) != 3 {
		t.Fatalf("Last5 length=%d, want 3", len(multStats.Last5))
	}
	// Order: oldest first → [true, false, true].
	if !(multStats.Last5[0] == true && multStats.Last5[1] == false && multStats.Last5[2] == true) {
		t.Errorf("Last5=%v, want [true false true]", multStats.Last5)
	}

	divStats, ok := mastery[FactKey{A: 12, B: 4, Op: OpDivide}]
	if !ok {
		t.Fatal("missing 12÷4 stats")
	}
	if divStats.Count != 1 || divStats.CorrectCount != 1 {
		t.Errorf("div stats=%+v", divStats)
	}
}

func TestMasteryLast5Window(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeMultiplication)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 7 attempts of 2×2=4 with alternating correctness: T,F,T,F,T,F,T.
	answers := []int{4, 3, 4, 3, 4, 3, 4}
	for _, ans := range answers {
		if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 2, 2, OpMultiply, ans, 100); err != nil {
			t.Fatalf("attempt: %v", err)
		}
	}

	mastery, err := svc.Mastery(ctx, 1)
	if err != nil {
		t.Fatalf("Mastery: %v", err)
	}
	stats := mastery[FactKey{A: 2, B: 2, Op: OpMultiply}]
	if stats.Count != 7 {
		t.Errorf("Count=%d, want 7", stats.Count)
	}
	if len(stats.Last5) != 5 {
		t.Fatalf("Last5 length=%d, want 5", len(stats.Last5))
	}
	// The last 5 attempts (indices 2..6) → answers [4,3,4,3,4] → [T,F,T,F,T].
	want := []bool{true, false, true, false, true}
	for i, w := range want {
		if stats.Last5[i] != w {
			t.Errorf("Last5[%d]=%v, want %v (full=%v)", i, stats.Last5[i], w, stats.Last5)
		}
	}
}

func TestMasteryScopedToUser(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id1, _, err := svc.Start(ctx, 1, ModeMultiplication)
	if err != nil {
		t.Fatalf("Start user 1: %v", err)
	}
	id2, _, err := svc.Start(ctx, 2, ModeMultiplication)
	if err != nil {
		t.Fatalf("Start user 2: %v", err)
	}

	if _, _, _, err := svc.RecordAttempt(ctx, id1, 1, 5, 5, OpMultiply, 25, 100); err != nil {
		t.Fatalf("attempt 1: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id2, 2, 6, 6, OpMultiply, 36, 100); err != nil {
		t.Fatalf("attempt 2: %v", err)
	}

	m1, err := svc.Mastery(ctx, 1)
	if err != nil {
		t.Fatalf("Mastery 1: %v", err)
	}
	if _, ok := m1[FactKey{A: 6, B: 6, Op: OpMultiply}]; ok {
		t.Error("user 1 should not see user 2's facts")
	}
	if _, ok := m1[FactKey{A: 5, B: 5, Op: OpMultiply}]; !ok {
		t.Error("user 1 should see their own fact")
	}

	m2, err := svc.Mastery(ctx, 2)
	if err != nil {
		t.Fatalf("Mastery 2: %v", err)
	}
	if _, ok := m2[FactKey{A: 5, B: 5, Op: OpMultiply}]; ok {
		t.Error("user 2 should not see user 1's facts")
	}
}

func TestMasteryEmpty(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	mastery, err := svc.Mastery(context.Background(), 1)
	if err != nil {
		t.Fatalf("Mastery: %v", err)
	}
	if len(mastery) != 0 {
		t.Errorf("expected empty mastery, got %d entries", len(mastery))
	}
}
