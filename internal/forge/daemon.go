package forge

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// signalDaemon writes a command to the forge daemon socket without waiting for
// a response. Unlike the full IPC round-trip (Client.SendCommand), this avoids
// the 5-second read timeout that caused dashboard mutations to hang when the
// daemon was slow to respond (see Hytte-e535).
func signalDaemon(command string) error {
	if command == "" {
		return fmt.Errorf("forge: command must not be empty")
	}
	if strings.ContainsAny(command, "\r\n") {
		return fmt.Errorf("forge: command must not contain newline characters")
	}

	socketPath := os.Getenv("FORGE_IPC_SOCKET")
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("forge: resolve home directory: %w", err)
		}
		socketPath = filepath.Join(home, ".forge", "forge.sock")
	}

	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return fmt.Errorf("forge: dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("forge: set write deadline: %w", err)
	}
	if _, err := fmt.Fprintf(conn, "%s\n", command); err != nil {
		return fmt.Errorf("forge: send command: %w", err)
	}
	return nil
}

// prActionCommand is the JSON IPC payload for PR actions sent to the forge daemon.
type prActionCommand struct {
	PRAction string `json:"pr_action"`
	ID       int    `json:"id"`
	Branch   string `json:"branch,omitempty"`
}

// signalDaemonPRAction sends a JSON pr_action command to the forge daemon socket.
// The id is the database PR ID and must be non-zero.
// Actions that operate on a branch (burnish, quench, rebase) must include the
// branch name; other actions (merge, approve_as_is, bellows, close) omit it.
func signalDaemonPRAction(action string, id int, branch string) error {
	switch action {
	case "burnish", "quench", "rebase":
		if branch == "" {
			return fmt.Errorf("forge: action %q requires a branch name", action)
		}
	}
	cmd := prActionCommand{PRAction: action, ID: id, Branch: branch}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("forge: marshal pr_action: %w", err)
	}
	return signalDaemon(string(data))
}

// daemonAliveFunc is the default implementation of the daemon liveness check.
// It is assigned to daemonAlive at package init and can be overridden in tests.
var daemonAlive = daemonAliveFunc

func daemonAliveFunc() (bool, string) {
	socketPath := os.Getenv("FORGE_IPC_SOCKET")
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, "cannot resolve home directory"
		}
		socketPath = filepath.Join(home, ".forge", "forge.sock")
	}

	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return false, fmt.Sprintf("daemon socket not reachable: %v", err)
	}
	conn.Close()
	return true, ""
}
