package tasks

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Task is a single user-owned task with optional tags and notes.
type Task struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Archived   bool       `json:"archived"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Tags       []string   `json:"tags"`
	NoteCount  int        `json:"note_count"`
}

// TaskTag is a per-user tag label used to group tasks.
type TaskTag struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Label  string `json:"label"`
}

// TaskNote is a free-text note attached to a task.
type TaskNote struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrTaskNotFound signals that no task matched the requested ID + user pair.
var ErrTaskNotFound = errors.New("task not found")

// ErrNoteNotFound signals that no note matched the requested task + note IDs.
var ErrNoteNotFound = errors.New("task note not found")

// formatTimestamp renders a time.Time for storage in a TIMESTAMP column.
// The codebase historically stores times as RFC3339 strings; this keeps the
// roundtrip predictable across the modernc.org/sqlite driver and tests.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTimestamp parses a database-stored timestamp back to time.Time. It is
// permissive about the exact RFC3339 sub-format (with or without sub-second
// precision) so callers do not need to track which writer produced the value.
func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time string")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

// CreateTask inserts a new task and, if tags are provided, upserts and assigns them.
func CreateTask(db *sql.DB, userID int64, title, body string, tags []string) (*Task, error) {
	now := time.Now().UTC()
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, fmt.Errorf("encrypt title: %w", err)
	}
	encBody, err := encryption.EncryptField(body)
	if err != nil {
		return nil, fmt.Errorf("encrypt body: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	nowStr := formatTimestamp(now)
	res, err := tx.Exec(
		`INSERT INTO tasks (user_id, title_enc, body_enc, archived, created_at, updated_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		userID, encTitle, encBody, nowStr, nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	if err := setTaskTags(tx, userID, id, tags); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetTask(db, id, userID)
}

// GetTask returns a single task scoped to the owner, with its tags and note count.
func GetTask(db *sql.DB, id, userID int64) (*Task, error) {
	var (
		t          Task
		encTitle   string
		encBody    string
		archived   int
		createdAt  string
		updatedAt  string
		archivedAt sql.NullString
	)
	err := db.QueryRow(
		`SELECT id, user_id, title_enc, body_enc, archived, created_at, updated_at, archived_at
		 FROM tasks WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&t.ID, &t.UserID, &encTitle, &encBody, &archived, &createdAt, &updatedAt, &archivedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	if t.Title, err = encryption.DecryptField(encTitle); err != nil {
		return nil, fmt.Errorf("decrypt title: %w", err)
	}
	if t.Body, err = encryption.DecryptField(encBody); err != nil {
		return nil, fmt.Errorf("decrypt body: %w", err)
	}
	t.Archived = archived != 0
	if t.CreatedAt, err = parseTimestamp(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if t.UpdatedAt, err = parseTimestamp(updatedAt); err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	if archivedAt.Valid && archivedAt.String != "" {
		v, perr := parseTimestamp(archivedAt.String)
		if perr != nil {
			return nil, fmt.Errorf("parse archived_at: %w", perr)
		}
		t.ArchivedAt = &v
	}

	tags, err := listTagsForTask(db, id)
	if err != nil {
		return nil, err
	}
	t.Tags = tags

	count, err := countNotesForTask(db, id)
	if err != nil {
		return nil, err
	}
	t.NoteCount = count

	return &t, nil
}

// ListTasks returns all tasks for the user filtered by archived state.
func ListTasks(db *sql.DB, userID int64, archived bool) ([]Task, error) {
	archInt := 0
	if archived {
		archInt = 1
	}
	rows, err := db.Query(
		`SELECT id, user_id, title_enc, body_enc, archived, created_at, updated_at, archived_at
		 FROM tasks WHERE user_id = ? AND archived = ?
		 ORDER BY updated_at DESC`,
		userID, archInt,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var (
			t          Task
			encTitle   string
			encBody    string
			arch       int
			createdAt  string
			updatedAt  string
			archivedAt sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.UserID, &encTitle, &encBody, &arch, &createdAt, &updatedAt, &archivedAt); err != nil {
			return nil, err
		}
		if t.Title, err = encryption.DecryptField(encTitle); err != nil {
			return nil, fmt.Errorf("decrypt title: %w", err)
		}
		if t.Body, err = encryption.DecryptField(encBody); err != nil {
			return nil, fmt.Errorf("decrypt body: %w", err)
		}
		t.Archived = arch != 0
		if t.CreatedAt, err = parseTimestamp(createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		if t.UpdatedAt, err = parseTimestamp(updatedAt); err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
		if archivedAt.Valid && archivedAt.String != "" {
			v, perr := parseTimestamp(archivedAt.String)
			if perr != nil {
				return nil, fmt.Errorf("parse archived_at: %w", perr)
			}
			t.ArchivedAt = &v
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range tasks {
		tags, err := listTagsForTask(db, tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i].Tags = tags
		count, err := countNotesForTask(db, tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i].NoteCount = count
	}

	if tasks == nil {
		tasks = []Task{}
	}
	return tasks, nil
}

// TaskUpdate is a partial update payload. Nil pointers leave the corresponding
// column untouched.
type TaskUpdate struct {
	Title    *string
	Body     *string
	Tags     *[]string
	Archived *bool
}

// UpdateTask applies the given partial update to a task scoped to the user.
func UpdateTask(db *sql.DB, id, userID int64, upd TaskUpdate) (*Task, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var currentArchived int
	if err := tx.QueryRow(`SELECT archived FROM tasks WHERE id = ? AND user_id = ?`, id, userID).Scan(&currentArchived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := formatTimestamp(now)
	setClauses := []string{"updated_at = ?"}
	args := []any{nowStr}

	if upd.Title != nil {
		enc, err := encryption.EncryptField(*upd.Title)
		if err != nil {
			return nil, fmt.Errorf("encrypt title: %w", err)
		}
		setClauses = append(setClauses, "title_enc = ?")
		args = append(args, enc)
	}
	if upd.Body != nil {
		enc, err := encryption.EncryptField(*upd.Body)
		if err != nil {
			return nil, fmt.Errorf("encrypt body: %w", err)
		}
		setClauses = append(setClauses, "body_enc = ?")
		args = append(args, enc)
	}
	if upd.Archived != nil {
		newArchived := 0
		if *upd.Archived {
			newArchived = 1
		}
		setClauses = append(setClauses, "archived = ?")
		args = append(args, newArchived)
		// Set archived_at when flipping to archived; clear it when unarchiving.
		if newArchived == 1 && currentArchived == 0 {
			setClauses = append(setClauses, "archived_at = ?")
			args = append(args, nowStr)
		} else if newArchived == 0 && currentArchived == 1 {
			setClauses = append(setClauses, "archived_at = NULL")
		}
	}

	args = append(args, id, userID)
	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ? AND user_id = ?", strings.Join(setClauses, ", "))
	res, err := tx.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, ErrTaskNotFound
	}

	if upd.Tags != nil {
		if err := setTaskTags(tx, userID, id, *upd.Tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetTask(db, id, userID)
}

// DeleteTask removes a task and (via ON DELETE CASCADE) its tag assignments and notes.
func DeleteTask(db *sql.DB, id, userID int64) error {
	res, err := db.Exec(`DELETE FROM tasks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// AddNote appends a free-text note to an existing task. The task must belong
// to the given user — returns ErrTaskNotFound otherwise.
func AddNote(db *sql.DB, taskID, userID int64, content string) (*TaskNote, error) {
	if err := ensureTaskOwned(db, taskID, userID); err != nil {
		return nil, err
	}
	enc, err := encryption.EncryptField(content)
	if err != nil {
		return nil, fmt.Errorf("encrypt note content: %w", err)
	}
	now := time.Now().UTC()
	res, err := db.Exec(
		`INSERT INTO task_notes (task_id, content_enc, created_at) VALUES (?, ?, ?)`,
		taskID, enc, formatTimestamp(now),
	)
	if err != nil {
		return nil, fmt.Errorf("insert note: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return &TaskNote{ID: id, TaskID: taskID, Content: content, CreatedAt: now}, nil
}

// DeleteNote removes a note owned by the given user (via the task ownership check).
func DeleteNote(db *sql.DB, taskID, noteID, userID int64) error {
	if err := ensureTaskOwned(db, taskID, userID); err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM task_notes WHERE id = ? AND task_id = ?`, noteID, taskID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNoteNotFound
	}
	return nil
}

// ListNotes returns every note attached to a task, oldest first.
func ListNotes(db *sql.DB, taskID, userID int64) ([]TaskNote, error) {
	if err := ensureTaskOwned(db, taskID, userID); err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT id, task_id, content_enc, created_at FROM task_notes WHERE task_id = ? ORDER BY created_at`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []TaskNote
	for rows.Next() {
		var (
			n         TaskNote
			enc       string
			createdAt string
		)
		if err := rows.Scan(&n.ID, &n.TaskID, &enc, &createdAt); err != nil {
			return nil, err
		}
		if n.Content, err = encryption.DecryptField(enc); err != nil {
			return nil, fmt.Errorf("decrypt note content: %w", err)
		}
		if n.CreatedAt, err = parseTimestamp(createdAt); err != nil {
			return nil, fmt.Errorf("parse note created_at: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []TaskNote{}
	}
	return notes, nil
}

// ensureTaskOwned returns ErrTaskNotFound if the task does not exist or is not
// owned by the given user. Used as a guard before mutating task children.
func ensureTaskOwned(db *sql.DB, taskID, userID int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ? AND user_id = ?`, taskID, userID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// setTaskTags replaces the tag set for a task. New labels are upserted into
// task_tags (reusing existing IDs when a plaintext label already exists for
// the user); removed labels are unlinked. The encryption nonce is random, so
// uniqueness is enforced in Go by decrypting and comparing plaintexts rather
// than relying on the UNIQUE(user_id, label_enc) constraint.
func setTaskTags(tx *sql.Tx, userID, taskID int64, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM task_tag_assignments WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("clear tag assignments: %w", err)
	}

	if len(tags) == 0 {
		return nil
	}

	// Build a map of plaintext label -> tag ID for this user's existing tags.
	existing, err := loadUserTags(tx, userID)
	if err != nil {
		return err
	}

	for _, raw := range tags {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		tagID, ok := existing[label]
		if !ok {
			enc, err := encryption.EncryptField(label)
			if err != nil {
				return fmt.Errorf("encrypt tag label: %w", err)
			}
			res, err := tx.Exec(`INSERT INTO task_tags (user_id, label_enc) VALUES (?, ?)`, userID, enc)
			if err != nil {
				return fmt.Errorf("insert tag: %w", err)
			}
			tagID, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("last insert id: %w", err)
			}
			existing[label] = tagID
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_tag_assignments (task_id, tag_id) VALUES (?, ?)`, taskID, tagID); err != nil {
			return fmt.Errorf("assign tag: %w", err)
		}
	}
	return nil
}

// loadUserTags returns a plaintext label -> tag ID map for the given user.
func loadUserTags(tx *sql.Tx, userID int64) (map[string]int64, error) {
	rows, err := tx.Query(`SELECT id, label_enc FROM task_tags WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var (
			id  int64
			enc string
		)
		if err := rows.Scan(&id, &enc); err != nil {
			return nil, err
		}
		label, err := encryption.DecryptField(enc)
		if err != nil {
			return nil, fmt.Errorf("decrypt tag label: %w", err)
		}
		// Last one wins if duplicates leak through (e.g. legacy plaintext).
		out[label] = id
	}
	return out, rows.Err()
}

// listTagsForTask returns the decrypted, sorted tag labels for a task.
func listTagsForTask(db *sql.DB, taskID int64) ([]string, error) {
	rows, err := db.Query(
		`SELECT t.label_enc FROM task_tags t
		 JOIN task_tag_assignments a ON a.tag_id = t.id
		 WHERE a.task_id = ?`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var enc string
		if err := rows.Scan(&enc); err != nil {
			return nil, err
		}
		label, err := encryption.DecryptField(enc)
		if err != nil {
			return nil, fmt.Errorf("decrypt tag label: %w", err)
		}
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(labels)
	if labels == nil {
		labels = []string{}
	}
	return labels, nil
}

func countNotesForTask(db *sql.DB, taskID int64) (int, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_notes WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
