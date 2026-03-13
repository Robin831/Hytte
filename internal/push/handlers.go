package push

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
)

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// VAPIDKeyHandler returns the public VAPID key for client-side subscription.
// Only the public key is read from the database — the private key is never loaded.
// GET /api/push/vapid-key
func VAPIDKeyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publicKey, err := GetVAPIDPublicKey(db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get VAPID key"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"public_key": publicKey})
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

		// Validate the endpoint URL: must be https with a non-empty host,
		// and must not target localhost or private IP ranges (SSRF prevention).
		if err := validatePushEndpoint(body.Endpoint); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		// Validate that p256dh and auth are valid base64url and the expected sizes.
		// p256dh: uncompressed P-256 public key = 65 bytes; auth: 16-byte secret.
		p256dhBytes, err := base64.RawURLEncoding.DecodeString(body.Keys.P256dh)
		if err != nil || len(p256dhBytes) != 65 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keys.p256dh must be a valid base64url-encoded 65-byte P-256 public key"})
			return
		}
		authBytes, err := base64.RawURLEncoding.DecodeString(body.Keys.Auth)
		if err != nil || len(authBytes) != 16 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keys.auth must be a valid base64url-encoded 16-byte auth secret"})
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

// validatePushEndpoint checks that the endpoint is a valid https URL with a public host.
// This prevents SSRF attacks where a crafted endpoint could make the server send requests
// to internal services.
func validatePushEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("invalid endpoint URL")
	}
	if u.Scheme != "https" {
		return errors.New("endpoint must use https")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("endpoint must have a host")
	}
	// Reject localhost variants.
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return errors.New("endpoint host is not allowed")
	}
	// Reject private/loopback IP ranges.
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return errors.New("endpoint host is not allowed")
		}
	}
	return nil
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
