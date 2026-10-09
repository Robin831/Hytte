package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// Preference keys. Both default on: tracking a race is already an opt-in,
// so only an explicit "false" turns a kind of push off.
const (
	PrefNotifyDeadlines = "races_notify_deadlines"
	PrefNotifyChanges   = "races_notify_changes"
	// PrefLanguage is the UI language the web app keeps in sync, so pushes
	// are written in the language the user reads Hytte in.
	PrefLanguage = "ui_language"
)

// NotifyInterval is how often the notify loop runs. Reminder slots are
// planned to the minute; a 5-minute tick keeps them on time without load.
const NotifyInterval = 5 * time.Minute

// changeSettle holds a change back briefly so a burst of edits to one race
// (an admin fixing several fields) lands as one push.
const changeSettle = 2 * time.Minute

// changeWindow bounds how long an undelivered change stays eligible: long
// enough to outlast any quiet-hours hold or a failed send, short enough that
// a returning user isn't flooded with stale news.
const changeWindow = 3 * 24 * time.Hour

// quietShiftStep and quietShiftMax control how a reminder that would land
// in quiet hours is moved earlier: back in steps until outside the window.
const (
	quietShiftStep = 15 * time.Minute
	quietShiftMax  = 24 * time.Hour
)

const defaultZone = "Europe/Oslo"

// Notifier sends race deadline reminders and change notifications. The
// function fields are injectable so tests run without VAPID keys or real
// push endpoints.
type Notifier struct {
	DB              *sql.DB
	Send            func(db *sql.DB, userID int64, payload []byte) error
	VAPIDConfigured func(db *sql.DB) (bool, error)
	LoadPrefs       func(db *sql.DB, userID int64) (map[string]string, error)
	Quiet           func(prefs map[string]string, at time.Time) bool
}

// NewNotifier returns a Notifier wired to real push and quiet hours.
func NewNotifier(db *sql.DB) *Notifier {
	return &Notifier{
		DB:              db,
		Send:            defaultSend,
		VAPIDConfigured: vapidKeysExist,
		LoadPrefs:       auth.GetPreferences,
		Quiet:           quiethours.IsActiveWithPrefsAt,
	}
}

// RunNotifyLoop runs a notify pass now and then every interval until ctx ends.
func RunNotifyLoop(ctx context.Context, db *sql.DB, interval time.Duration) {
	n := NewNotifier(db)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := n.Run(ctx, time.Now()); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("races: notify pass: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Run does one pass over every eligible user. Per-user failures are logged
// and skipped; an error is returned only when shared inputs can't be read.
func (n *Notifier) Run(ctx context.Context, now time.Time) error {
	if n.VAPIDConfigured != nil {
		ok, err := n.VAPIDConfigured(n.DB)
		if err != nil {
			return fmt.Errorf("check vapid keys: %w", err)
		}
		if !ok {
			return nil
		}
	}
	users, err := notifyCandidates(ctx, n.DB)
	if err != nil {
		return err
	}
	for _, userID := range users {
		if err := ctx.Err(); err != nil {
			return err
		}
		prefs, err := n.LoadPrefs(n.DB, userID)
		if err != nil {
			log.Printf("races: notify user %d: load prefs: %v", userID, err)
			continue
		}
		u := recipient{id: userID, prefs: prefs, lang: languageOf(prefs), loc: zoneOf(prefs)}
		if prefs[PrefNotifyDeadlines] != "false" {
			n.remindUser(ctx, u, now)
		}
		if prefs[PrefNotifyChanges] != "false" {
			n.announceChanges(ctx, u, now)
		}
	}
	return nil
}

type recipient struct {
	id    int64
	prefs map[string]string
	lang  string
	loc   *time.Location
}

func languageOf(prefs map[string]string) string {
	if l := prefs[PrefLanguage]; validLanguage[l] {
		return l
	}
	return "en"
}

func zoneOf(prefs map[string]string) *time.Location {
	if tz := prefs["quiet_hours_timezone"]; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(defaultZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (n *Notifier) quiet(u recipient, at time.Time) bool {
	return n.Quiet != nil && n.Quiet(u.prefs, at)
}

// notifyCandidates returns users who have the races feature (admins always
// do), at least one push subscription and at least one race they still care
// about (not completed or skipped).
func notifyCandidates(ctx context.Context, db *sql.DB) ([]int64, error) {
	featureCond := `EXISTS (SELECT 1 FROM user_features f
		WHERE f.user_id = u.id AND f.feature_key = 'races' AND f.enabled = 1)`
	if auth.FeatureDefaults["races"] {
		featureCond = `NOT EXISTS (SELECT 1 FROM user_features f
			WHERE f.user_id = u.id AND f.feature_key = 'races' AND f.enabled = 0)`
	}
	rows, err := db.QueryContext(ctx, `
		SELECT u.id FROM users u
		WHERE (u.is_admin = 1 OR `+featureCond+`)
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = u.id)
		  AND EXISTS (SELECT 1 FROM race_watch w WHERE w.user_id = u.id AND w.state NOT IN ('completed', 'skipped'))
		ORDER BY u.id`)
	if err != nil {
		return nil, fmt.Errorf("query notify candidates: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// remindable decides which deadline kinds still matter for a watch state:
// once registered, entry and lottery dates are noise; once in a lottery, only
// the results and the payment that follows.
func remindable(state, kind string) bool {
	switch state {
	case "completed", "skipped":
		return false
	case "registered", "got_place":
		return kind == "payment_due" || kind == "other"
	case "lottery_entered":
		return kind == "lottery_results" || kind == "payment_due" || kind == "other"
	default:
		return true
	}
}

type reminderSlot struct {
	name   string
	fireAt time.Time
}

// dueMoment is when a deadline passes: its exact instant when known,
// otherwise the end of its day in the user's zone.
func dueMoment(d Deadline, loc *time.Location) (time.Time, bool) {
	if d.DueAt != nil {
		return *d.DueAt, true
	}
	day, err := time.ParseInLocation("2006-01-02", d.DueDate, loc)
	if err != nil {
		return time.Time{}, false
	}
	return day.Add(24*time.Hour - time.Second), true
}

func atClock(day time.Time, hour int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, day.Location())
}

// planSlots lists a deadline's reminder moments in order, each moved out of
// the user's quiet hours (earlier, so a reminder never lands after the
// deadline it is about):
//   - exact time: 7 days, 24 hours and 3 hours before;
//   - all-day: 09:00 a week and a day before, 08:00 on the day;
//   - fuzzy window (early/mid/late/month): one heads-up a week before it starts.
func (n *Notifier) planSlots(u recipient, d Deadline, due time.Time) []reminderSlot {
	var slots []reminderSlot
	switch {
	case d.DueAt != nil:
		slots = []reminderSlot{
			{"week", due.Add(-7 * 24 * time.Hour)},
			{"day", due.Add(-24 * time.Hour)},
			{"final", due.Add(-3 * time.Hour)},
		}
	case d.DatePrecision == "day" || d.DatePrecision == "approx":
		day, _ := time.ParseInLocation("2006-01-02", d.DueDate, u.loc)
		slots = []reminderSlot{
			{"week", atClock(day.AddDate(0, 0, -7), 9)},
			{"day", atClock(day.AddDate(0, 0, -1), 9)},
			{"final", atClock(day, 8)},
		}
	default:
		day, _ := time.ParseInLocation("2006-01-02", d.DueDate, u.loc)
		slots = []reminderSlot{{"week", atClock(day.AddDate(0, 0, -7), 9)}}
	}
	for i := range slots {
		slots[i].fireAt = n.shiftOutOfQuiet(u, slots[i].fireAt)
	}
	return slots
}

func (n *Notifier) shiftOutOfQuiet(u recipient, t time.Time) time.Time {
	for back := time.Duration(0); back <= quietShiftMax; back += quietShiftStep {
		if candidate := t.Add(-back); !n.quiet(u, candidate) {
			return candidate
		}
	}
	return t
}

type watchedDeadline struct {
	Deadline
	eventName string
	state     string
}

func listWatchedDeadlines(ctx context.Context, db *sql.DB, userID int64) ([]watchedDeadline, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT d.id, d.event_id, d.kind, d.due_date, d.date_precision, d.due_time, d.tz, d.expected, d.texts,
		       e.name, w.state
		FROM race_deadlines d
		JOIN race_watch w ON w.event_id = d.event_id AND w.user_id = ?
		JOIN race_events e ON e.id = d.event_id
		WHERE w.state NOT IN ('completed', 'skipped')
		ORDER BY d.due_date, d.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list watched deadlines: %w", err)
	}
	defer rows.Close()
	var out []watchedDeadline
	for rows.Next() {
		var wd watchedDeadline
		var texts string
		if err := rows.Scan(&wd.ID, &wd.EventID, &wd.Kind, &wd.DueDate, &wd.DatePrecision, &wd.DueTime, &wd.TZ,
			&wd.Expected, &texts, &wd.eventName, &wd.state); err != nil {
			return nil, err
		}
		wd.Texts = map[string]DeadlineText{}
		_ = json.Unmarshal([]byte(texts), &wd.Texts)
		wd.DueAt = dueAt(wd.DueDate, wd.DueTime, wd.TZ)
		out = append(out, wd)
	}
	return out, rows.Err()
}

// remindUser sends at most one reminder per deadline per pass: the latest
// slot whose time has come, if it hasn't been sent for this due date yet.
// Earlier missed slots are not replayed — a watch added two days before a
// deadline gets one "in 2 days" push, not a late "in 7 days" one as well.
func (n *Notifier) remindUser(ctx context.Context, u recipient, now time.Time) {
	deadlines, err := listWatchedDeadlines(ctx, n.DB, u.id)
	if err != nil {
		log.Printf("races: remind user %d: %v", u.id, err)
		return
	}
	for _, wd := range deadlines {
		if !remindable(wd.state, wd.Kind) {
			continue
		}
		due, ok := dueMoment(wd.Deadline, u.loc)
		if !ok || !now.Before(due) {
			continue
		}
		var slot *reminderSlot
		for _, s := range n.planSlots(u, wd.Deadline, due) {
			if !s.fireAt.After(now) {
				s := s
				slot = &s
			}
		}
		if slot == nil {
			continue
		}
		// A late slot (watch added at night) waits for quiet hours to end —
		// unless the deadline itself would pass first.
		if n.quiet(u, now) && n.shiftOutOfQuiet(u, due).After(now) {
			continue
		}
		dueKey := wd.DueDate + " " + wd.DueTime + " " + wd.TZ
		var sent int
		if err := n.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_reminders
			WHERE user_id = ? AND deadline_id = ? AND slot = ? AND due_key = ?`,
			u.id, wd.ID, slot.name, dueKey).Scan(&sent); err != nil {
			log.Printf("races: remind user %d: %v", u.id, err)
			return
		}
		if sent > 0 {
			continue
		}
		payload, err := json.Marshal(reminderNotification(u, wd, now))
		if err != nil {
			log.Printf("races: remind user %d: marshal: %v", u.id, err)
			continue
		}
		if err := n.Send(n.DB, u.id, payload); err != nil {
			log.Printf("races: remind user %d deadline %d: send: %v", u.id, wd.ID, err)
			continue
		}
		if _, err := n.DB.ExecContext(ctx, `INSERT OR IGNORE INTO race_reminders
			(user_id, deadline_id, slot, due_key, sent_at) VALUES (?, ?, ?, ?, ?)`,
			u.id, wd.ID, slot.name, dueKey, now.UTC().Format(time.RFC3339)); err != nil {
			log.Printf("races: remind user %d: record: %v", u.id, err)
		}
	}
}

// reminderNotification renders e.g. "Berlin Marathon" /
// "Lottery closes tomorrow (6 Nov). Sources disagree: 6 or 12 November."
func reminderNotification(u recipient, wd watchedDeadline, now time.Time) push.Notification {
	s := stringsFor(u.lang)
	localNow := now.In(u.loc)
	var when string
	switch {
	case wd.DueAt != nil:
		when = s.formatInstant(*wd.DueAt, u.loc, now)
	case wd.DatePrecision == "day" || wd.DatePrecision == "approx":
		day, _ := time.ParseInLocation("2006-01-02", wd.DueDate, u.loc)
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, u.loc)
		days := int(day.Sub(today).Hours()/24 + 0.5)
		when = s.relativeDays(days, day, localNow)
		if wd.DatePrecision == "approx" || wd.Expected {
			when = fmt.Sprintf(s.around, when)
		}
	default:
		day, _ := time.ParseInLocation("2006-01-02", wd.DueDate, u.loc)
		when = fmt.Sprintf(s.expected, s.formatFuzzy(day, wd.DatePrecision, localNow))
	}
	body := s.kinds[wd.Kind] + " " + when + "."
	if t := pickDeadlineText(wd.Texts, u.lang); t != "" {
		body += " " + t
	}
	return push.Notification{
		Title: wd.eventName,
		Body:  truncate(body, 300),
		URL:   "/races/" + strconv.FormatInt(wd.EventID, 10),
		Tag:   "race-deadline-" + strconv.FormatInt(wd.ID, 10),
	}
}

func pickDeadlineText(texts map[string]DeadlineText, lang string) string {
	for _, l := range []string{lang, "en", "nb", "th"} {
		if t := texts[l].What; t != "" {
			return t
		}
	}
	return ""
}

type pendingChange struct {
	Change
	eventName     string
	raceDate      string
	datePrecision string
}

// announceChanges pushes one summary per race of notable changes the user
// hasn't been told about: status, race date, entry type and deadlines. Text
// edits are left to the page. Changes made by the user themselves, or made
// before they started watching, are not announced.
func (n *Notifier) announceChanges(ctx context.Context, u recipient, now time.Time) {
	if n.quiet(u, now) {
		return
	}
	rows, err := n.DB.QueryContext(ctx, `
		SELECT c.id, c.event_id, c.field, c.old_value, c.new_value, c.source, c.created_at,
		       e.name, e.race_date, e.date_precision
		FROM race_changes c
		JOIN race_watch w ON w.event_id = c.event_id AND w.user_id = ?
		JOIN race_events e ON e.id = c.event_id
		WHERE w.state NOT IN ('completed', 'skipped')
		  AND (c.field IN ('status', 'race_date', 'date_precision', 'entry_type') OR c.field LIKE 'deadline.%')
		  AND c.created_at >= w.created_at
		  AND c.created_at >= ? AND c.created_at <= ?
		  AND (c.user_id IS NULL OR c.user_id != ?)
		  AND NOT EXISTS (SELECT 1 FROM race_change_deliveries d WHERE d.user_id = ? AND d.change_id = c.id)
		ORDER BY c.event_id, c.id`,
		u.id, now.Add(-changeWindow).UTC().Format(time.RFC3339), now.Add(-changeSettle).UTC().Format(time.RFC3339),
		u.id, u.id)
	if err != nil {
		log.Printf("races: changes for user %d: %v", u.id, err)
		return
	}
	var pending []pendingChange
	for rows.Next() {
		var pc pendingChange
		if err := rows.Scan(&pc.ID, &pc.EventID, &pc.Field, &pc.OldValue, &pc.NewValue, &pc.Source, &pc.CreatedAt,
			&pc.eventName, &pc.raceDate, &pc.datePrecision); err != nil {
			rows.Close()
			log.Printf("races: changes for user %d: %v", u.id, err)
			return
		}
		pending = append(pending, pc)
	}
	rows.Close()

	for start := 0; start < len(pending); {
		end := start
		for end < len(pending) && pending[end].EventID == pending[start].EventID {
			end++
		}
		group := pending[start:end]
		start = end

		payload, err := json.Marshal(changeNotification(u, group, now))
		if err != nil {
			log.Printf("races: changes for user %d: marshal: %v", u.id, err)
			continue
		}
		if err := n.Send(n.DB, u.id, payload); err != nil {
			log.Printf("races: changes for user %d race %d: send: %v", u.id, group[0].EventID, err)
			continue
		}
		ts := now.UTC().Format(time.RFC3339)
		for _, c := range group {
			if _, err := n.DB.ExecContext(ctx, `INSERT OR IGNORE INTO race_change_deliveries (user_id, change_id, delivered_at)
				VALUES (?, ?, ?)`, u.id, c.ID, ts); err != nil {
				log.Printf("races: changes for user %d: record: %v", u.id, err)
			}
		}
	}
}

// parsedDeadline is a change-log deadline description
// ("2027-04-15 14:00 America/Chicago (expected)") taken apart again.
type parsedDeadline struct {
	date, clock, tz string
	expected        bool
}

func parseDeadlineDesc(s string) (parsedDeadline, bool) {
	var p parsedDeadline
	if strings.HasSuffix(s, " (expected)") {
		p.expected = true
		s = strings.TrimSuffix(s, " (expected)")
	}
	parts := strings.Fields(s)
	if len(parts) == 0 || !validDate(parts[0]) {
		return p, false
	}
	p.date = parts[0]
	if len(parts) == 3 {
		p.clock, p.tz = parts[1], parts[2]
	}
	return p, true
}

func (s pushStrings) describeDeadlineValue(v string, u recipient, now time.Time) string {
	p, ok := parseDeadlineDesc(v)
	if !ok {
		return v
	}
	if at := dueAt(p.date, p.clock, p.tz); at != nil {
		return s.formatInstant(*at, u.loc, now)
	}
	day, _ := time.Parse("2006-01-02", p.date)
	text := s.formatDay(day, now.In(u.loc))
	if p.expected {
		text = fmt.Sprintf(s.around, text)
	}
	return text
}

// changeNotification renders one race's notable changes as a single push:
// "Status: Later or unclear → Open now · Lottery closes: 6 Nov (new)".
func changeNotification(u recipient, group []pendingChange, now time.Time) push.Notification {
	s := stringsFor(u.lang)
	localNow := now.In(u.loc)
	var lines []string
	dateDone := false
	for _, c := range group {
		switch {
		case c.Field == "status":
			lines = append(lines, fmt.Sprintf("%s: %s → %s", s.status, s.statuses[c.OldValue], s.statuses[c.NewValue]))
		case c.Field == "entry_type":
			lines = append(lines, fmt.Sprintf("%s: %s → %s", s.entry, s.entryTypes[c.OldValue], s.entryTypes[c.NewValue]))
		case c.Field == "race_date" || c.Field == "date_precision":
			if dateDone {
				continue
			}
			dateDone = true
			// Always describe the race date as it stands now, with its precision.
			day, err := time.Parse("2006-01-02", group[0].raceDate)
			if err != nil {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s: %s", s.raceDate, s.formatFuzzy(day, group[0].datePrecision, localNow)))
		case strings.HasPrefix(c.Field, "deadline."):
			kind := s.kinds[strings.TrimPrefix(c.Field, "deadline.")]
			switch {
			case c.OldValue == "":
				lines = append(lines, fmt.Sprintf("%s: %s (%s)", kind, s.describeDeadlineValue(c.NewValue, u, now), s.isNew))
			case c.NewValue == "":
				lines = append(lines, fmt.Sprintf("%s: %s (%s)", kind, s.describeDeadlineValue(c.OldValue, u, now), s.removed))
			default:
				lines = append(lines, fmt.Sprintf("%s: %s → %s", kind,
					s.describeDeadlineValue(c.OldValue, u, now), s.describeDeadlineValue(c.NewValue, u, now)))
			}
		}
	}
	return push.Notification{
		Title: group[0].eventName,
		Body:  truncate(strings.Join(lines, " · "), 300),
		URL:   "/races/" + strconv.FormatInt(group[0].EventID, 10),
		Tag:   "race-changes-" + strconv.FormatInt(group[0].EventID, 10),
	}
}

// defaultSend pushes via push.SendToUser and succeeds only when at least one
// subscription accepted it, so a reminder isn't recorded as sent when every
// endpoint failed.
func defaultSend(db *sql.DB, userID int64, payload []byte) error {
	results, err := push.SendToUser(db, push.DefaultHTTPClient, userID, payload)
	if err != nil {
		return err
	}
	var lastErr error
	for _, r := range results {
		if r.Err == nil && r.StatusCode >= 200 && r.StatusCode < 300 {
			return nil
		}
		if r.Err != nil {
			lastErr = redactPushErr(r.Err)
		} else {
			lastErr = fmt.Errorf("push endpoint returned %d", r.StatusCode)
		}
	}
	if lastErr == nil {
		return errors.New("no push subscriptions")
	}
	return fmt.Errorf("no subscription accepted the push (%d attempted): %w", len(results), lastErr)
}

// redactPushErr strips the subscription endpoint (sensitive) from transport errors.
func redactPushErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("send request: %s: %w", ue.Op, ue.Err)
	}
	return err
}

func vapidKeysExist(db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM vapid_keys WHERE id = 1").Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
