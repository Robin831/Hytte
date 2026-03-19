package training

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

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

	cfg := &ClaudeConfig{
		Enabled: prefs["claude_enabled"] == "true",
		CLIPath: prefs["claude_cli_path"],
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

// RunPrompt sends a prompt to the Claude CLI and returns the text response.
func RunPrompt(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("claude is not enabled")
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

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
