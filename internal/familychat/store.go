package familychat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// isMember reports whether userID is a member of conversationID. It uses the
// supplied executor (sql.DB or sql.Tx) so callers can check membership inside
// a transaction. Returns (false, nil) when the user is not a member; any
// other error is propagated.
func isMember(ctx context.Context, q sqlQuerier, conversationID, userID int64) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM family_chat_members WHERE conversation_id = ? AND user_id = ?`,
		conversationID, userID,
	).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// sqlQuerier is the minimal interface satisfied by both *sql.DB and *sql.Tx
// that the store helpers need.
type sqlQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CreateConversation inserts a new conversation with the given (plaintext)
// name and a membership row for the owner plus each memberID. memberIDs that
// duplicate ownerID are silently skipped. Returns the new conversation ID.
func CreateConversation(ctx context.Context, db *sql.DB, ownerID int64, name string, memberIDs []int64) (int64, error) {
	now := time.Now().UTC()
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return 0, fmt.Errorf("encrypt name: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`INSERT INTO family_chat_conversations (name_enc, owner_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		encName, ownerID, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO family_chat_members (conversation_id, user_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		id, ownerID, RoleOwner, now,
	); err != nil {
		return 0, fmt.Errorf("insert owner member: %w", err)
	}

	for _, mid := range memberIDs {
		if mid == ownerID {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO family_chat_members (conversation_id, user_id, role, joined_at)
			 VALUES (?, ?, ?, ?)`,
			id, mid, RoleMember, now,
		); err != nil {
			return 0, fmt.Errorf("insert member %d: %w", mid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// ListUserConversations returns every conversation userID is a member of,
// most-recently-updated first. Names are decrypted.
func ListUserConversations(ctx context.Context, db *sql.DB, userID int64) ([]Conversation, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT c.id, c.name_enc, c.owner_id, c.created_at, c.updated_at
		 FROM family_chat_conversations c
		 JOIN family_chat_members m ON m.conversation_id = c.id
		 WHERE m.user_id = ?
		 ORDER BY c.updated_at DESC, c.id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		var nameEnc string
		if err := rows.Scan(&c.ID, &nameEnc, &c.OwnerID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		name, err := encryption.DecryptField(nameEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt conversation %d name: %w", c.ID, err)
		}
		c.Name = name
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Conversation{}
	}
	return out, nil
}

// GetConversation returns the conversation if askerID is a member of it.
// Returns ErrForbidden when askerID is not a member or the conversation does
// not exist — existence is not revealed to non-members.
func GetConversation(ctx context.Context, db *sql.DB, conversationID, askerID int64) (Conversation, error) {
	ok, err := isMember(ctx, db, conversationID, askerID)
	if err != nil {
		return Conversation{}, fmt.Errorf("check membership: %w", err)
	}
	if !ok {
		return Conversation{}, ErrForbidden
	}

	var c Conversation
	var nameEnc string
	err = db.QueryRowContext(ctx,
		`SELECT id, name_enc, owner_id, created_at, updated_at
		 FROM family_chat_conversations WHERE id = ?`,
		conversationID,
	).Scan(&c.ID, &nameEnc, &c.OwnerID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Conversation{}, err
	}
	name, err := encryption.DecryptField(nameEnc)
	if err != nil {
		return Conversation{}, fmt.Errorf("decrypt name: %w", err)
	}
	c.Name = name
	return c, nil
}

// AppendMessage inserts a new message into the conversation. senderID must be
// a member of the conversation, otherwise ErrForbidden is returned. body and
// attachmentPath are encrypted before storage. attachmentMime is stored as-is
// (it is not user-secret and may be used by attachment-serving handlers).
// Returns the new message ID.
func AppendMessage(ctx context.Context, db *sql.DB, conversationID, senderID int64, body, attachmentPath, attachmentMime string) (int64, error) {
	encBody, err := encryption.EncryptField(body)
	if err != nil {
		return 0, fmt.Errorf("encrypt body: %w", err)
	}
	encPath, err := encryption.EncryptField(attachmentPath)
	if err != nil {
		return 0, fmt.Errorf("encrypt attachment path: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	ok, err := isMember(ctx, tx, conversationID, senderID)
	if err != nil {
		return 0, fmt.Errorf("check membership: %w", err)
	}
	if !ok {
		return 0, ErrForbidden
	}

	now := time.Now().UTC()
	var pathArg any
	if attachmentPath == "" {
		pathArg = nil
	} else {
		pathArg = encPath
	}
	var mimeArg any
	if attachmentMime == "" {
		mimeArg = nil
	} else {
		mimeArg = attachmentMime
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO family_chat_messages (conversation_id, sender_id, body_enc, attachment_path_enc, attachment_mime, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		conversationID, senderID, encBody, pathArg, mimeArg, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE family_chat_conversations SET updated_at = ? WHERE id = ?`,
		now, conversationID,
	); err != nil {
		return 0, fmt.Errorf("bump updated_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// ListMessages returns messages from the conversation whose ID is greater
// than sinceID (use 0 to start from the beginning), ordered by ID ascending,
// capped at limit. askerID must be a member of the conversation.
// limit <= 0 returns an empty slice.
func ListMessages(ctx context.Context, db *sql.DB, conversationID, askerID, sinceID int64, limit int) ([]Message, error) {
	ok, err := isMember(ctx, db, conversationID, askerID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !ok {
		return nil, ErrForbidden
	}
	if limit <= 0 {
		return []Message{}, nil
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, sender_id, body_enc, attachment_path_enc, attachment_mime, sent_at
		 FROM family_chat_messages
		 WHERE conversation_id = ? AND id > ?
		 ORDER BY id ASC
		 LIMIT ?`,
		conversationID, sinceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var bodyEnc string
		var pathEnc, mime sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &bodyEnc, &pathEnc, &mime, &m.SentAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		body, err := encryption.DecryptField(bodyEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt message %d body: %w", m.ID, err)
		}
		m.Body = body
		if pathEnc.Valid && pathEnc.String != "" {
			path, err := encryption.DecryptField(pathEnc.String)
			if err != nil {
				return nil, fmt.Errorf("decrypt message %d attachment path: %w", m.ID, err)
			}
			m.AttachmentPath = path
		}
		if mime.Valid {
			m.AttachmentMime = mime.String
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Message{}
	}
	return out, nil
}

// MarkRead updates the membership row's last_read_at timestamp. Returns
// ErrForbidden if the user is not a member of the conversation.
func MarkRead(ctx context.Context, db *sql.DB, conversationID, userID int64, ts time.Time) error {
	res, err := db.ExecContext(ctx,
		`UPDATE family_chat_members SET last_read_at = ?
		 WHERE conversation_id = ? AND user_id = ?`,
		ts.UTC(), conversationID, userID,
	)
	if err != nil {
		return fmt.Errorf("update last_read_at: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrForbidden
	}
	return nil
}
