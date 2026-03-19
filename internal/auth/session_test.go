package auth

import (
	"database/sql"
	"testing"
	"time"

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	_, err := db.Exec(
		"INSERT INTO users (google_id, email, name, is_admin) VALUES ('g123', 'test@example.com', 'Test', 0)",
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	err = db.QueryRow("SELECT id FROM users WHERE google_id = 'g123'").Scan(&id)
	if err != nil {
		t.Fatalf("select user: %v", err)
	}
	return id
}

func createTestAdminUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	_, err := db.Exec(
		"INSERT INTO users (google_id, email, name, is_admin) VALUES ('gadmin', 'admin@example.com', 'Admin', 1)",
	)
	if err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	err = db.QueryRow("SELECT id FROM users WHERE google_id = 'gadmin'").Scan(&id)
	if err != nil {
		t.Fatalf("select admin user: %v", err)
	}
	return id
}

func TestCreateAndValidateSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	token, expiresAt, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64-char hex token, got %d chars", len(token))
	}
	if time.Until(expiresAt) < 29*24*time.Hour {
		t.Errorf("expiry too soon: %v", expiresAt)
	}

	gotID, err := ValidateSession(db, token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if gotID != userID {
		t.Errorf("expected user %d, got %d", userID, gotID)
	}
}

func TestValidateSession_Invalid(t *testing.T) {
	db := setupTestDB(t)

	_, err := ValidateSession(db, "nonexistent-token")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := DeleteSession(db, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, err = ValidateSession(db, token)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestCleanExpiredSessions(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Insert an already-expired session directly.
	_, err := db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES ('expired-tok', ?, ?)",
		userID, time.Now().Add(-1*time.Hour),
	)
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	// Also create a valid session.
	validToken, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	n, err := CleanExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 cleaned, got %d", n)
	}

	// Valid session should still work.
	if _, err := ValidateSession(db, validToken); err != nil {
		t.Errorf("valid session should still exist: %v", err)
	}
}
