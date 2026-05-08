package suggestions

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// runPromptFn is the function used to invoke Claude. It returns the response
// text, the cost of the call in USD parsed from the CLI envelope, and any
// error. Replaced in tests, mirroring the package-level pattern used in
// chat/handlers.go.
var runPromptFn = training.RunPromptWithCost

// PerPageTimeout is the deadline applied to each individual page's generation
// attempt. RunSuggestionsForPages caps each Claude call at this duration so a
// single hung page cannot starve the overall run. Bumped to 240s because
// Claude calls on large pages (many source files / long git history) can run
// well past the original 90s budget; the previous value was timing manual
// runs out before they could finish.
const PerPageTimeout = 240 * time.Second

// recentSuggestionsWindowDays is the look-back window used when injecting prior
// suggestion titles into the prompt to discourage repeats.
const recentSuggestionsWindowDays = 14

// MaxRetriesOnMalformedJSON is the number of retries attempted when Claude
// returns a response that does not parse as the expected JSON shape.
const MaxRetriesOnMalformedJSON = 1

// PageAtCapError is returned by runForPage when the page already has
// MaxPendingPerPage pending suggestions, so no Claude call is made. Callers
// detect this with errors.As and emit a "skipped" signal rather than counting
// it against the run's error total — it is the expected outcome of the
// per-page cap, not a failure.
type PageAtCapError struct {
	PageSlug     string
	PendingCount int
	Cap          int
}

func (e *PageAtCapError) Error() string {
	return fmt.Sprintf("page %q at cap (%d pending, cap %d)", e.PageSlug, e.PendingCount, e.Cap)
}

// RunResult summarises a generation pass. CostUSD is the total cost in US
// dollars of all Claude calls made during the pass, summed even across
// retried-on-malformed-JSON attempts (since each attempt is a paid API call).
type RunResult struct {
	Generated int     `json:"generated"`
	Errors    int     `json:"errors"`
	CostUSD   float64 `json:"cost_usd"`
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
// generate the deficit between MaxPendingPerPage and the page's current
// pending count and inserting them as pending rows owned by userID. Per-page
// errors are logged but do not abort the run, so a single Claude failure or
// malformed-JSON page does not lose the rest. Pages already at the cap are
// skipped without spending any API budget. The caller is responsible for
// filtering pages by rotation eligibility (see RotationEligible) and may
// optionally pre-filter at-cap pages with FilterUnderCap.
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
		inserted, failed, cost, err := runForPage(ctx, db, cfg, userID, page)
		result.Generated += inserted
		result.Errors += failed
		result.CostUSD += cost
		if err != nil {
			var atCap *PageAtCapError
			if errors.As(err, &atCap) {
				// Skipped-at-cap is the expected outcome of the per-page
				// cap, not a failure — do not increment Errors.
				continue
			}
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
//
// Recomputes the page's pending-suggestion count and the resulting deficit
// against MaxPendingPerPage at entry. If the page is already at the cap, the
// function returns (0, 0, 0, nil) without making a Claude call — this is the
// second stage of the two-stage filter (see FilterUnderCap) and protects
// against a page filling between the pre-rotation drop and execution. When
// the deficit is positive, exactly that many suggestions are requested.
func runForPage(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
	page Page,
) (inserted int, failed int, costUSD float64, err error) {
	pageCtx, cancel := context.WithTimeout(ctx, PerPageTimeout)
	defer cancel()

	pendingCount, pendingErr := PendingCountForPage(pageCtx, db, userID, page.Slug)
	if pendingErr != nil {
		return 0, 0, 0, fmt.Errorf("pending count: %w", pendingErr)
	}
	target := MaxPendingPerPage - pendingCount
	if target <= 0 {
		log.Printf("suggestions: skip page %q — at cap (%d pending)", page.Slug, pendingCount)
		return 0, 0, 0, &PageAtCapError{
			PageSlug:     page.Slug,
			PendingCount: pendingCount,
			Cap:          MaxPendingPerPage,
		}
	}

	sources := loadSourceFiles(page.SourceFiles)
	gitLog := loadGitLog(pageCtx, page.SourceFiles)

	recent, recentErr := recentForPage(pageCtx, db, userID, page.Slug, recentSuggestionsWindowDays)
	if recentErr != nil {
		log.Printf("suggestions: load recent for %q: %v; continuing without", page.Slug, recentErr)
		recent = nil
	}

	prompt := buildPagePrompt(page, sources, gitLog, recent, target)

	suggestions, costUSD, err := callAndParse(pageCtx, cfg, prompt, target)
	if err != nil {
		return 0, 0, costUSD, err
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
	return inserted, failed, costUSD, nil
}

// callAndParse calls Claude and parses the response into exactly `expected`
// suggestions. On a malformed JSON response it retries up to
// MaxRetriesOnMalformedJSON times, then returns the last error. The
// accumulated cost across all attempts (including the failed attempts that
// triggered retries) is returned so the caller can fold it into the run total
// — every attempt is a paid CLI invocation.
func callAndParse(ctx context.Context, cfg *training.ClaudeConfig, prompt string, expected int) ([]generated, float64, error) {
	var (
		lastErr   error
		totalCost float64
	)
	for attempt := 0; attempt <= MaxRetriesOnMalformedJSON; attempt++ {
		resp, cost, err := runPromptFn(ctx, cfg, prompt)
		totalCost += cost
		if err != nil {
			return nil, totalCost, fmt.Errorf("claude prompt: %w", err)
		}
		parsed, err := parseSuggestionsResponse(resp, expected)
		if err == nil {
			return parsed, totalCost, nil
		}
		lastErr = err
		log.Printf("suggestions: parse attempt %d failed: %v", attempt+1, err)
	}
	return nil, totalCost, fmt.Errorf("parse response after %d attempts: %w", MaxRetriesOnMalformedJSON+1, lastErr)
}

// parseSuggestionsResponse strips an optional markdown fence and decodes the
// response into exactly `expected` validated generated suggestions. The
// distinct-types check applies when expected > 1; for expected == 1 it is
// vacuously true and skipped.
func parseSuggestionsResponse(response string, expected int) ([]generated, error) {
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
	if len(items) != expected {
		return nil, fmt.Errorf("expected exactly %d suggestions, got %d", expected, len(items))
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
		if expected > 1 {
			if seenTypes[it.Type] {
				return nil, fmt.Errorf("item %d: duplicate type %q", i, it.Type)
			}
			seenTypes[it.Type] = true
		}
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
