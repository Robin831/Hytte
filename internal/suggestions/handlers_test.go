package suggestions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()
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

func TestRunHandlerAdminReturnsCounts(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather", Enabled: true},
		{Slug: "notes", Title: "Notes", Enabled: true},
	})()
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
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

	var got RunResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", got.Errors)
	}
	if got.Generated != 6 {
		t.Fatalf("expected 6 generated (3 per page × 2), got %d", got.Generated)
	}

	var rowCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 6 {
		t.Fatalf("expected 6 rows persisted, got %d", rowCount)
	}
}

func TestRunHandlerRequiresClaudeEnabled(t *testing.T) {
	d := setupTestDB(t)
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()
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
	insert(StatusBeadCreated, "BC1") // should not appear in any bucket

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
	for _, key := range []string{`"pending":[]`, `"planned":[]`, `"rejected":[]`} {
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
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()

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
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()

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
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()

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
	defer withSavedPages([]Page{{Slug: "weather", Title: "Weather", Enabled: true}})()

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

// --- PagesHandler ------------------------------------------------------------

func TestPagesHandlerRejectsNonAdmin(t *testing.T) {
	router := mountAdmin(PagesHandler(), http.MethodGet, "/api/suggestions/pages")
	req := adminContext(httptest.NewRequest(http.MethodGet, "/api/suggestions/pages", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestPagesHandlerReturnsRegistryPlusNewPage(t *testing.T) {
	defer withSavedPages([]Page{
		{Slug: "weather", Title: "Weather", Enabled: true},
		{Slug: "notes", Title: "Notes", Enabled: true},
		{Slug: "disabled", Title: "Disabled", Enabled: false},
	})()

	router := mountAdmin(PagesHandler(), http.MethodGet, "/api/suggestions/pages")
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
	// Expect 2 enabled pages + 1 synthetic.
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (2 enabled + new_page), got %d: %+v", len(got), got)
	}
	if got[0].Slug != "weather" || got[1].Slug != "notes" {
		t.Fatalf("registry order broken: %+v", got)
	}
	if got[2].Slug != NewPageSlug {
		t.Fatalf("expected last entry to be %q, got %q", NewPageSlug, got[2].Slug)
	}
}
