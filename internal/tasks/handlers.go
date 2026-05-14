package tasks

import (
	"database/sql"
	"encoding/json"
	"errors"
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
		log.Printf("tasks: writeJSON encode error: %v", err)
	}
}

// ListHandler returns the authenticated user's tasks, filtered by archived state.
// Defaults to archived=false when the query parameter is missing.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		archived := false
		if q := r.URL.Query().Get("archived"); q != "" {
			parsed, err := strconv.ParseBool(q)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "archived must be a boolean"})
				return
			}
			archived = parsed
		}

		items, err := ListTasks(db, user.ID, archived)
		if err != nil {
			log.Printf("tasks: list failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tasks"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": items})
	}
}

// CreateHandler creates a new task. Title is required.
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Title string   `json:"title"`
			Body  string   `json:"body"`
			Tags  []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Title = strings.TrimSpace(body.Title)
		if body.Title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
			return
		}

		task, err := CreateTask(db, user.ID, body.Title, body.Body, body.Tags)
		if err != nil {
			log.Printf("tasks: create failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create task"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"task": task})
	}
}

// UpdateHandler applies a partial update to a task. Fields omitted from the
// request body are left unchanged.
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
			return
		}

		var body struct {
			Title    *string   `json:"title"`
			Body     *string   `json:"body"`
			Tags     *[]string `json:"tags"`
			Archived *bool     `json:"archived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if body.Title != nil {
			trimmed := strings.TrimSpace(*body.Title)
			if trimmed == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title must not be empty"})
				return
			}
			body.Title = &trimmed
		}

		task, err := UpdateTask(db, id, user.ID, TaskUpdate{
			Title:    body.Title,
			Body:     body.Body,
			Tags:     body.Tags,
			Archived: body.Archived,
		})
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
				return
			}
			log.Printf("tasks: update failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	}
}

// DeleteHandler deletes a task and its cascaded children.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
			return
		}

		if err := DeleteTask(db, id, user.ID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
				return
			}
			log.Printf("tasks: delete failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete task"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// AddNoteHandler appends a note to a task.
func AddNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
			return
		}

		note, err := AddNote(db, taskID, user.ID, body.Content)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
				return
			}
			log.Printf("tasks: add note failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add note"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"note": note})
	}
}

// DeleteNoteHandler removes a note from a task.
func DeleteNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
			return
		}
		noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note ID"})
			return
		}

		if err := DeleteNote(db, taskID, noteID, user.ID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
				return
			}
			if errors.Is(err, ErrNoteNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "task note not found"})
				return
			}
			log.Printf("tasks: delete note failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete note"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
