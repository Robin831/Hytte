package push

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
)

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// VAPIDKeyHandler returns the public VAPID key for client-side subscription.
// GET /api/push/vapid-key
func VAPIDKeyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := GetOrCreateVAPIDKeys(db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get VAPID key"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"public_key": keys.PublicKey})
	}
}

// SubscribeHandler saves a push subscription for the authenticated user.
// POST /api/push/subscribe
func SubscribeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		var body struct {
			Endpoint string `json:"endpoint"`
			Keys     struct {
				P256dh string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint, keys.p256dh, and keys.auth are required"})
			return
		}

		sub, err := SaveSubscription(db, user.ID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save subscription"})
			return
		}

		writeJSON(w, http.StatusCreated, sub)
	}
}

// UnsubscribeHandler removes a push subscription for the authenticated user.
// DELETE /api/push/subscribe
func UnsubscribeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		var body struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
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
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete subscription"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
	}
}

// SubscriptionsListHandler returns all push subscriptions for the authenticated user.
// GET /api/push/subscriptions
func SubscriptionsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		subs, err := GetSubscriptionsByUser(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list subscriptions"})
			return
		}

		if subs == nil {
			subs = []Subscription{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
	}
}
