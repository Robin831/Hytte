package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// githubActionsURL extracts the GitHub Actions URL from a git remote URL.
// It handles HTTPS (https://github.com/owner/repo.git) and SSH (git@github.com:owner/repo.git) formats.
func githubActionsURL(remoteURL string) string {
	remoteURL = strings.TrimSpace(remoteURL)
	var owner, repo string
	if strings.HasPrefix(remoteURL, "https://github.com/") {
		// https://github.com/owner/repo.git
		path := strings.TrimPrefix(remoteURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 3)
		if len(parts) >= 2 {
			owner, repo = parts[0], parts[1]
		}
	} else if strings.HasPrefix(remoteURL, "git@github.com:") {
		// git@github.com:owner/repo.git
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 3)
		if len(parts) >= 2 {
			owner, repo = parts[0], parts[1]
		}
	}
	if owner == "" || repo == "" {
		return ""
	}
	return "https://github.com/" + owner + "/" + repo + "/actions"
}

// semverPattern validates a semantic version string (e.g. "1.2.3" or "0.10.0").
// It does NOT allow a leading "v" — the handler adds the "v" prefix for the tag.
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// StepResult describes the outcome of a single step in the release pipeline.
type StepResult struct {
	Step    string `json:"step"`
	Command string `json:"command"`
	Output  string `json:"output,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ReleaseResponse is the JSON response from the release endpoint.
type ReleaseResponse struct {
	Version    string       `json:"version"`
	Tag        string       `json:"tag"`
	Success    bool         `json:"success"`
	Steps      []StepResult `json:"steps"`
	ActionsURL string       `json:"actions_url,omitempty"`
}

// releaseRequest is the expected JSON body for POST /api/forge/release.
type releaseRequest struct {
	Version string `json:"version"`
}

// CommandRunner abstracts command execution for testing.
type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) (string, error)
}

// execRunner runs real commands via os/exec.
type execRunner struct{}

// runTimeout caps each individual command so a hung git/forge process cannot
// block the server indefinitely.
const runTimeout = 10 * time.Minute

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	// Use whichever deadline comes first: the request context or runTimeout.
	tctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, name, args...) //nolint:gosec
	cmd.Dir = dir
	// Prevent git from prompting for credentials or any terminal input.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// repoRoot returns the path to the Hytte source repository. It checks
// HYTTE_REPO_DIR first, then falls back to detecting the git repository root
// via `git rev-parse --show-toplevel`. The resolved directory is validated to
// contain a go.mod file so that deployment directories (which may be separate
// git checkouts with stale tags and fragments) are rejected early.
func repoRoot() (string, error) {
	if envDir := strings.TrimSpace(os.Getenv("HYTTE_REPO_DIR")); envDir != "" {
		dir := filepath.Clean(envDir)

		info, statErr := os.Stat(dir)
		if statErr != nil {
			return "", fmt.Errorf("HYTTE_REPO_DIR %q is invalid: %w", dir, statErr)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("HYTTE_REPO_DIR %q is not a directory; set it to the Hytte source repository path", dir)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr != nil {
			return "", fmt.Errorf("HYTTE_REPO_DIR %q does not contain go.mod; set it to the Hytte source repository path", dir)
		}
		return dir, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel") //nolint:gosec
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("HYTTE_REPO_DIR is not set and git rev-parse failed: %v (output: %s)", err, strings.TrimSpace(string(out)))
	}

	dir := strings.TrimSpace(string(out))
	// Validate that the detected directory is the actual source repository,
	// not a deployment checkout. A deployment directory may have stale tags
	// and changelog fragments that differ from the source repo.
	if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr != nil {
		return "", fmt.Errorf("detected repo root %q does not contain go.mod; set HYTTE_REPO_DIR to the Hytte source repository path", dir)
	}
	return dir, nil
}

// forgeRepoRoot returns the path to the Forge source repository. The release
// and version-suggestion endpoints must operate on the Forge repo — not the
// Hytte repo — so that version tags and changelog fragments are correct.
//
// It requires the FORGE_REPO_DIR environment variable to be set explicitly.
// There is no git rev-parse fallback because the server process typically runs
// inside the Hytte checkout, which would resolve to the wrong repository.
func forgeRepoRoot() (string, error) {
	envDir := strings.TrimSpace(os.Getenv("FORGE_REPO_DIR"))
	if envDir == "" {
		return "", fmt.Errorf("FORGE_REPO_DIR is not set; set it to the Forge source repository path (e.g. /home/robin/source/Forge)")
	}

	dir := filepath.Clean(envDir)

	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("FORGE_REPO_DIR %q must be an absolute path; relative paths resolve against the server working directory and may point at the wrong repository", dir)
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		return "", fmt.Errorf("FORGE_REPO_DIR %q is invalid: %w", dir, statErr)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("FORGE_REPO_DIR %q is not a directory; set it to the Forge source repository path", dir)
	}

	// Validate that the directory is a git repository to catch misconfiguration early.
	if _, gitStatErr := os.Stat(filepath.Join(dir, ".git")); gitStatErr != nil {
		return "", fmt.Errorf("FORGE_REPO_DIR %q does not contain a .git directory; set it to the Forge source repository path", dir)
	}

	return dir, nil
}

// forgeBin returns the absolute path to the forge CLI binary. It checks
// FORGE_BIN first, then falls back to ~/.forge/forge.
func forgeBin() string {
	if bin := os.Getenv("FORGE_BIN"); bin != "" {
		return filepath.Clean(bin)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/usr/local/bin/forge"
	}
	return filepath.Join(home, ".forge", "forge")
}

// releaseMu ensures only one release pipeline runs at a time, preventing
// concurrent requests from interleaving git operations on the shared checkout.
var releaseMu sync.Mutex

// ReleaseHandler executes the release pipeline: fetch/reset to main, assemble
// changelog, remove fragments, commit, tag, and push.
func ReleaseHandler(runner CommandRunner) http.HandlerFunc {
	if runner == nil {
		runner = execRunner{}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Limit request body to 1KB — the payload is a small JSON object.
		r.Body = http.MaxBytesReader(w, r.Body, 1024)

		var req releaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		version := strings.TrimSpace(req.Version)
		if version == "" {
			writeError(w, http.StatusBadRequest, "version is required")
			return
		}
		if !semverPattern.MatchString(version) {
			writeError(w, http.StatusBadRequest, "version must be valid semver (e.g. 1.2.3)")
			return
		}

		repoDir, err := forgeRepoRoot()
		if err != nil {
			log.Printf("forge: forgeRepoRoot failed: %v", err)
			writeError(w, http.StatusInternalServerError, "FORGE_REPO_DIR is not set or invalid; check server configuration")
			return
		}
		forgePath := forgeBin()
		// Validate forge binary path: must be absolute and clean (no shell metacharacters).
		if !filepath.IsAbs(forgePath) {
			writeError(w, http.StatusInternalServerError, "forge binary path must be absolute")
			return
		}
		if filepath.Clean(forgePath) != forgePath {
			writeError(w, http.StatusInternalServerError, "forge binary path is not clean")
			return
		}
		tag := "v" + version

		// Acquire the process-wide lock so concurrent requests cannot corrupt
		// the working tree or produce inconsistent commits/tags.
		releaseMu.Lock()
		defer releaseMu.Unlock()

		ctx := r.Context()

		resp := ReleaseResponse{
			Version: version,
			Tag:     tag,
			Success: true,
			Steps:   []StepResult{},
		}

		type step struct {
			name string
			cmd  string
			args []string
		}

		steps := []step{
			// Use fetch + checkout + reset --hard instead of pull to guarantee
			// we are on main, avoid merge commits, and enforce fast-forward behavior.
			{"fetch-main", "git", []string{"fetch", "origin", "main"}},
			{"checkout-main", "git", []string{"checkout", "main"}},
			{"reset-main", "git", []string{"reset", "--hard", "origin/main"}},
			{"changelog", forgePath, []string{"changelog", "assemble", "--version", version}},
			{"remove-fragments", "git", []string{"ls-files", "changelog.d/"}},
			// Stage only the expected paths, not all working-tree changes.
			{"stage", "git", []string{"add", "CHANGELOG.md"}},
			{"commit", "git", []string{"commit", "-m", fmt.Sprintf("release: %s\n\nAssembled changelog and tagged %s.", tag, tag)}},
			{"tag", "git", []string{"tag", "-a", tag, "-m", fmt.Sprintf("Release %s", tag)}},
			{"push", "git", []string{"push", "origin", "main", "--tags"}},
		}

		for _, s := range steps {
			// The "remove-fragments" step is special: first list tracked fragment
			// files, remove them from the working tree and index, then handle
			// any untracked leftovers in changelog.d/.
			if s.name == "remove-fragments" {
				result := StepResult{Step: s.name, Command: "rm changelog.d/ fragments + git add"}

				// List tracked fragment files.
				listOut, listErr := runner.Run(ctx, repoDir, "git", "ls-files", "changelog.d/")
				if listErr != nil {
					result.Success = false
					result.Error = fmt.Sprintf("failed to list changelog fragments: %v (output: %s)", listErr, listOut)
					resp.Steps = append(resp.Steps, result)
					resp.Success = false
					break
				}

				files := strings.Fields(listOut)
				// Validate fragment paths: must be under changelog.d/ with no traversal.
				for _, f := range files {
					if !strings.HasPrefix(f, "changelog.d/") || strings.Contains(f, "..") {
						result.Success = false
						result.Error = "unexpected path in changelog.d listing: " + f
						resp.Steps = append(resp.Steps, result)
						resp.Success = false
						break
					}
				}
				if !resp.Success {
					break
				}
				if len(files) > 0 {
					rmArgs := append([]string{"rm", "-f", "--"}, files...)
					rmOut, rmErr := runner.Run(ctx, repoDir, "git", rmArgs...)
					if rmErr != nil {
						result.Success = false
						result.Error = fmt.Sprintf("failed to remove fragments: %v (output: %s)", rmErr, rmOut)
						resp.Steps = append(resp.Steps, result)
						resp.Success = false
						break
					}
					result.Output = fmt.Sprintf("removed %d fragment(s)", len(files))
				} else {
					result.Output = "no fragments to remove"
				}

				// Also remove any untracked files in changelog.d/. Propagate errors
				// so the pipeline does not silently proceed with leftover files.
				untrackedOut, untrackedErr := runner.Run(ctx, repoDir, "git", "ls-files", "--others", "changelog.d/")
				if untrackedErr != nil {
					result.Success = false
					result.Error = fmt.Sprintf("failed to list untracked changelog fragments: %v (output: %s)", untrackedErr, untrackedOut)
					resp.Steps = append(resp.Steps, result)
					resp.Success = false
					break
				}
				if extras := strings.Fields(untrackedOut); len(extras) > 0 {
					absRepo, absErr := filepath.Abs(repoDir)
					if absErr != nil {
						result.Success = false
						result.Error = "failed to resolve repository path: " + absErr.Error()
						resp.Steps = append(resp.Steps, result)
						resp.Success = false
						break
					}
					for _, f := range extras {
						// Validate untracked fragment paths: must be under changelog.d/
						// with no path traversal components.
						if !strings.HasPrefix(f, "changelog.d/") || strings.Contains(f, "..") {
							result.Success = false
							result.Error = "invalid untracked fragment path: " + f
							resp.Steps = append(resp.Steps, result)
							resp.Success = false
							break
						}
						p := filepath.Join(repoDir, f)
						absP, absErr := filepath.Abs(p)
						if absErr != nil {
							result.Success = false
							result.Error = "failed to resolve fragment path: " + f
							resp.Steps = append(resp.Steps, result)
							resp.Success = false
							break
						}
						if !strings.HasPrefix(absP, absRepo+string(filepath.Separator)) {
							result.Success = false
							result.Error = "unsafe untracked fragment path outside repo: " + f
							resp.Steps = append(resp.Steps, result)
							resp.Success = false
							break
						}
						if rmErr := os.Remove(absP); rmErr != nil {
							result.Success = false
							result.Error = fmt.Sprintf("failed to remove untracked fragment %s: %v", f, rmErr)
							resp.Steps = append(resp.Steps, result)
							resp.Success = false
							break
						}
					}
					if !resp.Success {
						break
					}
				}

				result.Success = true
				resp.Steps = append(resp.Steps, result)
				continue
			}

			// The "stage" step adds only the expected paths (CHANGELOG.md), not
			// all working-tree changes, to avoid accidentally committing unrelated edits.
			if s.name == "stage" {
				result := StepResult{Step: s.name, Command: "git add CHANGELOG.md"}
				out, err := runner.Run(ctx, repoDir, "git", "add", "CHANGELOG.md")
				if err != nil {
					result.Success = false
					result.Error = out
					resp.Steps = append(resp.Steps, result)
					resp.Success = false
					break
				}
				result.Output = out
				result.Success = true
				resp.Steps = append(resp.Steps, result)
				continue
			}

			result := StepResult{
				Step:    s.name,
				Command: s.cmd + " " + strings.Join(s.args, " "),
			}

			out, err := runner.Run(ctx, repoDir, s.cmd, s.args...)
			result.Output = out
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				resp.Steps = append(resp.Steps, result)
				resp.Success = false
				break
			}
			result.Success = true
			resp.Steps = append(resp.Steps, result)
		}

		// Derive GitHub Actions URL from the git remote when the release succeeds.
		if resp.Success {
			if remoteURL, err := runner.Run(ctx, repoDir, "git", "remote", "get-url", "origin"); err == nil {
				if actionsURL := githubActionsURL(remoteURL); actionsURL != "" {
					resp.ActionsURL = actionsURL
				}
			}
		}

		status := http.StatusOK
		if !resp.Success {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, resp)
	}
}
