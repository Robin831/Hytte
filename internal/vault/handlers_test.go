package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
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

	req := withUser(httptest.NewRequest("GET", "/api/vault/files", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Files []File `json:"files"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(body.Files))
	}
}

func TestListHandler_WithFiles(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "a.txt", "text/plain", "docs", "private", []string{}, []byte("aaa"))
	_, _ = Create(db, 1, "b.txt", "text/plain", "", "private", []string{}, []byte("bbb"))

	req := withUser(httptest.NewRequest("GET", "/api/vault/files", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Files []File `json:"files"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(body.Files))
	}
}

func TestGetHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	f, err := Create(db, 1, "test.txt", "text/plain", "", "private", []string{"tag1"}, []byte("hello"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	idStr := strconv.FormatInt(f.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/vault/files/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/vault/files/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/vault/files/abc", nil), 1)
	req = withChiParam(req, "id", "abc")
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUploadHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "upload.txt")
	fw.Write([]byte("file content here"))
	w.WriteField("folder", "uploads")
	w.WriteField("access", "private")
	w.WriteField("tags", "test,upload")
	w.Close()

	req := withUser(httptest.NewRequest("POST", "/api/vault/files", &buf), 1)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	UploadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		File File `json:"file"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.File.Filename != "upload.txt" {
		t.Errorf("filename = %q, want %q", body.File.Filename, "upload.txt")
	}
	if body.File.Folder != "uploads" {
		t.Errorf("folder = %q, want %q", body.File.Folder, "uploads")
	}
	if len(body.File.Tags) != 2 {
		t.Errorf("tags = %v, want 2 tags", body.File.Tags)
	}
}

func TestUploadHandler_NoFile(t *testing.T) {
	db := setupTestDB(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.Close()

	req := withUser(httptest.NewRequest("POST", "/api/vault/files", &buf), 1)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	UploadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadHandler_InvalidAccess(t *testing.T) {
	db := setupTestDB(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "test.txt")
	fw.Write([]byte("data"))
	w.WriteField("access", "public")
	w.Close()

	req := withUser(httptest.NewRequest("POST", "/api/vault/files", &buf), 1)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	UploadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "old.txt", "text/plain", "", "private", []string{}, []byte("data"))

	idStr := strconv.FormatInt(f.ID, 10)
	payload := `{"filename":"new.txt","folder":"moved","access":"shared","tags":["a","b"]}`
	req := withUser(httptest.NewRequest("PUT", "/api/vault/files/"+idStr, strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		File File `json:"file"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.File.Filename != "new.txt" {
		t.Errorf("filename = %q, want %q", body.File.Filename, "new.txt")
	}
}

func TestUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"filename":"x.txt","folder":"","access":"private","tags":[]}`
	req := withUser(httptest.NewRequest("PUT", "/api/vault/files/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("PUT", "/api/vault/files/1", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateHandler_EmptyFilename(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "test.txt", "text/plain", "", "private", []string{}, []byte("data"))
	idStr := strconv.FormatInt(f.ID, 10)

	payload := `{"filename":"","folder":"","access":"private","tags":[]}`
	req := withUser(httptest.NewRequest("PUT", "/api/vault/files/"+idStr, strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "bye.txt", "text/plain", "", "private", []string{}, []byte("data"))

	idStr := strconv.FormatInt(f.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/vault/files/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/vault/files/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestFoldersHandler(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "a.txt", "text/plain", "alpha", "private", []string{}, []byte("a"))
	_, _ = Create(db, 1, "b.txt", "text/plain", "beta", "private", []string{}, []byte("b"))

	req := withUser(httptest.NewRequest("GET", "/api/vault/folders", nil), 1)
	rec := httptest.NewRecorder()
	FoldersHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Folders []string `json:"folders"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Folders) != 2 {
		t.Errorf("got %d folders, want 2", len(body.Folders))
	}
}

func TestTagsHandler(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "a.txt", "text/plain", "", "private", []string{"foo", "bar"}, []byte("a"))

	req := withUser(httptest.NewRequest("GET", "/api/vault/tags", nil), 1)
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

func TestDownloadHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	content := []byte("download me")
	f, _ := Create(db, 1, "dl.txt", "text/plain", "", "private", []string{}, content)

	idStr := strconv.FormatInt(f.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/vault/files/"+idStr+"/download", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DownloadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "download me" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "download me")
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Error("expected Content-Disposition to contain 'attachment'")
	}
}

func TestDownloadHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/vault/files/999/download", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DownloadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPreviewHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	content := []byte("fake png data")
	f, err := Create(db, 1, "test.png", "image/png", "", "private", []string{}, content)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	idStr := strconv.FormatInt(f.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/vault/files/"+idStr+"/preview", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PreviewHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), "image/png")
	}
	if rec.Header().Get("Content-Disposition") != "inline" {
		t.Errorf("Content-Disposition = %q, want %q", rec.Header().Get("Content-Disposition"), "inline")
	}
	if rec.Body.String() != string(content) {
		t.Errorf("body = %q, want %q", rec.Body.String(), string(content))
	}
}

func TestPreviewHandler_NotPreviewable(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "data.bin", "application/octet-stream", "", "private", []string{}, []byte("binary"))

	idStr := strconv.FormatInt(f.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/vault/files/"+idStr+"/preview", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PreviewHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
