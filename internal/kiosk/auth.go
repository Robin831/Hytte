package kiosk

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type contextKey string

const kioskConfigKey contextKey = "kiosk_config"

// KioskConfig holds the parsed JSON configuration for a kiosk token.
type KioskConfig map[string]any

// hashToken returns the SHA-256 hex digest of a kiosk token.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// KioskAuth is middleware that authenticates requests using a ?token= query parameter.
// It looks up the token hash in kiosk_tokens, validates expiry, updates last_used_at,
// and injects the parsed config JSON into the request context.
func KioskAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.URL.Query().Get("token")
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			hash := hashToken(token)

			var (
				id        int64
				configRaw string
				expiresAt sql.NullString
			)
			err := db.QueryRowContext(r.Context(),
				"SELECT id, config, expires_at FROM kiosk_tokens WHERE token_hash = ?",
				hash,
			).Scan(&id, &configRaw, &expiresAt)
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}

			// Check expiry if set.
			if expiresAt.Valid && expiresAt.String != "" {
				exp, parseErr := time.Parse(time.RFC3339, expiresAt.String)
				if parseErr != nil {
					// Try common SQLite datetime format as fallback.
					exp, parseErr = time.Parse("2006-01-02 15:04:05", expiresAt.String)
				}
				if parseErr != nil {
					// Unparseable expiry — deny access rather than silently allowing.
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
				if time.Now().After(exp) {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token expired"})
					return
				}
			}

			// Mitigate token leakage via caches and Referer headers.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")

			// Update last_used_at; log errors so operational issues are visible.
			_, err = db.ExecContext(r.Context(),
				"UPDATE kiosk_tokens SET last_used_at = ? WHERE id = ?",
				time.Now().UTC().Format(time.RFC3339),
				id,
			)
			if err != nil {
				log.Printf("kiosk: failed to update last_used_at for token id %d: %v", id, err)
			}

			// Parse config JSON into KioskConfig.
			var cfg KioskConfig
			if configRaw != "" {
				if err := json.Unmarshal([]byte(configRaw), &cfg); err != nil {
					// Invalid config JSON is a server-side data error; surface it instead of silently defaulting.
					log.Printf("kiosk: invalid config JSON for token id %d: %v", id, err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid kiosk token config"})
					return
				}
			}
			if cfg == nil {
				cfg = KioskConfig{}
			}

			ctx := context.WithValue(r.Context(), kioskConfigKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetKioskConfig retrieves the KioskConfig injected by KioskAuth middleware.
// Returns nil if not present.
func GetKioskConfig(ctx context.Context) KioskConfig {
	cfg, _ := ctx.Value(kioskConfigKey).(KioskConfig)
	return cfg
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
