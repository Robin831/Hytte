package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// OverallRunTimeout caps the entire RunHandler invocation. Five pages × 90s
// per-page worst case fits under 10 minutes, but Claude can occasionally
// stall — we want to return a result rather than hang the request indefinitely.
const OverallRunTimeout = 10 * time.Minute

// RunHandler triggers a synchronous suggestions-generation pass for all enabled
// pages in the registry. Admin-only.
//
// POST /api/suggestions/run
// Response: { "generated": int, "errors": int }
func RunHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("suggestions: load claude config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), OverallRunTimeout)
		defer cancel()

		result := RunSuggestionsForPages(ctx, db, cfg, user.ID, EnabledPages())
		writeJSON(w, http.StatusOK, result)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
