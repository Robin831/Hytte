package vault

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
		CREATE TABLE vault_files (
			id           INTEGER PRIMARY KEY,
			user_id      INTEGER NOT NULL,
			filename     TEXT NOT NULL DEFAULT '',
			mime_type    TEXT NOT NULL DEFAULT '',
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			folder       TEXT NOT NULL DEFAULT '',
			access       TEXT NOT NULL DEFAULT 'private',
			content_hash TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL DEFAULT '',
			updated_at   TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE vault_file_tags (
			file_id INTEGER NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
			tag     TEXT NOT NULL,
			PRIMARY KEY (file_id, tag)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Use a test encryption key.
	os.Setenv("ENCRYPTION_KEY", "test-vault-key-for-unit-tests-42")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() {
		os.Unsetenv("ENCRYPTION_KEY")
		encryption.ResetEncryptionKey()
	})

	// Use a temp dir for file storage.
	tmpDir := t.TempDir()
	os.Setenv("VAULT_STORAGE_DIR", tmpDir)
	t.Cleanup(func() {
		os.Unsetenv("VAULT_STORAGE_DIR")
	})

	return db
}

func TestCreateAndGet(t *testing.T) {
	db := setupTestDB(t)

	f, err := Create(db, 1, "test.txt", "text/plain", "docs", "private", []string{"important"}, []byte("hello world"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.Filename != "test.txt" {
		t.Errorf("filename = %q, want %q", f.Filename, "test.txt")
	}
	if f.SizeBytes != 11 {
		t.Errorf("size = %d, want 11", f.SizeBytes)
	}
	if f.Folder != "docs" {
		t.Errorf("folder = %q, want %q", f.Folder, "docs")
	}
	if len(f.Tags) != 1 || f.Tags[0] != "important" {
		t.Errorf("tags = %v, want [important]", f.Tags)
	}

	got, err := Get(db, 1, f.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Filename != "test.txt" {
		t.Errorf("Get filename = %q, want %q", got.Filename, "test.txt")
	}
}

func TestDownloadRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	content := []byte("encrypted vault content test")
	f, err := Create(db, 1, "secret.bin", "application/octet-stream", "", "private", nil, content)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data, err := Download(1, f.ID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("downloaded content = %q, want %q", string(data), string(content))
	}
}

func TestListWithFilters(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "doc1.pdf", "application/pdf", "docs", "private", []string{"work"}, []byte("pdf1"))
	_, _ = Create(db, 1, "photo.jpg", "image/jpeg", "photos", "private", []string{"vacation"}, []byte("jpg1"))
	_, _ = Create(db, 1, "doc2.pdf", "application/pdf", "docs", "private", []string{"work"}, []byte("pdf2"))

	// List all.
	files, err := List(db, 1, "", "", "")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("List all returned %d files, want 3", len(files))
	}

	// Filter by folder.
	files, err = List(db, 1, "docs", "", "")
	if err != nil {
		t.Fatalf("List by folder: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("List by folder returned %d files, want 2", len(files))
	}

	// Filter by tag.
	files, err = List(db, 1, "", "vacation", "")
	if err != nil {
		t.Fatalf("List by tag: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("List by tag returned %d files, want 1", len(files))
	}

	// Search.
	files, err = List(db, 1, "", "", "photo")
	if err != nil {
		t.Fatalf("List by search: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("List by search returned %d files, want 1", len(files))
	}
}

func TestUpdateFile(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "old.txt", "text/plain", "", "private", nil, []byte("data"))

	updated, err := Update(db, 1, f.ID, "new.txt", "renamed", "shared", []string{"tag1", "tag2"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated == nil {
		t.Fatal("Update returned nil")
	}
	if updated.Filename != "new.txt" {
		t.Errorf("filename = %q, want %q", updated.Filename, "new.txt")
	}
	if updated.Folder != "renamed" {
		t.Errorf("folder = %q, want %q", updated.Folder, "renamed")
	}
	if updated.Access != "shared" {
		t.Errorf("access = %q, want %q", updated.Access, "shared")
	}
	if len(updated.Tags) != 2 {
		t.Errorf("tags = %v, want 2 tags", updated.Tags)
	}
}

func TestDeleteFile(t *testing.T) {
	db := setupTestDB(t)

	f, _ := Create(db, 1, "delete-me.txt", "text/plain", "", "private", nil, []byte("data"))

	if err := Delete(db, 1, f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := Get(db, 1, f.ID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Error("file still exists after delete")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	db := setupTestDB(t)

	err := Delete(db, 1, 9999)
	if err != sql.ErrNoRows {
		t.Errorf("Delete non-existent = %v, want sql.ErrNoRows", err)
	}
}

func TestListFoldersAndTags(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "a.txt", "text/plain", "alpha", "private", []string{"x", "y"}, []byte("a"))
	_, _ = Create(db, 1, "b.txt", "text/plain", "beta", "private", []string{"y", "z"}, []byte("b"))

	folders, err := ListFolders(db, 1)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 2 {
		t.Errorf("folders = %v, want 2", folders)
	}

	tags, err := ListTags(db, 1)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("tags = %v, want 3 (x, y, z)", tags)
	}
}

func TestUserIsolation(t *testing.T) {
	db := setupTestDB(t)

	_, _ = Create(db, 1, "user1.txt", "text/plain", "", "private", nil, []byte("u1"))
	_, _ = Create(db, 2, "user2.txt", "text/plain", "", "private", nil, []byte("u2"))

	files, _ := List(db, 1, "", "", "")
	if len(files) != 1 {
		t.Errorf("user 1 files = %d, want 1", len(files))
	}
	if files[0].Filename != "user1.txt" {
		t.Errorf("user 1 file = %q, want %q", files[0].Filename, "user1.txt")
	}
}
