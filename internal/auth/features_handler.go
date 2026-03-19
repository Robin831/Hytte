package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// RequireAdmin is middleware that checks the current user is an admin.
// Returns 403 if not.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || !user.IsAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireFeature returns middleware that checks if the current user has the
// given feature enabled. Admin users bypass the check. Returns 403 if the
// feature is disabled.
func RequireFeature(db *sql.DB, featureKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			// Admins bypass feature checks.
			if user.IsAdmin {
				next.ServeHTTP(w, r)
				return
			}

			features, err := GetUserFeatures(db, user.ID, user.IsAdmin)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check features"})
				return
			}

			if !features[featureKey] {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "feature not enabled"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AdminListUsersHandler returns all users with their feature maps.
func AdminListUsersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := GetAllUsersFeatures(db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load users"})
			return
		}
		writeJSON(w, http.StatusOK, users)
	}
}

// AdminSetFeatureHandler sets a single feature toggle for a user.
func AdminSetFeatureHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		userID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
			return
		}

		var body struct {
			Feature string `json:"feature"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		// Validate feature key.
		if _, ok := FeatureDefaults[body.Feature]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown feature key"})
			return
		}

		// Verify user exists.
		if _, err := GetUserByID(db, userID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}

		if err := SetUserFeature(db, userID, body.Feature, body.Enabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to set feature"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
