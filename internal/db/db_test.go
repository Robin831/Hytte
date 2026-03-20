package db

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
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

func setupEncryptionKey(t *testing.T) {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-for-db-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
}

// initTestDBWithoutMigration creates a test DB and removes the encryption
// sentinel so we can seed plaintext data and then run the migration.
func initTestDBWithPlaintext(t *testing.T) *sql.DB {
	t.Helper()
	setupEncryptionKey(t)

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

	// Remove the encryption sentinel so we can re-run the migration after
	// inserting plaintext data.
	database.Exec(`DELETE FROM schema_migrations WHERE key = 'data_encryption_migrated'`)

	return database
}

func TestMigrateEncryptData(t *testing.T) {
	db := initTestDBWithPlaintext(t)

	// Seed plaintext data into each table the migration handles.
	_, err := db.Exec(`INSERT INTO notes (id, user_id, title, content, created_at, updated_at)
		VALUES (1, 1, 'My Secret Note', 'This is private content', '2025-01-01', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	_, err = db.Exec(`INSERT INTO lactate_tests (id, user_id, date, comment, created_at, updated_at)
		VALUES (1, 1, '2025-01-01', 'test comment', '2025-01-01', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert lactate_test: %v", err)
	}

	_, err = db.Exec(`INSERT INTO lactate_test_stages (id, test_id, stage_number, speed_kmh, lactate_mmol, notes)
		VALUES (1, 1, 1, 12.0, 1.5, 'stage notes here')`)
	if err != nil {
		t.Fatalf("insert lactate_test_stage: %v", err)
	}

	_, err = db.Exec(`INSERT INTO vapid_keys (id, public_key, private_key)
		VALUES (1, 'pubkey123', 'privkey-secret-456')`)
	if err != nil {
		t.Fatalf("insert vapid_key: %v", err)
	}

	// Disable FK checks to insert push subscription without valid user ref issues.
	db.Exec(`PRAGMA foreign_keys = OFF`)
	_, err = db.Exec(`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, created_at)
		VALUES (1, 1, 'https://push.example.com', 'plain-p256dh-key', 'plain-auth-key', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert push_subscription: %v", err)
	}
	db.Exec(`PRAGMA foreign_keys = ON`)

	// Insert a workout and workout_analysis with plaintext.
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (1, 1, 'running', '2025-01-01T00:00:00Z', 'hash1', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	_, err = db.Exec(`INSERT INTO workout_analyses (id, user_id, workout_id, model, prompt, response_json)
		VALUES (1, 1, 1, 'claude', 'analyze this workout', '{"tags":["easy"]}')`)
	if err != nil {
		t.Fatalf("insert workout_analysis: %v", err)
	}

	_, err = db.Exec(`INSERT INTO comparison_analyses (id, user_id, workout_id_a, workout_id_b, model, prompt, response_json)
		VALUES (1, 1, 1, 1, 'claude', 'compare these workouts', '{"diff":"none"}')`)
	if err != nil {
		t.Fatalf("insert comparison_analysis: %v", err)
	}

	// Run the migration.
	if err := migrateEncryptData(db); err != nil {
		t.Fatalf("migrateEncryptData: %v", err)
	}

	// Verify all sensitive fields are now encrypted (have enc: prefix).
	assertEncrypted := func(table, column string, query string, args ...any) {
		t.Helper()
		var val string
		if err := db.QueryRow(query, args...).Scan(&val); err != nil {
			t.Fatalf("query %s.%s: %v", table, column, err)
		}
		if val == "" {
			return // empty values are not encrypted
		}
		if !strings.HasPrefix(val, "enc:") {
			t.Errorf("%s.%s should be encrypted, got %q", table, column, val[:min(len(val), 30)])
		}
		// Verify it decrypts back to the original.
		decrypted, err := encryption.DecryptField(val)
		if err != nil {
			t.Errorf("%s.%s failed to decrypt: %v", table, column, err)
		}
		if decrypted == "" {
			t.Errorf("%s.%s decrypted to empty string", table, column)
		}
	}

	assertEncrypted("notes", "title", `SELECT title FROM notes WHERE id = 1`)
	assertEncrypted("notes", "content", `SELECT content FROM notes WHERE id = 1`)
	assertEncrypted("lactate_tests", "comment", `SELECT comment FROM lactate_tests WHERE id = 1`)
	assertEncrypted("lactate_test_stages", "notes", `SELECT notes FROM lactate_test_stages WHERE id = 1`)
	assertEncrypted("push_subscriptions", "p256dh", `SELECT p256dh FROM push_subscriptions WHERE id = 1`)
	assertEncrypted("push_subscriptions", "auth", `SELECT auth FROM push_subscriptions WHERE id = 1`)
	assertEncrypted("vapid_keys", "private_key", `SELECT private_key FROM vapid_keys WHERE id = 1`)
	assertEncrypted("workout_analyses", "prompt", `SELECT prompt FROM workout_analyses WHERE id = 1`)
	assertEncrypted("workout_analyses", "response_json", `SELECT response_json FROM workout_analyses WHERE id = 1`)
	assertEncrypted("comparison_analyses", "prompt", `SELECT prompt FROM comparison_analyses WHERE id = 1`)
	assertEncrypted("comparison_analyses", "response_json", `SELECT response_json FROM comparison_analyses WHERE id = 1`)

	// Verify specific decrypted values.
	var title string
	db.QueryRow(`SELECT title FROM notes WHERE id = 1`).Scan(&title)
	decTitle, _ := encryption.DecryptField(title)
	if decTitle != "My Secret Note" {
		t.Errorf("expected decrypted title 'My Secret Note', got %q", decTitle)
	}

	// Verify sentinel was set.
	var sentinel int
	db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE key = 'data_encryption_migrated'`).Scan(&sentinel)
	if sentinel != 1 {
		t.Error("expected encryption sentinel to be set")
	}
}

func TestMigrateEncryptDataIdempotent(t *testing.T) {
	db := initTestDBWithPlaintext(t)

	// Seed plaintext data.
	_, err := db.Exec(`INSERT INTO notes (id, user_id, title, content, created_at, updated_at)
		VALUES (1, 1, 'Test Note', 'Test Content', '2025-01-01', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	// Run migration twice.
	if err := migrateEncryptData(db); err != nil {
		t.Fatalf("first migrateEncryptData: %v", err)
	}

	// Get the encrypted value after first run.
	var firstEncrypted string
	db.QueryRow(`SELECT title FROM notes WHERE id = 1`).Scan(&firstEncrypted)

	// Remove sentinel to simulate re-run.
	db.Exec(`DELETE FROM schema_migrations WHERE key = 'data_encryption_migrated'`)

	// Run again — should detect enc: prefix and skip already-encrypted values.
	if err := migrateEncryptData(db); err != nil {
		t.Fatalf("second migrateEncryptData: %v", err)
	}

	// Value should still decrypt to the original.
	var secondEncrypted string
	db.QueryRow(`SELECT title FROM notes WHERE id = 1`).Scan(&secondEncrypted)

	// It might be re-encrypted (double-wrapped) if detection fails, so verify
	// it still decrypts correctly to the original plaintext.
	decrypted, err := encryption.DecryptField(secondEncrypted)
	if err != nil {
		t.Fatalf("decrypt after second migration: %v", err)
	}
	if decrypted != "Test Note" {
		t.Errorf("expected 'Test Note' after idempotent migration, got %q", decrypted)
	}
}

func TestMigrateEncryptDataSkipsEmptyValues(t *testing.T) {
	db := initTestDBWithPlaintext(t)

	// Insert note with empty content.
	_, err := db.Exec(`INSERT INTO notes (id, user_id, title, content, created_at, updated_at)
		VALUES (1, 1, 'Title Only', '', '2025-01-01', '2025-01-01')`)
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	if err := migrateEncryptData(db); err != nil {
		t.Fatalf("migrateEncryptData: %v", err)
	}

	// Empty content should remain empty.
	var content string
	db.QueryRow(`SELECT content FROM notes WHERE id = 1`).Scan(&content)
	if content != "" {
		t.Errorf("expected empty content to remain empty, got %q", content)
	}

	// Non-empty title should be encrypted.
	var title string
	db.QueryRow(`SELECT title FROM notes WHERE id = 1`).Scan(&title)
	if !strings.HasPrefix(title, "enc:") {
		t.Errorf("expected title to be encrypted, got %q", title)
	}
}
