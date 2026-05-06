package suggestions

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"
)

// rotationFixturePages returns five eligible pages with deterministic slugs.
// Using a local fixture rather than the package-level Pages registry means the
// tests stay valid even if the registry is reordered or extended.
func rotationFixturePages() []Page {
	return []Page{
		{Slug: "alpha", Title: "Alpha", Route: "/alpha"},
		{Slug: "bravo", Title: "Bravo", Route: "/bravo"},
		{Slug: "charlie", Title: "Charlie", Route: "/charlie"},
		{Slug: "delta", Title: "Delta", Route: "/delta"},
		{Slug: "echo", Title: "Echo", Route: "/echo"},
	}
}

// seedSuggestion inserts a minimal suggestion row with the supplied page slug
// and generated_at timestamp.
func seedSuggestion(t *testing.T, db *sql.DB, slug string, generatedAt time.Time) {
	t.Helper()
	if _, err := Insert(context.Background(), db, Suggestion{
		UserID:      1,
		PageSlug:    slug,
		GeneratedAt: generatedAt,
		Source:      SourceClaude,
		Type:        TypeImprovement,
		Size:        SizeS,
		Title:       "seed",
		Body:        "seed body",
		Status:      StatusPending,
	}); err != nil {
		t.Fatalf("seed suggestion for %q: %v", slug, err)
	}
}

func slugs(pages []Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Slug
	}
	return out
}

func TestPickRotation_SelectsLeastRecentlyRun(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// All five pages have prior suggestions with distinct timestamps.
	// "echo" is oldest, "alpha" is newest.
	seedSuggestion(t, d, "alpha", base.Add(4*time.Hour))
	seedSuggestion(t, d, "bravo", base.Add(3*time.Hour))
	seedSuggestion(t, d, "charlie", base.Add(2*time.Hour))
	seedSuggestion(t, d, "delta", base.Add(1*time.Hour))
	seedSuggestion(t, d, "echo", base)

	got, err := PickRotation(ctx, d, rotationFixturePages(), 2)
	if err != nil {
		t.Fatalf("PickRotation: %v", err)
	}
	want := []string{"echo", "delta"}
	if !reflect.DeepEqual(slugs(got), want) {
		t.Fatalf("slugs mismatch: got %v want %v", slugs(got), want)
	}
}

func TestPickRotation_PagesWithoutPriorSuggestionsComeFirst(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Only alpha and bravo have prior suggestions; charlie/delta/echo do not.
	// Among the new pages the ordering must be alphabetical.
	seedSuggestion(t, d, "alpha", base.Add(2*time.Hour))
	seedSuggestion(t, d, "bravo", base.Add(1*time.Hour))

	got, err := PickRotation(ctx, d, rotationFixturePages(), 0)
	if err != nil {
		t.Fatalf("PickRotation: %v", err)
	}
	want := []string{"charlie", "delta", "echo", "bravo", "alpha"}
	if !reflect.DeepEqual(slugs(got), want) {
		t.Fatalf("slugs mismatch: got %v want %v", slugs(got), want)
	}
}

func TestPickRotation_TieBreaksAlphabetically(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// All five pages have the same generated_at — they should be returned in
	// alphabetical order by slug to keep ordering deterministic.
	for _, s := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		seedSuggestion(t, d, s, ts)
	}

	got, err := PickRotation(ctx, d, rotationFixturePages(), 5)
	if err != nil {
		t.Fatalf("PickRotation: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	if !reflect.DeepEqual(slugs(got), want) {
		t.Fatalf("slugs mismatch: got %v want %v", slugs(got), want)
	}
}

func TestPickRotation_NReturnsAllWhenOutOfBounds(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	pages := rotationFixturePages()

	for _, n := range []int{-1, 0, len(pages), len(pages) + 5} {
		got, err := PickRotation(ctx, d, pages, n)
		if err != nil {
			t.Fatalf("PickRotation n=%d: %v", n, err)
		}
		if len(got) != len(pages) {
			t.Fatalf("n=%d: expected %d pages, got %d", n, len(pages), len(got))
		}
	}
}

func TestPickRotation_EmptyEligibleReturnsEmpty(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	got, err := PickRotation(ctx, d, []Page{}, RotationDefaultN)
	if err != nil {
		t.Fatalf("PickRotation: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", slugs(got))
	}
}

func TestPickRotation_ReturnedSliceIsACopy(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	pages := rotationFixturePages()
	pages[0].SourceFiles = []string{"a/b.go"}

	got, err := PickRotation(ctx, d, pages, 0)
	if err != nil {
		t.Fatalf("PickRotation: %v", err)
	}
	// Find alpha in the result and mutate its SourceFiles — the input slice
	// must not be affected. (PickRotation is allowed to reorder, so we look
	// the page up by slug rather than by index.)
	for i := range got {
		if got[i].Slug == "alpha" {
			got[i].SourceFiles[0] = "MUTATED"
		}
	}
	if pages[0].SourceFiles[0] == "MUTATED" {
		t.Fatalf("expected PickRotation to copy SourceFiles; input was mutated")
	}
}
