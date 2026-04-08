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
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ExternalPR represents a GitHub pull request not tracked by forge.
type ExternalPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Anvil      string `json:"anvil"`
	Branch     string `json:"branch"`
	BaseBranch string `json:"base_branch"`
	Author     string `json:"author"`
	URL        string `json:"url"`
	IsDraft    bool   `json:"is_draft"`
}

// AllPRsResponse contains all forge-tracked and external PRs returned by the API.
// Any grouping (e.g., by anvil) is performed client-side.
type AllPRsResponse struct {
	ForgePRs        []PR         `json:"forge_prs"`
	ExternalPRs     []ExternalPR `json:"external_prs"`
	RecentlyMerged  []PR         `json:"recently_merged"`
}

// ghPR is the JSON shape returned by `gh pr list --json`.
type ghPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	URL         string `json:"url"`
	IsDraft     bool   `json:"isDraft"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

// prCache holds cached external PRs with a TTL.
type prCache struct {
	mu       sync.Mutex
	data     []ExternalPR
	fetchedAt time.Time
	ttl      time.Duration
}

var externalPRCache = &prCache{ttl: 60 * time.Second}

// repoFromRemote extracts the GitHub "owner/repo" from a git remote URL.
func repoFromRemote(repoPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url: %w; output: %s", err, strings.TrimSpace(string(out)))
	}
	return parseGitHubRepo(strings.TrimSpace(string(out))), nil
}

// parseGitHubRepo extracts "owner/repo" from a GitHub remote URL.
func parseGitHubRepo(remote string) string {
	// SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(remote, "git@github.com:") {
		repo := strings.TrimPrefix(remote, "git@github.com:")
		repo = strings.TrimSuffix(repo, ".git")
		return repo
	}
	// HTTPS: https://github.com/owner/repo.git
	if strings.Contains(remote, "github.com/") {
		idx := strings.Index(remote, "github.com/")
		repo := remote[idx+len("github.com/"):]
		repo = strings.TrimSuffix(repo, ".git")
		return repo
	}
	return remote
}

// fetchExternalPRs fetches all open PRs from GitHub for each configured anvil,
// then filters out forge-tracked PRs to return only external ones.
// The cache mutex is only held while reading/writing cached data, not during CLI calls.
func fetchExternalPRs(forgePRs []PR) ([]ExternalPR, error) {
	externalPRCache.mu.Lock()
	if time.Since(externalPRCache.fetchedAt) < externalPRCache.ttl && externalPRCache.data != nil {
		// Re-filter against current forge PRs since forge state may have changed
		cached := externalPRCache.data
		externalPRCache.mu.Unlock()
		return filterExternal(cached, forgePRs), nil
	}
	externalPRCache.mu.Unlock()

	// Read forge config to get anvil paths
	cfgPath, err := configPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}

	forgeDir := filepath.Join(home, ".forge")
	if err := isRegularDir(forgeDir); err != nil {
		return nil, nil // No forge dir, no external PRs to fetch
	}
	if err := isRegularFile(cfgPath); err != nil {
		return nil, nil
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ForgeConfig
	if err := parseConfigYAML(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	ghPath := resolveCommand("gh")

	var allExternal []ExternalPR

	for _, anvil := range cfg.Anvils {
		if anvil.Path == "" {
			continue
		}
		repo, err := repoFromRemote(anvil.Path)
		if err != nil {
			log.Printf("forge: cannot resolve repo for %s: %v", anvil.Path, err)
			continue
		}
		if repo == "" || !strings.Contains(repo, "/") {
			continue
		}

		prs, err := ghListPRs(ghPath, repo)
		if err != nil {
			log.Printf("forge: gh pr list for %s failed: %v", repo, err)
			continue
		}

		for _, pr := range prs {
			allExternal = append(allExternal, ExternalPR{
				Number:     pr.Number,
				Title:      pr.Title,
				Anvil:      repo,
				Branch:     pr.HeadRefName,
				BaseBranch: pr.BaseRefName,
				Author:     pr.Author.Login,
				URL:        pr.URL,
				IsDraft:    pr.IsDraft,
			})
		}
	}

	// Cache ALL GitHub PRs (before filtering) so we can re-filter on next request.
	// Lock only for the cache write, not during the CLI calls above.
	externalPRCache.mu.Lock()
	externalPRCache.data = allExternal
	externalPRCache.fetchedAt = time.Now()
	externalPRCache.mu.Unlock()

	return filterExternal(allExternal, forgePRs), nil
}

// filterExternal removes PRs that are tracked by forge from the list.
func filterExternal(allGitHub []ExternalPR, forgePRs []PR) []ExternalPR {
	// Build a set of forge PR numbers keyed by both the short anvil name
	// (e.g. "hytte") and the full repo (e.g. "Robin831/Hytte") since the
	// external PRs use "owner/repo" while forge PRs use the short config name.
	forgeSet := make(map[int]bool, len(forgePRs))
	for _, fp := range forgePRs {
		forgeSet[fp.Number] = true
	}
	var result []ExternalPR
	for _, ep := range allGitHub {
		if !forgeSet[ep.Number] {
			result = append(result, ep)
		}
	}
	return result
}

// ghListPRs runs `gh pr list` for the given repo and returns parsed PRs.
func ghListPRs(ghPath, repo string) ([]ghPR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghPath, "pr", "list",
		"--repo", repo,
		"--state", "open",
		"--json", "number,title,headRefName,baseRefName,url,author,isDraft",
		"--limit", "100",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w; output: %s", err, strings.TrimSpace(string(out)))
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	return prs, nil
}

// parseConfigYAML unmarshals forge config YAML.
func parseConfigYAML(data []byte, cfg *ForgeConfig) error {
	return yaml.Unmarshal(data, cfg)
}

// AllPRsHandler returns all open PRs grouped into forge-tracked and external.
func AllPRsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var forgePRs []PR
		if db != nil {
			var err error
			forgePRs, err = db.PRs()
			if err != nil {
				log.Printf("forge: all-prs: failed to load forge PRs: %v", err)
				forgePRs = []PR{}
			}
		}
		if forgePRs == nil {
			forgePRs = []PR{}
		}

		// Always attempt to fetch external PRs; forgePRs may be empty when db is nil.
		external, err := fetchExternalPRs(forgePRs)
		if err != nil {
			log.Printf("forge: all-prs: failed to fetch external PRs: %v", err)
			external = []ExternalPR{}
		}
		if external == nil {
			external = []ExternalPR{}
		}

		// RecentlyMergedPRs uses last_checked (observation/polling time) as a
		// proxy for merge time, so the 24h window reflects when the merge was
		// last observed, not the exact GitHub merge timestamp.
		var recentlyMerged []PR
		if db != nil {
			since := time.Now().Add(-24 * time.Hour)
			recentlyMerged, err = db.RecentlyMergedPRs(since)
			if err != nil {
				log.Printf("forge: all-prs: failed to load recently merged PRs: %v", err)
				recentlyMerged = []PR{}
			}
		}
		if recentlyMerged == nil {
			recentlyMerged = []PR{}
		}

		writeJSON(w, http.StatusOK, AllPRsResponse{
			ForgePRs:       forgePRs,
			ExternalPRs:    external,
			RecentlyMerged: recentlyMerged,
		})
	}
}
