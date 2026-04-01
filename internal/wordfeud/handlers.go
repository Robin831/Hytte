package wordfeud

import (
	"database/sql"
	"encoding/json"
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
		log.Printf("Warning: failed to decrypt wordfeud_session_token: %v", err)
		return raw, nil
	}
	return token, nil
}

// GamesHandler returns the list of active Wordfeud games.
// GET /api/wordfeud/games
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

		games, err := client.GetGames(token)
		if err != nil {
			if isSessionError(err) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired — please re-authenticate in Settings"})
				return
			}
			log.Printf("Wordfeud API error (games): %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch games from Wordfeud"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"games": games})
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

		gs, err := GetGameCached(client, cache, token, gameID)
		if err != nil {
			if isSessionError(err) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired — please re-authenticate in Settings"})
				return
			}
			if isNotFoundError(err) {
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

// LoginHandler authenticates with Wordfeud and stores the session token.
// POST /api/wordfeud/login
func LoginHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

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
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud login failed — check your email and password"})
			return
		}

		encrypted, err := encryption.EncryptField(sessionToken)
		if err != nil {
			log.Printf("Failed to encrypt wordfeud session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save session token"})
			return
		}

		if err := auth.SetPreference(db, user.ID, "wordfeud_session_token", encrypted); err != nil {
			log.Printf("Failed to save wordfeud session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save session token"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func isSessionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "session expired") || strings.Contains(msg, "session invalid")
}

func isNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "not found")
}
