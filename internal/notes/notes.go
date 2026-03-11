package notes

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Note represents a markdown note with optional tags.
type Note struct {
	ID        int64    `json:"id"`
	UserID    int64    `json:"user_id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// List returns notes for a user, optionally filtered by full-text search and/or tag.
func List(db *sql.DB, userID int64, search, tag string) ([]Note, error) {
	// Use ASCII unit separator (0x1f) as the GROUP_CONCAT delimiter to keep parsing
	// unambiguous regardless of tag content (commas and other printable characters are valid).
	query := `
		SELECT n.id, n.user_id, n.title, n.content, n.created_at, n.updated_at,
		       GROUP_CONCAT(nt.tag, char(31)) AS tags
		FROM notes n
		LEFT JOIN note_tags nt ON nt.note_id = n.id
		WHERE n.user_id = ?`

	args := []any{userID}

	if search != "" {
		query += ` AND (n.title LIKE ? OR n.content LIKE ?)`
		like := "%" + search + "%"
		args = append(args, like, like)
	}

	if tag != "" {
		query += ` AND n.id IN (SELECT note_id FROM note_tags WHERE tag = ?)`
		args = append(args, tag)
	}

	query += ` GROUP BY n.id ORDER BY n.updated_at DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		var tagsStr sql.NullString
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Content, &n.CreatedAt, &n.UpdatedAt, &tagsStr); err != nil {
			return nil, err
		}
		if tagsStr.Valid && tagsStr.String != "" {
			n.Tags = strings.Split(tagsStr.String, "\x1f")
			sort.Strings(n.Tags)
		} else {
			n.Tags = []string{}
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

// Create inserts a new note with the given title, content, and tags.
func Create(db *sql.DB, userID int64, title, content string, tags []string) (*Note, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(
		"INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		userID, title, content, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert note: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	if err := setTags(tx, id, tags); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetByID(db, id, userID)
}

// GetByID returns a single note by ID, scoped to the given user.
func GetByID(db *sql.DB, id, userID int64) (*Note, error) {
	var n Note
	err := db.QueryRow(
		"SELECT id, user_id, title, content, created_at, updated_at FROM notes WHERE id = ? AND user_id = ?",
		id, userID,
	).Scan(&n.ID, &n.UserID, &n.Title, &n.Content, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}

	tags, err := getTags(db, id)
	if err != nil {
		return nil, err
	}
	n.Tags = tags

	return &n, nil
}

// Update modifies an existing note's title, content, and tags.
func Update(db *sql.DB, id, userID int64, title, content string, tags []string) (*Note, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(
		"UPDATE notes SET title = ?, content = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		title, content, now, id, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("update note: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	if err := setTags(tx, id, tags); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetByID(db, id, userID)
}

// Delete removes a note owned by the given user.
func Delete(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM notes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListTags returns all distinct tags used across a user's notes, sorted alphabetically.
func ListTags(db *sql.DB, userID int64) ([]string, error) {
	rows, err := db.Query(
		`SELECT DISTINCT nt.tag
		 FROM note_tags nt
		 JOIN notes n ON n.id = nt.note_id
		 WHERE n.user_id = ?
		 ORDER BY nt.tag`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// setTags replaces all tags for a note within an existing transaction.
// Tags must not contain commas, which are rejected by the handler layer before reaching here.
func setTags(tx *sql.Tx, noteID int64, tags []string) error {
	if _, err := tx.Exec("DELETE FROM note_tags WHERE note_id = ?", noteID); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if strings.ContainsRune(tag, ',') {
			return fmt.Errorf("tag %q must not contain a comma", tag)
		}
		if strings.ContainsRune(tag, '\x1f') {
			return fmt.Errorf("tag %q must not contain the unit-separator character", tag)
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO note_tags (note_id, tag) VALUES (?, ?)", noteID, tag); err != nil {
			return fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}
	return nil
}

// getTags returns sorted tags for a note.
func getTags(db *sql.DB, noteID int64) ([]string, error) {
	rows, err := db.Query("SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag", noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}
