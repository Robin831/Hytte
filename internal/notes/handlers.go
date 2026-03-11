package notes

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
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// ListHandler returns all notes for the authenticated user, with optional search and tag filters.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		tag := strings.TrimSpace(r.URL.Query().Get("tag"))

		notes, err := List(db, user.ID, search, tag)
		if err != nil {
			log.Printf("Failed to list notes: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list notes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
	}
}

// CreateHandler creates a new note.
// Expects JSON body: {"title": "...", "content": "...", "tags": [...]}
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Title   string   `json:"title"`
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Title = strings.TrimSpace(body.Title)
		if body.Tags == nil {
			body.Tags = []string{}
		}
		for _, tag := range body.Tags {
			if strings.ContainsRune(tag, ',') {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tags must not contain commas"})
				return
			}
		}

		note, err := Create(db, user.ID, body.Title, body.Content, body.Tags)
		if err != nil {
			log.Printf("Failed to create note: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create note"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"note": note})
	}
}

// GetHandler returns a single note by ID.
func GetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note ID"})
			return
		}

		note, err := GetByID(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
				return
			}
			log.Printf("Failed to get note %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get note"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"note": note})
	}
}

// UpdateHandler updates an existing note's title, content, and tags.
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note ID"})
			return
		}

		var body struct {
			Title   string   `json:"title"`
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Title = strings.TrimSpace(body.Title)
		if body.Tags == nil {
			body.Tags = []string{}
		}
		for _, tag := range body.Tags {
			if strings.ContainsRune(tag, ',') {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tags must not contain commas"})
				return
			}
		}

		note, err := Update(db, id, user.ID, body.Title, body.Content, body.Tags)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
				return
			}
			log.Printf("Failed to update note %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update note"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"note": note})
	}
}

// DeleteHandler deletes a note owned by the authenticated user.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note ID"})
			return
		}

		if err := Delete(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
				return
			}
			log.Printf("Failed to delete note %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete note"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// TagsHandler returns all distinct tags used by the authenticated user's notes.
func TagsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		tags, err := ListTags(db, user.ID)
		if err != nil {
			log.Printf("Failed to list tags: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tags"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	}
}
