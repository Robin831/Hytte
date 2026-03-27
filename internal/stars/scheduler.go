package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
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
// deliver is called with (userID, JSON payload) for each notification to send;
// it is the caller's responsibility to dispatch the push (e.g. via push.SendToUser).
func CheckStreakWarnings(ctx context.Context, db SchedulerDB, deliver func(int64, []byte)) {
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
		maybeWarnStreakAtRisk(ctx, db, deliver, userID, now)
	}
}

func maybeWarnStreakAtRisk(ctx context.Context, db SchedulerDB, deliver func(int64, []byte), userID int64, now time.Time) {
	prefs := schedulerGetPrefs(ctx, db, userID)
	loc := schedulerUserLocation(prefs)
	lastSent := schedulerGetLastSent(ctx, db, userID, "streak")
	fire, key := schedulerShouldFireStreakWarning(loc, lastSent, now)
	if !fire {
		return
	}

	// Streak is at risk when there is an active daily_workout streak and the
	// last activity was yesterday in UTC (i.e. no workout has been recorded
	// today yet, but the streak is still unbroken).
	utcToday := now.UTC().Format("2006-01-02")
	utcYesterday := now.UTC().Add(-24 * time.Hour).Format("2006-01-02")
	var streakCount int
	var lastActivity string
	_ = db.QueryRowContext(ctx,
		`SELECT current_count, last_activity FROM streaks WHERE user_id = ? AND streak_type = 'daily_workout'`,
		userID,
	).Scan(&streakCount, &lastActivity)
	if streakCount == 0 {
		// No active streak.
		return
	}
	if lastActivity == utcToday {
		// Already logged a workout today; streak not at immediate risk.
		return
	}
	if lastActivity != utcYesterday {
		// Streak is either already broken (last activity before yesterday) or
		// in an unexpected state; don't warn.
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

	if deliver != nil {
		deliver(userID, payload)
	}
}

// SendWeeklySummaries queries every parent user and, for each one for whom
// the current local time is Monday 08:xx, sends a push notification
// summarising each child's stars earned and distance run in the previous ISO
// week. Quiet hours are respected and exactly-once delivery is guaranteed via
// the daemon_notification_sent table.
func SendWeeklySummaries(ctx context.Context, db SchedulerDB, deliver func(int64, []byte)) {
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
		maybeSendWeeklySummary(ctx, db, deliver, parentID, now)
	}
}

// schedulerChild holds the fields needed from family_links for weekly summaries.
type schedulerChild struct {
	childID  int64
	nickname string
}

func maybeSendWeeklySummary(ctx context.Context, db SchedulerDB, deliver func(int64, []byte), parentID int64, now time.Time) {
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
		var encNickname string
		if err := childRows.Scan(&c.childID, &encNickname); err != nil {
			log.Printf("stars: weekly summary scan child: %v", err)
			childRows.Close()
			return
		}
		c.nickname = schedulerDecryptOrPlaintext(encNickname)
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

	if deliver != nil {
		deliver(parentID, payload)
	}
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
		log.Printf("scheduler: failed to query user_preferences for user_id=%d: %v", userID, err)
		return map[string]string{}
	}
	defer rows.Close()

	prefs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			log.Printf("scheduler: failed to scan user preference row for user_id=%d: %v", userID, err)
			continue
		}
		prefs[k] = v
	}
	if err := rows.Err(); err != nil {
		log.Printf("scheduler: rows error while reading user_preferences for user_id=%d: %v", userID, err)
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

// schedulerDecryptOrPlaintext decrypts an encrypted field value. If the value
// has the "enc:" prefix but decryption fails, it returns an empty string to
// avoid leaking ciphertext. For legacy plaintext values the value is returned
// as-is.
func schedulerDecryptOrPlaintext(val string) string {
	if val == "" {
		return val
	}
	decrypted, err := encryption.DecryptField(val)
	if err != nil {
		if len(val) >= 4 && val[:4] == "enc:" {
			log.Printf("stars: decrypt field failed for enc:-prefixed value: %v", err)
			return ""
		}
		return val
	}
	return decrypted
}

// CheckChallengeExpiry queries all active, uncompleted challenges for each
// child user and sends a push notification at two milestone windows:
//   - 2 days before the challenge end_date ("2-day warning")
//   - 1 day before the challenge end_date ("1-day warning")
//
// The function fires only during the 10:xx hour in the child's configured
// timezone. Each (challenge, milestone) pair is delivered at most once:
// deduplication is persisted in daemon_notification_sent so concurrent or
// repeated calls within the same run are safe.
func CheckChallengeExpiry(ctx context.Context, db SchedulerDB, deliver func(int64, []byte)) {
	checkChallengeExpiryAt(ctx, db, deliver, time.Now())
}

// checkChallengeExpiryAt is the time-injectable core of CheckChallengeExpiry,
// used directly in tests to control the clock.
func checkChallengeExpiryAt(ctx context.Context, db SchedulerDB, deliver func(int64, []byte), now time.Time) {
	// Drive off challenge_participants so children retain reminders even if a
	// family_link is removed without cleaning up challenge_participants.
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT cp.child_id
		FROM challenge_participants cp
		JOIN family_challenges fc ON fc.id = cp.challenge_id
		WHERE fc.is_active = 1 AND cp.completed_at = ''
	`)
	if err != nil {
		log.Printf("stars: challenge expiry get children: %v", err)
		return
	}

	var childIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("stars: challenge expiry scan: %v", err)
			rows.Close()
			return
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("stars: challenge expiry rows: %v", err)
		return
	}

	for _, childID := range childIDs {
		maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)
	}
}

// challengeExpiryMilestone describes a (challenge ID, milestone name) pair.
type challengeExpiryMilestone struct {
	challengeID int64
	milestone   string // "2d" or "1d"
}

func maybeSendChallengeExpiryReminder(ctx context.Context, db SchedulerDB, deliver func(int64, []byte), childID int64, now time.Time) {
	prefs := schedulerGetPrefs(ctx, db, childID)
	loc := schedulerUserLocation(prefs)
	childNow := now.In(loc)
	if childNow.Hour() != 10 {
		return
	}

	oneDayLater := childNow.AddDate(0, 0, 1).Format("2006-01-02")
	twoDaysLater := childNow.AddDate(0, 0, 2).Format("2006-01-02")

	challengeRows, err := db.QueryContext(ctx, `
		SELECT fc.id, fc.end_date
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id
		WHERE cp.child_id = ? AND fc.is_active = 1 AND cp.completed_at = ''
		  AND (fc.end_date = ? OR fc.end_date = ?)
	`, childID, oneDayLater, twoDaysLater)
	if err != nil {
		log.Printf("stars: challenge expiry query child %d: %v", childID, err)
		return
	}

	var expiring []challengeExpiryMilestone
	for challengeRows.Next() {
		var id int64
		var endDate string
		if err := challengeRows.Scan(&id, &endDate); err != nil {
			log.Printf("stars: challenge expiry scan challenge: %v", err)
			challengeRows.Close()
			return
		}
		milestone := "2d"
		if endDate == oneDayLater {
			milestone = "1d"
		}
		expiring = append(expiring, challengeExpiryMilestone{challengeID: id, milestone: milestone})
	}
	challengeRows.Close()
	if err := challengeRows.Err(); err != nil {
		log.Printf("stars: challenge expiry rows child %d: %v", childID, err)
		return
	}
	if len(expiring) == 0 {
		return
	}

	if quiethours.IsActiveWithPrefs(prefs) {
		return
	}

	for _, m := range expiring {
		// Dedup key encodes challenge ID and milestone so each (challenge, milestone)
		// pair is sent at most once, regardless of how many times the scheduler runs.
		dedupKey := fmt.Sprintf("%d-%s", m.challengeID, m.milestone)
		inserted, err := schedulerMarkSent(ctx, db, childID, "challenge_expiry", dedupKey)
		if err != nil {
			log.Printf("stars: challenge expiry record child %d: %v", childID, err)
			continue
		}
		if !inserted {
			continue
		}

		var title, body string
		if m.milestone == "2d" {
			title = "Challenge Expiring Soon"
			body = "You have 2 days left to complete a challenge — go for it!"
		} else {
			title = "Last Day for a Challenge!"
			body = "Only 1 day left to complete a challenge. Don't miss out!"
		}

		notification := push.Notification{
			Title:   title,
			Body:    body,
			URL:     "/stars",
			Tag:     "challenge-expiry-" + m.milestone,
			Urgency: "normal",
		}
		payload, err := json.Marshal(notification)
		if err != nil {
			log.Printf("stars: challenge expiry marshal child %d: %v", childID, err)
			continue
		}

		if deliver != nil {
			deliver(childID, payload)
		}
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
