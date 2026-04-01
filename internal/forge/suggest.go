package forge

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SuggestResponse is the JSON response from the version suggestion endpoint.
type SuggestResponse struct {
	CurrentVersion   string            `json:"current_version"`
	SuggestedVersion string            `json:"suggested_version"`
	SuggestedBump    string            `json:"suggested_bump"`
	ChangelogPreview []FragmentSummary `json:"changelog_preview"`
}

// FragmentSummary describes a single changelog fragment file.
type FragmentSummary struct {
	File     string `json:"file"`
	Category string `json:"category"`
	Summary  string `json:"summary"`
}

// SuggestHandler returns the suggested next version based on the latest git tag
// and the changelog fragment files in changelog.d/.
func SuggestHandler(runner CommandRunner) http.HandlerFunc {
	if runner == nil {
		runner = execRunner{}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		repoDir, err := forgeRepoRoot()
		if err != nil {
			log.Printf("forge: forgeRepoRoot failed: %v", err)
			writeError(w, http.StatusInternalServerError, "FORGE_REPO_DIR is not set or invalid; check server configuration")
			return
		}

		ctx := r.Context()

		currentVersion, err := latestVersion(ctx, runner, repoDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to determine current version")
			return
		}

		fragments, err := readFragments(repoDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read changelog fragments")
			return
		}

		bump := determineBump(fragments)
		suggested := bumpVersion(currentVersion, bump)

		writeJSON(w, http.StatusOK, SuggestResponse{
			CurrentVersion:   currentVersion,
			SuggestedVersion: suggested,
			SuggestedBump:    bump,
			ChangelogPreview: fragments,
		})
	}
}

// latestVersion retrieves the most recent semver tag from git.
// Returns "0.0.0" if no tags exist.
func latestVersion(ctx context.Context, runner CommandRunner, repoDir string) (string, error) {
	out, err := runner.Run(ctx, repoDir, "git", "tag", "--sort=-v:refname")
	if err != nil {
		return "", fmt.Errorf("git tag failed: %v", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return "0.0.0", nil
	}

	// Find the first line that looks like a semver tag (v1.2.3).
	for _, line := range strings.Split(out, "\n") {
		tag := strings.TrimSpace(line)
		ver := strings.TrimPrefix(tag, "v")
		if semverPattern.MatchString(ver) {
			return ver, nil
		}
	}

	return "0.0.0", nil
}

// readFragments reads all .md files in changelog.d/ and extracts category and summary.
func readFragments(repoDir string) ([]FragmentSummary, error) {
	changelogDir := filepath.Join(repoDir, "changelog.d")
	entries, err := os.ReadDir(changelogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []FragmentSummary{}, nil
		}
		return nil, err
	}

	var fragments []FragmentSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		category, summary, err := parseFragment(filepath.Join(changelogDir, entry.Name()))
		if err != nil {
			// Surface malformed fragments to callers instead of silently skipping them.
			fragments = append(fragments, FragmentSummary{
				File:     entry.Name(),
				Category: "unknown",
				Summary:  fmt.Sprintf("failed to parse fragment: %v", err),
			})
			continue
		}

		fragments = append(fragments, FragmentSummary{
			File:     entry.Name(),
			Category: category,
			Summary:  summary,
		})
	}

	if fragments == nil {
		fragments = []FragmentSummary{}
	}
	return fragments, nil
}

// parseFragment reads a changelog fragment file and returns category and summary.
func parseFragment(path string) (string, string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var category string
	var summaryLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if category == "" && strings.HasPrefix(line, "category:") {
			category = strings.TrimSpace(strings.TrimPrefix(line, "category:"))
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			summaryLines = append(summaryLines, trimmed)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("failed to read fragment %s: %w", path, err)
	}

	if category == "" {
		return "", "", fmt.Errorf("no category found")
	}

	summary := strings.Join(summaryLines, " ")
	// Truncate very long summaries for the preview, counting runes to keep valid UTF-8.
	runes := []rune(summary)
	if len(runes) > 200 {
		summary = string(runes[:200]) + "…"
	}
	return category, summary, nil
}

// determineBump decides the version bump type based on changelog fragment categories.
// Breaking → major, Added/Removed → minor, everything else (Fixed, Changed, Deprecated, Security) → patch.
func determineBump(fragments []FragmentSummary) string {
	if len(fragments) == 0 {
		return "patch"
	}

	hasMajor := false
	hasMinor := false
	for _, f := range fragments {
		cat := strings.ToLower(f.Category)
		switch cat {
		case "breaking":
			hasMajor = true
		case "added", "removed":
			hasMinor = true
		}
	}

	if hasMajor {
		return "major"
	}
	if hasMinor {
		return "minor"
	}
	return "patch"
}

// bumpVersion increments the given semver string by the specified bump type.
func bumpVersion(version, bump string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return version
	}

	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])

	switch bump {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	default:
		patch++
	}

	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}
