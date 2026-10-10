package races

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var airportPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// ResearchSettings are the app-wide limits for automatic research. They live
// in race_settings (not a user's preferences) because the job is shared.
type ResearchSettings struct {
	Enabled          bool    `json:"enabled"`
	DiscoveryEnabled bool    `json:"discovery_enabled"`
	Model            string  `json:"model"`
	DailyBudgetUSD   float64 `json:"daily_budget_usd"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	NightlyMaxRaces  int     `json:"nightly_max_races"`
	// HomeCity / HomeAirport: where the family travels from. Travel texts and
	// the direct-flight badge are written for this airport.
	HomeCity    string `json:"home_city"`
	HomeAirport string `json:"home_airport"`
	// Local races: imported daily from Kondis within LocalRadiusKM of home
	// (HomeLat/HomeLng). Parkrun runs every week, so it is left out unless
	// IncludeParkrun is on.
	HomeLat          float64 `json:"home_lat"`
	HomeLng          float64 `json:"home_lng"`
	LocalSyncEnabled bool    `json:"local_sync_enabled"`
	LocalRadiusKM    int     `json:"local_radius_km"`
	IncludeParkrun   bool    `json:"include_parkrun"`
}

// ResearchModels are the models the settings accept, cheapest first.
var ResearchModels = []string{"claude-haiku-4-5-20251001", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1"}

// DefaultResearchSettings apply until an admin saves their own.
var DefaultResearchSettings = ResearchSettings{
	Enabled:          true,
	DiscoveryEnabled: true,
	Model:            DefaultResearchModel,
	DailyBudgetUSD:   defaultDailyBudget,
	MonthlyBudgetUSD: 60,
	NightlyMaxRaces:  nightlyMaxRaces,
	HomeCity:         "Bergen",
	HomeAirport:      "BGO",
	HomeLat:          60.3913,
	HomeLng:          5.3221,
	LocalSyncEnabled: true,
	LocalRadiusKM:    50,
}

const (
	maxDailyBudget   = 100
	maxMonthlyBudget = 1000
	maxNightlyRaces  = 60
	minLocalRadius   = 5
	maxLocalRadius   = 300
)

// Validate checks settings sent by the admin.
func (s ResearchSettings) Validate() error {
	known := false
	for _, m := range ResearchModels {
		if s.Model == m {
			known = true
		}
	}
	switch {
	case !known:
		return invalid("unknown model %q", s.Model)
	case s.DailyBudgetUSD < 0 || s.DailyBudgetUSD > maxDailyBudget:
		return invalid("daily budget must be between 0 and %d USD", maxDailyBudget)
	case s.MonthlyBudgetUSD < 0 || s.MonthlyBudgetUSD > maxMonthlyBudget:
		return invalid("monthly budget must be between 0 and %d USD", maxMonthlyBudget)
	case s.NightlyMaxRaces < 0 || s.NightlyMaxRaces > maxNightlyRaces:
		return invalid("races per night must be between 0 and %d", maxNightlyRaces)
	case strings.TrimSpace(s.HomeCity) == "" || len([]rune(s.HomeCity)) > 60:
		return invalid("home city is required (max 60 characters)")
	case !airportPattern.MatchString(s.HomeAirport):
		return invalid("home airport must be a three-letter IATA code")
	case s.HomeLat < -90 || s.HomeLat > 90 || s.HomeLng < -180 || s.HomeLng > 180:
		return invalid("home position must be a valid latitude and longitude")
	case s.LocalRadiusKM < minLocalRadius || s.LocalRadiusKM > maxLocalRadius:
		return invalid("local radius must be between %d and %d km", minLocalRadius, maxLocalRadius)
	}
	return nil
}

// LoadResearchSettings reads the stored settings over the defaults.
func LoadResearchSettings(ctx context.Context, db *sql.DB) (ResearchSettings, error) {
	s := DefaultResearchSettings
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM race_settings`)
	if err != nil {
		return s, fmt.Errorf("load race settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return s, err
		}
		switch k {
		case "research_enabled":
			s.Enabled = v == "true"
		case "discovery_enabled":
			s.DiscoveryEnabled = v == "true"
		case "research_model":
			s.Model = v
		case "daily_budget_usd":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				s.DailyBudgetUSD = f
			}
		case "monthly_budget_usd":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				s.MonthlyBudgetUSD = f
			}
		case "nightly_max_races":
			if n, err := strconv.Atoi(v); err == nil {
				s.NightlyMaxRaces = n
			}
		case "home_city":
			s.HomeCity = v
		case "home_airport":
			s.HomeAirport = v
		case "home_lat":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				s.HomeLat = f
			}
		case "home_lng":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				s.HomeLng = f
			}
		case "local_sync_enabled":
			s.LocalSyncEnabled = v == "true"
		case "local_radius_km":
			if n, err := strconv.Atoi(v); err == nil {
				s.LocalRadiusKM = n
			}
		case "include_parkrun":
			s.IncludeParkrun = v == "true"
		}
	}
	return s, rows.Err()
}

// SaveResearchSettings validates and stores the settings.
func SaveResearchSettings(ctx context.Context, db *sql.DB, s ResearchSettings, userID int64) error {
	s.HomeCity = strings.TrimSpace(s.HomeCity)
	s.HomeAirport = strings.ToUpper(strings.TrimSpace(s.HomeAirport))
	if err := s.Validate(); err != nil {
		return err
	}
	values := map[string]string{
		"research_enabled":   strconv.FormatBool(s.Enabled),
		"discovery_enabled":  strconv.FormatBool(s.DiscoveryEnabled),
		"research_model":     s.Model,
		"daily_budget_usd":   strconv.FormatFloat(s.DailyBudgetUSD, 'f', -1, 64),
		"monthly_budget_usd": strconv.FormatFloat(s.MonthlyBudgetUSD, 'f', -1, 64),
		"nightly_max_races":  strconv.Itoa(s.NightlyMaxRaces),
		"home_city":          s.HomeCity,
		"home_airport":       s.HomeAirport,
		"home_lat":           strconv.FormatFloat(s.HomeLat, 'f', -1, 64),
		"home_lng":           strconv.FormatFloat(s.HomeLng, 'f', -1, 64),
		"local_sync_enabled": strconv.FormatBool(s.LocalSyncEnabled),
		"local_radius_km":    strconv.Itoa(s.LocalRadiusKM),
		"include_parkrun":    strconv.FormatBool(s.IncludeParkrun),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO race_settings (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at,
				updated_by = excluded.updated_by`, k, v, now(), nullableUser(userID)); err != nil {
			return fmt.Errorf("save race setting %s: %w", k, err)
		}
	}
	return tx.Commit()
}

// SpendDay is one Oslo calendar day of research spend.
type SpendDay struct {
	Date    string  `json:"date"`
	CostUSD float64 `json:"cost_usd"`
	Runs    int     `json:"runs"`
}

// SpendStats summarizes research spend for the admin view.
type SpendStats struct {
	TodayUSD        float64    `json:"today_usd"`
	Last7DaysUSD    float64    `json:"last_7_days_usd"`
	MonthToDateUSD  float64    `json:"month_to_date_usd"`
	Last30DaysUSD   float64    `json:"last_30_days_usd"`
	AllTimeUSD      float64    `json:"all_time_usd"`
	RaceChecks30d   int        `json:"race_checks_30d"`
	Discoveries30d  int        `json:"discoveries_30d"`
	AvgRaceCheckUSD float64    `json:"avg_race_check_usd"`
	Changes30d      int        `json:"changes_30d"`
	Daily           []SpendDay `json:"daily"` // last 30 days, oldest first, every day present
}

func osloMonthStart(now time.Time) time.Time {
	d := osloDayStart(now)
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
}

// ComputeSpendStats aggregates race_research_runs by Oslo day.
func ComputeSpendStats(ctx context.Context, db *sql.DB, now time.Time) (SpendStats, error) {
	var st SpendStats
	today := osloDayStart(now)
	since := today.AddDate(0, 0, -29)

	rows, err := db.QueryContext(ctx, `SELECT kind, status, started_at, cost_usd, changes FROM race_research_runs WHERE started_at >= ?`,
		ts(minTime(since, osloMonthStart(now))))
	if err != nil {
		return st, fmt.Errorf("spend stats: %w", err)
	}
	defer rows.Close()

	byDay := map[string]*SpendDay{}
	for d := since; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		byDay[key] = &SpendDay{Date: key}
		st.Daily = append(st.Daily, SpendDay{Date: key})
	}
	raceCost := 0.0
	monthStart := osloMonthStart(now)
	for rows.Next() {
		var kind, status, started string
		var cost float64
		var changes int
		if err := rows.Scan(&kind, &status, &started, &cost, &changes); err != nil {
			return st, err
		}
		t, err := time.Parse(time.RFC3339, started)
		if err != nil {
			continue
		}
		local := t.In(today.Location())
		if !local.Before(monthStart) {
			st.MonthToDateUSD += cost
		}
		if local.Before(since) {
			continue
		}
		st.Last30DaysUSD += cost
		st.Changes30d += changes
		if !local.Before(today) {
			st.TodayUSD += cost
		}
		if !local.Before(today.AddDate(0, 0, -6)) {
			st.Last7DaysUSD += cost
		}
		if kind == "discover" {
			st.Discoveries30d++
		} else if status != "running" {
			st.RaceChecks30d++
			raceCost += cost
		}
		if day := byDay[local.Format("2006-01-02")]; day != nil {
			day.CostUSD += cost
			day.Runs++
		}
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	for i := range st.Daily {
		st.Daily[i] = *byDay[st.Daily[i].Date]
	}
	if st.RaceChecks30d > 0 {
		st.AvgRaceCheckUSD = raceCost / float64(st.RaceChecks30d)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM race_research_runs`).Scan(&st.AllTimeUSD); err != nil {
		return st, err
	}
	return st, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
