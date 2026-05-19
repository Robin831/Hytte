package familychat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-familychat-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	// Pin to a single connection so :memory: stays consistent across queries.
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	t.Cleanup(func() { d.Close() })

	for _, u := range []struct {
		id      int64
		google  string
		email   string
		name    string
	}{
		{1, "g1", "alice@example.com", "Alice"},
		{2, "g2", "bob@example.com", "Bob"},
		{3, "g3", "carol@example.com", "Carol"},
	} {
		_, err := d.Exec(
			`INSERT INTO users (id, google_id, email, name, picture) VALUES (?, ?, ?, ?, '')`,
			u.id, u.google, u.email, u.name,
		)
		if err != nil {
			t.Fatalf("create user %d: %v", u.id, err)
		}
	}
	return d
}

func TestCreateConversationAndGet(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := CreateConversation(ctx, d, 1, "Family Dinner", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	got, err := GetConversation(ctx, d, id, 1)
	if err != nil {
		t.Fatalf("get as owner: %v", err)
	}
	if got.Name != "Family Dinner" {
		t.Errorf("name = %q, want %q", got.Name, "Family Dinner")
	}
	if got.OwnerID != 1 {
		t.Errorf("owner = %d, want 1", got.OwnerID)
	}

	got2, err := GetConversation(ctx, d, id, 2)
	if err != nil {
		t.Fatalf("get as member: %v", err)
	}
	if got2.Name != "Family Dinner" {
		t.Errorf("member sees name %q, want %q", got2.Name, "Family Dinner")
	}
}

func TestGetConversation_NonMember(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := CreateConversation(ctx, d, 1, "Private", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = GetConversation(ctx, d, id, 3)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestListUserConversations(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	c1, err := CreateConversation(ctx, d, 1, "First", []int64{2})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	// Ensure distinct updated_at ordering.
	time.Sleep(2 * time.Millisecond)
	c2, err := CreateConversation(ctx, d, 1, "Second", []int64{2, 3})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	convos, err := ListUserConversations(ctx, d, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convos))
	}
	// Newest first.
	if convos[0].ID != c2 || convos[1].ID != c1 {
		t.Errorf("order = [%d %d], want [%d %d]", convos[0].ID, convos[1].ID, c2, c1)
	}

	convos3, err := ListUserConversations(ctx, d, 3)
	if err != nil {
		t.Fatalf("list as 3: %v", err)
	}
	if len(convos3) != 1 || convos3[0].ID != c2 {
		t.Errorf("user 3 sees %v, want only conversation %d", convos3, c2)
	}

	convos4, err := ListUserConversations(ctx, d, 999)
	if err != nil {
		t.Fatalf("list as 999: %v", err)
	}
	if len(convos4) != 0 {
		t.Errorf("non-member sees %d conversations, want 0", len(convos4))
	}
}

func TestAppendAndListMessages(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	m1, err := AppendMessage(ctx, d, convID, 1, "hello bob", "", "")
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	m2, err := AppendMessage(ctx, d, convID, 2, "hi alice", "", "")
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if m2 <= m1 {
		t.Errorf("expected ascending IDs, got %d then %d", m1, m2)
	}

	msgs, err := ListMessages(ctx, d, convID, 1, 0, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Body != "hello bob" || msgs[1].Body != "hi alice" {
		t.Errorf("bodies = [%q %q], want [hello bob, hi alice]", msgs[0].Body, msgs[1].Body)
	}
	if msgs[0].ID >= msgs[1].ID {
		t.Errorf("expected ascending order, got %d then %d", msgs[0].ID, msgs[1].ID)
	}
}

func TestMessageEncryptedAtRest(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const plaintext = "this is a secret message"
	if _, err := AppendMessage(ctx, d, convID, 1, plaintext, "", ""); err != nil {
		t.Fatalf("append: %v", err)
	}

	var raw string
	if err := d.QueryRow(`SELECT body_enc FROM family_chat_messages WHERE conversation_id = ?`, convID).Scan(&raw); err != nil {
		t.Fatalf("select body_enc: %v", err)
	}
	if raw == plaintext {
		t.Fatalf("body stored as plaintext: %q", raw)
	}
	if !strings.HasPrefix(raw, "enc:") {
		t.Fatalf("body_enc missing ciphertext prefix: %q", raw)
	}

	// And ensure decrypt round-trips.
	msgs, err := ListMessages(ctx, d, convID, 1, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != plaintext {
		t.Fatalf("decrypted body = %v, want one message with body %q", msgs, plaintext)
	}
}

func TestConversationNameEncryptedAtRest(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	const name = "Top Secret Family Plans"
	convID, err := CreateConversation(ctx, d, 1, name, []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var raw string
	if err := d.QueryRow(`SELECT name_enc FROM family_chat_conversations WHERE id = ?`, convID).Scan(&raw); err != nil {
		t.Fatalf("select name_enc: %v", err)
	}
	if raw == name {
		t.Fatalf("name stored as plaintext: %q", raw)
	}
	if !strings.HasPrefix(raw, "enc:") {
		t.Fatalf("name_enc missing ciphertext prefix: %q", raw)
	}
}

func TestAppendMessage_NonMember(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = AppendMessage(ctx, d, convID, 3, "I should not be able to do this", "", "")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}

	// And no message should have been written.
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ?`, convID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages after forbidden append, got %d", count)
	}
}

func TestListMessages_NonMember(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := AppendMessage(ctx, d, convID, 1, "hello", "", ""); err != nil {
		t.Fatalf("append: %v", err)
	}

	_, err = ListMessages(ctx, d, convID, 3, 0, 100)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestListMessages_SinceAndLimit(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := AppendMessage(ctx, d, convID, 1, "msg", "", "")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// limit only.
	msgs, err := ListMessages(ctx, d, convID, 1, 0, 2)
	if err != nil {
		t.Fatalf("list limit: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != ids[0] || msgs[1].ID != ids[1] {
		t.Errorf("limit=2 returned %v, want [%d %d]", msgs, ids[0], ids[1])
	}

	// sinceID + limit.
	msgs, err = ListMessages(ctx, d, convID, 1, ids[1], 10)
	if err != nil {
		t.Fatalf("list since: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("sinceID returned %d messages, want 3", len(msgs))
	}
	if msgs[0].ID != ids[2] {
		t.Errorf("first id = %d, want %d", msgs[0].ID, ids[2])
	}

	// limit <= 0 returns empty.
	msgs, err = ListMessages(ctx, d, convID, 1, 0, 0)
	if err != nil {
		t.Fatalf("list limit=0: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("limit=0 returned %d messages, want 0", len(msgs))
	}
}

func TestAppendMessage_BumpsConversationUpdatedAt(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := GetConversation(ctx, d, convID, 1)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	if _, err := AppendMessage(ctx, d, convID, 1, "hi", "", ""); err != nil {
		t.Fatalf("append: %v", err)
	}

	second, err := GetConversation(ctx, d, convID, 1)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("UpdatedAt did not advance: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestAppendMessage_WithAttachment(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const path = "/data/attachments/1.jpg"
	const mime = "image/jpeg"
	if _, err := AppendMessage(ctx, d, convID, 1, "see pic", path, mime); err != nil {
		t.Fatalf("append: %v", err)
	}

	// attachment_path_enc should be ciphertext, mime stored plaintext.
	var rawPath, rawMime sql.NullString
	if err := d.QueryRow(
		`SELECT attachment_path_enc, attachment_mime FROM family_chat_messages WHERE conversation_id = ?`,
		convID,
	).Scan(&rawPath, &rawMime); err != nil {
		t.Fatalf("select attachment: %v", err)
	}
	if !rawPath.Valid || rawPath.String == path {
		t.Fatalf("attachment_path_enc not encrypted: %+v", rawPath)
	}
	if !strings.HasPrefix(rawPath.String, "enc:") {
		t.Fatalf("attachment_path_enc missing prefix: %q", rawPath.String)
	}
	if !rawMime.Valid || rawMime.String != mime {
		t.Fatalf("attachment_mime = %+v, want %q", rawMime, mime)
	}

	msgs, err := ListMessages(ctx, d, convID, 1, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].AttachmentPath != path {
		t.Errorf("attachment path = %q, want %q", msgs[0].AttachmentPath, path)
	}
	if msgs[0].AttachmentMime != mime {
		t.Errorf("attachment mime = %q, want %q", msgs[0].AttachmentMime, mime)
	}
}

func TestMarkRead(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Before MarkRead, last_read_at is NULL.
	var ts sql.NullTime
	if err := d.QueryRow(
		`SELECT last_read_at FROM family_chat_members WHERE conversation_id = ? AND user_id = ?`,
		convID, 2,
	).Scan(&ts); err != nil {
		t.Fatalf("scan last_read_at: %v", err)
	}
	if ts.Valid {
		t.Fatalf("expected NULL last_read_at, got %v", ts)
	}

	now := time.Now().UTC()
	if err := MarkRead(ctx, d, convID, 2, now); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	if err := d.QueryRow(
		`SELECT last_read_at FROM family_chat_members WHERE conversation_id = ? AND user_id = ?`,
		convID, 2,
	).Scan(&ts); err != nil {
		t.Fatalf("scan after mark: %v", err)
	}
	if !ts.Valid {
		t.Fatal("expected last_read_at to be set")
	}
}

func TestMarkRead_NonMember(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	convID, err := CreateConversation(ctx, d, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = MarkRead(ctx, d, convID, 3, time.Now())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}
