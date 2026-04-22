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

// Last5Attempt is a single entry in the last-5 window used by the heatmap
// detail panel. Correct is the outcome; ResponseMs is how long the user took
// to answer.
type Last5Attempt struct {
	Correct    bool `json:"correct"`
	ResponseMs int  `json:"response_ms"`
}

// Mastery level constants returned by ClassifyMastery. These classify a
// single fact's current mastery state based on the user's recent attempts
// and are rendered as cell colours on the heatmap.
const (
	MasteryUnseen = "unseen"
	MasteryRed    = "red"
	MasteryYellow = "yellow"
	MasteryGreen  = "green"
)

// Mastery classification thresholds.
const (
	MasteryFastMs    = 2000 // green upper bound: last-5 avg must be strictly less than this.
	MasterySlowMs    = 3000 // yellow upper bound: last-5 avg must be at most this.
	MasteryYellowPct = 80   // yellow lower accuracy bound: last-5 accuracy must be at least this.
)

// FactStats holds the aggregated mastery numbers for a single fact. Last5
// is ordered oldest-first (last element = most recent attempt) so the
// frontend can render a left-to-right correctness streak.
//
// Level is the server-side classification (unseen/red/yellow/green); the
// frontend uses it directly for cell colouring rather than recomputing.
// AvgMsLast5 is the average response time across the last-5 window and is
// what Level is derived from — the overall AvgMs stays available for the
// detail panel.
type FactStats struct {
	Count        int            `json:"count"`
	CorrectCount int            `json:"correct_count"`
	AvgMs        float64        `json:"avg_ms"`
	AvgMsLast5   float64        `json:"avg_ms_last5"`
	Last5        []Last5Attempt `json:"last5"`
	Level        string         `json:"level"`
}

// ClassifyMastery returns the mastery level for the given stats using the
// rules in the bead:
//
//   - unseen: no attempts recorded.
//   - green:  last-5 all correct AND avg response time (over last 5) < 2000ms.
//   - yellow: last-5 accuracy at least 80% AND avg response in [2000, 3000]ms.
//   - red:    everything else (accuracy <80% or avg response outside the green/yellow thresholds).
//
// The function operates on FactStats.Last5 and FactStats.AvgMsLast5 so the
// classification matches what the frontend detail panel shows.
func ClassifyMastery(s FactStats) string {
	if s.Count == 0 || len(s.Last5) == 0 {
		return MasteryUnseen
	}
	// Green: strict — every slot in the window must be correct AND the
	// window must be fully populated (5 attempts). A user with only 3
	// correct attempts is not yet "mastered".
	correctInWindow := 0
	for _, a := range s.Last5 {
		if a.Correct {
			correctInWindow++
		}
	}
	windowSize := len(s.Last5)
	if windowSize >= 5 && correctInWindow == windowSize && s.AvgMsLast5 < MasteryFastMs {
		return MasteryGreen
	}
	accuracyPct := float64(correctInWindow) * 100 / float64(windowSize)
	if accuracyPct >= MasteryYellowPct && s.AvgMsLast5 >= MasteryFastMs && s.AvgMsLast5 <= MasterySlowMs {
		return MasteryYellow
	}
	return MasteryRed
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
		recent       []Last5Attempt
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
		entry.recent = append(entry.recent, Last5Attempt{Correct: isCorrect != 0, ResponseMs: responseMs})
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
		var avgLast5 float64
		if len(e.recent) > 0 {
			var total int64
			for _, a := range e.recent {
				total += int64(a.ResponseMs)
			}
			avgLast5 = float64(total) / float64(len(e.recent))
		}
		stats := FactStats{
			Count:        e.count,
			CorrectCount: e.correctCount,
			AvgMs:        avg,
			AvgMsLast5:   avgLast5,
			Last5:        append([]Last5Attempt(nil), e.recent...),
		}
		stats.Level = ClassifyMastery(stats)
		out[k] = stats
	}
	return out, nil
}
