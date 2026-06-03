// Package familychat implements the REST handlers and storage for the Family
// Chat feature. Conversations live in family_chat_conversations, membership in
// family_chat_members, and messages in family_chat_messages. Message bodies
// and attachment paths are encrypted at rest via internal/encryption.
package familychat

import (
	"database/sql"
	"errors"
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

// maxBackfillLimit caps how many catch-up events EventsSince returns for a
// single reconnect. A reconnect after a brief blip replays only a handful of
// rows; this ceiling is a safety net for a client that resumes from a very old
// id (it still gets the oldest portion of the gap and can refresh for the rest,
// matching ListMessages' own cap).
const maxBackfillLimit = 500

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
//
// EditedAt, DeletedAt, and DeletedBy are pointer types so they marshal as JSON
// null (not the zero value) for messages that have not been edited or deleted.
// A soft-deleted message returns body, attachment_path, attachment_mime cleared
// and edited_at forced to null so the renderer treats it as a tombstone.
//
// MetaJSON is an opaque client-controlled JSON string (currently used by voice
// notes to persist the precomputed waveform + duration). It is encrypted at
// rest like the body and treated as nullable end-to-end: nil when the column
// is NULL. The handler normalizes empty/whitespace-only strings to nil before
// storage so clients can rely on a non-nil pointer meaning "the sender
// attached metadata".
type Message struct {
	ID             int64                       `json:"id"`
	ConversationID int64                       `json:"conversation_id"`
	SenderUserID   int64                       `json:"sender_user_id"`
	Body           string                      `json:"body"`
	AttachmentPath string                      `json:"attachment_path,omitempty"`
	AttachmentMime string                      `json:"attachment_mime,omitempty"`
	CreatedAt      string                      `json:"created_at"`
	EditedAt       *string                     `json:"edited_at"`
	DeletedAt      *string                     `json:"deleted_at"`
	DeletedBy      *int64                      `json:"deleted_by"`
	MetaJSON       *string                     `json:"meta_json"`
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
			messageSelectColumns+`
			 FROM family_chat_messages
			 WHERE conversation_id = ? AND id > ?
			 ORDER BY id DESC
			 LIMIT ?`,
			convID, since, limit,
		)
	} else {
		rows, queryErr = db.Query(
			messageSelectColumns+`
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

	out, err := scanMessageRows(rows)
	if err != nil {
		return nil, err
	}
	if err := attachReactions(db, out, userID); err != nil {
		return nil, err
	}
	return out, nil
}

// messageSelectColumns is the shared column list (in scan order) for queries
// that hydrate a Message via scanMessageRows. Kept as a single constant so the
// SELECT projection and the Scan call below can never drift apart.
const messageSelectColumns = `SELECT id, conversation_id, sender_user_id, body, attachment_path, attachment_mime, created_at, edited_at, deleted_at, deleted_by, meta_json`

// scanMessageRows scans rows produced by a messageSelectColumns query into
// []Message, decrypting body/attachment/meta_json and applying the tombstone
// contract (a soft-deleted row never surfaces its original body, attachment,
// or edit history). Reactions are NOT attached — callers that need them call
// attachReactions afterward. The returned slice is non-nil (possibly empty) so
// JSON callers get [] rather than null.
func scanMessageRows(rows *sql.Rows) ([]Message, error) {
	out := []Message{}
	for rows.Next() {
		var m Message
		var editedAt, deletedAt, metaJSON sql.NullString
		var deletedBy sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderUserID, &m.Body, &m.AttachmentPath, &m.AttachmentMime, &m.CreatedAt, &editedAt, &deletedAt, &deletedBy, &metaJSON); err != nil {
			return nil, err
		}
		var err error
		if m.Body, err = encryption.DecryptField(m.Body); err != nil {
			return nil, fmt.Errorf("decrypt body: %w", err)
		}
		if m.AttachmentPath, err = encryption.DecryptField(m.AttachmentPath); err != nil {
			return nil, fmt.Errorf("decrypt attachment path: %w", err)
		}
		if editedAt.Valid {
			s := editedAt.String
			m.EditedAt = &s
		}
		if metaJSON.Valid {
			plain, decErr := encryption.DecryptField(metaJSON.String)
			if decErr != nil {
				return nil, fmt.Errorf("decrypt meta_json: %w", decErr)
			}
			m.MetaJSON = &plain
		}
		if deletedAt.Valid {
			s := deletedAt.String
			m.DeletedAt = &s
			// Tombstone: never surface the original body, attachment, or edit
			// history to clients. The DB row is already cleared on soft-delete
			// but defensive zeroing here makes the contract obvious.
			m.Body = ""
			m.AttachmentPath = ""
			m.AttachmentMime = ""
			m.EditedAt = nil
			m.MetaJSON = nil
		}
		if deletedBy.Valid {
			id := deletedBy.Int64
			m.DeletedBy = &id
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// attachReactions populates the Reactions field on every message in msgs with
// the per-emoji summary (including the requesting user's `me` flag) in a single
// batched query. Each message ends up with a non-nil map so the JSON shape is
// stable.
func attachReactions(db *sql.DB, msgs []Message, userID int64) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]int64, len(msgs))
	for i := range msgs {
		ids[i] = msgs[i].ID
	}
	reactions, err := batchReactions(db, ids, userID)
	if err != nil {
		return fmt.Errorf("load reactions: %w", err)
	}
	for i := range msgs {
		byEmoji := reactions[msgs[i].ID]
		if byEmoji == nil {
			byEmoji = map[string]*ReactionSummary{}
		}
		msgs[i].Reactions = byEmoji
	}
	return nil
}

// EventsSince returns the messages a reconnecting (or buffer-overflowed) SSE
// client needs to replay to catch up to the live feed, given the highest
// message id it has already seen (sinceID). The result combines two effects
// that a naive `id > sinceID` query would miss:
//
//   - New messages: every message with id > sinceID (the client never saw these).
//   - Edits and deletes of OLDER messages: a message with id <= sinceID whose
//     edited_at/deleted_at is newer than the client's resume point. The resume
//     point is approximated by created_at of message sinceID — the moment the
//     client was last provably caught up — so any modification stamped after it
//     is replayed (modifications stamped before it were delivered live).
//
// Messages are returned in ascending id order so the caller emits new messages
// in chronological order; edits/deletes of older messages are idempotent on the
// client so their relative order does not matter. Each returned Message carries
// its current on-disk state (a tombstone for deletes, the new body for edits),
// which the stream handler maps to the matching SSE event. Reactions are
// attached so backfilled new-message events render identically to live ones.
//
// Membership is NOT re-checked here — the stream handler verifies it before
// calling this. When sinceID <= 0, or the resume id is unknown to this
// conversation, the edit/delete predicates are skipped and only new messages
// (if any) are returned, so an absent or stale resume id degrades to "new
// messages only" rather than replaying the entire history.
func EventsSince(db *sql.DB, convID, userID, sinceID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = maxBackfillLimit
	}
	if limit > maxBackfillLimit {
		limit = maxBackfillLimit
	}

	// Look up the resume point's timestamp. If the id is unknown (different
	// conversation, reset DB), watermark stays empty and we replay new messages
	// only — comparing against '' would otherwise match every edited/deleted row.
	var watermark string
	if sinceID > 0 {
		err := db.QueryRow(
			`SELECT created_at FROM family_chat_messages WHERE id = ? AND conversation_id = ?`,
			sinceID, convID,
		).Scan(&watermark)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	query := messageSelectColumns + ` FROM family_chat_messages WHERE conversation_id = ? AND (id > ?`
	args := []any{convID, sinceID}
	if watermark != "" {
		// A delete supersedes an edit, so a deleted row is matched purely on
		// deleted_at; an edited (but not deleted) row on edited_at.
		query += ` OR (deleted_at IS NOT NULL AND deleted_at > ?) OR (deleted_at IS NULL AND edited_at IS NOT NULL AND edited_at > ?)`
		args = append(args, watermark, watermark)
	}
	query += `) ORDER BY id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out, err := scanMessageRows(rows)
	if err != nil {
		return nil, err
	}
	if err := attachReactions(db, out, userID); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateMessage inserts a new message authored by senderID into convID,
// touches the conversation's last_message_at, and returns the inserted row
// in plaintext form. Returns ErrForbidden if the sender is not a member.
// Equivalent to CreateMessageWithMeta(..., nil) — kept as a thin wrapper so
// older call sites (and tests) that never persist meta_json stay compact.
func CreateMessage(db *sql.DB, convID, senderID int64, body, attachmentPath, attachmentMime string) (*Message, error) {
	return CreateMessageWithMeta(db, convID, senderID, body, attachmentPath, attachmentMime, nil)
}

// CreateMessageWithMeta is the full-fat constructor: metaJSON is nullable so
// callers can persist client-supplied opaque metadata (e.g. voice-note waveform
// + duration) encrypted at rest. Pass nil to leave the meta_json column NULL.
func CreateMessageWithMeta(db *sql.DB, convID, senderID int64, body, attachmentPath, attachmentMime string, metaJSON *string) (*Message, error) {
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
	var encMeta any // nil → NULL column; otherwise the ciphertext string.
	if metaJSON != nil {
		ct, mErr := encryption.EncryptField(*metaJSON)
		if mErr != nil {
			return nil, fmt.Errorf("encrypt meta_json: %w", mErr)
		}
		encMeta = ct
	}
	now := time.Now().UTC().Format(timeFormat)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT INTO family_chat_messages (conversation_id, sender_user_id, body, attachment_path, attachment_mime, created_at, meta_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		convID, senderID, encBody, encPath, attachmentMime, now, encMeta,
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
		MetaJSON:       metaJSON,
		Reactions:      map[string]*ReactionSummary{},
	}, nil
}

// ErrMessageDeleted is returned by EditMessage when the target message has
// already been soft-deleted. Handlers map this to a 409 Conflict so clients
// can refresh and surface the tombstone in the UI.
var ErrMessageDeleted = errors.New("familychat: message is deleted")

// EditMessage updates the body of msgID inside convID, but only when the
// requesting user is the author. Non-authors (and references to messages that
// do not exist in this conversation) receive ErrForbidden so handlers can map
// to 404 and avoid leaking existence. A tombstoned message returns
// ErrMessageDeleted so the caller distinguishes "you may not edit this" from
// "this message can no longer be edited".
//
// On success the message row's body is re-encrypted, edited_at is stamped, and
// the updated Message (decrypted) is returned. The conversation's
// last_message_at is not touched — an edit doesn't shift the conversation's
// sort order in the sidebar.
func EditMessage(db *sql.DB, convID, msgID, userID int64, body string) (*Message, error) {
	// Membership check — an ex-member must not be able to edit historical messages.
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	// Confirm the row belongs to convID + is owned by userID. A single row
	// lookup also pulls deleted_at so we can short-circuit the tombstone case.
	var (
		senderID  int64
		convInMsg int64
		deletedAt sql.NullString
	)
	if err := db.QueryRow(
		`SELECT sender_user_id, conversation_id, deleted_at FROM family_chat_messages WHERE id = ?`,
		msgID,
	).Scan(&senderID, &convInMsg, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	if convInMsg != convID || senderID != userID {
		return nil, ErrForbidden
	}
	if deletedAt.Valid {
		return nil, ErrMessageDeleted
	}

	encBody, err := encryption.EncryptField(body)
	if err != nil {
		return nil, fmt.Errorf("encrypt body: %w", err)
	}
	now := time.Now().UTC().Format(timeFormat)
	res, err := db.Exec(
		`UPDATE family_chat_messages SET body = ?, edited_at = ? WHERE id = ? AND conversation_id = ? AND sender_user_id = ? AND deleted_at IS NULL`,
		encBody, now, msgID, convID, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		// The message was soft-deleted between our SELECT and UPDATE.
		return nil, ErrMessageDeleted
	}

	// Read back the row so the response reflects the on-disk state (including
	// the freshly stamped edited_at and the original created_at). Reactions are
	// fetched separately so the contract matches ListMessages — clients always
	// receive a stable Message shape.
	var (
		createdAt    string
		editedAtNull sql.NullString
		metaJSONNull sql.NullString
	)
	if err := db.QueryRow(
		`SELECT created_at, edited_at, meta_json FROM family_chat_messages WHERE id = ?`,
		msgID,
	).Scan(&createdAt, &editedAtNull, &metaJSONNull); err != nil {
		return nil, err
	}
	editedAt := now
	if editedAtNull.Valid {
		editedAt = editedAtNull.String
	}
	var metaJSON *string
	if metaJSONNull.Valid {
		plain, decErr := encryption.DecryptField(metaJSONNull.String)
		if decErr != nil {
			return nil, fmt.Errorf("decrypt meta_json: %w", decErr)
		}
		metaJSON = &plain
	}
	reactions, err := batchReactions(db, []int64{msgID}, userID)
	if err != nil {
		return nil, fmt.Errorf("load reactions: %w", err)
	}
	byEmoji := reactions[msgID]
	if byEmoji == nil {
		byEmoji = map[string]*ReactionSummary{}
	}
	return &Message{
		ID:             msgID,
		ConversationID: convID,
		SenderUserID:   userID,
		Body:           body,
		CreatedAt:      createdAt,
		EditedAt:       &editedAt,
		MetaJSON:       metaJSON,
		Reactions:      byEmoji,
	}, nil
}

// SoftDeleteMessage marks msgID as deleted by userID and returns the cleared
// attachment path (so the handler can remove the file from disk after a
// successful update). Non-author callers and unknown messages return
// ErrForbidden — handlers map both to 404 to avoid leaking existence. Calling
// SoftDeleteMessage on an already-tombstoned message is a no-op that returns
// (attachmentPath="", nil); the wire payload is idempotent and the SSE
// re-broadcast is still useful for any client that missed the first event.
func SoftDeleteMessage(db *sql.DB, convID, msgID, userID int64) (attachmentPath string, err error) {
	// Membership check — an ex-member must not be able to tombstone historical messages.
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrForbidden
	}

	var (
		senderID  int64
		convInMsg int64
		encPath   string
		deletedAt sql.NullString
	)
	err = db.QueryRow(
		`SELECT sender_user_id, conversation_id, attachment_path, deleted_at FROM family_chat_messages WHERE id = ?`,
		msgID,
	).Scan(&senderID, &convInMsg, &encPath, &deletedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrForbidden
		}
		return "", err
	}
	if convInMsg != convID || senderID != userID {
		return "", ErrForbidden
	}
	if deletedAt.Valid {
		// Already tombstoned; nothing to do.
		return "", nil
	}

	plainPath, derr := encryption.DecryptField(encPath)
	if derr != nil {
		// Treat decrypt failures as no attachment to remove; the row is still
		// marked deleted so the user-visible tombstone path completes.
		log.Printf("familychat: decrypt attachment_path for soft-delete msg=%d: %v", msgID, derr)
		plainPath = ""
	}

	now := time.Now().UTC().Format(timeFormat)
	if _, err := db.Exec(
		`UPDATE family_chat_messages SET deleted_at = ?, deleted_by = ?, body = '', attachment_path = '', attachment_mime = '', meta_json = NULL WHERE id = ?`,
		now, userID, msgID,
	); err != nil {
		return "", err
	}
	return plainPath, nil
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
	// Filter out soft-deleted messages from the conversation list preview so
	// the sidebar shows the most recent visible message. The MAX(id) is
	// computed over visible rows only — if every row in a conversation has
	// been soft-deleted, the conversation simply has no preview.
	rows, err := db.Query(
		`SELECT m.conversation_id, m.sender_user_id, m.body, m.attachment_path
		 FROM family_chat_messages m
		 JOIN (
		   SELECT conversation_id, MAX(id) AS max_id
		   FROM family_chat_messages
		   WHERE conversation_id IN (`+placeholders+`) AND deleted_at IS NULL
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
