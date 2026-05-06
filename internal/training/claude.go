package training

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
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
// Existing callers and tests continue to use this seam; cost-aware callers
// should use RunPromptWithCost instead.
var runPromptFunc = runPromptCLI

// runPromptWithCostFunc is the cost-aware seam used by RunPromptWithCost.
// Override in tests when the caller needs to observe or stub the cost value.
var runPromptWithCostFunc = runPromptCLIWithCost

// costParseLogOnce ensures the cost-parse-failure warning is logged at most once
// per process so a malformed CLI envelope cannot flood the logs.
var costParseLogOnce sync.Once

// RunPrompt sends a prompt to the Claude CLI and returns the text response.
func RunPrompt(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
	return runPromptFunc(ctx, cfg, prompt)
}

// RunPromptWithCost sends a prompt to the Claude CLI using --output-format=json
// and returns the text response along with the cost in USD parsed from the
// envelope. If the JSON envelope cannot be parsed (or total_cost_usd is
// missing), the cost falls back to 0 and a single warning is logged via
// sync.Once — the call itself still succeeds with the raw stdout as text.
// CLI invocation errors are propagated as err.
func RunPromptWithCost(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, float64, error) {
	return runPromptWithCostFunc(ctx, cfg, prompt)
}

func runPromptCLI(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
	text, _, err := runPromptCLIWithCost(ctx, cfg, prompt)
	return text, err
}

// claudeCostEnvelope mirrors the subset of the claude --output-format=json
// envelope that the cost-aware path consumes.
type claudeCostEnvelope struct {
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

func runPromptCLIWithCost(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, float64, error) {
	if !cfg.Enabled {
		return "", 0, fmt.Errorf("claude is not enabled")
	}

	// Only apply a default timeout if the caller hasn't set a deadline.
	// Callers like the chat handler set longer timeouts for multi-turn conversations.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCLITimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, cfg.CLIPath, "--model", cfg.Model, "-p", "-", "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("claude CLI error: %w: %s", err, stderr.String())
	}

	raw := stdout.Bytes()
	var env claudeCostEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		costParseLogOnce.Do(func() {
			log.Printf("training: failed to parse claude JSON envelope, falling back to raw stdout (cost=0): %v", err)
		})
		return strings.TrimSpace(string(raw)), 0, nil
	}

	text := env.Result
	if text == "" {
		text = string(raw)
	}
	return strings.TrimSpace(text), env.TotalCostUSD, nil
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

// RunPromptWithSession sends a prompt to the Claude CLI with optional session resumption.
// If sessionID is empty, starts a new session. If non-empty, resumes the given session.
// Returns the response text and the session ID for future resumption.
func RunPromptWithSession(ctx context.Context, cfg *ClaudeConfig, prompt, sessionID string) (*SessionResult, error) {
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
