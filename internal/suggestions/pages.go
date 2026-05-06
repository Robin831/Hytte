package suggestions

import (
	"context"
	"database/sql"
	"fmt"
)

// Page describes a single Hytte page that can receive AI-generated improvement
// suggestions. The registry is hand-curated rather than auto-discovered so that
// each page comes with a meaningful description and an explicit list of source
// files for the prompt context.
type Page struct {
	// Slug is a stable, lowercase identifier used as the page_slug column value.
	Slug string
	// Title is a short human-readable label.
	Title string
	// Route is the frontend path (for display only).
	Route string
	// Description is a one-sentence summary of the page's purpose.
	Description string
	// SourceFiles are repository-relative paths whose contents (truncated) are
	// included in the prompt as context.
	SourceFiles []string
	// FeatureFlag is the user_features key gating the page. Informational only
	// in this bead — used in the prompt context.
	FeatureFlag string
}

// Pages is the curated registry. This bead seeds five pages; the full registry
// of ~70 pages lands in a later bead together with the rotation scheduler.
var Pages = []Page{
	{
		Slug:        "weather",
		Title:       "Weather",
		Route:       "/weather",
		Description: "Multi-day forecast for Norwegian cities backed by the yr.no API.",
		SourceFiles: []string{
			"web/src/pages/Weather.tsx",
			"internal/weather/handler.go",
		},
		FeatureFlag: "weather",
	},
	{
		Slug:        "budget",
		Title:       "Budget",
		Route:       "/budget",
		Description: "Personal finance: accounts, categories, transactions, recurring bills, and trends.",
		SourceFiles: []string{
			"web/src/pages/BudgetPage.tsx",
			"internal/budget/handlers.go",
		},
		FeatureFlag: "budget",
	},
	{
		Slug:        "notes",
		Title:       "Notes",
		Route:       "/notes",
		Description: "Encrypted personal notes with tagging and search.",
		SourceFiles: []string{
			"web/src/pages/Notes.tsx",
			"internal/notes/handlers.go",
		},
		FeatureFlag: "notes",
	},
	{
		Slug:        "training",
		Title:       "Training",
		Route:       "/training",
		Description: "Workout list with summaries, training load, VO2max, and Claude analysis.",
		SourceFiles: []string{
			"web/src/pages/Training.tsx",
			"internal/training/handlers.go",
		},
		FeatureFlag: "training",
	},
	{
		Slug:        "links",
		Title:       "Links",
		Route:       "/links",
		Description: "Personal short-link manager.",
		SourceFiles: []string{
			"web/src/pages/Links.tsx",
			"internal/links/handlers.go",
		},
		FeatureFlag: "links",
	},
}

// AllRegistered returns the unfiltered registry slice. Callers that need a
// rotation-aware view should use RotationEligible instead.
func AllRegistered() []Page {
	out := make([]Page, len(Pages))
	for i, p := range Pages {
		out[i] = p
		if p.SourceFiles != nil {
			out[i].SourceFiles = append([]string(nil), p.SourceFiles...)
		}
	}
	return out
}

// RotationEligible returns the subset of Pages that should participate in the
// rotation scheduler. A page is eligible when no row exists in
// suggestion_page_settings (the default) or when its rotation_enabled column
// is 1. The DB is the sole source of truth for rotation state.
func RotationEligible(ctx context.Context, db *sql.DB) ([]Page, error) {
	settings, err := loadPageRotationSettings(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load page rotation settings: %w", err)
	}

	out := make([]Page, 0, len(Pages))
	for _, p := range Pages {
		enabled, ok := settings[p.Slug]
		if !ok || enabled {
			cp := p
			if p.SourceFiles != nil {
				cp.SourceFiles = append([]string(nil), p.SourceFiles...)
			}
			out = append(out, cp)
		}
	}
	return out, nil
}

// init enforces that page slugs are unique. A duplicate slug would silently
// merge prompts and confuse stats — fail loud at startup instead of in prod.
func init() {
	seen := make(map[string]struct{}, len(Pages))
	for _, p := range Pages {
		if _, dup := seen[p.Slug]; dup {
			panic(fmt.Sprintf("suggestions: duplicate page slug %q in registry", p.Slug))
		}
		seen[p.Slug] = struct{}{}
	}
}
