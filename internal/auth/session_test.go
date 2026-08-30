package auth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
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
			expires_at DATETIME NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			last_seen_at DATETIME
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE user_features (
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			feature_key TEXT NOT NULL,
			enabled     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, feature_key)
		);
		CREATE TABLE google_tokens (
			user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			access_token  TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			token_type    TEXT NOT NULL DEFAULT 'Bearer',
			expiry        TEXT NOT NULL DEFAULT '',
			scopes        TEXT NOT NULL DEFAULT '',
			updated_at    TEXT NOT NULL DEFAULT ''
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

func TestCreateSessionForRequest_StoresEncryptedMetadata(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-session-metadata")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (TestBrowser)")
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	token, _, err := CreateSessionForRequest(db, userID, req)
	if err != nil {
		t.Fatalf("CreateSessionForRequest: %v", err)
	}

	var storedUA, storedIP string
	if err := db.QueryRow(
		"SELECT user_agent, ip_address FROM sessions WHERE token = ?", hashToken(token),
	).Scan(&storedUA, &storedIP); err != nil {
		t.Fatalf("select session metadata: %v", err)
	}
	if storedUA == "Mozilla/5.0 (TestBrowser)" || storedIP == "203.0.113.7" {
		t.Fatal("session metadata must not be stored in plaintext")
	}

	ua, err := encryption.DecryptField(storedUA)
	if err != nil {
		t.Fatalf("decrypt user agent: %v", err)
	}
	if ua != "Mozilla/5.0 (TestBrowser)" {
		t.Errorf("expected the request user agent, got %q", ua)
	}
	ip, err := encryption.DecryptField(storedIP)
	if err != nil {
		t.Fatalf("decrypt ip: %v", err)
	}
	if ip != "203.0.113.7" {
		t.Errorf("expected the left-most forwarded IP, got %q", ip)
	}
}

func TestCreateSession_LeavesMetadataEmpty(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var ua, ip string
	if err := db.QueryRow(
		"SELECT user_agent, ip_address FROM sessions WHERE token = ?", hashToken(token),
	).Scan(&ua, &ip); err != nil {
		t.Fatalf("select session metadata: %v", err)
	}
	if ua != "" || ip != "" {
		t.Errorf("expected empty metadata, got user_agent=%q ip=%q", ua, ip)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		forwarded  string
		remoteAddr string
		want       string
	}{
		{"forwarded chain", "198.51.100.5, 10.0.0.1", "10.0.0.1:1234", "198.51.100.5"},
		{"single forwarded", "198.51.100.5", "10.0.0.1:1234", "198.51.100.5"},
		{"remote addr fallback", "", "192.0.2.9:4321", "192.0.2.9"},
		{"remote addr without port", "", "192.0.2.9", "192.0.2.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if got := ClientIP(req); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateSessionAndTouch_ThrottlesWrites(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	lastSeen := func() time.Time {
		t.Helper()
		var seen sql.NullTime
		if err := db.QueryRow(
			"SELECT last_seen_at FROM sessions WHERE token = ?", hashToken(token),
		).Scan(&seen); err != nil {
			t.Fatalf("select last_seen_at: %v", err)
		}
		if !seen.Valid {
			t.Fatal("expected last_seen_at to be set")
		}
		return seen.Time
	}

	// A session is stamped on creation, so an immediate request must not write.
	atCreation := lastSeen()
	if _, err := ValidateSessionAndTouch(db, token); err != nil {
		t.Fatalf("ValidateSessionAndTouch: %v", err)
	}
	if got := lastSeen(); !got.Equal(atCreation) {
		t.Errorf("last_seen_at moved inside the throttle window: %v -> %v", atCreation, got)
	}

	// Backdate past the throttle window: the next request should refresh it.
	stale := time.Now().Add(-2 * touchInterval)
	if _, err := db.Exec(
		"UPDATE sessions SET last_seen_at = ? WHERE token = ?", stale, hashToken(token),
	); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
	if _, err := ValidateSessionAndTouch(db, token); err != nil {
		t.Fatalf("ValidateSessionAndTouch after backdating: %v", err)
	}
	if got := lastSeen(); !got.After(stale) {
		t.Errorf("expected last_seen_at to advance past %v, got %v", stale, got)
	}
}

func TestValidateSessionAndTouch_LegacyRowWithoutLastSeen(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// A session created before last-seen tracking existed has a NULL column.
	if _, err := db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		hashToken("legacy-token"), userID, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert legacy session: %v", err)
	}

	gotID, err := ValidateSessionAndTouch(db, "legacy-token")
	if err != nil {
		t.Fatalf("ValidateSessionAndTouch: %v", err)
	}
	if gotID != userID {
		t.Errorf("expected user %d, got %d", userID, gotID)
	}

	var seen sql.NullTime
	if err := db.QueryRow(
		"SELECT last_seen_at FROM sessions WHERE token = ?", hashToken("legacy-token"),
	).Scan(&seen); err != nil {
		t.Fatalf("select last_seen_at: %v", err)
	}
	if !seen.Valid {
		t.Error("expected a legacy session to get a last_seen_at on first use")
	}
}

func TestValidateSessionAndTouch_Invalid(t *testing.T) {
	db := setupTestDB(t)

	if _, err := ValidateSessionAndTouch(db, "nonexistent-token"); err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
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
