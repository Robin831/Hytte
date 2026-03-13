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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE push_subscriptions (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			endpoint   TEXT UNIQUE NOT NULL,
			p256dh     TEXT NOT NULL,
			auth       TEXT NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123');
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSaveSubscription(t *testing.T) {
	db := setupTestDB(t)

	sub, err := SaveSubscription(db, 1, "https://push.example.com/1", "p256dh-key", "auth-key", "TestBrowser/1.0")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if sub.Endpoint != "https://push.example.com/1" {
		t.Errorf("endpoint = %q, want %q", sub.Endpoint, "https://push.example.com/1")
	}
	if sub.P256dh != "p256dh-key" {
		t.Errorf("p256dh = %q, want %q", sub.P256dh, "p256dh-key")
	}
	if sub.UserID != 1 {
		t.Errorf("user_id = %d, want 1", sub.UserID)
	}
}

func TestSaveSubscription_UpsertOnConflict(t *testing.T) {
	db := setupTestDB(t)

	_, err := SaveSubscription(db, 1, "https://push.example.com/1", "old-key", "old-auth", "Browser/1.0")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	sub, err := SaveSubscription(db, 1, "https://push.example.com/1", "new-key", "new-auth", "Browser/2.0")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if sub.P256dh != "new-key" {
		t.Errorf("p256dh = %q, want %q", sub.P256dh, "new-key")
	}
	if sub.Auth != "new-auth" {
		t.Errorf("auth = %q, want %q", sub.Auth, "new-auth")
	}
}

func TestGetSubscriptions(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/1", "k1", "a1", "Browser")
	_, _ = SaveSubscription(db, 1, "https://push.example.com/2", "k2", "a2", "Browser")

	subs, err := GetSubscriptions(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("got %d subscriptions, want 2", len(subs))
	}
}

func TestGetSubscriptions_Empty(t *testing.T) {
	db := setupTestDB(t)

	subs, err := GetSubscriptions(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if subs != nil {
		t.Errorf("expected nil slice for no subscriptions, got %v", subs)
	}
}

func TestSaveSubscription_OwnershipTransfer(t *testing.T) {
	db := setupTestDB(t)

	// Add a second user
	_, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// User 1 subscribes
	_, err = SaveSubscription(db, 1, "https://push.example.com/shared", "k1", "a1", "Browser")
	if err != nil {
		t.Fatalf("save user1: %v", err)
	}

	// User 2 subscribes with same endpoint (same browser, different login)
	sub, err := SaveSubscription(db, 2, "https://push.example.com/shared", "k2", "a2", "Browser")
	if err != nil {
		t.Fatalf("save user2: %v", err)
	}

	if sub.UserID != 2 {
		t.Errorf("user_id = %d, want 2 (ownership should transfer)", sub.UserID)
	}

	// User 1 should no longer have this subscription
	subs, _ := GetSubscriptions(db, 1)
	if len(subs) != 0 {
		t.Errorf("user1 should have 0 subscriptions, got %d", len(subs))
	}

	// User 2 should own it
	subs, _ = GetSubscriptions(db, 2)
	if len(subs) != 1 {
		t.Errorf("user2 should have 1 subscription, got %d", len(subs))
	}
}

func TestDeleteSubscription(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/1", "k1", "a1", "Browser")

	err := DeleteSubscription(db, 1, "https://push.example.com/1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	subs, _ := GetSubscriptions(db, 1)
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions after delete, got %d", len(subs))
	}
}

func TestDeleteSubscription_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteSubscription(db, 1, "https://push.example.com/nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteSubscriptionByEndpoint(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/expired", "k", "a", "Browser")

	err := DeleteSubscriptionByEndpoint(db, "https://push.example.com/expired")
	if err != nil {
		t.Fatalf("delete by endpoint: %v", err)
	}

	subs, _ := GetSubscriptions(db, 1)
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(subs))
	}
}

func TestCascadeDelete(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/1", "k", "a", "Browser")

	_, err := db.Exec("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	subs, _ := GetSubscriptions(db, 1)
	if len(subs) != 0 {
		t.Errorf("expected cascade to delete subscriptions, got %d", len(subs))
	}
}
