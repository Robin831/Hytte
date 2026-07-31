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

// touchInterval is the minimum gap between last_seen_at writes for a session.
// Every authenticated request would otherwise write to the sessions table.
const touchInterval = 5 * time.Minute

// maxUserAgentLength caps how much of a User-Agent header we keep. Real agents
// are well under this; the cap keeps a hostile client from bloating the row.
const maxUserAgentLength = 512

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
	ua := r.UserAgent()
	if len(ua) > maxUserAgentLength {
		ua = ua[:maxUserAgentLength]
	}
	return CreateSessionWithMeta(db, userID, SessionMeta{
		UserAgent: ua,
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

	now := time.Now()
	expiresAt := now.Add(sessionDuration)

	_, err = db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at, user_agent, ip_address, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)",
		hashToken(token), userID, expiresAt,
		encryptSessionMeta("user_agent", meta.UserAgent),
		encryptSessionMeta("ip_address", meta.IPAddress),
		now,
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
	userID, _, err := validateSession(db, token)
	return userID, err
}

// ValidateSessionAndTouch validates a session and, at most once every
// touchInterval, records that the session was just used. A failed touch is
// logged but never fails the request — last-seen is informational only.
func ValidateSessionAndTouch(db *sql.DB, token string) (int64, error) {
	userID, lastSeen, err := validateSession(db, token)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if now.Sub(lastSeen) > touchInterval {
		if err := TouchSession(db, token, now); err != nil {
			log.Printf("Warning: failed to update session last_seen_at: %v", err)
		}
	}
	return userID, nil
}

// validateSession looks up a session and returns the user ID together with the
// last time the session was seen. Sessions created before last-seen tracking
// existed report the zero time.
func validateSession(db *sql.DB, token string) (int64, time.Time, error) {
	var userID int64
	var lastSeen sql.NullTime
	err := db.QueryRow(
		"SELECT user_id, last_seen_at FROM sessions WHERE token = ? AND expires_at > ?",
		hashToken(token), time.Now(),
	).Scan(&userID, &lastSeen)
	if err != nil {
		return 0, time.Time{}, err
	}
	return userID, lastSeen.Time, nil
}

// TouchSession records that a session was used at the given time.
func TouchSession(db *sql.DB, token string, at time.Time) error {
	_, err := db.Exec(
		"UPDATE sessions SET last_seen_at = ? WHERE token = ?",
		at, hashToken(token),
	)
	return err
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
