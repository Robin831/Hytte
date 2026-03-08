package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter creates and configures the Chi router with API routes and static
// file serving for the SPA frontend.
func NewRouter(db *sql.DB, authCfg *auth.Config) http.Handler {
	r := chi.NewRouter()

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
		r.Get("/health", HealthHandler(db))

		// Auth routes — only registered when OAuth is configured.
		if authCfg != nil {
			r.Get("/auth/google/login", auth.LoginHandler(authCfg))
			r.Get("/auth/google/callback", auth.CallbackHandler(authCfg, db))
			r.Post("/auth/logout", auth.LogoutHandler(authCfg, db))
		}

		// /auth/me always available (returns 401 when not logged in).
		r.Get("/auth/me", auth.MeHandler(db))
	})

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
