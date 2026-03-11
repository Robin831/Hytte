package notes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/notes", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Notes []Note `json:"notes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(body.Notes))
	}
}

func TestCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"title":"My Note","content":"# Hello","tags":["go","markdown"]}`
	req := withUser(httptest.NewRequest("POST", "/api/notes", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Note Note `json:"note"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Note.Title != "My Note" {
		t.Errorf("title = %q, want %q", body.Note.Title, "My Note")
	}
	if len(body.Note.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(body.Note.Tags))
	}
}

func TestCreateHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/notes", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Get Me", "content", []string{"tag"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/notes/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/notes/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Old", "old content", []string{"old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(note.ID, 10)
	payload := `{"title":"New","content":"new content","tags":["new"]}`
	req := withUser(httptest.NewRequest("PUT", "/api/notes/"+idStr, strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Note Note `json:"note"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Note.Title != "New" {
		t.Errorf("title = %q, want %q", body.Note.Title, "New")
	}
}

func TestUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"title":"X","content":"x","tags":[]}`
	req := withUser(httptest.NewRequest("PUT", "/api/notes/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Delete", "bye", []string{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/notes/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/notes/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTagsHandler(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "A", "a", []string{"foo", "bar"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/notes/tags", nil), 1)
	rec := httptest.NewRecorder()
	TagsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tags) != 2 {
		t.Errorf("got %d tags, want 2: %v", len(body.Tags), body.Tags)
	}
}

func TestListHandler_Search(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "Go notes", "goroutines are great", []string{}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := Create(db, 1, "Python notes", "list comprehensions", []string{}); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/notes?search=goroutine", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Notes []Note `json:"notes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Notes) != 1 {
		t.Errorf("got %d notes, want 1", len(body.Notes))
	}
}
