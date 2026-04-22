package math

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// maxBodyBytes caps incoming JSON payloads for the math handlers. All
// request shapes are tiny objects with a handful of integer/string fields,
// so 1 KiB is generous and prevents a caller from exhausting memory by
// streaming a huge body through json.Decoder.
const maxBodyBytes = 1 << 10

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("math: writeJSON encode error: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// StartSessionHandler returns POST /api/math/sessions: body {mode}, response
// {session_id, first_question}.
func StartSessionHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var body struct {
			Mode string `json:"mode"`
		}
		// An empty body is allowed (it means "use ModeMixed"); only reject
		// a body that is present but not valid JSON.
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Mode == "" {
			body.Mode = ModeMixed
		}
		id, first, err := svc.Start(r.Context(), user.ID, body.Mode)
		if err != nil {
			if errors.Is(err, ErrInvalidMode) {
				writeErr(w, http.StatusBadRequest, "invalid mode")
				return
			}
			log.Printf("math: start session: %v", err)
			writeErr(w, http.StatusInternalServerError, "failed to start session")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"session_id":     id,
			"first_question": first,
		})
	}
}

// RecordAttemptHandler returns POST /api/math/sessions/:id/attempts: body
// {a, b, op, user_answer, response_ms}, response {is_correct,
// expected_answer, next_question}. next_question is always present; the
// session continues until the client calls the finish endpoint.
func RecordAttemptHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || sessionID <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid session id")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var body struct {
			A          int    `json:"a"`
			B          int    `json:"b"`
			Op         string `json:"op"`
			UserAnswer int    `json:"user_answer"`
			ResponseMs int    `json:"response_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.ResponseMs < 0 {
			writeErr(w, http.StatusBadRequest, "response_ms must be non-negative")
			return
		}

		isCorrect, expected, next, err := svc.RecordAttempt(
			r.Context(), sessionID, user.ID, body.A, body.B, body.Op, body.UserAnswer, body.ResponseMs,
		)
		if err != nil {
			switch {
			case errors.Is(err, ErrSessionNotFound):
				writeErr(w, http.StatusNotFound, "session not found")
			case errors.Is(err, ErrSessionNotOwned):
				writeErr(w, http.StatusForbidden, "session not owned")
			case errors.Is(err, ErrSessionFinished):
				writeErr(w, http.StatusConflict, "session already finished")
			default:
				// Validation errors (invalid op, out-of-range) come back here as
				// generic errors — surface them as 400 so the client can correct.
				writeErr(w, http.StatusBadRequest, err.Error())
			}
			return
		}

		resp := map[string]any{
			"is_correct":      isCorrect,
			"expected_answer": expected,
			"next_question":   next,
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// FinishSessionHandler returns POST /api/math/sessions/:id/finish, response
// {summary}.
func FinishSessionHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || sessionID <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid session id")
			return
		}
		// Finish takes no body but callers may still send one; cap it so a
		// client can't stream an arbitrary payload at this endpoint.
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		summary, err := svc.Finish(r.Context(), sessionID, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrSessionNotFound):
				writeErr(w, http.StatusNotFound, "session not found")
			case errors.Is(err, ErrSessionNotOwned):
				writeErr(w, http.StatusForbidden, "session not owned")
			default:
				log.Printf("math: finish session: %v", err)
				writeErr(w, http.StatusInternalServerError, "failed to finish session")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
	}
}

// MarathonBestHandler returns GET /api/math/marathon/best: the user's
// fastest completed Marathon run, or {"best": null} if they have not
// finished one yet. The Marathon UI uses this to decide whether to show a
// "New PB!" badge after a finishing run.
func MarathonBestHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		best, err := svc.BestMarathon(r.Context(), user.ID)
		if err != nil {
			log.Printf("math: best marathon: %v", err)
			writeErr(w, http.StatusInternalServerError, "failed to load best marathon")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"best": best})
	}
}

// BlitzBestHandler returns GET /api/math/blitz/best: the user's
// highest-scoring finished Blitz run, or {"best": null} if they have not
// finished one yet. The Blitz UI uses this to decide whether to show a
// "New PB!" badge after a run.
func BlitzBestHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		best, err := svc.BestBlitz(r.Context(), user.ID)
		if err != nil {
			log.Printf("math: best blitz: %v", err)
			writeErr(w, http.StatusInternalServerError, "failed to load best blitz")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"best": best})
	}
}

// statsCell is a single cell in the heatmap response. (A, B) are the
// display coordinates — the row/column of the grid (1..10). For
// multiplication, A and B are the factors. For division, A is the quotient
// and B is the divisor, so the rendered problem is (A*B) ÷ B = A.
//
// AccuracyPct is the accuracy over the Last5 window (0-100). Cells with
// no attempts report AccuracyPct=0 and Level=MasteryUnseen; the frontend
// distinguishes those from 0%-with-attempts using Count.
type statsCell struct {
	A           int            `json:"a"`
	B           int            `json:"b"`
	Op          string         `json:"op"`
	Count       int            `json:"count"`
	Correct     int            `json:"correct_count"`
	AccuracyPct float64        `json:"accuracy_pct"`
	AvgMs       float64        `json:"avg_ms"`
	AvgMsLast5  float64        `json:"avg_ms_last5"`
	Last5       []Last5Attempt `json:"last5"`
	Level       string         `json:"level"`
}

// buildCell converts raw FactStats into the wire-level cell for (a, b, op).
// For division, (a, b) are the cell's display coordinates — the caller is
// responsible for looking up the matching FactStats with the underlying
// fact key (dividend=a*b, divisor=b).
func buildCell(a, b int, op string, stats FactStats) statsCell {
	cell := statsCell{
		A:          a,
		B:          b,
		Op:         op,
		Count:      stats.Count,
		Correct:    stats.CorrectCount,
		AvgMs:      stats.AvgMs,
		AvgMsLast5: stats.AvgMsLast5,
		Last5:      stats.Last5,
		Level:      stats.Level,
	}
	if len(stats.Last5) > 0 {
		correct := 0
		for _, attempt := range stats.Last5 {
			if attempt.Correct {
				correct++
			}
		}
		cell.AccuracyPct = float64(correct) * 100 / float64(len(stats.Last5))
	}
	if cell.Level == "" {
		cell.Level = MasteryUnseen
	}
	if cell.Last5 == nil {
		cell.Last5 = []Last5Attempt{}
	}
	return cell
}

// StatsHandler returns GET /api/math/stats: the user's per-fact mastery as
// two 10×10 grids — one for multiplication, one for division — indexed as
// grid[a-1][b-1]. Every cell is present; cells with zero attempts have
// Count=0 and Level="unseen". The frontend uses Level directly for cell
// colouring and drills into Last5 for the detail panel.
func StatsHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		mastery, err := svc.Mastery(r.Context(), user.ID)
		if err != nil {
			log.Printf("math: mastery: %v", err)
			writeErr(w, http.StatusInternalServerError, "failed to load stats")
			return
		}
		mult := make([][]statsCell, MaxOperand-MinOperand+1)
		div := make([][]statsCell, MaxOperand-MinOperand+1)
		for a := MinOperand; a <= MaxOperand; a++ {
			row := a - MinOperand
			mult[row] = make([]statsCell, MaxOperand-MinOperand+1)
			div[row] = make([]statsCell, MaxOperand-MinOperand+1)
			for b := MinOperand; b <= MaxOperand; b++ {
				col := b - MinOperand
				// Multiplication fact key is (a, b).
				multStats := mastery[FactKey{A: a, B: b, Op: OpMultiply}]
				mult[row][col] = buildCell(a, b, OpMultiply, multStats)
				// Division cell (a, b) represents the problem (a*b) ÷ b = a,
				// stored with fact key (dividend=a*b, divisor=b).
				divStats := mastery[FactKey{A: a * b, B: b, Op: OpDivide}]
				div[row][col] = buildCell(a, b, OpDivide, divStats)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"multiplication": mult,
			"division":       div,
		})
	}
}

// LeaderboardHandler returns GET /api/math/leaderboard?mode=marathon|blitz&period=all|week:
// the family-scoped leaderboard for the given mode and time window. Family
// members with no qualifying run appear with a null score.
func LeaderboardHandler(db *sql.DB) http.HandlerFunc {
	svc := NewService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		mode := r.URL.Query().Get("mode")
		period := r.URL.Query().Get("period")
		if period == "" {
			period = PeriodAll
		}
		lb, err := svc.BuildLeaderboard(r.Context(), user.ID, mode, period)
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidMode):
				writeErr(w, http.StatusBadRequest, "invalid mode")
			case errors.Is(err, ErrInvalidPeriod):
				writeErr(w, http.StatusBadRequest, "invalid period")
			default:
				log.Printf("math: leaderboard: %v", err)
				writeErr(w, http.StatusInternalServerError, "failed to load leaderboard")
			}
			return
		}
		writeJSON(w, http.StatusOK, lb)
	}
}

