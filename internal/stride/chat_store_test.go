package stride

import (
	"strings"
	"testing"
)

func TestListChatMessages_Empty(t *testing.T) {
	db := setupTestDB(t)

	// Create a plan so we have a valid plan_id.
	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	msgs, err := ListChatMessages(db, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if msgs == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestAddChatMessage_RoundTrip(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Add a user message.
	msg, err := AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  1,
		Role:    "user",
		Content: "Move Thursday's tempo to Friday",
	})
	if err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if msg.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if msg.CreatedAt == "" {
		t.Fatal("expected non-empty created_at")
	}

	// Add an assistant message.
	_, err = AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  1,
		Role:    "assistant",
		Content: "Done! I moved the tempo run to Friday.",
	})
	if err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	// List and verify content is decrypted.
	msgs, err := ListChatMessages(db, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Move Thursday's tempo to Friday" {
		t.Errorf("unexpected content: %q", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msgs[1].Role)
	}
	if msgs[1].Content != "Done! I moved the tempo run to Friday." {
		t.Errorf("unexpected content: %q", msgs[1].Content)
	}

	// Verify content is actually encrypted in DB.
	var raw string
	if err := db.QueryRow(`SELECT content FROM stride_chat_messages WHERE id = ?`, msgs[0].ID).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if !strings.HasPrefix(raw, "enc:") {
		t.Errorf("expected encrypted content (enc: prefix), got %q", raw)
	}
}

func TestAddChatMessage_InvalidRole(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	_, err = AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  1,
		Role:    "system",
		Content: "hello",
	})
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddChatMessage_EmptyContent(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	_, err = AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  1,
		Role:    "user",
		Content: "",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "content must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddChatMessage_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	// Plan belongs to user 1.
	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Insert user 2.
	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')")
	if err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	// User 2 tries to add a message to user 1's plan.
	_, err = AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  2,
		Role:    "user",
		Content: "sneaky",
	})
	if err == nil {
		t.Fatal("expected error for wrong user")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChatSessionID_RoundTrip(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	if err := UpdateChatSessionID(db, 1, 1, "session-abc-123"); err != nil {
		t.Fatalf("update session id: %v", err)
	}

	sid, err := GetChatSessionID(db, 1, 1)
	if err != nil {
		t.Fatalf("get session id: %v", err)
	}
	if sid != "session-abc-123" {
		t.Errorf("expected 'session-abc-123', got %q", sid)
	}
}

func TestChatSessionID_DefaultEmpty(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	sid, err := GetChatSessionID(db, 1, 1)
	if err != nil {
		t.Fatalf("get session id: %v", err)
	}
	if sid != "" {
		t.Errorf("expected empty string, got %q", sid)
	}
}

func TestMarkMessagePlanModified(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO stride_plans (id, user_id, week_start, week_end, plan_json, created_at) VALUES (1, 1, '2026-04-13', '2026-04-19', '{}', '2026-04-13T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	msg, err := AddChatMessage(db, ChatMessage{
		PlanID:  1,
		UserID:  1,
		Role:    "assistant",
		Content: "Updated your plan.",
	})
	if err != nil {
		t.Fatalf("add message: %v", err)
	}
	if msg.PlanModified {
		t.Fatal("expected plan_modified=false initially")
	}

	if err := MarkMessagePlanModified(db, msg.ID, 1); err != nil {
		t.Fatalf("mark modified: %v", err)
	}

	msgs, err := ListChatMessages(db, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].PlanModified {
		t.Error("expected plan_modified=true after marking")
	}
}
