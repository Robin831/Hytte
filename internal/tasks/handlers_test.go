package tasks

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
)

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParams(r *http.Request, kv map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range kv {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	payload := `{"title":"Buy bread","body":"sourdough","tags":["shopping"]}`
	req := withUser(httptest.NewRequest("POST", "/api/tasks", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Task Task `json:"task"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Task.Title != "Buy bread" || body.Task.Body != "sourdough" {
		t.Errorf("task = %+v", body.Task)
	}
}

func TestCreateHandler_MissingTitle(t *testing.T) {
	db := setupTestDB(t)
	req := withUser(httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"title":"   "}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	req := withUser(httptest.NewRequest("POST", "/api/tasks", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListHandler_FilterByArchived(t *testing.T) {
	db := setupTestDB(t)
	active, err := CreateTask(db, 1, "Active", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	archived, err := CreateTask(db, 1, "Archived", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	on := true
	if _, err := UpdateTask(db, archived.ID, 1, TaskUpdate{Archived: &on}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	for _, tc := range []struct {
		query   string
		wantID  int64
		wantLen int
	}{
		{"", active.ID, 1},
		{"archived=false", active.ID, 1},
		{"archived=true", archived.ID, 1},
	} {
		req := withUser(httptest.NewRequest("GET", "/api/tasks?"+tc.query, nil), 1)
		rec := httptest.NewRecorder()
		ListHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q: status = %d", tc.query, rec.Code)
		}
		var body struct {
			Tasks []Task `json:"tasks"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Tasks) != tc.wantLen {
			t.Errorf("query %q: got %d tasks, want %d", tc.query, len(body.Tasks), tc.wantLen)
		}
		if len(body.Tasks) > 0 && body.Tasks[0].ID != tc.wantID {
			t.Errorf("query %q: got id %d, want %d", tc.query, body.Tasks[0].ID, tc.wantID)
		}
	}
}

func TestListHandler_InvalidArchivedParam(t *testing.T) {
	db := setupTestDB(t)
	req := withUser(httptest.NewRequest("GET", "/api/tasks?archived=maybe", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateHandler_PartialPatch(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Old", "old body", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("PATCH", "/api/tasks/"+idStr, strings.NewReader(`{"body":"new body"}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Task Task `json:"task"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Task.Title != "Old" {
		t.Errorf("title should be unchanged, got %q", body.Task.Title)
	}
	if body.Task.Body != "new body" {
		t.Errorf("body = %q, want %q", body.Task.Body, "new body")
	}
}

func TestUpdateHandler_EmptyTitleRejected(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Title", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("PATCH", "/api/tasks/"+idStr, strings.NewReader(`{"title":"   "}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateHandler_NotFoundForOtherUser(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Mine", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("PATCH", "/api/tasks/"+idStr, strings.NewReader(`{"title":"hacked"}`)), 2)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Delete me", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/tasks/"+idStr, nil), 1)
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteHandler_NotFoundForOtherUser(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Hers", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/tasks/"+idStr, nil), 2)
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAddNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/tasks/"+idStr+"/notes", strings.NewReader(`{"content":"hello"}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	AddNoteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddNoteHandler_EmptyContent(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/tasks/"+idStr+"/notes", strings.NewReader(`{"content":"   "}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	AddNoteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddNoteHandler_TaskOwnedByAnotherUser(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/tasks/"+idStr+"/notes", strings.NewReader(`{"content":"hi"}`)), 2)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr})
	rec := httptest.NewRecorder()
	AddNoteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	note, err := AddNote(db, task.ID, 1, "Goodbye")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	idStr := strconv.FormatInt(task.ID, 10)
	noteStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/tasks/"+idStr+"/notes/"+noteStr, nil), 1)
	req = withChiParams(req, map[string]string{"id": idStr, "note_id": noteStr})
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_NoteFromOtherTask(t *testing.T) {
	db := setupTestDB(t)
	t1, err := CreateTask(db, 1, "One", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t2, err := CreateTask(db, 1, "Two", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	note, err := AddNote(db, t1.ID, 1, "for t1")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	t2Str := strconv.FormatInt(t2.ID, 10)
	noteStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/tasks/"+t2Str+"/notes/"+noteStr, nil), 1)
	req = withChiParams(req, map[string]string{"id": t2Str, "note_id": noteStr})
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
