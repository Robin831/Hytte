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

// statsEntry is the per-fact payload in the stats response. Operands are
// repeated alongside FactStats so the client can render without rebuilding
// the key.
type statsEntry struct {
	A int `json:"a"`
	B int `json:"b"`
	FactStats
}

// StatsHandler returns GET /api/math/stats: the user's per-fact mastery for
// both ops, sorted (a, b) ascending for a stable client-side render.
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
		mult := make([]statsEntry, 0)
		div := make([]statsEntry, 0)
		for k, v := range mastery {
			entry := statsEntry{A: k.A, B: k.B, FactStats: v}
			switch k.Op {
			case OpMultiply:
				mult = append(mult, entry)
			case OpDivide:
				div = append(div, entry)
			}
		}
		sortStats(mult)
		sortStats(div)
		writeJSON(w, http.StatusOK, map[string]any{
			"multiplication": mult,
			"division":       div,
		})
	}
}

// sortStats orders entries by (A, B) ascending. We sort in-place using a
// simple comparator-based sort rather than pulling in sort.Slice's reflection.
func sortStats(entries []statsEntry) {
	// Insertion sort — slices are at most 100 long, and this avoids a
	// reflection-based sort.Slice call.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			if statsLess(entries[j], entries[j-1]) {
				entries[j], entries[j-1] = entries[j-1], entries[j]
				continue
			}
			break
		}
	}
}

func statsLess(a, b statsEntry) bool {
	if a.A != b.A {
		return a.A < b.A
	}
	return a.B < b.B
}
