package training

import (
	"context"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
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
