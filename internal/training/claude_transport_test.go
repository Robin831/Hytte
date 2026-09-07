package training

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// runFailing runs a shell snippet that is expected to fail and returns the
// error exec produced, so the classification is tested against the real
// *exec.ExitError shapes rather than a hand-built stand-in.
func runFailing(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", script).Run()
	if err == nil {
		t.Fatalf("script %q unexpectedly succeeded", script)
	}
	return err
}

func TestClassifyCLIError(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	exitErr := runFailing(t, "exit 3")
	killedErr := runFailing(t, "kill -9 $$")

	tests := []struct {
		name      string
		ctx       context.Context
		err       error
		stderr    string
		transport bool
	}{
		{"killed by a signal with nothing on stderr", context.Background(), killedErr, "", true},
		{"non-zero exit with nothing on stderr", context.Background(), exitErr, "", true},
		{"non-zero exit with whitespace-only stderr", context.Background(), exitErr, "  \n", true},
		{"non-zero exit that reported a reason", context.Background(), exitErr, "API error: overloaded", false},
		{"our own deadline did the killing", cancelled, killedErr, "", false},
		{"the binary never started", context.Background(), &exec.Error{Name: "claude", Err: errors.New("not found")}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCLIError(tt.ctx, tt.err, tt.stderr)
			if IsCLITransportError(got) != tt.transport {
				t.Errorf("IsCLITransportError = %v, want %v (err = %v)", !tt.transport, tt.transport, got)
			}
			// Whatever the classification, the message and the wrapped cause
			// callers already rely on must be untouched.
			want := "claude CLI error: " + tt.err.Error() + ": " + tt.stderr
			if got.Error() != want {
				t.Errorf("Error() = %q, want %q", got.Error(), want)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("error %v no longer unwraps to the exec failure", got)
			}
		})
	}
}

func TestIsCLITransportErrorOnUnrelatedErrors(t *testing.T) {
	if IsCLITransportError(nil) {
		t.Error("a nil error must not read as a transport error")
	}
	if IsCLITransportError(errors.New("claude returned error: overloaded")) {
		t.Error("a plain API error must not read as a transport error")
	}
	wrapped := errors.New("outer")
	transport := &CLITransportError{Err: errors.New("claude CLI error: signal: killed: ")}
	if !IsCLITransportError(transport) {
		t.Error("a transport error must read as one")
	}
	if IsCLITransportError(wrapped) {
		t.Error("an unrelated error must not read as a transport error")
	}
	if !strings.Contains(transport.Error(), "signal: killed") {
		t.Errorf("Error() = %q, want it to carry the underlying message", transport.Error())
	}
}

// stubStreamExec replaces the streaming path's exec seam with a shell snippet,
// so the classification is exercised against a real subprocess death rather
// than a synthesised error.
func stubStreamExec(t *testing.T, script string) {
	t.Helper()
	orig := streamExecCommand
	streamExecCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
	t.Cleanup(func() { streamExecCommand = orig })
}

// The streaming path classifies the same failures as the one-shot paths — the
// chat session that died with "claude exit: signal: killed" during a CLI
// auto-update is transport-level — while keeping its own message wording.
func TestRunPromptWithSessionStreamClassifiesTransportFailures(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		transport bool
		wantMsg   string
	}{
		{"killed by a signal with nothing on stderr", "kill -9 $$", true, "claude exit: signal: killed"},
		{"non-zero exit with nothing on stderr", "exit 3", true, "claude exit: exit status 3"},
		{"non-zero exit that reported a reason", "echo 'API error: overloaded' >&2; exit 1", false,
			"claude exit: exit status 1: API error: overloaded"},
	}

	cfg := &ClaudeConfig{Enabled: true, CLIPath: "claude", Model: "claude-sonnet-4-6"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubStreamExec(t, tt.script)

			_, err := runPromptWithSessionStreamCLI(context.Background(), cfg, "hi", "", nil, nil)
			if err == nil {
				t.Fatal("expected the failed CLI call to return an error")
			}
			if IsCLITransportError(err) != tt.transport {
				t.Errorf("IsCLITransportError = %v, want %v (err = %v)", !tt.transport, tt.transport, err)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
