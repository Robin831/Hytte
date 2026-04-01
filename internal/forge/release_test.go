package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockRunner records and replays command results for testing.
type mockRunner struct {
	calls   []string
	results map[string]struct {
		out string
		err error
	}
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		results: make(map[string]struct {
			out string
			err error
		}),
	}
}

func (m *mockRunner) Set(key, out string, err error) {
	m.results[key] = struct {
		out string
		err error
	}{out, err}
}

func (m *mockRunner) Run(_ context.Context, dir, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, key)
	if r, ok := m.results[key]; ok {
		return r.out, r.err
	}
	// Return an error for any command not explicitly configured so tests
	// actually validate which commands the pipeline invokes.
	return "", fmt.Errorf("mockRunner: unexpected command %q", key)
}

func TestReleaseHandler_ValidVersion(t *testing.T) {
	runner := newMockRunner()
	runner.Set("git fetch origin main", "From github.com:user/repo", nil)
	runner.Set("git checkout main", "Already on 'main'", nil)
	runner.Set("git reset --hard origin/main", "HEAD is now at abc1234", nil)
	runner.Set("/usr/local/bin/forge changelog assemble --version 1.2.3", "Assembled changelog for v1.2.3", nil)
	runner.Set("git ls-files changelog.d/", "changelog.d/Hytte-abc1.md\nchangelog.d/Hytte-abc2.md", nil)
	runner.Set("git rm -f -- changelog.d/Hytte-abc1.md changelog.d/Hytte-abc2.md", "rm 'changelog.d/Hytte-abc1.md'\nrm 'changelog.d/Hytte-abc2.md'", nil)
	runner.Set("git ls-files --others changelog.d/", "", nil)
	runner.Set("git add CHANGELOG.md", "", nil)
	runner.Set("git commit -m release: v1.2.3\n\nAssembled changelog and tagged v1.2.3.", "", nil)
	runner.Set("git tag -a v1.2.3 -m Release v1.2.3", "", nil)
	runner.Set("git push origin main --tags", "To github.com:user/repo.git", nil)
	runner.Set("git remote get-url origin", "git@github.com:Robin831/Hytte.git", nil)

	// Override forgeBin and repoRoot for test.
	t.Setenv("FORGE_BIN", "/usr/local/bin/forge")
	t.Setenv("HYTTE_REPO_DIR", "/tmp/test-repo")

	body := `{"version": "1.2.3"}`
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ReleaseHandler(runner).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ReleaseResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got failure: %+v", resp)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", resp.Version)
	}
	if resp.Tag != "v1.2.3" {
		t.Errorf("expected tag v1.2.3, got %s", resp.Tag)
	}
	if len(resp.Steps) != 9 {
		t.Errorf("expected 9 steps, got %d", len(resp.Steps))
	}
	if resp.ActionsURL != "https://github.com/Robin831/Hytte/actions" {
		t.Errorf("expected actions URL https://github.com/Robin831/Hytte/actions, got %s", resp.ActionsURL)
	}
}

func TestGithubActionsURL(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"git@github.com:Robin831/Hytte.git", "https://github.com/Robin831/Hytte/actions"},
		{"https://github.com/Robin831/Hytte.git", "https://github.com/Robin831/Hytte/actions"},
		{"https://github.com/Robin831/Hytte", "https://github.com/Robin831/Hytte/actions"},
		{"git@gitlab.com:owner/repo.git", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := githubActionsURL(tt.remote)
		if got != tt.want {
			t.Errorf("githubActionsURL(%q) = %q, want %q", tt.remote, got, tt.want)
		}
	}
}

func TestReleaseHandler_InvalidVersion(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`{"version": ""}`, "version is required"},
		{`{"version": "abc"}`, "version must be valid semver"},
		{`{"version": "v1.2.3"}`, "version must be valid semver"},
		{`{"version": "1.2"}`, "version must be valid semver"},
		{`{"version": "1.2.3.4"}`, "version must be valid semver"},
		{`{"version": "01.2.3"}`, "version must be valid semver"},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			ReleaseHandler(nil).ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var errResp map[string]string
			json.NewDecoder(rec.Body).Decode(&errResp)
			if !strings.Contains(errResp["error"], tt.want) {
				t.Errorf("expected error containing %q, got %q", tt.want, errResp["error"])
			}
		})
	}
}

func TestReleaseHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	ReleaseHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestReleaseHandler_StepFailure(t *testing.T) {
	runner := newMockRunner()
	runner.Set("git fetch origin main", "error: cannot fetch", fmt.Errorf("exit status 1"))

	t.Setenv("FORGE_BIN", "/usr/local/bin/forge")
	t.Setenv("HYTTE_REPO_DIR", "/tmp/test-repo")

	body := `{"version": "2.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ReleaseHandler(runner).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ReleaseResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Success {
		t.Error("expected failure")
	}
	if len(resp.Steps) != 1 {
		t.Errorf("expected 1 step (stopped at failure), got %d", len(resp.Steps))
	}
	if resp.Steps[0].Step != "fetch-main" {
		t.Errorf("expected failed step to be 'fetch-main', got %q", resp.Steps[0].Step)
	}
}

func TestReleaseHandler_OversizedBody(t *testing.T) {
	// Body larger than 1KB should be rejected.
	big := `{"version": "` + strings.Repeat("x", 2048) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ReleaseHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized body, got %d", rec.Code)
	}
}

func TestReleaseHandler_RelativeForgeBin(t *testing.T) {
	t.Setenv("FORGE_BIN", "relative/path/forge")
	t.Setenv("HYTTE_REPO_DIR", "/tmp/test-repo")

	body := `{"version": "1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ReleaseHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for relative forge path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRepoRoot_EnvOverride(t *testing.T) {
	t.Setenv("HYTTE_REPO_DIR", "/custom/path")
	dir, err := repoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/custom/path" {
		t.Errorf("repoRoot() = %q, want %q", dir, "/custom/path")
	}
}

func TestRepoRoot_FallbackValidatesGoMod(t *testing.T) {
	// When HYTTE_REPO_DIR is not set, repoRoot() falls back to git rev-parse
	// and then validates that the directory contains go.mod. If we run this
	// test from the repo root (which has go.mod), it should succeed.
	// If run from outside a git repo, it fails gracefully — skip in that case.
	t.Setenv("HYTTE_REPO_DIR", "")
	dir, err := repoRoot()
	if err != nil {
		// Expected when running outside the source repo (e.g. CI with a
		// deployment layout). The error should mention HYTTE_REPO_DIR.
		if !strings.Contains(err.Error(), "HYTTE_REPO_DIR") {
			t.Errorf("expected error to mention HYTTE_REPO_DIR, got: %v", err)
		}
		return
	}
	// If it succeeds, the directory must contain go.mod.
	if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr != nil {
		t.Errorf("repoRoot() returned %q which does not contain go.mod", dir)
	}
}

func TestSemverPattern(t *testing.T) {
	valid := []string{"0.0.0", "1.0.0", "1.2.3", "10.20.30", "0.1.0"}
	invalid := []string{"v1.0.0", "1.0", "1", "1.2.3.4", "01.0.0", "1.02.3", "abc", "1.0.0-beta"}

	for _, v := range valid {
		if !semverPattern.MatchString(v) {
			t.Errorf("expected %q to be valid semver", v)
		}
	}
	for _, v := range invalid {
		if semverPattern.MatchString(v) {
			t.Errorf("expected %q to be invalid semver", v)
		}
	}
}
