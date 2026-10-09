package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

type sentPush struct {
	userID int64
	n      push.Notification
}

// testNotifier wires a Notifier to an in-memory outbox and per-user prefs.
func testNotifier(t *testing.T, d *sql.DB, prefs map[int64]map[string]string) (*Notifier, *[]sentPush) {
	t.Helper()
	var out []sentPush
	n := &Notifier{
		DB: d,
		Send: func(_ *sql.DB, userID int64, payload []byte) error {
			var p push.Notification
			if err := json.Unmarshal(payload, &p); err != nil {
				t.Fatalf("payload: %v", err)
			}
			out = append(out, sentPush{userID, p})
			return nil
		},
		VAPIDConfigured: func(*sql.DB) (bool, error) { return true, nil },
		LoadPrefs: func(_ *sql.DB, userID int64) (map[string]string, error) {
			if p := prefs[userID]; p != nil {
				return p, nil
			}
			return map[string]string{}, nil
		},
		Quiet: quiethours.IsActiveWithPrefsAt,
	}
	if _, err := d.Exec(`UPDATE users SET is_admin = 1`); err != nil {
		t.Fatalf("admins: %v", err)
	}
	for _, id := range []int64{1, 2} {
		if _, err := d.Exec(`INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth) VALUES (?, ?, 'k', 'a')`,
			id, "https://push.example/"+string(rune('0'+id))); err != nil {
			t.Fatalf("subscription: %v", err)
		}
	}
	return n, &out
}

func oslo(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func newRaceWithDeadline(t *testing.T, d *sql.DB, dl DeadlineInput) (*Event, *Deadline) {
	t.Helper()
	ctx := context.Background()
	e, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatalf("create race: %v", err)
	}
	created, err := CreateDeadline(ctx, d, e.ID, dl, "manual", 1)
	if err != nil {
		t.Fatalf("create deadline: %v", err)
	}
	return e, created
}

func watch(t *testing.T, d *sql.DB, userID, eventID int64, state string) {
	t.Helper()
	if _, err := SetWatch(context.Background(), d, userID, eventID, WatchInput{State: state}); err != nil {
		t.Fatalf("watch: %v", err)
	}
}

func run(t *testing.T, n *Notifier, now time.Time) {
	t.Helper()
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestDeadlineRemindersFireOncePerSlotAndRearmWhenMoved(t *testing.T) {
	d := setupTestDB(t)
	n, out := testNotifier(t, d, nil)
	e, dl := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2027-02-15",
		Texts: map[string]DeadlineText{"en": {What: "Closes unless sold out."}}})
	watch(t, d, 2, e.ID, "planning")

	run(t, n, oslo(t, "2027-02-08 08:55")) // before the week slot (09:00)
	if len(*out) != 0 {
		t.Fatalf("early pushes: %+v", *out)
	}
	run(t, n, oslo(t, "2027-02-08 09:05"))
	if len(*out) != 1 || (*out)[0].userID != 2 {
		t.Fatalf("week reminder: %+v", *out)
	}
	if got := (*out)[0].n; got.Title != "Test Half" || got.Body != "Entry closes in 7 days (15 Feb). Closes unless sold out." || got.URL == "" {
		t.Fatalf("week reminder = %+v", got)
	}
	run(t, n, oslo(t, "2027-02-08 12:00")) // same slot again: nothing new
	if len(*out) != 1 {
		t.Fatalf("duplicate week reminder: %d pushes", len(*out))
	}

	run(t, n, oslo(t, "2027-02-14 09:10"))
	if len(*out) != 2 || !strings.HasPrefix((*out)[1].n.Body, "Entry closes tomorrow.") {
		t.Fatalf("day reminder: %+v", (*out)[len(*out)-1])
	}
	run(t, n, oslo(t, "2027-02-15 08:01"))
	if len(*out) != 3 || !strings.HasPrefix((*out)[2].n.Body, "Entry closes today.") {
		t.Fatalf("final reminder: %+v", (*out)[len(*out)-1])
	}
	run(t, n, oslo(t, "2027-02-16 09:00")) // passed
	if len(*out) != 3 {
		t.Fatalf("reminder after deadline: %+v", (*out)[len(*out)-1])
	}

	// Moving the deadline re-arms its reminders.
	if _, err := UpdateDeadline(context.Background(), d, dl.ID, DeadlineInput{Kind: "entry_closes", DueDate: "2027-02-22"}, "manual", 1); err != nil {
		t.Fatalf("move: %v", err)
	}
	run(t, n, oslo(t, "2027-02-16 09:05"))
	if len(*out) != 4 || !strings.Contains((*out)[3].n.Body, "in 6 days (22 Feb)") {
		t.Fatalf("re-armed reminder: %+v", (*out)[len(*out)-1])
	}
}

func TestExactTimeDeadlineIsRemindedBeforeQuietHours(t *testing.T) {
	d := setupTestDB(t)
	quiet := map[string]string{"quiet_hours_enabled": "true", "quiet_hours_start": "22:00",
		"quiet_hours_end": "07:00", "quiet_hours_timezone": "Europe/Oslo"}
	n, out := testNotifier(t, d, map[int64]map[string]string{2: quiet})
	// 10:00 Sydney = 01:00 Oslo on Mon 19 Oct, inside quiet hours.
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "lottery_closes", DueDate: "2026-10-19", DueTime: "10:00", TZ: "Australia/Sydney"})
	watch(t, d, 2, e.ID, "watching")

	// The 3-hours-before slot (22:00) is moved to 21:45, before quiet starts.
	run(t, n, oslo(t, "2026-10-18 21:40"))
	before := len(*out)
	run(t, n, oslo(t, "2026-10-18 21:47"))
	if len(*out) != before+1 {
		t.Fatalf("no final reminder before quiet hours: %+v", *out)
	}
	last := (*out)[len(*out)-1].n
	if last.Body != "Lottery closes Mon 19 Oct at 01:00." {
		t.Fatalf("final reminder body = %q", last.Body)
	}
	// Nothing more during the night.
	run(t, n, oslo(t, "2026-10-18 23:30"))
	if len(*out) != before+1 {
		t.Fatalf("pushed during quiet hours: %+v", (*out)[len(*out)-1])
	}
}

func TestRemindersFollowWatchStateAndToggleAndLanguage(t *testing.T) {
	d := setupTestDB(t)
	prefs := map[int64]map[string]string{1: {PrefNotifyDeadlines: "false"}, 2: {PrefLanguage: "nb"}}
	n, out := testNotifier(t, d, prefs)
	ctx := context.Background()
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "lottery_closes", DueDate: "2027-03-10"})
	if _, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "payment_due", DueDate: "2027-03-10"}, "manual", 1); err != nil {
		t.Fatal(err)
	}
	watch(t, d, 1, e.ID, "watching")   // reminders turned off
	watch(t, d, 2, e.ID, "registered") // only the payment matters

	run(t, n, oslo(t, "2027-03-09 09:05"))
	if len(*out) != 1 || (*out)[0].userID != 2 {
		t.Fatalf("pushes = %+v, want one for user 2", *out)
	}
	if body := (*out)[0].n.Body; body != "Betalingsfrist i morgen." {
		t.Fatalf("nb body = %q", body)
	}
}

func TestLateWatchDuringQuietHoursWaitsForMorning(t *testing.T) {
	d := setupTestDB(t)
	quiet := map[string]string{"quiet_hours_enabled": "true", "quiet_hours_start": "22:00",
		"quiet_hours_end": "07:00", "quiet_hours_timezone": "Europe/Oslo"}
	n, out := testNotifier(t, d, map[int64]map[string]string{2: quiet})
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2027-05-20"})
	watch(t, d, 2, e.ID, "watching")

	run(t, n, oslo(t, "2027-05-15 23:30")) // week slot is due, but it's night
	if len(*out) != 0 {
		t.Fatalf("pushed at night: %+v", *out)
	}
	run(t, n, oslo(t, "2027-05-16 07:05"))
	if len(*out) != 1 || !strings.Contains((*out)[0].n.Body, "in 4 days") {
		t.Fatalf("morning push = %+v", *out)
	}
}

func TestChangeNotificationsGroupPerRaceAndSkipTheEditor(t *testing.T) {
	d := setupTestDB(t)
	quiet := map[string]string{"quiet_hours_enabled": "true", "quiet_hours_start": "00:00",
		"quiet_hours_end": "23:59", "quiet_hours_timezone": "Europe/Oslo"}
	prefs := map[int64]map[string]string{1: {}, 2: {PrefNotifyDeadlines: "false"}}
	n, out := testNotifier(t, d, prefs)
	ctx := context.Background()

	e, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatal(err)
	}
	// A change before user 2 watched is history, not news.
	early := validInput()
	early.EntryType = "lottery"
	if _, _, err := UpdateEvent(ctx, d, e.ID, early, "manual", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE race_changes SET created_at = '2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	watch(t, d, 1, e.ID, "watching")
	watch(t, d, 2, e.ID, "watching")

	// User 1 (admin) closes the race and adds a deadline: two changes, one push.
	edit := early
	edit.Status = StatusClosed
	if _, _, err := UpdateEvent(ctx, d, e.ID, edit, "manual", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "waitlist_closes", DueDate: "2027-03-31"}, "manual", 1); err != nil {
		t.Fatal(err)
	}

	run(t, n, time.Now()) // still settling
	if len(*out) != 0 {
		t.Fatalf("pushed before settle: %+v", *out)
	}

	// User 2 is in quiet hours: held, not lost.
	prefs[2] = quiet
	run(t, n, time.Now().Add(3*time.Minute))
	if len(*out) != 0 {
		t.Fatalf("pushed during quiet hours: %+v", *out)
	}
	prefs[2] = map[string]string{}
	run(t, n, time.Now().Add(4*time.Minute))
	if len(*out) != 1 || (*out)[0].userID != 2 {
		t.Fatalf("pushes = %+v, want exactly one for user 2 (user 1 made the edits)", *out)
	}
	body := (*out)[0].n.Body
	if !strings.Contains(body, "Status: Open now → Closed") || !strings.Contains(body, "Waitlist closes: 31 Mar 2027 (new)") ||
		strings.Contains(body, "Entry") {
		t.Fatalf("change body = %q", body)
	}

	run(t, n, time.Now().Add(5*time.Minute))
	if len(*out) != 1 {
		t.Fatalf("change pushed twice: %+v", *out)
	}
}

func TestParseDeadlineDescRoundTrips(t *testing.T) {
	in := DeadlineInput{Kind: "lottery_closes", DueDate: "2026-10-29", DueTime: "14:00", TZ: "America/Chicago", Expected: true}
	p, ok := parseDeadlineDesc(describeDeadline(in))
	if !ok || p.date != "2026-10-29" || p.clock != "14:00" || p.tz != "America/Chicago" || !p.expected {
		t.Fatalf("parsed = %+v ok=%v", p, ok)
	}
	if _, ok := parseDeadlineDesc("garbage"); ok {
		t.Fatal("garbage parsed")
	}
}
