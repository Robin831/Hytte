package suggestions

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Robin831/Hytte/internal/training"
)

// withRunPrompt swaps the package-level runPromptFn for the duration of a test
// and returns a restore function to defer.
func withRunPrompt(fn func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error)) func() {
	prev := runPromptFn
	runPromptFn = fn
	return func() { runPromptFn = prev }
}

// validJSONResponse intentionally repeats size "s" twice to exercise the
// loosened parser: distinct types are still required, but distinct sizes are
// not.
const validJSONResponse = `[
  {"type": "improvement", "size": "s", "title": "Cache the forecast", "body": "Add a 10-minute in-memory cache so repeated reloads do not hammer yr.no."},
  {"type": "addition", "size": "m", "title": "Add wind direction", "body": "Render the wind direction next to wind speed using the existing arrow icon."},
  {"type": "bugfix", "size": "s", "title": "Fix midnight rollover", "body": "Use local time when grouping forecast slots so the day label flips at the right moment."}
]`

func dummyCfg() *training.ClaudeConfig {
	return &training.ClaudeConfig{Enabled: true, CLIPath: "claude", Model: "claude-sonnet-4-6"}
}

func twoPages() []Page {
	return []Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
	}
}

func TestRunSuggestionsForPagesInsertsThreeRowsPerPage(t *testing.T) {
	d := setupTestDB(t)
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validJSONResponse, nil
	})()

	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 1, twoPages())
	if res.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", res.Errors)
	}
	if res.Generated != 6 {
		t.Fatalf("expected 6 generated (3 per page × 2 pages), got %d", res.Generated)
	}

	for _, slug := range []string{"weather", "notes"} {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = ?`, slug).Scan(&n); err != nil {
			t.Fatalf("count for %s: %v", slug, err)
		}
		if n != 3 {
			t.Fatalf("page %s: expected 3 rows, got %d", slug, n)
		}
	}
}

func TestRunSuggestionsForPagesRetriesOnceOnMalformedJSON(t *testing.T) {
	d := setupTestDB(t)

	var calls int
	var mu sync.Mutex
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		// First call: malformed. Second call: valid.
		if calls == 1 {
			return "{not valid json", nil
		}
		return validJSONResponse, nil
	})()

	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 1, []Page{
		{Slug: "weather", Title: "Weather"},
	})
	if calls != 2 {
		t.Fatalf("expected exactly 2 calls (1 retry), got %d", calls)
	}
	if res.Errors != 0 {
		t.Fatalf("expected 0 errors after successful retry, got %d", res.Errors)
	}
	if res.Generated != 3 {
		t.Fatalf("expected 3 rows after retry, got %d", res.Generated)
	}
}

func TestRunSuggestionsForPagesContinuesAfterPerPageError(t *testing.T) {
	d := setupTestDB(t)

	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		// Fail entirely on the weather page; succeed on notes.
		// The prompt embeds the page slug, so we look for it.
		if strings.Contains(prompt, "Slug: weather") {
			return "", errors.New("simulated claude failure")
		}
		return validJSONResponse, nil
	})()

	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 1, twoPages())
	if res.Errors != 1 {
		t.Fatalf("expected 1 error, got %d", res.Errors)
	}
	if res.Generated != 3 {
		t.Fatalf("expected 3 generated (notes succeeded), got %d", res.Generated)
	}

	// Confirm only the notes page has rows.
	var weatherCount, notesCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = 'weather'`).Scan(&weatherCount); err != nil {
		t.Fatalf("count weather: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = 'notes'`).Scan(&notesCount); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if weatherCount != 0 {
		t.Fatalf("weather: expected 0 rows after error, got %d", weatherCount)
	}
	if notesCount != 3 {
		t.Fatalf("notes: expected 3 rows, got %d", notesCount)
	}
}

func TestRunSuggestionsForPagesCountsPerRowInsertFailures(t *testing.T) {
	d := setupTestDB(t)

	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validJSONResponse, nil
	})()

	// Use a non-existent user_id so the FK constraint fails for every Insert.
	// The Claude call still succeeds, so this isolates per-row failures from
	// page-level errors.
	res := RunSuggestionsForPages(context.Background(), d, dummyCfg(), 9999, []Page{
		{Slug: "weather", Title: "Weather"},
	})
	if res.Generated != 0 {
		t.Fatalf("expected 0 generated, got %d", res.Generated)
	}
	if res.Errors != 3 {
		t.Fatalf("expected 3 per-row errors counted, got %d", res.Errors)
	}
}

func TestParseSuggestionsResponseRejectsWrongCount(t *testing.T) {
	_, err := parseSuggestionsResponse(`[
		{"type":"improvement","size":"s","title":"X","body":"y"},
		{"type":"improvement","size":"s","title":"X","body":"y"}
	]`)
	if err == nil {
		t.Fatal("expected error for 2-item response, got nil")
	}
}

func TestParseSuggestionsResponseRejectsInvalidEnum(t *testing.T) {
	_, err := parseSuggestionsResponse(`[
		{"type":"weird","size":"s","title":"X","body":"y"},
		{"type":"improvement","size":"s","title":"X","body":"y"},
		{"type":"improvement","size":"s","title":"X","body":"y"}
	]`)
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
}

func TestParseSuggestionsResponseAllowsDuplicateSizes(t *testing.T) {
	got, err := parseSuggestionsResponse(`[
		{"type":"improvement","size":"s","title":"A","body":"a"},
		{"type":"addition","size":"s","title":"B","body":"b"},
		{"type":"bugfix","size":"s","title":"C","body":"c"}
	]`)
	if err != nil {
		t.Fatalf("expected three same-size suggestions to parse, got error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
}

func TestParseSuggestionsResponseRejectsDuplicateTypes(t *testing.T) {
	_, err := parseSuggestionsResponse(`[
		{"type":"improvement","size":"s","title":"A","body":"a"},
		{"type":"improvement","size":"m","title":"B","body":"b"},
		{"type":"bugfix","size":"l","title":"C","body":"c"}
	]`)
	if err == nil {
		t.Fatal("expected error for duplicate type, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate type") {
		t.Fatalf("expected duplicate-type error, got: %v", err)
	}
}

func TestParseSuggestionsResponseStripsMarkdownFence(t *testing.T) {
	wrapped := "```json\n" + validJSONResponse + "\n```"
	got, err := parseSuggestionsResponse(wrapped)
	if err != nil {
		t.Fatalf("expected fence-stripped parse to succeed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
}
