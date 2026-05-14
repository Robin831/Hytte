package tasks

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE users (
	id         INTEGER PRIMARY KEY,
	email      TEXT UNIQUE NOT NULL,
	name       TEXT NOT NULL,
	picture    TEXT NOT NULL DEFAULT '',
	google_id  TEXT UNIQUE NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	is_admin   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE tasks (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title_enc     TEXT NOT NULL,
	body_enc      TEXT NOT NULL DEFAULT '',
	archived      INTEGER NOT NULL DEFAULT 0,
	created_at    TIMESTAMP NOT NULL,
	updated_at    TIMESTAMP NOT NULL,
	archived_at   TIMESTAMP
);
CREATE INDEX idx_tasks_user_archived ON tasks(user_id, archived);
CREATE TABLE task_tags (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	label_enc  TEXT NOT NULL,
	UNIQUE(user_id, label_enc)
);
CREATE TABLE task_tag_assignments (
	task_id  INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	tag_id   INTEGER NOT NULL REFERENCES task_tags(id) ON DELETE CASCADE,
	PRIMARY KEY (task_id, tag_id)
);
CREATE TABLE task_notes (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	content_enc TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_task_notes_task ON task_notes(task_id, created_at);
`

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-tasks-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'a@example.com', 'Alice', 'g1'), (2, 'b@example.com', 'Bob', 'g2')`); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	return db
}

func TestCreateAndGetTask(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Buy milk", "2 percent", []string{"shopping", "home"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Title != "Buy milk" {
		t.Errorf("title = %q, want %q", task.Title, "Buy milk")
	}
	if task.Body != "2 percent" {
		t.Errorf("body = %q, want %q", task.Body, "2 percent")
	}
	if task.Archived {
		t.Error("new task should not be archived")
	}
	if len(task.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(task.Tags))
	}
	if task.NoteCount != 0 {
		t.Errorf("note count = %d, want 0", task.NoteCount)
	}

	got, err := GetTask(db, task.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != task.ID || got.Title != "Buy milk" {
		t.Errorf("get mismatch: %+v", got)
	}
}

func TestGetTask_NotFoundForOtherUser(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Private", "secret", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := GetTask(db, task.ID, 2); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestListTasks_ActiveAndArchived(t *testing.T) {
	db := setupTestDB(t)

	active, err := CreateTask(db, 1, "Active", "", nil)
	if err != nil {
		t.Fatalf("create active: %v", err)
	}
	archived, err := CreateTask(db, 1, "Archived", "", nil)
	if err != nil {
		t.Fatalf("create archived: %v", err)
	}
	val := true
	if _, err := UpdateTask(db, archived.ID, 1, TaskUpdate{Archived: &val}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	activeList, err := ListTasks(db, 1, false)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(activeList) != 1 || activeList[0].ID != active.ID {
		t.Errorf("active list = %+v, want single active task", activeList)
	}

	archivedList, err := ListTasks(db, 1, true)
	if err != nil {
		t.Fatalf("list archived: %v", err)
	}
	if len(archivedList) != 1 || archivedList[0].ID != archived.ID {
		t.Errorf("archived list = %+v, want single archived task", archivedList)
	}
	if archivedList[0].ArchivedAt == nil {
		t.Error("archived_at should be set after archiving")
	}
}

func TestUpdateTask_PartialFields(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Original", "original body", []string{"old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newTitle := "Renamed"
	updated, err := UpdateTask(db, task.ID, 1, TaskUpdate{Title: &newTitle})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Errorf("title = %q, want %q", updated.Title, "Renamed")
	}
	if updated.Body != "original body" {
		t.Errorf("body should be unchanged, got %q", updated.Body)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "old" {
		t.Errorf("tags should be unchanged, got %v", updated.Tags)
	}
}

func TestUpdateTask_ArchiveUnarchiveSetsArchivedAt(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Toggle", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	on := true
	archived, err := UpdateTask(db, task.ID, 1, TaskUpdate{Archived: &on})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Archived || archived.ArchivedAt == nil {
		t.Errorf("archive: archived=%v archived_at=%v", archived.Archived, archived.ArchivedAt)
	}

	off := false
	unarchived, err := UpdateTask(db, task.ID, 1, TaskUpdate{Archived: &off})
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if unarchived.Archived || unarchived.ArchivedAt != nil {
		t.Errorf("unarchive: archived=%v archived_at=%v", unarchived.Archived, unarchived.ArchivedAt)
	}
}

func TestUpdateTask_OtherUserGetsNotFound(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Mine", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newTitle := "Hacked"
	_, err = UpdateTask(db, task.ID, 2, TaskUpdate{Title: &newTitle})
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_CascadesNotesAndAssignments(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Cascade", "", []string{"alpha"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := AddNote(db, task.ID, 1, "Note content"); err != nil {
		t.Fatalf("add note: %v", err)
	}

	if err := DeleteTask(db, task.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var assignmentCount, noteCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_tag_assignments WHERE task_id = ?`, task.ID).Scan(&assignmentCount); err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if assignmentCount != 0 {
		t.Errorf("assignments = %d, want 0", assignmentCount)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_notes WHERE task_id = ?`, task.ID).Scan(&noteCount); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if noteCount != 0 {
		t.Errorf("notes = %d, want 0", noteCount)
	}

	// The tag itself is kept for reuse.
	var tagCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_tags WHERE user_id = 1`).Scan(&tagCount); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tagCount != 1 {
		t.Errorf("tags = %d, want 1 (preserved for reuse)", tagCount)
	}
}

func TestAddNoteAndDeleteNote(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	note, err := AddNote(db, task.ID, 1, "First note")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if note.Content != "First note" {
		t.Errorf("note content = %q", note.Content)
	}

	notes, err := ListNotes(db, task.ID, 1)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Content != "First note" {
		t.Errorf("list = %+v", notes)
	}

	if err := DeleteNote(db, task.ID, note.ID, 1); err != nil {
		t.Fatalf("delete note: %v", err)
	}

	notes, err = ListNotes(db, task.ID, 1)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected empty notes after delete, got %v", notes)
	}
}

func TestAddNote_TaskOwnedByOtherUser(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Theirs", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = AddNote(db, task.ID, 2, "Sneaky")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteNote_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Owner", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	note, err := AddNote(db, task.ID, 1, "Mine")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if err := DeleteNote(db, task.ID, note.ID, 2); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestTagsReusedAcrossTasks(t *testing.T) {
	db := setupTestDB(t)

	t1, err := CreateTask(db, 1, "First", "", []string{"shared", "one"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	t2, err := CreateTask(db, 1, "Second", "", []string{"shared", "two"})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	// Three distinct tag rows: "shared", "one", "two".
	var tagCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_tags WHERE user_id = 1`).Scan(&tagCount); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tagCount != 3 {
		t.Errorf("task_tags rows = %d, want 3", tagCount)
	}

	// Same tag_id must back the "shared" label on both tasks.
	var sharedTagID1, sharedTagID2 int64
	row := db.QueryRow(
		`SELECT a.tag_id FROM task_tag_assignments a
		 JOIN task_tags t ON t.id = a.tag_id
		 WHERE a.task_id = ? AND t.user_id = 1
		 ORDER BY a.tag_id`,
		t1.ID,
	)
	if err := row.Scan(&sharedTagID1); err != nil {
		t.Fatalf("scan tag id 1: %v", err)
	}
	row = db.QueryRow(
		`SELECT a.tag_id FROM task_tag_assignments a
		 JOIN task_tags t ON t.id = a.tag_id
		 WHERE a.task_id = ? AND t.user_id = 1
		 ORDER BY a.tag_id`,
		t2.ID,
	)
	if err := row.Scan(&sharedTagID2); err != nil {
		t.Fatalf("scan tag id 2: %v", err)
	}
	// Since the lowest-ID tag is "shared" (inserted first), both should match.
	if sharedTagID1 != sharedTagID2 {
		t.Errorf("expected shared tag to be reused (id1=%d id2=%d)", sharedTagID1, sharedTagID2)
	}
}

func TestCrossUserIsolation(t *testing.T) {
	db := setupTestDB(t)

	a, err := CreateTask(db, 1, "Alice task", "", nil)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := CreateTask(db, 2, "Bob task", "", nil); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	bobList, err := ListTasks(db, 2, false)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	for _, task := range bobList {
		if task.ID == a.ID {
			t.Errorf("bob should not see alice's task %d", a.ID)
		}
	}

	if err := DeleteTask(db, a.ID, 2); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("bob deleting alice's task: expected ErrTaskNotFound, got %v", err)
	}
}

func TestEncryptionRoundTrip_RawDBIsCiphertext(t *testing.T) {
	db := setupTestDB(t)

	task, err := CreateTask(db, 1, "Sensitive title", "Sensitive body", []string{"private"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := AddNote(db, task.ID, 1, "Confidential"); err != nil {
		t.Fatalf("add note: %v", err)
	}

	var rawTitle, rawBody string
	if err := db.QueryRow(`SELECT title_enc, body_enc FROM tasks WHERE id = ?`, task.ID).Scan(&rawTitle, &rawBody); err != nil {
		t.Fatalf("raw select tasks: %v", err)
	}
	if !strings.HasPrefix(rawTitle, "enc:") || strings.Contains(rawTitle, "Sensitive title") {
		t.Errorf("title_enc should be ciphertext, got %q", rawTitle)
	}
	if !strings.HasPrefix(rawBody, "enc:") || strings.Contains(rawBody, "Sensitive body") {
		t.Errorf("body_enc should be ciphertext, got %q", rawBody)
	}

	var rawLabel string
	if err := db.QueryRow(`SELECT label_enc FROM task_tags WHERE user_id = 1`).Scan(&rawLabel); err != nil {
		t.Fatalf("raw select tag: %v", err)
	}
	if !strings.HasPrefix(rawLabel, "enc:") || strings.Contains(rawLabel, "private") {
		t.Errorf("label_enc should be ciphertext, got %q", rawLabel)
	}

	var rawNote string
	if err := db.QueryRow(`SELECT content_enc FROM task_notes WHERE task_id = ?`, task.ID).Scan(&rawNote); err != nil {
		t.Fatalf("raw select note: %v", err)
	}
	if !strings.HasPrefix(rawNote, "enc:") || strings.Contains(rawNote, "Confidential") {
		t.Errorf("content_enc should be ciphertext, got %q", rawNote)
	}

	// GetTask must return plaintext.
	got, err := GetTask(db, task.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Sensitive title" || got.Body != "Sensitive body" {
		t.Errorf("decrypted task mismatch: %+v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "private" {
		t.Errorf("decrypted tags: %v", got.Tags)
	}
	notes, err := ListNotes(db, task.ID, 1)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Content != "Confidential" {
		t.Errorf("decrypted note: %+v", notes)
	}
}
