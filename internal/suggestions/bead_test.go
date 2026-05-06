package suggestions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withBdCreate temporarily replaces bdCreateFn for the duration of a test and
// returns a restore function to defer.
func withBdCreate(fn func(ctx context.Context, cwd, title, body string) (string, string, error)) func() {
	prev := bdCreateFn
	bdCreateFn = fn
	return func() { bdCreateFn = prev }
}

func TestCreateBeadHandlerRejectsNonAdmin(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/1/bead", nil), 99, false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBeadHandlerInvalidID(t *testing.T) {
	d := setupTestDB(t)
	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")

	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/abc/bead", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateBeadHandlerNotFound(t *testing.T) {
	d := setupTestDB(t)
	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		t.Fatal("bdCreateFn should not be called for missing id")
		return "", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, "/api/suggestions/999/bead", nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBeadHandlerOtherUserSuggestionReturns404(t *testing.T) {
	d := setupTestDB(t)
	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Plan: "p", Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		t.Fatal("bdCreateFn should not be called for cross-user lookup")
		return "", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 2, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user bead create, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBeadHandlerRejectsPendingStatus(t *testing.T) {
	d := setupTestDB(t)
	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		t.Fatal("bdCreateFn should not be called for non-planned suggestion")
		return "", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for pending status, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBeadHandlerRejectsRejectedStatus(t *testing.T) {
	d := setupTestDB(t)
	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusRejected,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		t.Fatal("bdCreateFn should not be called for rejected suggestion")
		return "", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for rejected status, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBeadHandlerSuccess(t *testing.T) {
	d := setupTestDB(t)

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS,
		Title: "Cache forecasts", Body: "Original body content.",
		Plan: "## Scope\nDo the cache thing.", Feedback: "Use Redis.",
		Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var capturedBody, capturedTitle string
	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		capturedBody = body
		capturedTitle = title
		return "✓ Created issue: Hytte-test1234 — Cache forecasts\n", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusBeadCreated {
		t.Fatalf("status: got %q want %q", got.Status, StatusBeadCreated)
	}
	if got.BeadID != "Hytte-test1234" {
		t.Fatalf("bead_id: got %q want Hytte-test1234", got.BeadID)
	}
	if got.BeadCreatedAt == nil {
		t.Fatal("expected bead_created_at to be set")
	}
	if capturedTitle != "Cache forecasts" {
		t.Fatalf("title passed to bd create: got %q want %q", capturedTitle, "Cache forecasts")
	}
	if !strings.Contains(capturedBody, "Do the cache thing.") {
		t.Fatalf("expected plan body in bd create body, got:\n%s", capturedBody)
	}
	if !strings.Contains(capturedBody, "Original body content.") {
		t.Fatalf("expected original suggestion body in bd create body, got:\n%s", capturedBody)
	}
	if !strings.Contains(capturedBody, "Use Redis.") {
		t.Fatalf("expected feedback in bd create body, got:\n%s", capturedBody)
	}
	if !strings.Contains(capturedBody, "## Source") {
		t.Fatalf("expected ## Source heading in body, got:\n%s", capturedBody)
	}
}

func TestCreateBeadHandlerOmitsFeedbackWhenEmpty(t *testing.T) {
	d := setupTestDB(t)

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS,
		Title: "X", Body: "b",
		Plan: "## Scope\nDo it.",
		Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var capturedBody string
	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		capturedBody = body
		return "✓ Created issue: Hytte-abcd1234 — X\n", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(capturedBody, "## User feedback when planning") {
		t.Fatalf("expected no feedback section when feedback is empty, got:\n%s", capturedBody)
	}
}

func TestCreateBeadHandlerBdFailureKeepsPlanned(t *testing.T) {
	d := setupTestDB(t)

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Plan: "p",
		Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		return "", "bd: fatal error: database locked", errors.New("exit status 1")
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on bd failure, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(errResp["error"], "database locked") {
		t.Fatalf("expected error to include trimmed stderr, got %q", errResp["error"])
	}

	// Suggestion must remain in planned state, not bead_created.
	got, err := GetByID(context.Background(), d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusPlanned {
		t.Fatalf("status should remain planned after bd failure, got %q", got.Status)
	}
	if got.BeadID != "" {
		t.Fatalf("bead_id should remain empty after bd failure, got %q", got.BeadID)
	}
	if got.BeadCreatedAt != nil {
		t.Fatalf("bead_created_at should remain nil after bd failure, got %v", got.BeadCreatedAt)
	}
}

func TestCreateBeadHandlerUnparsableStdoutReturns500(t *testing.T) {
	d := setupTestDB(t)

	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Plan: "p",
		Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Successful exit but no bead ID anywhere in stdout.
	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		return "garbage with no recognisable id\n", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when bead ID cannot be parsed, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := GetByID(context.Background(), d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusPlanned {
		t.Fatalf("status should remain planned when parse fails, got %q", got.Status)
	}
}

func TestCreateBeadHandlerRejectsAlreadyBeadCreated(t *testing.T) {
	d := setupTestDB(t)
	id, err := Insert(context.Background(), d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Plan: "p",
		Status: StatusBeadCreated,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	defer withBdCreate(func(ctx context.Context, cwd, title, body string) (string, string, error) {
		t.Fatal("bdCreateFn should not be called when status is already bead_created")
		return "", "", nil
	})()

	router := mountAdmin(CreateBeadHandler(d), http.MethodPost, "/api/suggestions/{id}/bead")
	req := adminContext(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/suggestions/%d/bead", id), nil), 1, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for already-bead_created, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Cap title to MaxBeadTitleLength.
func TestCapTitleTruncatesLongInput(t *testing.T) {
	long := strings.Repeat("a", MaxBeadTitleLength+50)
	got := capTitle(long, MaxBeadTitleLength)
	if len([]rune(got)) > MaxBeadTitleLength {
		t.Fatalf("expected at most %d runes, got %d", MaxBeadTitleLength, len([]rune(got)))
	}
}

func TestBuildBeadBodyOrdersSections(t *testing.T) {
	body := buildBeadBody(Suggestion{
		ID: 7, PageSlug: "weather", Source: SourceClaude,
		Body: "ORIG", Plan: "PLAN", Feedback: "FB",
	})
	planIdx := strings.Index(body, "PLAN")
	srcIdx := strings.Index(body, "## Source")
	fbIdx := strings.Index(body, "## User feedback")
	origIdx := strings.Index(body, "## Original suggestion")
	if planIdx == -1 || srcIdx == -1 || fbIdx == -1 || origIdx == -1 {
		t.Fatalf("missing section in body:\n%s", body)
	}
	if !(planIdx < srcIdx && srcIdx < fbIdx && fbIdx < origIdx) {
		t.Fatalf("unexpected section order in body:\n%s", body)
	}
}

