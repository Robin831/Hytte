package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- GitHub token CRUD tests ---

func TestHasGitHubToken_NoToken(t *testing.T) {
	db := setupTestDB(t)
	has, err := HasGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected no token configured")
	}
}

func TestHasGitHubToken_WithToken(t *testing.T) {
	db := setupTestDB(t)
	if err := SetGitHubToken(db, 1, "ghp_test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	has, err := HasGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected token to be configured")
	}
}

func TestGetGitHubToken_Empty(t *testing.T) {
	db := setupTestDB(t)
	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestSetAndGetGitHubToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "ghp_test123"); err != nil {
		t.Fatalf("set: %v", err)
	}

	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "ghp_test123" {
		t.Errorf("expected 'ghp_test123', got %q", token)
	}
}

func TestSetGitHubToken_Upsert(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "first"); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := SetGitHubToken(db, 1, "second"); err != nil {
		t.Fatalf("set second: %v", err)
	}

	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "second" {
		t.Errorf("expected 'second', got %q", token)
	}
}

func TestDeleteGitHubToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "to-delete"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := DeleteGitHubToken(db, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty after delete, got %q", token)
	}
}

// --- GitHub repo CRUD tests ---

func TestListGitHubRepos_Empty(t *testing.T) {
	db := setupTestDB(t)
	repos, err := ListGitHubRepos(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

func TestAddAndListGitHubRepos(t *testing.T) {
	db := setupTestDB(t)

	repo, err := AddGitHubRepo(db, 1, "octocat", "hello-world")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if repo.Owner != "octocat" || repo.Repo != "hello-world" {
		t.Errorf("unexpected repo: %+v", repo)
	}

	repos, err := ListGitHubRepos(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
}

func TestDeleteGitHubRepo(t *testing.T) {
	db := setupTestDB(t)

	repo, err := AddGitHubRepo(db, 1, "owner", "repo")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteGitHubRepo(db, 1, repo.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	repos, err := ListGitHubRepos(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos after delete, got %d", len(repos))
	}
}

func TestDeleteGitHubRepo_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteGitHubRepo(db, 1, 999)
	if err == nil {
		t.Error("expected error for non-existent repo")
	}
}

// --- GitHub module Check tests ---

func TestGitHubActionsModule_NoToken(t *testing.T) {
	db := setupTestDB(t)
	mod := NewGitHubActionsModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no token, got %s", result.Status)
	}
	if result.Name != "github_actions" {
		t.Errorf("expected name github_actions, got %s", result.Name)
	}
}

func TestGitHubActionsModule_NoRepos(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "ghp_test"); err != nil {
		t.Fatal(err)
	}

	mod := NewGitHubActionsModule(db)
	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no repos, got %s", result.Status)
	}
}

func TestGitHubActionsModule_APIError(t *testing.T) {
	db := setupTestDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer ts.Close()

	if err := SetGitHubToken(db, 1, "bad-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddGitHubRepo(db, 1, "owner", "repo"); err != nil {
		t.Fatal(err)
	}

	mod := &GitHubActionsModule{
		db:      db,
		baseURL: ts.URL,
		client:  &http.Client{},
	}

	result := mod.Check(1)
	if result.Status != StatusDown {
		t.Errorf("expected down for API error, got %s: %s", result.Status, result.Message)
	}
}

func TestGitHubActionsModule_Success(t *testing.T) {
	db := setupTestDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workflow_runs": []map[string]any{
				{
					"id":          1,
					"name":        "CI",
					"status":      "completed",
					"conclusion":  "success",
					"head_branch": "main",
					"event":       "push",
					"created_at":  "2026-03-15T10:00:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/1",
				},
				{
					"id":          2,
					"name":        "Deploy",
					"status":      "completed",
					"conclusion":  "failure",
					"head_branch": "main",
					"event":       "push",
					"created_at":  "2026-03-15T09:00:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/2",
				},
			},
		})
	}))
	defer ts.Close()

	if err := SetGitHubToken(db, 1, "ghp_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddGitHubRepo(db, 1, "owner", "repo"); err != nil {
		t.Fatal(err)
	}

	mod := &GitHubActionsModule{
		db:      db,
		baseURL: ts.URL,
		client:  &http.Client{},
	}

	result := mod.Check(1)
	// One workflow failed → overall degraded.
	if result.Status != StatusDegraded {
		t.Errorf("expected degraded (one failure), got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	repos, ok := details["repos"].([]GitHubRepoResult)
	if !ok {
		t.Fatalf("expected []GitHubRepoResult, got %T", details["repos"])
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo result, got %d", len(repos))
	}
	if len(repos[0].Runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(repos[0].Runs))
	}
}

func TestGitHubActionsModule_LatestRunPerWorkflow(t *testing.T) {
	db := setupTestDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Runs are returned newest first. The "CI" workflow has a newer success
		// that supersedes the older failure.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workflow_runs": []map[string]any{
				{
					"id":          3,
					"name":        "CI",
					"status":      "completed",
					"conclusion":  "success",
					"head_branch": "main",
					"event":       "push",
					"created_at":  "2026-03-15T12:00:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/3",
				},
				{
					"id":          2,
					"name":        "Deploy",
					"status":      "completed",
					"conclusion":  "success",
					"head_branch": "main",
					"event":       "push",
					"created_at":  "2026-03-15T11:00:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/2",
				},
				{
					"id":          1,
					"name":        "CI",
					"status":      "completed",
					"conclusion":  "failure",
					"head_branch": "main",
					"event":       "push",
					"created_at":  "2026-03-15T10:00:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/1",
				},
			},
		})
	}))
	defer ts.Close()

	if err := SetGitHubToken(db, 1, "ghp_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddGitHubRepo(db, 1, "owner", "repo"); err != nil {
		t.Fatal(err)
	}

	mod := &GitHubActionsModule{
		db:      db,
		baseURL: ts.URL,
		client:  &http.Client{},
	}

	result := mod.Check(1)
	// Latest run per workflow is all success → overall OK.
	if result.Status != StatusOK {
		t.Errorf("expected ok (old failure superseded), got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected Details to be map[string]any, got %T", result.Details)
	}
	reposAny, ok := details["repos"]
	if !ok {
		t.Fatalf("expected Details[\"repos\"] to be present")
	}
	repos, ok := reposAny.([]GitHubRepoResult)
	if !ok {
		t.Fatalf("expected Details[\"repos\"] to be []GitHubRepoResult, got %T", reposAny)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo result, got %d", len(repos))
	}
	// Only 2 unique workflows (CI, Deploy) — the old CI failure should be filtered out.
	if len(repos[0].Runs) != 2 {
		t.Errorf("expected 2 runs (one per workflow), got %d", len(repos[0].Runs))
	}
	if repos[0].Status != string(StatusOK) {
		t.Errorf("expected repo status ok, got %s", repos[0].Status)
	}
}

// --- GitHub handler tests ---

func TestGitHubTokenGetHandler_NoToken(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/infra/github/token", nil), 1)
	rec := httptest.NewRecorder()
	GitHubTokenGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Configured bool   `json:"configured"`
		Masked     string `json:"masked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Configured {
		t.Error("expected not configured")
	}
}

func TestGitHubTokenGetHandler_WithToken(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "ghp_my_secret_token_value"); err != nil {
		t.Fatalf("set: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/github/token", nil), 1)
	rec := httptest.NewRecorder()
	GitHubTokenGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Configured bool   `json:"configured"`
		Masked     string `json:"masked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Configured {
		t.Error("expected configured")
	}
	// The masked value should be a fixed string, not derived from the decrypted token.
	if body.Masked != "****" {
		t.Errorf("expected fixed mask '****', got %q", body.Masked)
	}
}

func TestGitHubTokenSetHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"token":"ghp_my_secret_token"}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/github/token", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	GitHubTokenSetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "ghp_my_secret_token" {
		t.Errorf("expected 'ghp_my_secret_token', got %q", token)
	}
}

func TestGitHubTokenSetHandler_EmptyToken(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"token":""}`
	req := withUser(httptest.NewRequest("PUT", "/api/infra/github/token", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	GitHubTokenSetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGitHubTokenDeleteHandler(t *testing.T) {
	db := setupTestDB(t)

	if err := SetGitHubToken(db, 1, "to-delete"); err != nil {
		t.Fatalf("set: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/github/token", nil), 1)
	rec := httptest.NewRecorder()
	GitHubTokenDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	token, err := GetGitHubToken(db, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty after delete, got %q", token)
	}
}

func TestListGitHubReposHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddGitHubRepo(db, 1, "owner", "repo"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/github/repos", nil), 1)
	rec := httptest.NewRecorder()
	ListGitHubReposHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Repos []GitHubRepo `json:"repos"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(body.Repos))
	}
}

func TestAddGitHubRepoHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"owner":"octocat","repo":"hello-world"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/github/repos", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddGitHubRepoHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddGitHubRepoHandler_InvalidOwnerRepo(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"owner":"../evil","repo":"repo"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/github/repos", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddGitHubRepoHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal owner, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddGitHubRepoHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"owner":"","repo":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/github/repos", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddGitHubRepoHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteGitHubRepoHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	repo, err := AddGitHubRepo(db, 1, "owner", "repo")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	idStr := strconv.FormatInt(repo.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/github/repos/"+idStr, nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idStr)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteGitHubRepoHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteGitHubRepoHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/github/repos/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteGitHubRepoHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
