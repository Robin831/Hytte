package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Robin831/Hytte/internal/api"
	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/stars"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	database, err := db.Init("hytte.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Seed badge definitions on every startup (INSERT OR IGNORE is idempotent).
	if err := stars.SeedBadges(database); err != nil {
		log.Fatalf("Failed to seed badges: %v", err)
	}

	// Clean up expired sessions on startup and periodically.
	if n, err := auth.CleanExpiredSessions(database); err == nil && n > 0 {
		log.Printf("Cleaned %d expired sessions", n)
	}
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := auth.CleanExpiredSessions(database); err == nil && n > 0 {
				log.Printf("Cleaned %d expired sessions", n)
			}
		}
	}()

	router := api.NewRouter(database)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Hytte server starting on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server stopped")
}
