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
// single Claude failure or malformed-JSON page does not lose the rest.
//
// Returns counts of inserted suggestions and pages that errored.
func RunSuggestionsForPages(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
	pages []Page,
) RunResult {
	var result RunResult

	for _, page := range pages {
		if !page.Enabled {
			continue
		}
		inserted, err := runForPage(ctx, db, cfg, userID, page)
		if err != nil {
			result.Errors++
			log.Printf("suggestions: page %q errored: %v", page.Slug, err)
			continue
		}
		result.Generated += inserted
	}

	return result
}

// runForPage handles a single page end-to-end: build prompt, call Claude (with
// one retry on malformed JSON), and insert the resulting rows.
func runForPage(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
	page Page,
) (int, error) {
	pageCtx, cancel := context.WithTimeout(ctx, PerPageTimeout)
	defer cancel()

	sources := loadSourceFiles(page.SourceFiles)
	gitLog := loadGitLog(pageCtx, page.SourceFiles)

	recent, err := recentForPage(pageCtx, db, userID, page.Slug, recentSuggestionsWindowDays)
	if err != nil {
		log.Printf("suggestions: load recent for %q: %v; continuing without", page.Slug, err)
		recent = nil
	}

	prompt := buildPagePrompt(page, sources, gitLog, recent)

	suggestions, err := callAndParse(pageCtx, cfg, prompt)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	inserted := 0
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
		if _, err := Insert(pageCtx, db, row); err != nil {
			log.Printf("suggestions: insert for %q: %v", page.Slug, err)
			continue
		}
		inserted++
	}
	if inserted == 0 {
		return 0, errors.New("no suggestions inserted")
	}
	return inserted, nil
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
	}
	return items, nil
}

// loadSourceFiles reads each provided path relative to the current working
// directory and returns a map of path → contents. Files that cannot be read are
// silently skipped — the prompt simply omits them rather than failing the run.
func loadSourceFiles(paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		content, err := os.ReadFile(filepath.FromSlash(p))
		if err != nil {
			log.Printf("suggestions: read source %q: %v", p, err)
			continue
		}
		out[p] = string(content)
	}
	return out
}

// loadGitLog returns the recent commit history touching the given files. An
// empty string is returned (and the failure logged) if git is unavailable —
// this is a context-only enrichment, not a hard requirement for generation.
func loadGitLog(ctx context.Context, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	args := []string{"log", "-n", "20", "--oneline", "--"}
	args = append(args, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("suggestions: git log failed: %v: %s", err, stderr.String())
		return ""
	}
	return strings.TrimSpace(stdout.String())
}
