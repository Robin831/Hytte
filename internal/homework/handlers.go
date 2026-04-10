package homework

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// maxProfileBodySize is the maximum allowed request body size for profile updates (64 KB).
const maxProfileBodySize = 64 << 10

// maxConversationBodySize is the maximum allowed request body size for new conversations (8 KB).
const maxConversationBodySize = 8 << 10

// maxMessageUploadSize is the maximum allowed multipart body size for send message (10 MB).
const maxMessageUploadSize = 10 << 20

// allowedImageTypes lists MIME types accepted for homework image uploads.
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// verifyParentChild returns nil if a family_links row exists for parentID -> childID.
func verifyParentChild(db *sql.DB, parentID, childID int64) error {
	var id int64
	return db.QueryRow(
		`SELECT id FROM family_links WHERE parent_id = ? AND child_id = ?`,
		parentID, childID,
	).Scan(&id)
}

// parseChildID extracts the child ID from the URL and verifies the parent-child relationship.
// Returns the child ID or writes an error response and returns 0.
func parseChildID(w http.ResponseWriter, r *http.Request, db *sql.DB, userID int64) (int64, bool) {
	childID, err := strconv.ParseInt(chi.URLParam(r, "childId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
		return 0, false
	}

	if err := verifyParentChild(db, userID, childID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to access this child's data"})
		} else {
			log.Printf("homework: verify parent-child user %d child %d: %v", userID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return 0, false
	}
	return childID, true
}

// HandleGetProfile returns the homework profile for a child.
// GET /api/homework/children/{childId}/profile
func HandleGetProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		profile, err := GetProfileByKidID(db, childID)
		if err != nil {
			log.Printf("homework: get profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
			return
		}
		if profile == nil {
			writeJSON(w, http.StatusOK, map[string]any{"profile": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	}
}

// HandleUpdateProfile creates or updates the homework profile for a child.
// PUT /api/homework/children/{childId}/profile
func HandleUpdateProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxProfileBodySize)

		var body struct {
			Age               int      `json:"age"`
			GradeLevel        string   `json:"grade_level"`
			Subjects          []string `json:"subjects"`
			PreferredLanguage string   `json:"preferred_language"`
			SchoolName        string   `json:"school_name"`
			CurrentTopics     []string `json:"current_topics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.GradeLevel = strings.TrimSpace(body.GradeLevel)
		body.PreferredLanguage = strings.TrimSpace(body.PreferredLanguage)
		body.SchoolName = strings.TrimSpace(body.SchoolName)
		if body.Subjects == nil {
			body.Subjects = []string{}
		}
		if body.CurrentTopics == nil {
			body.CurrentTopics = []string{}
		}

		if body.Age < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "age must be non-negative"})
			return
		}

		profile := HomeworkProfile{
			KidID:             childID,
			Age:               body.Age,
			GradeLevel:        body.GradeLevel,
			Subjects:          body.Subjects,
			PreferredLanguage: body.PreferredLanguage,
			SchoolName:        body.SchoolName,
			CurrentTopics:     body.CurrentTopics,
		}

		// Try update first; if no row exists, create.
		err := UpdateProfile(db, profile)
		if errors.Is(err, sql.ErrNoRows) {
			created, createErr := CreateProfile(db, profile)
			if createErr != nil {
				log.Printf("homework: create profile kid %d: %v", childID, createErr)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create profile"})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"profile": created})
			return
		}
		if err != nil {
			log.Printf("homework: update profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
			return
		}

		// Re-read the updated profile.
		updated, err := GetProfileByKidID(db, childID)
		if err != nil {
			log.Printf("homework: re-read profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read updated profile"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": updated})
	}
}

// HandleListConversations returns all homework conversations for a child.
// GET /api/homework/children/{childId}/conversations
func HandleListConversations(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		convos, err := ListConversationsByKid(db, childID)
		if err != nil {
			log.Printf("homework: list conversations kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convos})
	}
}

// HandleGetConversation returns a single homework conversation with its messages.
// GET /api/homework/children/{childId}/conversations/{id}
func HandleGetConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		conv, err := GetConversation(db, convID, childID)
		if err != nil {
			log.Printf("homework: get conversation %d kid %d: %v", convID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		if conv == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		msgs, err := GetMessages(db, convID, childID)
		if err != nil {
			log.Printf("homework: get messages conv %d kid %d: %v", convID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get messages"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversation": conv,
			"messages":     msgs,
		})
	}
}

// HandleNewConversation starts a new homework conversation for a child.
// Accepts an optional "subject" field; if absent, defaults to empty string.
// POST /api/homework/children/{childId}/conversations
func HandleNewConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxConversationBodySize)

		var body struct {
			Subject string `json:"subject"`
		}
		// Subject is optional — tolerate an empty body, including whitespace-only bodies.
		if data, err := io.ReadAll(r.Body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		} else if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			if err := json.Unmarshal([]byte(trimmed), &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}

		body.Subject = strings.TrimSpace(body.Subject)

		conv, err := CreateConversation(db, HomeworkConversation{
			KidID:   childID,
			Subject: body.Subject,
		})
		if err != nil {
			log.Printf("homework: create conversation kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"conversation": conv})
	}
}

// HandleSendMessage accepts a multipart form with a text message and optional image,
// detects the subject, builds a system prompt, calls Claude CLI, and streams the
// response back to the client via SSE.
// POST /api/homework/children/{childId}/conversations/{id}/messages
func HandleSendMessage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		// Verify conversation exists and belongs to child.
		conv, err := GetConversation(db, convID, childID)
		if err != nil {
			log.Printf("homework: get conversation %d kid %d: %v", convID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		if conv == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		// Parse multipart form data.
		r.Body = http.MaxBytesReader(w, r.Body, maxMessageUploadSize)
		if err := r.ParseMultipartForm(maxMessageUploadSize); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}
		defer r.MultipartForm.RemoveAll()

		message := strings.TrimSpace(r.FormValue("message"))
		if message == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
			return
		}

		helpLevel := HelpLevel(strings.TrimSpace(r.FormValue("help_level")))
		if helpLevel == "" {
			helpLevel = HelpLevelHint
		}
		if !ValidHelpLevels[helpLevel] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid help_level: must be one of hint, explain, walkthrough, answer"})
			return
		}

		// Handle optional image upload.
		var imagePath string
		if fhs := r.MultipartForm.File["image"]; len(fhs) > 0 {
			file, err := fhs[0].Open()
			if err != nil {
				log.Printf("homework: open uploaded image: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read uploaded image"})
				return
			}
			defer file.Close()

			// Read first 512 bytes for MIME detection.
			header := make([]byte, 512)
			n, _ := file.Read(header)
			mimeType := http.DetectContentType(header[:n])
			if !allowedImageTypes[mimeType] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported image type"})
				return
			}

			// Seek back to beginning for full copy.
			if seeker, ok := file.(io.Seeker); ok {
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					log.Printf("homework: seek uploaded image: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read uploaded image"})
					return
				}
			}

			// Save to permanent upload directory so images are available in conversation history.
			uploadDir := filepath.Join(homeworkUploadsDir(), fmt.Sprintf("%d", conv.ID))
			if err := os.MkdirAll(uploadDir, 0700); err != nil {
				log.Printf("homework: create upload dir: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}

			ext := ".jpg"
			switch mimeType {
			case "image/png":
				ext = ".png"
			case "image/gif":
				ext = ".gif"
			case "image/webp":
				ext = ".webp"
			}

			imgFile, err := os.CreateTemp(uploadDir, fmt.Sprintf("hw-%d-*%s", convID, ext))
			if err != nil {
				log.Printf("homework: create image file: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}
			defer imgFile.Close()

			if _, err := io.Copy(imgFile, file); err != nil {
				log.Printf("homework: copy image: %v", err)
				os.Remove(imgFile.Name())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}
			imagePath = imgFile.Name()
		}

		// Detect subject from message text.
		detectedSubject := DetectSubject(message)

		// Use the persisted conversation subject when already set; only fall back to
		// the auto-detected value when the conversation has no subject yet.
		subjectForPrompt := conv.Subject
		if subjectForPrompt == "" {
			subjectForPrompt = detectedSubject
		}

		// Persist detected subject only when the conversation had none before.
		if conv.Subject == "" && detectedSubject != "general" {
			conv.Subject = detectedSubject
			if err := UpdateConversationSubject(db, conv.ID, childID, detectedSubject); err != nil {
				log.Printf("homework: update conversation subject conv %d: %v", conv.ID, err)
			}
		}

		// Load the child's profile for prompt building.
		profile, err := GetProfileByKidID(db, childID)
		if err != nil {
			log.Printf("homework: get profile for prompt kid %d: %v", childID, err)
		}
		if profile == nil {
			profile = &HomeworkProfile{}
		}

		systemPrompt := BuildSystemPrompt(*profile, helpLevel, subjectForPrompt)

		// Load Claude config from the parent's preferences.
		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("homework: load claude config user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Store the user message.
		userMsg, err := AddMessage(db, HomeworkMessage{
			ConversationID: conv.ID,
			Role:           "user",
			Content:        message,
			HelpLevel:      helpLevel,
			ImagePath:      imagePath,
		})
		if err != nil {
			log.Printf("homework: add user message conv %d: %v", conv.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
			return
		}

		// Get existing session ID for conversation continuity.
		var sessionID string
		sessionID, err = GetSessionID(db, conv.ID, childID)
		if err != nil {
			log.Printf("homework: get session ID conv %d: %v", conv.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		// Set up SSE streaming.
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Send the saved user message first.
		userMsgJSON, _ := json.Marshal(userMsg)
		fmt.Fprintf(w, "event: user_message\ndata: %s\n\n", userMsgJSON)
		flusher.Flush()

		// Build Claude CLI command with streaming output.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		fullResponse, newSessionID, err := streamClaude(ctx, cfg, systemPrompt, message, imagePath, sessionID, w, flusher)
		if err != nil && sessionID != "" {
			// Session may have expired — retry without session.
			log.Printf("homework: session resume failed, retrying fresh: %v", err)
			fmt.Fprintf(w, "event: retry\ndata: {\"reason\":\"session expired, retrying\"}\n\n")
			flusher.Flush()
			fullResponse, newSessionID, err = streamClaude(ctx, cfg, systemPrompt, message, imagePath, "", w, flusher)
		}

		if err != nil {
			log.Printf("homework: claude error conv %d: %v", conv.ID, err)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"Claude failed to respond\"}\n\n")
			flusher.Flush()
			return
		}

		// Save session ID for future resumption.
		if newSessionID != "" && newSessionID != sessionID {
			if dbErr := UpdateSessionID(db, conv.ID, childID, newSessionID); dbErr != nil {
				log.Printf("homework: save session ID conv %d: %v", conv.ID, dbErr)
			}
		}

		// Store the assistant response.
		assistantMsg, err := AddMessage(db, HomeworkMessage{
			ConversationID: conv.ID,
			Role:           "assistant",
			Content:        fullResponse,
			HelpLevel:      helpLevel,
		})
		if err != nil {
			log.Printf("homework: add assistant message conv %d: %v", conv.ID, err)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to save response\"}\n\n")
			flusher.Flush()
			return
		}

		// Send done event with the full saved assistant message.
		assistantJSON, _ := json.Marshal(assistantMsg)
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", assistantJSON)
		flusher.Flush()
	}
}

// ParentReviewResponse is the JSON shape returned by HandleParentReview.
type ParentReviewResponse struct {
	Conversations                  []ConversationSummary `json:"conversations"`
	TotalMessages                  int                   `json:"total_messages"`
	HelpLevelTotals                map[string]int        `json:"help_level_totals"`
	HelpLevelAverages              map[string]float64    `json:"help_level_averages"`
	AverageMessagesPerConversation float64               `json:"average_messages_per_conversation"`
}

// HandleParentReview returns an aggregated summary of a child's homework conversations
// with per-conversation and overall help-level statistics.
// GET /api/homework/children/{childId}/review
// Admin users bypass the family_links check and may access any child's review.
func HandleParentReview(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var childID int64
		if user.IsAdmin {
			id, err := strconv.ParseInt(chi.URLParam(r, "childId"), 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
				return
			}
			childID = id
		} else {
			var ok bool
			childID, ok = parseChildID(w, r, db, user.ID)
			if !ok {
				return
			}
		}

		summaries, err := GetConversationsForParentReview(db, childID)
		if err != nil {
			log.Printf("homework: parent review kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load review data"})
			return
		}
		if summaries == nil {
			summaries = []ConversationSummary{}
		}

		totalMessages := 0
		helpTotals := make(map[string]int)
		for _, s := range summaries {
			totalMessages += s.MessageCount
			for level, count := range s.HelpLevels {
				helpTotals[level] += count
			}
		}

		helpAverages := make(map[string]float64)
		var avgMsgsPerConv float64
		if len(summaries) > 0 {
			avgMsgsPerConv = float64(totalMessages) / float64(len(summaries))
			for level, count := range helpTotals {
				helpAverages[level] = float64(count) / float64(len(summaries))
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"review": ParentReviewResponse{
			Conversations:                  summaries,
			TotalMessages:                  totalMessages,
			HelpLevelTotals:                helpTotals,
			HelpLevelAverages:              helpAverages,
			AverageMessagesPerConversation: avgMsgsPerConv,
		}})
	}
}

// claudeStreamLine represents a line from Claude CLI's stream-json output.
type claudeStreamLine struct {
	Type      string `json:"type"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	// For assistant message events containing content blocks.
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// streamClaude runs the Claude CLI with stream-json output and sends text deltas
// as SSE events. If imagePath is non-empty, the image is included in the prompt
// using the @path syntax so Claude can analyse it. Returns the full response text
// and session ID.
func streamClaude(ctx context.Context, cfg *training.ClaudeConfig, systemPrompt, prompt, imagePath, sessionID string, w http.ResponseWriter, flusher http.Flusher) (string, string, error) {
	args := []string{"--model", cfg.Model, "-p", "-", "--output-format", "stream-json", "--system-prompt", systemPrompt}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	// Build the full prompt: include the image via @path so Claude can see it.
	fullPrompt := prompt
	if imagePath != "" {
		absPath, err := filepath.Abs(imagePath)
		if err == nil {
			imagePath = absPath
		}
		fullPrompt = prompt + "\n@" + imagePath
	}

	cmd := execCommand(ctx, cfg.CLIPath, args...)
	cmd.Stdin = strings.NewReader(fullPrompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("start claude: %w", err)
	}

	var fullText strings.Builder
	var resultSessionID string
	scanner := bufio.NewScanner(stdout)
	// Allow larger lines for content-heavy responses.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev claudeStreamLine
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "assistant":
			// Initial assistant message with content blocks.
			for _, block := range ev.Message.Content {
				if block.Type == "text" && block.Text != "" {
					fullText.WriteString(block.Text)
					data, _ := json.Marshal(map[string]string{"text": block.Text})
					fmt.Fprintf(w, "event: delta\ndata: %s\n\n", data)
					flusher.Flush()
				}
			}
		case "content_block_delta":
			// Parse delta text from the raw line.
			var delta struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal(line, &delta); err == nil && delta.Delta.Text != "" {
				fullText.WriteString(delta.Delta.Text)
				data, _ := json.Marshal(map[string]string{"text": delta.Delta.Text})
				fmt.Fprintf(w, "event: delta\ndata: %s\n\n", data)
				flusher.Flush()
			}
		case "result":
			if ev.IsError {
				cmd.Wait()
				return "", "", fmt.Errorf("claude returned error: %s", ev.Result)
			}
			if ev.Result != "" {
				// Use the result field as the authoritative full text.
				fullText.Reset()
				fullText.WriteString(ev.Result)
			}
			resultSessionID = ev.SessionID
		}
	}

	if err := scanner.Err(); err != nil {
		cmd.Wait()
		return "", "", fmt.Errorf("scan claude output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return "", "", fmt.Errorf("claude exit: %w", err)
	}

	return strings.TrimSpace(fullText.String()), resultSessionID, nil
}

// homeworkUploadsDir returns the base directory for homework image uploads.
// The HOMEWORK_UPLOADS_DIR environment variable overrides the default.
// --- Student-facing handlers ---
// These handlers let a child user access their own homework data.
// The child's user ID is used directly as the kid ID.

// HandleMyProfile returns the homework profile for the authenticated child.
// GET /api/homework/profile
func HandleMyProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		profile, err := GetProfileByKidID(db, user.ID)
		if err != nil {
			log.Printf("homework: get my profile user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
			return
		}
		if profile == nil {
			writeJSON(w, http.StatusOK, map[string]any{"profile": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	}
}

// HandleUpdateMyProfile creates or updates the homework profile for the
// authenticated child.
// PUT /api/homework/profile
func HandleUpdateMyProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxProfileBodySize)

		var body struct {
			Age               int      `json:"age"`
			GradeLevel        string   `json:"grade_level"`
			Subjects          []string `json:"subjects"`
			PreferredLanguage string   `json:"preferred_language"`
			SchoolName        string   `json:"school_name"`
			CurrentTopics     []string `json:"current_topics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.GradeLevel = strings.TrimSpace(body.GradeLevel)
		body.PreferredLanguage = strings.TrimSpace(body.PreferredLanguage)
		body.SchoolName = strings.TrimSpace(body.SchoolName)
		if body.Subjects == nil {
			body.Subjects = []string{}
		}
		if body.CurrentTopics == nil {
			body.CurrentTopics = []string{}
		}

		if body.Age < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "age must be non-negative"})
			return
		}

		profile := HomeworkProfile{
			KidID:             user.ID,
			Age:               body.Age,
			GradeLevel:        body.GradeLevel,
			Subjects:          body.Subjects,
			PreferredLanguage: body.PreferredLanguage,
			SchoolName:        body.SchoolName,
			CurrentTopics:     body.CurrentTopics,
		}

		err := UpdateProfile(db, profile)
		if errors.Is(err, sql.ErrNoRows) {
			created, createErr := CreateProfile(db, profile)
			if createErr != nil {
				log.Printf("homework: create my profile user %d: %v", user.ID, createErr)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create profile"})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"profile": created})
			return
		}
		if err != nil {
			log.Printf("homework: update my profile user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
			return
		}

		updated, err := GetProfileByKidID(db, user.ID)
		if err != nil {
			log.Printf("homework: re-read my profile user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read updated profile"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": updated})
	}
}

// HandleMyConversations returns all homework conversations for the authenticated child.
// GET /api/homework/conversations
func HandleMyConversations(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convos, err := ListConversationsByKid(db, user.ID)
		if err != nil {
			log.Printf("homework: list my conversations user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convos})
	}
}

// HandleNewMyConversation creates a new conversation for the authenticated child.
// POST /api/homework/conversations
func HandleNewMyConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxConversationBodySize)

		var body struct {
			Subject string `json:"subject"`
		}
		if data, err := io.ReadAll(r.Body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		} else if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			if err := json.Unmarshal([]byte(trimmed), &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}

		body.Subject = strings.TrimSpace(body.Subject)

		conv, err := CreateConversation(db, HomeworkConversation{
			KidID:   user.ID,
			Subject: body.Subject,
		})
		if err != nil {
			log.Printf("homework: create my conversation user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"conversation": conv})
	}
}

// HandleGetMyConversation returns a single conversation with messages for the
// authenticated child.
// GET /api/homework/conversations/{id}
func HandleGetMyConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		conv, err := GetConversation(db, convID, user.ID)
		if err != nil {
			log.Printf("homework: get my conversation %d user %d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		if conv == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		msgs, err := GetMessages(db, convID, user.ID)
		if err != nil {
			log.Printf("homework: get my messages conv %d: %v", convID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get messages"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversation": conv,
			"messages":     msgs,
		})
	}
}

// HandleSendMyMessage accepts a multipart form with a text message and optional
// image, builds a system prompt, calls Claude CLI, and streams the response
// back via SSE. Used by the authenticated child to send messages in their own
// conversations.
// POST /api/homework/conversations/{id}/messages
func HandleSendMyMessage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		kidID := user.ID

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		conv, err := GetConversation(db, convID, kidID)
		if err != nil {
			log.Printf("homework: get my conversation %d user %d: %v", convID, kidID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		if conv == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxMessageUploadSize)
		if err := r.ParseMultipartForm(maxMessageUploadSize); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}
		defer r.MultipartForm.RemoveAll()

		message := strings.TrimSpace(r.FormValue("message"))
		if message == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
			return
		}

		helpLevel := HelpLevel(strings.TrimSpace(r.FormValue("help_level")))
		if helpLevel == "" {
			helpLevel = HelpLevelHint
		}
		if !ValidHelpLevels[helpLevel] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid help_level: must be one of hint, explain, walkthrough, answer"})
			return
		}

		// Handle optional image upload.
		var imagePath string
		if fhs := r.MultipartForm.File["image"]; len(fhs) > 0 {
			file, err := fhs[0].Open()
			if err != nil {
				log.Printf("homework: open uploaded image: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read uploaded image"})
				return
			}
			defer file.Close()

			// Read first 512 bytes for MIME detection.
			hdr := make([]byte, 512)
			n, _ := file.Read(hdr)
			mimeType := http.DetectContentType(hdr[:n])
			if !allowedImageTypes[mimeType] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported image type"})
				return
			}

			// Seek back to beginning for full copy.
			if seeker, ok := file.(io.Seeker); ok {
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					log.Printf("homework: seek uploaded image: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read uploaded image"})
					return
				}
			}

			uploadDir := filepath.Join(homeworkUploadsDir(), fmt.Sprintf("%d", conv.ID))
			if err := os.MkdirAll(uploadDir, 0700); err != nil {
				log.Printf("homework: create upload dir: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}

			ext := ".jpg"
			switch mimeType {
			case "image/png":
				ext = ".png"
			case "image/gif":
				ext = ".gif"
			case "image/webp":
				ext = ".webp"
			}

			imgFile, err := os.CreateTemp(uploadDir, fmt.Sprintf("hw-%d-*%s", convID, ext))
			if err != nil {
				log.Printf("homework: create image file: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}
			defer imgFile.Close()

			if _, err := io.Copy(imgFile, file); err != nil {
				log.Printf("homework: copy image: %v", err)
				os.Remove(imgFile.Name())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save image"})
				return
			}
			imagePath = imgFile.Name()
		}

		// Detect subject from message text.
		detectedSubject := DetectSubject(message)

		subjectForPrompt := conv.Subject
		if subjectForPrompt == "" {
			subjectForPrompt = detectedSubject
		}

		// Persist detected subject only when the conversation had none before.
		if conv.Subject == "" && detectedSubject != "general" {
			conv.Subject = detectedSubject
			if err := UpdateConversationSubject(db, conv.ID, kidID, detectedSubject); err != nil {
				log.Printf("homework: update conversation subject conv %d: %v", conv.ID, err)
			}
		}

		// Load the child's profile for prompt building.
		profile, err := GetProfileByKidID(db, kidID)
		if err != nil {
			log.Printf("homework: get profile for prompt kid %d: %v", kidID, err)
		}
		if profile == nil {
			profile = &HomeworkProfile{}
		}

		systemPrompt := BuildSystemPrompt(*profile, helpLevel, subjectForPrompt)

		// Load Claude config from the child's own preferences.
		cfg, err := training.LoadClaudeConfig(db, kidID)
		if err != nil {
			log.Printf("homework: load claude config user %d: %v", kidID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Store the user message.
		userMsg, err := AddMessage(db, HomeworkMessage{
			ConversationID: conv.ID,
			Role:           "user",
			Content:        message,
			HelpLevel:      helpLevel,
			ImagePath:      imagePath,
		})
		if err != nil {
			log.Printf("homework: add my user message conv %d: %v", conv.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
			return
		}

		// Get existing session ID for conversation continuity.
		sessionID, err := GetSessionID(db, conv.ID, kidID)
		if err != nil {
			log.Printf("homework: get session ID conv %d: %v", conv.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		// Set up SSE streaming.
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Send the saved user message first.
		userMsgJSON, _ := json.Marshal(userMsg)
		fmt.Fprintf(w, "event: user_message\ndata: %s\n\n", userMsgJSON)
		flusher.Flush()

		// Build Claude CLI command with streaming output.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		fullResponse, newSessionID, err := streamClaude(ctx, cfg, systemPrompt, message, imagePath, sessionID, w, flusher)
		if err != nil && sessionID != "" {
			// Session may have expired — retry without session.
			log.Printf("homework: session resume failed, retrying fresh: %v", err)
			fmt.Fprintf(w, "event: retry\ndata: {\"reason\":\"session expired, retrying\"}\n\n")
			flusher.Flush()
			fullResponse, newSessionID, err = streamClaude(ctx, cfg, systemPrompt, message, imagePath, "", w, flusher)
		}

		if err != nil {
			log.Printf("homework: claude error conv %d: %v", conv.ID, err)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"Claude failed to respond\"}\n\n")
			flusher.Flush()
			return
		}

		// Save session ID for future resumption.
		if newSessionID != "" && newSessionID != sessionID {
			if dbErr := UpdateSessionID(db, conv.ID, kidID, newSessionID); dbErr != nil {
				log.Printf("homework: save session ID conv %d: %v", conv.ID, dbErr)
			}
		}

		// Store the assistant response.
		assistantMsg, err := AddMessage(db, HomeworkMessage{
			ConversationID: conv.ID,
			Role:           "assistant",
			Content:        fullResponse,
			HelpLevel:      helpLevel,
		})
		if err != nil {
			log.Printf("homework: add assistant message conv %d: %v", conv.ID, err)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to save response\"}\n\n")
			flusher.Flush()
			return
		}

		// Send done event with the full saved assistant message.
		assistantJSON, _ := json.Marshal(assistantMsg)
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", assistantJSON)
		flusher.Flush()
	}
}

func homeworkUploadsDir() string {
	if d := os.Getenv("HOMEWORK_UPLOADS_DIR"); d != "" {
		return d
	}
	return filepath.Join("data", "homework-uploads")
}

// execCommand creates an exec.Cmd. Extracted for test substitution.
var execCommand = execCommandImpl

func execCommandImpl(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
