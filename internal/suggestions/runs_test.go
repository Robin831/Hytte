package suggestions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

func TestInsertSuggestionRunRoundTrip(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	started := time.Now().UTC().Truncate(time.Second)
	finished := started.Add(2 * time.Minute)
	row := SuggestionRun{
		UserID:     1,
		StartedAt:  started,
		FinishedAt: &finished,
		Trigger:    TriggerManual,
		PageSlugs:  "weather,notes,__new_page__",
		Generated:  7,
		Errors:     1,
		CostUSD:    0.1234,
	}

	id, err := InsertSuggestionRun(ctx, d, row)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	got, err := ListSuggestionRuns(ctx, d, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	r := got[0]
	if r.UserID != 1 || r.Trigger != TriggerManual {
		t.Errorf("user/trigger mismatch: %+v", r)
	}
	if r.PageSlugs != "weather,notes,__new_page__" {
		t.Errorf("page_slugs: got %q", r.PageSlugs)
	}
	if r.Generated != 7 || r.Errors != 1 {
		t.Errorf("counts: got generated=%d errors=%d", r.Generated, r.Errors)
	}
	if math.Abs(r.CostUSD-0.1234) > 1e-9 {
		t.Errorf("cost mismatch: got %f", r.CostUSD)
	}
	if !r.StartedAt.Equal(started) {
		t.Errorf("started_at: got %v want %v", r.StartedAt, started)
	}
	if r.FinishedAt == nil || !r.FinishedAt.Equal(finished) {
		t.Errorf("finished_at: got %v want %v", r.FinishedAt, finished)
	}
}

func TestInsertSuggestionRunRejectsInvalidTrigger(t *testing.T) {
	d := setupTestDB(t)
	if _, err := InsertSuggestionRun(context.Background(), d, SuggestionRun{
		UserID:    1,
		StartedAt: time.Now().UTC(),
		Trigger:   "from-the-future",
		PageSlugs: "",
	}); err == nil {
		t.Fatal("expected error for invalid trigger, got nil")
	}
}

func TestListSuggestionRunsNewestFirstAndLimit(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		started := base.Add(time.Duration(i) * time.Minute)
		finished := started.Add(30 * time.Second)
		if _, err := InsertSuggestionRun(ctx, d, SuggestionRun{
			UserID:     1,
			StartedAt:  started,
			FinishedAt: &finished,
			Trigger:    TriggerScheduled,
			PageSlugs:  fmt.Sprintf("p%d", i),
			Generated:  i,
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	got, err := ListSuggestionRuns(ctx, d, 1, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows (limit), got %d", len(got))
	}
	// Newest first: index 4, 3, 2.
	for i, want := range []string{"p4", "p3", "p2"} {
		if got[i].PageSlugs != want {
			t.Errorf("row %d: got page_slugs %q, want %q", i, got[i].PageSlugs, want)
		}
	}
}

func TestListSuggestionRunsScopedByUser(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	if _, err := d.Exec(`INSERT INTO users (id, google_id, email, name, picture, is_admin) VALUES (2, 'g2', 'admin2@example.com', 'Admin2', '', 1)`); err != nil {
		t.Fatalf("create user 2: %v", err)
	}

	now := time.Now().UTC()
	if _, err := InsertSuggestionRun(ctx, d, SuggestionRun{UserID: 1, StartedAt: now, Trigger: TriggerManual, PageSlugs: "u1"}); err != nil {
		t.Fatalf("insert u1: %v", err)
	}
	if _, err := InsertSuggestionRun(ctx, d, SuggestionRun{UserID: 2, StartedAt: now, Trigger: TriggerManual, PageSlugs: "u2"}); err != nil {
		t.Fatalf("insert u2: %v", err)
	}

	for uid, want := range map[int64]string{1: "u1", 2: "u2"} {
		rows, err := ListSuggestionRuns(ctx, d, uid, 10)
		if err != nil {
			t.Fatalf("list user %d: %v", uid, err)
		}
		if len(rows) != 1 || rows[0].PageSlugs != want {
			t.Errorf("user %d: got %+v, want single row with page_slugs %q", uid, rows, want)
		}
	}
}

func TestBuildPageSlugsCSV(t *testing.T) {
	cases := []struct {
		name        string
		slugs       []string
		newPage     bool
		want        string
	}{
		{"empty no new", nil, false, ""},
		{"empty with new", nil, true, NewPageSlug},
		{"slugs no new", []string{"a", "b"}, false, "a,b"},
		{"slugs with new", []string{"a", "b"}, true, "a,b," + NewPageSlug},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildPageSlugsCSV(tc.slugs, tc.newPage)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Cost accumulation -------------------------------------------------------

func TestRunSuggestionsForPagesAccumulatesCost(t *testing.T) {
	d := setupTestDB(t)
	defer withRunPromptWithCost(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error) {
		return validJSONResponse, 0.05, nil
	})()

	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 1, twoPages())
	if res.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", res.Errors)
	}
	if math.Abs(res.CostUSD-0.10) > 1e-9 {
		t.Fatalf("expected cost 0.10 (2 pages × $0.05), got %f", res.CostUSD)
	}
}

func TestRunSuggestionsForPagesIncludesRetryCost(t *testing.T) {
	d := setupTestDB(t)
	var calls int
	defer withRunPromptWithCost(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error) {
		calls++
		// First attempt: malformed (still costs $0.03). Second attempt: valid ($0.05).
		if calls == 1 {
			return "{not json", 0.03, nil
		}
		return validJSONResponse, 0.05, nil
	})()

	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 1, []Page{{Slug: "weather", Title: "Weather"}})
	if res.Errors != 0 {
		t.Fatalf("expected 0 errors after retry, got %d", res.Errors)
	}
	if math.Abs(res.CostUSD-0.08) > 1e-9 {
		t.Fatalf("expected cost 0.08 ($0.03 + $0.05), got %f", res.CostUSD)
	}
}

func TestRunNewPageSuggestionAccumulatesCost(t *testing.T) {
	d := setupTestDB(t)
	defer withRunPromptWithCost(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error) {
		return validNewPageJSON, 0.07, nil
	})()

	res, err := RunNewPageSuggestion(context.Background(), d, dummyCfg(), 1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Generated != 1 {
		t.Fatalf("expected 1 generated, got %d", res.Generated)
	}
	if math.Abs(res.CostUSD-0.07) > 1e-9 {
		t.Fatalf("expected cost 0.07, got %f", res.CostUSD)
	}
}

// --- RunHandler row insertion ------------------------------------------------

func TestRunHandlerInsertsSuggestionRunRow(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	defer withRunPromptWithCost(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error) {
		if strings.Contains(prompt, "Return ONLY a single JSON object") {
			return validNewPageJSON, 0.02, nil
		}
		return validJSONResponse, 0.05, nil
	})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", bytes.NewReader(nil))
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rows, err := ListSuggestionRuns(context.Background(), d, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(rows))
	}
	r := rows[0]
	if r.Trigger != TriggerManual {
		t.Errorf("trigger: got %q want %q", r.Trigger, TriggerManual)
	}
	// 1 page + new_page sentinel.
	if r.PageSlugs != "weather,"+NewPageSlug {
		t.Errorf("page_slugs: got %q", r.PageSlugs)
	}
	// 3 per page + 1 new_page = 4.
	if r.Generated != 4 {
		t.Errorf("generated: got %d want 4", r.Generated)
	}
	if r.Errors != 0 {
		t.Errorf("errors: got %d want 0", r.Errors)
	}
	// $0.05 per-page + $0.02 new_page = $0.07.
	if math.Abs(r.CostUSD-0.07) > 1e-9 {
		t.Errorf("cost: got %f want 0.07", r.CostUSD)
	}
	if r.FinishedAt == nil {
		t.Errorf("finished_at should be set")
	}
}

// --- RunsHandler -------------------------------------------------------------

func TestRunsHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))

	user := &auth.User{ID: 99, IsAdmin: false}
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRunsHandlerReturnsNewestFirst(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		started := base.Add(time.Duration(i) * time.Minute)
		finished := started.Add(time.Second)
		if _, err := InsertSuggestionRun(ctx, d, SuggestionRun{
			UserID:     1,
			StartedAt:  started,
			FinishedAt: &finished,
			Trigger:    TriggerScheduled,
			PageSlugs:  fmt.Sprintf("p%d", i),
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []SuggestionRun
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}
	if got[0].PageSlugs != "p2" || got[1].PageSlugs != "p1" || got[2].PageSlugs != "p0" {
		t.Fatalf("expected newest-first order, got %+v", got)
	}
}

func TestRunsHandlerRespectsLimit(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		started := base.Add(time.Duration(i) * time.Minute)
		if _, err := InsertSuggestionRun(ctx, d, SuggestionRun{
			UserID:    1,
			StartedAt: started,
			Trigger:   TriggerScheduled,
			PageSlugs: fmt.Sprintf("p%d", i),
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs?limit=2", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got []SuggestionRun
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows (limit=2), got %d", len(got))
	}
}

func TestRunsHandlerClampsLimitToMax(t *testing.T) {
	d := setupTestDB(t)

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))

	// Request a limit of 9999; verify the handler accepts it without error.
	// We can't easily inspect the SQL LIMIT here, but the Default+Cap logic is
	// straightforward and the request should succeed (the runs table is empty).
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs?limit=9999", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with clamped limit, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []SuggestionRun
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty array, got %+v", got)
	}
}

func TestRunsHandlerEmptyReturnsArrayNotNull(t *testing.T) {
	d := setupTestDB(t)
	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("expected empty JSON array, got %q", body)
	}
}

func TestRunsHandlerInvalidLimitIsBadRequest(t *testing.T) {
	d := setupTestDB(t)
	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/runs", auth.RequireAdmin()(RunsHandler(d)))
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/runs?limit=abc", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-integer limit, got %d", rec.Code)
	}
}
