package suggestions

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
	// Enabled controls whether RunSuggestionsForPages will generate against this
	// page. The per-page enable toggle UI is a later bead; for now all seeded
	// pages default to true.
	Enabled bool
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
		Enabled:     true,
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
		Enabled:     true,
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
		Enabled:     true,
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
		Enabled:     true,
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
		Enabled:     true,
	},
}

// EnabledPages returns the subset of Pages with Enabled == true.
func EnabledPages() []Page {
	out := make([]Page, 0, len(Pages))
	for _, p := range Pages {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}
