package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

func testResearcher(t *testing.T, d *sql.DB, reply func(prompt string) (string, float64, error), budget float64) *Researcher {
	t.Helper()
	return &Researcher{
		DB: d,
		Run: func(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, float64, error) {
			return reply(prompt)
		},
		Config: func(context.Context, *sql.DB) (*training.ClaudeConfig, float64, error) {
			return &training.ClaudeConfig{Enabled: true, CLIPath: "claude", Model: DefaultResearchModel}, budget, nil
		},
		Now: time.Now,
	}
}

func waitRun(t *testing.T, r *Researcher, id int64) *ResearchRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := r.GetRun(context.Background(), id)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status != "running" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("research run did not finish")
	return nil
}

// replyFor builds a model reply from the current entry with edits applied.
func replyFor(t *testing.T, e *Event, edit func(res *raceResearchResult)) string {
	t.Helper()
	res := raceResearchResult{Event: eventToInput(e), Confident: true, Summary: "Lottery opened.", Sources: []string{"https://example.com"}}
	for _, d := range e.Deadlines {
		id := d.ID
		res.Deadlines = append(res.Deadlines, promptDeadline{ID: &id, DeadlineInput: deadlineAsInput(&d)})
	}
	edit(&res)
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	// The CLI appends a sources list after the JSON; the parser must cope.
	return "Here you go:\n" + string(b) + "\n\nSources:\n- [Example](https://example.com)"
}

func TestResearchAppliesChangesThroughTheChangeLog(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, keep := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-04-01",
		Texts: map[string]DeadlineText{"en": {What: "Closes."}}})
	stale, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "other", DueDate: "2099-02-01"}, "manual", 1)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := GetEvent(ctx, d, e.ID)

	r := testResearcher(t, d, func(prompt string) (string, float64, error) {
		if !strings.Contains(prompt, `"name": "Test Half"`) || !strings.Contains(prompt, "Today is ") {
			t.Errorf("prompt lacks the current entry or date")
		}
		return replyFor(t, current, func(res *raceResearchResult) {
			res.Event.Status = StatusOpen
			res.Event.EntryType = "lottery"
			res.Event.EditionYear = 2031 // must be ignored
			en := res.Event.Texts["en"]
			en.How = "  Open.  "         // whitespace-only: no change
			en.Place = ""                // blank: keep stored
			en.Price = "€59 until 1 Nov" // real change
			res.Event.Texts["en"] = en
			res.Deadlines = append(res.Deadlines, promptDeadline{DeadlineInput: DeadlineInput{Kind: "lottery_closes", DueDate: "2099-03-01",
				Texts: map[string]DeadlineText{"en": {What: "Ballot closes."}}}})
			res.Deadlines = append(res.Deadlines, promptDeadline{DeadlineInput: DeadlineInput{Kind: "other", DueDate: "2001-01-01"}}) // past: dropped
			res.RemoveDeadlineIDs = []int64{stale.ID, 99999}
		}), 0.42, nil
	}, 5)

	run, err := r.StartEventResearch(ctx, e.ID, 2, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, r, run.ID)
	if run.Status != "done" || run.CostUSD != 0.42 || run.Summary != "Lottery opened." || len(run.Sources) != 1 {
		t.Fatalf("run = %+v", run)
	}
	// entry_type + texts.en.price + new deadline + removed deadline = 4.
	if run.Changes != 4 {
		t.Fatalf("changes = %d (%s), want 4", run.Changes, run.Error)
	}

	after, _ := GetEvent(ctx, d, e.ID)
	if after.Status != StatusOpen || after.EntryType != "lottery" || after.EditionYear != 2027 {
		t.Fatalf("event after = %+v", after)
	}
	if after.Texts["en"].How != "Open." || after.Texts["en"].Place != "Bergen, Norway" || after.Texts["en"].Price != "€59 until 1 Nov" {
		t.Fatalf("en texts = %+v", after.Texts["en"])
	}
	kinds := map[string]bool{}
	for _, dl := range after.Deadlines {
		kinds[dl.Kind] = true
	}
	if len(after.Deadlines) != 2 || !kinds["entry_closes"] || !kinds["lottery_closes"] {
		t.Fatalf("deadlines after = %+v", after.Deadlines)
	}
	_ = keep

	var anonymous, total int
	if err := d.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN user_id IS NULL AND source = 'research' THEN 1 ELSE 0 END)
		FROM race_changes WHERE event_id = ? AND field != 'created'`, e.ID).Scan(&total, &anonymous); err != nil {
		t.Fatal(err)
	}
	// Two manual deadline creations plus the four research changes.
	if anonymous != 4 {
		t.Fatalf("research changes logged = %d of %d, want 4 with no user (so watchers are told)", anonymous, total)
	}

	// Asking again right away: the non-admin cooldown applies, the admin isn't held.
	if _, err := r.StartEventResearch(ctx, e.ID, 2, false); !errors.Is(err, ErrResearchCooldown) {
		t.Fatalf("second non-admin start: %v, want cooldown", err)
	}
	adminRun, err := r.StartEventResearch(ctx, e.ID, 1, true)
	if err != nil {
		t.Fatalf("admin start: %v", err)
	}
	waitRun(t, r, adminRun.ID)
}

func TestResearchSkipsUnconfidentAndFailsOnGarbage(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-04-01"})
	current, _ := GetEvent(ctx, d, e.ID)

	replies := []string{
		replyFor(t, current, func(res *raceResearchResult) { res.Confident = false; res.Event.Status = StatusClosed }),
		"Sorry, I could not find anything.",
	}
	call := 0
	r := testResearcher(t, d, func(string) (string, float64, error) {
		call++
		return replies[call-1], 0.1, nil
	}, 5)

	run, err := r.StartEventResearch(ctx, e.ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if run = waitRun(t, r, run.ID); run.Status != "skipped" {
		t.Fatalf("unconfident run = %+v", run)
	}
	if after, _ := GetEvent(ctx, d, e.ID); after.Status != StatusOpen {
		t.Fatalf("unconfident result applied: status %s", after.Status)
	}
	run, err = r.StartEventResearch(ctx, e.ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if run = waitRun(t, r, run.ID); run.Status != "failed" || !strings.Contains(run.Error, "could not read") {
		t.Fatalf("garbage run = %+v", run)
	}

	latest, err := LatestRunForEvent(ctx, d, e.ID)
	if err != nil || latest == nil || latest.ID != run.ID {
		t.Fatalf("latest run = %+v, %v", latest, err)
	}
}

func TestResearchBudgetAndBusy(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-04-01"})

	r := testResearcher(t, d, func(string) (string, float64, error) { return "{}", 0, nil }, 1.0)
	if _, err := d.Exec(`INSERT INTO race_research_runs (kind, trigger, status, started_at, cost_usd)
		VALUES ('race', 'scheduled', 'done', ?, 1.25)`, ts(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartEventResearch(ctx, e.ID, 1, true); !errors.Is(err, ErrResearchBudget) {
		t.Fatalf("over budget: %v", err)
	}

	r2 := testResearcher(t, d, func(string) (string, float64, error) { return "{}", 0, nil }, 10)
	if _, err := d.Exec(`INSERT INTO race_research_runs (kind, event_id, trigger, status, started_at)
		VALUES ('race', ?, 'manual', 'running', ?)`, e.ID, ts(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.StartEventResearch(ctx, e.ID, 1, true); !errors.Is(err, ErrResearchBusy) {
		t.Fatalf("while running: %v", err)
	}
	r2.MarkInterrupted(ctx)
	var status string
	if err := d.QueryRow(`SELECT status FROM race_research_runs WHERE event_id = ?`, e.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("interrupted run status = %q, %v", status, err)
	}
}

func TestPickNightlyPrioritizesWatchedRacesWithDeadlines(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	mk := func(name string, checkedAgo time.Duration) int64 {
		in := validInput()
		in.Name = name
		in.RaceDate = now.AddDate(0, 6, 0).Format("2006-01-02")
		e, err := CreateEvent(ctx, d, in, ts(now.Add(-checkedAgo)), "manual", 1)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	watchedSoon := mk("Watched Soon", time.Hour)              // checked recently, but a deadline is near
	watchedOld := mk("Watched Old", 4*24*time.Hour)           // watched, stale
	staleOther := mk("Stale Other", 20*24*time.Hour)          // unwatched, fortnight-stale
	fresh := mk("Fresh Other", 2*24*time.Hour)                // unwatched, fresh: skipped
	recentlyRun := mk("Recently Researched", 30*24*time.Hour) // researched an hour ago: skipped
	watch(t, d, 2, watchedSoon, "watching")
	watch(t, d, 2, watchedOld, "planning")
	if _, err := CreateDeadline(ctx, d, watchedSoon, DeadlineInput{Kind: "entry_closes", DueDate: now.AddDate(0, 0, 10).Format("2006-01-02")}, "manual", 1); err != nil {
		t.Fatal(err)
	}
	// Creating a deadline stamps checked_at; push it back so only "soon" qualifies it.
	if _, err := d.Exec(`UPDATE race_events SET checked_at = ? WHERE id = ?`, ts(now.Add(-time.Hour)), watchedSoon); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO race_research_runs (kind, event_id, trigger, status, started_at) VALUES ('race', ?, 'scheduled', 'done', ?)`,
		recentlyRun, ts(now.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}

	r := testResearcher(t, d, nil, 5)
	ids, err := r.pickNightly(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{watchedSoon, watchedOld, staleOther}
	if len(ids) != len(want) {
		t.Fatalf("picked %v, want %v (fresh %d, recently run %d excluded)", ids, want, fresh, recentlyRun)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("picked %v, want %v", ids, want)
		}
	}
}

func TestDiscoverAddsMissingRacesOnly(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := CreateEvent(ctx, d, validInput(), "", "manual", 1); err != nil { // "Test Half" 2027
		t.Fatal(err)
	}
	reply := `{"events": [
		{"name": "Test Half", "edition_year": 2027, "race_date": "2027-05-09", "country": "NO", "distance_m": 21097, "status": "open", "entry_type": "fcfs", "texts": {"nb": {"place": "Bergen"}}},
		{"name": "New Marathon", "edition_year": 2099, "race_date": "2099-06-01", "country": "DE", "distance_m": 42195, "status": "later", "entry_type": "lottery",
		 "texts": {"nb": {"place": "Berlin, Tyskland"}, "en": {"place": "Berlin, Germany"}, "th": {"place": "เบอร์ลิน, เยอรมนี"}},
		 "deadlines": [{"kind": "lottery_opens", "due_date": "2098-11-01", "expected": true, "texts": {"en": {"what": "Ballot opens."}}}]},
		{"name": "Old Race", "edition_year": 2001, "race_date": "2001-06-01", "country": "DE", "distance_m": 42195, "status": "closed"},
		{"name": "A 10K", "edition_year": 2099, "race_date": "2099-06-01", "country": "DE", "distance_m": 10000, "status": "open"}
	], "summary": "Found one new race.", "sources": ["https://example.com"]}`
	r := testResearcher(t, d, func(prompt string) (string, float64, error) {
		if !strings.Contains(prompt, "test-half-2027 | Test Half") {
			t.Errorf("discover prompt doesn't list the catalog")
		}
		return reply, 0.9, nil
	}, 5)

	run, err := r.StartDiscovery(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, r, run.ID)
	if run.Status != "done" || run.Changes != 1 || !strings.Contains(run.Error, "not a marathon or half") {
		t.Fatalf("discover run = %+v", run)
	}
	events, _ := ListEvents(ctx, d)
	if len(events) != 2 {
		t.Fatalf("catalog has %d races, want 2", len(events))
	}
	var added *Event
	for i := range events {
		if events[i].Name == "New Marathon" {
			added = &events[i]
		}
	}
	if added == nil || added.Slug != "new-marathon-2099" || len(added.Deadlines) != 1 || added.Texts["th"].Place == "" {
		t.Fatalf("added = %+v", added)
	}
}

func TestNextResearchRun(t *testing.T) {
	loc := zoneOrUTC()
	at := time.Date(2026, 10, 10, 2, 0, 0, 0, loc)
	if got := NextResearchRun(at); got.Format("2006-01-02 15:04") != "2026-10-10 03:30" {
		t.Fatalf("before 03:30: %v", got)
	}
	at = time.Date(2026, 10, 10, 4, 0, 0, 0, loc)
	if got := NextResearchRun(at); got.Format("2006-01-02 15:04") != "2026-10-11 03:30" {
		t.Fatalf("after 03:30: %v", got)
	}
}
