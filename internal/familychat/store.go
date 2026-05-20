// Package familychat implements the REST handlers and storage for the Family
// Chat feature. Conversations live in family_chat_conversations, membership in
// family_chat_members, and messages in family_chat_messages. Message bodies
// and attachment paths are encrypted at rest via internal/encryption.
package familychat

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// timeFormat is a fixed-width UTC timestamp with millisecond precision,
// matching the convention used by other stores in this repo (e.g. chat/store.go).
// An (id) tie-breaker in ORDER BY clauses handles same-millisecond writes.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// Conversation is a single family chat conversation as returned to the client.
type Conversation struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	OwnerUserID         int64   `json:"owner_user_id"`
	CreatedAt           string  `json:"created_at"`
	LastMessageAt       string  `json:"last_message_at"`
	UnreadCount         int64   `json:"unread_count"`
	MemberIDs           []int64 `json:"member_ids"`
	LastMessagePreview  string  `json:"last_message_preview"`
	LastMessageSenderID int64   `json:"last_message_sender_id,omitempty"`
}

// Message is a single chat message returned to the client. Body and
// AttachmentPath are returned in plaintext after decryption. Reactions is
// always non-nil so the JSON shape is stable (an empty `{}` instead of null
// when nobody has reacted yet).
type Message struct {
	ID             int64                       `json:"id"`
	ConversationID int64                       `json:"conversation_id"`
	SenderUserID   int64                       `json:"sender_user_id"`
	Body           string                      `json:"body"`
	AttachmentPath string                      `json:"attachment_path,omitempty"`
	AttachmentMime string                      `json:"attachment_mime,omitempty"`
	CreatedAt      string                      `json:"created_at"`
	Reactions      map[string]*ReactionSummary `json:"reactions"`
}

// IsMember reports whether userID belongs to convID. It delegates to
// DefaultMembership so there is a single SQL implementation shared with the
// SSE stream's membership check.
func IsMember(db *sql.DB, convID, userID int64) (bool, error) {
	return DefaultMembership(db)(userID, convID)
}

// ValidateMemberIDs checks that every id in ids exists in the users table
// using a single IN query. It returns the first missing id (and nil error) so
// callers can map it to a 400 response, or (0, error) on a DB failure.
func ValidateMemberIDs(db *sql.DB, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Query(`SELECT id FROM users WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	found := make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return id, nil
		}
	}
	return 0, nil
}

// ListConversations returns every conversation the user belongs to, newest
// activity first. Member IDs and unread counts are fetched with two batched
// queries (not N per-conversation queries) to keep response time predictable.
func ListConversations(db *sql.DB, userID int64) ([]Conversation, error) {
	rows, err := db.Query(
		`SELECT c.id, c.name, c.owner_user_id, c.created_at, c.last_message_at
		 FROM family_chat_conversations c
		 JOIN family_chat_members m ON m.conversation_id = c.id
		 WHERE m.user_id = ?
		 ORDER BY CASE WHEN c.last_message_at = '' THEN c.created_at ELSE c.last_message_at END DESC, c.id DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convBuf []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Name, &c.OwnerUserID, &c.CreatedAt, &c.LastMessageAt); err != nil {
			return nil, err
		}
		convBuf = append(convBuf, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(convBuf) == 0 {
		return []Conversation{}, nil
	}

	convIDs := make([]int64, len(convBuf))
	for i, c := range convBuf {
		convIDs[i] = c.ID
	}

	membersByConv, err := batchMemberIDs(db, convIDs)
	if err != nil {
		return nil, err
	}
	unreadByConv, err := batchUnreadCounts(db, userID)
	if err != nil {
		return nil, err
	}
	lastMsgByConv, err := batchLastMessages(db, convIDs)
	if err != nil {
		return nil, err
	}

	out := make([]Conversation, 0, len(convBuf))
	for _, c := range convBuf {
		c.MemberIDs = membersByConv[c.ID]
		if c.MemberIDs == nil {
			c.MemberIDs = []int64{}
		}
		c.UnreadCount = unreadByConv[c.ID]
		if last, ok := lastMsgByConv[c.ID]; ok {
			c.LastMessagePreview = last.preview
			c.LastMessageSenderID = last.senderID
		}
		out = append(out, c)
	}
	return out, nil
}

// CreateConversation inserts a new conversation owned by ownerID and adds
// every user in memberIDs (plus the owner) as a member. Duplicate or invalid
// member IDs are silently coalesced.
func CreateConversation(db *sql.DB, ownerID int64, name string, memberIDs []int64) (*Conversation, error) {
	now := time.Now().UTC().Format(timeFormat)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT INTO family_chat_conversations (name, owner_user_id, created_at, last_message_at) VALUES (?, ?, ?, ?)`,
		name, ownerID, now, "",
	)
	if err != nil {
		return nil, fmt.Errorf("insert conversation: %w", err)
	}
	convID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	seen := map[int64]struct{}{ownerID: {}}
	if _, err := tx.Exec(
		`INSERT INTO family_chat_members (conversation_id, user_id, joined_at, last_read_at) VALUES (?, ?, ?, ?)`,
		convID, ownerID, now, "",
	); err != nil {
		return nil, fmt.Errorf("insert owner member: %w", err)
	}
	for _, uid := range memberIDs {
		if uid <= 0 {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		if _, err := tx.Exec(
			`INSERT INTO family_chat_members (conversation_id, user_id, joined_at, last_read_at) VALUES (?, ?, ?, ?)`,
			convID, uid, now, "",
		); err != nil {
			return nil, fmt.Errorf("insert member %d: %w", uid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetConversation(db, convID, ownerID)
}

// GetConversation returns the conversation if userID is a member, otherwise
// returns ErrForbidden (handlers map to 404).
func GetConversation(db *sql.DB, convID, userID int64) (*Conversation, error) {
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	var c Conversation
	var lastReadAt string
	err = db.QueryRow(
		`SELECT c.id, c.name, c.owner_user_id, c.created_at, c.last_message_at, m.last_read_at
		 FROM family_chat_conversations c
		 JOIN family_chat_members m ON m.conversation_id = c.id
		 WHERE c.id = ? AND m.user_id = ?`,
		convID, userID,
	).Scan(&c.ID, &c.Name, &c.OwnerUserID, &c.CreatedAt, &c.LastMessageAt, &lastReadAt)
	if err != nil {
		return nil, err
	}

	c.MemberIDs, err = listMemberIDs(db, convID)
	if err != nil {
		return nil, err
	}
	c.UnreadCount, err = unreadCount(db, convID, userID, lastReadAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteConversation removes the conversation, but only if userID is its
// owner. Non-owners (including other members) receive ErrForbidden so the
// existence of conversations they do not own is not leaked.
func DeleteConversation(db *sql.DB, convID, userID int64) error {
	res, err := db.Exec(
		`DELETE FROM family_chat_conversations WHERE id = ? AND owner_user_id = ?`,
		convID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrForbidden
	}
	return nil
}

// ListMessages returns up to limit messages from convID, newest-first.
// If since > 0, only messages with id > since are returned (still newest-first)
// — this lets the client poll for new messages incrementally.
func ListMessages(db *sql.DB, convID, userID, since int64, limit int) ([]Message, error) {
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var (
		rows     *sql.Rows
		queryErr error
	)
	if since > 0 {
		rows, queryErr = db.Query(
			`SELECT id, conversation_id, sender_user_id, body, attachment_path, attachment_mime, created_at
			 FROM family_chat_messages
			 WHERE conversation_id = ? AND id > ?
			 ORDER BY id DESC
			 LIMIT ?`,
			convID, since, limit,
		)
	} else {
		rows, queryErr = db.Query(
			`SELECT id, conversation_id, sender_user_id, body, attachment_path, attachment_mime, created_at
			 FROM family_chat_messages
			 WHERE conversation_id = ?
			 ORDER BY id DESC
			 LIMIT ?`,
			convID, limit,
		)
	}
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()

	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderUserID, &m.Body, &m.AttachmentPath, &m.AttachmentMime, &m.CreatedAt); err != nil {
			return nil, err
		}
		if m.Body, err = encryption.DecryptField(m.Body); err != nil {
			return nil, fmt.Errorf("decrypt body: %w", err)
		}
		if m.AttachmentPath, err = encryption.DecryptField(m.AttachmentPath); err != nil {
			return nil, fmt.Errorf("decrypt attachment path: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) > 0 {
		ids := make([]int64, len(out))
		for i := range out {
			ids[i] = out[i].ID
		}
		reactions, err := batchReactions(db, ids, userID)
		if err != nil {
			return nil, fmt.Errorf("load reactions: %w", err)
		}
		for i := range out {
			byEmoji := reactions[out[i].ID]
			if byEmoji == nil {
				byEmoji = map[string]*ReactionSummary{}
			}
			out[i].Reactions = byEmoji
		}
	}
	return out, nil
}

// CreateMessage inserts a new message authored by senderID into convID,
// touches the conversation's last_message_at, and returns the inserted row
// in plaintext form. Returns ErrForbidden if the sender is not a member.
func CreateMessage(db *sql.DB, convID, senderID int64, body, attachmentPath, attachmentMime string) (*Message, error) {
	ok, err := IsMember(db, convID, senderID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	encBody, err := encryption.EncryptField(body)
	if err != nil {
		return nil, fmt.Errorf("encrypt body: %w", err)
	}
	encPath, err := encryption.EncryptField(attachmentPath)
	if err != nil {
		return nil, fmt.Errorf("encrypt attachment path: %w", err)
	}
	now := time.Now().UTC().Format(timeFormat)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT INTO family_chat_messages (conversation_id, sender_user_id, body, attachment_path, attachment_mime, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		convID, senderID, encBody, encPath, attachmentMime, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE family_chat_conversations SET last_message_at = ? WHERE id = ?`,
		now, convID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Message{
		ID:             id,
		ConversationID: convID,
		SenderUserID:   senderID,
		Body:           body,
		AttachmentPath: attachmentPath,
		AttachmentMime: attachmentMime,
		CreatedAt:      now,
		Reactions:      map[string]*ReactionSummary{},
	}, nil
}

// MarkRead sets the requesting member's last_read_at to at. Returns
// ErrForbidden if the user is not a member of convID.
func MarkRead(db *sql.DB, convID, userID int64, at string) error {
	if at == "" {
		at = time.Now().UTC().Format(timeFormat)
	}
	res, err := db.Exec(
		`UPDATE family_chat_members SET last_read_at = ? WHERE conversation_id = ? AND user_id = ?`,
		at, convID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrForbidden
	}
	return nil
}

// listMemberIDs returns every user_id in convID sorted ascending. Used by
// single-conversation paths (GetConversation, CreateConversation).
func listMemberIDs(db *sql.DB, convID int64) ([]int64, error) {
	rows, err := db.Query(
		`SELECT user_id FROM family_chat_members WHERE conversation_id = ? ORDER BY user_id`,
		convID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []int64{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// batchMemberIDs fetches all member user IDs for the given conversation IDs in
// a single query, returning a map keyed by conversation ID.
func batchMemberIDs(db *sql.DB, convIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64, len(convIDs))
	if len(convIDs) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(convIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(convIDs))
	for i, id := range convIDs {
		args[i] = id
		result[id] = []int64{}
	}
	rows, err := db.Query(
		`SELECT conversation_id, user_id FROM family_chat_members WHERE conversation_id IN (`+placeholders+`) ORDER BY user_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var convID, userID int64
		if err := rows.Scan(&convID, &userID); err != nil {
			return nil, err
		}
		result[convID] = append(result[convID], userID)
	}
	return result, rows.Err()
}

// batchUnreadCounts returns unread message counts for every conversation the
// user belongs to in a single JOIN query, keyed by conversation ID.
func batchUnreadCounts(db *sql.DB, userID int64) (map[int64]int64, error) {
	result := make(map[int64]int64)
	rows, err := db.Query(`
		SELECT m.conversation_id, COUNT(msg.id)
		FROM family_chat_members m
		LEFT JOIN family_chat_messages msg
			ON  msg.conversation_id  = m.conversation_id
			AND msg.sender_user_id  <> m.user_id
			AND (m.last_read_at = '' OR msg.created_at > m.last_read_at)
		WHERE m.user_id = ?
		GROUP BY m.conversation_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var convID, count int64
		if err := rows.Scan(&convID, &count); err != nil {
			return nil, err
		}
		result[convID] = count
	}
	return result, rows.Err()
}

// lastMessage holds the decrypted preview text and sender id for the most
// recent message in a conversation. Used by the conversation list to render
// each row without a follow-up message fetch.
type lastMessage struct {
	preview  string
	senderID int64
}

// previewMaxRunes caps the length of the last-message preview returned with
// each conversation. Long messages are truncated; clients can fetch the full
// message via the /messages endpoint when the conversation is opened.
const previewMaxRunes = 140

// batchLastMessages returns the most recent message (decrypted) per
// conversation id. Conversations with no messages are absent from the result
// map. The query uses a grouped derived subquery joined back to the messages
// table to pick the max message id per conversation in a single round-trip.
func batchLastMessages(db *sql.DB, convIDs []int64) (map[int64]lastMessage, error) {
	result := make(map[int64]lastMessage, len(convIDs))
	if len(convIDs) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(convIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(convIDs))
	for i, id := range convIDs {
		args[i] = id
	}
	rows, err := db.Query(
		`SELECT m.conversation_id, m.sender_user_id, m.body, m.attachment_path
		 FROM family_chat_messages m
		 JOIN (
		   SELECT conversation_id, MAX(id) AS max_id
		   FROM family_chat_messages
		   WHERE conversation_id IN (`+placeholders+`)
		   GROUP BY conversation_id
		 ) latest ON latest.conversation_id = m.conversation_id AND latest.max_id = m.id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			convID, senderID       int64
			body, attachmentPath   string
		)
		if err := rows.Scan(&convID, &senderID, &body, &attachmentPath); err != nil {
			return nil, err
		}
		// A failed decrypt of a single message body must not blow up the entire
		// conversation list — that would lock users out of every conversation.
		// Follow the repo convention (see internal/allowance/storage.go):
		// log + return legacy plaintext as-is, or log + blank for corrupted
		// enc:-prefixed values.
		plainBody, err := encryption.DecryptField(body)
		if err != nil {
			if strings.HasPrefix(body, "enc:") {
				log.Printf("familychat: decrypt preview body failed for conversation_id=%d (corrupted enc value): %v", convID, err)
				plainBody = ""
			} else {
				log.Printf("familychat: returning legacy plaintext preview for conversation_id=%d after decrypt failure: %v", convID, err)
				plainBody = body
			}
		}
		preview := truncateRunes(plainBody, previewMaxRunes)
		if preview == "" && attachmentPath != "" {
			// No text body but the message had an attachment; surface a generic
			// placeholder so the list still shows something meaningful.
			preview = "📎"
		}
		result[convID] = lastMessage{preview: preview, senderID: senderID}
	}
	return result, rows.Err()
}

// truncateRunes returns s capped to max runes, appending an ellipsis when
// truncation actually occurred. Operating on runes (not bytes) keeps multibyte
// characters intact.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// unreadCount returns the number of messages in convID newer than lastReadAt
// that were not authored by userID. Used by single-conversation paths.
func unreadCount(db *sql.DB, convID, userID int64, lastReadAt string) (int64, error) {
	var count int64
	if lastReadAt == "" {
		err := db.QueryRow(
			`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ? AND sender_user_id <> ?`,
			convID, userID,
		).Scan(&count)
		return count, err
	}
	err := db.QueryRow(
		`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ? AND sender_user_id <> ? AND created_at > ?`,
		convID, userID, lastReadAt,
	).Scan(&count)
	return count, err
}
