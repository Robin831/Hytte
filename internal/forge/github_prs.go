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
	Number               int    `json:"number"`
	Title                string `json:"title"`
	Anvil                string `json:"anvil"`
	Branch               string `json:"branch"`
	BaseBranch           string `json:"base_branch"`
	Author               string `json:"author"`
	URL                  string `json:"url"`
	IsDraft              bool   `json:"is_draft"`
	CIPassing            bool   `json:"ci_passing"`
	CIPending            bool   `json:"ci_pending"`
	HasApproval          bool   `json:"has_approval"`
	ChangesRequested     bool   `json:"changes_requested"`
	IsConflicting        bool   `json:"is_conflicting"`
	HasUnresolvedThreads bool   `json:"has_unresolved_threads"`
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
	ReviewDecision     string            `json:"reviewDecision"`
	Mergeable          string            `json:"mergeable"`
	StatusCheckRollup  []ghCheckStatus   `json:"statusCheckRollup"`
	ReviewRequests     []ghReviewRequest `json:"reviewRequests"`
}

// ghCheckStatus represents a single CI check from `statusCheckRollup`.
type ghCheckStatus struct {
	// CheckRun fields
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	// StatusContext fields
	State string `json:"state"`
}

// ghReviewRequest represents a pending review request.
type ghReviewRequest struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// prCache holds cached GitHub PRs with a TTL.
type prCache struct {
	mu          sync.Mutex
	data        []ExternalPR
	raw         map[string]ghPR    // keyed by "repo:number"
	anvilToRepo map[string]string  // short anvil name → "owner/repo"
	fetchedAt   time.Time
	ttl         time.Duration
}

var externalPRCache = &prCache{ttl: 60 * time.Second}

// ghPRKey returns a cache key for a GitHub PR.
func ghPRKey(anvil string, number int) string {
	return fmt.Sprintf("%s:%d", anvil, number)
}

// repoFromRemote extracts the GitHub "owner/repo" from a git remote URL.
// It retries once on failure to tolerate transient subprocess errors (e.g.
// brief resource contention during parallel test execution on CI).
func repoFromRemote(repoPath string) (string, error) {
	run := func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", "origin")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git remote get-url: %w; output: %s", err, strings.TrimSpace(string(out)))
		}
		return parseGitHubRepo(strings.TrimSpace(string(out))), nil
	}
	r, err := run()
	if err != nil {
		r, err = run()
	}
	return r, err
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

// ghPRResult holds the output of fetchGitHubPRs.
type ghPRResult struct {
	external    []ExternalPR
	raw         map[string]ghPR // keyed by "repo:number"
	anvilToRepo map[string]string // short anvil name → "owner/repo"
}

// fetchGitHubPRs fetches all open PRs from GitHub for each configured anvil.
// Returns the external PRs (filtered to exclude forge-tracked ones) and raw
// GitHub PR data for enriching forge PRs.
// The cache mutex is only held while reading/writing cached data, not during CLI calls.
func fetchGitHubPRs(forgePRs []PR) (*ghPRResult, error) {
	externalPRCache.mu.Lock()
	if time.Since(externalPRCache.fetchedAt) < externalPRCache.ttl && externalPRCache.data != nil {
		// Re-filter against current forge PRs since forge state may have changed
		cached := externalPRCache.data
		raw := externalPRCache.raw
		a2r := externalPRCache.anvilToRepo
		externalPRCache.mu.Unlock()
		return &ghPRResult{external: filterExternal(cached, forgePRs, a2r), raw: raw, anvilToRepo: a2r}, nil
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
	rawMap := make(map[string]ghPR)
	anvilToRepo := make(map[string]string)

	for anvilName, anvil := range cfg.Anvils {
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
		anvilToRepo[strings.ToLower(anvilName)] = repo

		prs, err := ghListPRs(ghPath, repo)
		if err != nil {
			log.Printf("forge: gh pr list for %s failed: %v", repo, err)
			continue
		}

		for _, pr := range prs {
			rawMap[ghPRKey(repo, pr.Number)] = pr
			ep := ExternalPR{
				Number:     pr.Number,
				Title:      pr.Title,
				Anvil:      repo,
				Branch:     pr.HeadRefName,
				BaseBranch: pr.BaseRefName,
				Author:     pr.Author.Login,
				URL:        pr.URL,
				IsDraft:    pr.IsDraft,
			}
			enrichExternalPR(&ep, pr)
			allExternal = append(allExternal, ep)
		}
	}

	// Cache ALL GitHub PRs (before filtering) so we can re-filter on next request.
	// Lock only for the cache write, not during the CLI calls above.
	externalPRCache.mu.Lock()
	externalPRCache.data = allExternal
	externalPRCache.raw = rawMap
	externalPRCache.anvilToRepo = anvilToRepo
	externalPRCache.fetchedAt = time.Now()
	externalPRCache.mu.Unlock()

	return &ghPRResult{
		external:    filterExternal(allExternal, forgePRs, anvilToRepo),
		raw:         rawMap,
		anvilToRepo: anvilToRepo,
	}, nil
}

// filterExternal removes PRs that are tracked by forge from the list.
// anvilToRepo maps short anvil names (e.g. "hytte") to "owner/repo" strings,
// which is the same value stored in ExternalPR.Anvil. Using a composite
// "owner/repo:number" key prevents false-positive collisions when PR numbers
// are the same across different repos.
func filterExternal(allGitHub []ExternalPR, forgePRs []PR, anvilToRepo map[string]string) []ExternalPR {
	forgeSet := make(map[string]bool, len(forgePRs))
	for _, fp := range forgePRs {
		repo := anvilToRepo[strings.ToLower(fp.Anvil)]
		if repo != "" {
			forgeSet[fmt.Sprintf("%s:%d", repo, fp.Number)] = true
		}
	}
	var result []ExternalPR
	for _, ep := range allGitHub {
		key := fmt.Sprintf("%s:%d", ep.Anvil, ep.Number)
		if !forgeSet[key] {
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
		"--json", "number,title,headRefName,baseRefName,url,author,isDraft,reviewDecision,mergeable,statusCheckRollup,reviewRequests",
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

// ghCIStatus interprets statusCheckRollup into (passing, pending).
func ghCIStatus(checks []ghCheckStatus) (passing bool, pending bool) {
	if len(checks) == 0 {
		return false, false
	}
	allDone := true
	anyFailed := false
	for _, c := range checks {
		// StatusContext uses State; CheckRun uses Status+Conclusion
		if c.State != "" {
			switch strings.ToUpper(c.State) {
			case "SUCCESS":
				// ok
			case "PENDING", "EXPECTED":
				allDone = false
			default:
				anyFailed = true
			}
		} else {
			switch strings.ToUpper(c.Status) {
			case "COMPLETED":
				if strings.ToUpper(c.Conclusion) != "SUCCESS" && strings.ToUpper(c.Conclusion) != "NEUTRAL" && strings.ToUpper(c.Conclusion) != "SKIPPED" {
					anyFailed = true
				}
			default:
				allDone = false
			}
		}
	}
	if anyFailed {
		return false, false
	}
	if !allDone {
		return false, true
	}
	return true, false
}

// enrichExternalPR populates status fields on an ExternalPR from GitHub data.
func enrichExternalPR(ep *ExternalPR, gh ghPR) {
	ciPassing, ciPending := ghCIStatus(gh.StatusCheckRollup)
	ep.CIPassing = ciPassing
	ep.CIPending = ciPending
	ep.HasApproval = strings.EqualFold(gh.ReviewDecision, "APPROVED")
	ep.ChangesRequested = strings.EqualFold(gh.ReviewDecision, "CHANGES_REQUESTED")
	ep.IsConflicting = strings.EqualFold(gh.Mergeable, "CONFLICTING")
	ep.HasUnresolvedThreads = false // not directly available from gh pr list
}

// enrichForgePR overlays live GitHub status onto a non-bellows-managed forge PR.
func enrichForgePR(pr *PR, gh ghPR) {
	ciPassing, ciPending := ghCIStatus(gh.StatusCheckRollup)
	pr.CIPassing = ciPassing
	pr.CIPending = ciPending
	pr.HasApproval = strings.EqualFold(gh.ReviewDecision, "APPROVED")
	pr.ChangesRequested = strings.EqualFold(gh.ReviewDecision, "CHANGES_REQUESTED")
	pr.HasPendingReviews = len(gh.ReviewRequests) > 0
	pr.IsConflicting = strings.EqualFold(gh.Mergeable, "CONFLICTING")
	// has_unresolved_threads not available from gh pr list — leave as-is
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

		// Fetch all GitHub PRs; also returns raw data for enriching forge PRs.
		ghResult, err := fetchGitHubPRs(forgePRs)
		var external []ExternalPR
		if err != nil {
			log.Printf("forge: all-prs: failed to fetch external PRs: %v", err)
			external = []ExternalPR{}
		} else if ghResult != nil {
			external = ghResult.external

			// Enrich non-bellows-managed forge PRs with live GitHub status.
			for i := range forgePRs {
				if forgePRs[i].BellowsManaged {
					continue
				}
				// Map the short anvil name to the GitHub "owner/repo" for lookup.
				repo := ghResult.anvilToRepo[strings.ToLower(forgePRs[i].Anvil)]
				if repo == "" {
					continue
				}
				key := ghPRKey(repo, forgePRs[i].Number)
				if gh, ok := ghResult.raw[key]; ok {
					enrichForgePR(&forgePRs[i], gh)
				}
			}
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
