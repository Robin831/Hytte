package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/chat"
	"github.com/Robin831/Hytte/internal/dashboard"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/infra"
	"github.com/Robin831/Hytte/internal/lactate"
	"github.com/Robin831/Hytte/internal/links"
	"github.com/Robin831/Hytte/internal/notes"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/stars"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/Robin831/Hytte/internal/transit"
	"github.com/Robin831/Hytte/internal/weather"
	"github.com/Robin831/Hytte/internal/webhooks"
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

	// Infrastructure module registry pre-populated with built-in modules.
	infraRegistry := infra.NewDefaultRegistry(db)

	// Transit service (Entur API client with 30-second departure cache).
	transitSvc := transit.NewService()

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

			// Transit departures — gated by "transit" feature.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireFeature(db, "transit"))
				r.Get("/transit/departures", transit.DeparturesHandler(db, transitSvc))
				r.Get("/transit/search", transit.SearchHandler(transitSvc))
				r.Get("/transit/settings", transit.SettingsGetHandler(db))
				r.Put("/transit/settings", transit.SettingsPutHandler(db))
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
