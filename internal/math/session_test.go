package math

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-math-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (1, 'a@example.com', 'A', '', 'g1', '')`); err != nil {
		t.Fatalf("insert user 1: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (2, 'b@example.com', 'B', '', 'g2', '')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
	return database
}

func TestServiceStartRejectsInvalidMode(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	if _, _, err := svc.Start(context.Background(), 1, "bogus"); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("expected ErrInvalidMode, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, first, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero session id")
	}
	if first.Op != OpMultiply && first.Op != OpDivide {
		t.Fatalf("first question has bogus op: %+v", first)
	}

	// Two correct attempts.
	if ok, exp, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 1500); err != nil {
		t.Fatalf("RecordAttempt 1: %v", err)
	} else if !ok || exp != 12 {
		t.Errorf("attempt 1: ok=%v exp=%d", ok, exp)
	}
	if ok, exp, _, err := svc.RecordAttempt(ctx, id, 1, 20, 4, OpDivide, 5, 2000); err != nil {
		t.Fatalf("RecordAttempt 2: %v", err)
	} else if !ok || exp != 5 {
		t.Errorf("attempt 2: ok=%v exp=%d", ok, exp)
	}
	// One wrong attempt.
	if ok, exp, _, err := svc.RecordAttempt(ctx, id, 1, 7, 6, OpMultiply, 41, 3000); err != nil {
		t.Fatalf("RecordAttempt 3: %v", err)
	} else if ok || exp != 42 {
		t.Errorf("attempt 3: ok=%v exp=%d", ok, exp)
	}

	// Validation error from RecordAttempt should not insert a row.
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 99, 99, OpMultiply, 0, 100); err == nil {
		t.Error("expected validation error for out-of-range operands")
	}

	summary, err := svc.Finish(ctx, id, 1)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if summary.SessionID != id {
		t.Errorf("summary.SessionID=%d, want %d", summary.SessionID, id)
	}
	if summary.TotalCorrect != 2 {
		t.Errorf("TotalCorrect=%d, want 2", summary.TotalCorrect)
	}
	if summary.TotalWrong != 1 {
		t.Errorf("TotalWrong=%d, want 1", summary.TotalWrong)
	}
	if summary.ScoreNum != 2 {
		t.Errorf("ScoreNum=%d, want 2", summary.ScoreNum)
	}
	if summary.EndedAt == "" {
		t.Error("EndedAt should be set")
	}

	// Verify row counts in DB.
	var attemptCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM math_attempts WHERE session_id = ?`, id).Scan(&attemptCount); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attemptCount != 3 {
		t.Errorf("attempt rows=%d, want 3", attemptCount)
	}

	// Recording after finish should fail.
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 2, 2, OpMultiply, 4, 100); !errors.Is(err, ErrSessionFinished) {
		t.Errorf("expected ErrSessionFinished, got %v", err)
	}
}

func TestRecordAttemptOwnership(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 2, 3, 4, OpMultiply, 12, 100); !errors.Is(err, ErrSessionNotOwned) {
		t.Errorf("expected ErrSessionNotOwned, got %v", err)
	}
}

func TestFinishOwnership(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := svc.Finish(ctx, id, 2); !errors.Is(err, ErrSessionNotOwned) {
		t.Errorf("expected ErrSessionNotOwned, got %v", err)
	}
}

func TestFinishNotFound(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	if _, err := svc.Finish(context.Background(), 9999, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestBestMarathonNoneRecorded(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	best, err := svc.BestMarathon(context.Background(), 1)
	if err != nil {
		t.Fatalf("BestMarathon: %v", err)
	}
	if best != nil {
		t.Errorf("expected nil best, got %+v", best)
	}
}

func TestBestMarathonPicksFastestThenFewestWrongs(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	// Insert three completed marathon sessions for user 1 with the
	// canonical attempt count, plus a partial session (skipped) and a
	// non-marathon session (skipped).
	insertSession := func(durationMs, correct, wrong int) int64 {
		t.Helper()
		res, err := d.Exec(`INSERT INTO math_sessions
			(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong)
			VALUES (1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z', ?, ?, ?)`,
			ModeMarathon, durationMs, correct, wrong)
		if err != nil {
			t.Fatalf("insert session: %v", err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	insertSession(310000, 199, 1) // 5:10, 1 wrong — slower than the next one
	fastID := insertSession(290500, 200, 0)
	tieID := insertSession(290500, 198, 2) // same duration, more wrongs — should lose tiebreak
	_ = tieID
	// Partial session — only 100 attempts.
	if _, err := d.Exec(`INSERT INTO math_sessions
		(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong)
		VALUES (1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z', 100000, 100, 0)`,
		ModeMarathon); err != nil {
		t.Fatalf("insert partial: %v", err)
	}
	// Non-marathon session — should be ignored even with a faster duration.
	if _, err := d.Exec(`INSERT INTO math_sessions
		(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong)
		VALUES (1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 60000, 200, 0)`,
		ModeMixed); err != nil {
		t.Fatalf("insert mixed: %v", err)
	}

	svc := NewService(d)
	best, err := svc.BestMarathon(ctx, 1)
	if err != nil {
		t.Fatalf("BestMarathon: %v", err)
	}
	if best == nil {
		t.Fatal("expected non-nil best")
	}
	if best.SessionID != fastID {
		t.Errorf("SessionID=%d, want %d", best.SessionID, fastID)
	}
	if best.DurationMs != 290500 {
		t.Errorf("DurationMs=%d, want 290500", best.DurationMs)
	}
	if best.TotalWrong != 0 {
		t.Errorf("TotalWrong=%d, want 0", best.TotalWrong)
	}
}

func TestBestMarathonScopedToUser(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(`INSERT INTO math_sessions
		(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong)
		VALUES (2, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z', 200000, 200, 0)`,
		ModeMarathon); err != nil {
		t.Fatalf("insert: %v", err)
	}
	svc := NewService(d)
	best, err := svc.BestMarathon(context.Background(), 1)
	if err != nil {
		t.Fatalf("BestMarathon: %v", err)
	}
	if best != nil {
		t.Errorf("expected nil best for user 1, got %+v", best)
	}
}

func TestComputeBlitzPoints(t *testing.T) {
	cases := []struct {
		name         string
		responseMs   int
		streakBefore int
		want         int
	}{
		// Speed bonus boundaries (streakBefore=0, so streak_mult = 1.0).
		{"fast under 1s", 500, 0, 2},       // round(1.5 * 1.0) = 2
		{"exactly 1000 is medium", 1000, 0, 1}, // round(1.2 * 1.0) = 1
		{"medium under 2s", 1500, 0, 1},    // round(1.2 * 1.0) = 1
		{"exactly 2000 is slow", 2000, 0, 1}, // round(1.0 * 1.0) = 1
		{"slow over 2s", 3000, 0, 1},       // round(1.0 * 1.0) = 1

		// Streak multiplier steps (responseMs=500, so speed_bonus=1.5).
		{"streak 0 fast", 500, 0, 2},   // round(1.5 * 1.0) = 2
		{"streak 10 fast", 500, 10, 3}, // round(1.5 * 2.0) = 3
		{"streak 20 fast capped at 3.0", 500, 20, 5}, // round(1.5 * 3.0) = 5 (half-away rounds 4.5 → 5)
		{"streak 30 still capped", 500, 30, 5},       // same as 20

		// Streak cap with slow answer.
		{"slow but long streak", 3000, 25, 3}, // round(1.0 * 3.0) = 3
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeBlitzPoints(tc.responseMs, tc.streakBefore)
			if got != tc.want {
				t.Errorf("ComputeBlitzPoints(%d, %d) = %d, want %d",
					tc.responseMs, tc.streakBefore, got, tc.want)
			}
		})
	}
}

func TestFinishBlitzUsesWeightedScoring(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeBlitz)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Sequence: fast-correct (streak 0), fast-correct (streak 1),
	// wrong, medium-correct (streak 0), fast-correct (streak 1).
	steps := []struct {
		a, b, userAnswer, responseMs int
		op                           string
	}{
		{3, 4, 12, 500, OpMultiply},   // correct, streak 0 → round(1.5*1.0)=2
		{5, 6, 30, 700, OpMultiply},   // correct, streak 1 → round(1.5*1.1)=2
		{2, 2, 5, 800, OpMultiply},    // WRONG
		{4, 4, 16, 1500, OpMultiply},  // correct, streak 0 → round(1.2*1.0)=1
		{7, 2, 14, 900, OpMultiply},   // correct, streak 1 → round(1.5*1.1)=2
	}
	for _, s := range steps {
		if _, _, _, err := svc.RecordAttempt(ctx, id, 1, s.a, s.b, s.op, s.userAnswer, s.responseMs); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	summary, err := svc.Finish(ctx, id, 1)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if summary.Mode != ModeBlitz {
		t.Errorf("Mode=%q, want %q", summary.Mode, ModeBlitz)
	}
	if summary.TotalCorrect != 4 {
		t.Errorf("TotalCorrect=%d, want 4", summary.TotalCorrect)
	}
	if summary.TotalWrong != 1 {
		t.Errorf("TotalWrong=%d, want 1", summary.TotalWrong)
	}
	// Expected score = 2 + 2 + 1 + 2 = 7
	if summary.ScoreNum != 7 {
		t.Errorf("ScoreNum=%d, want 7", summary.ScoreNum)
	}
	// Best streak was 2 (first two correct answers before the wrong).
	if summary.BestStreak != 2 {
		t.Errorf("BestStreak=%d, want 2", summary.BestStreak)
	}
}

func TestFinishNonBlitzUsesCorrectCountScore(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Two correct (fast) and one wrong. In Blitz this would score ≥ 4;
	// in other modes it should be exactly total_correct = 2.
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 300); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 5, 5, OpMultiply, 25, 400); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 7, 6, OpMultiply, 41, 900); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	summary, err := svc.Finish(ctx, id, 1)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if summary.ScoreNum != 2 {
		t.Errorf("ScoreNum=%d, want 2 (total_correct) for non-Blitz mode", summary.ScoreNum)
	}
	if summary.BestStreak != 2 {
		t.Errorf("BestStreak=%d, want 2", summary.BestStreak)
	}
}

func TestBestBlitzNoneRecorded(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	best, err := svc.BestBlitz(context.Background(), 1)
	if err != nil {
		t.Fatalf("BestBlitz: %v", err)
	}
	if best != nil {
		t.Errorf("expected nil best, got %+v", best)
	}
}

func TestBestBlitzPicksHighestScore(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	// Run three Blitz sessions via the service so math_attempts is populated
	// (which BestBlitz needs to re-derive best_streak).
	runSession := func(userID int64, steps []struct {
		a, b, userAnswer, responseMs int
		op                           string
	}) int64 {
		t.Helper()
		id, _, err := svc.Start(ctx, userID, ModeBlitz)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		for _, s := range steps {
			if _, _, _, err := svc.RecordAttempt(ctx, id, userID, s.a, s.b, s.op, s.userAnswer, s.responseMs); err != nil {
				t.Fatalf("RecordAttempt: %v", err)
			}
		}
		if _, err := svc.Finish(ctx, id, userID); err != nil {
			t.Fatalf("Finish: %v", err)
		}
		return id
	}

	type step = struct {
		a, b, userAnswer, responseMs int
		op                           string
	}
	// Session A: 3 fast correct answers (streak 0,1,2) → 2 + 2 + 2 = 6.
	runSession(1, []step{
		{3, 4, 12, 400, OpMultiply},
		{5, 5, 25, 400, OpMultiply},
		{6, 6, 36, 400, OpMultiply},
	})
	// Session B (target): 5 fast correct answers → 2+2+2+2+2 = 10.
	targetID := runSession(1, []step{
		{2, 3, 6, 400, OpMultiply},
		{3, 3, 9, 400, OpMultiply},
		{4, 3, 12, 400, OpMultiply},
		{5, 3, 15, 400, OpMultiply},
		{6, 3, 18, 400, OpMultiply},
	})
	// Session C: 2 slow correct answers → 1 + 1 = 2.
	runSession(1, []step{
		{2, 2, 4, 2500, OpMultiply},
		{3, 2, 6, 2500, OpMultiply},
	})

	best, err := svc.BestBlitz(ctx, 1)
	if err != nil {
		t.Fatalf("BestBlitz: %v", err)
	}
	if best == nil {
		t.Fatal("expected non-nil best")
	}
	if best.SessionID != targetID {
		t.Errorf("SessionID=%d, want %d", best.SessionID, targetID)
	}
	if best.ScoreNum != 10 {
		t.Errorf("ScoreNum=%d, want 10", best.ScoreNum)
	}
	if best.BestStreak != 5 {
		t.Errorf("BestStreak=%d, want 5", best.BestStreak)
	}
}

func TestBestBlitzScopedToUser(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	// User 2 posts a finished Blitz run with a nonzero score.
	id, _, err := svc.Start(ctx, 2, ModeBlitz)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 2, 3, 4, OpMultiply, 12, 500); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if _, err := svc.Finish(ctx, id, 2); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	best, err := svc.BestBlitz(ctx, 1)
	if err != nil {
		t.Fatalf("BestBlitz: %v", err)
	}
	if best != nil {
		t.Errorf("expected nil best for user 1, got %+v", best)
	}
}

func TestFinishWithNoAttempts(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeDivision)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	summary, err := svc.Finish(ctx, id, 1)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if summary.TotalCorrect != 0 || summary.TotalWrong != 0 || summary.ScoreNum != 0 {
		t.Errorf("expected zero totals, got %+v", summary)
	}
}
