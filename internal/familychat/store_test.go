package familychat

import (
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
