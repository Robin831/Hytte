package main

import (
	"context"
	"encoding/json"
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
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/stars"
	"github.com/Robin831/Hytte/internal/stride"
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

	// Clean up chore photos older than 7 days on startup (goroutine started below after notifCtx).
	allowance.CleanOldCompletionPhotos(database)

	// Graceful shutdown on SIGINT/SIGTERM.
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Start periodic push notification scheduler (streak warnings + weekly summaries).
	// The context is canceled as soon as the shutdown signal is received so background
	// work stops before the DB is closed.
	notifCtx, notifCancel := context.WithCancel(context.Background())
	go daemon.NewScheduler().Run(notifCtx, database, &http.Client{Timeout: 15 * time.Second})

	// Daily photo cleanup tied to shutdown context so it stops before the DB is closed.
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-notifCtx.Done():
				return
			case <-ticker.C:
				allowance.CleanOldCompletionPhotos(database)
			}
		}
	}()

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

	// Schedule nightly Stride evaluation at 03:00 Europe/Oslo.
	// Evaluates workouts from the previous day against their planned sessions
	// and sends push notifications for critical flags.
	go func() {
		oslo, err := time.LoadLocation("Europe/Oslo")
		if err != nil {
			log.Printf("stride eval: failed to load Europe/Oslo timezone: %v", err)
			return
		}
		for {
			next := stride.NextNightlyEvaluationRun(time.Now(), oslo)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-notifCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
				evalHTTPClient := &http.Client{Timeout: 120 * time.Second}
				evalCtx, evalCancel := context.WithTimeout(notifCtx, 10*time.Minute)
				if err := stride.RunNightlyEvaluation(evalCtx, database, evalHTTPClient); err != nil {
					log.Printf("stride eval: nightly evaluation error: %v", err)
				} else {
					log.Println("stride eval: nightly evaluation complete")
				}
				evalCancel()
			}
		}
	}()

	// Schedule weekly Stride plan generation on Sundays at 18:00 Europe/Oslo.
	// Generates a training plan for each user with stride_enabled=true and sends
	// a push notification confirming the plan is ready.
	go func() {
		oslo, err := time.LoadLocation("Europe/Oslo")
		if err != nil {
			log.Printf("stride: failed to load Europe/Oslo timezone: %v", err)
			return
		}
		for {
			next := stride.NextStrideRun(time.Now(), oslo)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-notifCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
				rows, err := database.QueryContext(notifCtx,
					`SELECT DISTINCT user_id FROM user_preferences WHERE key='stride_enabled' AND value='true'`)
				if err != nil {
					log.Printf("stride: query enabled users: %v", err)
					continue
				}
				var userIDs []int64
				for rows.Next() {
					var id int64
					if err := rows.Scan(&id); err != nil {
						log.Printf("stride: scan user id: %v", err)
						continue
					}
					userIDs = append(userIDs, id)
				}
				if err := rows.Err(); err != nil {
					log.Printf("stride: rows iteration error: %v", err)
				}
				rows.Close()

				strideHTTPClient := &http.Client{Timeout: 120 * time.Second}
				for _, userID := range userIDs {
					planCtx, planCancel := context.WithTimeout(notifCtx, 90*time.Second)
					if err := stride.GeneratePlan(planCtx, database, userID, "next"); err != nil {
						log.Printf("stride: generate plan for user %d: %v", userID, err)
						planCancel()
						continue
					}
					planCancel()

					notif := push.Notification{
						Title: "Stride",
						Body:  "Stride has your plan for next week",
						Tag:   "stride-weekly-plan",
					}
					payload, err := json.Marshal(notif)
					if err != nil {
						log.Printf("stride: marshal notification for user %d: %v", userID, err)
						continue
					}
					if _, err := push.SendToUser(database, strideHTTPClient, userID, payload); err != nil {
						log.Printf("stride: push notification for user %d: %v", userID, err)
					}
				}
				log.Printf("stride: weekly plan generation complete (%d users)", len(userIDs))
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
