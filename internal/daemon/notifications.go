package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
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

// Run starts the periodic notification loop. It ticks every minute and fires
// streak warnings at 19:xx in the user's local timezone and weekly summaries
// on Monday at 08:xx in the parent's local timezone. It blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context, db *sql.DB, httpClient *http.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndSendStreakWarnings(ctx, db, httpClient)
			s.checkAndSendWeeklySummary(ctx, db, httpClient)
		}
	}
}

// checkAndSendStreakWarnings sends streak-at-risk notifications to all users
// for whom it is currently 7 PM in their configured timezone and whose streak
// is at risk of breaking. Quiet hours are respected.
func (s *Scheduler) checkAndSendStreakWarnings(ctx context.Context, db *sql.DB, httpClient *http.Client) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT user_id FROM streaks WHERE current_count > 0`)
	if err != nil {
		log.Printf("daemon: streak warnings query: %v", err)
		return
	}

	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("daemon: streak warnings scan: %v", err)
			rows.Close()
			return
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("daemon: streak warnings rows: %v", err)
		return
	}

	now := time.Now()
	for _, userID := range userIDs {
		s.maybeWarnStreakAtRisk(ctx, db, httpClient, userID, now)
	}
}

func (s *Scheduler) maybeWarnStreakAtRisk(ctx context.Context, db *sql.DB, httpClient *http.Client, userID int64, now time.Time) {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("daemon: streak warn prefs user %d: %v", userID, err)
		return
	}

	loc := userLocation(prefs)
	lastSent := getLastSent(ctx, db, userID, "streak")
	fire, key := shouldFireStreakWarning(loc, lastSent, now)
	if !fire {
		return
	}

	atRisk, err := stars.CheckStreakAtRisk(ctx, db, userID)
	if err != nil {
		log.Printf("daemon: check streak at risk user %d: %v", userID, err)
		return
	}
	if !atRisk.DailyAtRisk && !atRisk.WeeklyAtRisk {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	// Mark as sent before attempting delivery. If the row already existed another
	// scheduler instance already claimed this key — skip to avoid duplicate sends.
	inserted, err := recordNotifSent(ctx, db, userID, "streak", key)
	if err != nil {
		log.Printf("daemon: streak warn record user %d: %v", userID, err)
		return
	}
	if !inserted {
		return
	}

	notification := push.Notification{
		Title:   "Streak Alert",
		Body:    "⭐ streak will break — log a workout to keep it going!",
		URL:     "/stars",
		Tag:     "streak-warning",
		Urgency: "normal",
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		log.Printf("daemon: streak warn marshal user %d: %v", userID, err)
		return
	}

	if _, err := push.SendToUser(db, httpClient, userID, payload); err != nil {
		log.Printf("daemon: streak warn send user %d: %v", userID, err)
	}
}

// checkAndSendWeeklySummary sends a weekly summary to parent users on Monday
// at 8 AM in their configured timezone, listing each child's stars earned and
// distance run in the previous ISO week.
func (s *Scheduler) checkAndSendWeeklySummary(ctx context.Context, db *sql.DB, httpClient *http.Client) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT parent_id FROM family_links`)
	if err != nil {
		log.Printf("daemon: weekly summary query parents: %v", err)
		return
	}

	var parentIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("daemon: weekly summary scan: %v", err)
			rows.Close()
			return
		}
		parentIDs = append(parentIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("daemon: weekly summary rows: %v", err)
		return
	}

	now := time.Now()
	for _, parentID := range parentIDs {
		s.maybeSendWeeklySummary(ctx, db, httpClient, parentID, now)
	}
}

func (s *Scheduler) maybeSendWeeklySummary(ctx context.Context, db *sql.DB, httpClient *http.Client, parentID int64, now time.Time) {
	prefs, err := auth.GetPreferences(db, parentID)
	if err != nil {
		log.Printf("daemon: weekly summary prefs parent %d: %v", parentID, err)
		return
	}

	loc := userLocation(prefs)
	lastSent := getLastSent(ctx, db, parentID, "weekly")
	fire, key := shouldFireWeeklySummary(loc, lastSent, now)
	if !fire {
		return
	}

	children, err := family.GetChildren(db, parentID)
	if err != nil {
		log.Printf("daemon: weekly summary get children parent %d: %v", parentID, err)
		return
	}
	if len(children) == 0 {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	// Mark as sent before delivery. If the row already existed another scheduler
	// instance already claimed this key — skip to avoid duplicate sends.
	inserted, err := recordNotifSent(ctx, db, parentID, "weekly", key)
	if err != nil {
		log.Printf("daemon: weekly summary record parent %d: %v", parentID, err)
		return
	}
	if !inserted {
		return
	}

	// Compute the previous ISO week's date range.
	// now is in the parent's timezone on Monday 08:xx; yesterday is Sunday of last week.
	parentNow := now.In(loc)
	prevWeekAny := parentNow.AddDate(0, 0, -1)
	prevYear, prevWeek := prevWeekAny.UTC().ISOWeek()
	prevMon := isoWeekMonday(prevYear, prevWeek)
	prevWeekStart := prevMon.Format(time.RFC3339)
	prevWeekEnd := prevMon.AddDate(0, 0, 7).Format(time.RFC3339)

	var lines []string
	for _, child := range children {
		var starsEarned int
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0) FROM star_transactions
			WHERE user_id = ? AND amount > 0 AND created_at >= ? AND created_at < ?
		`, child.ChildID, prevWeekStart, prevWeekEnd).Scan(&starsEarned); err != nil {
			log.Printf("daemon: weekly summary stars child %d: %v", child.ChildID, err)
		}

		var distanceM float64
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(distance_meters), 0) FROM workouts
			WHERE user_id = ? AND started_at >= ? AND started_at < ?
		`, child.ChildID, prevWeekStart, prevWeekEnd).Scan(&distanceM); err != nil {
			log.Printf("daemon: weekly summary distance child %d: %v", child.ChildID, err)
		}

		name := child.Nickname
		if name == "" {
			name = fmt.Sprintf("Child %d", child.ChildID)
		}
		lines = append(lines, fmt.Sprintf("This week: %s earned %d ⭐, ran %.1f km", name, starsEarned, distanceM/1000.0))
	}

	notification := push.Notification{
		Title: "Weekly Family Summary",
		Body:  strings.Join(lines, "\n"),
		URL:   "/family",
		Tag:   "weekly-summary",
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		log.Printf("daemon: weekly summary marshal parent %d: %v", parentID, err)
		return
	}

	if _, err := push.SendToUser(db, httpClient, parentID, payload); err != nil {
		log.Printf("daemon: weekly summary send parent %d: %v", parentID, err)
	}
}

// shouldFireStreakWarning reports whether a streak warning should be fired.
// Returns (true, dateKey) when it is currently the 19:xx hour in loc and
// no warning has been sent today (lastSent is "YYYY-MM-DD" in loc).
func shouldFireStreakWarning(loc *time.Location, lastSent string, now time.Time) (bool, string) {
	userNow := now.In(loc)
	if userNow.Hour() != 19 {
		return false, ""
	}
	key := userNow.Format("2006-01-02")
	if lastSent == key {
		return false, ""
	}
	return true, key
}

// shouldFireWeeklySummary reports whether a weekly summary should be fired.
// Returns (true, weekKey) when it is currently Monday 08:xx in loc and no
// summary has been sent for this ISO week (lastSent is "YYYY-WXX").
func shouldFireWeeklySummary(loc *time.Location, lastSent string, now time.Time) (bool, string) {
	userNow := now.In(loc)
	if userNow.Weekday() != time.Monday || userNow.Hour() != 8 {
		return false, ""
	}
	y, w := userNow.ISOWeek()
	key := fmt.Sprintf("%d-W%02d", y, w)
	if lastSent == key {
		return false, ""
	}
	return true, key
}

// getLastSent retrieves the most recently recorded key for a given (userID, kind) pair
// from the DB. Returns an empty string if none is found.
func getLastSent(ctx context.Context, db *sql.DB, userID int64, kind string) string {
	var key string
	err := db.QueryRowContext(ctx,
		`SELECT key FROM daemon_notification_sent WHERE user_id = ? AND key LIKE ? ORDER BY sent_at DESC LIMIT 1`,
		userID, kind+":%",
	).Scan(&key)
	if err != nil {
		return ""
	}
	// Strip the "kind:" prefix to return just the date/week key.
	if len(key) > len(kind)+1 {
		return key[len(kind)+1:]
	}
	return key
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

// isoWeekMonday returns the Monday (UTC midnight) of the given ISO year/week.
func isoWeekMonday(year, week int) time.Time {
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	jan4DOW := int(jan4.Weekday())
	if jan4DOW == 0 {
		jan4DOW = 7
	}
	week1Monday := jan4.AddDate(0, 0, 1-jan4DOW)
	return week1Monday.AddDate(0, 0, (week-1)*7)
}
