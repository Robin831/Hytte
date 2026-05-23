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

// Pages is the curated registry of Hytte feature pages eligible for nightly
// suggestion runs. Rotation eligibility is filtered at query time via
// suggestion_page_settings (see RotationEligible) — the DB is the source of
// truth for which pages currently participate in the rotation.
var Pages = []Page{
	{
		Slug:        "dashboard",
		Title:       "Dashboard",
		Route:       "/dashboard",
		Description: "Personal landing dashboard summarising recent activity across enabled Hytte features.",
		SourceFiles: []string{
			"web/src/pages/Dashboard.tsx",
			"web/src/pages/HomePage.tsx",
			"internal/dashboard/activity.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "today",
		Title:       "Today",
		Route:       "/today",
		Description: "Compact at-a-glance view combining weather, calendar, and other daily signals.",
		SourceFiles: []string{
			"web/src/pages/TodayView.tsx",
		},
		FeatureFlag: "",
	},
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
		Slug:        "calendar",
		Title:       "Calendar",
		Route:       "/calendar",
		Description: "Google Calendar integration with multi-calendar event display and sync.",
		SourceFiles: []string{
			"web/src/pages/CalendarPage.tsx",
			"internal/calendar/handlers.go",
		},
		FeatureFlag: "calendar",
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
		Slug:        "links",
		Title:       "Links",
		Route:       "/links",
		Description: "Personal short-link manager with /go/{code} redirector.",
		SourceFiles: []string{
			"web/src/pages/Links.tsx",
			"internal/links/handlers.go",
		},
		FeatureFlag: "links",
	},
	{
		Slug:        "webhooks",
		Title:       "Webhooks",
		Route:       "/webhooks",
		Description: "Inspect and replay incoming webhook deliveries with a live SSE stream.",
		SourceFiles: []string{
			"web/src/pages/Webhooks.tsx",
			"internal/webhooks/handlers.go",
		},
		FeatureFlag: "webhooks",
	},
	{
		Slug:        "vault",
		Title:       "Vault",
		Route:       "/vault",
		Description: "Encrypted file storage with folder organisation, tagging, and previews.",
		SourceFiles: []string{
			"web/src/pages/Vault.tsx",
			"internal/vault/handlers.go",
		},
		FeatureFlag: "vault",
	},
	{
		Slug:        "chat",
		Title:       "Chat",
		Route:       "/chat",
		Description: "Conversational Claude chat with persistent conversations and history.",
		SourceFiles: []string{
			"web/src/pages/Chat.tsx",
			"internal/chat/handlers.go",
		},
		FeatureFlag: "chat",
	},
	{
		Slug:        "transit",
		Title:       "Transit",
		Route:       "/transit",
		Description: "Live public transport departures from configured stops via the Entur API.",
		SourceFiles: []string{
			"web/src/pages/Transit.tsx",
			"internal/transit/handlers.go",
		},
		FeatureFlag: "transit",
	},
	{
		Slug:        "skywatch",
		Title:       "Sky Watch",
		Route:       "/skywatch",
		Description: "Aurora forecast, moon phase calendar, and other astronomy signals.",
		SourceFiles: []string{
			"web/src/pages/SkyWatchPage.tsx",
			"internal/skywatch/handlers.go",
		},
		FeatureFlag: "skywatch",
	},
	{
		Slug:        "grocery",
		Title:       "Grocery List",
		Route:       "/grocery",
		Description: "Shared household grocery list with check-off, reorder, and translation helpers.",
		SourceFiles: []string{
			"web/src/pages/GroceryPage.tsx",
			"internal/grocery/handlers.go",
		},
		FeatureFlag: "grocery",
	},
	{
		Slug:        "infra",
		Title:       "Infra",
		Route:       "/infra",
		Description: "Self-hosted infrastructure dashboard: health checks, SSL certs, DNS, Hetzner stats, and Docker hosts.",
		SourceFiles: []string{
			"web/src/pages/Infra.tsx",
			"internal/infra/handlers.go",
		},
		FeatureFlag: "infra",
	},
	{
		Slug:        "wordfeud",
		Title:       "Wordfeud",
		Route:       "/wordfeud",
		Description: "Wordfeud companion: live game board, dictionary lookup, move solver, and local game tracking.",
		SourceFiles: []string{
			"web/src/pages/WordfeudPage.tsx",
			"web/src/pages/WordfeudBoard.tsx",
			"web/src/pages/WordfeudLocalGames.tsx",
			"internal/wordfeud/handlers.go",
			"internal/wordfeud/api.go",
		},
		FeatureFlag: "wordfeud",
	},
	{
		Slug:        "training",
		Title:       "Training",
		Route:       "/training",
		Description: "Workout list with summaries, training load, VO2max, race predictions, and Claude analysis.",
		SourceFiles: []string{
			"web/src/pages/Training.tsx",
			"web/src/pages/TrainingDetail.tsx",
			"web/src/pages/TrainingTrends.tsx",
			"web/src/pages/TrainingCompare.tsx",
			"internal/training/handlers.go",
		},
		FeatureFlag: "training",
	},
	{
		Slug:        "stride",
		Title:       "Stride",
		Route:       "/training/stride",
		Description: "AI running coach: races, training plan generation, daily evaluations, and plan chat.",
		SourceFiles: []string{
			"web/src/pages/StridePage.tsx",
			"internal/stride/handlers.go",
		},
		FeatureFlag: "stride",
	},
	{
		Slug:        "lactate",
		Title:       "Lactate Tests",
		Route:       "/lactate",
		Description: "Lactate threshold tests with stage data, threshold curves, zone calculations, and insights.",
		SourceFiles: []string{
			"web/src/pages/LactateTests.tsx",
			"web/src/pages/LactateNewTest.tsx",
			"web/src/pages/LactateTestDetail.tsx",
			"web/src/pages/LactateInsights.tsx",
			"internal/lactate/handlers.go",
		},
		FeatureFlag: "lactate",
	},
	{
		Slug:        "budget",
		Title:       "Budget",
		Route:       "/budget",
		Description: "Personal finance: accounts, categories, transactions, charts, and CSV import.",
		SourceFiles: []string{
			"web/src/pages/BudgetPage.tsx",
			"web/src/pages/BudgetCharts.tsx",
			"web/src/pages/BudgetAccounts.tsx",
			"web/src/pages/BudgetCategories.tsx",
			"web/src/pages/BudgetImport.tsx",
			"internal/budget/handlers.go",
		},
		FeatureFlag: "budget",
	},
	{
		Slug:        "budget-credit-cards",
		Title:       "Credit Cards",
		Route:       "/budget/credit-cards",
		Description: "Credit-card transactions with rule-based categorisation, recurring merchants, and import flow.",
		SourceFiles: []string{
			"web/src/pages/BudgetCreditCards.tsx",
			"internal/creditcard/transactions.go",
			"internal/creditcard/import.go",
			"internal/creditcard/groups.go",
		},
		FeatureFlag: "budget",
	},
	{
		Slug:        "budget-recurring",
		Title:       "Recurring Bills",
		Route:       "/budget/recurring",
		Description: "Recurring bills, variable bills, and the regning view for upcoming due dates.",
		SourceFiles: []string{
			"web/src/pages/BudgetRecurring.tsx",
			"web/src/pages/BudgetVariables.tsx",
			"web/src/pages/BudgetRegning.tsx",
			"internal/budget/regning.go",
			"internal/budget/variable_bills.go",
		},
		FeatureFlag: "budget",
	},
	{
		Slug:        "budget-loans",
		Title:       "Loans",
		Route:       "/budget/loans",
		Description: "Loan tracking with amortisation schedules and interest-rate change history.",
		SourceFiles: []string{
			"web/src/pages/BudgetLoan.tsx",
			"internal/budget/loans.go",
		},
		FeatureFlag: "budget",
	},
	{
		Slug:        "salary",
		Title:       "Salary",
		Route:       "/salary",
		Description: "Norwegian salary estimator with tax tables, trekktabell assignments, and budget sync.",
		SourceFiles: []string{
			"web/src/pages/SalaryPage.tsx",
			"internal/salary/handlers.go",
		},
		FeatureFlag: "salary",
	},
	{
		Slug:        "workhours",
		Title:       "Work Hours",
		Route:       "/workhours",
		Description: "Work-hours tracking with sessions, deductions, flex pool, leave balance, and punch in/out.",
		SourceFiles: []string{
			"web/src/pages/WorkHoursPage.tsx",
			"internal/workhours/handlers.go",
		},
		FeatureFlag: "work_hours",
	},
	{
		Slug:        "family",
		Title:       "Family",
		Route:       "/family",
		Description: "Parent-facing family management: children, rewards, claims, and challenges.",
		SourceFiles: []string{
			"web/src/pages/Family.tsx",
			"web/src/pages/FamilyChildDetail.tsx",
			"web/src/pages/FamilyRewards.tsx",
			"web/src/pages/family/FamilyChallenges.tsx",
			"internal/family/handlers.go",
		},
		FeatureFlag: "kids_stars",
	},
	{
		Slug:        "stars",
		Title:       "Stars",
		Route:       "/stars",
		Description: "Kid-facing stars dashboard: balance, badges, challenges, leaderboard, rewards, and savings.",
		SourceFiles: []string{
			"web/src/pages/Stars.tsx",
			"web/src/pages/StarBadges.tsx",
			"web/src/pages/StarChallenges.tsx",
			"web/src/pages/StarLeaderboard.tsx",
			"web/src/pages/StarRewards.tsx",
			"internal/stars/handlers.go",
		},
		FeatureFlag: "kids_stars",
	},
	{
		Slug:        "allowance",
		Title:       "Allowance",
		Route:       "/allowance",
		Description: "Kids allowance: parent chore management plus the kid-facing chore and earnings view.",
		SourceFiles: []string{
			"web/src/pages/AllowancePage.tsx",
			"web/src/pages/MyChoresPage.tsx",
			"internal/allowance/handlers.go",
		},
		FeatureFlag: "kids_allowance",
	},
	{
		Slug:        "homework",
		Title:       "Homework",
		Route:       "/homework",
		Description: "Claude-powered homework helper for kids plus a parent review surface.",
		SourceFiles: []string{
			"web/src/pages/HomeworkPage.tsx",
			"web/src/pages/HomeworkChat.tsx",
			"web/src/pages/HomeworkSettings.tsx",
			"web/src/pages/HomeworkParentReview.tsx",
			"internal/homework/handlers.go",
		},
		FeatureFlag: "homework",
	},
	{
		Slug:        "math",
		Title:       "Regnemester",
		Route:       "/math",
		Description: "Norwegian math practice game with marathon, blitz, leaderboards, heatmap, and achievements.",
		SourceFiles: []string{
			"web/src/pages/Math.tsx",
			"web/src/pages/MathMarathon.tsx",
			"web/src/pages/MathBlitz.tsx",
			"web/src/pages/MathLeaderboard.tsx",
			"web/src/pages/MathHeatmap.tsx",
			"internal/math/handlers.go",
		},
		FeatureFlag: "regnemester",
	},
	{
		Slug:        "kiosk",
		Title:       "Kiosk",
		Route:       "/kiosk",
		Description: "Token-authenticated public kiosk display aggregating weather, transit, and Netatmo for a shared screen.",
		SourceFiles: []string{
			"web/src/pages/KioskPage.tsx",
			"internal/kiosk/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "settings",
		Title:       "Settings",
		Route:       "/settings",
		Description: "User preferences, session management, feature toggles, and account deletion.",
		SourceFiles: []string{
			"web/src/pages/Settings.tsx",
			"internal/auth/settings_handlers.go",
			"internal/auth/preferences.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "forge",
		Title:       "Forge Dashboard",
		Route:       "/forge",
		Description: "Admin-only single-user dashboard for the Forge orchestration daemon: status, queue, PRs, and bead actions.",
		SourceFiles: []string{
			"web/src/pages/ForgeDashboardPage.tsx",
			"web/src/pages/ForgeSettingsPage.tsx",
			"internal/forge/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "mezzanine",
		Title:       "Mezzanine",
		Route:       "/forge/mezzanine",
		Description: "Admin-only single-user Forge mezzanine: live worker activity, events, and worker logs.",
		SourceFiles: []string{
			"web/src/pages/MezzaninePage.tsx",
			"web/src/pages/EventsPage.tsx",
			"web/src/pages/WorkerDetailPage.tsx",
			"internal/forge/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "anvils",
		Title:       "Anvils",
		Route:       "/forge/mezzanine/anvils",
		Description: "Admin-only single-user view of Forge anvil health and per-anvil cost breakdown.",
		SourceFiles: []string{
			"web/src/pages/AnvilsPage.tsx",
			"internal/forge/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "ingots",
		Title:       "Ingots",
		Route:       "/forge/mezzanine/ingots",
		Description: "Admin-only single-user view of Forge ingot inventory and lifecycle.",
		SourceFiles: []string{
			"web/src/pages/IngotsPage.tsx",
			"internal/forge/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "forge-costs",
		Title:       "Forge Costs",
		Route:       "/forge/mezzanine/costs",
		Description: "Admin-only single-user Forge cost dashboard: spend trends, top beads by cost, and per-anvil totals.",
		SourceFiles: []string{
			"web/src/pages/ForgeCostsDashboardPage.tsx",
			"internal/forge/handlers.go",
		},
		FeatureFlag: "",
	},
	{
		Slug:        "pokemon",
		Title:       "Pokémon",
		Route:       "/pokemon",
		Description: "Pokémon TCG collection: browse sets, scan cards, view top cards by value, and track scanned inventory.",
		SourceFiles: []string{
			"web/src/pages/PokemonSets.tsx",
			"web/src/pages/PokemonSet.tsx",
			"web/src/pages/PokemonTop.tsx",
			"web/src/pages/PokemonScanned.tsx",
		},
		FeatureFlag: "pokemon",
	},
	{
		Slug:        "family-chat",
		Title:       "Family Chat",
		Route:       "/family-chat",
		Description: "Real-time family messaging with attachments, reactions, voice calls, and turn-based prompts.",
		SourceFiles: []string{
			"web/src/pages/FamilyChat.tsx",
			"internal/familychat/handlers.go",
			"internal/familychat/hub.go",
		},
		FeatureFlag: "family_chat",
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
