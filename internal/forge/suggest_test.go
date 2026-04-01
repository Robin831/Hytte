package forge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeFragment(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeSuggestResponse(t *testing.T, rr *httptest.ResponseRecorder) SuggestResponse {
	t.Helper()
	var resp SuggestResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestSuggestHandler_Success(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "feat-1.md", "category: Added\n- **New feature** - Details.\n")
	writeFragment(t, changelogDir, "fix-1.md", "category: Fixed\n- **Bug fix** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.2.3\nv1.2.2\nv1.2.1", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.CurrentVersion != "1.2.3" {
		t.Errorf("currentVersion = %q, want %q", resp.CurrentVersion, "1.2.3")
	}
	if resp.SuggestedBump != "minor" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "minor")
	}
	if resp.SuggestedVersion != "1.3.0" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "1.3.0")
	}
	if len(resp.ChangelogPreview) != 2 {
		t.Errorf("changelogPreview length = %d, want 2", len(resp.ChangelogPreview))
	}
}

func TestSuggestHandler_NoTags(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "fix-1.md", "category: Fixed\n- **Bug fix** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.CurrentVersion != "0.0.0" {
		t.Errorf("currentVersion = %q, want %q", resp.CurrentVersion, "0.0.0")
	}
	if resp.SuggestedBump != "patch" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "patch")
	}
	if resp.SuggestedVersion != "0.0.1" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "0.0.1")
	}
}

func TestSuggestHandler_SecurityBumpsPatch(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "sec-1.md", "category: Security\n- **Security fix** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v2.1.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.SuggestedBump != "patch" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "patch")
	}
	if resp.SuggestedVersion != "2.1.1" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "2.1.1")
	}
}

func TestSuggestHandler_RemovedBumpsMinor(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "rm-1.md", "category: Removed\n- **Removed feature** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.5.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.SuggestedBump != "minor" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "minor")
	}
	if resp.SuggestedVersion != "1.6.0" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "1.6.0")
	}
}

func TestSuggestHandler_NoFragments(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	// No changelog.d/ directory at all.
	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.0.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.SuggestedBump != "patch" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "patch")
	}
	if resp.SuggestedVersion != "1.0.1" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "1.0.1")
	}
	if len(resp.ChangelogPreview) != 0 {
		t.Errorf("changelogPreview length = %d, want 0", len(resp.ChangelogPreview))
	}
}

func TestSuggestHandler_PatchOnlyChanges(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "fix-1.md", "category: Fixed\n- **Bug fix** - Details.\n")
	writeFragment(t, changelogDir, "change-1.md", "category: Changed\n- **Change** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.5.2", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.SuggestedBump != "patch" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "patch")
	}
	if resp.SuggestedVersion != "1.5.3" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "1.5.3")
	}
}

func TestSuggestHandler_MalformedFragment(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fragment with no category line — should be surfaced as "unknown".
	writeFragment(t, changelogDir, "bad-1.md", "- **No category here** - Details.\n")
	writeFragment(t, changelogDir, "good-1.md", "category: Fixed\n- **Good fix** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.0.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if len(resp.ChangelogPreview) != 2 {
		t.Fatalf("changelogPreview length = %d, want 2", len(resp.ChangelogPreview))
	}

	// Find the malformed fragment entry.
	var found bool
	for _, f := range resp.ChangelogPreview {
		if f.File == "bad-1.md" {
			found = true
			if f.Category != "unknown" {
				t.Errorf("malformed fragment category = %q, want %q", f.Category, "unknown")
			}
		}
	}
	if !found {
		t.Error("expected malformed fragment to appear in changelog_preview")
	}

	// Malformed fragment should not trigger a non-patch bump.
	if resp.SuggestedBump != "patch" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "patch")
	}
}

func TestSuggestHandler_BreakingBumpsMajor(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	changelogDir := filepath.Join(tmpDir, "changelog.d")
	if err := os.Mkdir(changelogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFragment(t, changelogDir, "break-1.md", "category: Breaking\n- **Breaking change** - Details.\n")

	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	runner.Set("git tag --sort=-v:refname", "v1.5.2", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := decodeSuggestResponse(t, rr)

	if resp.SuggestedBump != "major" {
		t.Errorf("suggestedBump = %q, want %q", resp.SuggestedBump, "major")
	}
	if resp.SuggestedVersion != "2.0.0" {
		t.Errorf("suggestedVersion = %q, want %q", resp.SuggestedVersion, "2.0.0")
	}
}

func TestSuggestHandler_GitTagFailure(t *testing.T) {
	tmpDir := makeTempForgeRepo(t)
	t.Setenv("FORGE_REPO_DIR", tmpDir)

	runner := newMockRunner()
	// Don't set git tag result — mockRunner returns error for unexpected commands.

	req := httptest.NewRequest(http.MethodGet, "/api/forge/release/suggest", nil)
	rr := httptest.NewRecorder()

	SuggestHandler(runner).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDetermineBump(t *testing.T) {
	tests := []struct {
		name      string
		fragments []FragmentSummary
		want      string
	}{
		{"empty", nil, "patch"},
		{"fixed only", []FragmentSummary{{Category: "Fixed"}}, "patch"},
		{"changed only", []FragmentSummary{{Category: "Changed"}}, "patch"},
		{"security only", []FragmentSummary{{Category: "Security"}}, "patch"},
		{"added", []FragmentSummary{{Category: "Added"}, {Category: "Fixed"}}, "minor"},
		{"removed", []FragmentSummary{{Category: "Removed"}, {Category: "Fixed"}}, "minor"},
		{"added and removed", []FragmentSummary{{Category: "Added"}, {Category: "Removed"}}, "minor"},
		{"deprecated", []FragmentSummary{{Category: "Deprecated"}}, "patch"},
		{"breaking", []FragmentSummary{{Category: "Breaking"}}, "major"},
		{"breaking overrides minor", []FragmentSummary{{Category: "Breaking"}, {Category: "Added"}}, "major"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineBump(tt.fragments)
			if got != tt.want {
				t.Errorf("determineBump() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBumpVersion(t *testing.T) {
	tests := []struct {
		version string
		bump    string
		want    string
	}{
		{"1.2.3", "patch", "1.2.4"},
		{"1.2.3", "minor", "1.3.0"},
		{"1.2.3", "major", "2.0.0"},
		{"0.0.0", "patch", "0.0.1"},
		{"0.0.0", "minor", "0.1.0"},
		{"0.0.0", "major", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.version+"_"+tt.bump, func(t *testing.T) {
			got := bumpVersion(tt.version, tt.bump)
			if got != tt.want {
				t.Errorf("bumpVersion(%q, %q) = %q, want %q", tt.version, tt.bump, got, tt.want)
			}
		})
	}
}
