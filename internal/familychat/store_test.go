package familychat

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStoreCreateAndGetConversation(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	c, err := CreateConversation(db, 1, "Family Dinner", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID <= 0 {
		t.Fatalf("expected positive id, got %d", c.ID)
	}
	if c.Name != "Family Dinner" {
		t.Errorf("name = %q, want %q", c.Name, "Family Dinner")
	}
	if c.OwnerUserID != 1 {
		t.Errorf("owner = %d, want 1", c.OwnerUserID)
	}
	if len(c.MemberIDs) != 2 {
		t.Errorf("expected 2 members, got %d: %v", len(c.MemberIDs), c.MemberIDs)
	}

	got, err := GetConversation(db, c.ID, 2)
	if err != nil {
		t.Fatalf("get as member: %v", err)
	}
	if got.Name != "Family Dinner" {
		t.Errorf("member sees name %q, want %q", got.Name, "Family Dinner")
	}
}

func TestStoreGetConversation_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	c, err := CreateConversation(db, 1, "Private", []int64{2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = GetConversation(db, c.ID, 3)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestStoreListConversations(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	c1, err := CreateConversation(db, 1, "First", []int64{2})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	c2, err := CreateConversation(db, 1, "Second", []int64{2, 3})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	convos, err := ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convos))
	}
	if convos[0].ID != c2.ID || convos[1].ID != c1.ID {
		t.Errorf("order = [%d %d], want [%d %d]", convos[0].ID, convos[1].ID, c2.ID, c1.ID)
	}

	convos3, err := ListConversations(db, 3)
	if err != nil {
		t.Fatalf("list as carol: %v", err)
	}
	if len(convos3) != 1 || convos3[0].ID != c2.ID {
		t.Errorf("carol sees %v, want only conversation %d", convos3, c2.ID)
	}

	convos999, err := ListConversations(db, 999)
	if err != nil {
		t.Fatalf("list as non-member: %v", err)
	}
	if len(convos999) != 0 {
		t.Errorf("non-member sees %d conversations, want 0", len(convos999))
	}
}

func TestStoreCreateMessage_EncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	const plaintext = "this is a secret message"
	msg, err := CreateMessage(db, c.ID, 1, plaintext, "", "")
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}
	if msg.Body != plaintext {
		t.Errorf("returned body = %q, want %q", msg.Body, plaintext)
	}

	var raw string
	if err := db.QueryRow(`SELECT body FROM family_chat_messages WHERE id = ?`, msg.ID).Scan(&raw); err != nil {
		t.Fatalf("select body: %v", err)
	}
	if raw == plaintext {
		t.Fatalf("body stored as plaintext: %q", raw)
	}
	if !strings.HasPrefix(raw, "enc:") {
		t.Fatalf("body missing ciphertext prefix: %q", raw)
	}
}

func TestStoreCreateMessage_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	_, err = CreateMessage(db, c.ID, 3, "I should not be able to do this", "", "")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ?`, c.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages after forbidden create, got %d", count)
	}
}

func TestStoreListMessages_SinceAndLimit(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	var ids []int64
	for i := 0; i < 5; i++ {
		msg, err := CreateMessage(db, c.ID, 1, "msg", "", "")
		if err != nil {
			t.Fatalf("create msg %d: %v", i, err)
		}
		ids = append(ids, msg.ID)
	}

	// Default order is newest-first. limit=2 returns the 2 most recently inserted.
	msgs, err := ListMessages(db, c.ID, 1, 0, 2)
	if err != nil {
		t.Fatalf("list limit=2: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("limit=2 returned %d messages, want 2", len(msgs))
	}
	if msgs[0].ID != ids[4] || msgs[1].ID != ids[3] {
		t.Errorf("limit=2 ids = [%d %d], want [%d %d]", msgs[0].ID, msgs[1].ID, ids[4], ids[3])
	}

	// since=ids[1] returns ids > ids[1], newest-first: ids[4], ids[3], ids[2].
	msgs, err = ListMessages(db, c.ID, 1, ids[1], 10)
	if err != nil {
		t.Fatalf("list since: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("since returned %d messages, want 3", len(msgs))
	}
}

func TestStoreListMessages_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	_, err = ListMessages(db, c.ID, 3, 0, 100)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestStoreMarkRead_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	err = MarkRead(db, c.ID, 3, "")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestStoreUnreadCount(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	c, err := CreateConversation(db, 1, "", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := CreateMessage(db, c.ID, 1, "hi", "", ""); err != nil {
			t.Fatalf("create msg: %v", err)
		}
	}

	got, err := GetConversation(db, c.ID, 2)
	if err != nil {
		t.Fatalf("get conv: %v", err)
	}
	if got.UnreadCount != 3 {
		t.Errorf("unread = %d, want 3", got.UnreadCount)
	}

	if err := MarkRead(db, c.ID, 2, "9999-12-31T23:59:59.000000000Z"); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	got, err = GetConversation(db, c.ID, 2)
	if err != nil {
		t.Fatalf("get conv after mark: %v", err)
	}
	if got.UnreadCount != 0 {
		t.Errorf("unread after mark = %d, want 0", got.UnreadCount)
	}
}

func TestStoreListConversations_LastMessagePreview(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	c, err := CreateConversation(db, 1, "Preview Test", []int64{2})
	if err != nil {
		t.Fatalf("create conv: %v", err)
	}

	// No messages yet — preview should be empty.
	convos, err := ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list (empty): %v", err)
	}
	if len(convos) != 1 || convos[0].LastMessagePreview != "" {
		t.Errorf("expected empty preview before any message, got %q", convos[0].LastMessagePreview)
	}

	// First message — preview reflects it (decrypted).
	if _, err := CreateMessage(db, c.ID, 1, "first", "", ""); err != nil {
		t.Fatalf("create msg 1: %v", err)
	}
	convos, err = ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list after first: %v", err)
	}
	if convos[0].LastMessagePreview != "first" {
		t.Errorf("preview = %q, want %q", convos[0].LastMessagePreview, "first")
	}
	if convos[0].LastMessageSenderID != 1 {
		t.Errorf("preview sender = %d, want 1", convos[0].LastMessageSenderID)
	}

	// Second message replaces the preview.
	if _, err := CreateMessage(db, c.ID, 2, "second", "", ""); err != nil {
		t.Fatalf("create msg 2: %v", err)
	}
	convos, err = ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list after second: %v", err)
	}
	if convos[0].LastMessagePreview != "second" {
		t.Errorf("preview = %q, want %q", convos[0].LastMessagePreview, "second")
	}
	if convos[0].LastMessageSenderID != 2 {
		t.Errorf("preview sender = %d, want 2", convos[0].LastMessageSenderID)
	}

	// Long bodies get truncated with an ellipsis so the list stays compact.
	long := strings.Repeat("a", previewMaxRunes+50)
	if _, err := CreateMessage(db, c.ID, 1, long, "", ""); err != nil {
		t.Fatalf("create long msg: %v", err)
	}
	convos, err = ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list after long: %v", err)
	}
	preview := convos[0].LastMessagePreview
	if !strings.HasSuffix(preview, "…") {
		t.Errorf("expected truncated preview to end with ellipsis, got %q", preview)
	}
	if len([]rune(preview)) != previewMaxRunes+1 {
		t.Errorf("preview rune length = %d, want %d", len([]rune(preview)), previewMaxRunes+1)
	}

	// Empty-body attachment-only messages surface a placeholder.
	if _, err := CreateMessage(db, c.ID, 1, "", "/uploads/file.png", "image/png"); err != nil {
		t.Fatalf("create attach msg: %v", err)
	}
	convos, err = ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list after attach: %v", err)
	}
	if convos[0].LastMessagePreview == "" {
		t.Errorf("attachment-only message should have a placeholder preview, got empty")
	}
}

// overrideMsgTimes rewrites the created_at (and optionally edited_at/deleted_at)
// of a message so backfill watermark logic can be tested deterministically,
// independent of the wall clock. Empty edited/deleted strings leave that column
// untouched.
func overrideMsgTimes(t *testing.T, db *sql.DB, msgID int64, created, edited, deleted string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE family_chat_messages SET created_at = ? WHERE id = ?`, created, msgID); err != nil {
		t.Fatalf("override created_at for %d: %v", msgID, err)
	}
	if edited != "" {
		if _, err := db.Exec(`UPDATE family_chat_messages SET edited_at = ? WHERE id = ?`, edited, msgID); err != nil {
			t.Fatalf("override edited_at for %d: %v", msgID, err)
		}
	}
	if deleted != "" {
		if _, err := db.Exec(`UPDATE family_chat_messages SET deleted_at = ? WHERE id = ?`, deleted, msgID); err != nil {
			t.Fatalf("override deleted_at for %d: %v", msgID, err)
		}
	}
}

func TestEventsSince_NewMessagesOnly(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "a@example.com")
	makeUser(t, db, 2, "b@example.com")
	conv := seedConversation(t, db, 1, "c", 2)

	m1, err := CreateMessage(db, conv, 1, "one", "", "")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := CreateMessage(db, conv, 2, "two", "", "")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	m3, err := CreateMessage(db, conv, 1, "three", "", "")
	if err != nil {
		t.Fatalf("create m3: %v", err)
	}

	got, err := EventsSince(db, conv, 1, m1.ID, 100)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 backfill messages, got %d", len(got))
	}
	// Ascending id order so the client replays in chronological order.
	if got[0].ID != m2.ID || got[1].ID != m3.ID {
		t.Fatalf("expected ids [%d %d], got [%d %d]", m2.ID, m3.ID, got[0].ID, got[1].ID)
	}
	if got[0].Body != "two" || got[1].Body != "three" {
		t.Fatalf("bodies not decrypted: %q %q", got[0].Body, got[1].Body)
	}
	if got[0].Reactions == nil {
		t.Errorf("expected non-nil reactions map on backfilled message")
	}
}

func TestEventsSince_ReplaysEditAndDeleteOfOlderMessages(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "a@example.com")
	makeUser(t, db, 2, "b@example.com")
	conv := seedConversation(t, db, 1, "c", 2)

	m1, _ := CreateMessage(db, conv, 1, "one", "", "")
	m2, _ := CreateMessage(db, conv, 2, "two", "", "")
	m3, _ := CreateMessage(db, conv, 1, "three", "", "")
	m4, _ := CreateMessage(db, conv, 2, "four", "", "")

	// Deterministic ordering of creation timestamps.
	overrideMsgTimes(t, db, m1.ID, "2026-01-01T00:00:01.000Z", "", "")
	overrideMsgTimes(t, db, m2.ID, "2026-01-01T00:00:02.000Z", "", "")
	overrideMsgTimes(t, db, m3.ID, "2026-01-01T00:00:03.000Z", "", "")
	overrideMsgTimes(t, db, m4.ID, "2026-01-01T00:00:04.000Z", "", "")

	// Edit m1 (after the watermark) and m2 (before the watermark); delete m3
	// (after the watermark). The resume point is m4, so its created_at (:04) is
	// the watermark.
	if _, err := EditMessage(db, conv, m1.ID, 1, "one-edited"); err != nil {
		t.Fatalf("edit m1: %v", err)
	}
	overrideMsgTimes(t, db, m1.ID, "2026-01-01T00:00:01.000Z", "2026-01-01T00:00:10.000Z", "")
	if _, err := EditMessage(db, conv, m2.ID, 2, "two-edited"); err != nil {
		t.Fatalf("edit m2: %v", err)
	}
	overrideMsgTimes(t, db, m2.ID, "2026-01-01T00:00:02.000Z", "2026-01-01T00:00:02.500Z", "")
	if _, err := SoftDeleteMessage(db, conv, m3.ID, 1); err != nil {
		t.Fatalf("delete m3: %v", err)
	}
	overrideMsgTimes(t, db, m3.ID, "2026-01-01T00:00:03.000Z", "", "2026-01-01T00:00:11.000Z")

	got, err := EventsSince(db, conv, 1, m4.ID, 100)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	// Expect m1 (edited after watermark) and m3 (deleted after watermark) only —
	// m2's edit predates the watermark and is excluded, and there are no newer
	// messages than m4.
	if len(got) != 2 {
		t.Fatalf("expected 2 backfill messages, got %d: %+v", len(got), got)
	}
	if got[0].ID != m1.ID || got[1].ID != m3.ID {
		t.Fatalf("expected ids [%d %d], got [%d %d]", m1.ID, m3.ID, got[0].ID, got[1].ID)
	}
	if got[0].Body != "one-edited" {
		t.Errorf("expected edited body for m1, got %q", got[0].Body)
	}
	if got[0].EditedAt == nil {
		t.Errorf("expected edited_at set for m1")
	}
	// m3 is a tombstone: deleted_at set, body cleared.
	if got[1].DeletedAt == nil {
		t.Errorf("expected deleted_at set for m3")
	}
	if got[1].Body != "" {
		t.Errorf("expected cleared body for tombstone m3, got %q", got[1].Body)
	}
}

func TestEventsSince_UnknownResumeIDReturnsOnlyNewer(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "a@example.com")
	makeUser(t, db, 2, "b@example.com")
	conv := seedConversation(t, db, 1, "c", 2)

	m1, _ := CreateMessage(db, conv, 1, "one", "", "")
	// Edit m1 so an edited row exists; a stale/unknown resume id must NOT pull it
	// in (no watermark → edit predicate skipped).
	if _, err := EditMessage(db, conv, m1.ID, 1, "one-edited"); err != nil {
		t.Fatalf("edit m1: %v", err)
	}

	// Resume from an id larger than any message: unknown row → no watermark, and
	// id > since matches nothing.
	got, err := EventsSince(db, conv, 1, m1.ID+9999, 100)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty backfill for unknown resume id, got %d", len(got))
	}
}
