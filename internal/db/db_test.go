package db

import (
	"database/sql"
	"testing"
	"time"
)

func TestOrphanCleanup(t *testing.T) {
	database := initTestDB(t)

	// Insert a workout.
	var err error
	_, err = database.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (100, 1, 'running', '2025-01-01T00:00:00Z', 'hash1', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Insert valid child rows (parent workout_id=100 exists).
	_, err = database.Exec(`INSERT INTO workout_laps (workout_id, lap_number) VALUES (100, 1)`)
	if err != nil {
		t.Fatalf("insert valid lap: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_samples (workout_id, data) VALUES (100, '[]')`)
	if err != nil {
		t.Fatalf("insert valid sample: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (100, 'easy')`)
	if err != nil {
		t.Fatalf("insert valid tag: %v", err)
	}

	// Simulate orphaned rows by disabling foreign keys temporarily.
	_, err = database.Exec(`PRAGMA foreign_keys = OFF`)
	if err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_laps (workout_id, lap_number) VALUES (999, 1)`)
	if err != nil {
		t.Fatalf("insert orphan lap: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_samples (workout_id, data) VALUES (999, '[]')`)
	if err != nil {
		t.Fatalf("insert orphan sample: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (999, 'orphan')`)
	if err != nil {
		t.Fatalf("insert orphan tag: %v", err)
	}
	_, err = database.Exec(`PRAGMA foreign_keys = ON`)
	if err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	// Run createSchema again — orphan cleanup should remove the bad rows.
	if err := createSchema(database); err != nil {
		t.Fatalf("createSchema: %v", err)
	}

	// Verify orphaned rows are gone.
	for _, q := range []struct {
		table string
		query string
	}{
		{"workout_laps", "SELECT COUNT(*) FROM workout_laps WHERE workout_id = 999"},
		{"workout_samples", "SELECT COUNT(*) FROM workout_samples WHERE workout_id = 999"},
		{"workout_tags", "SELECT COUNT(*) FROM workout_tags WHERE workout_id = 999"},
	} {
		var count int
		if err := database.QueryRow(q.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if count != 0 {
			t.Errorf("expected 0 orphaned rows in %s, got %d", q.table, count)
		}
	}

	// Verify valid rows are still present.
	for _, q := range []struct {
		table string
		query string
	}{
		{"workout_laps", "SELECT COUNT(*) FROM workout_laps WHERE workout_id = 100"},
		{"workout_samples", "SELECT COUNT(*) FROM workout_samples WHERE workout_id = 100"},
		{"workout_tags", "SELECT COUNT(*) FROM workout_tags WHERE workout_id = 100"},
	} {
		var count int
		if err := database.QueryRow(q.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if count != 1 {
			t.Errorf("expected 1 valid row in %s, got %d", q.table, count)
		}
	}
}

func initTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g1')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return database
}

func TestChatConversationInsertAndRetrieve(t *testing.T) {
	db := initTestDB(t)

	_, err := db.Exec(`INSERT INTO chat_conversations (user_id, model, title) VALUES (1, 'claude-sonnet-4-6', 'Test Chat')`)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}

	var id int64
	var userID int64
	var title, model, createdAt, updatedAt string
	err = db.QueryRow(`SELECT id, user_id, title, model, created_at, updated_at FROM chat_conversations WHERE user_id = 1`).
		Scan(&id, &userID, &title, &model, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("query conversation: %v", err)
	}

	if title != "Test Chat" {
		t.Errorf("expected title 'Test Chat', got %q", title)
	}
	if model != "claude-sonnet-4-6" {
		t.Errorf("expected model 'claude-sonnet-4-6', got %q", model)
	}

	// Verify UTC default timestamps are populated and parseable.
	ts, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Fatalf("parse created_at %q: %v", createdAt, err)
	}
	if time.Since(ts) > 10*time.Second {
		t.Errorf("created_at too old: %v", ts)
	}
	if createdAt == "" || updatedAt == "" {
		t.Error("expected non-empty default timestamps")
	}
}

func TestChatMessageInsertAndRetrieve(t *testing.T) {
	db := initTestDB(t)

	res, err := db.Exec(`INSERT INTO chat_conversations (user_id, model) VALUES (1, 'claude-sonnet-4-6')`)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	convID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO chat_messages (conversation_id, role, content) VALUES (?, 'user', 'Hello')`, convID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	_, err = db.Exec(`INSERT INTO chat_messages (conversation_id, role, content) VALUES (?, 'assistant', 'Hi there!')`, convID)
	if err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE conversation_id = ?`, convID).Scan(&count)
	if err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 messages, got %d", count)
	}

	var role, content, createdAt string
	err = db.QueryRow(`SELECT role, content, created_at FROM chat_messages WHERE conversation_id = ? ORDER BY id LIMIT 1`, convID).
		Scan(&role, &content, &createdAt)
	if err != nil {
		t.Fatalf("query message: %v", err)
	}
	if role != "user" || content != "Hello" {
		t.Errorf("unexpected message: role=%q content=%q", role, content)
	}
	if createdAt == "" {
		t.Error("expected non-empty created_at default")
	}
}

func TestChatCascadeDeleteConversation(t *testing.T) {
	db := initTestDB(t)

	res, err := db.Exec(`INSERT INTO chat_conversations (user_id, model) VALUES (1, 'claude-sonnet-4-6')`)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	convID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO chat_messages (conversation_id, role, content) VALUES (?, 'user', 'Hello')`, convID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Delete conversation — messages should cascade.
	_, err = db.Exec(`DELETE FROM chat_conversations WHERE id = ?`, convID)
	if err != nil {
		t.Fatalf("delete conversation: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE conversation_id = ?`, convID).Scan(&count)
	if err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d", count)
	}
}

func TestChatCascadeDeleteUser(t *testing.T) {
	db := initTestDB(t)

	res, err := db.Exec(`INSERT INTO chat_conversations (user_id, model) VALUES (1, 'claude-sonnet-4-6')`)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	convID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO chat_messages (conversation_id, role, content) VALUES (?, 'user', 'Hello')`, convID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Delete user — conversations and messages should cascade.
	_, err = db.Exec(`DELETE FROM users WHERE id = 1`)
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var convCount, msgCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_conversations WHERE user_id = 1`).Scan(&convCount); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE conversation_id = ?`, convID).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}

	if convCount != 0 {
		t.Errorf("expected 0 conversations after user delete, got %d", convCount)
	}
	if msgCount != 0 {
		t.Errorf("expected 0 messages after user delete, got %d", msgCount)
	}
}

func TestOrphanCleanupIdempotent(t *testing.T) {
	database := initTestDB(t)

	// Running createSchema on a clean DB should succeed without errors.
	if err := createSchema(database); err != nil {
		t.Fatalf("second createSchema should be idempotent: %v", err)
	}
}
