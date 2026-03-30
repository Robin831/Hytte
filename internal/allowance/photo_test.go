package allowance

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---- saveChorePhoto tests ----

func TestSaveChorePhoto_Success(t *testing.T) {
	dir := t.TempDir()
	origDir := chorePhotosDir
	chorePhotosDir = dir
	defer func() { chorePhotosDir = origDir }()

	data := bytes.Repeat([]byte("x"), 512)
	path, err := saveChorePhoto(42, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("saveChorePhoto: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file not found at %s: %v", path, err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Errorf("file contents mismatch")
	}
}

func TestSaveChorePhoto_ExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	origDir := chorePhotosDir
	chorePhotosDir = dir
	defer func() { chorePhotosDir = origDir }()

	// Write maxPhotoSize+2 bytes — should be rejected.
	data := bytes.Repeat([]byte("y"), maxPhotoSize+2)
	_, err := saveChorePhoto(99, bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for oversized photo, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error message: %v", err)
	}
	// Temp file should have been cleaned up.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("expected temp file to be removed after overflow, found %d files", len(entries))
	}
}

// ---- SetCompletionPhotoPath + round-trip tests ----

func TestSetCompletionPhotoPath(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}

	if err := SetCompletionPhotoPath(db, comp.ID, "data/chore-photos/1.jpg"); err != nil {
		t.Fatalf("SetCompletionPhotoPath: %v", err)
	}

	// Verify stored value via getCompletionPhotoAccess (child has access).
	path, err := getCompletionPhotoAccess(db, comp.ID, 2)
	if err != nil {
		t.Fatalf("getCompletionPhotoAccess: %v", err)
	}
	if path != "data/chore-photos/1.jpg" {
		t.Errorf("expected path %q, got %q", "data/chore-photos/1.jpg", path)
	}

	// Parent also has access.
	path, err = getCompletionPhotoAccess(db, comp.ID, 1)
	if err != nil {
		t.Fatalf("getCompletionPhotoAccess parent: %v", err)
	}
	if path != "data/chore-photos/1.jpg" {
		t.Errorf("expected path %q, got %q", "data/chore-photos/1.jpg", path)
	}
}

func TestGetCompletionPhotoAccess_NoPhoto(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")

	path, err := getCompletionPhotoAccess(db, comp.ID, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestGetCompletionPhotoAccess_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")
	_ = SetCompletionPhotoPath(db, comp.ID, "data/chore-photos/1.jpg")

	// User 99 is not the child or parent.
	_, err := getCompletionPhotoAccess(db, comp.ID, 99)
	if err != ErrCompletionNotFound {
		t.Errorf("expected ErrCompletionNotFound, got %v", err)
	}
}

// ---- CleanOldCompletionPhotos tests ----

func TestCleanOldCompletionPhotos(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	dir := t.TempDir()
	origDir := chorePhotosDir
	chorePhotosDir = dir
	defer func() { chorePhotosDir = origDir }()

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, "2026-01-01", "")

	// Create a real file to clean.
	photoPath, err := saveChorePhoto(comp.ID, bytes.NewReader([]byte("img")))
	if err != nil {
		t.Fatalf("saveChorePhoto: %v", err)
	}
	_ = SetCompletionPhotoPath(db, comp.ID, photoPath)

	// Back-date created_at so it falls outside the 7-day window.
	old := time.Now().UTC().AddDate(0, 0, -(photoMaxAgeDays + 1)).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE allowance_completions SET created_at = ? WHERE id = ?`, old, comp.ID); err != nil {
		t.Fatalf("back-date: %v", err)
	}

	CleanOldCompletionPhotos(db)

	// File should be removed.
	if _, err := os.Stat(photoPath); !os.IsNotExist(err) {
		t.Errorf("expected photo file to be deleted, stat err: %v", err)
	}

	// DB should have cleared photo_path.
	var path string
	_ = db.QueryRow(`SELECT photo_path FROM allowance_completions WHERE id = ?`, comp.ID).Scan(&path)
	if path != "" {
		t.Errorf("expected empty photo_path in DB, got %q", path)
	}
}

// ---- ServePhotoHandler tests ----

func TestServePhotoHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	dir := t.TempDir()
	origDir := chorePhotosDir
	chorePhotosDir = dir
	defer func() { chorePhotosDir = origDir }()

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")

	imgData := []byte("\xff\xd8\xff\xe0" + strings.Repeat("x", 100)) // minimal JPEG header
	photoPath, _ := saveChorePhoto(comp.ID, bytes.NewReader(imgData))
	_ = SetCompletionPhotoPath(db, comp.ID, photoPath)

	req := httptest.NewRequest(http.MethodGet, "/api/allowance/photos/1", nil)
	req = withUser(req, testChild)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("completion_id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	ServePhotoHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct == "" {
		t.Error("expected Content-Type header")
	}
}

func TestServePhotoHandler_NoPhoto(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")

	req := httptest.NewRequest(http.MethodGet, "/api/allowance/photos/1", nil)
	req = withUser(req, testChild)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("completion_id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	_ = comp // suppress unused
	rr := httptest.NewRecorder()
	ServePhotoHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestServePhotoHandler_MissingFile(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, _ := CreateChore(db, 1, int64Ptr(2), "Test", "", 5, "daily", "🧹", false, "solo", 2, 0)
	comp, _ := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")
	_ = SetCompletionPhotoPath(db, comp.ID, "data/chore-photos/1.jpg")

	// Parent has access but the file is not on disk — expect 404 for missing file.
	req := httptest.NewRequest(http.MethodGet, "/api/allowance/photos/1", nil)
	req = withUser(req, testParent)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("completion_id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	ServePhotoHandler(db).ServeHTTP(rr, req)

	// photo_path is set but file doesn't exist on disk — expect 404 for missing file.
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 (file not on disk), got %d", rr.Code)
	}
}

func TestServePhotoHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/allowance/photos/bad", nil)
	req = withUser(req, testChild)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("completion_id", "bad")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	ServePhotoHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// int64Ptr is a test helper.
func int64Ptr(v int64) *int64 { return &v }
