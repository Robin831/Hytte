package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"golang.org/x/oauth2"
)

const googleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// googleUserInfo is the response from Google's userinfo endpoint.
type googleUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// LoginHandler redirects the user to Google's OAuth2 consent screen.
func LoginHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := generateState()
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Path:     "/api/auth/google/callback",
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   300,
		})

		url := cfg.OAuth2.AuthCodeURL(state, oauth2.AccessTypeOffline)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

// CallbackHandler handles the OAuth2 callback from Google.
func CallbackHandler(cfg *Config, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

		// Clear the state cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    "",
			Path:     "/api/auth/google/callback",
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		token, err := cfg.OAuth2.Exchange(r.Context(), code)
		if err != nil {
			log.Printf("OAuth exchange error: %v", err)
			http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
			return
		}

		userInfo, err := fetchGoogleUserInfo(r.Context(), cfg.OAuth2, token)
		if err != nil {
			log.Printf("Failed to fetch user info: %v", err)
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
			return
		}

		userID, err := upsertUser(db, userInfo)
		if err != nil {
			log.Printf("Failed to upsert user: %v", err)
			http.Error(w, "Failed to save user", http.StatusInternalServerError)
			return
		}

		if err := CreateSession(db, w, userID, cfg.CookieSecure); err != nil {
			log.Printf("Failed to create session: %v", err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, cfg.BaseURL+"/", http.StatusTemporaryRedirect)
	}
}

// LogoutHandler clears the session and returns a JSON response.
func LogoutHandler(cfg *Config, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		DeleteSession(db, w, r, cfg.CookieSecure)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// MeHandler returns the currently authenticated user or 401.
func MeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetSessionUser(db, r)
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "not authenticated",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(user)
	}
}

func fetchGoogleUserInfo(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*googleUserInfo, error) {
	client := cfg.Client(ctx, token)
	resp, err := client.Get(googleUserInfoURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info googleUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func upsertUser(db *sql.DB, info *googleUserInfo) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO users (google_id, email, name, avatar_url)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(google_id) DO UPDATE SET
			email = excluded.email,
			name = excluded.name,
			avatar_url = excluded.avatar_url
	`, info.ID, info.Email, info.Name, info.Picture)
	if err != nil {
		return 0, err
	}

	var id int64
	err = db.QueryRow("SELECT id FROM users WHERE google_id = ?", info.ID).Scan(&id)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
