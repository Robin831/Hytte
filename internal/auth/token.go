package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"log"
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
// Authentication precedence: bearer token is tried first; if no Bearer
// header is present, or the token is wrong, the request falls through to
// session-cookie auth. This means a valid session cookie always works even
// when a stray Authorization header is attached.
//
// If HYTTE_UPLOAD_TOKEN is empty or unset, bearer-token auth is disabled
// and only session cookies are accepted.
func RequireAuthOrToken(db *sql.DB) func(http.Handler) http.Handler {
	uploadToken := os.Getenv("HYTTE_UPLOAD_TOKEN")

	rawID := os.Getenv("HYTTE_UPLOAD_USER_ID")
	var tokenUserID int64 = 1
	if rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed <= 0 {
			// Fail closed: a present-but-invalid user ID is a misconfiguration.
			// Disabling token auth is safer than silently authenticating as the
			// wrong user.
			log.Printf("auth: HYTTE_UPLOAD_USER_ID=%q is invalid (%v); disabling bearer-token auth", rawID, err)
			uploadToken = ""
		} else {
			tokenUserID = parsed
		}
	}

	// Pre-compute the SHA-256 hash of the configured token so the per-request
	// comparison operates on fixed-length values. crypto/subtle.ConstantTimeCompare
	// returns immediately on length mismatch, which leaks token length; hashing
	// both sides eliminates that side-channel.
	var uploadTokenHash [32]byte
	if uploadToken != "" {
		uploadTokenHash = sha256.Sum256([]byte(uploadToken))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Attempt bearer token auth first (only when token is configured).
			if uploadToken != "" {
				if authHeader := r.Header.Get("Authorization"); authHeader != "" {
					bearer, found := strings.CutPrefix(authHeader, "Bearer ")
					if found {
						// Constant-time compare of SHA-256 hashes prevents timing
						// attacks even when the request token has a different length.
						bearerHash := sha256.Sum256([]byte(bearer))
						if subtle.ConstantTimeCompare(bearerHash[:], uploadTokenHash[:]) == 1 {
							user, err := GetUserByID(db, tokenUserID)
							if err != nil {
								writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
								return
							}
							ctx := context.WithValue(r.Context(), userContextKey, user)
							next.ServeHTTP(w, r.WithContext(ctx))
							return
						}
						// Bearer header present but token wrong — fall through to
						// session-cookie auth so a valid session still succeeds.
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
