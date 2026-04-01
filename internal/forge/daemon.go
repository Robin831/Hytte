package forge

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// daemonAliveFunc is the default implementation of the daemon liveness check.
// It is assigned to daemonAlive at package init and can be overridden in tests.
var daemonAlive = daemonAliveFunc

func daemonAliveFunc() (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "cannot resolve home directory"
	}
	pidFile := filepath.Join(home, ".forge", "forge.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, "PID file not found"
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, "invalid PID file"
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Sprintf("process %d not found", pid)
	}
	// Signal 0 checks if the process exists without actually sending a signal.
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, fmt.Sprintf("process %d not running", pid)
	}
	return true, ""
}
