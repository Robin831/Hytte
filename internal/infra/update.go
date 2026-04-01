package infra

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
)

// updateToolResult is the JSON response for tool update endpoints.
type updateToolResult struct {
	Success bool   `json:"success"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
}

// beadsRunner abstracts the download+execute steps for the beads update so
// tests can inject stubs without hitting the network or running bash.
// Returns stdout, stderr, and an error; non-nil error signals failure.
type beadsRunner func(ctx context.Context) (stdout, stderr string, err error)

// UpdateToolHandler runs the update/install script for a given tool.
// Supported tools: "forge" (runs ~/.forge/restart.sh) and "beads" (runs the
// beads install script). Requires admin auth middleware.
func UpdateToolHandler() http.HandlerFunc {
	return updateToolHandlerWithRunner(defaultBeadsRunner)
}

func updateToolHandlerWithRunner(runner beadsRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tool := chi.URLParam(r, "tool")

		switch tool {
		case "forge":
			handleForgeUpdate(w)
		case "beads":
			handleBeadsUpdateWithRunner(w, r, runner)
		default:
			writeError(w, http.StatusBadRequest, "unknown tool: "+tool)
		}
	}
}

// handleForgeUpdate runs ~/.forge/restart.sh. Because the script restarts the
// server process, the handler returns 202 Accepted and launches the script
// asynchronously (same semantics as the forge dashboard restart).
func handleForgeUpdate(w http.ResponseWriter) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve home directory")
		return
	}
	scriptPath := filepath.Join(home, ".forge", "restart.sh")
	fi, err := os.Lstat(scriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "restart script not found at ~/.forge/restart.sh")
		} else if os.IsPermission(err) {
			writeError(w, http.StatusInternalServerError, "permission denied accessing restart script")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to stat restart script")
		}
		return
	}
	if !fi.Mode().IsRegular() {
		writeError(w, http.StatusInternalServerError, "restart script is not a regular file")
		return
	}

	writeJSON(w, http.StatusAccepted, updateToolResult{Success: true, Stdout: "restart initiated"})
	go func() {
		time.Sleep(200 * time.Millisecond)
		cmd := exec.Command("/bin/sh", scriptPath) //nolint:gosec
		if err := cmd.Run(); err != nil {
			log.Printf("infra: forge restart script failed: %v", err)
		}
	}()
}

// handleBeadsUpdate runs the beads install script to update the bd CLI tool.
func handleBeadsUpdate(w http.ResponseWriter, r *http.Request) {
	handleBeadsUpdateWithRunner(w, r, defaultBeadsRunner)
}

// handleBeadsUpdateWithRunner is the testable core of handleBeadsUpdate.
func handleBeadsUpdateWithRunner(w http.ResponseWriter, r *http.Request, runner beadsRunner) {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	stdout, stderr, err := runner(ctx)
	if err != nil {
		log.Printf("infra: beads update failed: %v; stderr: %s", err, stderr)
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stdout:  stdout,
			Stderr:  stderr,
		})
		return
	}

	// Invalidate the versions cache so the next fetch picks up the new version.
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = nil
	versionsCacheInstance.mu.Unlock()

	writeJSON(w, http.StatusOK, updateToolResult{
		Success: true,
		Stdout:  stdout,
		Stderr:  stderr,
	})
}

// defaultBeadsRunner implements the real download+execute for the beads CLI.
//
// Security note: the install script is fetched from the beads repository's
// main branch on every invocation. This is a known supply-chain risk: the
// script content can change without notice. A future improvement should pin
// to a specific commit SHA or release tag and verify a checksum/signature
// before execution.
func defaultBeadsRunner(ctx context.Context) (stdout, stderr string, err error) {
	// Download the install script to a temp file (avoids curl-pipe-bash and
	// allows integrity inspection before execution).
	tmpFile, tmpErr := os.CreateTemp("", "beads-install-*.sh")
	if tmpErr != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", tmpErr)
	}
	defer os.Remove(tmpFile.Name()) //nolint:errcheck
	tmpFile.Close()

	var dlStderr bytes.Buffer
	dlCmd := exec.CommandContext(ctx, "curl", "-fsSL", "-o", tmpFile.Name(), //nolint:gosec
		"https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh")
	dlCmd.Stderr = &dlStderr
	if dlErr := dlCmd.Run(); dlErr != nil {
		log.Printf("infra: beads install script download failed: %v; stderr: %s", dlErr, dlStderr.String())
		return "", dlStderr.String(), fmt.Errorf("failed to download beads install script: %w", dlErr)
	}

	// Execute the downloaded script.
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", tmpFile.Name()) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if runErr := cmd.Run(); runErr != nil {
		log.Printf("infra: beads update failed: %v; stderr: %s", runErr, errBuf.String())
		return outBuf.String(), errBuf.String(), runErr
	}

	return outBuf.String(), errBuf.String(), nil
}
