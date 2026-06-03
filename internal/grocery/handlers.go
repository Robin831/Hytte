package grocery

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// sseHeartbeatInterval is how often the events stream emits a comment line to
// keep the connection alive through proxies that close idle connections.
const sseHeartbeatInterval = 25 * time.Second

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
		DefaultBroker.Publish(user.ID, GroceryEvent{Type: EventItemAdded, Payload: created})
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
		DefaultBroker.Publish(user.ID, GroceryEvent{
			Type:    EventItemChanged,
			Payload: map[string]any{"id": id, "checked": body.Checked},
		})
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
		DefaultBroker.Publish(user.ID, GroceryEvent{
			Type:    EventItemReordered,
			Payload: map[string]any{"id": id, "sort_order": body.SortOrder},
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleTranslate accepts raw speech text, translates/normalizes it via Claude CLI,
// and returns structured grocery items.
func HandleTranslate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		const maxBody int64 = 4096
		data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
			return
		}
		if int64(len(data)) > maxBody {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}

		var body struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
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
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "claude is not enabled"})
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

		// Capture the IDs before deletion so subscribers can patch them out.
		ids, err := CompletedIDs(db, user.ID)
		if err != nil {
			log.Printf("Failed to list completed grocery items: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to clear completed items"})
			return
		}

		deleted, err := DeleteCompleted(db, user.ID)
		if err != nil {
			log.Printf("Failed to clear completed grocery items: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to clear completed items"})
			return
		}
		if len(ids) > 0 {
			DefaultBroker.Publish(user.ID, GroceryEvent{
				Type:    EventItemRemoved,
				Payload: map[string]any{"ids": ids},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	}
}

// HandleEvents streams grocery list mutations for the authenticated user's
// household as server-sent events. It holds the connection open, pushing one
// SSE frame per event, and emits periodic heartbeat comments so proxies don't
// close the idle connection. The subscription is released when the client
// disconnects (r.Context().Done()), preventing goroutine/connection leaks.
func HandleEvents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		events, unsubscribe := DefaultBroker.Subscribe(user.ID)
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		heartbeat := time.NewTicker(sseHeartbeatInterval)
		defer heartbeat.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case event, ok := <-events:
				if !ok {
					return
				}
				data, err := json.Marshal(event.Payload)
				if err != nil {
					log.Printf("grocery: marshal sse event %q: %v", event.Type, err)
					continue
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
