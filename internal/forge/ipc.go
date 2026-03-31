package forge

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	ipcDialTimeout  = 3 * time.Second
	ipcReadTimeout  = 5 * time.Second
	ipcWriteTimeout = 5 * time.Second
	ipcMaxResponse  = 1 << 20 // 1 MiB
)

// Client is a Unix IPC client for communicating with the forge daemon.
type Client struct {
	socketPath string
}

// NewClient returns a Client using the socket path from the FORGE_IPC_SOCKET
// environment variable, falling back to ~/.forge/forge.sock.
func NewClient() (*Client, error) {
	path := os.Getenv("FORGE_IPC_SOCKET")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("forge: resolve home directory: %w", err)
		}
		path = filepath.Join(home, ".forge", "forge.sock")
	}
	return &Client{socketPath: path}, nil
}

// SendCommand dials the forge daemon socket, sends cmd followed by a newline,
// reads the response, and returns it. The connection is closed after each call.
func (c *Client) SendCommand(cmd string) ([]byte, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, ipcDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("forge: dial %s: %w", c.socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(ipcWriteTimeout)); err != nil {
		return nil, fmt.Errorf("forge: set write deadline: %w", err)
	}
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return nil, fmt.Errorf("forge: send command: %w", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(ipcReadTimeout)); err != nil {
		return nil, fmt.Errorf("forge: set read deadline: %w", err)
	}

	// Read up to ipcMaxResponse+1 bytes so we can detect overflow without
	// silently truncating. io.ReadAll drains until EOF, avoiding partial reads.
	lr := &io.LimitedReader{R: conn, N: ipcMaxResponse + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("forge: read response: %w", err)
	}
	if int64(len(data)) > ipcMaxResponse {
		return nil, fmt.Errorf("forge: response exceeds %d bytes", ipcMaxResponse)
	}
	return data, nil
}

// Health checks whether the forge daemon is reachable by sending a "ping"
// command and expecting any response. Returns nil if the daemon is alive.
func (c *Client) Health() error {
	_, err := c.SendCommand("ping")
	if err != nil {
		return fmt.Errorf("forge: daemon not reachable: %w", err)
	}
	return nil
}
