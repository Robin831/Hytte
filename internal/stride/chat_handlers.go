package stride

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// execCommand creates an exec.Cmd. Extracted for test substitution.
var execCommand = execCommandImpl

func execCommandImpl(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// claudeStreamLine represents a line from Claude CLI's stream-json output.
type claudeStreamLine struct {
	Type      string `json:"type"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// extractPlanJSON scans the response for the last fenced ```json ... ``` block
// containing a JSON array. Using the last block means Claude can show a
// "before" version and an "after" version and we always act on the final one.
// The extraction finds fence boundaries first and takes the full fenced content,
// so JSON strings containing ']' are handled correctly. Both \n and \r\n line
// endings are supported.
func extractPlanJSON(response string) (string, bool) {
	// Find the last closing fence (``` on its own line, \r?\n```).
	const fence = "```"
	lastClose := strings.LastIndex(response, "\n"+fence)
	if lastClose < 0 {
		return "", false
	}
	// Find the opening fence that precedes the closing one.
	openStart := strings.LastIndex(response[:lastClose], fence)
	if openStart < 0 {
		return "", false
	}
	// Skip past the opening fence and optional language tag to the newline that
	// ends the opening fence line.
	afterTag := response[openStart+len(fence):]
	nl := strings.IndexByte(afterTag, '\n')
	if nl < 0 {
		return "", false
	}
	// innerStart is the absolute position in response of the first content line.
	innerStart := openStart + len(fence) + nl + 1
	// innerEnd is lastClose (the \n before closing fence); strip trailing \r too.
	innerEnd := lastClose
	if innerEnd > innerStart && response[innerEnd-1] == '\r' {
		innerEnd--
	}
	if innerEnd <= innerStart {
		return "", false
	}
	inner := strings.TrimSpace(response[innerStart:innerEnd])
	if !strings.HasPrefix(inner, "[") {
		return "", false
	}
	return inner, true
}

// validatePlanUpdate parses the extracted plan JSON and verifies it covers
// exactly the 7 days of the given week range with no duplicates. Returns the
// validated []DayPlan or a descriptive error.
func validatePlanUpdate(planJSON string, weekStart, weekEnd string) ([]DayPlan, error) {
	days, err := parsePlanResponse(planJSON, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("validate plan update: %w", err)
	}
	return days, nil
}

// StrideChatListHandler returns all messages for a plan's chat conversation.
// GET /api/stride/plans/{planId}/chat
func StrideChatListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		planIDStr := chi.URLParam(r, "planId")
		planID, err := strconv.ParseInt(planIDStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan ID"})
			return
		}

		// Verify plan exists and belongs to user.
		_, err = GetPlanByID(db, planID, user.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
				return
			}
			log.Printf("stride chat: get plan %d: %v", planID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get plan"})
			return
		}

		msgs, err := ListChatMessages(db, planID, user.ID)
		if err != nil {
			log.Printf("stride chat: list messages plan %d: %v", planID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list messages"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"messages": msgs,
			"plan_id":  planID,
		})
	}
}

// StrideChatSendHandler accepts a user message and streams Claude's response
// via SSE with plan modification detection.
// POST /api/stride/plans/{planId}/chat
func StrideChatSendHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		planIDStr := chi.URLParam(r, "planId")
		planID, err := strconv.ParseInt(planIDStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan ID"})
			return
		}

		// Parse request body.
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content must not be empty"})
			return
		}

		// Verify plan exists and belongs to user.
		plan, err := GetPlanByID(db, planID, user.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
				return
			}
			log.Printf("stride chat: get plan %d: %v", planID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get plan"})
			return
		}

		// Load Claude config.
		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("stride chat: load claude config user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		// Store user message.
		userMsg, err := AddChatMessage(db, ChatMessage{
			PlanID:  planID,
			UserID:  user.ID,
			Role:    "user",
			Content: body.Content,
		})
		if err != nil {
			log.Printf("stride chat: add user message plan %d: %v", planID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
			return
		}

		// Get existing session ID for conversation continuity.
		sessionID, err := GetChatSessionID(db, planID, user.ID)
		if err != nil {
			log.Printf("stride chat: get session ID plan %d: %v", planID, err)
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

		// Stream Claude response.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		// Load context for the system prompt.
		systemPrompt := buildChatContext(ctx, db, user.ID, *plan)

		fullResponse, newSessionID, err := streamChatClaude(ctx, cfg, systemPrompt, body.Content, sessionID, w, flusher)
		if err != nil && sessionID != "" {
			// Session may have expired — retry without session.
			log.Printf("stride chat: session resume failed, retrying fresh: %v", err)
			fmt.Fprintf(w, "event: retry\ndata: {\"reason\":\"session expired, retrying\"}\n\n")
			flusher.Flush()
			fullResponse, newSessionID, err = streamChatClaude(ctx, cfg, systemPrompt, body.Content, "", w, flusher)
		}

		if err != nil {
			log.Printf("stride chat: claude error plan %d: %v", planID, err)
			errJSON, _ := json.Marshal(map[string]string{"error": "Claude failed to respond. Please try again."})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()
			return
		}

		// Save session ID for future resumption.
		if newSessionID != "" && newSessionID != sessionID {
			if dbErr := UpdateChatSessionID(db, planID, user.ID, newSessionID); dbErr != nil {
				log.Printf("stride chat: save session ID plan %d: %v", planID, dbErr)
			}
		}

		// Check for plan modification in the response.
		planModified := false
		if planJSON, found := extractPlanJSON(fullResponse); found {
			days, err := validatePlanUpdate(planJSON, plan.WeekStart, plan.WeekEnd)
			if err != nil {
				log.Printf("stride chat: plan update validation failed plan %d msg %d: %v", planID, userMsg.ID, err)
			} else {
				// Update plan_json in the database.
				updatedJSON, err := json.Marshal(days)
				if err != nil {
					log.Printf("stride chat: marshal updated plan: %v", err)
				} else if err := updatePlanJSON(db, planID, user.ID, string(updatedJSON)); err != nil {
					log.Printf("stride chat: update plan_json plan %d: %v", planID, err)
				} else {
					planModified = true
					// Send plan_updated SSE event so the frontend can re-render.
					planEvt, _ := json.Marshal(map[string]any{"plan": days})
					fmt.Fprintf(w, "event: plan_updated\ndata: %s\n\n", planEvt)
					flusher.Flush()
				}
			}
		}

		// Store assistant message.
		assistantMsg, err := AddChatMessage(db, ChatMessage{
			PlanID:  planID,
			UserID:  user.ID,
			Role:    "assistant",
			Content: fullResponse,
		})
		if err != nil {
			log.Printf("stride chat: add assistant message plan %d: %v", planID, err)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to save response\"}\n\n")
			flusher.Flush()
			return
		}

		// Mark as plan-modified if applicable.
		if planModified {
			if err := MarkMessagePlanModified(db, assistantMsg.ID, user.ID); err != nil {
				log.Printf("stride chat: mark plan_modified msg %d: %v", assistantMsg.ID, err)
			}
			assistantMsg.PlanModified = true
		}

		// Send done event.
		doneJSON, _ := json.Marshal(assistantMsg)
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", doneJSON)
		flusher.Flush()
	}
}

// buildChatContext loads all context needed for the chat system prompt.
// ctx is threaded through to DB calls so they respect request cancellation.
func buildChatContext(ctx context.Context, db *sql.DB, userID int64, plan Plan) string {
	profile := training.BuildUserTrainingProfile(db, userID)

	pid := plan.ID
	evaluations, err := ListEvaluations(db, userID, &pid, nil)
	if err != nil {
		log.Printf("stride chat: list evaluations plan %d: %v", plan.ID, err)
	}

	allRaces, err := ListRaces(db, userID)
	if err != nil {
		log.Printf("stride chat: list races: %v", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	var races []Race
	for _, r := range allRaces {
		if r.Date >= today && r.ResultTime == nil {
			races = append(races, r)
		}
	}

	acr, acute, chronic, acrErr := training.ComputeACR(db, userID, time.Now().UTC())
	if acrErr != nil {
		log.Printf("stride chat: compute ACR user %d: %v", userID, acrErr)
	}

	notes, err := listUnconsumedNotes(ctx, db, userID)
	if err != nil {
		log.Printf("stride chat: list notes: %v", err)
	}

	return BuildChatSystemPrompt(profile, plan, evaluations, races, acr, acute, chronic, notes)
}

// streamChatClaude runs the Claude CLI with stream-json output and sends text
// deltas as SSE events. Returns the full response text and session ID.
func streamChatClaude(ctx context.Context, cfg *training.ClaudeConfig, systemPrompt, prompt, sessionID string, w http.ResponseWriter, flusher http.Flusher) (string, string, error) {
	args := []string{"--model", cfg.Model, "-p", "-", "--output-format", "stream-json", "--verbose", "--system-prompt", systemPrompt}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := execCommand(ctx, cfg.CLIPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

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
			for _, block := range ev.Message.Content {
				if block.Type == "text" && block.Text != "" {
					fullText.WriteString(block.Text)
					data, _ := json.Marshal(map[string]string{"text": block.Text})
					fmt.Fprintf(w, "event: delta\ndata: %s\n\n", data)
					flusher.Flush()
				}
			}
		case "content_block_delta":
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
				stderr := strings.TrimSpace(stderrBuf.String())
				if stderr != "" {
					return "", "", fmt.Errorf("claude returned error: %s: %s", ev.Result, stderr)
				}
				return "", "", fmt.Errorf("claude returned error: %s", ev.Result)
			}
			if ev.Result != "" {
				fullText.Reset()
				fullText.WriteString(ev.Result)
			}
			resultSessionID = ev.SessionID
		}
	}

	if err := scanner.Err(); err != nil {
		cmd.Wait()
		if stderr := strings.TrimSpace(stderrBuf.String()); stderr != "" {
			return "", "", fmt.Errorf("scan claude output: %w: %s", err, stderr)
		}
		return "", "", fmt.Errorf("scan claude output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if stderr := strings.TrimSpace(stderrBuf.String()); stderr != "" {
			return "", "", fmt.Errorf("claude exit: %w: %s", err, stderr)
		}
		return "", "", fmt.Errorf("claude exit: %w", err)
	}

	return strings.TrimSpace(fullText.String()), resultSessionID, nil
}

// updatePlanJSON updates the plan_json column for a plan. Scoped to userID.
func updatePlanJSON(db *sql.DB, planID, userID int64, planJSON string) error {
	res, err := db.Exec(`UPDATE stride_plans SET plan_json = ? WHERE id = ? AND user_id = ?`, planJSON, planID, userID)
	if err != nil {
		return fmt.Errorf("update plan_json: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
