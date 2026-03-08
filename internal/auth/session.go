package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

const (
	sessionCookieName = "hytte_session"
	sessionDuration   = 30 * 24 * time.Hour // 30 days
)

// User represents an authenticated user.
type User struct {
	ID        int64  `json:"id"`
	GoogleID  string `json:"-"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

// CreateSession generates a new session token, stores it in the database,
// and sets it as an HTTP-only cookie.
func CreateSession(db *sql.DB, w http.ResponseWriter, userID int64, secure bool) error {
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	expiresAt := time.Now().Add(sessionDuration)

	_, err = db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	return nil
}

// DeleteSession removes the session from the database and clears the cookie.
func DeleteSession(db *sql.DB, w http.ResponseWriter, r *http.Request, secure bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_, _ = db.Exec("DELETE FROM sessions WHERE token = ?", cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// GetSessionUser retrieves the user associated with the session cookie.
// Returns nil if there is no valid session.
func GetSessionUser(db *sql.DB, r *http.Request) *User {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}

	var u User
	err = db.QueryRow(`
		SELECT u.id, u.google_id, u.email, u.name, u.avatar_url
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > datetime('now')
	`, cookie.Value).Scan(&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.AvatarURL)
	if err != nil {
		return nil
	}

	return &u
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
