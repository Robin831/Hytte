package forge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func (m *mockRunner) Run(dir, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, key)
	if r, ok := m.results[key]; ok {
		return r.out, r.err
	}
	return "", nil
}

func TestReleaseHandler_ValidVersion(t *testing.T) {
	runner := newMockRunner()
	runner.Set("git pull origin main", "Already up to date.", nil)
	runner.Set("/usr/local/bin/forge changelog assemble --version 1.2.3", "Assembled changelog for v1.2.3", nil)
	runner.Set("git ls-files changelog.d/", "changelog.d/Hytte-abc1.md\nchangelog.d/Hytte-abc2.md", nil)
	runner.Set("git rm -f -- changelog.d/Hytte-abc1.md changelog.d/Hytte-abc2.md", "rm 'changelog.d/Hytte-abc1.md'\nrm 'changelog.d/Hytte-abc2.md'", nil)
	runner.Set("git ls-files --others changelog.d/", "", nil)
	runner.Set("git add -A", "", nil)
	runner.Set("git commit -m release: v1.2.3\n\nAssembled changelog and tagged v1.2.3.", "", nil)
	runner.Set("git tag -a v1.2.3 -m Release v1.2.3", "", nil)
	runner.Set("git push origin main --tags", "To github.com:user/repo.git", nil)

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
	if len(resp.Steps) != 7 {
		t.Errorf("expected 7 steps, got %d", len(resp.Steps))
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
	runner.Set("git pull origin main", "error: cannot pull", fmt.Errorf("exit status 1"))

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
	if resp.Steps[0].Step != "pull" {
		t.Errorf("expected failed step to be 'pull', got %q", resp.Steps[0].Step)
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
	runner := newMockRunner()
	runner.Set("git pull origin main", "ok", nil)

	t.Setenv("FORGE_BIN", "relative/path/forge")
	t.Setenv("HYTTE_REPO_DIR", "/tmp/test-repo")

	body := `{"version": "1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/forge/release", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ReleaseHandler(runner).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for relative forge path, got %d: %s", rec.Code, rec.Body.String())
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
