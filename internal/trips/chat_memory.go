package trips

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Robin831/Hytte/internal/encryption"
)

// What the trip assistant remembers about the family across trips: short
// notes it writes itself ("remember" in its change block) when it learns a
// lasting fact or preference. Everyone with the trips feature can see and
// remove them from the chat panel. Notes are encrypted.

const (
	maxNotes   = 60
	maxNoteLen = 300
)

// AssistantNote is one remembered fact.
type AssistantNote struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// ListNotes returns the remembered facts, oldest first.
func ListNotes(ctx context.Context, db *sql.DB) ([]AssistantNote, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, note, created_at FROM trip_assistant_notes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list assistant notes: %w", err)
	}
	defer rows.Close()
	out := []AssistantNote{}
	for rows.Next() {
		var n AssistantNote
		var enc string
		if err := rows.Scan(&n.ID, &enc, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Text = encryption.DecryptLenient(enc)
		out = append(out, n)
	}
	return out, rows.Err()
}

// AddNote stores a fact. Duplicates and overflow are ignored.
func AddNote(ctx context.Context, db *sql.DB, userID int64, text string) error {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return nil
	}
	if len([]rune(text)) > maxNoteLen {
		text = string([]rune(text)[:maxNoteLen])
	}
	existing, err := ListNotes(ctx, db)
	if err != nil {
		return err
	}
	if len(existing) >= maxNotes {
		return nil
	}
	for _, n := range existing {
		if strings.EqualFold(n.Text, text) {
			return nil
		}
	}
	enc, err := encryption.EncryptField(text)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO trip_assistant_notes (note, created_by, created_at) VALUES (?, ?, ?)`, enc, nullUser(userID), now())
	return err
}

func nullUser(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// DeleteNote forgets a fact.
func DeleteNote(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM trip_assistant_notes WHERE id = ?`, id)
	return err
}

func applyNotes(ctx context.Context, db *sql.DB, userID int64, remember []string, forget []int64) {
	for _, id := range forget {
		_ = DeleteNote(ctx, db, id)
	}
	for _, text := range remember {
		_ = AddNote(ctx, db, userID, text)
	}
}

// notesContext is the memory as the prompt shows it.
func notesContext(ctx context.Context, db *sql.DB) string {
	notes, err := ListNotes(ctx, db)
	if err != nil || len(notes) == 0 {
		return "(nothing yet)"
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "- [%d] %s\n", n.ID, n.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}
