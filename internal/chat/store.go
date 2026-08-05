package chat

import (
	"database/sql"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// timeFormat is a fixed-width UTC timestamp with millisecond precision,
// ensuring correct lexicographic ordering and consistent string widths.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// decryptOrRaw decrypts a stored conversation title or message body, falling
// back to the raw column value if decryption fails. Legacy rows written before
// encryption carry no ciphertext prefix and are returned unchanged by
// DecryptField; a corrupt ciphertext row would otherwise fail the whole
// request, so we log and surface the stored value instead. We deliberately do
// not use encryption.DecryptLenient here — it blanks the value, which would
// silently drop chat history.
func decryptOrRaw(value string) string {
	plain, err := encryption.DecryptField(value)
	if err != nil {
		log.Printf("chat: decrypt failed, returning raw stored value: %v", err)
		return value
	}
	return plain
}

// Conversation represents a chat conversation.
type Conversation struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	SessionID string `json:"-"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Message represents a single message in a conversation.
type Message struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	CreatedAt      string `json:"created_at"`
}

// ListConversations returns all conversations for the given user, newest first.
func ListConversations(db *sql.DB, userID int64) ([]Conversation, error) {
	rows, err := db.Query(
		`SELECT id, user_id, title, model, session_id, created_at, updated_at
		 FROM chat_conversations
		 WHERE user_id = ?
		 ORDER BY updated_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convos []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.SessionID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Title = decryptOrRaw(c.Title)
		convos = append(convos, c)
	}
	return convos, rows.Err()
}

// CreateConversation inserts a new conversation and returns it.
func CreateConversation(db *sql.DB, userID int64, title, model string) (*Conversation, error) {
	now := time.Now().UTC().Format(timeFormat)
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	result, err := db.Exec(
		`INSERT INTO chat_conversations (user_id, title, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, encTitle, model, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Conversation{
		ID:        id,
		UserID:    userID,
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetConversation returns a single conversation owned by the given user.
func GetConversation(db *sql.DB, id, userID int64) (*Conversation, error) {
	var c Conversation
	err := db.QueryRow(
		`SELECT id, user_id, title, model, session_id, created_at, updated_at
		 FROM chat_conversations
		 WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.SessionID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Title = decryptOrRaw(c.Title)
	return &c, nil
}

// DeleteConversation removes a conversation owned by the given user.
// Returns sql.ErrNoRows if the conversation doesn't exist or isn't owned by the user.
func DeleteConversation(db *sql.DB, id, userID int64) error {
	result, err := db.Exec(
		`DELETE FROM chat_conversations WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RenameConversation updates the title of a conversation owned by the given user.
func RenameConversation(db *sql.DB, id, userID int64, title string) (*Conversation, error) {
	now := time.Now().UTC().Format(timeFormat)
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, err
	}
	result, err := db.Exec(
		`UPDATE chat_conversations SET title = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		encTitle, now, id, userID,
	)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return GetConversation(db, id, userID)
}

// UpdateConversation updates the title and/or model of a conversation owned by
// the given user. Only the non-nil fields are written; updated_at is always
// touched. Passing both nil touches only updated_at. Returns sql.ErrNoRows if
// the conversation doesn't exist or isn't owned by the user.
func UpdateConversation(db *sql.DB, id, userID int64, title, model *string) (*Conversation, error) {
	now := time.Now().UTC().Format(timeFormat)
	sets := []string{"updated_at = ?"}
	args := []any{now}
	if title != nil {
		encTitle, err := encryption.EncryptField(*title)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "title = ?")
		args = append(args, encTitle)
	}
	if model != nil {
		sets = append(sets, "model = ?")
		args = append(args, *model)
	}
	args = append(args, id, userID)

	query := "UPDATE chat_conversations SET " + strings.Join(sets, ", ") + " WHERE id = ? AND user_id = ?"
	result, err := db.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return GetConversation(db, id, userID)
}

// GetMessages returns all messages for a conversation, ordered chronologically.
func GetMessages(db *sql.DB, conversationID int64) ([]Message, error) {
	rows, err := db.Query(
		`SELECT id, conversation_id, role, content, created_at
		 FROM chat_messages
		 WHERE conversation_id = ?
		 ORDER BY created_at ASC, id ASC`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = decryptOrRaw(m.Content)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// TruncateFrom deletes the given message and every message ordered after it in
// the same conversation, then clears the stored Claude CLI session ID so the
// next turn starts a fresh session instead of resuming context that references
// the removed messages.
//
// The conversation must be owned by userID and the message must belong to that
// conversation; otherwise sql.ErrNoRows is returned and no rows are changed.
// The deleted target message is returned so callers can echo its content back
// to the client (used to re-send on Regenerate / prefill the composer on Edit).
func TruncateFrom(db *sql.DB, conversationID, userID, messageID int64) (*Message, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var m Message
	err = tx.QueryRow(
		`SELECT m.id, m.conversation_id, m.role, m.content, m.created_at
		 FROM chat_messages m
		 JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.id = ? AND m.conversation_id = ? AND c.user_id = ?`,
		messageID, conversationID, userID,
	).Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt)
	if err != nil {
		// sql.ErrNoRows when the message is unknown, belongs to another
		// conversation, or the conversation isn't owned by this user.
		return nil, err
	}
	m.Content = decryptOrRaw(m.Content)

	// Delete the target and everything after it using the same
	// (created_at, id) ordering GetMessages reads with, so the surviving
	// history is exactly the prefix the client still displays.
	if _, err := tx.Exec(
		`DELETE FROM chat_messages
		 WHERE conversation_id = ?
		   AND (created_at > ? OR (created_at = ? AND id >= ?))`,
		conversationID, m.CreatedAt, m.CreatedAt, m.ID,
	); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(timeFormat)
	if _, err := tx.Exec(
		`UPDATE chat_conversations SET session_id = '', updated_at = ? WHERE id = ? AND user_id = ?`,
		now, conversationID, userID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateSessionID stores the Claude CLI session ID on a conversation owned by the given user.
func UpdateSessionID(db *sql.DB, conversationID, userID int64, sessionID string) error {
	now := time.Now().UTC().Format(timeFormat)
	result, err := db.Exec(
		`UPDATE chat_conversations SET session_id = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		sessionID, now, conversationID, userID,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// InsertMessage adds a message to a conversation and touches updated_at.
// Both operations are performed in a single transaction for atomicity.
func InsertMessage(db *sql.DB, conversationID int64, role, content string) (*Message, error) {
	now := time.Now().UTC().Format(timeFormat)

	encContent, err := encryption.EncryptField(content)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	result, err := tx.Exec(
		`INSERT INTO chat_messages (conversation_id, role, content, created_at)
		 VALUES (?, ?, ?, ?)`,
		conversationID, role, encContent, now,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	// Touch the conversation's updated_at.
	if _, err := tx.Exec(
		`UPDATE chat_conversations SET updated_at = ? WHERE id = ?`,
		now, conversationID,
	); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	return &Message{
		ID:             id,
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      now,
	}, nil
}
