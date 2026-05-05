package suggestions

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// runPromptFn is the function used to invoke Claude. Replaced in tests, mirroring
// the package-level pattern used in chat/handlers.go.
var runPromptFn = training.RunPrompt

// PerPageTimeout is the deadline applied to each individual page's generation
// attempt. RunSuggestionsForPages caps each Claude call at this duration so a
// single hung page cannot starve the overall run.
const PerPageTimeout = 90 * time.Second

// recentSuggestionsWindowDays is the look-back window used when injecting prior
// suggestion titles into the prompt to discourage repeats.
const recentSuggestionsWindowDays = 14

// MaxRetriesOnMalformedJSON is the number of retries attempted when Claude
// returns a response that does not parse as the expected JSON shape.
const MaxRetriesOnMalformedJSON = 1

// RunResult summarises a generation pass.
type RunResult struct {
	Generated int `json:"generated"`
	Errors    int `json:"errors"`
}

// generated is the per-suggestion shape we expect from Claude.
type generated struct {
	Type  string `json:"type"`
	Size  string `json:"size"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// validTypes / validSizes are the allowed enum values from the prompt schema.
// new_page is generated through a separate prompt in a later bead, so it is
// excluded from per-page suggestion generation here.
var validTypes = map[string]bool{
	TypeAddition:    true,
	TypeBugfix:      true,
	TypeImprovement: true,
	TypeRefactor:    true,
}

var validSizes = map[string]bool{
	SizeS: true,
	SizeM: true,
	SizeL: true,
}

// RunSuggestionsForPages iterates over pages, calling Claude once per page to
// generate three improvement suggestions and inserting them as pending rows
// owned by userID. Per-page errors are logged but do not abort the run, so a
// single Claude failure or malformed-JSON page does not lose the rest. The
// caller is responsible for filtering pages by Enabled (see EnabledPages).
//
// Returns counts of inserted suggestions and total errors. Errors include both
// page-level failures (Claude call, JSON parse) and per-row insert failures, so
// "13 generated, 2 errors" tells the operator that two intended suggestions did
// not land in the DB.
func RunSuggestionsForPages(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
	pages []Page,
) RunResult {
	var result RunResult

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			log.Printf("suggestions: context done before page %q: %v", page.Slug, err)
			result.Errors++
			break
		}
		inserted, failed, err := runForPage(ctx, db, cfg, userID, page)
		result.Generated += inserted
		result.Errors += failed
		if err != nil {
			result.Errors++
			log.Printf("suggestions: page %q errored: %v", page.Slug, err)
		}
	}

	return result
}

// runForPage handles a single page end-to-end: build prompt, call Claude (with
// one retry on malformed JSON), and insert the resulting rows. Returns the
// number of rows successfully inserted, the number of per-row insert failures,
// and a page-level error (Claude call / JSON parse). Per-row insert failures
// are reported via the second return value so the caller can surface them in
// the overall error count.
func runForPage(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
	page Page,
) (inserted int, failed int, err error) {
	pageCtx, cancel := context.WithTimeout(ctx, PerPageTimeout)
	defer cancel()

	sources := loadSourceFiles(page.SourceFiles)
	gitLog := loadGitLog(pageCtx, page.SourceFiles)

	recent, recentErr := recentForPage(pageCtx, db, userID, page.Slug, recentSuggestionsWindowDays)
	if recentErr != nil {
		log.Printf("suggestions: load recent for %q: %v; continuing without", page.Slug, recentErr)
		recent = nil
	}

	prompt := buildPagePrompt(page, sources, gitLog, recent)

	suggestions, err := callAndParse(pageCtx, cfg, prompt)
	if err != nil {
		return 0, 0, err
	}

	now := time.Now().UTC()
	for _, sg := range suggestions {
		row := Suggestion{
			UserID:      userID,
			GeneratedAt: now,
			PageSlug:    page.Slug,
			Source:      SourceClaude,
			Type:        sg.Type,
			Size:        sg.Size,
			Title:       sg.Title,
			Body:        sg.Body,
			Status:      StatusPending,
		}
		if _, insErr := Insert(pageCtx, db, row); insErr != nil {
			log.Printf("suggestions: insert for %q: %v", page.Slug, insErr)
			failed++
			continue
		}
		inserted++
	}
	return inserted, failed, nil
}

// callAndParse calls Claude and parses the response. On a malformed JSON
// response it retries up to MaxRetriesOnMalformedJSON times, then returns the
// last error.
func callAndParse(ctx context.Context, cfg *training.ClaudeConfig, prompt string) ([]generated, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetriesOnMalformedJSON; attempt++ {
		resp, err := runPromptFn(ctx, cfg, prompt)
		if err != nil {
			return nil, fmt.Errorf("claude prompt: %w", err)
		}
		parsed, err := parseSuggestionsResponse(resp)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
		log.Printf("suggestions: parse attempt %d failed: %v", attempt+1, err)
	}
	return nil, fmt.Errorf("parse response after %d attempts: %w", MaxRetriesOnMalformedJSON+1, lastErr)
}

// parseSuggestionsResponse strips an optional markdown fence and decodes the
// response into exactly three validated generated suggestions.
func parseSuggestionsResponse(response string) ([]generated, error) {
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var items []generated
	if err := json.Unmarshal([]byte(response), &items); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(items) != 3 {
		return nil, fmt.Errorf("expected exactly 3 suggestions, got %d", len(items))
	}
	seenTypes := make(map[string]bool, len(items))
	for i, it := range items {
		if !validTypes[it.Type] {
			return nil, fmt.Errorf("item %d: invalid type %q", i, it.Type)
		}
		if !validSizes[it.Size] {
			return nil, fmt.Errorf("item %d: invalid size %q", i, it.Size)
		}
		if strings.TrimSpace(it.Title) == "" {
			return nil, fmt.Errorf("item %d: empty title", i)
		}
		if strings.TrimSpace(it.Body) == "" {
			return nil, fmt.Errorf("item %d: empty body", i)
		}
		if seenTypes[it.Type] {
			return nil, fmt.Errorf("item %d: duplicate type %q", i, it.Type)
		}
		seenTypes[it.Type] = true
	}
	return items, nil
}

var (
	repoRootOnce  sync.Once
	repoRootValue string
	repoRootErr   error
)

// repoRoot returns the absolute path to the repository root by running
// "git rev-parse --show-toplevel". The result is cached after the first call.
// SourceFiles in the page registry are repo-root-relative, so resolving against
// this path is necessary when the server or tests run from any other directory.
func repoRoot() (string, error) {
	repoRootOnce.Do(func() {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			repoRootErr = fmt.Errorf("git rev-parse --show-toplevel: %w", err)
			return
		}
		repoRootValue = strings.TrimSpace(string(out))
	})
	return repoRootValue, repoRootErr
}

// loadSourceFiles reads each provided repo-root-relative path and returns a
// map of path → contents. Files that cannot be read are skipped with a log
// warning — the prompt simply omits them rather than failing the run.
func loadSourceFiles(paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	root, err := repoRoot()
	if err != nil {
		log.Printf("suggestions: cannot resolve repo root for source files: %v", err)
		return out
	}
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		content, err := os.ReadFile(full)
		if err != nil {
			log.Printf("suggestions: read source %q: %v", p, err)
			continue
		}
		out[p] = string(content)
	}
	return out
}

// loadGitLog returns the recent commit history touching the given files. The
// command runs from the repository root so that repo-root-relative paths in
// SourceFiles are resolved correctly regardless of the server's working
// directory. An empty string is returned (and the failure logged) if git is
// unavailable — this is a context-only enrichment, not a hard requirement.
func loadGitLog(ctx context.Context, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	root, err := repoRoot()
	if err != nil {
		log.Printf("suggestions: cannot resolve repo root for git log: %v", err)
		return ""
	}
	args := []string{"log", "-n", "20", "--oneline", "--"}
	args = append(args, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("suggestions: git log failed: %v: %s", err, stderr.String())
		return ""
	}
	return strings.TrimSpace(stdout.String())
}
