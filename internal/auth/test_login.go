package auth

import (
	"database/sql"
	"log"
	"net/http"
	"time"
)

// TestLoginHandler creates a test admin user and session for automated testing.
// Only registered when HYTTE_TEST_AUTH=1 — NEVER in production.
func TestLoginHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upsert a test user (admin).
		user, err := UpsertUser(db, "test-google-id", "test@hytte.local", "Test Admin", "")
		if err != nil {
			log.Printf("test-login: failed to upsert user: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create test user"})
			return
		}

		// Ensure admin flag is set.
		if _, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE id = ?`, user.ID); err != nil {
			log.Printf("test-login: failed to set admin: %v", err)
		}

		// Create session.
		token, expiresAt, err := CreateSession(db, user.ID)
		if err != nil {
			log.Printf("test-login: failed to create session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		log.Printf("test-login: created session for test user %d (%s)", user.ID, user.Email)
		writeJSON(w, http.StatusOK, map[string]any{
			"user":       user,
			"expires_at": expiresAt.Format(time.RFC3339),
		})
	}
}
