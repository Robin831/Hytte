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
