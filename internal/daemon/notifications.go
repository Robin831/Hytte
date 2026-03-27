package daemon

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/stars"
)

// Scheduler manages periodic push notification checks. Deduplication state is
// persisted in the daemon_notification_sent DB table so it survives restarts.
type Scheduler struct{}

// NewScheduler creates a new Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// Run starts the periodic notification loop. It ticks every minute and fires:
//   - Streak warnings at 19:xx in the user's local timezone.
//   - Weekly summaries on Monday at 08:xx in the parent's local timezone.
//   - Weekly challenge generation on Monday at 08:xx UTC.
//   - Challenge expiry warnings at 10:xx in the child's local timezone.
//
// It blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context, db *sql.DB, httpClient *http.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deliver := func(userID int64, payload []byte) {
				if _, err := push.SendToUser(db, httpClient, userID, payload); err != nil {
					log.Printf("stars: scheduler push user %d: %v", userID, err)
				}
			}
			stars.CheckStreakWarnings(ctx, db, deliver)
			stars.SendWeeklySummaries(ctx, db, deliver)
			stars.CheckChallengeExpiry(ctx, db, deliver)
			s.checkAndGenerateWeeklyChallenges(ctx, db)
		}
	}
}

// checkAndGenerateWeeklyChallenges creates the four fixed system weekly challenges
// for all active children. Fires on Monday after 08:00 UTC. Generation inside
// stars.GenerateWeeklyChallenges is idempotent so repeated calls are safe —
// using >= 8 rather than == 8 ensures a brief daemon downtime during the 08:xx
// window does not cause challenges to be skipped for the entire week.
func (s *Scheduler) checkAndGenerateWeeklyChallenges(ctx context.Context, db *sql.DB) {
	now := time.Now().UTC()
	if !shouldFireWeeklyChallenges(now) {
		return
	}
	if err := stars.GenerateWeeklyChallenges(ctx, db); err != nil {
		log.Printf("daemon: generate weekly challenges: %v", err)
	}
}

// shouldFireWeeklyChallenges reports whether weekly challenge generation should
// run. It returns true on any Monday at or after 08:00 UTC.
func shouldFireWeeklyChallenges(now time.Time) bool {
	utcNow := now.UTC()
	return utcNow.Weekday() == time.Monday && utcNow.Hour() >= 8
}

