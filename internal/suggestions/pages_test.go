package suggestions

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"
)

// insertPageSetting writes a row into suggestion_page_settings for a given slug
// and rotation_enabled value. Helper kept local to the test file rather than
// exported from the package — production writes happen through the admin API,
// which is built in a sibling bead.
func insertPageSetting(t *testing.T, d *sql.DB, slug string, enabled int) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO suggestion_page_settings (page_slug, rotation_enabled, updated_at) VALUES (?, ?, ?)`,
		slug, enabled, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("insert page setting %q=%d: %v", slug, enabled, err)
	}
}

// slugs extracts page slugs in order from a slice of pages.
func slugs(pages []Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Slug
	}
	return out
}

// pinTestRegistry replaces the package-level Pages slice for the duration of a
// test so RotationEligible runs against a known, small registry rather than
// the production list.
func pinTestRegistry() func() {
	return withSavedPages([]Page{
		{Slug: "weather", Title: "Weather"},
		{Slug: "notes", Title: "Notes"},
		{Slug: "training", Title: "Training"},
	})
}

func TestRotationEligibleEmptySettingsReturnsAll(t *testing.T) {
	d := setupTestDB(t)
	defer pinTestRegistry()()

	got, err := RotationEligible(context.Background(), d)
	if err != nil {
		t.Fatalf("RotationEligible: %v", err)
	}
	want := []string{"weather", "notes", "training"}
	if diff := slugs(got); !equalStrings(diff, want) {
		t.Fatalf("expected %v, got %v", want, diff)
	}
}

func TestRotationEligibleExplicitEnableIncludesPage(t *testing.T) {
	d := setupTestDB(t)
	defer pinTestRegistry()()
	insertPageSetting(t, d, "weather", 1)

	got, err := RotationEligible(context.Background(), d)
	if err != nil {
		t.Fatalf("RotationEligible: %v", err)
	}
	want := []string{"weather", "notes", "training"}
	if diff := slugs(got); !equalStrings(diff, want) {
		t.Fatalf("expected %v, got %v", want, diff)
	}
}

func TestRotationEligibleExplicitDisableExcludesPage(t *testing.T) {
	d := setupTestDB(t)
	defer pinTestRegistry()()
	insertPageSetting(t, d, "weather", 0)

	got, err := RotationEligible(context.Background(), d)
	if err != nil {
		t.Fatalf("RotationEligible: %v", err)
	}
	want := []string{"notes", "training"}
	if diff := slugs(got); !equalStrings(diff, want) {
		t.Fatalf("expected %v, got %v", want, diff)
	}
}

func TestRotationEligibleMixedSettings(t *testing.T) {
	d := setupTestDB(t)
	defer pinTestRegistry()()
	insertPageSetting(t, d, "weather", 1) // explicit on
	insertPageSetting(t, d, "notes", 0)   // explicit off
	// "training" has no row → defaults to eligible.

	got, err := RotationEligible(context.Background(), d)
	if err != nil {
		t.Fatalf("RotationEligible: %v", err)
	}
	want := []string{"weather", "training"}
	if diff := slugs(got); !equalStrings(diff, want) {
		t.Fatalf("expected %v, got %v", want, diff)
	}

	// Make sure the order of returned pages matches registry order even when
	// settings rows arrived in a different sequence.
	wantSorted := append([]string{}, want...)
	sort.Strings(wantSorted)
	gotSorted := append([]string{}, slugs(got)...)
	sort.Strings(gotSorted)
	if !equalStrings(wantSorted, gotSorted) {
		t.Fatalf("set mismatch: want %v got %v", wantSorted, gotSorted)
	}
}

func TestRotationEligibleIgnoresUnknownSlug(t *testing.T) {
	d := setupTestDB(t)
	defer pinTestRegistry()()
	// A row for a slug that is not in the current registry should not affect
	// the output: registry membership drives the result, settings only filter.
	insertPageSetting(t, d, "ghost-page", 0)

	got, err := RotationEligible(context.Background(), d)
	if err != nil {
		t.Fatalf("RotationEligible: %v", err)
	}
	want := []string{"weather", "notes", "training"}
	if diff := slugs(got); !equalStrings(diff, want) {
		t.Fatalf("expected %v, got %v", want, diff)
	}
}

func TestAllRegisteredReturnsCopy(t *testing.T) {
	defer pinTestRegistry()()

	got := AllRegistered()
	if len(got) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(got))
	}
	// Mutating the returned slice must not affect the package-level registry.
	got[0].Slug = "tampered"
	if Pages[0].Slug == "tampered" {
		t.Fatalf("AllRegistered returned a slice that aliases Pages")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
