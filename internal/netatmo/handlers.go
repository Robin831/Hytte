package netatmo

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
)

// OAuthLoginHandler initiates the Netatmo OAuth2 flow.
// Admin users only — redirects to the Netatmo authorization page.
func OAuthLoginHandler(oauthClient *OAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !oauthClient.IsConfigured() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "netatmo not configured"})
			return
		}

		state, err := GenerateState()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate state"})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "netatmo_state",
			Value:    state,
			Path:     "/",
			MaxAge:   600, // 10 minutes
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   os.Getenv("SECURE_COOKIES") == "true",
		})

		http.Redirect(w, r, oauthClient.AuthorizationURL(state), http.StatusFound)
	}
}

// OAuthCallbackHandler handles the Netatmo OAuth2 callback after the user authorizes.
// Exchanges the authorization code for tokens and saves them for the current admin user.
func OAuthCallbackHandler(oauthClient *OAuthClient, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Validate CSRF state.
		stateCookie, err := r.Cookie("netatmo_state")
		if err != nil || stateCookie.Value == "" {
			http.Redirect(w, r, "/settings?netatmo=error", http.StatusFound)
			return
		}

		// Clear the state cookie regardless of outcome.
		http.SetCookie(w, &http.Cookie{
			Name:   "netatmo_state",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})

		if r.URL.Query().Get("state") != stateCookie.Value {
			http.Redirect(w, r, "/settings?netatmo=error", http.StatusFound)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Redirect(w, r, "/settings?netatmo=error", http.StatusFound)
			return
		}

		token, err := oauthClient.ExchangeCode(r.Context(), code)
		if err != nil {
			log.Printf("netatmo: code exchange failed for user %d: %v", user.ID, err)
			http.Redirect(w, r, "/settings?netatmo=error", http.StatusFound)
			return
		}

		if err := SaveToken(db, user.ID, token); err != nil {
			log.Printf("netatmo: save token for user %d: %v", user.ID, err)
			http.Redirect(w, r, "/settings?netatmo=error", http.StatusFound)
			return
		}

		http.Redirect(w, r, "/settings?netatmo=connected", http.StatusFound)
	}
}

// OAuthStatusHandler returns the Netatmo connection status for the current admin user.
func OAuthStatusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		connected, err := HasToken(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check status"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"connected": connected})
	}
}

// OAuthDisconnectHandler removes the stored Netatmo token for the current admin user.
func OAuthDisconnectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := DeleteToken(db, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to disconnect"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	}
}

// CurrentHandler returns the latest station readings for the authenticated user.
// It fetches from the 5-minute in-memory cache and attempts to persist the
// fresh reading to the historical store before returning (best-effort).
func CurrentHandler(client *Client, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		readings, err := client.GetStationsData(r.Context(), user.ID)
		if err != nil {
			log.Printf("netatmo: fetch station data for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch station data"})
			return
		}

		if err := StoreReadings(db, user.ID, *readings); err != nil {
			log.Printf("netatmo: store readings for user %d: %v", user.ID, err)
		}

		writeJSON(w, http.StatusOK, readings)
	}
}

// HistoryHandler returns historical sensor readings for the authenticated user.
// It accepts an optional "hours" query parameter (default 24, capped at 168).
// A fresh reading is attempted from the API before querying; if the fetch
// fails it is logged and the handler falls back to existing stored data.
func HistoryHandler(client *Client, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		hours := 24
		if h := r.URL.Query().Get("hours"); h != "" {
			if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
				hours = parsed
			}
		}
		if hours > 168 {
			hours = 168
		}

		// Attempt to refresh from the API before querying history (best-effort).
		if readings, err := client.GetStationsData(r.Context(), user.ID); err != nil {
			log.Printf("netatmo: refresh readings for history (user %d): %v", user.ID, err)
		} else if storeErr := StoreReadings(db, user.ID, *readings); storeErr != nil {
			log.Printf("netatmo: store readings for user %d: %v", user.ID, storeErr)
		}

		history, err := QueryHistory(db, user.ID, hours)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query history"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"readings": history})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
