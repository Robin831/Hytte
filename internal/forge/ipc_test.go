package forge

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// startFakeServer creates a Unix socket listener at sockPath and launches a
// goroutine that accepts one connection, reads the command, and writes reply.
// The server closes the connection after sending reply, so io.ReadAll terminates.
func startFakeServer(t *testing.T, sockPath string, reply string) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		defer conn.Close()
		buf := make([]byte, 256)
		conn.Read(buf) //nolint:errcheck // test helper, ignore read result
		fmt.Fprint(conn, reply)
	}()
}

func tempSocketPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.sock")
}

func clientAt(t *testing.T, sockPath string) *Client {
	t.Helper()
	t.Setenv("FORGE_IPC_SOCKET", sockPath)
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestSendCommand_success(t *testing.T) {
	sock := tempSocketPath(t)
	startFakeServer(t, sock, "pong")

	c := clientAt(t, sock)
	resp, err := c.SendCommand("ping")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if string(resp) != "pong" {
		t.Fatalf("expected pong, got %q", resp)
	}
}

func TestSendCommand_noServer(t *testing.T) {
	sock := tempSocketPath(t)
	// Do not start a server — dial should fail.
	c := clientAt(t, sock)
	_, err := c.SendCommand("ping")
	if err == nil {
		t.Fatal("expected error when no server is listening")
	}
}

func TestSendCommand_overflow(t *testing.T) {
	sock := tempSocketPath(t)
	// Reply is ipcMaxResponse+1 bytes, which should trigger the overflow error.
	bigReply := strings.Repeat("x", int(ipcMaxResponse)+1)
	startFakeServer(t, sock, bigReply)

	c := clientAt(t, sock)
	_, err := c.SendCommand("ping")
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestHealth_alive(t *testing.T) {
	sock := tempSocketPath(t)
	startFakeServer(t, sock, "pong")

	c := clientAt(t, sock)
	if err := c.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestHealth_dead(t *testing.T) {
	sock := tempSocketPath(t)
	c := clientAt(t, sock)
	if err := c.Health(); err == nil {
		t.Fatal("expected error from Health when daemon is not running")
	}
}

func TestNewClient_defaultPath(t *testing.T) {
	t.Setenv("FORGE_IPC_SOCKET", "")
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if !strings.HasSuffix(c.socketPath, filepath.Join(".forge", "forge.sock")) {
		t.Fatalf("unexpected default socket path: %s", c.socketPath)
	}
}

func TestNewClient_envOverride(t *testing.T) {
	t.Setenv("FORGE_IPC_SOCKET", "/tmp/custom.sock")
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.socketPath != "/tmp/custom.sock" {
		t.Fatalf("expected /tmp/custom.sock, got %s", c.socketPath)
	}
}
