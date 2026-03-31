package api

import (
	"database/sql"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/allowance"
	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/chat"
	"github.com/Robin831/Hytte/internal/dashboard"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/forge"
	"github.com/Robin831/Hytte/internal/infra"
	"github.com/Robin831/Hytte/internal/kiosk"
	"github.com/Robin831/Hytte/internal/lactate"
	"github.com/Robin831/Hytte/internal/links"
	"github.com/Robin831/Hytte/internal/netatmo"
	"github.com/Robin831/Hytte/internal/notes"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/settings"
	"github.com/Robin831/Hytte/internal/stars"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/Robin831/Hytte/internal/transit"
	"github.com/Robin831/Hytte/internal/weather"
	"github.com/Robin831/Hytte/internal/webhooks"
	"github.com/Robin831/Hytte/internal/workhours"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter creates and configures the Chi router with API routes and static
// file serving for the SPA frontend.
func NewRouter(db *sql.DB) http.Handler {
	r := chi.NewRouter()
	webhookHub := webhooks.NewHub()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "https://localhost:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Forge dashboard — read-only connection to the forge state database and
	// IPC client for the forge daemon. Either may be nil if forge is not
	// installed or the daemon is not running; handlers degrade gracefully.
	forgeDB, err := forge.Open()
	if err != nil {
		log.Printf("forge: state DB unavailable (%v) — /api/forge/* will return 503", err)
	}
	rawForgeClient, err := forge.NewClient()
	if err != nil {
		log.Printf("forge: IPC client unavailable (%v) — /api/forge/status will return 503", err)
	}
	var forgeClient forge.IPCClient
	if rawForgeClient != nil {
		forgeClient = rawForgeClient
	}

	// Infrastructure module registry pre-populated with built-in modules.
	infraRegistry := infra.NewDefaultRegistry(db)

	// Transit service (Entur API client with 30-second departure cache).
	transitSvc := transit.NewService()

	// Netatmo weather station client (5-minute API response cache).
	netatmoOAuth := netatmo.ClientFromEnv()
	netatmoClient := netatmo.NewClient(netatmoOAuth, db)

	// API routes.
	r.Route("/api", func(r chi.Router) {
		// Public routes — no authentication required.
		r.Get("/health", HealthHandler(db))
		r.Get("/auth/google/login", auth.GoogleLoginHandler())
		r.Get("/auth/google/callback", auth.GoogleCallbackHandler(db))
		r.Post("/auth/logout", auth.LogoutHandler(db))

		// Weather (public — no auth needed for forecasts).
		weatherSvc := weather.NewService()
		r.Get("/weather/forecast", weatherSvc.ForecastHandler())
		r.Get("/weather/locations", weather.LocationsHandler())
		r.Get("/weather/search", weatherSvc.SearchHandler())

		// Push notifications — public VAPID key endpoint.
		r.Get("/push/vapid-key", push.VAPIDKeyHandler(db))

		// Kiosk data endpoint — authenticated by ?token= query parameter via KioskAuth.
		r.Group(func(r chi.Router) {
			r.Use(kiosk.KioskAuth(db))
			r.Get("/kiosk/data", kiosk.DataHandler(db, transitSvc, netatmoClient, weatherSvc))
		})

		// /auth/me uses OptionalAuth (returns user if logged in, null otherwise).
		r.Group(func(r chi.Router) {
			r.Use(auth.OptionalAuth(db))
			r.Get("/auth/me", auth.MeHandler(db))
		})

		// Test auth endpoint — only available when HYTTE_TEST_AUTH=1.
		// Creates a test admin user and session for automated testing (e.g. QuestGiver).
		if os.Getenv("HYTTE_TEST_AUTH") == "1" {
			r.Post("/auth/test-login", auth.TestLoginHandler(db))
		}

		// Webhook receiver — public, no auth required.
		// Accepts any HTTP method so external services can POST/PUT/etc.
		r.HandleFunc("/hooks/{endpointID}", webhooks.ReceiveWebhook(db, webhookHub))

		// Upload route — accepts bearer token (HYTTE_UPLOAD_TOKEN) or session cookie.
		// Bearer token is checked first; a wrong or absent token falls through to
		// session-cookie auth. Pulled out of the RequireAuth group so it can use
		// RequireAuthOrToken instead.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuthOrToken(db))
			r.Use(auth.WithFeatures(db))
			r.Use(auth.RequireFeature(db, "training"))
			r.Post("/training/upload", training.UploadHandler(db))
		})

		// All other API routes require authentication by default.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth(db))
			// Load the user's feature map once and cache it in the request
			// context so nested RequireFeature checks share a single DB query.
			r.Use(auth.WithFeatures(db))

			// Admin routes — require admin access.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin())
				r.Get("/admin/users", auth.AdminListUsersHandler(db))
				r.Put("/admin/users/{id}/features", auth.AdminSetFeatureHandler(db))
				r.Post("/admin/stars/award", stars.AdminAwardStarsHandler(db))

				// AI prompt management — admin only.
				r.Get("/settings/ai-prompts", settings.GetAIPromptsHandler(db))
				r.Put("/settings/ai-prompts/{key}", settings.PutAIPromptHandler(db))
				r.Delete("/settings/ai-prompts/{key}", settings.DeleteAIPromptHandler(db))

				// Kiosk token management — admin only.
				r.Post("/kiosk/tokens", kiosk.CreateTokenHandler(db))
				r.Get("/kiosk/tokens", kiosk.ListTokensHandler(db))
				r.Delete("/kiosk/tokens/{id}", kiosk.DeleteTokenHandler(db))

				// Forge dashboard — admin only, registered only when FEATURE_FORGE_DASHBOARD=1.
				// The feature flag is evaluated at startup; when disabled, these routes are
				// not registered and return 404.
				if os.Getenv("FEATURE_FORGE_DASHBOARD") == "1" {
					r.Get("/forge/status", forge.StatusHandler(forgeDB, forgeClient))
					r.Get("/forge/workers", forge.WorkersHandler(forgeDB))
					r.Get("/forge/queue", forge.QueueHandler(forgeDB))
					r.Get("/forge/prs", forge.PRsHandler(forgeDB))
					r.Get("/forge/events", forge.EventsHandler(forgeDB))
					r.Get("/forge/costs", forge.CostsHandler(forgeDB))
					r.Post("/forge/beads/{id}/retry", forge.RetryBeadHandler(forgeClient))
				}
			})

			// Settings: event types list (requires auth — only needed on authenticated Settings page).
			r.Get("/settings/event-types", auth.EventTypesHandler())

			// User preferences.
			r.Get("/settings/preferences", auth.PreferencesGetHandler(db))
			r.Put("/settings/preferences", auth.PreferencesPutHandler(db))

			// Session management.
			r.Get("/settings/sessions", auth.SessionsListHandler(db))
			r.Post("/settings/sessions/revoke-others", auth.SignOutEverywhereHandler(db))

			// Account deletion.
			r.Delete("/settings/account", auth.DeleteAccountHandler(db))

			// Short links — gated by "links" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "links"))
				r.Get("/links", links.ListHandler(db))
				r.Post("/links", links.CreateHandler(db))
				r.Put("/links/{id}", links.UpdateHandler(db))
				r.Delete("/links/{id}", links.DeleteHandler(db))
			})

			// Webhook management — gated by "webhooks" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "webhooks"))
				r.Get("/webhooks", webhooks.ListEndpoints(db))
				r.Post("/webhooks", webhooks.CreateEndpoint(db))
				r.Delete("/webhooks/{endpointID}", webhooks.DeleteEndpoint(db))
				r.Get("/webhooks/{endpointID}/requests", webhooks.ListRequests(db))
				r.Delete("/webhooks/{endpointID}/requests", webhooks.ClearRequests(db))
				r.Get("/webhooks/{endpointID}/stream", webhooks.StreamRequests(db, webhookHub))
			})

			// Push notification subscriptions.
			r.Post("/push/subscribe", push.SubscribeHandler(db))
			r.Delete("/push/subscribe", push.UnsubscribeHandler(db))
			r.Get("/push/subscriptions", push.SubscriptionsListHandler(db))
			r.Delete("/push/subscriptions/{id}", push.DeleteSubscriptionByIDHandler(db))
			r.Post("/push/test", push.TestNotificationHandler(db, push.DefaultHTTPClient))

			// Notes — gated by "notes" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "notes"))
				r.Get("/notes", notes.ListHandler(db))
				r.Post("/notes", notes.CreateHandler(db))
				r.Get("/notes/tags", notes.TagsHandler(db))
				r.Get("/notes/{id}", notes.GetHandler(db))
				r.Put("/notes/{id}", notes.UpdateHandler(db))
				r.Delete("/notes/{id}", notes.DeleteHandler(db))
			})

			// Lactate tests — gated by "lactate" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "lactate"))
				r.Get("/lactate/tests", lactate.ListHandler(db))
				r.Post("/lactate/tests", lactate.CreateHandler(db))
				r.Get("/lactate/tests/{id}", lactate.GetHandler(db))
				r.Put("/lactate/tests/{id}", lactate.UpdateHandler(db))
				r.Delete("/lactate/tests/{id}", lactate.DeleteHandler(db))
				r.Get("/lactate/tests/{id}/thresholds", lactate.ThresholdsHandler(db))
				r.Get("/lactate/tests/{id}/analysis", lactate.AnalysisHandler(db))
				r.Post("/lactate/calculate", lactate.CalculateHandler())
				r.Post("/lactate/tests/preview-from-workout", lactate.PreviewFromWorkoutHandler(db))
				r.Post("/lactate/tests/from-workout", lactate.ImportFromWorkoutHandler(db))
			})

			// Dashboard.
			r.Get("/dashboard/activity", dashboard.ActivityHandler(db))

			// Training / workouts — gated by "training" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "training"))
				r.Get("/training/workouts", training.ListHandler(db))
				r.Get("/training/workouts/{id}", training.GetHandler(db))
				r.Put("/training/workouts/{id}", training.UpdateHandler(db))
				r.Delete("/training/workouts/{id}", training.DeleteHandler(db))
				r.Get("/training/workouts/{id}/similar", training.SimilarHandler(db))
				r.Get("/training/workouts/{id}/zones", training.ZonesHandler(db))
				r.Get("/training/summary", training.SummaryHandler(db))
				r.Get("/training/progression", training.ProgressionHandler(db))
				r.Get("/training/compare", training.CompareHandler(db))
				r.Get("/training/acr-trend", training.ACRTrendHandler(db))
				r.Post("/training/metrics/backfill", training.MetricsBackfillHandler(db))
				r.Get("/training/load", training.GetTrainingLoadHandler(db))
				r.Get("/training/vo2max", training.GetVO2maxHandler(db))
				r.Get("/training/predictions", training.GetRacePredictionsHandler(db))

				// Claude AI analysis endpoints — additionally gated by "claude_ai" feature.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireFeature(db, "claude_ai"))
					r.Post("/training/workouts/{id}/analyze", training.AnalyzeHandler(db))
					r.Get("/training/workouts/{id}/analysis", training.GetAnalysisHandler(db))
					r.Delete("/training/workouts/{id}/analysis", training.DeleteAnalysisHandler(db))
					r.Get("/training/workouts/{id}/insights", training.GetCachedInsightsHandler(db))
					r.Post("/training/workouts/{id}/insights", training.InsightsHandler(db))
					r.Post("/training/compare/analyze", training.CompareAnalyzeHandler(db))
					r.Get("/training/compare/analyses", training.ListComparisonAnalysesHandler(db))
					r.Get("/training/compare/analyses/{id}", training.GetComparisonAnalysisHandler(db))
					r.Delete("/training/compare/analyses/{id}", training.DeleteComparisonAnalysisHandler(db))
					r.Post("/training/summary/analyze", training.AnalyzeTrainingSummaryHandler(db))
				})
			})

			// Claude CLI test — gated by "claude_ai" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "claude_ai"))
				r.Post("/settings/claude-test", training.ClaudeTestHandler(db))
			})

			// Chat — gated by "chat" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "chat"))
				r.Get("/chat/conversations", chat.ListHandler(db))
				r.Post("/chat/conversations", chat.CreateHandler(db))
				r.Get("/chat/conversations/{id}", chat.GetHandler(db))
				r.Delete("/chat/conversations/{id}", chat.DeleteHandler(db))
				r.Put("/chat/conversations/{id}", chat.RenameHandler(db))
				r.Post("/chat/conversations/{id}/messages", chat.SendMessageHandler(db))
			})

			// Kids Stars: family management — gated by "kids_stars" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "kids_stars"))
				r.Get("/family/status", family.StatusHandler(db))
				r.Get("/family/my-family", family.MyFamilyHandler(db))
				r.Get("/family/children", family.ListChildrenHandler(db))
				r.Put("/family/children/{id}", family.UpdateChildHandler(db))
				r.Delete("/family/children/{id}", family.UnlinkChildHandler(db))
				r.Get("/family/children/{id}/stats", family.ChildStatsHandler(db))
				r.Get("/family/children/{id}/workouts", family.ChildWorkoutsHandler(db))
				r.Get("/family/children/{id}/settings", stars.GetChildSettingsHandler(db))
				r.Put("/family/children/{id}/settings", stars.PutChildSettingsHandler(db))
				r.Post("/family/invite", family.GenerateInviteHandler(db))
				r.Post("/family/invite/accept", family.AcceptInviteHandler(db))
				// Reward management (parent-facing).
				r.Get("/family/rewards", family.ListRewardsHandler(db))
				r.Post("/family/rewards", family.CreateRewardHandler(db))
				r.Put("/family/rewards/{id}", family.UpdateRewardHandler(db))
				r.Delete("/family/rewards/{id}", family.DeleteRewardHandler(db))
				// Claim management (parent-facing).
				r.Get("/family/claims", family.ListClaimsHandler(db))
				r.Put("/family/claims/{id}", family.ResolveClaimHandler(db))
				// Challenge management (parent-facing).
				r.Get("/family/challenges", family.ListChallengesHandler(db))
				r.Post("/family/challenges", family.CreateChallengeHandler(db))
				r.Put("/family/challenges/{id}", family.UpdateChallengeHandler(db))
				r.Delete("/family/challenges/{id}", family.DeleteChallengeHandler(db))
				r.Get("/family/challenges/participants", family.ListAllChallengeParticipantsHandler(db))
				r.Get("/family/challenges/{id}/participants", family.ListChallengeParticipantsHandler(db))
				r.Post("/family/challenges/{id}/participants", family.AddParticipantHandler(db))
				r.Delete("/family/challenges/{id}/participants/{childId}", family.RemoveParticipantHandler(db))
			})

			// Stars balance and transaction history — gated by "kids_stars" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "kids_stars"))
				r.Get("/stars/balance", stars.BalanceHandler(db))
				r.Get("/stars/transactions", stars.TransactionsHandler(db))
				r.Get("/stars/streaks", stars.StreaksHandler(db))
				r.Get("/stars/badges", stars.BadgesHandler(db))
				r.Get("/stars/badges/available", stars.AvailableBadgesHandler(db))
				r.Get("/stars/weekly-bonus-summary", stars.WeeklyBonusSummaryHandler(db))
				r.Get("/stars/leaderboard", stars.LeaderboardHandler(db))
				// Rewards and claims (kid-facing).
				r.Get("/stars/rewards", stars.KidRewardsHandler(db))
				r.Post("/stars/rewards/{id}/claim", stars.ClaimRewardHandler(db))
				r.Get("/stars/claims", stars.KidClaimsHandler(db))
				// Challenges (kid-facing).
				r.Get("/stars/challenges", stars.KidChallengesHandler(db))
				// Story journey.
				r.Get("/stars/journey", stars.GetJourneyHandler(db))
				r.Put("/stars/journey/theme", stars.ChangeThemeHandler(db))
				// Star Bank (savings account).
				r.Get("/stars/savings", stars.GetSavingsHandler(db))
				r.Post("/stars/savings/deposit", stars.DepositSavingsHandler(db))
				r.Post("/stars/savings/withdraw", stars.WithdrawSavingsHandler(db))
				// Workout Bingo.
				r.Get("/stars/bingo", stars.BingoHandler(db))
				// Beat My Parent distance challenge.
				r.Get("/stars/beat-parent", stars.BeatMyParentHandler(db))
			})

			// Kids Allowance: chore management and earnings — gated by "kids_allowance" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "kids_allowance"))
				// Parent: chore management.
				r.Get("/allowance/chores", allowance.ListChoresHandler(db))
				r.Post("/allowance/chores", allowance.CreateChoreHandler(db))
				r.Put("/allowance/chores/{id}", allowance.UpdateChoreHandler(db))
				r.Delete("/allowance/chores/{id}", allowance.DeactivateChoreHandler(db))
				// Parent: approval flow.
				r.Get("/allowance/pending", allowance.ListPendingHandler(db))
				r.Post("/allowance/approve/{id}", allowance.ApproveCompletionHandler(db))
				r.Post("/allowance/reject/{id}", allowance.RejectCompletionHandler(db))
				r.Post("/allowance/quality-bonus/{id}", allowance.QualityBonusHandler(db))
				// Parent: extra tasks.
				r.Get("/allowance/extras", allowance.ListExtrasHandler(db))
				r.Post("/allowance/extras", allowance.CreateExtraHandler(db))
				r.Post("/allowance/extras/{id}/approve", allowance.ApproveExtraHandler(db))
				// Parent: payouts.
				r.Get("/allowance/payouts", allowance.ListPayoutsHandler(db))
				r.Post("/allowance/payouts/{id}/paid", allowance.MarkPaidHandler(db))
				// Parent: bonus rules.
				r.Get("/allowance/bonuses", allowance.ListBonusRulesHandler(db))
				r.Put("/allowance/bonuses", allowance.UpdateBonusRulesHandler(db))
				// Parent: per-child settings.
				r.Get("/allowance/children/{id}/settings", allowance.GetChildSettingsHandler(db))
				r.Put("/allowance/children/{id}/settings", allowance.UpdateChildSettingsHandler(db))
				// Parent: per-child savings goals.
				r.Get("/allowance/children/{id}/goals", allowance.ListChildGoalsHandler(db))
				r.Post("/allowance/children/{id}/goals", allowance.CreateChildGoalHandler(db))
				r.Put("/allowance/children/{id}/goals/{goalId}", allowance.UpdateChildGoalHandler(db))
				r.Delete("/allowance/children/{id}/goals/{goalId}", allowance.DeleteChildGoalHandler(db))
				// Kid: sibling identity for team-chore UI (child_id, nickname, avatar_emoji only).
				r.Get("/allowance/my/siblings", allowance.MySiblingsHandler(db))
				// Kid: chores and completions.
				r.Get("/allowance/my/chores", allowance.MyChoresHandler(db))
				r.Post("/allowance/my/complete/{id}", allowance.CompleteChoreHandler(db))
				r.Post("/allowance/my/team-start/{chore_id}", allowance.TeamStartHandler(db))
				r.Post("/allowance/my/team-join/{completion_id}", allowance.TeamJoinHandler(db))
				// Kid: extras.
				r.Get("/allowance/my/extras", allowance.MyExtrasHandler(db))
				r.Post("/allowance/my/claim-extra/{id}", allowance.ClaimExtraHandler(db))
				r.Post("/allowance/my/complete-extra/{id}", allowance.CompleteExtraHandler(db))
				// Kid: earnings.
				r.Get("/allowance/my/earnings", allowance.MyEarningsHandler(db))
				r.Get("/allowance/my/history", allowance.MyHistoryHandler(db))
				// Kid: bingo card.
				r.Get("/allowance/my/bingo", allowance.MyBingoHandler(db))
				// Kid: savings goals.
				r.Get("/allowance/my/goals", allowance.MyGoalsHandler(db))
				r.Post("/allowance/my/goals", allowance.CreateMyGoalHandler(db))
				r.Put("/allowance/my/goals/{id}", allowance.UpdateMyGoalHandler(db))
				// Photo serving (accessible to the child who uploaded and the parent).
				r.Get("/allowance/photos/{completion_id}", allowance.ServePhotoHandler(db))
			})

			// Transit departures — gated by "transit" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "transit"))
				r.Get("/transit/departures", transit.DeparturesHandler(db, transitSvc))
				r.Get("/transit/search", transit.SearchHandler(transitSvc))
				r.Get("/transit/settings", transit.SettingsGetHandler(db))
				r.Put("/transit/settings", transit.SettingsPutHandler(db))
			})

			// Netatmo weather station — gated by "netatmo" feature.
			// Returns 404 when disabled so the endpoints are hidden from users
			// who do not have the feature enabled.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeatureOrNotFound(db, "netatmo"))
				r.Get("/netatmo/current", netatmo.CurrentHandler(netatmoClient, db))
				r.Get("/netatmo/history", netatmo.HistoryHandler(netatmoClient, db))
			})

			// Netatmo OAuth management — admin only.
			// Allows the admin to connect and disconnect the shared Netatmo account.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin())
				r.Get("/netatmo/auth/login", netatmo.OAuthLoginHandler(netatmoOAuth))
				r.Get("/netatmo/callback", netatmo.OAuthCallbackHandler(netatmoOAuth, db))
				r.Get("/netatmo/status", netatmo.OAuthStatusHandler(db))
				r.Delete("/netatmo/token", netatmo.OAuthDisconnectHandler(db))
			})

			// Work hours tracking — gated by "work_hours" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "work_hours"))
				r.Get("/workhours/day", workhours.DayGetHandler(db))
				r.Put("/workhours/day", workhours.DayPutHandler(db))
				r.Delete("/workhours/day", workhours.DayDeleteHandler(db))
				r.Post("/workhours/day/session", workhours.SessionAddHandler(db))
				r.Put("/workhours/day/session/{id}", workhours.SessionUpdateHandler(db))
				r.Delete("/workhours/day/session/{id}", workhours.SessionDeleteHandler(db))
				r.Post("/workhours/day/deduction", workhours.DeductionAddHandler(db))
				r.Delete("/workhours/day/deduction/{id}", workhours.DeductionDeleteHandler(db))
				r.Get("/workhours/presets", workhours.PresetsListHandler(db))
				r.Post("/workhours/presets", workhours.PresetCreateHandler(db))
				r.Put("/workhours/presets/{id}", workhours.PresetUpdateHandler(db))
				r.Delete("/workhours/presets/{id}", workhours.PresetDeleteHandler(db))
				r.Get("/workhours/summary/week", workhours.WeekSummaryHandler(db))
				r.Get("/workhours/summary/month", workhours.MonthSummaryHandler(db))
				r.Get("/workhours/flex", workhours.FlexPoolHandler(db))
				r.Post("/workhours/flex/reset", workhours.FlexResetHandler(db))
				r.Get("/workhours/leave", workhours.LeaveDayListHandler(db))
				r.Put("/workhours/leave", workhours.LeaveDayPutHandler(db))
				r.Delete("/workhours/leave", workhours.LeaveDayDeleteHandler(db))
				r.Get("/workhours/leave/balance", workhours.LeaveBalanceHandler(db))
				r.Post("/workhours/punch-in", workhours.PunchInHandler(db))
				r.Get("/workhours/punch-session", workhours.GetPunchSessionHandler(db))
				r.Delete("/workhours/punch-session", workhours.DeletePunchSessionHandler(db))
				r.Post("/workhours/punch-out", workhours.PunchOutHandler(db))
			})

			// Infrastructure monitoring — gated by "infra" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "infra"))
				r.Get("/infra/status", infra.StatusHandler(db, infraRegistry))
				r.Get("/infra/modules", infra.ModulesListHandler(db, infraRegistry))
				r.Put("/infra/modules/{name}", infra.ModuleToggleHandler(db, infraRegistry))
				r.Get("/infra/modules/{name}/detail", infra.ModuleDetailHandler(db, infraRegistry))

				// Infra: health check service management.
				r.Get("/infra/health-checks", infra.ListHealthServicesHandler(db))
				r.Post("/infra/health-checks", infra.AddHealthServiceHandler(db))
				r.Delete("/infra/health-checks/{id}", infra.DeleteHealthServiceHandler(db))

				// Infra: SSL certificate host management.
				r.Get("/infra/ssl-certs", infra.ListSSLHostsHandler(db))
				r.Post("/infra/ssl-certs", infra.AddSSLHostHandler(db))
				r.Delete("/infra/ssl-certs/{id}", infra.DeleteSSLHostHandler(db))

				// Infra: uptime history.
				r.Get("/infra/uptime", infra.UptimeHistoryHandler(db))
				r.Delete("/infra/uptime", infra.ClearUptimeHistoryHandler(db))

				// Infra: Hetzner API token management (shared by VPS stats and bandwidth modules).
				r.Get("/infra/hetzner/token", infra.HetznerTokenGetHandler(db))
				r.Put("/infra/hetzner/token", infra.HetznerTokenSetHandler(db))
				r.Delete("/infra/hetzner/token", infra.HetznerTokenDeleteHandler(db))

				// Infra: Docker host management.
				r.Get("/infra/docker-hosts", infra.ListDockerHostsHandler(db))
				r.Post("/infra/docker-hosts", infra.AddDockerHostHandler(db))
				r.Delete("/infra/docker-hosts/{id}", infra.DeleteDockerHostHandler(db))

				// Infra: GitHub Actions token and repository management.
				r.Get("/infra/github/token", infra.GitHubTokenGetHandler(db))
				r.Put("/infra/github/token", infra.GitHubTokenSetHandler(db))
				r.Delete("/infra/github/token", infra.GitHubTokenDeleteHandler(db))
				r.Get("/infra/github/repos", infra.ListGitHubReposHandler(db))
				r.Post("/infra/github/repos", infra.AddGitHubRepoHandler(db))
				r.Delete("/infra/github/repos/{id}", infra.DeleteGitHubRepoHandler(db))

				// Infra: DNS monitoring.
				r.Get("/infra/dns-monitors", infra.ListDNSMonitorsHandler(db))
				r.Post("/infra/dns-monitors", infra.AddDNSMonitorHandler(db))
				r.Delete("/infra/dns-monitors/{id}", infra.DeleteDNSMonitorHandler(db))

				// Infra: systemd service monitoring.
				r.Get("/infra/systemd-services", infra.ListSystemdServicesHandler(db))
				r.Post("/infra/systemd-services", infra.AddSystemdServiceHandler(db))
				r.Delete("/infra/systemd-services/{id}", infra.DeleteSystemdServiceHandler(db))

				// Infra: per-module preferences.
				r.Get("/infra/modules/preferences", infra.AllModulePreferencesHandler(db))
				r.Get("/infra/modules/{name}/preferences", infra.ModulePreferencesGetHandler(db, infraRegistry))
				r.Put("/infra/modules/{name}/preferences", infra.ModulePreferencesPutHandler(db, infraRegistry))
				r.Delete("/infra/modules/{name}/preferences", infra.ModulePreferencesDeleteHandler(db, infraRegistry))
			})
		})
	})

	// Short link redirect (outside /api, public).
	r.Get("/go/{code}", links.RedirectHandler(db))

	// Serve static files from ./web/dist with SPA fallback.
	spaHandler := spaFileServer("web/dist")
	r.Handle("/*", spaHandler)

	return r
}

// spaFileServer serves static files and falls back to index.html for SPA
// client-side routing.
func spaFileServer(dir string) http.HandlerFunc {
	absDir, _ := filepath.Abs(dir)

	return func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash and clean the path.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		fullPath := filepath.Join(absDir, filepath.FromSlash(path))

		// Check if the file exists.
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			// SPA fallback: serve index.html for any route that doesn't match
			// a static file.
			http.ServeFile(w, r, filepath.Join(absDir, "index.html"))
			return
		}

		// Prevent directory listing.
		if info.Mode().Type() == fs.ModeDir {
			http.ServeFile(w, r, filepath.Join(absDir, "index.html"))
			return
		}

		http.ServeFile(w, r, fullPath)
	}
}
