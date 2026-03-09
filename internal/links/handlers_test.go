package links

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

// withUser adds an authenticated user to the request context.
func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.NewContext(r.Context(), user)
	return r.WithContext(ctx)
}

// withChiParam adds a chi URL param to the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/links", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Links []Link `json:"links"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Links) != 0 {
		t.Errorf("expected 0 links, got %d", len(body.Links))
	}
}

func TestCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"target_url":"https://example.com","title":"Example"}`
	req := withUser(httptest.NewRequest("POST", "/api/links", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Link Link `json:"link"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Link.TargetURL != "https://example.com" {
		t.Errorf("target_url = %q, want %q", body.Link.TargetURL, "https://example.com")
	}
	if len(body.Link.Code) != 6 {
		t.Errorf("auto code length = %d, want 6", len(body.Link.Code))
	}
}

func TestCreateHandler_MissingURL(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"title":"No URL"}`
	req := withUser(httptest.NewRequest("POST", "/api/links", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateHandler_DuplicateCode(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"target_url":"https://a.com","code":"dup"}`
	req := withUser(httptest.NewRequest("POST", "/api/links", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec.Code)
	}

	// Same code again.
	req = withUser(httptest.NewRequest("POST", "/api/links", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d", rec.Code)
	}
}

func TestCreateHandler_AutoPrefixHTTPS(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"target_url":"example.com"}`
	req := withUser(httptest.NewRequest("POST", "/api/links", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var body struct {
		Link Link `json:"link"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Link.TargetURL != "https://example.com" {
		t.Errorf("target_url = %q, want %q", body.Link.TargetURL, "https://example.com")
	}
}

func TestUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	link, err := Create(db, 1, "upd", "https://old.com", "Old")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	payload := `{"target_url":"https://new.com","title":"New","code":"upd2"}`
	req := withUser(httptest.NewRequest("PUT", "/api/links/1", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Link Link `json:"link"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Link.Code != "upd2" {
		t.Errorf("code = %q, want %q", body.Link.Code, "upd2")
	}

	_ = link // used to create test data
}

func TestUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"target_url":"https://new.com","title":"New","code":"x"}`
	req := withUser(httptest.NewRequest("PUT", "/api/links/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	_, err := Create(db, 1, "del", "https://del.com", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", "/api/links/1", nil), 1)
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/links/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRedirectHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	_, err := Create(db, 1, "rdr", "https://target.com", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest("GET", "/go/rdr", nil)
	req = withChiParam(req, "code", "rdr")
	rec := httptest.NewRecorder()
	RedirectHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://target.com" {
		t.Errorf("Location = %q, want %q", loc, "https://target.com")
	}

	// Verify click was incremented.
	link, _ := GetByCode(db, "rdr")
	if link.Clicks != 1 {
		t.Errorf("clicks = %d, want 1", link.Clicks)
	}
}

func TestRedirectHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest("GET", "/go/nope", nil)
	req = withChiParam(req, "code", "nope")
	rec := httptest.NewRecorder()
	RedirectHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRedirectHandler_InvalidScheme(t *testing.T) {
	db := setupTestDB(t)

	// Manually insert a link with a javascript: scheme to test the safety check.
	_, err := db.Exec(
		"INSERT INTO short_links (user_id, code, target_url, title) VALUES (1, 'evil', 'javascript:alert(1)', '')",
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	req := httptest.NewRequest("GET", "/go/evil", nil)
	req = withChiParam(req, "code", "evil")
	rec := httptest.NewRecorder()
	RedirectHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for javascript: scheme, got %d", rec.Code)
	}
}
