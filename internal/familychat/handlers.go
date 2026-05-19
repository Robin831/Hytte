package familychat

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Input validation bounds for the Family Chat handlers. The body cap is a
// generous safety net to keep one client from filling the DB with one
// request; clients should enforce friendlier UI limits well below these.
const (
	maxNameLen        = 200
	maxBodyLen        = 8000
	maxAttachmentPath = 1024
	maxAttachmentMime = 128
	maxMembersPerConv = 100
	defaultMsgLimit   = 50
	maxMsgLimit       = 500
	maxRequestBytes   = 1 << 20 // 1 MiB
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

// decodeJSON enforces a body-size cap and parses JSON into dst. Returns true
// on success; on failure it writes a 400 response and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return false
	}
	return true
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
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if len([]rune(body.Name)) > maxNameLen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is too long"})
			return
		}
		if len(body.MemberUserIDs) > maxMembersPerConv {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many members"})
			return
		}
		for _, uid := range body.MemberUserIDs {
			if uid <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid member id"})
				return
			}
		}
		// Pre-validate that referenced users exist so FK failures map to 400.
		for _, uid := range body.MemberUserIDs {
			var exists int
			if err := db.QueryRow(`SELECT 1 FROM users WHERE id = ?`, uid).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("user %d not found", uid)})
					return
				}
				log.Printf("familychat: lookup user %d: %v", uid, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to validate members"})
				return
			}
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
			if errors.Is(err, ErrForbidden) || errors.Is(err, sql.ErrNoRows) {
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
		limit := defaultMsgLimit
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > maxMsgLimit {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit parameter"})
				return
			}
			limit = n
		}

		msgs, err := ListMessages(db, convID, user.ID, since, limit)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
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
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Body = strings.TrimSpace(body.Body)
		body.AttachmentPath = strings.TrimSpace(body.AttachmentPath)
		body.AttachmentMime = strings.TrimSpace(body.AttachmentMime)
		if body.Body == "" && body.AttachmentPath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body or attachment_path is required"})
			return
		}
		if len([]rune(body.Body)) > maxBodyLen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is too long"})
			return
		}
		if len(body.AttachmentPath) > maxAttachmentPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attachment_path is too long"})
			return
		}
		if len(body.AttachmentMime) > maxAttachmentMime {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attachment_mime is too long"})
			return
		}

		msg, err := CreateMessage(db, convID, user.ID, body.Body, body.AttachmentPath, body.AttachmentMime)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
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
		// Decode the body unconditionally; an empty body (io.EOF) means "mark
		// read up to now". Relying on ContentLength is unreliable for chunked
		// requests where ContentLength == -1.
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		at := strings.TrimSpace(body.At)
		if at != "" {
			// Parse RFC3339 (with or without sub-second precision), then
			// normalize to UTC with the same fixed-width timeFormat used for
			// stored created_at values so that string comparisons stay correct.
			t, err := time.Parse(time.RFC3339Nano, at)
			if err != nil {
				t, err = time.Parse(time.RFC3339, at)
			}
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid at timestamp (expected RFC3339)"})
				return
			}
			at = t.UTC().Format(timeFormat)
		}

		if err := MarkRead(db, convID, user.ID, at); err != nil {
			if errors.Is(err, ErrForbidden) {
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
			if errors.Is(err, ErrForbidden) {
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
