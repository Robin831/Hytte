package links

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ListHandler returns all short links for the authenticated user.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		links, err := ListByUser(db, user.ID)
		if err != nil {
			log.Printf("Failed to list links: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list links"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"links": links})
	}
}

// CreateHandler creates a new short link.
// Expects JSON body: {"target_url": "...", "title": "...", "code": "..."}
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			TargetURL string `json:"target_url"`
			Title     string `json:"title"`
			Code      string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.TargetURL = strings.TrimSpace(body.TargetURL)
		body.Title = strings.TrimSpace(body.Title)
		body.Code = strings.TrimSpace(body.Code)

		if body.TargetURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_url is required"})
			return
		}

		// Basic URL validation.
		if !strings.HasPrefix(body.TargetURL, "http://") && !strings.HasPrefix(body.TargetURL, "https://") {
			body.TargetURL = "https://" + body.TargetURL
		}

		link, err := Create(db, user.ID, body.Code, body.TargetURL, body.Title)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "that short code is already taken"})
				return
			}
			log.Printf("Failed to create link: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create link"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"link": link})
	}
}

// UpdateHandler updates an existing short link.
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid link ID"})
			return
		}

		var body struct {
			TargetURL string `json:"target_url"`
			Title     string `json:"title"`
			Code      string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.TargetURL = strings.TrimSpace(body.TargetURL)
		body.Title = strings.TrimSpace(body.Title)
		body.Code = strings.TrimSpace(body.Code)

		if body.TargetURL == "" || body.Code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_url and code are required"})
			return
		}

		if !strings.HasPrefix(body.TargetURL, "http://") && !strings.HasPrefix(body.TargetURL, "https://") {
			body.TargetURL = "https://" + body.TargetURL
		}

		link, err := Update(db, id, user.ID, body.Code, body.TargetURL, body.Title)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "that short code is already taken"})
				return
			}
			log.Printf("Failed to update link: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update link"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"link": link})
	}
}

// DeleteHandler deletes a short link owned by the authenticated user.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid link ID"})
			return
		}

		if err := Delete(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "link not found"})
				return
			}
			log.Printf("Failed to delete link: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete link"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// RedirectHandler looks up a short code and redirects to the target URL.
// This is mounted outside the /api prefix (e.g., /go/{code}).
func RedirectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := chi.URLParam(r, "code")
		if code == "" {
			http.NotFound(w, r)
			return
		}

		link, err := GetByCode(db, code)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Increment clicks asynchronously — don't block the redirect.
		go func() {
			if err := IncrementClicks(db, link.ID); err != nil {
				log.Printf("Failed to increment clicks for link %d: %v", link.ID, err)
			}
		}()

		http.Redirect(w, r, link.TargetURL, http.StatusFound)
	}
}
