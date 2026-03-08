package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const sessionDuration = 30 * 24 * time.Hour // 30 days

// CreateSession generates a random session token, stores it in the database,
// and returns the token.
func CreateSession(db *sql.DB, userID int64) (string, time.Time, error) {
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}

	expiresAt := time.Now().Add(sessionDuration)

	_, err = db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, expiresAt,
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}

	return token, expiresAt, nil
}

// ValidateSession looks up a session token and returns the associated user ID.
// Returns sql.ErrNoRows if the session is invalid or expired.
func ValidateSession(db *sql.DB, token string) (int64, error) {
	var userID int64
	err := db.QueryRow(
		"SELECT user_id FROM sessions WHERE token = ? AND expires_at > ?",
		token, time.Now(),
	).Scan(&userID)
	return userID, err
}

// DeleteSession removes a session from the database.
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
