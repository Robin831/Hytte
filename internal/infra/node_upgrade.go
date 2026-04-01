package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// knownLTSMajors lists Node.js LTS major versions available from nodesource.
// Even-numbered releases enter LTS; this list covers currently relevant lines.
var knownLTSMajors = []int{18, 20, 22, 24}

// nodeLTSChecker abstracts the HTTP HEAD check so tests can stub it.
type nodeLTSChecker func(ctx context.Context, major int) (bool, error)

// defaultNodeLTSChecker performs a HEAD request against the nodesource setup
// script URL to determine whether a given major version is available.
func defaultNodeLTSChecker(ctx context.Context, major int) (bool, error) {
	url := fmt.Sprintf("https://deb.nodesource.com/setup_%d.x", major)
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// nodeInstalledMajor returns the currently installed Node.js major version
// by running "node --version" and parsing the output.
var nodeInstalledMajor = func(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, resolveCommand("node"), "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("node --version: %w", err)
	}
	return parseNodeMajor(string(out))
}

// parseNodeMajor extracts the major version number from a "vX.Y.Z" string.
func parseNodeMajor(version string) (int, error) {
	v := bytes.TrimSpace([]byte(version))
	s := string(v)
	if len(s) > 0 && s[0] == 'v' {
		s = s[1:]
	}
	parts := bytes.SplitN([]byte(s), []byte("."), 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("cannot parse node major from %q", version)
	}
	major, err := strconv.Atoi(string(parts[0]))
	if err != nil {
		return 0, fmt.Errorf("cannot parse node major from %q: %w", version, err)
	}
	return major, nil
}

// NodeLTSResponse is the JSON response for the LTS versions endpoint.
type NodeLTSResponse struct {
	CurrentMajor    int   `json:"current_major"`
	AvailableMajors []int `json:"available_majors"`
}

// nodeLTSHandlerWith is the testable core of NodeLTSVersionsHandler.
func nodeLTSHandlerWith(checker nodeLTSChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		currentMajor, err := nodeInstalledMajor(ctx)
		if err != nil {
			log.Printf("infra: failed to detect current node major: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to detect current Node.js version")
			return
		}

		// Check which LTS majors beyond the current one are available.
		available := []int{}
		for _, major := range knownLTSMajors {
			if major <= currentMajor {
				continue
			}
			ok, checkErr := checker(ctx, major)
			if checkErr != nil {
				log.Printf("infra: nodesource check for v%d failed: %v", major, checkErr)
				continue
			}
			if ok {
				available = append(available, major)
			}
		}

		writeJSON(w, http.StatusOK, NodeLTSResponse{
			CurrentMajor:    currentMajor,
			AvailableMajors: available,
		})
	}
}

// NodeLTSVersionsHandler returns the current Node.js major version and a list
// of available LTS major versions that are newer than the installed one.
func NodeLTSVersionsHandler() http.HandlerFunc {
	return nodeLTSHandlerWith(defaultNodeLTSChecker)
}

// isKnownLTSMajor returns true if the given major version is in the
// knownLTSMajors whitelist. This prevents arbitrary user input from being
// interpolated into shell commands.
func isKnownLTSMajor(major int) bool {
	for _, m := range knownLTSMajors {
		if m == major {
			return true
		}
	}
	return false
}

// nodeUpgradeRunner abstracts the upgrade execution so tests can stub it.
type nodeUpgradeRunner func(ctx context.Context, major int) (stdout, stderr string, err error)

// defaultNodeUpgradeRunner performs the actual nodesource repo setup and
// apt-get install for the given major version.
func defaultNodeUpgradeRunner(ctx context.Context, major int) (string, string, error) {
	// major is already validated against the whitelist; use Itoa for safety.
	majorStr := strconv.Itoa(major)

	// Step 1: Download the NodeSource setup script to a temp file to avoid
	// piping curl directly into bash (supply-chain risk + auditability).
	tmpFile, tmpErr := os.CreateTemp("", "nodesource-setup-*.sh")
	if tmpErr != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", tmpErr)
	}
	defer os.Remove(tmpFile.Name()) //nolint:errcheck
	tmpFile.Close()

	scriptURL := "https://deb.nodesource.com/setup_" + majorStr + ".x"
	var dlStderr bytes.Buffer
	dlCmd := exec.CommandContext(ctx, "curl", "-fsSL", "-o", tmpFile.Name(), scriptURL) //nolint:gosec
	dlCmd.Stderr = &dlStderr
	if dlErr := dlCmd.Run(); dlErr != nil {
		log.Printf("infra: nodesource setup script download failed: %v; stderr: %s", dlErr, dlStderr.String())
		return "", dlStderr.String(), fmt.Errorf("failed to download NodeSource setup script: %w", dlErr)
	}

	// Execute the downloaded script with sudo.
	var setupOut, setupErr bytes.Buffer
	setupCmd := exec.CommandContext(ctx, "sudo", "-E", "bash", tmpFile.Name()) //nolint:gosec
	setupCmd.Stdout = &setupOut
	setupCmd.Stderr = &setupErr
	if err := setupCmd.Run(); err != nil {
		return setupOut.String(), fmt.Sprintf("Repository setup failed: %s", setupErr.String()), err
	}

	// Step 2: Install nodejs from the newly configured repository.
	var installOut, installErr bytes.Buffer
	installCmd := exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "nodejs") //nolint:gosec
	installCmd.Stdout = &installOut
	installCmd.Stderr = &installErr
	if err := installCmd.Run(); err != nil {
		return setupOut.String() + "\n" + installOut.String(),
			fmt.Sprintf("Package install failed: %s", installErr.String()), err
	}

	return setupOut.String() + "\n" + installOut.String(),
		setupErr.String() + installErr.String(), nil
}

// nodeMajorUpgradeHandlerWith is the testable core of NodeMajorUpgradeHandler.
func nodeMajorUpgradeHandlerWith(runner nodeUpgradeRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Major int `json:"major"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Major < 1 {
			writeError(w, http.StatusBadRequest, "invalid or missing major version")
			return
		}

		// Validate against the known LTS whitelist to prevent arbitrary
		// version numbers from reaching shell commands.
		if !isKnownLTSMajor(body.Major) {
			writeError(w, http.StatusBadRequest, "unsupported major version")
			return
		}

		// Reject attempts to install the same or an older major version —
		// this endpoint is for upgrades only.
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
		defer cancel()

		currentMajor, err := nodeInstalledMajor(ctx)
		if err != nil {
			log.Printf("infra: failed to detect current node major: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to detect current Node.js version")
			return
		}
		if body.Major <= currentMajor {
			writeError(w, http.StatusBadRequest, "target major must be greater than installed major")
			return
		}

		stdout, stderr, err := runner(ctx, body.Major)
		if err != nil {
			log.Printf("infra: node major upgrade to v%d failed: %v", body.Major, err)
			writeJSON(w, http.StatusOK, updateToolResult{
				Success: false,
				Stdout:  stdout,
				Stderr:  stderr,
			})
			return
		}

		invalidateVersionsCache()

		// Also invalidate latest versions cache since the apt source changed.
		latestCacheInstance.mu.Lock()
		latestCacheInstance.data = nil
		latestCacheInstance.mu.Unlock()

		writeJSON(w, http.StatusOK, updateToolResult{
			Success: true,
			Stdout:  stdout,
			Stderr:  stderr,
		})
	}
}

// NodeMajorUpgradeHandler runs the nodesource repo reconfiguration for a
// target major version and then installs Node.js from the new repository.
func NodeMajorUpgradeHandler() http.HandlerFunc {
	return nodeMajorUpgradeHandlerWith(defaultNodeUpgradeRunner)
}
