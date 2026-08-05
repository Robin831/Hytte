package chat

import (
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

// rawColumn reads a single string column straight from SQLite, bypassing the
// store's decrypt path, so tests can assert what is actually stored at rest.
func rawColumn(t *testing.T, d *sql.DB, query string, args ...any) string {
	t.Helper()
	var v string
	if err := d.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("read raw column: %v", err)
	}
	return v
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-chat-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	// Ensure a single connection for SQLite :memory: to avoid separate in-memory DBs.
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	t.Cleanup(func() { d.Close() })

	// Create a test user.
	_, err = d.Exec(`INSERT INTO users (id, google_id, email, name, picture) VALUES (1, 'g1', 'test@example.com', 'Test', '')`)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return d
}

func TestCreateAndListConversations(t *testing.T) {
	d := setupTestDB(t)

	// Empty list initially.
	convos, err := ListConversations(d, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 0 {
		t.Fatalf("expected 0 conversations, got %d", len(convos))
	}

	// Create two conversations.
	c1, err := CreateConversation(d, 1, "First Chat", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c1.Title != "First Chat" || c1.Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected conversation: %+v", c1)
	}

	c2, err := CreateConversation(d, 1, "", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	convos, err = ListConversations(d, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(convos))
	}
	// Newest first.
	if convos[0].ID != c2.ID {
		t.Fatalf("expected newest first, got ID %d", convos[0].ID)
	}
}

func TestGetConversation(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Test" {
		t.Fatalf("expected title 'Test', got %q", got.Title)
	}

	// Wrong user should return ErrNoRows.
	_, err = GetConversation(d, c.ID, 999)
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestDeleteConversation(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "To Delete", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := DeleteConversation(d, c.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should be gone.
	_, err = GetConversation(d, c.ID, 1)
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows after delete, got %v", err)
	}

	// Deleting again should return ErrNoRows.
	if err := DeleteConversation(d, c.ID, 1); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for re-delete, got %v", err)
	}

	// Wrong user.
	c2, _ := CreateConversation(d, 1, "Other", "claude-sonnet-4-6")
	if err := DeleteConversation(d, c2.ID, 999); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestRenameConversation(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := RenameConversation(d, c.ID, 1, "New Title")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if updated.Title != "New Title" {
		t.Fatalf("expected 'New Title', got %q", updated.Title)
	}

	// Wrong user.
	_, err = RenameConversation(d, c.ID, 999, "Hack")
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestUpdateSessionID(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Session Test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Initially empty.
	got, err := GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.SessionID != "" {
		t.Fatalf("expected empty session_id, got %q", got.SessionID)
	}

	// Update session ID.
	if err := UpdateSessionID(d, c.ID, 1, "sess-abc-123"); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err = GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get conversation after update: %v", err)
	}
	if got.SessionID != "sess-abc-123" {
		t.Fatalf("expected 'sess-abc-123', got %q", got.SessionID)
	}
}

func TestMessages(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Chat", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Insert messages.
	m1, err := InsertMessage(d, c.ID, "user", "Hello")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if m1.Role != "user" || m1.Content != "Hello" {
		t.Fatalf("unexpected message: %+v", m1)
	}

	m2, err := InsertMessage(d, c.ID, "assistant", "Hi there!")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != m1.ID || msgs[1].ID != m2.ID {
		t.Fatalf("messages in wrong order")
	}

	// Cascade delete: deleting conversation should remove messages.
	if err := DeleteConversation(d, c.ID, 1); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	msgs, err = GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after cascade delete, got %d", len(msgs))
	}
}

// insertUser adds an extra user row so ownership rejection can be exercised.
func insertUser(t *testing.T, d *sql.DB, id int64) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO users (id, google_id, email, name, picture) VALUES (?, ?, ?, 'Other', '')`,
		id, "g"+strconv.FormatInt(id, 10), "other"+strconv.FormatInt(id, 10)+"@example.com",
	)
	if err != nil {
		t.Fatalf("create user %d: %v", id, err)
	}
}

func TestTruncateFrom(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Truncate me", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A second conversation that must be left untouched.
	other, err := CreateConversation(d, 1, "Untouched", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := InsertMessage(d, other.ID, "user", "Other conversation"); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	m1, err := InsertMessage(d, c.ID, "user", "First question")
	if err != nil {
		t.Fatalf("insert m1: %v", err)
	}
	m2, err := InsertMessage(d, c.ID, "assistant", "First answer")
	if err != nil {
		t.Fatalf("insert m2: %v", err)
	}
	m3, err := InsertMessage(d, c.ID, "user", "Second question")
	if err != nil {
		t.Fatalf("insert m3: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "assistant", "Second answer"); err != nil {
		t.Fatalf("insert m4: %v", err)
	}

	if err := UpdateSessionID(d, c.ID, 1, "session-abc"); err != nil {
		t.Fatalf("set session id: %v", err)
	}

	got, err := TruncateFrom(d, c.ID, 1, m3.ID)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if got.ID != m3.ID || got.Role != "user" || got.Content != "Second question" {
		t.Fatalf("unexpected truncated message: %+v", got)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 surviving messages, got %d", len(msgs))
	}
	if msgs[0].ID != m1.ID || msgs[1].ID != m2.ID {
		t.Fatalf("wrong messages survived: %+v", msgs)
	}

	// session_id is cleared so the next turn starts a fresh Claude CLI session.
	convo, err := GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if convo.SessionID != "" {
		t.Fatalf("expected session_id to be cleared, got %q", convo.SessionID)
	}

	// The other conversation is untouched.
	otherMsgs, err := GetMessages(d, other.ID)
	if err != nil {
		t.Fatalf("get other messages: %v", err)
	}
	if len(otherMsgs) != 1 {
		t.Fatalf("expected other conversation untouched, got %d messages", len(otherMsgs))
	}
}

func TestTruncateFrom_FirstMessageRemovesAll(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Wipe", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m1, err := InsertMessage(d, c.ID, "user", "Hello")
	if err != nil {
		t.Fatalf("insert m1: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "assistant", "Hi"); err != nil {
		t.Fatalf("insert m2: %v", err)
	}

	if _, err := TruncateFrom(d, c.ID, 1, m1.ID); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestTruncateFrom_UnknownMessage(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Chat", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "user", "Hello"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := TruncateFrom(d, c.ID, 1, 99999); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected the message to survive, got %d", len(msgs))
	}
}

func TestTruncateFrom_ForeignConversation(t *testing.T) {
	d := setupTestDB(t)
	insertUser(t, d, 2)

	c, err := CreateConversation(d, 2, "Someone else's chat", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m1, err := InsertMessage(d, c.ID, "user", "Private")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := UpdateSessionID(d, c.ID, 2, "session-xyz"); err != nil {
		t.Fatalf("set session id: %v", err)
	}

	if _, err := TruncateFrom(d, c.ID, 1, m1.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for foreign conversation, got %v", err)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected message to survive, got %d", len(msgs))
	}
	convo, err := GetConversation(d, c.ID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if convo.SessionID != "session-xyz" {
		t.Fatalf("expected session_id untouched, got %q", convo.SessionID)
	}
}

func TestTruncateFrom_MessageFromAnotherConversation(t *testing.T) {
	d := setupTestDB(t)

	a, err := CreateConversation(d, 1, "A", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateConversation(d, 1, "B", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	bMsg, err := InsertMessage(d, b.ID, "user", "In B")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertMessage(d, a.ID, "user", "In A"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := TruncateFrom(d, a.ID, 1, bMsg.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	aMsgs, err := GetMessages(d, a.ID)
	if err != nil {
		t.Fatalf("get a messages: %v", err)
	}
	bMsgs, err := GetMessages(d, b.ID)
	if err != nil {
		t.Fatalf("get b messages: %v", err)
	}
	if len(aMsgs) != 1 || len(bMsgs) != 1 {
		t.Fatalf("expected both conversations untouched, got a=%d b=%d", len(aMsgs), len(bMsgs))
	}
}

func TestEncryptionAtRest(t *testing.T) {
	d := setupTestDB(t)

	const title = "Secret Title"
	const content = "Secret message body"

	c, err := CreateConversation(d, 1, title, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m, err := InsertMessage(d, c.ID, "user", content)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	storedTitle := rawColumn(t, d, `SELECT title FROM chat_conversations WHERE id = ?`, c.ID)
	if storedTitle == title {
		t.Fatalf("title stored as plaintext: %q", storedTitle)
	}
	if !strings.HasPrefix(storedTitle, "enc:") {
		t.Fatalf("expected ciphertext prefix on stored title, got %q", storedTitle)
	}

	storedContent := rawColumn(t, d, `SELECT content FROM chat_messages WHERE id = ?`, m.ID)
	if storedContent == content {
		t.Fatalf("content stored as plaintext: %q", storedContent)
	}
	if !strings.HasPrefix(storedContent, "enc:") {
		t.Fatalf("expected ciphertext prefix on stored content, got %q", storedContent)
	}

	// Model, role, session_id and timestamps stay queryable plaintext.
	storedModel := rawColumn(t, d, `SELECT model FROM chat_conversations WHERE id = ?`, c.ID)
	if storedModel != "claude-sonnet-4-6" {
		t.Fatalf("expected plaintext model, got %q", storedModel)
	}
	storedRole := rawColumn(t, d, `SELECT role FROM chat_messages WHERE id = ?`, m.ID)
	if storedRole != "user" {
		t.Fatalf("expected plaintext role, got %q", storedRole)
	}
}

func TestEncryptionRoundTrip(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Round Trip", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "user", "Hello there"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "assistant", "General Kenobi"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Round Trip" {
		t.Fatalf("expected decrypted title, got %q", got.Title)
	}

	convos, err := ListConversations(d, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 1 || convos[0].Title != "Round Trip" {
		t.Fatalf("expected decrypted title in list, got %+v", convos)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "Hello there" || msgs[1].Content != "General Kenobi" {
		t.Fatalf("expected decrypted message content, got %+v", msgs)
	}

	// Rename and UpdateConversation also round-trip.
	renamed, err := RenameConversation(d, c.ID, 1, "Renamed")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("expected 'Renamed', got %q", renamed.Title)
	}
	if stored := rawColumn(t, d, `SELECT title FROM chat_conversations WHERE id = ?`, c.ID); stored == "Renamed" {
		t.Fatalf("rename stored plaintext title")
	}

	newTitle := "Updated"
	updated, err := UpdateConversation(d, c.ID, 1, &newTitle, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Updated" {
		t.Fatalf("expected 'Updated', got %q", updated.Title)
	}
	if stored := rawColumn(t, d, `SELECT title FROM chat_conversations WHERE id = ?`, c.ID); stored == "Updated" {
		t.Fatalf("update stored plaintext title")
	}
}

func TestEmptyTitleStaysEmpty(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Title != "" {
		t.Fatalf("expected empty title from create, got %q", c.Title)
	}
	if stored := rawColumn(t, d, `SELECT title FROM chat_conversations WHERE id = ?`, c.ID); stored != "" {
		t.Fatalf("expected empty title stored as empty, got %q", stored)
	}
	got, err := GetConversation(d, c.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "" {
		t.Fatalf("expected empty title on read, got %q", got.Title)
	}
}

func TestLegacyPlaintextRowsStillRead(t *testing.T) {
	d := setupTestDB(t)

	// Simulate rows written before encryption was introduced: no ciphertext prefix.
	res, err := d.Exec(
		`INSERT INTO chat_conversations (user_id, title, model, created_at, updated_at)
		 VALUES (1, 'Legacy Title', 'claude-sonnet-4-6', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
	)
	if err != nil {
		t.Fatalf("insert legacy conversation: %v", err)
	}
	convoID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO chat_messages (conversation_id, role, content, created_at)
		 VALUES (?, 'user', 'Legacy content', '2026-01-01T00:00:00.000Z')`,
		convoID,
	); err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}

	got, err := GetConversation(d, convoID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Legacy Title" {
		t.Fatalf("expected legacy title unchanged, got %q", got.Title)
	}

	convos, err := ListConversations(d, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 1 || convos[0].Title != "Legacy Title" {
		t.Fatalf("expected legacy title in list, got %+v", convos)
	}

	msgs, err := GetMessages(d, convoID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "Legacy content" {
		t.Fatalf("expected legacy content unchanged, got %+v", msgs)
	}

	// A legacy row is re-encrypted the next time it is written.
	if _, err := RenameConversation(d, convoID, 1, "Now Encrypted"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	stored := rawColumn(t, d, `SELECT title FROM chat_conversations WHERE id = ?`, convoID)
	if !strings.HasPrefix(stored, "enc:") {
		t.Fatalf("expected re-encrypted title, got %q", stored)
	}
}

func TestCorruptCiphertextFallsBackToRawValue(t *testing.T) {
	d := setupTestDB(t)

	res, err := d.Exec(
		`INSERT INTO chat_conversations (user_id, title, model, created_at, updated_at)
		 VALUES (1, 'enc:not-valid-ciphertext', 'claude-sonnet-4-6', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
	)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	convoID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO chat_messages (conversation_id, role, content, created_at)
		 VALUES (?, 'user', 'enc:also-garbage', '2026-01-01T00:00:00.000Z')`,
		convoID,
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	got, err := GetConversation(d, convoID, 1)
	if err != nil {
		t.Fatalf("get should not fail on corrupt ciphertext: %v", err)
	}
	if got.Title != "enc:not-valid-ciphertext" {
		t.Fatalf("expected raw stored title, got %q", got.Title)
	}

	convos, err := ListConversations(d, 1)
	if err != nil {
		t.Fatalf("list should not fail on corrupt ciphertext: %v", err)
	}
	if len(convos) != 1 || convos[0].Title != "enc:not-valid-ciphertext" {
		t.Fatalf("expected raw stored title in list, got %+v", convos)
	}

	msgs, err := GetMessages(d, convoID)
	if err != nil {
		t.Fatalf("get messages should not fail on corrupt ciphertext: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "enc:also-garbage" {
		t.Fatalf("expected raw stored content, got %+v", msgs)
	}
}

func TestTruncateFromReturnsDecryptedContent(t *testing.T) {
	d := setupTestDB(t)

	c, err := CreateConversation(d, 1, "Truncate", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	target, err := InsertMessage(d, c.ID, "user", "Regenerate me")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertMessage(d, c.ID, "assistant", "Old reply"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	deleted, err := TruncateFrom(d, c.ID, 1, target.ID)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if deleted.Content != "Regenerate me" {
		t.Fatalf("expected decrypted content, got %q", deleted.Content)
	}

	msgs, err := GetMessages(d, c.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected all messages deleted, got %d", len(msgs))
	}
}
