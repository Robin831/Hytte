package chat

import (
	"database/sql"
	"time"
)

// Conversation represents a chat conversation.
type Conversation struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Title     string `json:"title"`
	Model     string `json:"model"`
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
		`SELECT id, user_id, title, model, created_at, updated_at
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
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		convos = append(convos, c)
	}
	return convos, rows.Err()
}

// CreateConversation inserts a new conversation and returns it.
func CreateConversation(db *sql.DB, userID int64, title, model string) (*Conversation, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO chat_conversations (user_id, title, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, title, model, now, now,
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
		`SELECT id, user_id, title, model, created_at, updated_at
		 FROM chat_conversations
		 WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
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
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`UPDATE chat_conversations SET title = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		title, now, id, userID,
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
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// InsertMessage adds a message to a conversation and touches updated_at.
func InsertMessage(db *sql.DB, conversationID int64, role, content string) (*Message, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO chat_messages (conversation_id, role, content, created_at)
		 VALUES (?, ?, ?, ?)`,
		conversationID, role, content, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	// Touch the conversation's updated_at.
	_, _ = db.Exec(
		`UPDATE chat_conversations SET updated_at = ? WHERE id = ?`,
		now, conversationID,
	)
	return &Message{
		ID:             id,
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      now,
	}, nil
}
