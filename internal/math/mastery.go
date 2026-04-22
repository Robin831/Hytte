package math

import (
	"context"
	"fmt"
)

// FactKey identifies one of the 200 facts (a, b, op).
type FactKey struct {
	A  int    `json:"a"`
	B  int    `json:"b"`
	Op string `json:"op"`
}

// FactStats holds the aggregated mastery numbers for a single fact.
// Last5 is ordered oldest-first (last element = most recent attempt) so
// the frontend can render a left-to-right correctness streak.
type FactStats struct {
	Count        int     `json:"count"`
	CorrectCount int     `json:"correct_count"`
	AvgMs        float64 `json:"avg_ms"`
	Last5        []bool  `json:"last5"`
}

// Mastery returns one FactStats per fact the user has ever attempted, keyed
// by (a, b, op). Facts the user has not yet attempted are omitted from the
// map — callers can fill in zero-stats by enumerating AllFacts on the side
// when they need to render an exhaustive grid.
func (s *Service) Mastery(ctx context.Context, userID int64) (map[FactKey]FactStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fact_a, fact_b, op, is_correct, response_ms
		FROM math_attempts
		WHERE user_id = ?
		ORDER BY id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query math_attempts: %w", err)
	}
	defer rows.Close()

	type acc struct {
		count        int
		correctCount int
		totalMs      int64
		recent       []bool
	}
	bucket := make(map[FactKey]*acc)

	for rows.Next() {
		var (
			a, b, isCorrect, responseMs int
			op                          string
		)
		if err := rows.Scan(&a, &b, &op, &isCorrect, &responseMs); err != nil {
			return nil, fmt.Errorf("scan math_attempts: %w", err)
		}
		k := FactKey{A: a, B: b, Op: op}
		entry, ok := bucket[k]
		if !ok {
			entry = &acc{}
			bucket[k] = entry
		}
		entry.count++
		if isCorrect != 0 {
			entry.correctCount++
		}
		entry.totalMs += int64(responseMs)
		entry.recent = append(entry.recent, isCorrect != 0)
		if len(entry.recent) > 5 {
			entry.recent = entry.recent[len(entry.recent)-5:]
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate math_attempts: %w", err)
	}

	out := make(map[FactKey]FactStats, len(bucket))
	for k, e := range bucket {
		var avg float64
		if e.count > 0 {
			avg = float64(e.totalMs) / float64(e.count)
		}
		out[k] = FactStats{
			Count:        e.count,
			CorrectCount: e.correctCount,
			AvgMs:        avg,
			Last5:        append([]bool(nil), e.recent...),
		}
	}
	return out, nil
}
