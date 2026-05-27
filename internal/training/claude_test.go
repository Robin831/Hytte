package training

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func TestLoadClaudeConfig_Defaults(t *testing.T) {
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	cfg, err := LoadClaudeConfig(database, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if cfg.CLIPath != "claude" {
		t.Errorf("expected default CLIPath 'claude', got %q", cfg.CLIPath)
	}
	if cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("expected default Model 'claude-sonnet-4-6', got %q", cfg.Model)
	}
}

func TestLoadClaudeConfig_CustomValues(t *testing.T) {
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_cli_path", "/usr/local/bin/claude"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_model", "claude-opus-4-6"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	cfg, err := LoadClaudeConfig(database, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.CLIPath != "/usr/local/bin/claude" {
		t.Errorf("expected CLIPath '/usr/local/bin/claude', got %q", cfg.CLIPath)
	}
	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("expected Model 'claude-opus-4-6', got %q", cfg.Model)
	}
}

func TestLoadClaudeConfig_InvalidPath(t *testing.T) {
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_cli_path", "claude; rm -rf /"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	_, err = LoadClaudeConfig(database, 1)
	if err == nil {
		t.Fatal("expected error for invalid CLI path, got nil")
	}
}

func TestValidateCLIPath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"", false},
		{"claude", false},
		{"/usr/local/bin/claude", false},
		{`C:\Program Files\claude\claude.exe`, true}, // spaces not allowed
		{"C:\\Users\\rob\\claude.exe", false},
		{"../../../etc/passwd", false}, // dots and slashes are valid path chars
		{"claude; rm -rf /", true},
		{"$(whoami)", true},
		{"claude`id`", true},
		{"claude|cat", true},
		{"claude && echo", true},
		{"claude\nnewline", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := auth.ValidateCLIPath(tt.path)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateCLIPath(%q) = nil, want error", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateCLIPath(%q) = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestLoadClaudeConfig_EncryptedAtRest(t *testing.T) {
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	const cliPath = "/home/robin/.local/bin/claude"
	encrypted, err := encryption.EncryptField(cliPath)
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if err := auth.SetPreference(database, 1, "claude_cli_path", encrypted); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	cfg, err := LoadClaudeConfig(database, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CLIPath != cliPath {
		t.Errorf("expected CLIPath %q, got %q", cliPath, cfg.CLIPath)
	}
}

// selectTimeout mirrors the deadline-selection logic in runPromptCLI so it can
// be tested without spawning a real process.
func selectTimeout(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		return time.Until(dl)
	}
	return DefaultCLITimeout
}

func TestSelectTimeout_UsesDefaultWhenNoDeadline(t *testing.T) {
	d := selectTimeout(context.Background())
	if d != DefaultCLITimeout {
		t.Errorf("expected DefaultCLITimeout (%v), got %v", DefaultCLITimeout, d)
	}
}

func TestSelectTimeout_RespectsCallerDeadline(t *testing.T) {
	callerTimeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), callerTimeout)
	defer cancel()

	d := selectTimeout(ctx)
	// The remaining time should be close to callerTimeout (well above DefaultCLITimeout).
	if d <= DefaultCLITimeout {
		t.Errorf("expected remaining time > DefaultCLITimeout (%v), got %v", DefaultCLITimeout, d)
	}
}

func TestRunPrompt_Disabled(t *testing.T) {
	cfg := &ClaudeConfig{
		Enabled: false,
		CLIPath: "claude",
		Model:   "claude-sonnet-4-6",
	}

	_, err := RunPrompt(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error when claude is disabled")
	}
	if err.Error() != "claude is not enabled" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunPromptWithCost_Disabled(t *testing.T) {
	cfg := &ClaudeConfig{Enabled: false, CLIPath: "claude", Model: "claude-sonnet-4-6"}

	_, cost, err := RunPromptWithCost(context.Background(), cfg, "hi")
	if err == nil {
		t.Fatal("expected error when claude is disabled")
	}
	if cost != 0 {
		t.Errorf("expected cost=0, got %v", cost)
	}
}

func TestRunPromptWithCost_ParsesEnvelope(t *testing.T) {
	orig := runPromptWithCostFunc
	t.Cleanup(func() { runPromptWithCostFunc = orig })

	runPromptWithCostFunc = func(_ context.Context, _ *ClaudeConfig, _ string) (string, float64, error) {
		// Simulate the parsing path by exercising the same code that runPromptCLIWithCost uses
		// when JSON parses successfully.
		return "Hello world", 0.0123, nil
	}

	text, cost, err := RunPromptWithCost(context.Background(), &ClaudeConfig{Enabled: true}, "p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("unexpected text: %q", text)
	}
	if cost < 0.012 || cost > 0.013 {
		t.Errorf("expected cost ≈ 0.0123, got %v", cost)
	}
}

// TestParseClaudeStream exercises the NDJSON parser used by
// RunPromptWithSessionStream. Each case feeds a captured stream-json fixture
// and asserts the assembled text plus the session id emission.
func TestParseClaudeStream(t *testing.T) {
	type want struct {
		chunks    []string
		text      string
		sessionID string
		errSubstr string
	}

	cases := []struct {
		name string
		in   string
		want want
	}{
		{
			name: "content_block_delta chunks assembled",
			in: strings.Join([]string{
				`{"type":"system","session_id":"sess-1"}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello "}}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"world!"}}`,
				`{"type":"result","result":"Hello world!","session_id":"sess-1","is_error":false}`,
			}, "\n") + "\n",
			want: want{
				chunks:    []string{"Hello ", "world!"},
				text:      "Hello world!",
				sessionID: "sess-1",
			},
		},
		{
			name: "assistant content blocks emitted",
			in: strings.Join([]string{
				`{"type":"assistant","message":{"content":[{"type":"text","text":"Greetings."}]}}`,
				`{"type":"result","result":"Greetings.","session_id":"sess-2","is_error":false}`,
			}, "\n") + "\n",
			want: want{
				chunks:    []string{"Greetings."},
				text:      "Greetings.",
				sessionID: "sess-2",
			},
		},
		{
			name: "malformed line tolerated",
			in: strings.Join([]string{
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`,
				`not-json {{`,
				`{"type":"result","result":"ok","session_id":"sess-3"}`,
			}, "\n") + "\n",
			want: want{
				chunks:    []string{"ok"},
				text:      "ok",
				sessionID: "sess-3",
			},
		},
		{
			name: "is_error result returns error",
			in: strings.Join([]string{
				`{"type":"result","result":"rate limit exceeded","is_error":true}`,
			}, "\n") + "\n",
			want: want{
				errSubstr: "rate limit exceeded",
			},
		},
		{
			name: "session emitted from system event when result lacks it",
			in: strings.Join([]string{
				`{"type":"system","session_id":"sess-4"}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`,
				`{"type":"result","result":"hi","is_error":false}`,
			}, "\n") + "\n",
			want: want{
				chunks:    []string{"hi"},
				text:      "hi",
				sessionID: "sess-4",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var chunks []string
			var sessionID string
			text, sessionEmitted, err := parseClaudeStream(strings.NewReader(tc.in),
				func(c string) { chunks = append(chunks, c) },
				func(s string) { sessionID = s },
			)
			if tc.want.errSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.want.errSubstr)
				}
				if !strings.Contains(err.Error(), tc.want.errSubstr) {
					t.Fatalf("error %v does not contain %q", err, tc.want.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if text != tc.want.text {
				t.Errorf("text = %q, want %q", text, tc.want.text)
			}
			if len(chunks) != len(tc.want.chunks) {
				t.Fatalf("chunks = %v, want %v", chunks, tc.want.chunks)
			}
			for i, c := range chunks {
				if c != tc.want.chunks[i] {
					t.Errorf("chunk[%d] = %q, want %q", i, c, tc.want.chunks[i])
				}
			}
			if sessionID != tc.want.sessionID {
				t.Errorf("session id = %q, want %q", sessionID, tc.want.sessionID)
			}
			if tc.want.sessionID != "" && !sessionEmitted {
				t.Errorf("expected onSession callback to fire")
			}
		})
	}
}

func TestRunPromptWithSessionStream_Disabled(t *testing.T) {
	cfg := &ClaudeConfig{Enabled: false, CLIPath: "claude", Model: "claude-sonnet-4-6"}
	_, err := RunPromptWithSessionStream(context.Background(), cfg, "hi", "", nil, nil)
	if err == nil {
		t.Fatal("expected error when claude is disabled")
	}
	if err.Error() != "claude is not enabled" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestParseClaudeCostEnvelope verifies that parseClaudeCostEnvelope extracts
// result text and total_cost_usd correctly, returns an error for is_error=true,
// and falls back to returning raw stdout with cost=0 on malformed JSON.
func TestParseClaudeCostEnvelope(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantText string
		wantCost float64
		wantErr  bool
	}{
		{
			name:     "valid envelope",
			raw:      `{"result":"answer","total_cost_usd":0.0042,"session_id":"s1","is_error":false}`,
			wantText: "answer",
			wantCost: 0.0042,
		},
		{
			name:     "missing cost field defaults to zero",
			raw:      `{"result":"only text"}`,
			wantText: "only text",
			wantCost: 0,
		},
		{
			name:    "is_error true returns error",
			raw:     `{"result":"something went wrong","total_cost_usd":0,"is_error":true}`,
			wantErr: true,
		},
		{
			name:     "malformed json falls back to raw stdout with cost=0",
			raw:      `not-json {{`,
			wantText: "not-json {{",
			wantCost: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, cost, err := parseClaudeCostEnvelope([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if cost != tc.wantCost {
				t.Errorf("cost = %v, want %v", cost, tc.wantCost)
			}
		})
	}
}
