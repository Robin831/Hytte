package forge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

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
	Version string       `json:"version"`
	Tag     string       `json:"tag"`
	Success bool         `json:"success"`
	Steps   []StepResult `json:"steps"`
}

// releaseRequest is the expected JSON body for POST /api/forge/release.
type releaseRequest struct {
	Version string `json:"version"`
}

// CommandRunner abstracts command execution for testing.
type CommandRunner interface {
	Run(dir, name string, args ...string) (string, error)
}

// execRunner runs real commands via os/exec.
type execRunner struct{}

func (execRunner) Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// repoRoot returns the path to the main repository. It checks HYTTE_REPO_DIR
// first, then falls back to the current working directory.
func repoRoot() (string, error) {
	if dir := os.Getenv("HYTTE_REPO_DIR"); dir != "" {
		return dir, nil
	}
	return os.Getwd()
}

// forgeBin returns the path to the forge CLI binary. It checks FORGE_BIN first,
// then falls back to ~/.forge/forge.
func forgeBin() string {
	if bin := os.Getenv("FORGE_BIN"); bin != "" {
		return bin
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "forge"
	}
	return filepath.Join(home, ".forge", "forge")
}

// ReleaseHandler executes the release pipeline: pull, assemble changelog,
// remove fragments, commit, tag, and push.
func ReleaseHandler(runner CommandRunner) http.HandlerFunc {
	if runner == nil {
		runner = execRunner{}
	}

	return func(w http.ResponseWriter, r *http.Request) {
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

		repoDir, err := repoRoot()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to determine repository directory")
			return
		}
		forgePath := forgeBin()
		tag := "v" + version

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
			{"pull", "git", []string{"pull", "origin", "main"}},
			{"changelog", forgePath, []string{"changelog", "assemble", "--version", version}},
			{"remove-fragments", "git", []string{"ls-files", "changelog.d/"}},
			{"stage", "git", []string{"add", "CHANGELOG.md"}},
			{"commit", "git", []string{"commit", "-m", fmt.Sprintf("release: %s\n\nAssembled changelog and tagged %s.", tag, tag)}},
			{"tag", "git", []string{"tag", "-a", tag, "-m", fmt.Sprintf("Release %s", tag)}},
			{"push", "git", []string{"push", "origin", "main", "--tags"}},
		}

		for _, s := range steps {
			// The "remove-fragments" step is special: first list tracked fragment
			// files, remove them from the working tree and index, then stage the
			// removals together with any new changelog changes.
			if s.name == "remove-fragments" {
				result := StepResult{Step: s.name, Command: "rm changelog.d/ fragments + git add"}

				// List tracked fragment files.
				listOut, listErr := runner.Run(repoDir, "git", "ls-files", "changelog.d/")
				if listErr != nil {
					result.Success = false
					result.Error = "failed to list changelog fragments: " + listOut
					resp.Steps = append(resp.Steps, result)
					resp.Success = false
					break
				}

				files := strings.Fields(listOut)
				if len(files) > 0 {
					rmArgs := append([]string{"rm", "-f", "--"}, files...)
					rmOut, rmErr := runner.Run(repoDir, "git", rmArgs...)
					if rmErr != nil {
						result.Success = false
						result.Error = "failed to remove fragments: " + rmOut
						resp.Steps = append(resp.Steps, result)
						resp.Success = false
						break
					}
					result.Output = fmt.Sprintf("removed %d fragment(s)", len(files))
				} else {
					result.Output = "no fragments to remove"
				}

				// Also remove any untracked files in changelog.d/.
				untracked, _ := runner.Run(repoDir, "git", "ls-files", "--others", "changelog.d/")
				if extras := strings.Fields(untracked); len(extras) > 0 {
					for _, f := range extras {
						p := filepath.Join(repoDir, f)
						os.Remove(p) //nolint:errcheck
					}
				}

				result.Success = true
				resp.Steps = append(resp.Steps, result)
				continue
			}

			// The "stage" step adds CHANGELOG.md and any removals from the
			// fragment cleanup.
			if s.name == "stage" {
				result := StepResult{Step: s.name, Command: "git add -A"}
				out, err := runner.Run(repoDir, "git", "add", "-A")
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

			out, err := runner.Run(repoDir, s.cmd, s.args...)
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

		status := http.StatusOK
		if !resp.Success {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, resp)
	}
}
