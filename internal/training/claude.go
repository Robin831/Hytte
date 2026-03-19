package training

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// validCLIPathRe matches safe CLI paths: alphanumeric, slashes, backslashes,
// dots, hyphens, underscores, colons (for Windows drive letters). No shell
// metacharacters, spaces, or other characters that could cause trouble.
var validCLIPathRe = regexp.MustCompile(`^[a-zA-Z0-9._/\\:-]+$`)

// ValidateCLIPath checks that a CLI path contains only safe characters.
// Empty string is valid (means "use default").
func ValidateCLIPath(path string) error {
	if path == "" {
		return nil
	}
	if !validCLIPathRe.MatchString(path) {
		return fmt.Errorf("invalid CLI path: only alphanumeric characters, slashes, dots, hyphens, underscores, and colons are allowed")
	}
	return nil
}

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
	if err := ValidateCLIPath(cliPath); err != nil {
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
