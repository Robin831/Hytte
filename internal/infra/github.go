package infra

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// GitHubRepo represents a configured GitHub repository to monitor.
type GitHubRepo struct {
	ID        int64  `json:"id"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
}

// GitHubWorkflowRun represents a workflow run from the GitHub API.
type GitHubWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Branch     string `json:"branch"`
	Event      string `json:"event"`
	CreatedAt  string `json:"created_at"`
	HTMLURL    string `json:"html_url"`
}

// GitHubRepoResult holds workflow run results for a single repository.
type GitHubRepoResult struct {
	Owner    string              `json:"owner"`
	Repo     string              `json:"repo"`
	Status   string              `json:"status"`
	Error    string              `json:"error,omitempty"`
	Runs     []GitHubWorkflowRun `json:"runs"`
}

// GitHubActionsModule monitors GitHub Actions workflow status.
type GitHubActionsModule struct {
	db      *sql.DB
	client  *http.Client
	baseURL string // overridable for tests; defaults to https://api.github.com
}

// NewGitHubActionsModule creates a GitHub Actions monitoring module.
// The HTTP client uses safeDialContext to prevent SSRF attacks by validating
// resolved IPs at connection time.
func NewGitHubActionsModule(db *sql.DB) *GitHubActionsModule {
	return &GitHubActionsModule{
		db: db,
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: safeDialContext,
				Proxy:       nil,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (m *GitHubActionsModule) Name() string        { return "github_actions" }
func (m *GitHubActionsModule) DisplayName() string { return "GitHub Actions" }
func (m *GitHubActionsModule) Description() string {
	return "Monitor GitHub Actions workflow run status"
}

// Check fetches recent workflow runs for all configured repositories.
func (m *GitHubActionsModule) Check(userID int64) ModuleResult {
	token, err := GetGitHubToken(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   "GitHub token configuration error: " + err.Error(),
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"repos": []GitHubRepoResult{}},
		}
	}
	if token == "" {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No GitHub token configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"repos": []GitHubRepoResult{}},
		}
	}

	repos, err := ListGitHubRepos(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to load repositories",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(repos) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No repositories configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"repos": []GitHubRepoResult{}},
		}
	}

	results := make([]GitHubRepoResult, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(idx int, r GitHubRepo) {
			defer wg.Done()
			results[idx] = m.checkRepo(token, r)
		}(i, repo)
	}
	wg.Wait()

	failedRepos := 0
	errorRepos := 0
	for _, result := range results {
		target := result.Owner + "/" + result.Repo
		if result.Error != "" {
			errorRepos++
			if err := RecordCheck(m.db, userID, m.Name(), target, StatusDown, result.Error); err != nil {
				log.Printf("infra: failed to record GitHub check for %s: %v", target, err)
			}
		} else {
			status := StatusOK
			for _, run := range result.Runs {
				if run.Conclusion == "failure" {
					status = StatusDegraded
					break
				}
			}
			if status == StatusDegraded {
				failedRepos++
			}
			if err := RecordCheck(m.db, userID, m.Name(), target, status, result.Status); err != nil {
				log.Printf("infra: failed to record GitHub check for %s: %v", target, err)
			}
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d repositories monitored", len(repos))
	if errorRepos == len(repos) {
		overall = StatusDown
		msg = "All repositories unreachable"
	} else if errorRepos > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d repositories unreachable", errorRepos, len(repos))
	} else if failedRepos > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d repositories have failing workflows", failedRepos, len(repos))
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"repos": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *GitHubActionsModule) checkRepo(token string, repo GitHubRepo) GitHubRepoResult {
	result := GitHubRepoResult{
		Owner: repo.Owner,
		Repo:  repo.Repo,
	}

	base := m.baseURL
	if base == "" {
		base = "https://api.github.com"
	}

	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=100&status=completed", base, repo.Owner, repo.Repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.client.Do(req)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	const maxResponseSize int64 = 1 << 20
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseSize + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("read: %v", err)
		return result
	}
	if int64(len(body)) > maxResponseSize {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("response body too large (>%d bytes)", maxResponseSize)
		return result
	}

	if resp.StatusCode != http.StatusOK {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		return result
	}

	var apiResp struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HeadBranch string `json:"head_branch"`
			Event      string `json:"event"`
			CreatedAt  string `json:"created_at"`
			HTMLURL    string `json:"html_url"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("decode: %v", err)
		return result
	}

	// Group by workflow name and keep only the most recent run per workflow.
	// GitHub API returns runs sorted by created_at desc, so the first seen per name is the latest.
	// Collect at most 5 unique workflows.
	const maxWorkflows = 5
	seen := make(map[string]bool)
	result.Status = string(StatusOK)
	result.Runs = make([]GitHubWorkflowRun, 0)
	for _, run := range apiResp.WorkflowRuns {
		if seen[run.Name] {
			continue
		}
		if len(seen) >= maxWorkflows {
			break
		}
		seen[run.Name] = true
		if run.Conclusion == "failure" {
			result.Status = string(StatusDegraded)
		}
		result.Runs = append(result.Runs, GitHubWorkflowRun{
			ID:         run.ID,
			Name:       run.Name,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			Branch:     run.HeadBranch,
			Event:      run.Event,
			CreatedAt:  run.CreatedAt,
			HTMLURL:    run.HTMLURL,
		})
	}

	return result
}

// --- Database operations ---

// HasGitHubToken checks whether a GitHub token is configured for userID
// without decrypting it.
func HasGitHubToken(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM infra_github_config WHERE user_id = ?`,
		userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetGitHubToken returns the stored GitHub token for userID, decrypting it.
func GetGitHubToken(db *sql.DB, userID int64) (string, error) {
	var encrypted string
	err := db.QueryRow(
		`SELECT api_token FROM infra_github_config WHERE user_id = ?`,
		userID,
	).Scan(&encrypted)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return DecryptToken(encrypted)
}

// SetGitHubToken encrypts and upserts the GitHub token for userID.
func SetGitHubToken(db *sql.DB, userID int64, token string) error {
	encrypted, err := EncryptToken(token)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO infra_github_config (user_id, api_token, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET api_token = excluded.api_token, updated_at = excluded.updated_at`,
		userID, encrypted, now,
	)
	return err
}

// DeleteGitHubToken removes the GitHub token for userID.
func DeleteGitHubToken(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM infra_github_config WHERE user_id = ?`, userID)
	return err
}

// ListGitHubRepos returns all GitHub repos configured for userID.
func ListGitHubRepos(db *sql.DB, userID int64) ([]GitHubRepo, error) {
	rows, err := db.Query(
		`SELECT id, owner, repo, created_at FROM infra_github_repos WHERE user_id = ? ORDER BY owner, repo`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repos := make([]GitHubRepo, 0)
	for rows.Next() {
		var r GitHubRepo
		if err := rows.Scan(&r.ID, &r.Owner, &r.Repo, &r.CreatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// AddGitHubRepo adds a repository to monitor for userID.
func AddGitHubRepo(db *sql.DB, userID int64, owner, repo string) (GitHubRepo, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_github_repos (user_id, owner, repo, created_at) VALUES (?, ?, ?, ?)`,
		userID, owner, repo, now,
	)
	if err != nil {
		return GitHubRepo{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return GitHubRepo{}, err
	}
	return GitHubRepo{ID: id, Owner: owner, Repo: repo, CreatedAt: now}, nil
}

// DeleteGitHubRepo removes a repository by ID, scoped to userID.
func DeleteGitHubRepo(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_github_repos WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- HTTP handlers ---

// GitHubTokenGetHandler returns whether a GitHub token is configured (masked).
func GitHubTokenGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		configured, err := HasGitHubToken(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get token")
			return
		}
		masked := ""
		if configured {
			masked = "****"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": configured,
			"masked":     masked,
		})
	}
}

// GitHubTokenSetHandler stores or updates the GitHub token.
func GitHubTokenSetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Token = strings.TrimSpace(body.Token)
		if body.Token == "" {
			writeError(w, http.StatusBadRequest, "token is required")
			return
		}

		if err := SetGitHubToken(db, user.ID, body.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GitHubTokenDeleteHandler removes the stored GitHub token.
func GitHubTokenDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := DeleteGitHubToken(db, user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ListGitHubReposHandler returns all configured GitHub repos for the authenticated user.
func ListGitHubReposHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		repos, err := ListGitHubRepos(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list repositories")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
	}
}

// AddGitHubRepoHandler adds a GitHub repository to monitor.
func AddGitHubRepoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Owner = strings.TrimSpace(body.Owner)
		body.Repo = strings.TrimSpace(body.Repo)

		if body.Owner == "" || body.Repo == "" {
			writeError(w, http.StatusBadRequest, "owner and repo are required")
			return
		}

		if err := ValidateGitHubOwnerRepo(body.Owner, body.Repo); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		repo, err := AddGitHubRepo(db, user.ID, body.Owner, body.Repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add repository")
			return
		}
		writeJSON(w, http.StatusCreated, repo)
	}
}

// DeleteGitHubRepoHandler removes a GitHub repository.
func DeleteGitHubRepoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteGitHubRepo(db, user.ID, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "repository not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete repository")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
