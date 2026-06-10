package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// SupportedModels is the allowlist of Claude model IDs a conversation may use.
// Keep this in sync with the model options offered in the chat header dropdown
// (web/src/pages/Chat.tsx) and the Settings page (web/src/pages/Settings.tsx).
var SupportedModels = map[string]bool{
	"claude-fable-5":    true,
	"claude-opus-4-8":   true,
	"claude-opus-4-7":   true,
	"claude-opus-4-6":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
}

// runPromptFn is the function used to invoke Claude (for auto-title generation).
var runPromptFn = training.RunPrompt

// runPromptWithSessionFn is the function used for session-aware Claude calls. It can be replaced in tests.
var runPromptWithSessionFn = training.RunPromptWithSession

// runPromptWithSessionStreamFn is the streaming variant used by
// StreamMessageHandler. Replaced in tests so the handler can be exercised
// without spawning the Claude CLI.
var runPromptWithSessionStreamFn = training.RunPromptWithSessionStream

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ListHandler returns all conversations for the authenticated user.
// GET /api/chat/conversations
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convos, err := ListConversations(db, user.ID)
		if err != nil {
			log.Printf("Failed to list conversations: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
			return
		}
		if convos == nil {
			convos = []Conversation{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convos})
	}
}

// CreateHandler creates a new conversation.
// POST /api/chat/conversations
// Body: {"model": "claude-sonnet-4-6"}
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		model := strings.TrimSpace(body.Model)
		if model == "" {
			// Fall back to user's configured Claude model, validated against
			// the allowlist so an unsupported config value can't slip through.
			cfg, err := training.LoadClaudeConfig(db, user.ID)
			if err == nil && SupportedModels[strings.TrimSpace(cfg.Model)] {
				model = strings.TrimSpace(cfg.Model)
			} else {
				model = "claude-sonnet-4-6"
			}
		} else if !SupportedModels[model] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported model"})
			return
		}

		convo, err := CreateConversation(db, user.ID, "", model)
		if err != nil {
			log.Printf("Failed to create conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"conversation": convo})
	}
}

// GetHandler returns a conversation with all its messages.
// GET /api/chat/conversations/{id}
func GetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		convo, err := GetConversation(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to get conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}

		msgs, err := GetMessages(db, convo.ID)
		if err != nil {
			log.Printf("Failed to get messages: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get messages"})
			return
		}
		if msgs == nil {
			msgs = []Message{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversation": convo,
			"messages":     msgs,
		})
	}
}

// DeleteHandler deletes a conversation and all its messages.
// DELETE /api/chat/conversations/{id}
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		if err := DeleteConversation(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to delete conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete conversation"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// SendMessageHandler adds a user message, calls Claude CLI, and returns the assistant response.
// POST /api/chat/conversations/{id}/messages
// Body: {"content": "Hello, Claude!"}
func SendMessageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		content := strings.TrimSpace(body.Content)
		if content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
			return
		}

		// Verify conversation exists and belongs to user.
		convo, err := GetConversation(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to get conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}

		// Load Claude configuration.
		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load Claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Override model with the conversation's model.
		cfg.Model = convo.Model

		// Store the user message.
		userMsg, err := InsertMessage(db, convo.ID, "user", content)
		if err != nil {
			log.Printf("Failed to insert user message: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
			return
		}

		// Call Claude CLI with session resumption.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		result, err := runPromptWithSessionFn(ctx, cfg, content, convo.SessionID)
		if err != nil && convo.SessionID != "" {
			// Session may have expired or become invalid — retry without session.
			log.Printf("Session resume failed, retrying fresh: %v", err)
			result, err = runPromptWithSessionFn(ctx, cfg, content, "")
		}
		if err != nil {
			log.Printf("Claude CLI error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude failed to respond"})
			return
		}

		// Save the session ID for future resumption.
		if result.SessionID != "" && result.SessionID != convo.SessionID {
			if dbErr := UpdateSessionID(db, convo.ID, user.ID, result.SessionID); dbErr != nil {
				log.Printf("Failed to save session ID: %v", dbErr)
			}
		}

		// Store the assistant response.
		assistantMsg, err := InsertMessage(db, convo.ID, "assistant", result.Response)
		if err != nil {
			log.Printf("Failed to insert assistant message: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save response"})
			return
		}

		// Auto-title: generate a title after the first exchange if the conversation has no title.
		// Runs synchronously with a bounded timeout so the returned conversation reflects the
		// final title on the first turn — the client merges this into local state instead of
		// firing a follow-up GET /api/chat/conversations.
		if convo.Title == "" {
			// Derive a child context from the already-deadlined ctx with an additional
			// 15s cap. Because WithTimeout takes the minimum of the parent deadline and
			// the new deadline, auto-titling is bounded by min(remaining parent budget, 15s).
			titleCtx, titleCancel := context.WithTimeout(ctx, 15*time.Second)
			defer titleCancel()
			autoTitle(titleCtx, db, cfg, convo.ID, user.ID, content)
		}

		// Re-load the conversation row so title and updated_at reflect the final state
		// (after the message insert and any auto-title update).
		updatedConvo, err := GetConversation(db, convo.ID, user.ID)
		if err != nil {
			log.Printf("Failed to reload conversation after send: %v", err)
			// Fall back to the pre-send snapshot; the client can still display the message pair.
			updatedConvo = convo
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user_message":      userMsg,
			"assistant_message": assistantMsg,
			"conversation":      updatedConvo,
		})
	}
}

// RenameHandler updates a conversation's title and/or model. Both fields are
// optional; at least one must be supplied. A supplied model is validated
// against SupportedModels.
// PUT /api/chat/conversations/{id}
// Body: {"title": "New Title"} and/or {"model": "claude-opus-4-8"}
func RenameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		var body struct {
			Title *string `json:"title"`
			Model *string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		var titlePtr *string
		if body.Title != nil {
			title := strings.TrimSpace(*body.Title)
			if title == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
				return
			}
			titlePtr = &title
		}

		var modelPtr *string
		if body.Model != nil {
			model := strings.TrimSpace(*body.Model)
			if !SupportedModels[model] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported model"})
				return
			}
			modelPtr = &model
		}

		if titlePtr == nil && modelPtr == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title or model is required"})
			return
		}

		convo, err := UpdateConversation(db, id, user.ID, titlePtr, modelPtr)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to update conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update conversation"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"conversation": convo})
	}
}

// truncateRunes returns s truncated to at most max runes, preserving valid UTF-8.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// StreamMessageHandler is the SSE counterpart to SendMessageHandler. It
// persists the incoming user message, then opens a text/event-stream
// response and forwards Claude CLI text deltas as they arrive. After the
// stream finishes successfully the assembled assistant message is persisted
// and a final `done` event carries the saved row. CLI errors emit a single
// `error` event and leave no assistant row behind.
//
// POST /api/chat/conversations/{id}/messages/stream
// Body: {"content": "Hello!"}
// Events:
//   - user_message  {message}             — the persisted user row
//   - token         {text}                — incremental text delta from Claude
//   - done          {assistant_message}   — the persisted assistant row
//   - error         {error}               — single fatal error before `done`
func StreamMessageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		content := strings.TrimSpace(body.Content)
		if content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
			return
		}

		// Verify conversation exists and belongs to user.
		convo, err := GetConversation(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to get conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}

		// Load Claude configuration.
		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load Claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Override model with the conversation's model.
		cfg.Model = convo.Model

		// Check Flusher support before persisting the user message so that a
		// non-streaming response writer surfaces as a regular JSON error rather
		// than leaving a dangling user message without an assistant reply.
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		// Persist the user message. DB failures surface as a normal JSON error
		// because we haven't committed to SSE mode yet (headers not sent).
		userMsg, err := InsertMessage(db, convo.ID, "user", content)
		if err != nil {
			log.Printf("Failed to insert user message: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Send the persisted user row first so the client can replace its
		// optimistic placeholder with the canonical id.
		writeSSEEvent(w, flusher, "user_message", userMsg)

		// Cancel the Claude subprocess when the client disconnects.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		var newSessionID string
		var tokensEmitted bool
		runStream := func(sessionID string) (string, error) {
			return runPromptWithSessionStreamFn(ctx, cfg, content, sessionID,
				func(chunk string) {
					tokensEmitted = true
					writeSSEEvent(w, flusher, "token", map[string]string{"text": chunk})
				},
				func(sid string) {
					newSessionID = sid
				},
			)
		}

		full, err := runStream(convo.SessionID)
		// Only retry without the stored session id if the first attempt
		// failed BEFORE any tokens reached the client. Replaying after
		// partial output would duplicate text in the assistant bubble.
		if err != nil && convo.SessionID != "" && ctx.Err() == nil && !tokensEmitted {
			log.Printf("Session resume failed before any tokens, retrying fresh: %v", err)
			full, err = runStream("")
		}

		if err != nil {
			ctxErr := ctx.Err()
			// Client disconnected or user clicked Stop — leave partial text on
			// the client and skip the assistant row. The user message stays
			// persisted as a faithful record of what the user typed.
			if ctxErr == context.Canceled {
				log.Printf("chat stream cancelled for conversation %d", convo.ID)
				return
			}
			// Server-side timeout — emit an error event so the client can
			// distinguish this from a voluntary Stop.
			if ctxErr == context.DeadlineExceeded {
				log.Printf("chat stream timed out for conversation %d", convo.ID)
				writeSSEEvent(w, flusher, "error", map[string]string{"error": "response timed out"})
				return
			}
			log.Printf("Claude CLI streaming error: %v", err)
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "Claude failed to respond"})
			return
		}

		// Save the session ID for future resumption.
		if newSessionID != "" && newSessionID != convo.SessionID {
			if dbErr := UpdateSessionID(db, convo.ID, user.ID, newSessionID); dbErr != nil {
				log.Printf("Failed to save session ID: %v", dbErr)
			}
		}

		full = strings.TrimSpace(full)
		if full == "" {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "Claude returned an empty response"})
			return
		}

		assistantMsg, err := InsertMessage(db, convo.ID, "assistant", full)
		if err != nil {
			log.Printf("Failed to insert assistant message: %v", err)
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "failed to save response"})
			return
		}

		// Auto-title: same logic as SendMessageHandler. autoTitle re-checks
		// the title so we don't overwrite a user-set title.
		if convo.Title == "" {
			convoID := convo.ID
			userID := user.ID
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer bgCancel()
				autoTitle(bgCtx, db, cfg, convoID, userID, content)
			}()
		}

		writeSSEEvent(w, flusher, "done", map[string]any{"assistant_message": assistantMsg})
	}
}

// writeSSEEvent serialises payload as JSON and writes a single SSE frame.
// Failures are swallowed because a write error means the client is gone.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("chat: marshal sse event %q: %v", event, err)
		return
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return
	}
	flusher.Flush()
}

// autoTitle generates a short title for the conversation based on the first user message
// and updates the row in place. The caller controls the deadline via ctx.
func autoTitle(ctx context.Context, db *sql.DB, cfg *training.ClaudeConfig, convoID, userID int64, firstMessage string) {
	// Re-check the title under the caller's context to avoid overwriting a user-set title.
	var currentTitle string
	if err := db.QueryRowContext(ctx,
		"SELECT title FROM chat_conversations WHERE id = ? AND user_id = ?",
		convoID, userID,
	).Scan(&currentTitle); err != nil {
		return
	}
	if currentTitle != "" {
		return
	}

	// Truncate long messages for title generation (rune-safe to avoid splitting UTF-8).
	msg := truncateRunes(firstMessage, 500)

	prompt := fmt.Sprintf(
		"Generate a very short title (max 6 words) for a conversation that starts with this message. "+
			"Reply with ONLY the title, no quotes, no punctuation at the end.\n\nMessage: %s", msg)

	title, err := runPromptFn(ctx, cfg, prompt)
	if err != nil {
		log.Printf("Auto-title generation failed: %v", err)
		return
	}

	title = strings.TrimSpace(title)
	// Sanitize: remove surrounding quotes if present.
	title = strings.Trim(title, "\"'")
	if title == "" {
		return
	}

	// Cap title length (rune-safe to avoid splitting multi-byte characters).
	title = truncateRunes(title, 100)

	if _, err := RenameConversation(db, convoID, userID, title); err != nil {
		log.Printf("Failed to auto-title conversation %d: %v", convoID, err)
	}
}
