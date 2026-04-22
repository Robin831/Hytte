package math

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"time"
)

// timeFormat is the canonical RFC3339 format used by every Hytte module that
// persists timestamps as TEXT.
const timeFormat = time.RFC3339

// ErrInvalidMode is returned by Start when the supplied mode is not one of
// the recognised IsValidMode values.
var ErrInvalidMode = errors.New("invalid math session mode")

// ErrSessionNotOwned is returned when a user tries to record an attempt or
// finish a session that belongs to a different user.
var ErrSessionNotOwned = errors.New("session not owned by user")

// ErrSessionFinished is returned when an attempt is recorded against a
// session that already has ended_at set.
var ErrSessionFinished = errors.New("session already finished")

// ErrSessionNotFound is returned when the session id does not exist.
var ErrSessionNotFound = errors.New("session not found")

// Service exposes the persistent session lifecycle on top of *sql.DB.
type Service struct {
	db *sql.DB
}

// NewService wraps the given DB handle.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// Summary captures the result of finishing a session. ScoreNum's formula
// depends on the mode: for most modes it equals total_correct; for Blitz
// it is the streak/speed-weighted sum computed by ComputeBlitzPoints.
// BestStreak is the longest run of consecutive correct attempts in the
// session — always computed so the result screen can show it without a
// second round-trip.
type Summary struct {
	SessionID    int64  `json:"session_id"`
	Mode         string `json:"mode"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	DurationMs   int64  `json:"duration_ms"`
	TotalCorrect int    `json:"total_correct"`
	TotalWrong   int    `json:"total_wrong"`
	ScoreNum     int    `json:"score_num"`
	BestStreak   int    `json:"best_streak"`
}

// ComputeBlitzPoints returns the points awarded for one correct Blitz
// answer. streakBefore is the number of consecutive correct answers
// immediately preceding this one (0 for the first correct after a wrong
// or the very first attempt). The formula mirrors the client-side display
// logic in the Blitz UI so live score and final stored score agree.
//
//	speed_bonus       = 1.5 if responseMs < 1000
//	                    1.2 if responseMs < 2000
//	                    1.0 otherwise
//	streak_multiplier = min(3.0, 1.0 + streakBefore/10)
//	points            = round(1 * speed_bonus * streak_multiplier)
func ComputeBlitzPoints(responseMs, streakBefore int) int {
	if streakBefore < 0 {
		streakBefore = 0
	}
	var speedBonus float64
	switch {
	case responseMs < 1000:
		speedBonus = 1.5
	case responseMs < 2000:
		speedBonus = 1.2
	default:
		speedBonus = 1.0
	}
	streakMult := 1.0 + float64(streakBefore)/10.0
	if streakMult > 3.0 {
		streakMult = 3.0
	}
	return int(math.Round(speedBonus * streakMult))
}

// Start creates a new math_sessions row, returning its id and the first
// question. Mode is validated against IsValidMode.
func (s *Service) Start(ctx context.Context, userID int64, mode string) (int64, Fact, error) {
	if !IsValidMode(mode) {
		return 0, Fact{}, ErrInvalidMode
	}
	now := time.Now().UTC().Format(timeFormat)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO math_sessions (user_id, mode, started_at) VALUES (?, ?, ?)`,
		userID, mode, now,
	)
	if err != nil {
		return 0, Fact{}, fmt.Errorf("insert math_session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, Fact{}, fmt.Errorf("last insert id: %w", err)
	}
	return id, NextQuestion(mode, nil), nil
}

// RecordAttempt validates the answer, persists a math_attempts row, and
// returns the next question. Returns ErrSessionFinished if the session has
// already been finished or the Blitz deadline has passed, or
// ErrSessionNotOwned if the session belongs to a different user.
func (s *Service) RecordAttempt(ctx context.Context, sessionID, userID int64, a, b int, op string, userAnswer, responseMs int) (bool, int, *Fact, error) {
	owner, mode, startedAt, ended, err := s.loadSession(ctx, sessionID)
	if err != nil {
		return false, 0, nil, err
	}
	if owner != userID {
		return false, 0, nil, ErrSessionNotOwned
	}
	if ended {
		return false, 0, nil, ErrSessionFinished
	}
	if mode == ModeBlitz {
		startedT, parseErr := time.Parse(timeFormat, startedAt)
		if parseErr == nil {
			deadline := startedT.Add(time.Duration(BlitzDurationMs) * time.Millisecond)
			if time.Now().UTC().After(deadline) {
				return false, 0, nil, ErrSessionFinished
			}
		}
	}

	isCorrect, expected, err := Validate(a, b, op, userAnswer)
	if err != nil {
		return false, 0, nil, err
	}

	correctInt := 0
	if isCorrect {
		correctInt = 1
	}
	now := time.Now().UTC().Format(timeFormat)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO math_attempts
			(session_id, user_id, fact_a, fact_b, op, expected_answer, user_answer, is_correct, response_ms, created_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM math_sessions
		WHERE id = ? AND user_id = ? AND (ended_at IS NULL OR ended_at = '')`,
		sessionID, userID, a, b, op, expected, userAnswer, correctInt, responseMs, now,
		sessionID, userID,
	)
	if err != nil {
		return false, 0, nil, fmt.Errorf("insert math_attempt: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, 0, nil, fmt.Errorf("rows affected math_attempt: %w", err)
	}
	if rowsAffected == 0 {
		return false, 0, nil, ErrSessionFinished
	}

	next := NextQuestion(mode, nil)
	return isCorrect, expected, &next, nil
}

// Finish marks the session as completed, computes totals from math_attempts,
// and returns a Summary. ended_at and duration_ms are set only on the first
// call; subsequent calls recompute totals against the latest attempts while
// preserving the original completion timestamps.
func (s *Service) Finish(ctx context.Context, sessionID, userID int64) (Summary, error) {
	var (
		owner       int64
		mode        string
		startedAt   string
		endedAtSQL  sql.NullString
		durationSQL sql.NullInt64
	)
	if err := s.db.QueryRowContext(ctx,
		`SELECT user_id, mode, started_at, ended_at, duration_ms FROM math_sessions WHERE id = ?`,
		sessionID,
	).Scan(&owner, &mode, &startedAt, &endedAtSQL, &durationSQL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Summary{}, ErrSessionNotFound
		}
		return Summary{}, fmt.Errorf("select math_session: %w", err)
	}
	if owner != userID {
		return Summary{}, ErrSessionNotOwned
	}

	// Walk attempts in insertion order so we can compute mode-specific
	// scoring (Blitz weights each correct answer by speed and streak) and
	// the longest consecutive-correct run in one pass.
	rows, err := s.db.QueryContext(ctx,
		`SELECT is_correct, response_ms FROM math_attempts WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("select math_attempts: %w", err)
	}
	defer rows.Close()

	var (
		correctCount int
		wrong        int
		score        int
		bestStreak   int
		curStreak    int
	)
	for rows.Next() {
		var (
			isCorrect  int
			responseMs int
		)
		if err := rows.Scan(&isCorrect, &responseMs); err != nil {
			return Summary{}, fmt.Errorf("scan math_attempt: %w", err)
		}
		if isCorrect == 1 {
			// streakBefore is the streak prior to this answer, which is
			// what the Blitz formula keys on.
			if mode == ModeBlitz {
				score += ComputeBlitzPoints(responseMs, curStreak)
			} else {
				score++
			}
			correctCount++
			curStreak++
			if curStreak > bestStreak {
				bestStreak = curStreak
			}
		} else {
			wrong++
			curStreak = 0
		}
	}
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("iterate math_attempts: %w", err)
	}

	// Preserve ended_at and duration_ms if the session was already finished.
	var endedStr string
	var duration int64
	if endedAtSQL.Valid && endedAtSQL.String != "" {
		endedStr = endedAtSQL.String
		duration = durationSQL.Int64
	} else {
		now := time.Now().UTC()
		endedStr = now.Format(timeFormat)
		startedT, parseErr := time.Parse(timeFormat, startedAt)
		if parseErr != nil {
			// started_at should always be written as RFC3339 by Start, so a parse
			// failure here means either legacy data or a concurrent writer that
			// clobbered the value. Log and record zero duration rather than
			// silently attributing a large duration to clock skew.
			log.Printf("math: parse started_at %q for session %d: %v", startedAt, sessionID, parseErr)
		} else {
			duration = now.Sub(startedT).Milliseconds()
			if duration < 0 {
				duration = 0
			}
		}
	}
	if mode == ModeBlitz && duration > BlitzDurationMs {
		duration = BlitzDurationMs
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE math_sessions
		SET total_correct = ?, total_wrong = ?, score_num = ?,
		    ended_at    = CASE WHEN (ended_at IS NULL OR ended_at = '') THEN ? ELSE ended_at END,
		    duration_ms = CASE WHEN (ended_at IS NULL OR ended_at = '') THEN ? ELSE duration_ms END
		WHERE id = ?`,
		correctCount, wrong, score, endedStr, duration, sessionID,
	); err != nil {
		return Summary{}, fmt.Errorf("update math_session: %w", err)
	}

	return Summary{
		SessionID:    sessionID,
		Mode:         mode,
		StartedAt:    startedAt,
		EndedAt:      endedStr,
		DurationMs:   duration,
		TotalCorrect: correctCount,
		TotalWrong:   wrong,
		ScoreNum:     score,
		BestStreak:   bestStreak,
	}, nil
}

// MarathonBest holds the personal-best marathon run for a user. SessionID
// identifies the row; DurationMs is the primary score (lower is better)
// and TotalWrong is the tiebreaker (fewer is better).
type MarathonBest struct {
	SessionID    int64  `json:"session_id"`
	DurationMs   int64  `json:"duration_ms"`
	TotalWrong   int    `json:"total_wrong"`
	TotalCorrect int    `json:"total_correct"`
	EndedAt      string `json:"ended_at"`
}

// BestMarathon returns the user's fastest completed marathon session, or
// nil if they have not finished one yet. A session counts as "completed"
// when ended_at is set and the attempt count equals MarathonFactCount —
// abandoned or partial runs are excluded.
func (s *Service) BestMarathon(ctx context.Context, userID int64) (*MarathonBest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, duration_ms, total_wrong, total_correct, ended_at
		FROM math_sessions
		WHERE user_id = ?
		  AND mode = ?
		  AND ended_at IS NOT NULL AND ended_at != ''
		  AND (total_correct + total_wrong) = ?
		ORDER BY duration_ms ASC, total_wrong ASC
		LIMIT 1`,
		userID, ModeMarathon, MarathonFactCount,
	)
	var best MarathonBest
	if err := row.Scan(&best.SessionID, &best.DurationMs, &best.TotalWrong, &best.TotalCorrect, &best.EndedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select best marathon: %w", err)
	}
	return &best, nil
}

// BlitzBest holds the personal-best Blitz run for a user. ScoreNum is the
// primary ranking (higher is better); BestStreak and TotalCorrect are
// returned for display only, not used as tiebreakers.
type BlitzBest struct {
	SessionID    int64  `json:"session_id"`
	ScoreNum     int    `json:"score_num"`
	BestStreak   int    `json:"best_streak"`
	TotalCorrect int    `json:"total_correct"`
	TotalWrong   int    `json:"total_wrong"`
	EndedAt      string `json:"ended_at"`
}

// BestBlitz returns the user's highest-scoring finished Blitz session, or
// nil if they have not finished one yet. Best_streak is re-derived from
// math_attempts because math_sessions does not store it as a column.
func (s *Service) BestBlitz(ctx context.Context, userID int64) (*BlitzBest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, score_num, total_correct, total_wrong, ended_at
		FROM math_sessions
		WHERE user_id = ?
		  AND mode = ?
		  AND ended_at IS NOT NULL AND ended_at != ''
		ORDER BY score_num DESC, duration_ms ASC
		LIMIT 1`,
		userID, ModeBlitz,
	)
	var best BlitzBest
	if err := row.Scan(&best.SessionID, &best.ScoreNum, &best.TotalCorrect, &best.TotalWrong, &best.EndedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select best blitz: %w", err)
	}
	// Recover best_streak from the attempts log. Keeping this off the
	// math_sessions row avoids a schema change; the attempt count per
	// session is small (≤ a few hundred) so the cost is negligible.
	streak, err := s.longestCorrectStreak(ctx, best.SessionID)
	if err != nil {
		return nil, err
	}
	best.BestStreak = streak
	return &best, nil
}

// longestCorrectStreak returns the longest run of consecutive correct
// attempts in the given session, ordered by insertion.
func (s *Service) longestCorrectStreak(ctx context.Context, sessionID int64) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT is_correct FROM math_attempts WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return 0, fmt.Errorf("select math_attempts for streak: %w", err)
	}
	defer rows.Close()
	best, cur := 0, 0
	for rows.Next() {
		var isCorrect int
		if err := rows.Scan(&isCorrect); err != nil {
			return 0, fmt.Errorf("scan is_correct: %w", err)
		}
		if isCorrect == 1 {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 0
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate is_correct: %w", err)
	}
	return best, nil
}

// loadSession returns the owner, mode, startedAt and finished flag for a
// session id, or ErrSessionNotFound if no row exists.
func (s *Service) loadSession(ctx context.Context, sessionID int64) (int64, string, string, bool, error) {
	var (
		owner     int64
		mode      string
		startedAt string
		endedAt   sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, mode, started_at, ended_at FROM math_sessions WHERE id = ?`,
		sessionID,
	).Scan(&owner, &mode, &startedAt, &endedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", "", false, ErrSessionNotFound
		}
		return 0, "", "", false, fmt.Errorf("select math_session: %w", err)
	}
	return owner, mode, startedAt, endedAt.Valid && endedAt.String != "", nil
}
