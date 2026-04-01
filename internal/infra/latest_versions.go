package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const latestVersionCacheTTL = 1 * time.Hour

// latestVersionCache stores fetched latest versions with expiry tracking.
type latestVersionCache struct {
	mu        sync.Mutex
	data      map[string]string
	fetchedAt time.Time
}

var (
	latestCacheInstance latestVersionCache
	latestVersionsGroup singleflight.Group
)

// latestVersionFetcher fetches the latest available version for a tool from
// an upstream source. The http.Client is provided so tests can inject a stub.
type latestVersionFetcher func(ctx context.Context, client *http.Client) (string, error)

// beadsRepoID is the numeric GitHub repository ID for steveyegge/beads (bd).
// Using the repo ID avoids 301 redirects from the GitHub API when a repository
// has been transferred or renamed (e.g. after ownership moves between accounts).
const beadsRepoID int64 = 1074561042

// latestVersionFetchers maps tool names to their upstream fetcher functions.
// Tools updated via apt (git, gh, node) check the apt candidate version rather
// than upstream GitHub/website releases, because the update command runs
// apt-get install — showing a newer upstream version is misleading when apt
// cannot actually provide it.
func latestVersionFetchers() map[string]latestVersionFetcher {
	return map[string]latestVersionFetcher{
		"forge":  makeGitHubReleaseFetcher("Robin831", "Forge"),
		"bd":     makeGitHubRepoIDReleaseFetcher(beadsRepoID),
		"gh":     makeAptCandidateFetcher("gh"),
		"dolt":   makeGitHubReleaseFetcher("dolthub", "dolt"),
		"go":     fetchLatestGo,
		"node":   makeAptCandidateFetcher("nodejs"),
		"npm":    fetchLatestNpm,
		"git":    makeAptCandidateFetcher("git"),
		"claude": fetchLatestClaude,
	}
}

// doGitHubRequest performs a GET to the given GitHub API URL, sets the
// required Accept and User-Agent headers, limits the response body to 1 MiB,
// and returns the raw body bytes. It returns an error for non-200 responses.
func doGitHubRequest(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Hytte/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: 1<<20 + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > 1<<20 {
		return nil, fmt.Errorf("response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// makeGitHubReleaseFetcher returns a fetcher that queries the GitHub releases
// API for the latest release tag of the given owner/repo.
func makeGitHubReleaseFetcher(owner, repo string) latestVersionFetcher {
	return func(ctx context.Context, client *http.Client) (string, error) {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
		body, err := doGitHubRequest(ctx, client, url)
		if err != nil {
			return "", err
		}
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}
		return release.TagName, nil
	}
}

// makeGitHubRepoIDReleaseFetcher returns a fetcher that queries the GitHub
// releases API using a numeric repository ID. This avoids 301 redirects that
// occur when a repository has been transferred or renamed.
func makeGitHubRepoIDReleaseFetcher(repoID int64) latestVersionFetcher {
	return func(ctx context.Context, client *http.Client) (string, error) {
		url := fmt.Sprintf("https://api.github.com/repositories/%d/releases/latest", repoID)
		body, err := doGitHubRequest(ctx, client, url)
		if err != nil {
			return "", err
		}
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}
		return release.TagName, nil
	}
}

// gitStableTagRe matches Git stable version tags like "v2.45.0" but not
// release candidates like "v2.45.0-rc0".
var gitStableTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// fetchLatestGitTag queries the GitHub tags API for the git/git repo and
// returns the highest stable version tag by explicit semver comparison,
// filtering out release candidates. The git/git repo does not use GitHub
// Releases, only tags. Explicit comparison avoids relying on GitHub's tag
// ordering or the latest stable tag being within the first page.
func fetchLatestGitTag(ctx context.Context, client *http.Client) (string, error) {
	body, err := doGitHubRequest(ctx, client, "https://api.github.com/repos/git/git/tags?per_page=100")
	if err != nil {
		return "", err
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	// Parse all stable tags and select the maximum version by explicit
	// comparison so correctness does not depend on GitHub's sort order.
	bestTag := ""
	var bestMajor, bestMinor, bestPatch int
	for _, tag := range tags {
		if !gitStableTagRe.MatchString(tag.Name) {
			continue
		}
		var major, minor, patch int
		if _, err := fmt.Sscanf(tag.Name, "v%d.%d.%d", &major, &minor, &patch); err != nil {
			continue
		}
		if bestTag == "" ||
			major > bestMajor ||
			(major == bestMajor && minor > bestMinor) ||
			(major == bestMajor && minor == bestMinor && patch > bestPatch) {
			bestMajor, bestMinor, bestPatch = major, minor, patch
			bestTag = tag.Name
		}
	}
	if bestTag == "" {
		return "", fmt.Errorf("no stable git tag found")
	}
	return bestTag, nil
}

// aptVersionExtractRe extracts the version number from an apt-cache policy
// Candidate value, stripping the epoch prefix and Debian/Ubuntu revision suffix.
// Example: "1:2.43.0-1ubuntu7.2" → "2.43.0"
var aptVersionExtractRe = regexp.MustCompile(`(\d+\.\d+[\d.]*)`)

// aptCandidateRunner abstracts the apt-cache policy command for testing.
// Production code uses defaultAptCandidateRunner; tests inject a stub.
var aptCandidateRunner func(ctx context.Context, pkg string) ([]byte, error) = defaultAptCandidateRunner

func defaultAptCandidateRunner(ctx context.Context, pkg string) ([]byte, error) {
	cmdPath := resolveCommand("apt-cache")
	return exec.CommandContext(ctx, cmdPath, "policy", pkg).CombinedOutput()
}

// makeAptCandidateFetcher returns a fetcher that queries `apt-cache policy`
// for the candidate (installable) version of the given package. This gives the
// version that `apt-get install <pkg>` would actually install, avoiding
// misleading "Update available" when the upstream source release is newer than
// what the configured apt repositories provide.
func makeAptCandidateFetcher(pkg string) latestVersionFetcher {
	return func(ctx context.Context, _ *http.Client) (string, error) {
		out, err := aptCandidateRunner(ctx, pkg)
		if err != nil {
			// Include (truncated) apt-cache output to aid debugging missing packages / misconfig.
			outStr := strings.TrimSpace(string(out))
			const maxOutputLen = 1024
			if len(outStr) > maxOutputLen {
				outStr = outStr[:maxOutputLen] + "..."
			}
			if outStr != "" {
				return "", fmt.Errorf("apt-cache policy %s failed: %w; output: %s", pkg, err, outStr)
			}
			return "", fmt.Errorf("apt-cache policy %s failed: %w", pkg, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "Candidate:") {
				continue
			}
			candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:"))
			if candidate == "(none)" || candidate == "" {
				return "", fmt.Errorf("no apt candidate for %s", pkg)
			}
			// Extract the clean version number, stripping epoch and revision.
			m := aptVersionExtractRe.FindString(candidate)
			if m == "" {
				return "", fmt.Errorf("could not parse version from apt candidate %q", candidate)
			}
			return m, nil
		}
		return "", fmt.Errorf("no Candidate line in apt-cache policy output for %s", pkg)
	}
}

// fetchLatestGo queries go.dev for the latest stable Go version.
func fetchLatestGo(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://go.dev/dl/?mode=json", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: 1<<20 + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > 1<<20 {
		return "", fmt.Errorf("response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	for _, r := range releases {
		if r.Stable {
			return r.Version, nil
		}
	}
	return "", fmt.Errorf("no stable Go release found")
}

// aptCommandRunner is a small abstraction over command execution so that
// callers like fetchLatestNode can be unit tested by stubbing it.
type aptCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// nodeAptCommandRunner is the runner used by fetchLatestNode. Tests may
// override this to avoid shelling out.
var nodeAptCommandRunner aptCommandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// aptCandidateRe matches the "Candidate: <version>" line in apt-cache policy output.
var aptCandidateRe = regexp.MustCompile(`Candidate:\s+(\S+)`)

// aptVersionRe extracts the semver portion from an apt version string like
// "22.14.0-1nodesource1" → "22.14.0".
var aptVersionRe = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// parseAptPolicyCandidate extracts the semver version string from the output
// of "apt-cache policy nodejs", returning a "vX.Y.Z" string or an error.
func parseAptPolicyCandidate(out []byte) (string, error) {
	candidateMatch := aptCandidateRe.FindSubmatch(out)
	if candidateMatch == nil {
		return "", fmt.Errorf("no candidate version in apt-cache policy output")
	}

	candidate := string(candidateMatch[1])
	if candidate == "(none)" {
		return "", fmt.Errorf("nodejs has no apt candidate version")
	}

	// Extract the semver portion (e.g. "22.14.0" from "22.14.0-1nodesource1").
	semverMatch := aptVersionRe.FindString(candidate)
	if semverMatch == "" {
		return "", fmt.Errorf("cannot parse semver from apt candidate %q", candidate)
	}

	return "v" + semverMatch, nil
}

// fetchLatestNode queries apt-cache for the latest available Node.js version
// in the configured apt repository, rather than checking nodejs.org for the
// absolute latest. This avoids misleading "update available" messages when
// the installed version matches the pinned major line in the apt source.
func fetchLatestNode(ctx context.Context, _ *http.Client) (string, error) {
	out, err := nodeAptCommandRunner(ctx, "apt-cache", "policy", "nodejs")
	if err != nil {
		return "", fmt.Errorf("apt-cache policy nodejs: %w", err)
	}
	return parseAptPolicyCandidate(out)
}

// fetchLatestNpm queries the npm registry for the latest npm version.
func fetchLatestNpm(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://registry.npmjs.org/npm/latest", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: 1<<20 + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > 1<<20 {
		return "", fmt.Errorf("response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return pkg.Version, nil
}

// fetchLatestClaude queries the npm registry for the latest published version
// of @anthropic-ai/claude-code. This replaces the previous approach of shelling
// out to `claude update --check` which does not exist as a valid flag.
func fetchLatestClaude(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://registry.npmjs.org/@anthropic-ai/claude-code/latest", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: 1<<20 + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > 1<<20 {
		return "", fmt.Errorf("response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("empty version in npm registry response")
	}
	return pkg.Version, nil
}

// getLatestVersions fetches the latest available versions for all tools.
// Results are cached for 1 hour. On fetch failure for individual tools,
// the stale cached value is kept and the cache TTL is extended to avoid
// hammering a failing upstream.
func getLatestVersions(client *http.Client, fetchers map[string]latestVersionFetcher) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := make(map[string]string, len(fetchers))

	type fetchResult struct {
		name    string
		version string
		err     error
	}

	ch := make(chan fetchResult, len(fetchers))
	for name, fetcher := range fetchers {
		go func(n string, f latestVersionFetcher) {
			v, err := f(ctx, client)
			ch <- fetchResult{name: n, version: v, err: err}
		}(name, fetcher)
	}

	for range fetchers {
		fr := <-ch
		if fr.err != nil {
			log.Printf("latest_versions: fetch %q failed: %v", fr.name, fr.err)
			// Fall back to stale cached value if available.
			// Copy the value while holding the lock to avoid reading after release.
			staleVal := ""
			latestCacheInstance.mu.Lock()
			if latestCacheInstance.data != nil {
				if sv, ok := latestCacheInstance.data[fr.name]; ok && sv != "" {
					staleVal = sv
				}
			}
			latestCacheInstance.mu.Unlock()
			if staleVal != "" {
				result[fr.name] = staleVal
			} else {
				result[fr.name] = "unknown"
			}
		} else {
			result[fr.name] = fr.version
		}
	}

	return result
}

// copyMap returns a shallow copy of m so the caller can use it without holding
// the cache lock.
func copyMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// latestVersionEntry is used for sorted JSON output of latest-version results.
// (Named differently from versionEntry in versions.go which tracks CLI commands.)
type latestVersionEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func sortedVersions(m map[string]string) []latestVersionEntry {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]latestVersionEntry, len(keys))
	for i, k := range keys {
		entries[i] = latestVersionEntry{Name: k, Version: m[k]}
	}
	return entries
}

// latestVersionsHandlerWith is the testable core of LatestVersionsHandler.
func latestVersionsHandlerWith(client *http.Client, fetchers map[string]latestVersionFetcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		latestCacheInstance.mu.Lock()
		if latestCacheInstance.data != nil && time.Since(latestCacheInstance.fetchedAt) < latestVersionCacheTTL {
			// Copy the map while holding the lock to avoid reading mutated data
			// after unlock (warden: never read lock-protected fields after releasing).
			data := copyMap(latestCacheInstance.data)
			latestCacheInstance.mu.Unlock()
			writeJSON(w, http.StatusOK, sortedVersions(data))
			return
		}
		latestCacheInstance.mu.Unlock()

		v, _, _ := latestVersionsGroup.Do("fetch-latest", func() (any, error) {
			versions := getLatestVersions(client, fetchers)

			latestCacheInstance.mu.Lock()
			// Update the shared cache and timestamp after a singleflight fetch
			// so subsequent requests can serve cached results.
			latestCacheInstance.data = versions
			latestCacheInstance.fetchedAt = time.Now()
			latestCacheInstance.mu.Unlock()

			return versions, nil
		})

		writeJSON(w, http.StatusOK, sortedVersions(v.(map[string]string)))
	}
}

// LatestVersionsHandler returns a JSON array of {name, version} entries,
// sorted alphabetically by tool name, representing the latest available
// upstream versions. Results are cached for 1 hour. Concurrent requests
// share a single fetch via singleflight.
func LatestVersionsHandler() http.HandlerFunc {
	client := &http.Client{Timeout: 15 * time.Second}
	return latestVersionsHandlerWith(client, latestVersionFetchers())
}

// truncate returns at most maxLen characters of s, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
