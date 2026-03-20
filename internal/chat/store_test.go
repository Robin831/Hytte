package chat

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
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
