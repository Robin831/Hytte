package familychat

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
		log.Printf("familychat: writeJSON encode: %v", err)
	}
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// parseConvID extracts and validates the {id} path parameter for the
// conversation routes. On failure we respond 404 (not 400) because an
// unparseable id may simply be a probe for a conversation that does not
// exist, and we never want to leak that signal.
func parseConvID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ListConversationsHandler returns every conversation the authenticated user
// is a member of.
func ListConversationsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convos, err := ListConversations(db, user.ID)
		if err != nil {
			log.Printf("familychat: list conversations for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convos})
	}
}

// CreateConversationHandler creates a new conversation with the requesting
// user as owner. The body shape is {name, member_user_ids}.
func CreateConversationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Name          string  `json:"name"`
			MemberUserIDs []int64 `json:"member_user_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		c, err := CreateConversation(db, user.ID, body.Name, body.MemberUserIDs)
		if err != nil {
			log.Printf("familychat: create conversation for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"conversation": c})
	}
}

// GetConversationHandler returns full metadata for a single conversation.
// Non-members get 404 to avoid revealing existence.
func GetConversationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		c, err := GetConversation(db, convID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotMember) || errors.Is(err, sql.ErrNoRows) {
				notFound(w)
				return
			}
			log.Printf("familychat: get conversation %d for user %d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversation": c})
	}
}

// ListMessagesHandler returns messages, newest first. Optional query params:
//
//	since=<id>  - return only messages with id > since
//	limit=<n>   - cap the response size (default 50, max 500)
func ListMessagesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}

		var since int64
		if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since parameter"})
				return
			}
			since = n
		}
		limit := 50
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit parameter"})
				return
			}
			limit = n
		}

		msgs, err := ListMessages(db, convID, user.ID, since, limit)
		if err != nil {
			if errors.Is(err, ErrNotMember) {
				notFound(w)
				return
			}
			log.Printf("familychat: list messages conv=%d user=%d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list messages"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
	}
}

// PostMessageHandler appends a message to a conversation and returns the
// inserted row decrypted.
func PostMessageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}

		var body struct {
			Body           string `json:"body"`
			AttachmentPath string `json:"attachment_path"`
			AttachmentMime string `json:"attachment_mime"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		body.Body = strings.TrimSpace(body.Body)
		if body.Body == "" && body.AttachmentPath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body or attachment_path is required"})
			return
		}

		msg, err := CreateMessage(db, convID, user.ID, body.Body, body.AttachmentPath, body.AttachmentMime)
		if err != nil {
			if errors.Is(err, ErrNotMember) {
				notFound(w)
				return
			}
			log.Printf("familychat: post message conv=%d user=%d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to post message"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"message": msg})
	}
}

// MarkReadHandler updates the requesting member's last_read_at to {at}.
// Missing/empty `at` defaults to the current server time.
func MarkReadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}

		var body struct {
			At string `json:"at"`
		}
		// An empty body is valid: it just means "mark read up to now".
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}

		if err := MarkRead(db, convID, user.ID, strings.TrimSpace(body.At)); err != nil {
			if errors.Is(err, ErrNotMember) {
				notFound(w)
				return
			}
			log.Printf("familychat: mark read conv=%d user=%d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark read"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// DeleteConversationHandler removes a conversation. Only the owner may
// delete; non-owners (including members) get 404.
func DeleteConversationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		if err := DeleteConversation(db, convID, user.ID); err != nil {
			if errors.Is(err, ErrNotMember) {
				notFound(w)
				return
			}
			log.Printf("familychat: delete conversation %d for user %d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete conversation"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
