package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// hashToken returns the SHA-256 hex digest of a session token.
// We store the hash in the DB so raw tokens aren't exposed if the DB leaks.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

const sessionDuration = 30 * 24 * time.Hour // 30 days

// SessionMeta describes the sign-in that created a session. It is stored
// alongside the session so a user can tell their sessions apart in the settings
// page and in the data export. Both fields are encrypted at rest.
type SessionMeta struct {
	UserAgent string
	IPAddress string
}

// CreateSession generates a random session token, stores it in the database,
// and returns the token. No sign-in metadata is recorded.
func CreateSession(db *sql.DB, userID int64) (string, time.Time, error) {
	return CreateSessionWithMeta(db, userID, SessionMeta{})
}

// CreateSessionForRequest creates a session and records the user agent and
// client IP of the request that signed in.
func CreateSessionForRequest(db *sql.DB, userID int64, r *http.Request) (string, time.Time, error) {
	return CreateSessionWithMeta(db, userID, SessionMeta{
		UserAgent: r.UserAgent(),
		IPAddress: ClientIP(r),
	})
}

// CreateSessionWithMeta generates a random session token, stores it together
// with the given sign-in metadata, and returns the token.
func CreateSessionWithMeta(db *sql.DB, userID int64, meta SessionMeta) (string, time.Time, error) {
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}

	expiresAt := time.Now().Add(sessionDuration)

	_, err = db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at, user_agent, ip_address) VALUES (?, ?, ?, ?, ?)",
		hashToken(token), userID, expiresAt,
		encryptSessionMeta("user_agent", meta.UserAgent),
		encryptSessionMeta("ip_address", meta.IPAddress),
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}

	return token, expiresAt, nil
}

// encryptSessionMeta encrypts a session metadata field, falling back to an
// empty value if encryption fails — metadata is a convenience, never a reason
// to block a sign-in.
func encryptSessionMeta(field, value string) string {
	if value == "" {
		return ""
	}
	enc, err := encryption.EncryptField(value)
	if err != nil {
		log.Printf("Warning: failed to encrypt session %s, storing empty: %v", field, err)
		return ""
	}
	return enc
}

// ClientIP returns the originating IP of a request. Hytte runs behind Caddy, so
// the left-most X-Forwarded-For entry is preferred and RemoteAddr is the
// fallback.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, found := strings.Cut(fwd, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ValidateSession looks up a session token and returns the associated user ID.
// Returns sql.ErrNoRows if the session is invalid or expired.
func ValidateSession(db *sql.DB, token string) (int64, error) {
	var userID int64
	err := db.QueryRow(
		"SELECT user_id FROM sessions WHERE token = ? AND expires_at > ?",
		hashToken(token), time.Now(),
	).Scan(&userID)
	return userID, err
}

// DeleteSession removes a session from the database.
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token = ?", hashToken(token))
	return err
}

// CleanExpiredSessions removes all sessions that have passed their expiry time.
func CleanExpiredSessions(db *sql.DB) (int64, error) {
	result, err := db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
