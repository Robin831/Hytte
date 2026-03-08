package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// GoogleLoginHandler redirects the user to Google's OAuth consent screen.
func GoogleLoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := Config()
		if cfg.ClientID == "" {
			http.Error(w, "OAuth not configured", http.StatusServiceUnavailable)
			return
		}

		state, err := generateState()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Path:     "/",
			MaxAge:   600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure(),
		})

		url := cfg.AuthCodeURL(state)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

// GoogleCallbackHandler handles the OAuth callback from Google.
func GoogleCallbackHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify state parameter.
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		// Clear the state cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})

		// Check for errors from Google.
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Redirect(w, r, "/?error="+errParam, http.StatusTemporaryRedirect)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}

		cfg := Config()
		token, err := cfg.Exchange(r.Context(), code)
		if err != nil {
			log.Printf("OAuth exchange error: %v", err)
			http.Error(w, "failed to exchange token", http.StatusInternalServerError)
			return
		}

		// Fetch user info from Google.
		client := cfg.Client(r.Context(), token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			log.Printf("Failed to fetch user info: %v", err)
			http.Error(w, "failed to get user info", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		var userInfo struct {
			ID      string `json:"id"`
			Email   string `json:"email"`
			Name    string `json:"name"`
			Picture string `json:"picture"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
			log.Printf("Failed to decode user info: %v", err)
			http.Error(w, "failed to parse user info", http.StatusInternalServerError)
			return
		}

		// Upsert user in database.
		user, err := UpsertUser(db, userInfo.ID, userInfo.Email, userInfo.Name, userInfo.Picture)
		if err != nil {
			log.Printf("Failed to upsert user: %v", err)
			http.Error(w, "failed to save user", http.StatusInternalServerError)
			return
		}

		// Create session.
		sessionToken, expiresAt, err := CreateSession(db, user.ID)
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    sessionToken,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure(),
		})

		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	}
}

// MeHandler returns the currently authenticated user's info.
// Expects OptionalAuth middleware to have run, populating user context if valid.
func MeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusOK, map[string]any{"user": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}

// LogoutHandler clears the session.
func LogoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err == nil {
			DeleteSession(db, cookie.Value)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure(),
		})

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func isSecure() bool {
	return os.Getenv("SECURE_COOKIES") == "true"
}
