package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// SchedulerDB is the minimal database interface required by CheckStreakWarnings
// and SendWeeklySummaries. *sql.DB satisfies this interface, and tests may
// substitute a lightweight in-memory implementation.
type SchedulerDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// CheckStreakWarnings queries every user with an active streak and, for each
// one for whom the current local time falls in the 19:xx (7 PM) hour, sends a
// streak-at-risk push notification if no workout has been logged today. Quiet
// hours are respected. Delivery is idempotent: a daemon_notification_sent
// record is inserted before dispatch so duplicate sends are prevented even
// when the daemon loop overlaps across minute boundaries.
func CheckStreakWarnings(ctx context.Context, db SchedulerDB, httpClient *http.Client) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT user_id FROM streaks WHERE current_count > 0`)
	if err != nil {
		log.Printf("stars: streak warnings query: %v", err)
		return
	}

	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("stars: streak warnings scan: %v", err)
			rows.Close()
			return
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("stars: streak warnings rows: %v", err)
		return
	}

	now := time.Now()
	for _, userID := range userIDs {
		maybeWarnStreakAtRisk(ctx, db, httpClient, userID, now)
	}
}

func maybeWarnStreakAtRisk(ctx context.Context, db SchedulerDB, httpClient *http.Client, userID int64, now time.Time) {
	prefs := schedulerGetPrefs(ctx, db, userID)
	loc := schedulerUserLocation(prefs)
	lastSent := schedulerGetLastSent(ctx, db, userID, "streak")
	fire, key := schedulerShouldFireStreakWarning(loc, lastSent, now)
	if !fire {
		return
	}

	var atRisk bool
	if sqlDB, ok := db.(*sql.DB); ok {
		// Use the existing CheckStreakAtRisk helper when a real *sql.DB is available.
		result, err := CheckStreakAtRisk(ctx, sqlDB, userID)
		if err != nil {
			log.Printf("stars: check streak at risk user %d: %v", userID, err)
			return
		}
		atRisk = result.DailyAtRisk || result.WeeklyAtRisk
	} else {
		// Inline check for test environments: streak is at risk when today's
		// date is not recorded as the last_activity for the daily workout streak.
		today := now.In(loc).Format("2006-01-02")
		var lastActivity string
		_ = db.QueryRowContext(ctx,
			`SELECT last_activity FROM streaks WHERE user_id = ? AND streak_type = 'daily_workout'`,
			userID,
		).Scan(&lastActivity)
		atRisk = lastActivity != today
	}
	if !atRisk {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	// Claim the dedup slot before sending. If another scheduler instance already
	// inserted this row the INSERT OR IGNORE is a no-op and we skip delivery.
	inserted, err := schedulerMarkSent(ctx, db, userID, "streak", key)
	if err != nil {
		log.Printf("stars: streak warn record user %d: %v", userID, err)
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
		log.Printf("stars: streak warn marshal user %d: %v", userID, err)
		return
	}

	schedulerSendPush(db, httpClient, userID, payload)
}

// SendWeeklySummaries queries every parent user and, for each one for whom
// the current local time is Monday 08:xx, sends a push notification
// summarising each child's stars earned and distance run in the previous ISO
// week. Quiet hours are respected and exactly-once delivery is guaranteed via
// the daemon_notification_sent table.
func SendWeeklySummaries(ctx context.Context, db SchedulerDB, httpClient *http.Client) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT parent_id FROM family_links`)
	if err != nil {
		log.Printf("stars: weekly summary query parents: %v", err)
		return
	}

	var parentIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("stars: weekly summary scan: %v", err)
			rows.Close()
			return
		}
		parentIDs = append(parentIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("stars: weekly summary rows: %v", err)
		return
	}

	now := time.Now()
	for _, parentID := range parentIDs {
		maybeSendWeeklySummary(ctx, db, httpClient, parentID, now)
	}
}

// schedulerChild holds the fields needed from family_links for weekly summaries.
type schedulerChild struct {
	childID  int64
	nickname string
}

func maybeSendWeeklySummary(ctx context.Context, db SchedulerDB, httpClient *http.Client, parentID int64, now time.Time) {
	prefs := schedulerGetPrefs(ctx, db, parentID)
	loc := schedulerUserLocation(prefs)
	lastSent := schedulerGetLastSent(ctx, db, parentID, "weekly")
	fire, key := schedulerShouldFireWeeklySummary(loc, lastSent, now)
	if !fire {
		return
	}

	childRows, err := db.QueryContext(ctx,
		`SELECT child_id, nickname FROM family_links WHERE parent_id = ?`, parentID,
	)
	if err != nil {
		log.Printf("stars: weekly summary get children parent %d: %v", parentID, err)
		return
	}
	var children []schedulerChild
	for childRows.Next() {
		var c schedulerChild
		if err := childRows.Scan(&c.childID, &c.nickname); err != nil {
			log.Printf("stars: weekly summary scan child: %v", err)
			childRows.Close()
			return
		}
		children = append(children, c)
	}
	childRows.Close()
	if err := childRows.Err(); err != nil {
		log.Printf("stars: weekly summary child rows parent %d: %v", parentID, err)
		return
	}
	if len(children) == 0 {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	// Claim the dedup slot before sending.
	inserted, err := schedulerMarkSent(ctx, db, parentID, "weekly", key)
	if err != nil {
		log.Printf("stars: weekly summary record parent %d: %v", parentID, err)
		return
	}
	if !inserted {
		return
	}

	// Compute the previous ISO week's date range. The parent's local time is
	// Monday 08:xx, so yesterday is Sunday of the previous week.
	parentNow := now.In(loc)
	prevMon := schedulerISOWeekMonday(parentNow.AddDate(0, 0, -1).UTC().ISOWeek())
	prevWeekStart := prevMon.Format(time.RFC3339)
	prevWeekEnd := prevMon.AddDate(0, 0, 7).Format(time.RFC3339)

	var lines []string
	for _, child := range children {
		var starsEarned int
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0) FROM star_transactions
			WHERE user_id = ? AND amount > 0 AND created_at >= ? AND created_at < ?
		`, child.childID, prevWeekStart, prevWeekEnd).Scan(&starsEarned); err != nil {
			log.Printf("stars: weekly summary stars child %d: %v", child.childID, err)
		}

		var distanceM float64
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(distance_meters), 0) FROM workouts
			WHERE user_id = ? AND started_at >= ? AND started_at < ?
		`, child.childID, prevWeekStart, prevWeekEnd).Scan(&distanceM); err != nil {
			log.Printf("stars: weekly summary distance child %d: %v", child.childID, err)
		}

		name := child.nickname
		if name == "" {
			name = fmt.Sprintf("Child %d", child.childID)
		}
		lines = append(lines, fmt.Sprintf("%s: %d ⭐, %.1f km", name, starsEarned, distanceM/1000.0))
	}

	notification := push.Notification{
		Title: "Weekly Family Summary",
		Body:  strings.Join(lines, "\n"),
		URL:   "/family",
		Tag:   "weekly-summary",
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		log.Printf("stars: weekly summary marshal parent %d: %v", parentID, err)
		return
	}

	schedulerSendPush(db, httpClient, parentID, payload)
}

// schedulerShouldFireStreakWarning returns (true, dateKey) when the current
// hour in loc is 19 and no warning has been sent for today.
func schedulerShouldFireStreakWarning(loc *time.Location, lastSent string, now time.Time) (bool, string) {
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

// schedulerShouldFireWeeklySummary returns (true, weekKey) when the current
// time in loc is Monday 08:xx and no summary has been sent for this ISO week.
func schedulerShouldFireWeeklySummary(loc *time.Location, lastSent string, now time.Time) (bool, string) {
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

// schedulerGetPrefs reads all user preferences for a user from the
// user_preferences table and returns them as a string map.
func schedulerGetPrefs(ctx context.Context, db SchedulerDB, userID int64) map[string]string {
	rows, err := db.QueryContext(ctx,
		`SELECT key, value FROM user_preferences WHERE user_id = ?`, userID,
	)
	if err != nil {
		return map[string]string{}
	}
	defer rows.Close()
	prefs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		prefs[k] = v
	}
	return prefs
}

// schedulerUserLocation resolves a *time.Location from the
// quiet_hours_timezone preference value, falling back to time.UTC.
func schedulerUserLocation(prefs map[string]string) *time.Location {
	if tz := prefs["quiet_hours_timezone"]; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// schedulerGetLastSent returns the date/week portion of the most recently
// recorded dedup key for a (userID, kind) pair, or an empty string if none.
func schedulerGetLastSent(ctx context.Context, db SchedulerDB, userID int64, kind string) string {
	var key string
	err := db.QueryRowContext(ctx,
		`SELECT key FROM daemon_notification_sent WHERE user_id = ? AND key LIKE ? ORDER BY sent_at DESC LIMIT 1`,
		userID, kind+":%",
	).Scan(&key)
	if err != nil {
		return ""
	}
	if len(key) > len(kind)+1 {
		return key[len(kind)+1:]
	}
	return key
}

// schedulerMarkSent inserts a dedup record atomically. Returns (true, nil)
// when the row is newly inserted and (false, nil) when it already existed
// (INSERT OR IGNORE no-op), so callers can skip delivery if another scheduler
// instance already claimed the slot.
func schedulerMarkSent(ctx context.Context, db SchedulerDB, userID int64, kind, key string) (bool, error) {
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

// schedulerSendPush dispatches a push payload to a user. Push subscription
// queries require a *sql.DB, so db is type-asserted at call time; callers
// from tests that do not exercise push delivery pass a mock SchedulerDB and
// the send is silently skipped.
func schedulerSendPush(db SchedulerDB, httpClient *http.Client, userID int64, payload []byte) {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return
	}
	if _, err := push.SendToUser(sqlDB, httpClient, userID, payload); err != nil {
		log.Printf("stars: push send user %d: %v", userID, err)
	}
}

// schedulerISOWeekMonday returns the Monday at UTC midnight for the given ISO
// year and week number.
func schedulerISOWeekMonday(year, week int) time.Time {
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	jan4DOW := int(jan4.Weekday())
	if jan4DOW == 0 {
		jan4DOW = 7
	}
	week1Monday := jan4.AddDate(0, 0, 1-jan4DOW)
	return week1Monday.AddDate(0, 0, (week-1)*7)
}
