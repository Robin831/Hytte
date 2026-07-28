package offers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// HandleList returns all current offers ranked for the authenticated user
// (watchlist matches first, then best discount), plus the user's watchlist and
// the freshness timestamp. Chain filtering and text search are client-side —
// the full set is ~1,400 small rows.
func HandleList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		current, err := ListCurrent(db)
		if err != nil {
			log.Printf("offers: list current: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list offers")
			return
		}
		watchlist, err := ListWatchlist(db, user.ID)
		if err != nil {
			log.Printf("offers: list watchlist: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list offers")
			return
		}
		keywords := make([]string, len(watchlist))
		for i, wle := range watchlist {
			keywords[i] = wle.Keyword
		}

		var fetchedAt *time.Time
		if last, err := LastFetchedAt(db); err == nil && !last.IsZero() {
			fetchedAt = &last
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"offers":     Rank(current, keywords),
			"watchlist":  watchlist,
			"fetched_at": fetchedAt,
		})
	}
}

// HandleAddWatch adds a priority keyword to the user's watchlist.
func HandleAddWatch(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body AddWatchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		body.Keyword = strings.TrimSpace(body.Keyword)
		if body.Keyword == "" {
			writeError(w, http.StatusBadRequest, "keyword is required")
			return
		}
		if len([]rune(body.Keyword)) > 60 {
			writeError(w, http.StatusBadRequest, "keyword too long")
			return
		}

		entry, err := AddWatchlist(db, user.ID, body.Keyword)
		if err != nil {
			if err == ErrDuplicateKeyword {
				writeError(w, http.StatusConflict, "keyword already on watchlist")
				return
			}
			log.Printf("offers: add watchlist: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add keyword")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"entry": entry})
	}
}

// HandleDeleteWatch removes a keyword from the user's watchlist.
func HandleDeleteWatch(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid keyword ID")
			return
		}
		if err := DeleteWatchlist(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "keyword not found")
				return
			}
			log.Printf("offers: delete watchlist: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to delete keyword")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// refreshTimeout bounds an admin-triggered manual sync: 16 dealers with
// pagination and polite pauses complete in well under two minutes.
const refreshTimeout = 3 * time.Minute

// HandleRefresh triggers a synchronous sync sweep. Admin-only (registered
// behind RequireAdmin) — the scheduled daily run is the normal path.
func HandleRefresh(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), refreshTimeout)
		defer cancel()
		if err := Sync(ctx, db); err != nil {
			log.Printf("offers: manual refresh: %v", err)
			writeError(w, http.StatusBadGateway, "refresh failed")
			return
		}
		last, _ := LastFetchedAt(db)
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "fetched_at": last})
	}
}
