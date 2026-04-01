package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

// latestVersionFetchers maps tool names to their upstream fetcher functions.
func latestVersionFetchers() map[string]latestVersionFetcher {
	return map[string]latestVersionFetcher{
		"forge":  makeGitHubReleaseFetcher("Robin831", "Forge"),
		"bd":     makeGitHubReleaseFetcher("steveyegge", "beads"),
		"gh":     makeGitHubReleaseFetcher("cli", "cli"),
		"dolt":   makeGitHubReleaseFetcher("dolthub", "dolt"),
		"go":     fetchLatestGo,
		"node":   fetchLatestNode,
		"npm":    fetchLatestNpm,
		"git":    fetchLatestGit,
		"claude": fetchLatestClaude,
	}
}

// makeGitHubReleaseFetcher returns a fetcher that queries the GitHub releases
// API for the latest release tag of the given owner/repo.
func makeGitHubReleaseFetcher(owner, repo string) latestVersionFetcher {
	return func(ctx context.Context, client *http.Client) (string, error) {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "Hytte/1.0")

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

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}
		return release.TagName, nil
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

// fetchLatestNode queries nodejs.org for the latest LTS Node.js version.
func fetchLatestNode(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://nodejs.org/dist/index.json", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: 2<<20 + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > 2<<20 {
		return "", fmt.Errorf("response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var entries []struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"` // false or string codename
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	for _, e := range entries {
		// LTS is false for non-LTS releases, or a string codename for LTS.
		if e.LTS != nil && e.LTS != false {
			return e.Version, nil
		}
	}
	return "", fmt.Errorf("no LTS Node.js release found")
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

// fetchLatestGit queries the GitHub tags API for the git/git repository,
// since git/git does not publish proper GitHub Releases. It filters out
// release candidates (tags containing "-rc") and returns the latest stable tag.
func fetchLatestGit(ctx context.Context, client *http.Client) (string, error) {
	url := "https://api.github.com/repos/git/git/tags?per_page=50"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Hytte/1.0")

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

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	for _, tag := range tags {
		// Skip release candidates and non-version tags.
		if !strings.HasPrefix(tag.Name, "v") || strings.Contains(tag.Name, "-rc") {
			continue
		}
		return tag.Name, nil
	}
	return "", fmt.Errorf("no stable git release found")
}

// fetchLatestClaude queries the npm registry for the latest version of the
// @anthropic-ai/claude-code package, which is the distribution channel for
// the Claude CLI. This is more reliable than shelling out to `claude update
// --check`, which may not be in PATH or produce parseable output.
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
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("empty version in npm response")
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
