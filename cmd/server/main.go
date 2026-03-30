package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Robin831/Hytte/internal/allowance"
	"github.com/Robin831/Hytte/internal/api"
	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/daemon"
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

	// Clean up chore photos older than 7 days on startup and daily.
	allowance.CleanOldCompletionPhotos(database)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			allowance.CleanOldCompletionPhotos(database)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Start periodic push notification scheduler (streak warnings + weekly summaries).
	// The context is canceled as soon as the shutdown signal is received so background
	// work stops before the DB is closed.
	notifCtx, notifCancel := context.WithCancel(context.Background())
	go daemon.NewScheduler().Run(notifCtx, database, &http.Client{Timeout: 15 * time.Second})

	// Schedule weekly allowance payout generation on Sundays at 21:00 UTC.
	// Runs near end-of-week so all Sunday chores can be completed first.
	go func() {
		for {
			now := time.Now().UTC()
			daysUntilSunday := (7 - int(now.Weekday())) % 7
			var next time.Time
			if daysUntilSunday == 0 {
				todayRun := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, time.UTC)
				if now.Before(todayRun) {
					next = todayRun
				} else {
					next = todayRun.AddDate(0, 0, 7)
				}
			} else {
				next = time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 21, 0, 0, 0, time.UTC)
			}
			timer := time.NewTimer(time.Until(next))
			select {
			case <-notifCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
				allowanceHTTPClient := &http.Client{Timeout: 30 * time.Second}
				allowance.GenerateWeeklyPayouts(database, allowanceHTTPClient)
				log.Println("allowance: weekly payouts generated")
			}
		}
	}()

	// Schedule weekly savings interest payment on Sundays at 00:05 UTC.
	// Uses notifCtx so it stops cleanly on shutdown.
	go func() {
		for {
			now := time.Now().UTC()
			// time.Weekday: Sunday=0. Compute days until Sunday.
			daysUntilSunday := (7 - int(now.Weekday())) % 7
			var next time.Time
			if daysUntilSunday == 0 {
				// Today is Sunday: run at 00:05 today if still upcoming, otherwise next Sunday.
				todayRun := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, time.UTC)
				if now.Before(todayRun) {
					next = todayRun
				} else {
					next = todayRun.AddDate(0, 0, 7)
				}
			} else {
				// Upcoming Sunday at 00:05 UTC.
				next = time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 0, 5, 0, 0, time.UTC)
			}
			timer := time.NewTimer(time.Until(next))
			select {
			case <-notifCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
				ctx, cancel := context.WithTimeout(notifCtx, 30*time.Second)
				if err := stars.PayInterest(ctx, database, time.Now()); err != nil {
					log.Printf("savings: weekly interest payment error: %v", err)
				} else {
					log.Println("savings: weekly interest paid")
				}
				cancel()
			}
		}
	}()

	router := api.NewRouter(database)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	go func() {
		log.Printf("Hytte server starting on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	notifCancel() // Stop scheduler before shutting down the DB.
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server stopped")
}
