package suggestions

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

const validNewPageJSON = `{"size": "m", "title": "Reading log", "body": "A page that tracks books I am reading, with notes per chapter and a finished-by-date target. Useful for spotting which books I drop, and for keeping a backlog of next-up titles. Adds a small dashboard widget for current progress."}`

func TestRunNewPageSuggestionInsertsOneRow(t *testing.T) {
	d := setupTestDB(t)
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		return validNewPageJSON, nil
	})()

	res, err := RunNewPageSuggestion(context.Background(), d, dummyCfg(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Generated != 1 {
		t.Fatalf("expected 1 generated, got %d", res.Generated)
	}
	if res.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", res.Errors)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE page_slug = ? AND type = ? AND source = ? AND status = ?`,
		NewPageSlug, TypeNewPage, SourceClaude, StatusPending).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 row matching new_page contract, got %d", n)
	}

	var size, title string
	if err := d.QueryRow(`SELECT size, title FROM suggestions WHERE page_slug = ?`, NewPageSlug).Scan(&size, &title); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if size != SizeM {
		t.Fatalf("expected size=%q, got %q", SizeM, size)
	}
	if title != "Reading log" {
		t.Fatalf("expected title 'Reading log', got %q", title)
	}
}

func TestRunNewPageSuggestionMalformedJSONRetriesAndFails(t *testing.T) {
	d := setupTestDB(t)

	var calls int
	var mu sync.Mutex
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		// Return invalid JSON on every call: original + one retry, both fail.
		return "not valid json at all", nil
	})()

	res, err := RunNewPageSuggestion(context.Background(), d, dummyCfg(), 1)
	if err != nil {
		t.Fatalf("expected nil error for log-but-don't-abort semantics, got %v", err)
	}
	if calls != MaxRetriesOnMalformedJSON+1 {
		t.Fatalf("expected exactly %d calls (original + retries), got %d", MaxRetriesOnMalformedJSON+1, calls)
	}
	if res.Generated != 0 {
		t.Fatalf("expected 0 generated after parse failures, got %d", res.Generated)
	}
	if res.Errors == 0 {
		t.Fatalf("expected Errors > 0 when parse fails; got %d", res.Errors)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM suggestions`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no rows inserted on parse failure, got %d", n)
	}
}

func TestBuildNewPagePromptIncludesRegistryAndRecentTitles(t *testing.T) {
	registered := []Page{
		{Slug: "weather", Title: "Weather", Description: "Forecast page."},
		{Slug: "notes", Title: "Notes", Description: "Encrypted notes."},
		{Slug: "training", Title: "Training", Description: "Workouts."},
	}
	recent := []Suggestion{
		{Title: "Reading log", Status: StatusPending, Size: SizeM, Type: TypeNewPage},
		{Title: "Recipe vault", Status: StatusPlanned, Size: SizeL, Type: TypeNewPage},
	}

	prompt := buildNewPagePrompt(registered, recent)

	for _, p := range registered {
		if !strings.Contains(prompt, p.Title) {
			t.Errorf("prompt missing registered page title %q", p.Title)
		}
		if !strings.Contains(prompt, p.Slug) {
			t.Errorf("prompt missing registered page slug %q", p.Slug)
		}
	}

	for _, s := range recent {
		if !strings.Contains(prompt, s.Title) {
			t.Errorf("prompt missing recent new_page suggestion title %q", s.Title)
		}
	}
}

func TestRunNewPageSuggestionPromptCarriesRecentNewPageTitles(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	// Seed two prior new_page suggestions so the prompt should mention them.
	mustInsert := func(title string) {
		t.Helper()
		_, err := Insert(ctx, d, Suggestion{
			UserID:      1,
			GeneratedAt: time.Now().UTC().Add(-24 * time.Hour),
			PageSlug:    NewPageSlug,
			Source:      SourceClaude,
			Type:        TypeNewPage,
			Size:        SizeM,
			Title:       title,
			Body:        "irrelevant body",
			Status:      StatusPending,
		})
		if err != nil {
			t.Fatalf("seed insert %q: %v", title, err)
		}
	}
	mustInsert("Reading log")
	mustInsert("Recipe vault")

	var captured string
	defer withRunPrompt(func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error) {
		captured = prompt
		return validNewPageJSON, nil
	})()

	if _, err := RunNewPageSuggestion(ctx, d, dummyCfg(), 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, want := range []string{"Reading log", "Recipe vault"} {
		if !strings.Contains(captured, want) {
			t.Errorf("expected prompt to contain recent title %q, got: %s", want, captured)
		}
	}

	for _, p := range AllRegistered() {
		if !strings.Contains(captured, p.Title) {
			t.Errorf("expected prompt to contain registered page title %q", p.Title)
		}
	}
}

func TestParseNewPageResponseAcceptsValid(t *testing.T) {
	got, err := parseNewPageResponse(validNewPageJSON)
	if err != nil {
		t.Fatalf("expected valid JSON to parse, got: %v", err)
	}
	if got.Size != SizeM {
		t.Errorf("size: got %q want %q", got.Size, SizeM)
	}
	if got.Title != "Reading log" {
		t.Errorf("title: got %q", got.Title)
	}
	if got.Body == "" {
		t.Errorf("body should not be empty")
	}
}

func TestParseNewPageResponseStripsMarkdownFence(t *testing.T) {
	wrapped := "```json\n" + validNewPageJSON + "\n```"
	if _, err := parseNewPageResponse(wrapped); err != nil {
		t.Fatalf("expected fence-stripped parse to succeed: %v", err)
	}
}

func TestParseNewPageResponseRejectsInvalidSize(t *testing.T) {
	if _, err := parseNewPageResponse(`{"size":"xl","title":"X","body":"y"}`); err == nil {
		t.Fatal("expected error for invalid size, got nil")
	}
}

func TestParseNewPageResponseRejectsEmptyTitle(t *testing.T) {
	if _, err := parseNewPageResponse(`{"size":"s","title":"","body":"y"}`); err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
}

func TestParseNewPageResponseRejectsEmptyBody(t *testing.T) {
	if _, err := parseNewPageResponse(`{"size":"s","title":"X","body":""}`); err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestParseNewPageResponseRejectsUnknownFields(t *testing.T) {
	if _, err := parseNewPageResponse(`{"size":"s","title":"X","body":"y","extra":"oops"}`); err == nil {
		t.Fatal("expected error for unknown fields, got nil")
	}
}
