package training

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// DefaultCLITimeout is the fallback timeout applied when the caller has not set
// a deadline on the context. Callers that need a longer window (e.g. multi-turn
// chat) should set their own deadline before calling RunPrompt.
const DefaultCLITimeout = 120 * time.Second

// ClaudeConfig holds the Claude CLI configuration for a user.
type ClaudeConfig struct {
	Enabled bool
	CLIPath string // path to claude binary
	Model   string // e.g. "claude-sonnet-4-6"
}

// LoadClaudeConfig reads Claude settings from user_preferences.
func LoadClaudeConfig(db *sql.DB, userID int64) (*ClaudeConfig, error) {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return nil, fmt.Errorf("loading preferences: %w", err)
	}

	cliPath := prefs["claude_cli_path"]
	if cliPath != "" {
		decrypted, decErr := encryption.DecryptField(cliPath)
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt claude_cli_path: %w", decErr)
		}
		cliPath = decrypted
	}
	if err := auth.ValidateCLIPath(cliPath); err != nil {
		return nil, err
	}

	cfg := &ClaudeConfig{
		Enabled: prefs["claude_enabled"] == "true",
		CLIPath: cliPath,
		Model:   prefs["claude_model"],
	}

	if cfg.CLIPath == "" {
		cfg.CLIPath = "claude"
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}

	return cfg, nil
}

// runPromptFunc is the function used to run prompts. Override in tests.
var runPromptFunc = runPromptCLI

// RunPrompt sends a prompt to the Claude CLI and returns the text response.
func RunPrompt(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
	return runPromptFunc(ctx, cfg, prompt)
}

func runPromptCLI(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("claude is not enabled")
	}

	// Only apply a default timeout if the caller hasn't set a deadline.
	// Callers like the chat handler set longer timeouts for multi-turn conversations.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCLITimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, cfg.CLIPath, "--model", cfg.Model, "-p", "-", "--output-format", "text")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude CLI error: %w: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// SessionResult holds the response text and session ID from a Claude CLI call.
type SessionResult struct {
	Response  string
	SessionID string
}

// claudeJSONResponse is the JSON structure returned by claude --output-format json.
type claudeJSONResponse struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
}

// runPromptWithSessionFunc is the function used to run session-aware prompts. Override in tests.
var runPromptWithSessionFunc = runPromptWithSessionCLI

// RunPromptWithSession sends a prompt to the Claude CLI with optional session resumption.
// If sessionID is empty, starts a new session. If non-empty, resumes the given session.
// Returns the response text and the session ID for future resumption.
func RunPromptWithSession(ctx context.Context, cfg *ClaudeConfig, prompt, sessionID string) (*SessionResult, error) {
	return runPromptWithSessionFunc(ctx, cfg, prompt, sessionID)
}

func runPromptWithSessionCLI(ctx context.Context, cfg *ClaudeConfig, prompt, sessionID string) (*SessionResult, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("claude is not enabled")
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	args := []string{"--model", cfg.Model, "-p", "-", "--output-format", "json"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(ctx, cfg.CLIPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude CLI error: %w: %s", err, stderr.String())
	}

	var resp claudeJSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse claude JSON response: %w", err)
	}

	if resp.IsError {
		return nil, fmt.Errorf("claude returned error: %s", resp.Result)
	}

	return &SessionResult{
		Response:  strings.TrimSpace(resp.Result),
		SessionID: resp.SessionID,
	}, nil
}
