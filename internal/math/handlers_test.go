package math

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func withChi(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

var testUser = &auth.User{ID: 1, Email: "u@test.com", Name: "U"}

func TestStartSessionHandler(t *testing.T) {
	d := setupTestDB(t)
	h := StartSessionHandler(d)

	r := withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions", map[string]string{"mode": ModeMixed}), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		SessionID     int64 `json:"session_id"`
		FirstQuestion Fact  `json:"first_question"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID == 0 {
		t.Error("expected non-zero session id")
	}
	if resp.FirstQuestion.Op != OpMultiply && resp.FirstQuestion.Op != OpDivide {
		t.Errorf("first question op=%q", resp.FirstQuestion.Op)
	}
}

func TestStartSessionInvalidMode(t *testing.T) {
	d := setupTestDB(t)
	h := StartSessionHandler(d)

	r := withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions", map[string]string{"mode": "nope"}), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRecordAttemptHandlerRoundTrip(t *testing.T) {
	d := setupTestDB(t)

	// Start a session directly so we know the id.
	id, _, err := NewService(d).Start(context.Background(), 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	h := RecordAttemptHandler(d)
	body := map[string]any{
		"a":           3,
		"b":           4,
		"op":          OpMultiply,
		"user_answer": 12,
		"response_ms": 1500,
	}
	r := withChi(withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions/"+strconv.FormatInt(id, 10)+"/attempts", body), testUser), map[string]string{"id": strconv.FormatInt(id, 10)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		IsCorrect      bool  `json:"is_correct"`
		ExpectedAnswer int   `json:"expected_answer"`
		NextQuestion   *Fact `json:"next_question"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.IsCorrect {
		t.Error("expected is_correct=true")
	}
	if resp.ExpectedAnswer != 12 {
		t.Errorf("expected_answer=%d, want 12", resp.ExpectedAnswer)
	}
	if resp.NextQuestion == nil {
		t.Error("expected non-nil next_question")
	}
}

func TestRecordAttemptHandlerForeignSession(t *testing.T) {
	d := setupTestDB(t)
	id, _, err := NewService(d).Start(context.Background(), 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	other := &auth.User{ID: 2, Email: "x@test.com"}

	h := RecordAttemptHandler(d)
	body := map[string]any{"a": 3, "b": 4, "op": OpMultiply, "user_answer": 12, "response_ms": 100}
	r := withChi(withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions/"+strconv.FormatInt(id, 10)+"/attempts", body), other), map[string]string{"id": strconv.FormatInt(id, 10)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestFinishSessionHandler(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	id, _, err := svc.Start(context.Background(), 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(context.Background(), id, 1, 3, 4, OpMultiply, 12, 100); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	h := FinishSessionHandler(d)
	r := withChi(withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions/"+strconv.FormatInt(id, 10)+"/finish", nil), testUser), map[string]string{"id": strconv.FormatInt(id, 10)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Summary Summary `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Summary.SessionID != id {
		t.Errorf("Summary.SessionID=%d, want %d", resp.Summary.SessionID, id)
	}
	if resp.Summary.TotalCorrect != 1 {
		t.Errorf("TotalCorrect=%d, want 1", resp.Summary.TotalCorrect)
	}
}

func TestMarathonBestHandlerEmpty(t *testing.T) {
	d := setupTestDB(t)
	h := MarathonBestHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/marathon/best", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Best *MarathonBest `json:"best"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Best != nil {
		t.Errorf("expected null best, got %+v", resp.Best)
	}
}

func TestMarathonBestHandlerReturnsFastest(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(`INSERT INTO math_sessions
		(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong)
		VALUES (1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z', 245000, 200, 0)`,
		ModeMarathon); err != nil {
		t.Fatalf("insert: %v", err)
	}
	h := MarathonBestHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/marathon/best", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Best *MarathonBest `json:"best"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Best == nil {
		t.Fatal("expected non-nil best")
	}
	if resp.Best.DurationMs != 245000 {
		t.Errorf("DurationMs=%d, want 245000", resp.Best.DurationMs)
	}
}

func TestBlitzBestHandlerEmpty(t *testing.T) {
	d := setupTestDB(t)
	h := BlitzBestHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/blitz/best", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Best *BlitzBest `json:"best"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Best != nil {
		t.Errorf("expected null best, got %+v", resp.Best)
	}
}

func TestBlitzBestHandlerReturnsHighestScore(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	// Finish a small Blitz run so there's something to rank.
	id, _, err := svc.Start(ctx, 1, ModeBlitz)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 500); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 5, 5, OpMultiply, 25, 500); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if _, err := svc.Finish(ctx, id, 1); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := BlitzBestHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/blitz/best", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Best *BlitzBest `json:"best"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Best == nil {
		t.Fatal("expected non-nil best")
	}
	// Two fast correct answers: streak 0 → round(1.5*1.0)=2, streak 1 →
	// round(1.5*1.1)=round(1.65)=2. Total = 4.
	if resp.Best.ScoreNum != 4 {
		t.Errorf("ScoreNum=%d, want 4", resp.Best.ScoreNum)
	}
	if resp.Best.BestStreak != 2 {
		t.Errorf("BestStreak=%d, want 2", resp.Best.BestStreak)
	}
}

func TestStatsHandlerSorted(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	id, _, err := svc.Start(ctx, 1, ModeMixed)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Record attempts in random order.
	pairs := [][2]int{{5, 5}, {2, 3}, {7, 4}}
	for _, p := range pairs {
		if _, _, _, err := svc.RecordAttempt(ctx, id, 1, p[0], p[1], OpMultiply, p[0]*p[1], 100); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	h := StatsHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/stats", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Multiplication []statsEntry `json:"multiplication"`
		Division       []statsEntry `json:"division"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Multiplication) != 3 {
		t.Fatalf("got %d mult entries, want 3", len(resp.Multiplication))
	}
	// Verify ascending (A, B) order.
	for i := 1; i < len(resp.Multiplication); i++ {
		prev, cur := resp.Multiplication[i-1], resp.Multiplication[i]
		if prev.A > cur.A || (prev.A == cur.A && prev.B > cur.B) {
			t.Errorf("entries not sorted: %+v then %+v", prev, cur)
		}
	}
}
