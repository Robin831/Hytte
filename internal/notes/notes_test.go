package notes

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE notes (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title      TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE note_tags (
			note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
			tag     TEXT NOT NULL,
			PRIMARY KEY (note_id, tag)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

func TestCreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Hello", "# Hello World", []string{"go", "test"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if note.Title != "Hello" {
		t.Errorf("title = %q, want %q", note.Title, "Hello")
	}
	if note.Content != "# Hello World" {
		t.Errorf("content = %q, want %q", note.Content, "# Hello World")
	}
	if len(note.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(note.Tags))
	}

	got, err := GetByID(db, note.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != note.ID {
		t.Errorf("id = %d, want %d", got.ID, note.ID)
	}
}

func TestCreateWrongUserCannotGet(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Private", "secret", []string{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = GetByID(db, note.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestList_Empty(t *testing.T) {
	db := setupTestDB(t)

	notes, err := List(db, 1, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(notes))
	}
}

func TestList_Search(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "Go tips", "Use goroutines", []string{"go"}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := Create(db, 1, "Python tricks", "List comprehensions", []string{"python"}); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	notes, err := List(db, 1, "goroutine", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(notes))
	}
	if notes[0].Title != "Go tips" {
		t.Errorf("title = %q, want %q", notes[0].Title, "Go tips")
	}
}

func TestList_TagFilter(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "Note A", "content A", []string{"go", "backend"}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := Create(db, 1, "Note B", "content B", []string{"frontend"}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	notes, err := List(db, 1, "", "go")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(notes))
	}
	if notes[0].Title != "Note A" {
		t.Errorf("title = %q, want %q", notes[0].Title, "Note A")
	}
}

func TestUpdate(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Old Title", "old content", []string{"old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := Update(db, note.ID, 1, "New Title", "new content", []string{"new", "tag"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("title = %q, want %q", updated.Title, "New Title")
	}
	if len(updated.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(updated.Tags))
	}
}

func TestUpdateWrongUser(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Mine", "content", []string{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = Update(db, note.ID, 999, "Hacked", "x", []string{})
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Delete me", "bye", []string{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := Delete(db, note.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = GetByID(db, note.ID, 1)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestDeleteWrongUser(t *testing.T) {
	db := setupTestDB(t)

	note, err := Create(db, 1, "Keep", "content", []string{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = Delete(db, note.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestListTags(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "A", "a", []string{"alpha", "beta"}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := Create(db, 1, "B", "b", []string{"beta", "gamma"}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	tags, err := ListTags(db, 1)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	// Should be sorted: alpha, beta, gamma (beta appears in two notes but only once)
	if len(tags) != 3 {
		t.Fatalf("got %d tags, want 3: %v", len(tags), tags)
	}
}

func TestCascadeDeleteUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "Cascade", "content", []string{"tag"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec("DELETE FROM users WHERE id = 1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	notes, err := List(db, 1, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes after user delete, got %d", len(notes))
	}
}
