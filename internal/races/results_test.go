package races

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

func secs(h, m, s int) *int { v := h*3600 + m*60 + s; return &v }

func TestImportReviewAndHallOfFame(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`UPDATE users SET name = 'Robin Smith' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	exactFalse := false
	cands := []ImportCandidate{
		{Person: "Robin", RaceName: "Valencia Marathon", Date: "2016-11-20", DistanceM: 42195, City: "Valencia", Country: "ES",
			FinishSeconds: secs(3, 59, 28), TimeSource: "strava results email", Confidence: "high",
			Evidence: []Evidence{{ThreadID: "158836", From: "strava.com", What: "results email"}}, Notes: "first marathon"},
		{Person: "Robin", RaceName: "Prague Half Marathon", Date: "2023-04-01", DistanceM: 21097, City: "Prague", Country: "CZ",
			FinishSeconds: secs(1, 48, 2), TimeSource: "organizer email", Confidence: "high"},
		{Person: "Robin", RaceName: "Bergen City Marathon", Date: "2017-04-29", DistanceM: 21097, City: "Bergen", Country: "NO",
			Confidence: "medium"},
		{Person: "Robin", RaceName: "Cardiff Half", Date: "2023-10-01", DateExact: &exactFalse, DistanceM: 21097, City: "Cardiff",
			Country: "GB", Confidence: "low"},
		{Person: "Khatiya", RaceName: "Bergen3000", Date: "2025-06-10", DistanceM: 3000, City: "Bergen", Country: "NO",
			FinishSeconds: secs(0, 16, 0), Confidence: "medium"},
		{Person: "Robin", RaceName: "Valencia Marathon", Date: "2016-11-20", DistanceM: 42195, Confidence: "high"}, // duplicate
		{Person: "Robin", RaceName: "Bad", Date: "2016-13-40", DistanceM: 42195},                                   // invalid
	}
	added, skipped, errs := ImportResults(ctx, d, 1, "Robin", cands)
	if added != 5 || skipped != 1 || len(errs) != 1 {
		t.Fatalf("import: added %d skipped %d errs %v", added, skipped, errs)
	}

	results, _ := ListResults(ctx, d, 1)
	var valencia, khatiya *Result
	for i := range results {
		switch results[i].RaceName {
		case "Valencia Marathon":
			valencia = &results[i]
		case "Bergen3000":
			khatiya = &results[i]
		}
	}
	if valencia == nil || valencia.Status != "pending" || valencia.PersonName != "" || valencia.TimeSource != "strava" ||
		valencia.Notes != "first marathon" || len(valencia.Evidence) != 1 || valencia.Source != "gmail" {
		t.Fatalf("valencia = %+v", valencia)
	}
	if khatiya == nil || khatiya.PersonName != "Khatiya" {
		t.Fatalf("guest = %+v", khatiya)
	}
	var rawNotes string
	_ = d.QueryRow(`SELECT notes FROM race_results WHERE id = ?`, valencia.ID).Scan(&rawNotes)
	if strings.Contains(rawNotes, "first marathon") {
		t.Fatal("notes stored in plaintext")
	}

	// Nothing counts until confirmed.
	if h := Summarize(results); h.Races != 0 || h.Pending != 4 {
		t.Fatalf("before confirm: %+v", h)
	}
	n, err := ConfirmResults(ctx, d, 1, nil, true)
	if err != nil || n != 2 {
		t.Fatalf("confirm high: %d %v", n, err)
	}
	results, _ = ListResults(ctx, d, 1)
	var bcm int64
	for _, r := range results {
		if r.RaceName == "Bergen City Marathon" {
			bcm = r.ID
		}
	}
	if n, _ := ConfirmResults(ctx, d, 1, []int64{bcm}, false); n != 1 {
		t.Fatalf("confirm by id: %d", n)
	}
	results, _ = ListResults(ctx, d, 1)
	h := Summarize(results)
	if h.Races != 3 || h.Pending != 1 || h.FirstYear != 2016 || strings.Join(h.Countries, ",") != "CZ,ES,NO" {
		t.Fatalf("summary = %+v", h)
	}
	pbs := map[string]PB{}
	for _, p := range h.PBs {
		pbs[p.Distance] = p
	}
	if pbs["marathon"].FinishSeconds != *secs(3, 59, 28) || pbs["half"].FinishSeconds != *secs(1, 48, 2) || pbs["half"].Count != 2 {
		t.Fatalf("pbs = %+v", pbs)
	}
	if _, ok := pbs["3k"]; ok {
		t.Fatal("a guest's 3k counted as the user's PB")
	}

	// Series: confirmed Prague half counts for SuperHalfs; Valencia full counts for nothing.
	p, _ := BuildSeriesProgress(ctx, d, 1, "2026-10-10")
	for _, s := range p {
		if s.Key == "superhalfs" && s.Done != 1 {
			t.Fatalf("superhalfs done = %d", s.Done)
		}
		if s.Key == "majors" && s.Done != 0 {
			t.Fatalf("majors done = %d", s.Done)
		}
	}

	// Rejecting = deleting.
	if err := DeleteResult(ctx, d, 1, khatiya.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteResult(ctx, d, 2, valencia.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's delete: %v", err)
	}
}

func TestResultSeriesKeyAndWorkoutTimes(t *testing.T) {
	cases := []struct {
		slug, city string
		dist       int
		want       string
	}{
		{"", "Berlin", 42195, "berlin"},
		{"", "Berlin", 21097, "berlin_half"},
		{"", "København", 21097, "copenhagen_half"},
		{"", "Valencia", 42195, ""},
		{"tcs-london-marathon-2027", "", 42195, "london"},
		{"", "Bergen", 21097, ""},
	}
	for _, c := range cases {
		if got := resultSeriesKey(c.slug, c.city, c.dist); got != c.want {
			t.Errorf("resultSeriesKey(%q, %q, %d) = %q, want %q", c.slug, c.city, c.dist, got, c.want)
		}
	}

	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO workouts (user_id, sport, started_at, duration_seconds, distance_meters, fit_file_hash)
		VALUES (1, 'running', '2026-10-04T10:00:00Z', 6668, 21310, 'h1'), (1, 'running', '2026-10-04T18:00:00Z', 1800, 5000, 'h2')`); err != nil {
		t.Fatal(err)
	}
	r, err := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "Cardiff Half", RaceDate: "2026-10-04", DistanceM: 21097, City: "Cardiff"}, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if r.FinishSeconds == nil || *r.FinishSeconds != 6668 || r.TimeSource != "workout" || r.SeriesKey != "cardiff_half" {
		t.Fatalf("cardiff = %+v", r)
	}
	if _, err := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "Cardiff Half", RaceDate: "2026-10-04", DistanceM: 21100}, "manual"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "X", RaceDate: "2026-10-05", DistanceM: 10000, Status: "maybe"}, "manual"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad status: %v", err)
	}
}

func TestCompletedWatchBecomesResult(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-01-01"})
	watch(t, d, 1, e.ID, "registered")
	watch(t, d, 1, e.ID, "completed")
	results, _ := ListResults(ctx, d, 1)
	if len(results) != 1 || results[0].Status != "confirmed" || results[0].EventID == nil || *results[0].EventID != e.ID ||
		results[0].City != "Bergen" || results[0].Source != "catalog" {
		t.Fatalf("results = %+v", results)
	}
	watch(t, d, 1, e.ID, "registered")
	watch(t, d, 1, e.ID, "completed") // no duplicate
	if results, _ := ListResults(ctx, d, 1); len(results) != 1 {
		t.Fatalf("%d results after completing twice", len(results))
	}
}

func TestTimeLookups(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`UPDATE users SET name = 'Robin Smith' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	found, _ := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "Berlin Half", RaceDate: "2025-04-06", DistanceM: 21097, City: "Berlin"}, "manual")
	missing, _ := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "Ålesund Maraton", RaceDate: "2018-09-01", DistanceM: 21097, City: "Ålesund"}, "manual")
	guest, _ := SaveResult(ctx, d, 1, 0, ResultInput{PersonName: "Khatiya", RaceName: "Bergen3000", RaceDate: "2025-06-10", DistanceM: 3000}, "manual")

	var prompts []string
	r := testResearcher(t, d, func(prompt string) (string, float64, error) {
		prompts = append(prompts, prompt)
		switch {
		case strings.Contains(prompt, "Berlin Half"):
			return `{"found": true, "finish_seconds": 6420, "url": "https://results.example/berlin", "note": "chip time"}`, 0.1, nil
		default:
			return `{"found": false, "note": "not in the list"}`, 0.1, nil
		}
	}, 5)

	run, err := r.StartTimeLookup(ctx, 1, found.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run = waitRun(t, r, run.ID); run.Status != "done" || run.Kind != "time" {
		t.Fatalf("lookup run = %+v", run)
	}
	got, _ := GetResult(ctx, d, 1, found.ID)
	if got.FinishSeconds == nil || *got.FinishSeconds != 6420 || got.TimeSource != "results_site" || got.TimeURL == "" {
		t.Fatalf("after lookup = %+v", got)
	}
	if !strings.Contains(prompts[0], `"Robin Smith"`) {
		t.Fatalf("prompt runner name: %s", prompts[0][:120])
	}

	// Batch: the remaining two (own + guest) are looked up; not found leaves them as they were.
	n, err := r.StartMissingTimeLookups(ctx, 1)
	if err != nil || n != 2 {
		t.Fatalf("batch queued %d, %v", n, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(prompts) < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if len(prompts) != 3 || !strings.Contains(strings.Join(prompts, "\n"), `"Khatiya Smith"`) {
		t.Fatalf("batch prompts = %d", len(prompts))
	}
	if g, _ := GetResult(ctx, d, 1, missing.ID); g.FinishSeconds != nil {
		t.Fatal("not-found lookup set a time")
	}
	_ = guest

	// Over budget: refused.
	broke := &Researcher{DB: d, Now: time.Now, Run: r.Run,
		Config: func(context.Context, *sql.DB) (*training.ClaudeConfig, ResearchSettings, error) {
			s := DefaultResearchSettings
			s.DailyBudgetUSD = 0
			return &training.ClaudeConfig{Enabled: true}, s, nil
		}}
	if _, err := broke.StartTimeLookup(ctx, 1, missing.ID); !errors.Is(err, ErrResearchBudget) {
		t.Fatalf("over budget: %v", err)
	}
}
