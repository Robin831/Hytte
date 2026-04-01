package infra

import (
	"bytes"
	"context"
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

// UpdateToolHandler runs the update/install script for a given tool.
// Supported tools: "forge" (runs ~/.forge/restart.sh) and "beads" (runs the
// beads install script). Requires admin auth middleware.
func UpdateToolHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tool := chi.URLParam(r, "tool")

		switch tool {
		case "forge":
			handleForgeUpdate(w)
		case "beads":
			handleBeadsUpdate(w)
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
// The script is fetched via curl and piped to bash.
func handleBeadsUpdate(w http.ResponseWriter) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c",
		"curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash") //nolint:gosec
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		log.Printf("infra: beads update failed: %v; stderr: %s", err, stderr.String())
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stdout:  stdout.String(),
			Stderr:  stderr.String(),
		})
		return
	}

	// Invalidate the versions cache so the next fetch picks up the new version.
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = nil
	versionsCacheInstance.mu.Unlock()

	writeJSON(w, http.StatusOK, updateToolResult{
		Success: true,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	})
}
