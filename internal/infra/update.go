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
	"strings"
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
		case "claude":
			handleSimpleCommandUpdate(w, r, "claude", "claude", "update")
		case "go":
			handleGoUpdate(w, r)
		case "node":
			handleSimpleCommandUpdate(w, r, "node", "/bin/sh", "-c", "sudo apt-get update -qq && sudo apt-get install -y nodejs")
		case "npm":
			handleSimpleCommandUpdate(w, r, "npm", "npm", "install", "-g", "npm@latest")
		case "git":
			handleSimpleCommandUpdate(w, r, "git", "/bin/sh", "-c", "sudo apt-get update -qq && sudo apt-get install -y git")
		case "dolt":
			handleDoltUpdate(w, r)
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

	invalidateVersionsCache()

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

// handleSimpleCommandUpdate runs a command and returns the result. Used for
// tools that can be updated with a single command invocation.
func handleSimpleCommandUpdate(w http.ResponseWriter, r *http.Request, toolName string, name string, args ...string) {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		log.Printf("infra: %s update failed: %v; stderr: %s", toolName, err, errBuf.String())
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stdout:  outBuf.String(),
			Stderr:  errBuf.String(),
		})
		return
	}

	invalidateVersionsCache()

	writeJSON(w, http.StatusOK, updateToolResult{
		Success: true,
		Stdout:  outBuf.String(),
		Stderr:  errBuf.String(),
	})
}

// handleGoUpdate downloads and installs the latest Go version from go.dev.
func handleGoUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	// Fetch latest version string from go.dev.
	verCmd := exec.CommandContext(ctx, "curl", "-fsSL", "https://go.dev/VERSION?m=text") //nolint:gosec
	verOut, err := verCmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stderr:  "failed to fetch latest Go version from go.dev",
		})
		return
	}
	// VERSION?m=text returns lines like "go1.22.0\ntime ...\n"; take the first line.
	version := strings.SplitN(strings.TrimSpace(string(verOut)), "\n", 2)[0]
	if !strings.HasPrefix(version, "go") {
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stderr:  "unexpected version format: " + version,
		})
		return
	}

	// Download and install.
	script := fmt.Sprintf(
		"curl -fsSL 'https://go.dev/dl/%s.linux-amd64.tar.gz' -o /tmp/go-update.tar.gz "+
			"&& sudo rm -rf /usr/local/go "+
			"&& sudo tar -C /usr/local -xzf /tmp/go-update.tar.gz "+
			"&& rm -f /tmp/go-update.tar.gz",
		version,
	)

	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if runErr := cmd.Run(); runErr != nil {
		log.Printf("infra: go update failed: %v; stderr: %s", runErr, errBuf.String())
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stdout:  outBuf.String(),
			Stderr:  errBuf.String(),
		})
		return
	}

	invalidateVersionsCache()

	writeJSON(w, http.StatusOK, updateToolResult{
		Success: true,
		Stdout:  fmt.Sprintf("installed %s\n%s", version, outBuf.String()),
		Stderr:  errBuf.String(),
	})
}

// handleDoltUpdate downloads and installs the latest Dolt release from GitHub.
func handleDoltUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	script := "curl -fsSL https://github.com/dolthub/dolt/releases/latest/download/install.sh | sudo /bin/bash"

	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		log.Printf("infra: dolt update failed: %v; stderr: %s", err, errBuf.String())
		writeJSON(w, http.StatusOK, updateToolResult{
			Success: false,
			Stdout:  outBuf.String(),
			Stderr:  errBuf.String(),
		})
		return
	}

	invalidateVersionsCache()

	writeJSON(w, http.StatusOK, updateToolResult{
		Success: true,
		Stdout:  outBuf.String(),
		Stderr:  errBuf.String(),
	})
}

// invalidateVersionsCache clears the cached versions so the next fetch picks
// up any changes from a tool update.
func invalidateVersionsCache() {
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = nil
	versionsCacheInstance.mu.Unlock()
}
