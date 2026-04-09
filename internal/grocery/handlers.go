package grocery

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// HandleList returns all grocery items for the authenticated user's household.
func HandleList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		items, err := ListByHousehold(db, user.ID)
		if err != nil {
			log.Printf("Failed to list grocery items: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list items"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdd creates a new grocery item.
func HandleAdd(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body AddItemRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Content = strings.TrimSpace(body.Content)
		if body.Content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
			return
		}

		originalText := body.OriginalText
		if originalText == "" {
			originalText = body.Content
		}

		item := GroceryItem{
			HouseholdID:    user.ID,
			Content:        body.Content,
			OriginalText:   originalText,
			SourceLanguage: body.SourceLanguage,
			AddedBy:        user.ID,
		}

		created, err := Add(db, item)
		if err != nil {
			log.Printf("Failed to add grocery item: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add item"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"item": created})
	}
}

// HandleCheck toggles the checked state of a grocery item.
func HandleCheck(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid item ID"})
			return
		}

		var body UpdateCheckedRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if err := UpdateChecked(db, id, user.ID, body.Checked); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
				return
			}
			log.Printf("Failed to update grocery item checked: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update item"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleReorder updates the sort_order of a grocery item.
func HandleReorder(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid item ID"})
			return
		}

		var body UpdateSortOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if err := UpdateSortOrder(db, id, user.ID, body.SortOrder); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
				return
			}
			log.Printf("Failed to update grocery item sort order: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder item"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleTranslate accepts raw speech text, translates/normalizes it via Claude CLI,
// and returns structured grocery items.
func HandleTranslate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Text = strings.TrimSpace(body.Text)
		if body.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
			return
		}

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load Claude config for translate: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load translation config"})
			return
		}

		items, err := TranslateAndNormalize(r.Context(), cfg, body.Text)
		if err != nil {
			log.Printf("Translation failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "translation failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleClearCompleted deletes all checked items for the authenticated user's household.
func HandleClearCompleted(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		deleted, err := DeleteCompleted(db, user.ID)
		if err != nil {
			log.Printf("Failed to clear completed grocery items: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to clear completed items"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	}
}
