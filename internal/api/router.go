package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/dashboard"
	"github.com/Robin831/Hytte/internal/infra"
	"github.com/Robin831/Hytte/internal/lactate"
	"github.com/Robin831/Hytte/internal/links"
	"github.com/Robin831/Hytte/internal/notes"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
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
			r.Get("/auth/me", auth.MeHandler())
		})

		// Webhook receiver — public, no auth required.
		// Accepts any HTTP method so external services can POST/PUT/etc.
		r.HandleFunc("/hooks/{endpointID}", webhooks.ReceiveWebhook(db, webhookHub))

		// All other API routes require authentication by default.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth(db))

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

			// Short links.
			r.Get("/links", links.ListHandler(db))
			r.Post("/links", links.CreateHandler(db))
			r.Put("/links/{id}", links.UpdateHandler(db))
			r.Delete("/links/{id}", links.DeleteHandler(db))

			// Webhook management.
			r.Get("/webhooks", webhooks.ListEndpoints(db))
			r.Post("/webhooks", webhooks.CreateEndpoint(db))
			r.Delete("/webhooks/{endpointID}", webhooks.DeleteEndpoint(db))
			r.Get("/webhooks/{endpointID}/requests", webhooks.ListRequests(db))
			r.Delete("/webhooks/{endpointID}/requests", webhooks.ClearRequests(db))
			r.Get("/webhooks/{endpointID}/stream", webhooks.StreamRequests(db, webhookHub))

			// Push notification subscriptions.
			r.Post("/push/subscribe", push.SubscribeHandler(db))
			r.Delete("/push/subscribe", push.UnsubscribeHandler(db))
			r.Get("/push/subscriptions", push.SubscriptionsListHandler(db))
			r.Delete("/push/subscriptions/{id}", push.DeleteSubscriptionByIDHandler(db))
			r.Post("/push/test", push.TestNotificationHandler(db, push.DefaultHTTPClient))

			// Notes (markdown knowledge base).
			r.Get("/notes", notes.ListHandler(db))
			r.Post("/notes", notes.CreateHandler(db))
			r.Get("/notes/tags", notes.TagsHandler(db))
			r.Get("/notes/{id}", notes.GetHandler(db))
			r.Put("/notes/{id}", notes.UpdateHandler(db))
			r.Delete("/notes/{id}", notes.DeleteHandler(db))

			// Lactate tests.
			r.Get("/lactate/tests", lactate.ListHandler(db))
			r.Post("/lactate/tests", lactate.CreateHandler(db))
			r.Get("/lactate/tests/{id}", lactate.GetHandler(db))
			r.Put("/lactate/tests/{id}", lactate.UpdateHandler(db))
			r.Delete("/lactate/tests/{id}", lactate.DeleteHandler(db))
			r.Get("/lactate/tests/{id}/thresholds", lactate.ThresholdsHandler(db))
			r.Get("/lactate/tests/{id}/analysis", lactate.AnalysisHandler(db))
			r.Post("/lactate/calculate", lactate.CalculateHandler())

			// Dashboard.
			r.Get("/dashboard/activity", dashboard.ActivityHandler(db))

			// Training / workouts.
			r.Post("/training/upload", training.UploadHandler(db))
			r.Get("/training/workouts", training.ListHandler(db))
			r.Get("/training/workouts/{id}", training.GetHandler(db))
			r.Put("/training/workouts/{id}", training.UpdateHandler(db))
			r.Delete("/training/workouts/{id}", training.DeleteHandler(db))
			r.Get("/training/workouts/{id}/similar", training.SimilarHandler(db))
			r.Get("/training/workouts/{id}/zones", training.ZonesHandler(db))
			r.Post("/training/workouts/{id}/analyze", training.AnalyzeHandler(db))
			r.Get("/training/workouts/{id}/analysis", training.GetAnalysisHandler(db))
			r.Delete("/training/workouts/{id}/analysis", training.DeleteAnalysisHandler(db))
			r.Post("/training/workouts/{id}/insights", training.InsightsHandler(db))
			r.Get("/training/compare", training.CompareHandler(db))
			r.Post("/training/compare/analyze", training.CompareAnalyzeHandler(db))
			r.Get("/training/summary", training.SummaryHandler(db))
			r.Get("/training/progression", training.ProgressionHandler(db))

			// Claude CLI test.
			r.Post("/settings/claude-test", training.ClaudeTestHandler(db))

			// Infrastructure monitoring.
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
