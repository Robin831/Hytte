package budget

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("budget: writeJSON encode error: %v", err)
	}
}

// CategoriesListHandler seeds the default categories on first access (idempotent)
// and returns the user's full category list.
func CategoriesListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if err := SeedDefaultCategories(db, user.ID); err != nil {
			log.Printf("budget: seed categories for user %d: %v", user.ID, err)
			// Continue — partial seed is better than a hard failure.
		}

		cats, err := ListCategories(db, user.ID)
		if err != nil {
			log.Printf("budget: list categories for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list categories"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
	}
}
