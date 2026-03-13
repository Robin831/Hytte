package push

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/Robin831/Hytte/internal/auth"
)

// VAPIDPublicKeyHandler returns the VAPID public key so the frontend can
// subscribe to push notifications.
func VAPIDPublicKeyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := os.Getenv("VAPID_PUBLIC_KEY")
		if key == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "push not configured"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"public_key": key})
	}
}

// SubscribeHandler stores a push subscription for the authenticated user.
func SubscribeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Endpoint string `json:"endpoint"`
			Keys     struct {
				P256dh string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint, keys.p256dh, and keys.auth are required"})
			return
		}

		userAgent := r.UserAgent()
		sub, err := SaveSubscription(db, user.ID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth, userAgent)
		if err != nil {
			log.Printf("Failed to save push subscription: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save subscription"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"subscription": sub})
	}
}

// UnsubscribeHandler removes a push subscription for the authenticated user.
func UnsubscribeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if body.Endpoint == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint is required"})
			return
		}

		err := DeleteSubscription(db, user.ID, body.Endpoint)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
			return
		}
		if err != nil {
			log.Printf("Failed to delete push subscription: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete subscription"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ListSubscriptionsHandler returns all push subscriptions for the authenticated user.
func ListSubscriptionsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		subs, err := GetSubscriptions(db, user.ID)
		if err != nil {
			log.Printf("Failed to list push subscriptions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list subscriptions"})
			return
		}
		if subs == nil {
			subs = []Subscription{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
