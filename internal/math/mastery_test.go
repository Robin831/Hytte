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
	if !(multStats.Last5[0].Correct == true && multStats.Last5[1].Correct == false && multStats.Last5[2].Correct == true) {
		t.Errorf("Last5=%v, want correctness [true false true]", multStats.Last5)
	}
	// Response times should round-trip intact in the last-5 window.
	if multStats.Last5[0].ResponseMs != 1000 || multStats.Last5[2].ResponseMs != 3000 {
		t.Errorf("Last5 response_ms=%v, want [1000,2000,3000]", multStats.Last5)
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
		if stats.Last5[i].Correct != w {
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

func TestClassifyMasteryLevels(t *testing.T) {
	// Helper to build FactStats from a compact list of (correct, ms) pairs.
	build := func(attempts [][2]int) FactStats {
		last5 := make([]Last5Attempt, 0, len(attempts))
		var total int64
		correct := 0
		for _, a := range attempts {
			last5 = append(last5, Last5Attempt{Correct: a[0] == 1, ResponseMs: a[1]})
			if a[0] == 1 {
				correct++
			}
			total += int64(a[1])
		}
		avg := 0.0
		if len(attempts) > 0 {
			avg = float64(total) / float64(len(attempts))
		}
		return FactStats{
			Count:        len(attempts),
			CorrectCount: correct,
			AvgMs:        avg,
			AvgMsLast5:   avg,
			Last5:        last5,
		}
	}

	tests := []struct {
		name     string
		attempts [][2]int
		want     string
	}{
		{"unseen when no attempts", nil, MasteryUnseen},
		{
			"green: 5 correct and fast (<2000ms)",
			[][2]int{{1, 1500}, {1, 1500}, {1, 1500}, {1, 1500}, {1, 1500}},
			MasteryGreen,
		},
		{
			// Boundary: avg exactly 2000ms disqualifies green (strict <).
			"not green when avg hits 2000ms exactly",
			[][2]int{{1, 2000}, {1, 2000}, {1, 2000}, {1, 2000}, {1, 2000}},
			MasteryYellow,
		},
		{
			// Only 4 correct attempts — can't be green even if all right and fast.
			"not green with fewer than 5 attempts",
			[][2]int{{1, 500}, {1, 500}, {1, 500}, {1, 500}},
			MasteryRed,
		},
		{
			// 4/5 correct (80%) at 2500ms avg → yellow.
			"yellow at 80% accuracy and mid-range speed",
			[][2]int{{0, 2500}, {1, 2500}, {1, 2500}, {1, 2500}, {1, 2500}},
			MasteryYellow,
		},
		{
			// avg exactly 3000ms → still yellow (inclusive upper bound).
			"yellow at 3000ms avg upper bound",
			[][2]int{{1, 3000}, {1, 3000}, {1, 3000}, {1, 3000}, {1, 3000}},
			MasteryYellow,
		},
		{
			// avg > 3000ms → red regardless of perfect accuracy.
			"red when avg exceeds 3000ms",
			[][2]int{{1, 3100}, {1, 3100}, {1, 3100}, {1, 3100}, {1, 3100}},
			MasteryRed,
		},
		{
			// 3/5 correct (60%) → red even at a fast pace.
			"red when accuracy below 80%",
			[][2]int{{0, 800}, {0, 800}, {1, 800}, {1, 800}, {1, 800}},
			MasteryRed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyMastery(build(tc.attempts))
			if got != tc.want {
				t.Errorf("ClassifyMastery=%q, want %q", got, tc.want)
			}
		})
	}
}
