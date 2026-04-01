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

// toolRunner abstracts a tool update operation so tests can inject stubs
// without hitting the network or running real commands.
// Returns stdout, stderr, and an error; non-nil error signals failure.
type toolRunner func(ctx context.Context) (stdout, stderr string, err error)

// defaultToolRunners returns the production runners for each updatable tool.
func defaultToolRunners() map[string]toolRunner {
	return map[string]toolRunner{
		"beads":  defaultBeadsRunner,
		"claude": makeSimpleRunner("claude", "update"),
		"go":     defaultGoRunner,
		"node":   makeSimpleRunner("/bin/sh", "-c", "sudo apt-get update -qq && sudo apt-get install -y nodejs"),
		"npm":    makeSimpleRunner("npm", "install", "-g", "npm@latest"),
		"git":    makeSimpleRunner("/bin/sh", "-c", "sudo apt-get update -qq && sudo apt-get install -y git"),
		"dolt":   defaultDoltRunner,
	}
}

// UpdateToolHandler runs the update/install script for a given tool.
// Supported tools: "forge", "beads", "claude", "go", "node", "npm", "git",
// "dolt". Auth is enforced by RequireAdmin middleware in the router.
func UpdateToolHandler() http.HandlerFunc {
	return updateToolHandlerWithRunners(defaultToolRunners())
}

func updateToolHandlerWithRunners(runners map[string]toolRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tool := chi.URLParam(r, "tool")

		// Forge is special: it restarts the server asynchronously.
		if tool == "forge" {
			handleForgeUpdate(w)
			return
		}

		runner, ok := runners[tool]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown tool: "+tool)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()

		stdout, stderr, err := runner(ctx)
		if err != nil {
			log.Printf("infra: %s update failed: %v; stderr: %s", tool, err, stderr)
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

// runCommand executes a command and captures stdout/stderr.
func runCommand(ctx context.Context, name string, args ...string) (string, string, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// makeSimpleRunner creates a toolRunner that executes a single command.
func makeSimpleRunner(name string, args ...string) toolRunner {
	return func(ctx context.Context) (string, string, error) {
		return runCommand(ctx, name, args...)
	}
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

// defaultGoRunner downloads and installs the latest Go version from go.dev.
func defaultGoRunner(ctx context.Context) (string, string, error) {
	// Fetch latest version string from go.dev.
	verCmd := exec.CommandContext(ctx, "curl", "-fsSL", "https://go.dev/VERSION?m=text") //nolint:gosec
	verOut, err := verCmd.Output()
	if err != nil {
		return "", "failed to fetch latest Go version from go.dev", err
	}
	// VERSION?m=text returns lines like "go1.22.0\ntime ...\n"; take the first line.
	version := strings.SplitN(strings.TrimSpace(string(verOut)), "\n", 2)[0]
	if !strings.HasPrefix(version, "go") {
		return "", "unexpected version format: " + version, fmt.Errorf("unexpected version format: %s", version)
	}

	// Download tarball to a temp file, then extract.
	tarPath := filepath.Join(os.TempDir(), "go-update.tar.gz")
	if _, dlStderr, dlErr := runCommand(ctx, "curl", "-fsSL", "-o", tarPath,
		fmt.Sprintf("https://go.dev/dl/%s.linux-amd64.tar.gz", version)); dlErr != nil {
		return "", dlStderr, fmt.Errorf("failed to download Go tarball: %w", dlErr)
	}
	defer os.Remove(tarPath) //nolint:errcheck

	script := fmt.Sprintf(
		"sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf %s",
		tarPath,
	)
	stdout, stderr, runErr := runCommand(ctx, "/bin/sh", "-c", script)
	if runErr != nil {
		return stdout, stderr, runErr
	}
	return fmt.Sprintf("installed %s\n%s", version, stdout), stderr, nil
}

// defaultDoltRunner downloads and installs the latest Dolt release from GitHub.
// The install script is downloaded to a temp file and then executed, avoiding
// the security risk of piping curl directly to bash.
func defaultDoltRunner(ctx context.Context) (string, string, error) {
	tmpFile, err := os.CreateTemp("", "dolt-install-*.sh")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) //nolint:errcheck
	tmpFile.Close()

	if _, dlStderr, dlErr := runCommand(ctx, "curl", "-fsSL", "-o", tmpFile.Name(),
		"https://github.com/dolthub/dolt/releases/latest/download/install.sh"); dlErr != nil {
		return "", dlStderr, fmt.Errorf("failed to download dolt install script: %w", dlErr)
	}

	return runCommand(ctx, "sudo", "/bin/bash", tmpFile.Name())
}

// invalidateVersionsCache clears the cached versions so the next fetch picks
// up any changes from a tool update.
func invalidateVersionsCache() {
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = nil
	versionsCacheInstance.mu.Unlock()
}
