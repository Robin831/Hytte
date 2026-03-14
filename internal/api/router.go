package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/links"
	"github.com/Robin831/Hytte/internal/notes"
	"github.com/Robin831/Hytte/internal/push"
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

			// Notes (markdown knowledge base).
			r.Get("/notes", notes.ListHandler(db))
			r.Post("/notes", notes.CreateHandler(db))
			r.Get("/notes/tags", notes.TagsHandler(db))
			r.Get("/notes/{id}", notes.GetHandler(db))
			r.Put("/notes/{id}", notes.UpdateHandler(db))
			r.Delete("/notes/{id}", notes.DeleteHandler(db))
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
