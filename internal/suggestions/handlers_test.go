package suggestions

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// withSavedPages temporarily overrides the global Pages registry for a test
// (so the handler doesn't try to generate against the full prod registry) and
// returns a restore function to defer.
func withSavedPages(replacement []Page) func() {
	prev := Pages
	Pages = replacement
	return func() { Pages = prev }
}

func TestRunHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validJSONResponse, nil
	})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	// Non-admin user.
	user := &auth.User{ID: 99, IsAdmin: false}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRunHandlerAdminStreamsEvents(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
	})()
	// The run handler now calls both the per-page rotation pass and the
	// separate new-page pass; respond with the right shape based on the
	// prompt's distinguishing phrase so both passes succeed.
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		if strings.Contains(prompt, "Return ONLY a single JSON object") {
			return validNewPageJSON, nil
		}
		return validJSONResponse, nil
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
		t.Fatalf("expected 200 for admin, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	events := parseSSEEvents(t, rec.Body.String())

	// Expect: started, page_complete×2, new_page_complete, done.
	if len(events) != 5 {
		t.Fatalf("expected 5 events (started + 2 page_complete + new_page_complete + done), got %d: %+v", len(events), events)
	}
	if events[0].name != "started" {
		t.Fatalf("event 0: expected started, got %q", events[0].name)
	}
	var started startedEvent
	if err := json.Unmarshal([]byte(events[0].data), &started); err != nil {
		t.Fatalf("decode started: %v", err)
	}
	if started.RunID == 0 {
		t.Fatalf("started.run_id should be > 0")
	}
	if started.TotalPages != 2 {
		t.Fatalf("started.total_pages: got %d want 2", started.TotalPages)
	}
	if len(started.PageSlugs) != 2 || started.PageSlugs[0] != "weather" || started.PageSlugs[1] != "notes" {
		t.Fatalf("started.page_slugs: got %+v", started.PageSlugs)
	}

	for i, want := range []string{"weather", "notes"} {
		ev := events[1+i]
		if ev.name != "page_complete" {
			t.Fatalf("event %d: expected page_complete, got %q", 1+i, ev.name)
		}
		var pc pageCompleteEvent
		if err := json.Unmarshal([]byte(ev.data), &pc); err != nil {
			t.Fatalf("decode page_complete %d: %v", i, err)
		}
		if pc.PageSlug != want {
			t.Errorf("page_complete %d: page_slug got %q want %q", i, pc.PageSlug, want)
		}
		if pc.Status != "ok" {
			t.Errorf("page_complete %d: status got %q want ok", i, pc.Status)
		}
		if pc.Generated != 3 {
			t.Errorf("page_complete %d: generated got %d want 3", i, pc.Generated)
		}
	}

	if events[3].name != "new_page_complete" {
		t.Fatalf("event 3: expected new_page_complete, got %q", events[3].name)
	}
	var npc newPageCompleteEvent
	if err := json.Unmarshal([]byte(events[3].data), &npc); err != nil {
		t.Fatalf("decode new_page_complete: %v", err)
	}
	if npc.PageSlug != NewPageSlug || npc.Status != "ok" || npc.Generated != 1 {
		t.Errorf("new_page_complete: %+v", npc)
	}

	if events[4].name != "done" {
		t.Fatalf("event 4: expected done, got %q", events[4].name)
	}
	var done doneEvent
	if err := json.Unmarshal([]byte(events[4].data), &done); err != nil {
		t.Fatalf("decode done: %v", err)
	}
	if done.RunID != started.RunID {
		t.Errorf("done.run_id: got %d want %d", done.RunID, started.RunID)
	}
	// 3 per page × 2 pages = 6 from per-page rotation, plus 1 from the
	// new-page pass = 7 total.
	if done.Generated != 7 {
		t.Errorf("done.generated: got %d want 7", done.Generated)
	}
	if done.Errors != 0 {
		t.Errorf("done.errors: got %d want 0", done.Errors)
	}

	var rowCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 7 {
		t.Fatalf("expected 7 rows persisted, got %d", rowCount)
	}

	var newPageCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = ?`, NewPageSlug).Scan(&newPageCount); err != nil {
		t.Fatalf("count new_page rows: %v", err)
	}
	if newPageCount != 1 {
		t.Fatalf("expected exactly 1 new_page row, got %d", newPageCount)
	}

	// The audit row should be marked finished with the same totals as the
	// done event.
	var (
		gotGenerated, gotErrors int
		gotCost                 float64
		finishedAt              sql.NullString
	)
	if err := d.QueryRow(`SELECT generated, errors, cost_usd, finished_at FROM suggestion_runs WHERE id = ?`, started.RunID).
		Scan(&gotGenerated, &gotErrors, &gotCost, &finishedAt); err != nil {
		t.Fatalf("read run row: %v", err)
	}
	if !finishedAt.Valid {
		t.Errorf("finished_at should be set after a successful run")
	}
	if gotGenerated != done.Generated || gotErrors != done.Errors {
		t.Errorf("audit row mismatch: generated=%d errors=%d, want done %+v", gotGenerated, gotErrors, done)
	}
}

func TestRunHandlerReturns409WhenInflightRunExists(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	// Seed an in-flight run (finished_at NULL).
	existingID, err := InsertSuggestionRun(context.Background(), d, SuggestionRun{
		UserID:    1,
		StartedAt: time.Now().UTC(),
		Trigger:   TriggerManual,
		PageSlugs: "weather",
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when run in flight, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		RunID int64  `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RunID != existingID {
		t.Fatalf("expected run_id %d, got %d", existingID, body.RunID)
	}
}

// TestRunHandlerConcurrentRequestsSecondReturns409 fires two simultaneous POSTs
// and asserts that the second one observes the first one's in-flight run row
// and is rejected with 409 + the original run_id. The first request's mock
// blocks on a channel so its in-flight row is guaranteed to be visible when
// the second request hits the InflightRunForUser check.
func TestRunHandlerConcurrentRequestsSecondReturns409(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	// reachedMock signals that goroutine #1's work loop has entered the mock —
	// at that point the in-flight suggestion_runs row is committed and the
	// second goroutine is guaranteed to see it. release lets the first
	// request's mock return so the run can complete cleanly at the end.
	reachedMock := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		once.Do(func() { close(reachedMock) })
		<-release
		if strings.Contains(prompt, "Return ONLY a single JSON object") {
			return validNewPageJSON, nil
		}
		return validJSONResponse, nil
	})()

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}

	// Goroutine #1: holds the in-flight slot until release is closed.
	rec1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
		mux.ServeHTTP(rec1, req)
	}()

	// Wait until goroutine #1 has reached the mock, ensuring the in-flight
	// suggestion_runs row is committed before the second POST fires.
	select {
	case <-reachedMock:
	case <-time.After(2 * time.Second):
		close(release)
		<-done1
		t.Fatalf("first request did not reach mock in time")
	}

	// Goroutine #2: same user, same endpoint, fired while #1 is in flight.
	req2 := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req2 = req2.WithContext(auth.ContextWithUser(req2.Context(), user))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		close(release)
		<-done1
		t.Fatalf("expected 409 from concurrent POST, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		RunID int64  `json:"run_id"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
		close(release)
		<-done1
		t.Fatalf("decode 409 body: %v", err)
	}
	if body.Error == "" {
		t.Errorf("expected non-empty error, got %q", body.Error)
	}

	// The reported run_id must match the in-flight row from goroutine #1.
	var inflightID int64
	if err := d.QueryRow(`SELECT id FROM suggestion_runs WHERE finished_at IS NULL AND user_id = 1 ORDER BY id DESC LIMIT 1`).Scan(&inflightID); err != nil {
		close(release)
		<-done1
		t.Fatalf("read in-flight run id: %v", err)
	}
	if body.RunID != inflightID {
		t.Errorf("run_id: got %d want %d", body.RunID, inflightID)
	}

	// Release goroutine #1 and let it complete cleanly.
	close(release)
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatalf("first request did not finish")
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec1.Code)
	}
}

// TestRunHandlerClientDisconnectStillPersists verifies that cancelling the
// client context mid-stream does not abort the work goroutine: the
// suggestion_runs row is updated with finished_at, the final totals match the
// rows actually persisted, and per-page suggestions for every completed page
// land in the DB. This pins the detached-context behaviour described in
// RunHandler's doc comment.
func TestRunHandlerClientDisconnectStillPersists(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
	})()
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	// Add a small delay per Claude call so the test has time to disconnect
	// after seeing the first page_complete event but before the work goroutine
	// finishes. The detached runCtx is independent of the client context, so
	// this delay is unaffected by the client cancellation we issue below.
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		time.Sleep(50 * time.Millisecond)
		if strings.Contains(prompt, "Return ONLY a single JSON object") {
			return validNewPageJSON, nil
		}
		return validJSONResponse, nil
	})()

	user := &auth.User{ID: 1, IsAdmin: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject an admin user the same way RequireAuth would in production
		// so RequireAdmin sees a non-nil admin in context.
		r = r.WithContext(auth.ContextWithUser(r.Context(), user))
		auth.RequireAdmin()(RunHandler(d)).ServeHTTP(w, r)
	}))
	defer srv.Close()

	clientCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(clientCtx, http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read the SSE stream until we see the first page_complete event; this
	// proves the server has actually started streaming and at least one
	// per-page write has begun.
	scanner := bufio.NewScanner(resp.Body)
	var sawStarted, sawPageComplete bool
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: started"):
			sawStarted = true
		case strings.HasPrefix(line, "event: page_complete"):
			sawPageComplete = true
		}
		if sawPageComplete {
			break
		}
	}
	if !sawStarted || !sawPageComplete {
		t.Fatalf("expected to see started + page_complete; sawStarted=%v sawPageComplete=%v err=%v",
			sawStarted, sawPageComplete, scanner.Err())
	}

	// Disconnect mid-stream. The handler's streaming loop will observe
	// r.Context().Done() and stop writing — but the detached work goroutine
	// must continue persisting per-page suggestions and updating the audit
	// row with finished_at.
	cancel()
	_ = resp.Body.Close()

	// Poll the audit row for finished_at. Generous deadline to absorb the
	// per-page sleeps and the new-page pass that complete after disconnect.
	var (
		finishedAt sql.NullString
		generated  int
		errCount   int
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		err := d.QueryRow(`
			SELECT finished_at, generated, errors
			FROM suggestion_runs
			ORDER BY id DESC
			LIMIT 1
		`).Scan(&finishedAt, &generated, &errCount)
		if err != nil {
			t.Fatalf("read run row: %v", err)
		}
		if finishedAt.Valid {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !finishedAt.Valid {
		t.Fatalf("expected finished_at to be set after client disconnect (work goroutine should persist independently)")
	}
	if errCount != 0 {
		t.Errorf("expected 0 errors after detached run, got %d", errCount)
	}

	// Final totals on the audit row must match the suggestions actually
	// persisted: 3 per page × 2 pages + 1 new_page = 7.
	var rowCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE user_id = 1`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != generated {
		t.Errorf("rows persisted (%d) should match suggestion_runs.generated (%d)", rowCount, generated)
	}
	if rowCount != 7 {
		t.Errorf("expected 7 rows after disconnect (2 pages × 3 + 1 new_page), got %d", rowCount)
	}

	// Both per-page slugs and the synthetic new-page slug must have rows —
	// detached persistence covers every page that completed in the goroutine.
	for _, slug := range []string{"weather", "notes", NewPageSlug} {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = ? AND user_id = 1`, slug).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", slug, err)
		}
		if n == 0 {
			t.Errorf("expected suggestions persisted for page_slug=%q after disconnect, got 0", slug)
		}
	}
}

// sseTestEvent is the parsed shape of a single Server-Sent Event used by tests.
type sseTestEvent struct {
	name string
	data string
}

// parseSSEEvents splits a raw SSE response body into individual events.
// Only event/data pairs are recognised; comments and other fields are skipped.
func parseSSEEvents(t *testing.T, body string) []sseTestEvent {
	t.Helper()
	var out []sseTestEvent
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var ev sseTestEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
		if ev.name != "" {
			out = append(out, ev)
		}
	}
	return out
}

func TestRunHandlerRequiresClaudeEnabled(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	// Note: claude_enabled is intentionally NOT set.

	mux := http.NewServeMux()
	mux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))

	user := &auth.User{ID: 1, IsAdmin: true}
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when claude disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

// mountAdmin wraps a single handler in RequireAdmin, mounted on a chi router
// so that URL params are populated as they are at runtime.
func mountAdmin(handler http.Handler, method, pattern string) chi.Router {
	r := chi.NewRouter()
	r.Method(method, pattern, auth.RequireAdmin()(handler))
	return r
}

func adminContext(r *http.Request, userID int64, isAdmin bool) *http.Request {
	user := &auth.User{ID: userID, IsAdmin: isAdmin}
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

// --- ListHandler -------------------------------------------------------------

func TestListHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(ListHandler(d), http.MethodGet, "/api/suggestions")

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions", nil)
	req = adminContext(req, 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListHandlerPartitionsByStatus(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	insert := func(status, title string) {
		t.Helper()
		if _, err := Insert(ctx, d, Suggestion{
			UserID: 1, PageSlug: "weather", Source: SourceClaude,
			Type: TypeImprovement, Size: SizeS, Title: title, Body: "b", Status: status,
		}); err != nil {
			t.Fatalf("insert %s: %v", status, err)
		}
	}
	insert(StatusPending, "P1")
	insert(StatusPending, "P2")
	insert(StatusPlanned, "PL1")
	insert(StatusRejected, "R1")
	insert(StatusBeadCreated, "BC1")

	router := mountAdmin(ListHandler(d), http.MethodGet, "/api/suggestions")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got listResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Pending) != 2 {
		t.Fatalf("pending: expected 2, got %d", len(got.Pending))
	}
	if len(got.Planned) != 1 || got.Planned[0].Title != "PL1" {
		t.Fatalf("planned mismatch: %+v", got.Planned)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Title != "R1" {
		t.Fatalf("rejected mismatch: %+v", got.Rejected)
	}
	if len(got.BeadCreated) != 1 || got.BeadCreated[0].Title != "BC1" {
		t.Fatalf("bead_created mismatch: %+v", got.BeadCreated)
	}
}

func TestListHandlerEmptyBucketsAreArrays(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(ListHandler(d), http.MethodGet, "/api/suggestions")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, key := range []string{`"pending":[]`, `"planned":[]`, `"rejected":[]`, `"bead_created":[]`} {
		if !strings.Contains(body, key) {
			t.Fatalf("expected empty array for %s, body=%s", key, body)
		}
	}
}

// --- CreateHandler -----------------------------------------------------------

func TestCreateHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CreateHandler(d), http.MethodPost, "/api/suggestions")
	body := `{"type":"improvement","size":"s","page_slug":"weather","title":"t","body":"b"}`
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions", strings.NewReader(body)), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}
}

func TestCreateHandlerSuccess(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	router := mountAdmin(CreateHandler(d), http.MethodPost, "/api/suggestions")
	payload := `{"type":"improvement","size":"m","page_slug":"weather","title":"Cache forecasts","body":"Cache yr.no for 10 minutes."}`
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions", strings.NewReader(payload)), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID <= 0 {
		t.Fatalf("expected positive id, got %d", got.ID)
	}
	if got.Source != SourceUser {
		t.Fatalf("expected source=user, got %q", got.Source)
	}
	if got.Status != StatusPending {
		t.Fatalf("expected status=pending, got %q", got.Status)
	}
	if got.Title != "Cache forecasts" || got.Body != "Cache yr.no for 10 minutes." {
		t.Fatalf("title/body round-trip failed: %+v", got)
	}
}

func TestCreateHandlerAllowsNewPageSlug(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	router := mountAdmin(CreateHandler(d), http.MethodPost, "/api/suggestions")
	payload := fmt.Sprintf(`{"type":"new_page","size":"l","page_slug":%q,"title":"Add expense tracker","body":"A page that..."}`, NewPageSlug)
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions", strings.NewReader(payload)), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for __new_page__, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateHandlerValidationRejections(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	cases := []struct {
		name    string
		payload string
	}{
		{"bad type", `{"type":"weird","size":"s","page_slug":"weather","title":"t","body":"b"}`},
		{"bad size", `{"type":"improvement","size":"xl","page_slug":"weather","title":"t","body":"b"}`},
		{"unknown slug", `{"type":"improvement","size":"s","page_slug":"nope","title":"t","body":"b"}`},
		{"empty title", `{"type":"improvement","size":"s","page_slug":"weather","title":"  ","body":"b"}`},
		{"empty body", `{"type":"improvement","size":"s","page_slug":"weather","title":"t","body":""}`},
		{"invalid json", `{not json`},
		// Cross-validation: new_page type must pair with __new_page__ slug and vice versa.
		{"new_page type with registered slug", `{"type":"new_page","size":"s","page_slug":"weather","title":"t","body":"b"}`},
		{"bugfix type with __new_page__ slug", fmt.Sprintf(`{"type":"bugfix","size":"s","page_slug":%q,"title":"t","body":"b"}`, NewPageSlug)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := mountAdmin(CreateHandler(d), http.MethodPost, "/api/suggestions")
			req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions", strings.NewReader(tc.payload)), 1, true)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateThenListRoundTripsDecryptedBody(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	const plaintext = "Encrypted at rest, decrypted at the boundary."
	create := mountAdmin(CreateHandler(d), http.MethodPost, "/api/suggestions")
	payload := fmt.Sprintf(`{"type":"improvement","size":"s","page_slug":"weather","title":"Round trip","body":%q}`, plaintext)
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions", strings.NewReader(payload)), 1, true)
	rec := httptest.NewRecorder()
	create.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	list := mountAdmin(ListHandler(d), http.MethodGet, "/api/suggestions")
	req = adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions", nil), 1, true)
	rec = httptest.NewRecorder()
	list.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got listResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(got.Pending))
	}
	if got.Pending[0].Body != plaintext {
		t.Fatalf("body round-trip failed: got %q want %q", got.Pending[0].Body, plaintext)
	}
}

// --- RejectHandler -----------------------------------------------------------

func TestRejectHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/1/reject", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRejectHandlerSuccess(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/reject", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusRejected {
		t.Fatalf("status: got %q want %q", got.Status, StatusRejected)
	}
	if got.RejectedAt == nil {
		t.Fatal("expected rejected_at to be set")
	}
}

func TestRejectHandlerNotFound(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/999/reject", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRejectHandlerInvalidID(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/abc/reject", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRejectHandlerOtherUserSuggestionReturns404(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	// Suggestion owned by user 1.
	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")
	// Different admin (user 2) tries to reject user 1's suggestion.
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/reject", id), nil), 2, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user reject, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRejectHandlerCannotRejectBeadCreated(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusBeadCreated,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/reject", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for bead_created suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRejectHandlerIsIdempotent(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(RejectHandler(d), http.MethodPost, "/api/suggestions/{id}/reject")

	// First reject — should succeed.
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/reject", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first reject: expected 200, got %d", rec.Code)
	}

	// Second reject — must also return 200 with the existing row.
	req = adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/reject", id), nil), 1, true)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second reject: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusRejected {
		t.Fatalf("status: %q", got.Status)
	}
}

// --- PlanHandler -------------------------------------------------------------

// withPlanTimeout temporarily shrinks PlanTimeout so the timeout-path test can
// run in milliseconds rather than the production 120s. Returns a restore fn.
func withPlanTimeout(d time.Duration) func() {
	prev := PlanTimeout
	PlanTimeout = d
	return func() { PlanTimeout = prev }
}

func TestPlanHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/1/plan", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerSuccess(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	const cannedPlan = "### Scope\nDo the thing.\n\n### Files to touch\n- foo.go\n\n### Acceptance criteria\n- It works.\n\n### Non-goals\n- Anything else."
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return cannedPlan, nil
	})()

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusPlanned {
		t.Fatalf("status: got %q want %q", got.Status, StatusPlanned)
	}
	if got.Plan != cannedPlan {
		t.Fatalf("plan round-trip: got %q want %q", got.Plan, cannedPlan)
	}
	if got.PlannedAt == nil {
		t.Fatal("expected planned_at to be set")
	}
}

func TestPlanHandlerPassesFeedbackIntoPrompt(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	const feedback = "Use Redis instead of an in-memory map; we already run Redis."
	var capturedPrompt string
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		capturedPrompt = prompt
		return "ok plan", nil
	})()

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	payload := fmt.Sprintf(`{"feedback":%q}`, feedback)
	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), strings.NewReader(payload)), 1, true)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(capturedPrompt, feedback) {
		t.Fatalf("expected captured prompt to contain feedback verbatim, got:\n%s", capturedPrompt)
	}
}

func TestPlanHandlerNotFound(t *testing.T) {
	d := setupTestDB(t)
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	// runPromptFn must NOT be called on the not-found path, but defining it
	// keeps the test hermetic if the handler ever changes.
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		t.Fatal("runPromptFn should not be called for missing id")
		return "", nil
	})()

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/999/plan", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerInvalidID(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/abc/plan", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPlanHandlerOtherUserSuggestionReturns404(t *testing.T) {
	d := setupTestDB(t)
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	// User 2 (a different admin) tries to plan user 1's suggestion.
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 2, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user plan, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerTimeoutDoesNotMarkPlanned(t *testing.T) {
	d := setupTestDB(t)
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	defer withPlanTimeout(50 * time.Millisecond)()
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})()

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 on timeout, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the suggestion was NOT marked planned.
	got, err := GetByID(context.Background(), d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("status should remain pending after timeout, got %q", got.Status)
	}
	if got.PlannedAt != nil {
		t.Fatalf("planned_at should remain nil after timeout, got %v", got.PlannedAt)
	}
	if got.Plan != "" {
		t.Fatalf("plan should remain empty after timeout, got %q", got.Plan)
	}
}

func TestPlanHandlerNewPageSlugProducesValidPrompt(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()

	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	var capturedPrompt string
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		capturedPrompt = prompt
		return "plan body", nil
	})()

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: NewPageSlug, Source: SourceUser,
		Type: TypeNewPage, Size: SizeL, Title: "Brand new page", Body: "details", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(capturedPrompt, NewPageSlug) {
		t.Fatalf("expected prompt to acknowledge __new_page__, got:\n%s", capturedPrompt)
	}
}

func TestPlanHandlerReplacesPriorPlan(t *testing.T) {
	d := setupTestDB(t)
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// First plan.
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return "first plan", nil
	})()
	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first plan: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second plan replaces the first.
	runPromptFn = func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error) {
		return "second plan", 0, nil
	}
	req = adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second plan: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Plan != "second plan" {
		t.Fatalf("expected re-plan to replace plan, got %q", got.Plan)
	}
}

func TestPlanHandlerCannotPlanBeadCreated(t *testing.T) {
	d := setupTestDB(t)

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusBeadCreated,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for bead_created suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerCanPlanRejectedSuggestion(t *testing.T) {
	d := setupTestDB(t)
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return "recovery plan", nil
	})()

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := MarkRejected(context.Background(), d, id); err != nil {
		t.Fatalf("mark rejected: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when re-planning rejected suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusPlanned {
		t.Fatalf("status: got %q want %q", got.Status, StatusPlanned)
	}
	if got.RejectedAt != nil {
		t.Fatalf("expected rejected_at to be cleared after re-planning, got %v", got.RejectedAt)
	}
	if got.Plan != "recovery plan" {
		t.Fatalf("plan: got %q want %q", got.Plan, "recovery plan")
	}
}

func TestPlanHandlerRequiresClaudeEnabled(t *testing.T) {
	d := setupTestDB(t)
	// claude_enabled intentionally NOT set.

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	router := mountAdmin(PlanHandler(d), http.MethodPost, "/api/suggestions/{id}/plan")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/plan", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when claude disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- PagesHandler ------------------------------------------------------------

func TestPagesHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(PagesHandler(d), http.MethodGet, "/api/suggestions/pages")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions/pages", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestPagesHandlerReturnsRegistryPlusNewPage(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
	})()

	router := mountAdmin(PagesHandler(d), http.MethodGet, "/api/suggestions/pages")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions/pages", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []pageSummary
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Expect 2 registered pages + 1 synthetic.
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (2 registered + new_page), got %d: %+v", len(got), got)
	}
	if got[0].Slug != "weather" || got[1].Slug != "notes" {
		t.Fatalf("registry order broken: %+v", got)
	}
	if got[2].Slug != NewPageSlug {
		t.Fatalf("expected last entry to be %q, got %q", NewPageSlug, got[2].Slug)
	}
	// With no rows in suggestion_page_settings, every entry should report
	// rotation_enabled=null so the frontend can render the default-on state.
	for _, p := range got {
		if p.RotationEnabled != nil {
			t.Fatalf("expected rotation_enabled=nil for %q with no settings row, got %v", p.Slug, *p.RotationEnabled)
		}
	}
}

func TestPagesHandlerIncludesRotationEnabledFromSettings(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
		{Slug: "training", Title: "Training"},
	})()

	// weather has an explicit on row, notes has an explicit off row, training
	// has no row (default null).
	insertPageSetting(t, d, "weather", 1)
	insertPageSetting(t, d, "notes", 0)

	router := mountAdmin(PagesHandler(d), http.MethodGet, "/api/suggestions/pages")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions/pages", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []pageSummary
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	bySlug := make(map[string]pageSummary, len(got))
	for _, p := range got {
		bySlug[p.Slug] = p
	}

	w := bySlug["weather"]
	if w.RotationEnabled == nil || !*w.RotationEnabled {
		t.Fatalf("weather: expected rotation_enabled=true, got %v", w.RotationEnabled)
	}
	n := bySlug["notes"]
	if n.RotationEnabled == nil || *n.RotationEnabled {
		t.Fatalf("notes: expected rotation_enabled=false, got %v", n.RotationEnabled)
	}
	tr := bySlug["training"]
	if tr.RotationEnabled != nil {
		t.Fatalf("training: expected rotation_enabled=nil (default), got %v", *tr.RotationEnabled)
	}
	// Synthetic new-page entry never carries a rotation flag.
	np := bySlug[NewPageSlug]
	if np.RotationEnabled != nil {
		t.Fatalf("%s: expected rotation_enabled=nil, got %v", NewPageSlug, *np.RotationEnabled)
	}
}

// --- UpdatePageSettingsHandler -----------------------------------------------

func TestUpdatePageSettingsHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	router := mountAdmin(UpdatePageSettingsHandler(d), http.MethodPatch, "/api/suggestions/pages/{slug}")

	req := adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/weather", strings.NewReader(`{"rotation_enabled":true}`)), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePageSettingsHandlerInsertsAndUpdates(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	router := mountAdmin(UpdatePageSettingsHandler(d), http.MethodPatch, "/api/suggestions/pages/{slug}")

	// First call inserts a row with rotation_enabled=false.
	req := adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/weather", strings.NewReader(`{"rotation_enabled":false}`)), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got pageSummary
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "weather" || got.Title != "Weather" {
		t.Fatalf("unexpected page summary: %+v", got)
	}
	if got.RotationEnabled == nil || *got.RotationEnabled {
		t.Fatalf("expected rotation_enabled=false, got %v", got.RotationEnabled)
	}

	// Verify the row landed in the DB with the expected value.
	var enabled int
	if err := d.QueryRow(`SELECT rotation_enabled FROM suggestion_page_settings WHERE page_slug = ?`, "weather").Scan(&enabled); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if enabled != 0 {
		t.Fatalf("expected rotation_enabled=0 in DB, got %d", enabled)
	}

	// Second call flips it back to true via the upsert path.
	req = adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/weather", strings.NewReader(`{"rotation_enabled":true}`)), 1, true)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := d.QueryRow(`SELECT rotation_enabled FROM suggestion_page_settings WHERE page_slug = ?`, "weather").Scan(&enabled); err != nil {
		t.Fatalf("read row after upsert: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("expected rotation_enabled=1 after upsert, got %d", enabled)
	}

	// Only one row should exist for the slug — confirms the upsert path.
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestion_page_settings WHERE page_slug = ?`, "weather").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row for weather, got %d", count)
	}
}

func TestUpdatePageSettingsHandlerUnknownSlugReturns404(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	router := mountAdmin(UpdatePageSettingsHandler(d), http.MethodPatch, "/api/suggestions/pages/{slug}")

	req := adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/nope", strings.NewReader(`{"rotation_enabled":true}`)), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown slug, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePageSettingsHandlerNewPageSlugReturns404(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	router := mountAdmin(UpdatePageSettingsHandler(d), http.MethodPatch, "/api/suggestions/pages/{slug}")

	// The synthetic __new_page__ sentinel is not a registered page; rotation
	// does not apply to it, so the endpoint must reject it as unknown.
	req := adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/"+NewPageSlug, strings.NewReader(`{"rotation_enabled":true}`)), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for synthetic slug, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePageSettingsHandlerInvalidBody(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather"}})()
	router := mountAdmin(UpdatePageSettingsHandler(d), http.MethodPatch, "/api/suggestions/pages/{slug}")

	cases := []struct {
		name    string
		payload string
	}{
		{"malformed json", `{not json`},
		{"missing field", `{}`},
		{"wrong type", `{"rotation_enabled":"yes"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := adminContext(httptest.NewRequest(http.MethodPatch, "/api/suggestions/pages/weather", strings.NewReader(tc.payload)), 1, true)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// --- CancelHandler -----------------------------------------------------------

func TestCancelHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/run/1/cancel", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelHandlerInvalidIDReturns400(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/run/notanumber/cancel", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelHandlerUnknownRunReturns404(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/run/9999/cancel", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown run, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelHandlerCrossUserReturns404(t *testing.T) {
	d := setupTestDB(t)

	// Seed an in-flight run owned by user 2 and try to cancel as user 1.
	otherID, err := InsertSuggestionRun(context.Background(), d, SuggestionRun{
		UserID:    2,
		StartedAt: time.Now().UTC(),
		Trigger:   TriggerManual,
		PageSlugs: "weather",
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	// Register a cancel handle so the test would otherwise succeed if the
	// cross-user guard were missing.
	registerRunCancel(otherID, func() {})
	defer unregisterRunCancel(otherID)

	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/run/%d/cancel", otherID), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user run, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelHandlerAlreadyFinishedReturns409(t *testing.T) {
	d := setupTestDB(t)

	finished := time.Now().UTC().Add(-time.Minute)
	row := SuggestionRun{
		UserID:     1,
		StartedAt:  time.Now().UTC().Add(-2 * time.Minute),
		FinishedAt: &finished,
		Trigger:    TriggerManual,
		PageSlugs:  "weather",
	}
	id, err := InsertSuggestionRun(context.Background(), d, row)
	if err != nil {
		t.Fatalf("seed finished run: %v", err)
	}

	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/run/%d/cancel", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for finished run, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCancelHandlerNotInRegistryReturns409 covers the orphaned-row case: the
// suggestion_runs row says in-flight (finished_at NULL) but no cancel handle is
// registered for it on this process. This happens after a server restart that
// left the run row stranded.
func TestCancelHandlerNotInRegistryReturns409(t *testing.T) {
	d := setupTestDB(t)

	id, err := InsertSuggestionRun(context.Background(), d, SuggestionRun{
		UserID:    1,
		StartedAt: time.Now().UTC(),
		Trigger:   TriggerManual,
		PageSlugs: "weather",
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}

	router := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/run/%d/cancel", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for orphaned run, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCancelHandlerCancelsInflightRunAndPersists fires a real run, blocks the
// first Claude call until the cancel arrives, then asserts that the goroutine
// exits cleanly: finished_at is set, remaining pages are skipped, and the
// cancel handle is unregistered (so a second cancel returns 409).
func TestCancelHandlerCancelsInflightRunAndPersists(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
	})()
	if err := auth.SetPreference(d, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	// First mock invocation signals it has been entered, then blocks on ctx
	// — the cancel signal arrives by way of runCtx propagating into pageCtx.
	reachedMock := make(chan struct{})
	var once sync.Once
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		once.Do(func() { close(reachedMock) })
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			// Safety bound — fail loudly rather than hang the test.
			return "", fmt.Errorf("mock never observed cancellation")
		}
	})()

	user := &auth.User{ID: 1, IsAdmin: true}

	runMux := http.NewServeMux()
	runMux.Handle("/api/suggestions/run", auth.RequireAdmin()(RunHandler(d)))
	cancelRouter := mountAdmin(CancelHandler(d), http.MethodPost, "/api/suggestions/run/{id}/cancel")

	runRec := httptest.NewRecorder()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		req := httptest.NewRequest(http.MethodPost, "/api/suggestions/run", nil)
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
		runMux.ServeHTTP(runRec, req)
	}()

	select {
	case <-reachedMock:
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not reach mock in time")
	}

	var inflightID int64
	if err := d.QueryRow(`SELECT id FROM suggestion_runs WHERE user_id = 1 AND finished_at IS NULL`).Scan(&inflightID); err != nil {
		t.Fatalf("read in-flight run: %v", err)
	}

	cancelReq := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/run/%d/cancel", inflightID), nil), 1, true)
	cancelRec := httptest.NewRecorder()
	cancelRouter.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 from cancel, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}

	// Cancellation is asynchronous; wait for the streaming response to close.
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("run did not finish after cancel")
	}
	if runRec.Code != http.StatusOK {
		t.Fatalf("run handler expected 200 (already streaming), got %d", runRec.Code)
	}

	// Audit row must be marked finished even though the run was aborted.
	var finishedAt sql.NullString
	if err := d.QueryRow(`SELECT finished_at FROM suggestion_runs WHERE id = ?`, inflightID).Scan(&finishedAt); err != nil {
		t.Fatalf("read finished_at: %v", err)
	}
	if !finishedAt.Valid {
		t.Errorf("finished_at should be set after cancellation")
	}

	// Cancel handle must be unregistered after the goroutine exits, so a
	// second cancel call sees the row as finished and returns 409.
	cancelReq2 := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/run/%d/cancel", inflightID), nil), 1, true)
	cancelRec2 := httptest.NewRecorder()
	cancelRouter.ServeHTTP(cancelRec2, cancelReq2)
	if cancelRec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 from second cancel, got %d: %s", cancelRec2.Code, cancelRec2.Body.String())
	}

	// No new_page row should exist — the new-page pass is skipped on
	// cancellation rather than running with an already-cancelled context.
	var newPageCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = ? AND user_id = 1`, NewPageSlug).Scan(&newPageCount); err != nil {
		t.Fatalf("count new_page rows: %v", err)
	}
	if newPageCount != 0 {
		t.Errorf("expected 0 new_page rows on cancelled run, got %d", newPageCount)
	}
}
