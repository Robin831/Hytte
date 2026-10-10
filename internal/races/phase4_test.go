package races

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/calendar"
	"github.com/Robin831/Hytte/internal/stride"
)

func TestLinkToStrideCreatesFollowsAndUnlinks(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`UPDATE users SET is_admin = 1 WHERE id = 1`); err != nil { // admins have every feature
		t.Fatal(err)
	}
	in := validInput()
	in.DistanceM = 42195
	e, err := CreateEvent(ctx, d, in, "", "manual", 1)
	if err != nil {
		t.Fatal(err)
	}

	target := 3*3600 + 15*60
	watch, sr, err := LinkToStride(ctx, d, 1, e.ID, StrideLinkInput{Priority: "a", TargetTime: &target})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if watch.State != "registered" || watch.StrideRaceID == nil || *watch.StrideRaceID != sr.ID {
		t.Fatalf("watch = %+v", watch)
	}
	if sr.Name != "Test Half" || sr.Date != "2027-05-09" || sr.DistanceM != 42195 || sr.Priority != "A" || *sr.TargetTime != target {
		t.Fatalf("stride race = %+v", sr)
	}
	if _, _, err := LinkToStride(ctx, d, 1, e.ID, StrideLinkInput{}); !errors.Is(err, ErrAlreadyLinked) {
		t.Fatalf("second link: %v", err)
	}

	// A catalog date change follows into Stride.
	moved := in
	moved.RaceDate = "2027-05-16"
	if _, _, err := UpdateEvent(ctx, d, e.ID, moved, "research", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := stride.GetRaceByID(d, sr.ID, 1); got.Date != "2027-05-16" {
		t.Fatalf("stride date = %s, want it moved to 2027-05-16", got.Date)
	}

	// Unlink keeps the Stride race unless asked to delete it.
	if err := UnlinkFromStride(ctx, d, 1, e.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := stride.GetRaceByID(d, sr.ID, 1); err != nil {
		t.Fatalf("stride race gone after plain unlink: %v", err)
	}
	if err := UnlinkFromStride(ctx, d, 1, e.ID, false); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("unlink twice: %v", err)
	}
	_, sr2, err := LinkToStride(ctx, d, 1, e.ID, StrideLinkInput{Priority: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := UnlinkFromStride(ctx, d, 1, e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := stride.GetRaceByID(d, sr2.ID, 1); err == nil {
		t.Fatal("stride race still there after unlink with delete")
	}

	// User 2 has no Stride: refused. Bad priority: validation error.
	if _, _, err := LinkToStride(ctx, d, 2, e.ID, StrideLinkInput{}); !errors.Is(err, ErrNoStride) {
		t.Fatalf("no stride: %v", err)
	}
	if _, _, err := LinkToStride(ctx, d, 1, e.ID, StrideLinkInput{Priority: "Z"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad priority: %v", err)
	}
}

type fakeCalendar struct {
	events map[string]calendar.WriteEvent // google id → event
	next   int
	ops    []string
}

func (f *fakeCalendar) InsertEvent(_ context.Context, _ int64, cal string, ev calendar.WriteEvent) (string, error) {
	f.next++
	id := fmt.Sprintf("g%d", f.next)
	f.events[id] = ev
	f.ops = append(f.ops, "insert:"+cal+":"+ev.Summary)
	return id, nil
}

func (f *fakeCalendar) UpdateEvent(_ context.Context, _ int64, _, id string, ev calendar.WriteEvent) error {
	if _, ok := f.events[id]; !ok {
		return calendar.ErrEventGone
	}
	f.events[id] = ev
	f.ops = append(f.ops, "update:"+ev.Summary)
	return nil
}

func (f *fakeCalendar) DeleteEvent(_ context.Context, _ int64, _, id string) error {
	delete(f.events, id)
	f.ops = append(f.ops, "delete:"+id)
	return nil
}

func setPref(t *testing.T, ctx context.Context, userID int64, key, value string, exec func(string, ...any) error) {
	t.Helper()
	if err := exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, key, value); err != nil {
		t.Fatalf("set pref %s: %v", key, err)
	}
	_ = ctx
}

func TestSyncCalendarInsertsUpdatesAndCleansUp(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error { _, err := d.Exec(q, args...); return err }
	now := time.Date(2027, 1, 10, 12, 0, 0, 0, time.UTC)

	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "payment_due", DueDate: "2027-02-01",
		Texts: map[string]DeadlineText{"en": {What: "Pay up."}}})
	if _, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "lottery_closes", DueDate: "2027-01-20", DueTime: "14:00", TZ: "America/Chicago"}, "manual", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "entry_opens", DueDate: "2027-03-01", DatePrecision: "month"}, "manual", 1); err != nil {
		t.Fatal(err)
	}
	watch(t, d, 1, e.ID, "registered")
	fake := &fakeCalendar{events: map[string]calendar.WriteEvent{}}

	// Off by default: nothing is written.
	if res, err := SyncCalendar(ctx, d, fake, 1, now); err != nil || res.Created != 0 {
		t.Fatalf("sync while off: %+v %v", res, err)
	}

	setPref(t, ctx, 1, PrefCalendarSync, "true", exec)
	res, err := SyncCalendar(ctx, d, fake, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	// Registered: the race day and the payment, not the lottery or the month-wide window.
	if res.Created != 2 {
		t.Fatalf("created = %+v, ops %v", res, fake.ops)
	}
	summaries := map[string]calendar.WriteEvent{}
	for _, ev := range fake.events {
		summaries[ev.Summary] = ev
	}
	if day, ok := summaries["🏃 Test Half"]; !ok || day.AllDayDate != "2027-05-09" || day.Location != "Bergen, Norway" {
		t.Fatalf("race day event = %+v (all: %v)", day, fake.ops)
	}
	if pay, ok := summaries["Payment due: Test Half"]; !ok || pay.AllDayDate != "2027-02-01" || !strings.HasPrefix(pay.Description, "Pay up.") {
		t.Fatalf("payment event = %+v", pay)
	}

	// Nothing changed: nothing written.
	if res, _ := SyncCalendar(ctx, d, fake, 1, now); res.Kept != 2 || res.Created+res.Updated+res.Deleted != 0 {
		t.Fatalf("idle sync = %+v", res)
	}

	// The race moves a week: one update. The user deletes the payment event in Google: re-created.
	moved := validInput()
	moved.RaceDate = "2027-05-16"
	if _, _, err := UpdateEvent(ctx, d, e.ID, moved, "manual", 1); err != nil {
		t.Fatal(err)
	}
	for id, ev := range fake.events {
		if strings.HasPrefix(ev.Summary, "Payment") {
			delete(fake.events, id)
		}
	}
	if _, err := d.Exec(`UPDATE race_calendar_events SET synced_hash = 'stale' WHERE item_key LIKE 'deadline:%'`); err != nil {
		t.Fatal(err)
	}
	res, err = SyncCalendar(ctx, d, fake, 1, now)
	if err != nil || res.Updated != 1 || res.Created != 1 {
		t.Fatalf("after move = %+v %v (ops %v)", res, err, fake.ops)
	}

	// Switching it off removes everything Hytte created.
	setPref(t, ctx, 1, PrefCalendarSync, "false", exec)
	res, err = SyncCalendar(ctx, d, fake, 1, now)
	if err != nil || res.Deleted != 2 || len(fake.events) != 0 {
		t.Fatalf("after off = %+v %v, %d events left", res, err, len(fake.events))
	}
	var left int
	_ = d.QueryRow(`SELECT COUNT(*) FROM race_calendar_events WHERE user_id = 1`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d tracking rows left", left)
	}
}
