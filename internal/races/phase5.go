package races

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
)

// --- Watch history -----------------------------------------------------

// recordWatchChange logs a status change; called by SetWatch.
func recordWatchChange(ctx context.Context, db *sql.DB, userID, eventID int64, from, to string) {
	if from == to {
		return
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO race_watch_history (user_id, event_id, from_state, to_state, at) VALUES (?, ?, ?, ?, ?)`,
		userID, eventID, from, to, now()); err != nil {
		log.Printf("races: record watch change: %v", err)
	}
}

const historyBackfillKey = "races_watch_history_backfill"

// backfillWatchHistory gives watches that predate the history table one
// entry with their current state, so the ledger knows about them.
func backfillWatchHistory(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, historyBackfillKey).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO race_watch_history (user_id, event_id, from_state, to_state, at)
		SELECT w.user_id, w.event_id, '', w.state, COALESCE(NULLIF(w.updated_at, ''), w.created_at) FROM race_watch w
		WHERE NOT EXISTS (SELECT 1 FROM race_watch_history h WHERE h.user_id = w.user_id AND h.event_id = w.event_id)`); err != nil {
		return fmt.Errorf("backfill watch history: %w", err)
	}
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, historyBackfillKey)
	return err
}

// --- Lottery ledger ----------------------------------------------------

// LedgerEntry is one lottery the user entered.
type LedgerEntry struct {
	EventID     int64  `json:"event_id"`
	Name        string `json:"name"`
	EditionYear int    `json:"edition_year"`
	RaceDate    string `json:"race_date"`
	EnteredAt   string `json:"entered_at"`
	Outcome     string `json:"outcome"` // won | lost | pending
	DecidedAt   string `json:"decided_at"`
}

// Ledger is the user's lottery record.
type Ledger struct {
	Entries []LedgerEntry `json:"entries"`
	Entered int           `json:"entered"`
	Won     int           `json:"won"`
	Lost    int           `json:"lost"`
	Pending int           `json:"pending"`
	// Streaks: consecutive "not selected" for the same race across
	// editions, newest last (some lotteries favour repeat applicants).
	Streaks []LossStreak `json:"streaks"`
}

// LossStreak is a run of lost lotteries for one race.
type LossStreak struct {
	Race   string `json:"race"`
	Losses int    `json:"losses"`
}

var editionSuffix = regexp.MustCompile(`\s*\b(19|20)\d\d\b`)

// raceFamily is a race's name without its edition year, for streaks.
func raceFamily(name string) string {
	return strings.ToLower(strings.TrimSpace(editionSuffix.ReplaceAllString(name, "")))
}

// BuildLedger reads the watch history: a lottery counts as entered when
// the user set "in the lottery", won when they then got a place or
// registered, lost when they were not selected.
func BuildLedger(ctx context.Context, db *sql.DB, userID int64) (Ledger, error) {
	l := Ledger{Entries: []LedgerEntry{}, Streaks: []LossStreak{}}
	rows, err := db.QueryContext(ctx, `
		SELECT h.event_id, e.name, e.edition_year, e.race_date, h.to_state, h.at
		FROM race_watch_history h JOIN race_events e ON e.id = h.event_id
		WHERE h.user_id = ? ORDER BY h.event_id, h.at, h.id`, userID)
	if err != nil {
		return l, fmt.Errorf("ledger: %w", err)
	}
	defer rows.Close()
	byEvent := map[int64]*LedgerEntry{}
	var order []int64
	for rows.Next() {
		var id int64
		var name, date, state, at string
		var year int
		if err := rows.Scan(&id, &name, &year, &date, &state, &at); err != nil {
			return l, err
		}
		e := byEvent[id]
		switch state {
		case "lottery_entered":
			if e == nil {
				e = &LedgerEntry{EventID: id, Name: name, EditionYear: year, RaceDate: date}
				byEvent[id] = e
				order = append(order, id)
			}
			e.EnteredAt, e.Outcome, e.DecidedAt = at, "pending", ""
		case "got_place", "registered", "completed":
			if e != nil && e.Outcome == "pending" {
				e.Outcome, e.DecidedAt = "won", at
			}
		case "not_selected":
			if e != nil && e.Outcome == "pending" {
				e.Outcome, e.DecidedAt = "lost", at
			}
		}
	}
	if err := rows.Err(); err != nil {
		return l, err
	}
	for _, id := range order {
		e := *byEvent[id]
		l.Entries = append(l.Entries, e)
		l.Entered++
		switch e.Outcome {
		case "won":
			l.Won++
		case "lost":
			l.Lost++
		default:
			l.Pending++
		}
	}
	sort.SliceStable(l.Entries, func(i, j int) bool { return l.Entries[i].RaceDate > l.Entries[j].RaceDate })

	// Losing streaks per race, oldest edition first.
	byRace := map[string][]LedgerEntry{}
	var races []string
	for _, e := range l.Entries {
		k := raceFamily(e.Name)
		if _, ok := byRace[k]; !ok {
			races = append(races, k)
		}
		byRace[k] = append(byRace[k], e)
	}
	for _, k := range races {
		list := byRace[k]
		sort.Slice(list, func(i, j int) bool { return list[i].RaceDate < list[j].RaceDate })
		streak := 0
		for _, e := range list {
			switch e.Outcome {
			case "lost":
				streak++
			case "won":
				streak = 0
			}
		}
		if streak > 0 {
			l.Streaks = append(l.Streaks, LossStreak{Race: list[len(list)-1].Name, Losses: streak})
		}
	}
	return l, nil
}

// --- Series progress ---------------------------------------------------

// SeriesRace is one race of a series.
type SeriesRace struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// SeriesDef describes a series and how many races complete it.
type SeriesDef struct {
	Key      string       `json:"key"`
	Required int          `json:"required"`
	Races    []SeriesRace `json:"races"`
}

// Series are the race series the catalog tracks progress for.
var Series = []SeriesDef{
	{Key: "majors", Required: 7, Races: []SeriesRace{
		{"tokyo", "Tokyo"}, {"boston", "Boston"}, {"london", "London"}, {"sydney", "Sydney"},
		{"berlin", "Berlin"}, {"chicago", "Chicago"}, {"nyc", "New York"},
	}},
	{Key: "emc", Required: 5, Races: []SeriesRace{
		{"rome", "Rome"}, {"vienna", "Vienna"}, {"london", "London"}, {"madrid", "Madrid"},
		{"copenhagen", "Copenhagen"}, {"warsaw", "Warsaw"}, {"lisbon", "Lisbon"}, {"frankfurt", "Frankfurt"},
	}},
}

// seriesSlugKeys maps a catalog slug (without its year) to a series race.
var seriesSlugKeys = map[string]string{
	"tokyo-marathon": "tokyo", "boston-marathon": "boston", "tcs-london-marathon": "london",
	"tcs-sydney-marathon": "sydney", "bmw-berlin-marathon": "berlin", "bank-of-america-chicago-marathon": "chicago",
	"tcs-new-york-city-marathon": "nyc", "run-rome-the-marathon": "rome", "vienna-city-marathon": "vienna",
	"zurich-rock-n-roll-madrid-marathon": "madrid", "copenhagen-marathon": "copenhagen", "warsaw-marathon": "warsaw",
	"edp-lisbon-marathon": "lisbon", "mainova-frankfurt-marathon": "frankfurt",
}

var slugYear = regexp.MustCompile(`-(19|20)\d\d(-\d+)?$`)

// SeriesKeyForSlug returns the series race a catalog race is an edition
// of, or "" (half marathons and other races never match).
func SeriesKeyForSlug(slug string) string {
	return seriesSlugKeys[slugYear.ReplaceAllString(slug, "")]
}

func validSeriesKey(key string) bool {
	for _, s := range Series {
		for _, r := range s.Races {
			if r.Key == key {
				return true
			}
		}
	}
	return false
}

// Finish is one finished series race.
type Finish struct {
	RaceKey       string `json:"race_key"`
	Year          int    `json:"year"`
	FinishSeconds *int   `json:"finish_seconds"`
	Source        string `json:"source"`
}

// FinishInput is the body of POST /api/races/series/finishes.
type FinishInput struct {
	RaceKey       string `json:"race_key"`
	Year          int    `json:"year"`
	FinishSeconds *int   `json:"finish_seconds"`
}

// SaveFinish stores (or replaces) a finished series race.
func SaveFinish(ctx context.Context, db *sql.DB, userID int64, in FinishInput, source string) error {
	if !validSeriesKey(in.RaceKey) {
		return invalid("unknown series race %q", in.RaceKey)
	}
	if in.Year < 1897 || in.Year > 2100 {
		return invalid("year must be between 1897 and 2100")
	}
	if in.FinishSeconds != nil && (*in.FinishSeconds <= 0 || *in.FinishSeconds > maxTargetSeconds) {
		return invalid("finish_seconds must be a positive number of seconds")
	}
	_, err := db.ExecContext(ctx, `INSERT INTO race_series_finishes (user_id, race_key, year, finish_seconds, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(user_id, race_key, year) DO UPDATE SET
		finish_seconds = COALESCE(excluded.finish_seconds, race_series_finishes.finish_seconds), source = excluded.source`,
		userID, in.RaceKey, in.Year, in.FinishSeconds, source, now())
	return err
}

// DeleteFinish removes a finished series race.
func DeleteFinish(ctx context.Context, db *sql.DB, userID int64, raceKey string, year int) error {
	res, err := db.ExecContext(ctx, `DELETE FROM race_series_finishes WHERE user_id = ? AND race_key = ? AND year = ?`, userID, raceKey, year)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// autoFinish records a series finish when a watch is marked completed.
func autoFinish(ctx context.Context, db *sql.DB, userID, eventID int64) {
	var slug string
	var year int
	if err := db.QueryRowContext(ctx, `SELECT slug, edition_year FROM race_events WHERE id = ?`, eventID).Scan(&slug, &year); err != nil {
		return
	}
	key := SeriesKeyForSlug(slug)
	if key == "" {
		return
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO race_series_finishes (user_id, race_key, year, source, created_at)
		VALUES (?, ?, ?, 'auto', ?)`, userID, key, year, now()); err != nil {
		log.Printf("races: auto series finish: %v", err)
	}
}

// SeriesRaceProgress is one race's state within a series for a user.
type SeriesRaceProgress struct {
	SeriesRace
	Finishes []Finish `json:"finishes"`
	// NextEventID is the next upcoming catalog edition, if any.
	NextEventID *int64 `json:"next_event_id"`
}

// SeriesProgress is a user's progress through one series.
type SeriesProgress struct {
	Key      string               `json:"key"`
	Required int                  `json:"required"`
	Done     int                  `json:"done"`
	Races    []SeriesRaceProgress `json:"races"`
}

// BuildSeriesProgress returns progress for every series.
func BuildSeriesProgress(ctx context.Context, db *sql.DB, userID int64, today string) ([]SeriesProgress, error) {
	rows, err := db.QueryContext(ctx, `SELECT race_key, year, finish_seconds, source FROM race_series_finishes
		WHERE user_id = ? ORDER BY year`, userID)
	if err != nil {
		return nil, fmt.Errorf("series finishes: %w", err)
	}
	finishes := map[string][]Finish{}
	for rows.Next() {
		var f Finish
		var secs sql.NullInt64
		if err := rows.Scan(&f.RaceKey, &f.Year, &secs, &f.Source); err != nil {
			rows.Close()
			return nil, err
		}
		if secs.Valid {
			s := int(secs.Int64)
			f.FinishSeconds = &s
		}
		finishes[f.RaceKey] = append(finishes[f.RaceKey], f)
	}
	rows.Close()

	next := map[string]int64{}
	erows, err := db.QueryContext(ctx, `SELECT id, slug FROM race_events WHERE race_date >= ? ORDER BY race_date`, today)
	if err != nil {
		return nil, err
	}
	for erows.Next() {
		var id int64
		var slug string
		if err := erows.Scan(&id, &slug); err != nil {
			erows.Close()
			return nil, err
		}
		if key := SeriesKeyForSlug(slug); key != "" {
			if _, seen := next[key]; !seen {
				next[key] = id
			}
		}
	}
	erows.Close()

	var out []SeriesProgress
	for _, s := range Series {
		p := SeriesProgress{Key: s.Key, Required: s.Required}
		for _, r := range s.Races {
			rp := SeriesRaceProgress{SeriesRace: r, Finishes: finishes[r.Key]}
			if rp.Finishes == nil {
				rp.Finishes = []Finish{}
			}
			if id, ok := next[r.Key]; ok {
				id := id
				rp.NextEventID = &id
			}
			if len(rp.Finishes) > 0 {
				p.Done++
			}
			p.Races = append(p.Races, rp)
		}
		out = append(out, p)
	}
	return out, nil
}

// --- Family view -------------------------------------------------------

// PrefShareRaces: a user's race statuses are shown to the others on this
// Hytte unless set to "false". Notes are never shared.
const PrefShareRaces = "races_share"

// FamilyWatch is another user's status on a race.
type FamilyWatch struct {
	UserID  int64  `json:"user_id"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	State   string `json:"state"`
}

// FamilyWatches returns, per race, the statuses of the other users who
// have the races feature and share their plans.
func FamilyWatches(ctx context.Context, db *sql.DB, viewerID int64) (map[int64][]FamilyWatch, error) {
	featureCond := `EXISTS (SELECT 1 FROM user_features f WHERE f.user_id = u.id AND f.feature_key = 'races' AND f.enabled = 1)`
	if auth.FeatureDefaults["races"] {
		featureCond = `NOT EXISTS (SELECT 1 FROM user_features f WHERE f.user_id = u.id AND f.feature_key = 'races' AND f.enabled = 0)`
	}
	rows, err := db.QueryContext(ctx, `
		SELECT w.event_id, u.id, u.name, u.picture, w.state
		FROM race_watch w JOIN users u ON u.id = w.user_id
		WHERE w.user_id != ? AND w.state != 'skipped'
		  AND (u.is_admin = 1 OR `+featureCond+`)
		  AND NOT EXISTS (SELECT 1 FROM user_preferences p WHERE p.user_id = u.id AND p.key = ? AND p.value = 'false')
		ORDER BY u.name`, viewerID, PrefShareRaces)
	if err != nil {
		return nil, fmt.Errorf("family watches: %w", err)
	}
	defer rows.Close()
	out := map[int64][]FamilyWatch{}
	for rows.Next() {
		var eventID int64
		var fw FamilyWatch
		if err := rows.Scan(&eventID, &fw.UserID, &fw.Name, &fw.Picture, &fw.State); err != nil {
			return nil, err
		}
		if first := strings.Fields(fw.Name); len(first) > 0 {
			fw.Name = first[0]
		}
		out[eventID] = append(out[eventID], fw)
	}
	return out, rows.Err()
}
