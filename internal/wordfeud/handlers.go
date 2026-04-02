package wordfeud

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// getSessionToken reads the user's stored Wordfeud session token from preferences.
func getSessionToken(db *sql.DB, userID int64) (string, error) {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return "", err
	}
	raw, ok := prefs["wordfeud_session_token"]
	if !ok || raw == "" {
		return "", nil
	}
	token, err := encryption.DecryptField(raw)
	if err != nil {
		return "", fmt.Errorf("wordfeud: session token is corrupted or encryption key has changed — please re-authenticate in Settings")
	}
	return token, nil
}

// GamesHandler returns the active and finished Wordfeud games.
// GET /api/wordfeud/games
// Response: {"games": [...active games], "finished_games": [...finished games]}
func GamesHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		token, err := getSessionToken(db, user.ID)
		if err != nil {
			log.Printf("Failed to read wordfeud preferences: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read preferences"})
			return
		}
		if token == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no Wordfeud session token configured — add it in Settings"})
			return
		}

		result, err := client.GetGames(token)
		if err != nil {
			if errors.Is(err, ErrSessionExpired) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired — please re-authenticate in Settings"})
				return
			}
			log.Printf("Wordfeud API error (games): %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch games from Wordfeud"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"games": result.Active, "finished_games": result.Finished})
	}
}

// GameHandler returns the full state for a single Wordfeud game.
// GET /api/wordfeud/games/{id}
func GameHandler(db *sql.DB, client *Client, cache *GameCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		idStr := chi.URLParam(r, "id")
		gameID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		token, err := getSessionToken(db, user.ID)
		if err != nil {
			log.Printf("Failed to read wordfeud preferences: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read preferences"})
			return
		}
		if token == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no Wordfeud session token configured — add it in Settings"})
			return
		}

		gs, err := GetGameCached(client, cache, token, user.ID, gameID)
		if err != nil {
			if errors.Is(err, ErrSessionExpired) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired — please re-authenticate in Settings"})
				return
			}
			if errors.Is(err, ErrGameNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
				return
			}
			log.Printf("Wordfeud API error (game %d): %v", gameID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch game from Wordfeud"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"game": gs})
	}
}

// LoginHandler authenticates with Wordfeud and stores the encrypted credentials.
// POST /api/wordfeud/login
func LoginHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return loginAndStore(db, client)
}

// ConnectHandler tests Wordfeud credentials via Login and stores the encrypted
// email, password, and session token on success. This is the admin-only settings endpoint.
// POST /api/wordfeud/connect
func ConnectHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return loginAndStore(db, client)
}

// loginAndStore is the shared implementation for LoginHandler and ConnectHandler.
// It validates credentials against the Wordfeud API, then stores the encrypted
// email, password, and session token in user preferences.
func loginAndStore(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Limit request body to 1 KiB to prevent abuse.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if body.Email == "" || body.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
			return
		}

		sessionToken, err := client.Login(body.Email, body.Password)
		if err != nil {
			log.Printf("Wordfeud login failed for user %d: %v", user.ID, err)
			if errors.Is(err, ErrInvalidCredentials) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud login failed — check your email and password"})
			} else {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Wordfeud login failed — upstream service unavailable"})
			}
			return
		}

		// Encrypt all three fields: email, password, and session token.
		encEmail, err := encryption.EncryptField(body.Email)
		if err != nil {
			log.Printf("Failed to encrypt wordfeud email: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save credentials"})
			return
		}
		encPassword, err := encryption.EncryptField(body.Password)
		if err != nil {
			log.Printf("Failed to encrypt wordfeud password: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save credentials"})
			return
		}
		encToken, err := encryption.EncryptField(sessionToken)
		if err != nil {
			log.Printf("Failed to encrypt wordfeud session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save credentials"})
			return
		}

		// Store all credentials with best-effort all-or-nothing semantics.
		creds := []struct{ k, v string }{
			{"wordfeud_email", encEmail},
			{"wordfeud_password", encPassword},
			{"wordfeud_session_token", encToken},
		}
		for i, kv := range creds {
			if err := auth.SetPreference(db, user.ID, kv.k, kv.v); err != nil {
				log.Printf("Failed to save %s: %v", kv.k, err)
				// Best-effort rollback: clear any credentials written earlier in this request.
				for j := 0; j < i; j++ {
					if rbErr := auth.SetPreference(db, user.ID, creds[j].k, ""); rbErr != nil {
						log.Printf("Failed to rollback %s after error: %v", creds[j].k, rbErr)
					}
				}
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save credentials"})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

// DisconnectHandler removes all stored Wordfeud credentials (email, password, session token).
// DELETE /api/wordfeud/disconnect
func DisconnectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		_, err := db.Exec(
			"DELETE FROM user_preferences WHERE user_id = ? AND key IN (?, ?, ?)",
			user.ID, "wordfeud_email", "wordfeud_password", "wordfeud_session_token",
		)
		if err != nil {
			log.Printf("Failed to delete wordfeud credentials for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to disconnect"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	}
}

// maskEmail returns a partially-redacted version of an email address,
// e.g. "player@example.com" → "pl***@example.com".
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	domain := email[at:]
	show := 2
	if len(local) <= 2 {
		show = 1
	}
	return local[:show] + "***" + domain
}

// StatusHandler returns whether Wordfeud credentials are configured and the
// connected email (masked).
// GET /api/wordfeud/status
func StatusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		prefs, err := auth.GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("Failed to read wordfeud status for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusOK, map[string]any{"connected": false})
			return
		}

		rawEmail := prefs["wordfeud_email"]
		rawToken := prefs["wordfeud_session_token"]

		// The session token is the primary indicator of a connected account.
		connected := rawToken != ""
		resp := map[string]any{"connected": connected}

		if connected && rawEmail != "" {
			email, err := encryption.DecryptField(rawEmail)
			if err != nil {
				log.Printf("Failed to decrypt wordfeud email for user %d: %v", user.ID, err)
			} else {
				resp["email"] = maskEmail(email)
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
