package news

import (
	"testing"
	"time"
)

func TestContainsWordBoundaries(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        bool
	}{
		{"krig i ukraina eskalerer", "ukraina", true},
		{"miranda july lager film", "iran", false}, // substring must not match
		{"spenningen mellom iran og usa", "iran", true},
		{"ny paradise hotel-sesong", "paradise hotel", true},
		{"forward-tenkning i næringslivet", "war", false},
		{"tysk influenser tjener millioner", "influenser", true},
	}
	for _, c := range cases {
		if got := containsWord(c.hay, c.needle); got != c.want {
			t.Errorf("containsWord(%q,%q)=%v want %v", c.hay, c.needle, got, c.want)
		}
	}
}

func TestApplyFiltersKeywordAndCategory(t *testing.T) {
	arts := []Article{
		{ID: "1", Title: "Trump taler igjen", Summary: ""},
		{ID: "2", Title: "Ny norsk gründer lykkes", Summary: "spennende teknologi"},
		{ID: "3", Title: "Fotballkamp", Categories: []string{"Sport"}},
	}
	s := Settings{
		BlockKeywords:   []string{"trump"},
		BlockCategories: []string{"Sport"},
		HidePaywalled:   false,
	}
	visible, hidden := applyFilters(arts, s)
	if len(visible) != 1 || visible[0].ID != "2" {
		t.Fatalf("expected only article 2 visible, got %+v", visible)
	}
	if len(hidden) != 2 {
		t.Fatalf("expected 2 hidden, got %d", len(hidden))
	}
}

func TestPaywallFilter(t *testing.T) {
	arts := []Article{
		{ID: "1", Title: "+ Eksklusiv sak", Summary: "noe"},
		{ID: "2", Title: "Vanlig sak", Summary: "Les hele saken hos oss"},
		{ID: "3", Title: "Gratis sak", Summary: "full tekst her"},
	}
	visible, hidden := applyFilters(arts, Settings{HidePaywalled: true})
	if len(visible) != 1 || visible[0].ID != "3" {
		t.Fatalf("expected only article 3 visible, got %+v", visible)
	}
	if len(hidden) != 2 {
		t.Fatalf("expected 2 paywalled hidden, got %d", len(hidden))
	}
	// With paywall hiding off, all pass.
	visible, _ = applyFilters(arts, Settings{HidePaywalled: false})
	if len(visible) != 3 {
		t.Fatalf("expected 3 visible with paywall off, got %d", len(visible))
	}
}

func TestDedupeCrossSource(t *testing.T) {
	now := time.Now()
	arts := []Article{
		{ID: "1", Source: "vg", SourceName: "VG", Title: "Regjeringen kutter strømstøtte til husholdninger", PublishedAt: now},
		{ID: "2", Source: "nrk", SourceName: "NRK", Title: "Regjeringen kutter strømstøtte til husholdninger neste år", URL: "http://nrk/x", PublishedAt: now.Add(-time.Minute)},
		{ID: "3", Source: "tek", SourceName: "tek.no", Title: "Ny iPhone lansert med bedre kamera", PublishedAt: now.Add(-2 * time.Minute)},
	}
	out := dedupe(arts)
	if len(out) != 2 {
		t.Fatalf("expected 2 after dedupe, got %d: %+v", len(out), out)
	}
	if len(out[0].AlsoIn) != 1 || out[0].AlsoIn[0].Source != "nrk" {
		t.Errorf("expected NRK merged into VG story, got AlsoIn=%+v", out[0].AlsoIn)
	}
}

func TestDedupeKeepsSameSourceSeparate(t *testing.T) {
	arts := []Article{
		{ID: "1", Source: "vg", Title: "Stor brann i Oslo sentrum"},
		{ID: "2", Source: "vg", Title: "Stor brann i Oslo sentrum"},
	}
	out := dedupe(arts)
	if len(out) != 2 {
		t.Errorf("same-source near-dupes should not merge, got %d", len(out))
	}
}
