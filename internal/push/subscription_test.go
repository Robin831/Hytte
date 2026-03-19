package push

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Limit to one connection so all queries share the same in-memory database.
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE vapid_keys (
			id INTEGER PRIMARY KEY,
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL
		);
		CREATE TABLE push_subscriptions (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			endpoint   TEXT NOT NULL,
			p256dh     TEXT NOT NULL,
			auth       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, endpoint)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

func TestSaveSubscription(t *testing.T) {
	db := setupTestDB(t)

	sub, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "p256dh_key", "auth_key")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if sub.Endpoint != "https://push.example.com/sub1" {
		t.Errorf("endpoint = %q, want %q", sub.Endpoint, "https://push.example.com/sub1")
	}
	if sub.P256dh != "p256dh_key" {
		t.Errorf("p256dh = %q, want %q", sub.P256dh, "p256dh_key")
	}
	if sub.UserID != 1 {
		t.Errorf("user_id = %d, want 1", sub.UserID)
	}
}

func TestSaveSubscription_Upsert(t *testing.T) {
	db := setupTestDB(t)

	_, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "old_p256dh", "old_auth")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	sub, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "new_p256dh", "new_auth")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if sub.P256dh != "new_p256dh" {
		t.Errorf("p256dh = %q, want %q", sub.P256dh, "new_p256dh")
	}
	if sub.Auth != "new_auth" {
		t.Errorf("auth = %q, want %q", sub.Auth, "new_auth")
	}

	// Should still be just one subscription.
	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("got %d subscriptions, want 1", len(subs))
	}
}

func TestDeleteSubscription(t *testing.T) {
	db := setupTestDB(t)

	_, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "p256dh", "auth")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	err = DeleteSubscription(db, 1, "https://push.example.com/sub1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("got %d subscriptions after delete, want 0", len(subs))
	}
}

func TestDeleteSubscription_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteSubscription(db, 1, "https://push.example.com/nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestGetSubscriptionsByUser(t *testing.T) {
	db := setupTestDB(t)

	_, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "key1", "auth1")
	if err != nil {
		t.Fatalf("save sub1: %v", err)
	}
	_, err = SaveSubscription(db, 1, "https://push.example.com/sub2", "key2", "auth2")
	if err != nil {
		t.Fatalf("save sub2: %v", err)
	}

	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("got %d subscriptions, want 2", len(subs))
	}
}

func TestGetSubscriptionsByUser_Empty(t *testing.T) {
	db := setupTestDB(t)

	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if subs != nil {
		t.Errorf("expected nil for empty result, got %v", subs)
	}
}

func TestDeleteSubscriptionByID(t *testing.T) {
	db := setupTestDB(t)

	sub, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "key", "auth")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	err = DeleteSubscriptionByID(db, 1, sub.ID)
	if err != nil {
		t.Fatalf("delete by id: %v", err)
	}

	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("got %d subscriptions after delete, want 0", len(subs))
	}
}

func TestDeleteSubscriptionByID_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteSubscriptionByID(db, 1, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestDeleteSubscriptionByID_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	// Create a second user.
	_, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')")
	if err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	sub, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "key", "auth")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// User 2 should not be able to delete user 1's subscription.
	err = DeleteSubscriptionByID(db, 2, sub.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}

	// Verify the subscription still exists.
	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("got %d subscriptions, want 1 (should not have been deleted)", len(subs))
	}
}

func TestCascadeDeleteUser(t *testing.T) {
	db := setupTestDB(t)

	_, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "key", "auth")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err = db.Exec("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	subs, err := GetSubscriptionsByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions after user delete, got %d", len(subs))
	}
}
