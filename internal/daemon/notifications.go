package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
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
			stars.CheckStreakWarnings(ctx, db, httpClient)
			stars.SendWeeklySummaries(ctx, db, httpClient)
			s.checkAndGenerateWeeklyChallenges(ctx, db)
			s.checkAndSendChallengeExpiryWarnings(ctx, db, httpClient)
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

// checkAndSendChallengeExpiryWarnings sends push notifications to children whose
// active, uncompleted challenges expire in 2 days ("2-day warning") or today
// ("final day warning"). Fires at 10:xx in the child's configured timezone.
func (s *Scheduler) checkAndSendChallengeExpiryWarnings(ctx context.Context, db *sql.DB, httpClient *http.Client) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT child_id FROM family_links`)
	if err != nil {
		log.Printf("daemon: challenge expiry get children: %v", err)
		return
	}

	var childIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("daemon: challenge expiry scan: %v", err)
			rows.Close()
			return
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("daemon: challenge expiry rows: %v", err)
		return
	}

	now := time.Now()
	for _, childID := range childIDs {
		s.maybeSendChallengeExpiryWarning(ctx, db, httpClient, childID, now)
	}
}

func (s *Scheduler) maybeSendChallengeExpiryWarning(ctx context.Context, db *sql.DB, httpClient *http.Client, childID int64, now time.Time) {
	prefs, err := auth.GetPreferences(db, childID)
	if err != nil {
		log.Printf("daemon: challenge expiry prefs child %d: %v", childID, err)
		return
	}

	loc := userLocation(prefs)
	childNow := now.In(loc)
	if childNow.Hour() != 10 {
		return
	}

	today := childNow.Format("2006-01-02")
	twoDaysLater := childNow.AddDate(0, 0, 2).Format("2006-01-02")

	// Find active challenges expiring today or in 2 days that this child has not completed.
	challengeRows, err := db.QueryContext(ctx, `
		SELECT fc.id, fc.end_date
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id
		WHERE cp.child_id = ? AND fc.is_active = 1 AND cp.completed_at = ''
		  AND (fc.end_date = ? OR fc.end_date = ?)
	`, childID, today, twoDaysLater)
	if err != nil {
		log.Printf("daemon: challenge expiry query child %d: %v", childID, err)
		return
	}

	type expiringChallenge struct {
		id      int64
		endDate string
	}
	var expiring []expiringChallenge
	for challengeRows.Next() {
		var c expiringChallenge
		if err := challengeRows.Scan(&c.id, &c.endDate); err != nil {
			log.Printf("daemon: challenge expiry scan challenge: %v", err)
			challengeRows.Close()
			return
		}
		expiring = append(expiring, c)
	}
	challengeRows.Close()
	if err := challengeRows.Err(); err != nil {
		log.Printf("daemon: challenge expiry rows child %d: %v", childID, err)
		return
	}
	if len(expiring) == 0 {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	for _, c := range expiring {
		kind := "2d"
		if c.endDate == today {
			kind = "1d"
		}
		notifKey := fmt.Sprintf("%d-%s", c.id, today)
		inserted, err := recordNotifSent(ctx, db, childID, "challenge_expiry_"+kind, notifKey)
		if err != nil {
			log.Printf("daemon: challenge expiry record child %d: %v", childID, err)
			continue
		}
		if !inserted {
			continue
		}

		var title, body string
		if kind == "2d" {
			title = "Challenge Expiring Soon"
			body = "You have 2 days left to complete a challenge — go for it!"
		} else {
			title = "Last Day for a Challenge!"
			body = "Today is your last chance to complete a challenge. Don't miss out!"
		}

		notification := push.Notification{
			Title:   title,
			Body:    body,
			URL:     "/stars",
			Tag:     "challenge-expiry-" + kind,
			Urgency: "normal",
		}
		payload, err := json.Marshal(notification)
		if err != nil {
			log.Printf("daemon: challenge expiry marshal child %d: %v", childID, err)
			continue
		}
		if _, err := push.SendToUser(db, httpClient, childID, payload); err != nil {
			log.Printf("daemon: challenge expiry send child %d: %v", childID, err)
		}
	}
}

// recordNotifSent inserts a dedup record so the same notification is not sent again.
// Returns (true, nil) when the row was newly inserted, (false, nil) when it already
// existed (INSERT OR IGNORE was a no-op), so callers can skip delivery if another
// scheduler instance already claimed the key.
func recordNotifSent(ctx context.Context, db *sql.DB, userID int64, kind, key string) (bool, error) {
	res, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		userID, kind+":"+key, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// userLocation returns the *time.Location for a user based on their
// quiet_hours_timezone preference, falling back to UTC.
func userLocation(prefs map[string]string) *time.Location {
	if tz := prefs["quiet_hours_timezone"]; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}
