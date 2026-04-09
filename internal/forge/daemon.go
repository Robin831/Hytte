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

// ipcCommand is the JSON structure expected by the forge daemon IPC socket.
type ipcCommand struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// prActionPayload is the payload for a "pr_action" IPC command.
type prActionPayload struct {
	PRID     int    `json:"pr_id"`
	PRNumber int    `json:"pr_number"`
	Anvil    string `json:"anvil"`
	BeadID   string `json:"bead_id"`
	Branch   string `json:"branch"`
	Action   string `json:"action"`
}

// retryBeadPayload is the payload for a "retry_bead" IPC command.
type retryBeadPayload struct {
	BeadID string `json:"bead_id"`
	Anvil  string `json:"anvil"`
	PRID   int    `json:"pr_id"`
}

// dismissBeadPayload is the payload for a "dismiss_bead" IPC command.
type dismissBeadPayload struct {
	BeadID string `json:"bead_id"`
	Anvil  string `json:"anvil"`
	PRID   int    `json:"pr_id"`
}

// approveAsIsPayload is the payload for an "approve_as_is" IPC command.
type approveAsIsPayload struct {
	BeadID string `json:"bead_id"`
	Anvil  string `json:"anvil"`
}

// forceSmithPayload is the payload for a "force_smith" IPC command.
type forceSmithPayload struct {
	BeadID   string `json:"bead_id"`
	Anvil    string `json:"anvil"`
	UserNote string `json:"user_note"`
}

// wardenRerunPayload is the payload for a "warden_rerun" IPC command.
type wardenRerunPayload struct {
	BeadID string `json:"bead_id"`
	Anvil  string `json:"anvil"`
}

// sendIPCCommand sends a structured JSON command to the forge daemon socket.
// Like signalDaemon, this is fire-and-forget — it does not wait for a response
// to avoid the timeout issues described in Hytte-e535.
func sendIPCCommand(cmdType string, payload any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("forge: marshal payload: %w", err)
	}
	cmd := ipcCommand{
		Type:    cmdType,
		Payload: payloadBytes,
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("forge: marshal command: %w", err)
	}
	if strings.ContainsAny(string(data), "\r\n") {
		return fmt.Errorf("forge: serialised command must not contain newline characters")
	}
	return signalDaemon(string(data))
}

// signalDaemon writes a raw line to the forge daemon socket without waiting for
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
