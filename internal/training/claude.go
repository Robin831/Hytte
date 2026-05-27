package training

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
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
	IsError      bool    `json:"is_error"`
}

// parseClaudeCostEnvelope parses raw JSON from the claude CLI --output-format=json
// envelope. On success it returns the result text and cost. If is_error is set in
// a valid envelope, it returns an error. On JSON parse failure it logs once (via
// costParseLogOnce) and returns the raw bytes as text with cost=0.
func parseClaudeCostEnvelope(raw []byte) (string, float64, error) {
	var env claudeCostEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		costParseLogOnce.Do(func() {
			log.Printf("training: failed to parse claude JSON envelope, falling back to raw stdout (cost=0): %v", err)
		})
		return strings.TrimSpace(string(raw)), 0, nil
	}
	if env.IsError {
		return "", 0, fmt.Errorf("claude returned error: %s", env.Result)
	}
	text := env.Result
	if text == "" {
		text = string(raw)
	}
	return strings.TrimSpace(text), env.TotalCostUSD, nil
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

	return parseClaudeCostEnvelope(stdout.Bytes())
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

// runPromptWithImageFn is the seam used by RunPromptWithImage. Tests override
// this to stub out the CLI invocation.
var runPromptWithImageFn = runPromptCLIWithImage

// RunPromptWithImage sends a prompt plus a single image file path to the Claude
// CLI and returns the text response. The image is referenced inside the prompt
// so Claude's built-in Read tool can view it — this works around the fact that
// `claude -p` in v2.1.x does not expose a dedicated `--image` flag. To allow
// the Read tool to access the image, the directory containing it is granted
// via `--add-dir` and tool permissions are bypassed for the call.
//
// Security note: Read access is granted to filepath.Dir(imagePath), so callers
// MUST place the image inside a dedicated per-call directory (e.g. via
// os.MkdirTemp) that contains no other files. Passing a path whose parent is
// a shared location like /tmp would expose every file in that directory to
// Claude. The caller is responsible for removing the directory after this
// returns.
func RunPromptWithImage(ctx context.Context, cfg *ClaudeConfig, prompt, imagePath string) (string, error) {
	return runPromptWithImageFn(ctx, cfg, prompt, imagePath)
}

func runPromptCLIWithImage(ctx context.Context, cfg *ClaudeConfig, prompt, imagePath string) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("claude is not enabled")
	}
	if imagePath == "" {
		return "", fmt.Errorf("image path is required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCLITimeout)
		defer cancel()
	}

	dir := filepath.Dir(imagePath)
	augmented := fmt.Sprintf("Read the image at %s, then answer:\n\n%s", imagePath, prompt)

	cmd := exec.CommandContext(ctx, cfg.CLIPath,
		"--model", cfg.Model,
		"-p", "-",
		"--output-format", "json",
		"--add-dir", dir,
		"--allowedTools", "Read",
		"--permission-mode", "bypassPermissions",
	)
	cmd.Stdin = strings.NewReader(augmented)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude CLI error: %w: %s", err, stderr.String())
	}

	text, _, err := parseClaudeCostEnvelope(stdout.Bytes())
	return text, err
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

// claudeStreamLine mirrors the subset of the Claude CLI
// --output-format=stream-json envelope that the streaming chat path consumes.
// Only the fields needed to extract incremental text deltas and the resolved
// session id are decoded.
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

// runPromptWithSessionStreamFn is the seam used by RunPromptWithSessionStream.
// Tests override this to feed a scripted stream without spawning the CLI.
var runPromptWithSessionStreamFn = runPromptWithSessionStreamCLI

// RunPromptWithSessionStream invokes the Claude CLI with
// --output-format=stream-json --verbose so callers can render assistant text
// progressively. Each text delta is delivered to onChunk in the order the CLI
// emits it; the resolved session id is delivered to onSession exactly once
// when the CLI reports it (which may differ from sessionID on a fresh
// conversation). Both callbacks are optional — pass nil to ignore.
//
// The caller's context controls the subprocess lifetime: cancelling ctx kills
// the CLI. Returns the trimmed assistant text on success.
func RunPromptWithSessionStream(ctx context.Context, cfg *ClaudeConfig, prompt, sessionID string, onChunk func(string), onSession func(string)) (string, error) {
	return runPromptWithSessionStreamFn(ctx, cfg, prompt, sessionID, onChunk, onSession)
}

// streamExecCommand creates an exec.Cmd for RunPromptWithSessionStream.
// Extracted as a variable so tests can substitute a fake command that emits
// a scripted stream-json sequence on stdout.
var streamExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func runPromptWithSessionStreamCLI(ctx context.Context, cfg *ClaudeConfig, prompt, sessionID string, onChunk func(string), onSession func(string)) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("claude is not enabled")
	}

	// Apply a default timeout when the caller has not set a deadline,
	// consistent with runPromptCLIWithCost and runPromptCLIWithImage, so the
	// Claude CLI subprocess cannot run indefinitely if the caller forgets to
	// add a timeout.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCLITimeout)
		defer cancel()
	}

	args := []string{"--model", cfg.Model, "-p", "-", "--output-format", "stream-json", "--verbose"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := streamExecCommand(ctx, cfg.CLIPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start claude: %w", err)
	}

	fullText, _, parseErr := parseClaudeStream(stdout, onChunk, onSession)
	if parseErr != nil {
		// Kill the subprocess first so it stops writing, then drain any
		// remaining bytes from stdout so the pipe buffer cannot block the
		// process (and therefore cmd.Wait()) from exiting.
		_ = cmd.Process.Kill()
		go func() { _, _ = io.Copy(io.Discard, stdout) }()
		_ = cmd.Wait()
		return "", parseErr
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if stderr := strings.TrimSpace(stderrBuf.String()); stderr != "" {
			return "", fmt.Errorf("claude exit: %w: %s", err, stderr)
		}
		return "", fmt.Errorf("claude exit: %w", err)
	}

	return strings.TrimSpace(fullText), nil
}

// parseClaudeStream reads NDJSON events from r, invokes onChunk for each text
// delta in emission order, and invokes onSession once when the CLI reports a
// session id. The returned string is the assistant text assembled from
// streamed deltas (or the authoritative result envelope when present). The
// session-emitted boolean reports whether onSession was actually called so
// callers can detect malformed streams that never produced a session id.
//
// Malformed lines are skipped silently — the CLI occasionally interleaves
// status messages that the chat path does not need.
func parseClaudeStream(r io.Reader, onChunk func(string), onSession func(string)) (string, bool, error) {
	var fullText strings.Builder
	var sessionEmitted bool

	scanner := bufio.NewScanner(r)
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
					if onChunk != nil {
						onChunk(block.Text)
					}
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
				if onChunk != nil {
					onChunk(delta.Delta.Text)
				}
			}
		case "result":
			if ev.IsError {
				if ev.Result != "" {
					return "", sessionEmitted, fmt.Errorf("claude returned error: %s", ev.Result)
				}
				return "", sessionEmitted, fmt.Errorf("claude returned error")
			}
			if ev.Result != "" {
				// The result envelope is the authoritative assembled text.
				fullText.Reset()
				fullText.WriteString(ev.Result)
			}
			if ev.SessionID != "" && !sessionEmitted {
				sessionEmitted = true
				if onSession != nil {
					onSession(ev.SessionID)
				}
			}
		case "system":
			// Some CLI versions emit the session id on the initial system event.
			if ev.SessionID != "" && !sessionEmitted {
				sessionEmitted = true
				if onSession != nil {
					onSession(ev.SessionID)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", sessionEmitted, fmt.Errorf("scan claude output: %w", err)
	}

	return fullText.String(), sessionEmitted, nil
}
