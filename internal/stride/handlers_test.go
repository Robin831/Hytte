package stride

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

// --- Race handler tests ---

func TestListRacesHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/races", nil), 1)
	rec := httptest.NewRecorder()
	ListRacesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Races []Race `json:"races"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Races) != 0 {
		t.Errorf("expected 0 races, got %d", len(body.Races))
	}
}

func TestCreateRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Bergen City Marathon","date":"2026-10-18","distance_m":42195,"priority":"A","notes":"Goal race"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Race Race `json:"race"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Race.Name != "Bergen City Marathon" {
		t.Errorf("name = %q, want %q", body.Race.Name, "Bergen City Marathon")
	}
	if body.Race.Priority != "A" {
		t.Errorf("priority = %q, want %q", body.Race.Priority, "A")
	}
}

func TestCreateRaceHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateRaceHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date":"2026-10-18","distance_m":42195,"priority":"A"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateRaceHandler_InvalidPriority(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Race","date":"2026-10-18","distance_m":42195,"priority":"D"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/races", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Old Name", "2026-05-01", 10000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	payload := `{"name":"New Name","date":"2026-05-02","distance_m":21097,"priority":"B","notes":"updated"}`
	idStr := strconv.FormatInt(race.ID, 10)
	req := withUser(httptest.NewRequest("PUT", "/api/stride/races/"+idStr, strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Race Race `json:"race"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Race.Name != "New Name" {
		t.Errorf("name = %q, want %q", body.Race.Name, "New Name")
	}
}

func TestUpdateRaceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X","date":"2026-05-01","distance_m":5000,"priority":"C"}`
	req := withUser(httptest.NewRequest("PUT", "/api/stride/races/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteRaceHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Delete Me", "2026-05-01", 5000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(race.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/stride/races/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteRaceHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/stride/races/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteRaceHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- Note handler tests ---

func TestListNotesHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/stride/notes", nil), 1)
	rec := httptest.NewRecorder()
	ListNotesHandler(db).ServeHTTP(rec, req)

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

func TestCreateNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"content":"Feeling strong this week"}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/notes", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Note Note `json:"note"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Note.Content != "Feeling strong this week" {
		t.Errorf("content = %q, want %q", body.Note.Content, "Feeling strong this week")
	}
}

func TestCreateNoteHandler_EmptyContent(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"content":""}`
	req := withUser(httptest.NewRequest("POST", "/api/stride/notes", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Delete me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(note.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/stride/notes/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteNoteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/stride/notes/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteNoteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
