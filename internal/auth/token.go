package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// RequireAuthOrToken is like RequireAuth but also accepts an
// Authorization: Bearer <token> header matching HYTTE_UPLOAD_TOKEN.
// When the token matches, the user identified by HYTTE_UPLOAD_USER_ID
// (default "1") is loaded from the DB and injected into the context.
// This lets automated tools (e.g. the local fit-file monitor) upload
// without a browser session.
//
// If HYTTE_UPLOAD_TOKEN is empty or unset, bearer-token auth is disabled
// and only session cookies are accepted.
func RequireAuthOrToken(db *sql.DB) func(http.Handler) http.Handler {
	uploadToken := os.Getenv("HYTTE_UPLOAD_TOKEN")

	rawID := os.Getenv("HYTTE_UPLOAD_USER_ID")
	var tokenUserID int64 = 1
	if rawID != "" {
		if parsed, err := strconv.ParseInt(rawID, 10, 64); err == nil && parsed > 0 {
			tokenUserID = parsed
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Attempt bearer token auth first (only when token is configured).
			if uploadToken != "" {
				if authHeader := r.Header.Get("Authorization"); authHeader != "" {
					bearer, found := strings.CutPrefix(authHeader, "Bearer ")
					if found {
						// Constant-time compare to prevent timing attacks.
						if subtle.ConstantTimeCompare([]byte(bearer), []byte(uploadToken)) == 1 {
							user, err := GetUserByID(db, tokenUserID)
							if err != nil {
								writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
								return
							}
							ctx := context.WithValue(r.Context(), userContextKey, user)
							next.ServeHTTP(w, r.WithContext(ctx))
							return
						}
						// Bearer header present but token wrong — reject immediately.
						writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
						return
					}
				}
			}

			// Fall through to session-cookie auth.
			cookie, err := r.Cookie("session")
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			userID, err := ValidateSession(db, cookie.Value)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			user, err := GetUserByID(db, userID)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
