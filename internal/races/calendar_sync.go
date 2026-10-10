package races

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/calendar"
)

// Google Calendar sync: race days and the deadlines that matter for each
// tracked race go into the user's calendar. Opt-in (it writes to their own
// calendar); every event Hytte creates is tracked in race_calendar_events so
// a sync can update or remove exactly its own events and nothing else.

const (
	PrefCalendarSync = "races_calendar_sync" // "true" to sync
	PrefCalendarID   = "races_calendar_id"   // default "primary"

	CalendarSyncInterval = 30 * time.Minute
	calendarSource       = "hytte-races"
	timedEventLength     = 30 * time.Minute
)

// CalendarWriter is the part of calendar.Client the sync needs.
type CalendarWriter interface {
	InsertEvent(ctx context.Context, userID int64, calendarID string, ev calendar.WriteEvent) (string, error)
	UpdateEvent(ctx context.Context, userID int64, calendarID, eventID string, ev calendar.WriteEvent) error
	DeleteEvent(ctx context.Context, userID int64, calendarID, eventID string) error
}

// CalendarSyncResult counts what one sync did.
type CalendarSyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Kept    int `json:"kept"`
}

// appBaseURL is the site origin, taken from the OAuth redirect URL (the one
// public address the server is configured with).
func appBaseURL() string {
	u, err := url.Parse(auth.Config().RedirectURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

type calendarItem struct {
	key string
	ev  calendar.WriteEvent
}

func (it calendarItem) hash(calendarID string) string {
	b, _ := json.Marshal(struct {
		C  string
		Ev calendar.WriteEvent
	}{calendarID, it.ev})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// raceDayStates are the commitments worth a race-day entry in the calendar.
var raceDayStates = set("registered", "got_place")

// desiredCalendarItems lists the events the user's calendar should hold.
func desiredCalendarItems(ctx context.Context, db *sql.DB, u recipient, base string, now time.Time) ([]calendarItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.name, e.race_date, e.date_precision, e.distance_m, e.url, e.texts, w.state
		FROM race_watch w JOIN race_events e ON e.id = w.event_id
		WHERE w.user_id = ? AND w.state NOT IN ('completed', 'skipped', 'not_selected')`, u.id)
	if err != nil {
		return nil, fmt.Errorf("calendar items: %w", err)
	}
	type watched struct {
		id                         int64
		name, date, precision, url string
		distance                   int
		texts                      map[string]EventText
		state                      string
	}
	var list []watched
	for rows.Next() {
		var w watched
		var texts string
		if err := rows.Scan(&w.id, &w.name, &w.date, &w.precision, &w.distance, &w.url, &texts, &w.state); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(texts), &w.texts)
		list = append(list, w)
	}
	rows.Close()

	s := stringsFor(u.lang)
	today := now.In(u.loc).Format("2006-01-02")
	link := func(id int64) string {
		if base == "" {
			return ""
		}
		return base + "/races/" + strconv.FormatInt(id, 10)
	}
	var items []calendarItem
	for _, w := range list {
		place := ""
		for _, l := range []string{u.lang, "en", "nb", "th"} {
			if p := w.texts[l].Place; p != "" {
				place = p
				break
			}
		}
		if raceDayStates[w.state] && (w.precision == "day" || w.precision == "approx") && w.date >= today {
			desc := strings.TrimSpace(fmt.Sprintf("%.1f km\n%s\n%s", float64(w.distance)/1000, w.url, link(w.id)))
			items = append(items, calendarItem{key: "race:" + strconv.FormatInt(w.id, 10), ev: calendar.WriteEvent{
				Summary: "🏃 " + w.name, Location: place, Description: desc, AllDayDate: w.date, Source: calendarSource,
			}})
		}

		event, err := GetEvent(ctx, db, w.id)
		if err != nil {
			continue
		}
		for _, d := range event.Deadlines {
			if !remindable(w.state, d.Kind) {
				continue
			}
			what := pickDeadlineText(d.Texts, u.lang)
			desc := strings.TrimSpace(what + "\n\n" + link(w.id))
			ev := calendar.WriteEvent{Summary: s.kinds[d.Kind] + ": " + w.name, Description: desc, Source: calendarSource}
			switch {
			case d.DueAt != nil:
				if !d.DueAt.After(now) {
					continue
				}
				ev.Start, ev.End = *d.DueAt, d.DueAt.Add(timedEventLength)
			case d.DatePrecision == "day" || d.DatePrecision == "approx":
				if d.DueDate < today {
					continue
				}
				ev.AllDayDate = d.DueDate
				if d.DatePrecision == "approx" || d.Expected {
					ev.Summary += " (~)"
				}
			default:
				continue // a month-wide window isn't a calendar entry
			}
			items = append(items, calendarItem{key: "deadline:" + strconv.FormatInt(d.ID, 10), ev: ev})
		}
	}
	return items, nil
}

type syncedEvent struct {
	key, calendarID, googleID, hash string
}

// SyncCalendar brings the user's calendar in line with their tracked races.
// With sync turned off it removes every event Hytte created.
func SyncCalendar(ctx context.Context, db *sql.DB, w CalendarWriter, userID int64, now time.Time) (CalendarSyncResult, error) {
	var res CalendarSyncResult
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return res, err
	}
	u := recipient{id: userID, prefs: prefs, lang: languageOf(prefs), loc: zoneOf(prefs)}
	calendarID := prefs[PrefCalendarID]
	if calendarID == "" {
		calendarID = "primary"
	}

	var desired []calendarItem
	if prefs[PrefCalendarSync] == "true" {
		if desired, err = desiredCalendarItems(ctx, db, u, appBaseURL(), now); err != nil {
			return res, err
		}
	}

	rows, err := db.QueryContext(ctx, `SELECT item_key, calendar_id, google_event_id, synced_hash FROM race_calendar_events WHERE user_id = ?`, userID)
	if err != nil {
		return res, fmt.Errorf("load synced calendar events: %w", err)
	}
	existing := map[string]syncedEvent{}
	for rows.Next() {
		var s syncedEvent
		if err := rows.Scan(&s.key, &s.calendarID, &s.googleID, &s.hash); err != nil {
			rows.Close()
			return res, err
		}
		existing[s.key] = s
	}
	rows.Close()

	save := func(key, googleID, hash string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO race_calendar_events (user_id, item_key, calendar_id, google_event_id, synced_hash, synced_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(user_id, item_key) DO UPDATE SET calendar_id = excluded.calendar_id,
			google_event_id = excluded.google_event_id, synced_hash = excluded.synced_hash, synced_at = excluded.synced_at`,
			userID, key, calendarID, googleID, hash, ts(now))
		return err
	}

	var errs []string
	wanted := map[string]bool{}
	for _, it := range desired {
		wanted[it.key] = true
		h := it.hash(calendarID)
		old, had := existing[it.key]
		switch {
		case had && old.hash == h:
			res.Kept++
			continue
		case had && old.calendarID == calendarID:
			err := w.UpdateEvent(ctx, userID, calendarID, old.googleID, it.ev)
			if err == nil {
				res.Updated++
				if err := save(it.key, old.googleID, h); err != nil {
					errs = append(errs, err.Error())
				}
				continue
			}
			if !errors.Is(err, calendar.ErrEventGone) {
				errs = append(errs, err.Error())
				continue
			}
		case had:
			// Calendar changed: move the event.
			if err := w.DeleteEvent(ctx, userID, old.calendarID, old.googleID); err != nil {
				errs = append(errs, err.Error())
				continue
			}
		}
		id, err := w.InsertEvent(ctx, userID, calendarID, it.ev)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		res.Created++
		if err := save(it.key, id, h); err != nil {
			errs = append(errs, err.Error())
		}
	}
	for key, old := range existing {
		if wanted[key] {
			continue
		}
		if err := w.DeleteEvent(ctx, userID, old.calendarID, old.googleID); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		res.Deleted++
		if _, err := db.ExecContext(ctx, `DELETE FROM race_calendar_events WHERE user_id = ? AND item_key = ?`, userID, key); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return res, fmt.Errorf("calendar sync: %s", strings.Join(errs, "; "))
	}
	return res, nil
}

// calendarUsers are users with sync on, plus anyone with leftover synced
// events (sync just turned off: clean up).
func calendarUsers(ctx context.Context, db *sql.DB) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT user_id FROM user_preferences WHERE key = ? AND value = 'true'
		UNION SELECT DISTINCT user_id FROM race_calendar_events`, PrefCalendarSync)
	if err != nil {
		return nil, err
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

// SyncUserCalendarSoon syncs one user's calendar in the background, after a
// watch change. Best effort: the periodic loop catches anything missed.
func SyncUserCalendarSoon(db *sql.DB, userID int64) {
	var on int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_preferences WHERE user_id = ? AND key = ? AND value = 'true'`,
		userID, PrefCalendarSync).Scan(&on); err != nil || on == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := SyncCalendar(ctx, db, calendar.NewClient(db), userID, time.Now()); err != nil {
			log.Printf("races: calendar sync for user %d: %v", userID, err)
		}
	}()
}

// RunCalendarLoop syncs every opted-in user's calendar periodically.
func RunCalendarLoop(ctx context.Context, db *sql.DB, interval time.Duration) {
	client := calendar.NewClient(db)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		users, err := calendarUsers(ctx, db)
		if err != nil {
			log.Printf("races: calendar users: %v", err)
		}
		for _, id := range users {
			if ok, _ := auth.HasGoogleToken(db, id); !ok {
				continue
			}
			syncCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if _, err := SyncCalendar(syncCtx, db, client, id, time.Now()); err != nil {
				log.Printf("races: calendar sync for user %d: %v", id, err)
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
