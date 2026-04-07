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

// runPromptFn is the function used to invoke Claude. It can be replaced in tests.
var runPromptFn = training.RunPrompt

// maxContextMessages is the maximum number of messages included in the Claude prompt.
// Limits request size and avoids hitting Claude CLI context limits as conversations grow.
const maxContextMessages = 50

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
			// Fall back to user's configured Claude model.
			cfg, err := training.LoadClaudeConfig(db, user.ID)
			if err == nil && cfg.Model != "" {
				model = cfg.Model
			} else {
				model = "claude-sonnet-4-6"
			}
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

		// Build context from conversation history.
		history, err := GetMessages(db, convo.ID)
		if err != nil {
			log.Printf("Failed to get message history: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build context"})
			return
		}

		// Limit context to avoid unbounded prompt growth as conversations grow.
		if len(history) > maxContextMessages {
			history = history[len(history)-maxContextMessages:]
		}

		prompt := buildPrompt(history)

		// Call Claude CLI.
		ctx, cancel := context.WithTimeout(r.Context(), training.DefaultCLITimeout)
		defer cancel()

		response, err := runPromptFn(ctx, cfg, prompt)
		if err != nil {
			log.Printf("Claude CLI error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Claude failed to respond"})
			return
		}

		// Store the assistant response.
		assistantMsg, err := InsertMessage(db, convo.ID, "assistant", response)
		if err != nil {
			log.Printf("Failed to insert assistant message: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save response"})
			return
		}

		// Auto-title: generate a title after the first exchange if the conversation has no title.
		// Re-check the title inside the goroutine to avoid overwriting a user-set title.
		if convo.Title == "" {
			convoID := convo.ID
			userID := user.ID
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				var currentTitle string
				err := db.QueryRowContext(ctx,
					"SELECT title FROM chat_conversations WHERE id = ? AND user_id = ?",
					convoID, userID,
				).Scan(&currentTitle)
				if err != nil {
					return
				}
				if currentTitle != "" {
					// User has already set a title; skip auto-title.
					return
				}
				autoTitle(db, cfg, convoID, userID, content)
			}()
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user_message":      userMsg,
			"assistant_message": assistantMsg,
		})
	}
}

// RenameHandler updates a conversation's title.
// PUT /api/chat/conversations/{id}
// Body: {"title": "New Title"}
func RenameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		title := strings.TrimSpace(body.Title)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
			return
		}

		convo, err := RenameConversation(db, id, user.ID, title)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
				return
			}
			log.Printf("Failed to rename conversation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to rename conversation"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"conversation": convo})
	}
}

// buildPrompt constructs the full prompt from conversation history.
// Each message is formatted as "Human:" or "Assistant:" for Claude CLI context.
func buildPrompt(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "user":
			sb.WriteString("Human: ")
		case "assistant":
			sb.WriteString("Assistant: ")
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// truncateRunes returns s truncated to at most max runes, preserving valid UTF-8.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// autoTitle generates a short title for the conversation based on the first user message.
// It runs in the background and silently updates the title.
func autoTitle(db *sql.DB, cfg *training.ClaudeConfig, convoID, userID int64, firstMessage string) {
	// Truncate long messages for title generation (rune-safe to avoid splitting UTF-8).
	msg := truncateRunes(firstMessage, 500)

	prompt := fmt.Sprintf(
		"Generate a very short title (max 6 words) for a conversation that starts with this message. "+
			"Reply with ONLY the title, no quotes, no punctuation at the end.\n\nMessage: %s", msg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
