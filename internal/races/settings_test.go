package races

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

func TestResearchSettingsRoundTripAndValidation(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	s, err := LoadResearchSettings(ctx, d)
	if err != nil || s != DefaultResearchSettings {
		t.Fatalf("defaults = %+v, %v", s, err)
	}
	s.Model = "claude-haiku-4-5-20251001"
	s.DailyBudgetUSD = 1.5
	s.MonthlyBudgetUSD = 20
	s.NightlyMaxRaces = 0
	s.DiscoveryEnabled = false
	if err := SaveResearchSettings(ctx, d, s, 1); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := LoadResearchSettings(ctx, d)
	if got != s {
		t.Fatalf("loaded %+v, want %+v", got, s)
	}

	for name, bad := range map[string]ResearchSettings{
		"model":   {Model: "gpt-5", DailyBudgetUSD: 1, MonthlyBudgetUSD: 1, NightlyMaxRaces: 1},
		"daily":   {Model: DefaultResearchModel, DailyBudgetUSD: 101, MonthlyBudgetUSD: 1, NightlyMaxRaces: 1},
		"monthly": {Model: DefaultResearchModel, DailyBudgetUSD: 1, MonthlyBudgetUSD: -1, NightlyMaxRaces: 1},
		"nightly": {Model: DefaultResearchModel, DailyBudgetUSD: 1, MonthlyBudgetUSD: 1, NightlyMaxRaces: 61},
	} {
		if err := SaveResearchSettings(ctx, d, bad, 1); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v, want validation error", name, err)
		}
	}
}

func TestMonthlyCapAndMasterSwitchBlockResearch(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, _ := newRaceWithDeadline(t, d, DeadlineInput{Kind: "entry_closes", DueDate: "2099-04-01"})

	settings := DefaultResearchSettings
	r := &Researcher{DB: d, Now: time.Now,
		Run: func(context.Context, *training.ClaudeConfig, string) (string, float64, error) { return "{}", 0, nil },
		Config: func(context.Context, *sql.DB) (*training.ClaudeConfig, ResearchSettings, error) {
			return &training.ClaudeConfig{Enabled: true, CLIPath: "claude", Model: settings.Model}, settings, nil
		}}

	settings.Enabled = false
	if _, err := r.StartEventResearch(ctx, e.ID, 1, true); !errors.Is(err, ErrResearchDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	settings.Enabled = true
	settings.MonthlyBudgetUSD = 2
	// Spent earlier this month (but not today): only the monthly cap trips.
	earlier := osloDayStart(time.Now()).Add(-time.Minute)
	if earlier.Before(osloMonthStart(time.Now())) {
		earlier = osloMonthStart(time.Now()).Add(time.Minute)
	}
	if _, err := d.Exec(`INSERT INTO race_research_runs (kind, trigger, status, started_at, cost_usd)
		VALUES ('race', 'scheduled', 'done', ?, 2.5)`, ts(earlier)); err != nil {
		t.Fatal(err)
	}
	settings.DailyBudgetUSD = 100
	if _, err := r.StartEventResearch(ctx, e.ID, 1, true); !errors.Is(err, ErrResearchMonthly) && !errors.Is(err, ErrResearchBudget) {
		t.Fatalf("over monthly cap: %v", err)
	}
}

func TestComputeSpendStatsBucketsByOsloDay(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	loc := zoneOrUTC()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, loc)
	insert := func(at time.Time, kind string, cost float64, changes int) {
		if _, err := d.Exec(`INSERT INTO race_research_runs (kind, trigger, status, started_at, cost_usd, changes)
			VALUES (?, 'scheduled', 'done', ?, ?, ?)`, kind, ts(at), cost, changes); err != nil {
			t.Fatal(err)
		}
	}
	insert(time.Date(2026, 10, 20, 3, 31, 0, 0, loc), "race", 0.25, 2)    // today
	insert(time.Date(2026, 10, 20, 3, 40, 0, 0, loc), "race", 0.35, 0)    // today
	insert(time.Date(2026, 10, 15, 3, 31, 0, 0, loc), "discover", 1.0, 3) // within 7 days
	insert(time.Date(2026, 10, 2, 3, 31, 0, 0, loc), "race", 0.4, 1)      // this month, >7 days
	insert(time.Date(2026, 9, 25, 3, 31, 0, 0, loc), "race", 0.6, 0)      // last month, within 30 days
	insert(time.Date(2026, 8, 1, 3, 31, 0, 0, loc), "race", 5.0, 0)       // old: all-time only
	// 00:30 Oslo on the 20th is still the 19th in UTC; it must count as the 20th.
	insert(time.Date(2026, 10, 20, 0, 30, 0, 0, loc), "race", 0.1, 0)

	st, err := ComputeSpendStats(ctx, d, now)
	if err != nil {
		t.Fatal(err)
	}
	near := func(a, b float64) bool { return a > b-1e-9 && a < b+1e-9 }
	if !near(st.TodayUSD, 0.7) || !near(st.Last7DaysUSD, 1.7) || !near(st.MonthToDateUSD, 2.1) ||
		!near(st.Last30DaysUSD, 2.7) || !near(st.AllTimeUSD, 7.7) {
		t.Fatalf("totals = %+v", st)
	}
	if st.RaceChecks30d != 5 || st.Discoveries30d != 1 || st.Changes30d != 6 || !near(st.AvgRaceCheckUSD, 1.7/5) {
		t.Fatalf("counts = %+v", st)
	}
	if len(st.Daily) != 30 || st.Daily[29].Date != "2026-10-20" || st.Daily[29].Runs != 3 || !near(st.Daily[29].CostUSD, 0.7) {
		t.Fatalf("daily tail = %+v", st.Daily[len(st.Daily)-1])
	}
	if st.Daily[0].Date != "2026-09-21" {
		t.Fatalf("daily starts %s, want 2026-09-21", st.Daily[0].Date)
	}
}
