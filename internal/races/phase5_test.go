package races

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLedgerFollowsLotteryOutcomes(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	mk := func(name, date string) int64 {
		in := validInput()
		in.Name, in.RaceDate, in.EditionYear = name, date, 0
		e, err := CreateEvent(ctx, d, in, "", "manual", 1)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	berlin26 := mk("Berlin Marathon 2026", "2026-09-27")
	berlin27 := mk("Berlin Marathon", "2027-09-26")
	london27 := mk("London Marathon", "2027-04-25")
	nyc27 := mk("New York Marathon", "2027-11-07")
	set := func(id int64, states ...string) {
		for _, s := range states {
			if _, err := SetWatch(ctx, d, 2, id, WatchInput{State: s}); err != nil {
				t.Fatal(err)
			}
		}
	}
	set(berlin26, "watching", "lottery_entered", "not_selected")
	set(berlin27, "lottery_entered", "not_selected")
	set(london27, "lottery_entered", "got_place", "registered")
	set(nyc27, "planning", "lottery_entered")

	l, err := BuildLedger(ctx, d, 2)
	if err != nil {
		t.Fatal(err)
	}
	if l.Entered != 4 || l.Won != 1 || l.Lost != 2 || l.Pending != 1 {
		t.Fatalf("ledger counts = %+v", l)
	}
	if l.Entries[0].EventID != nyc27 || l.Entries[0].Outcome != "pending" {
		t.Fatalf("newest first: %+v", l.Entries[0])
	}
	if len(l.Streaks) != 1 || l.Streaks[0].Losses != 2 || !strings.HasPrefix(l.Streaks[0].Race, "Berlin") {
		t.Fatalf("streaks = %+v", l.Streaks)
	}
	// Other users don't see it.
	if other, _ := BuildLedger(ctx, d, 1); other.Entered != 0 {
		t.Fatalf("user 1 ledger = %+v", other)
	}
}

func TestSeriesProgressCountsAutoAndManualFinishes(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if err := SeedCatalog(ctx, d); err != nil {
		t.Fatal(err)
	}
	var berlin, london int64
	if err := d.QueryRow(`SELECT id FROM race_events WHERE slug = 'bmw-berlin-marathon-2027'`).Scan(&berlin); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT id FROM race_events WHERE slug = 'tcs-london-marathon-2027'`).Scan(&london); err != nil {
		t.Fatal(err)
	}
	if SeriesKeyForSlug("vienna-city-marathon-halvmaraton-2027") != "" || SeriesKeyForSlug("vienna-city-marathon-2027") != "vienna" {
		t.Fatal("slug → series key mapping wrong for Vienna")
	}

	// Completing a catalog race records it; London counts for both series.
	if _, err := SetWatch(ctx, d, 1, london, WatchInput{State: "completed"}); err != nil {
		t.Fatal(err)
	}
	finish := 3*3600 + 20*60
	if err := SaveFinish(ctx, d, 1, FinishInput{RaceKey: "berlin", Year: 2019, FinishSeconds: &finish}, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := SaveFinish(ctx, d, 1, FinishInput{RaceKey: "rome", Year: 2024}, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := SaveFinish(ctx, d, 1, FinishInput{RaceKey: "oslo", Year: 2024}, "manual"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown race: %v", err)
	}

	p, err := BuildSeriesProgress(ctx, d, 1, "2026-10-10")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]SeriesProgress{}
	for _, s := range p {
		byKey[s.Key] = s
	}
	if byKey["majors"].Done != 2 || byKey["majors"].Required != 7 {
		t.Fatalf("majors = %d/%d", byKey["majors"].Done, byKey["majors"].Required)
	}
	if byKey["emc"].Done != 2 || byKey["emc"].Required != 5 || len(byKey["emc"].Races) != 8 {
		t.Fatalf("emc = %+v", byKey["emc"])
	}
	for _, r := range byKey["majors"].Races {
		switch r.Key {
		case "london":
			if len(r.Finishes) != 1 || r.Finishes[0].Source != "result" || r.Finishes[0].Year != 2027 {
				t.Fatalf("london finishes = %+v", r.Finishes)
			}
		case "berlin":
			if r.NextEventID == nil || *r.NextEventID != berlin || *r.Finishes[0].FinishSeconds != finish {
				t.Fatalf("berlin = %+v", r)
			}
		}
	}
	if err := DeleteFinish(ctx, d, 1, "rome", 2024); err != nil {
		t.Fatal(err)
	}
	if err := DeleteFinish(ctx, d, 1, "rome", 2024); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestFamilyWatchesShareStatusesNotNotes(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (3, 'c@example.com', 'Kari Nordmann', '', 'g3', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE users SET is_admin = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO user_features (user_id, feature_key, enabled) VALUES (3, 'races', 1)`); err != nil {
		t.Fatal(err)
	}
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-01-01"})
	watch(t, d, 1, e.ID, "registered")
	if _, err := SetWatch(ctx, d, 3, e.ID, WatchInput{State: "lottery_entered", Notes: "secret"}); err != nil {
		t.Fatal(err)
	}
	watch(t, d, 2, e.ID, "planning") // user 2 has no races feature: not shown

	fam, err := FamilyWatches(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := fam[e.ID]
	if len(got) != 1 || got[0].Name != "Kari" || got[0].State != "lottery_entered" {
		t.Fatalf("family for user 1 = %+v", got)
	}
	// Kari sees user 1 (admin), not herself.
	if fam3, _ := FamilyWatches(ctx, d, 3); len(fam3[e.ID]) != 1 || fam3[e.ID][0].UserID != 1 {
		t.Fatalf("family for user 3 = %+v", fam3[e.ID])
	}
	// Opting out hides her.
	if _, err := d.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (3, ?, 'false')`, PrefShareRaces); err != nil {
		t.Fatal(err)
	}
	if fam, _ := FamilyWatches(ctx, d, 1); len(fam[e.ID]) != 0 {
		t.Fatalf("opted-out user still shown: %+v", fam[e.ID])
	}
}

func TestHomeBaseSettingsAndPrompt(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	s := DefaultResearchSettings
	s.HomeCity, s.HomeAirport = " Trondheim ", "trd"
	if err := SaveResearchSettings(ctx, d, s, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadResearchSettings(ctx, d)
	if got.HomeCity != "Trondheim" || got.HomeAirport != "TRD" {
		t.Fatalf("home = %q %q", got.HomeCity, got.HomeAirport)
	}
	bad := got
	bad.HomeAirport = "TRDX"
	if err := SaveResearchSettings(ctx, d, bad, 1); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad airport: %v", err)
	}
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-01-01"})
	full, _ := GetEvent(ctx, d, e.ID)
	prompt, err := raceResearchPrompt(full, "2026-10-10", got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "based in Trondheim") || !strings.Contains(prompt, "from Trondheim airport TRD") ||
		strings.Contains(prompt, "based in Bergen") || strings.Contains(prompt, "BGO") {
		t.Fatalf("prompt doesn't use the home base:\n%s", prompt[:400])
	}
}
