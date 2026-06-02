package stride

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ChatMessage represents a single message in a stride plan's coaching chat.
type ChatMessage struct {
	ID           int64  `json:"id"`
	PlanID       int64  `json:"plan_id"`
	UserID       int64  `json:"user_id"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	PlanModified bool   `json:"plan_modified"`
	CreatedAt    string `json:"created_at"`
}

// ListChatMessages returns all messages for a plan, ordered by created_at ASC.
// Content is decrypted before returning. Scoped to userID for safety.
func ListChatMessages(db *sql.DB, planID, userID int64) ([]ChatMessage, error) {
	rows, err := db.Query(`
		SELECT id, plan_id, user_id, role, content, plan_modified, created_at
		FROM stride_chat_messages
		WHERE plan_id = ? AND user_id = ?
		ORDER BY created_at ASC, id ASC
	`, planID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var planMod int
		if err := rows.Scan(&m.ID, &m.PlanID, &m.UserID, &m.Role, &m.Content, &planMod, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.PlanModified = planMod != 0
		if m.Content, err = encryption.DecryptField(m.Content); err != nil {
			return nil, fmt.Errorf("decrypt chat message content: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []ChatMessage{}
	}
	return msgs, nil
}

// AddChatMessage inserts a new chat message with encrypted content.
// Validates that role is "user" or "assistant", content is non-empty,
// and the plan exists and belongs to the user.
func AddChatMessage(db *sql.DB, msg ChatMessage) (ChatMessage, error) {
	if msg.Role != "user" && msg.Role != "assistant" {
		return ChatMessage{}, fmt.Errorf("invalid role %q: must be \"user\" or \"assistant\"", msg.Role)
	}
	if msg.Content == "" {
		return ChatMessage{}, fmt.Errorf("content must not be empty")
	}

	// Verify plan exists and belongs to the user.
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_plans WHERE id = ? AND user_id = ?`, msg.PlanID, msg.UserID).Scan(&exists); err != nil {
		return ChatMessage{}, fmt.Errorf("check plan ownership: %w", err)
	}
	if exists == 0 {
		return ChatMessage{}, fmt.Errorf("plan %d not found for user %d", msg.PlanID, msg.UserID)
	}

	encContent, err := encryption.EncryptField(msg.Content)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("encrypt chat content: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO stride_chat_messages (plan_id, user_id, role, content, plan_modified, created_at)
		VALUES (?, ?, ?, ?, 0, ?)
	`, msg.PlanID, msg.UserID, msg.Role, encContent, now)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("insert chat message: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, fmt.Errorf("last insert id: %w", err)
	}

	msg.ID = id
	msg.CreatedAt = now
	msg.PlanModified = false
	return msg, nil
}

// GetChatSessionID returns the chat_session_id for a plan.
// Returns empty string if not set or plan not found.
func GetChatSessionID(db *sql.DB, planID, userID int64) (string, error) {
	var sessionID string
	err := db.QueryRow(`SELECT chat_session_id FROM stride_plans WHERE id = ? AND user_id = ?`, planID, userID).Scan(&sessionID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get chat session id: %w", err)
	}
	return sessionID, nil
}

// UpdateChatSessionID updates the chat_session_id on the plan row. Scoped to userID.
func UpdateChatSessionID(db *sql.DB, planID, userID int64, sessionID string) error {
	res, err := db.Exec(`UPDATE stride_plans SET chat_session_id = ? WHERE id = ? AND user_id = ?`, sessionID, planID, userID)
	if err != nil {
		return fmt.Errorf("update chat session id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResetChatSession clears the chat_session_id for a plan, scoped to userID.
// On the next turn the absence of a session id makes the handler start a FRESH
// Claude session (no --resume) instead of replaying the entire prior
// conversation, which is what actually reduces per-turn latency. The plan state
// itself (plan_json, profile, evaluations, races, notes) is rebuilt into the
// system prompt every turn, so no real context is lost.
//
// It also advances chat_session_msg_floor to the highest existing chat message
// id, so EstimateChatContext only counts messages added after this reset. The
// fresh native session replays only those messages, so the size estimate must
// start from zero again — otherwise it would keep measuring the full,
// never-trimmed history and reset on every subsequent turn, permanently
// destroying multi-turn continuity instead of resetting once.
func ResetChatSession(db *sql.DB, planID, userID int64) error {
	res, err := db.Exec(`
		UPDATE stride_plans
		SET chat_session_id = '',
		    chat_session_msg_floor = COALESCE(
		        (SELECT MAX(id) FROM stride_chat_messages WHERE plan_id = ? AND user_id = ?), 0)
		WHERE id = ? AND user_id = ?
	`, planID, userID, planID, userID)
	if err != nil {
		return fmt.Errorf("reset chat session id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// EstimateChatContext returns the message count and total stored byte size of
// the chat that Claude replays in the CURRENT native session — i.e. only the
// messages added since the last reset (chat_session_msg_floor). It is a cheap
// proxy for how much conversation Claude replays each turn via --resume (the
// native session is not directly inspectable). Stored content is
// encrypted/base64, so totalBytes is larger than the plaintext, but it stays
// roughly proportional and is good enough to drive an auto-reset threshold.
//
// Counting from the floor (rather than the full history) is what lets a thread
// reset ONCE when it grows large and then accumulate again, instead of
// tripping the threshold on every turn forever.
func EstimateChatContext(db *sql.DB, planID, userID int64) (msgCount, totalBytes int, err error) {
	err = db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(LENGTH(content)), 0)
		FROM stride_chat_messages
		WHERE plan_id = ? AND user_id = ?
		  AND id > COALESCE(
		      (SELECT chat_session_msg_floor FROM stride_plans WHERE id = ? AND user_id = ?), 0)
	`, planID, userID, planID, userID).Scan(&msgCount, &totalBytes)
	if err != nil {
		return 0, 0, fmt.Errorf("estimate chat context: %w", err)
	}
	return msgCount, totalBytes, nil
}

// MarkMessagePlanModified sets plan_modified=1 on a message. Scoped to userID.
func MarkMessagePlanModified(db *sql.DB, messageID, userID int64) error {
	res, err := db.Exec(`UPDATE stride_chat_messages SET plan_modified = 1 WHERE id = ? AND user_id = ?`, messageID, userID)
	if err != nil {
		return fmt.Errorf("mark message plan modified: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
