package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

// Hall of fame: every race a user (or a family member without an account)
// has run. Results come from the catalog (a watch marked completed), manual
// entry, or imports (the Gmail backfill) that wait as "pending" until the
// user confirms them. Personal data: evidence and notes are encrypted.

var (
	validResultStatus = set("pending", "confirmed")
	validConfidence   = set("high", "medium", "low", "")
	validTimeSource   = set("", "email", "strava", "workout", "results_site", "manual", "catalog")
)

// Result is one run race.
type Result struct {
	ID            int64      `json:"id"`
	PersonName    string     `json:"person_name"` // "" = the user themselves
	EventID       *int64     `json:"event_id"`
	RaceName      string     `json:"race_name"`
	RaceDate      string     `json:"race_date"`
	DateExact     bool       `json:"date_exact"`
	DistanceM     int        `json:"distance_m"`
	City          string     `json:"city"`
	Country       string     `json:"country"`
	FinishSeconds *int       `json:"finish_seconds"`
	TimeSource    string     `json:"time_source"`
	TimeURL       string     `json:"time_url"`
	Bib           string     `json:"bib"`
	Status        string     `json:"status"`
	Confidence    string     `json:"confidence"`
	Source        string     `json:"source"`
	Evidence      []Evidence `json:"evidence"`
	Notes         string     `json:"notes"`
	SeriesKey     string     `json:"series_key"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// Evidence points at where a result came from (an email thread, a page).
type Evidence struct {
	ThreadID string `json:"thread_id,omitempty"`
	URL      string `json:"url,omitempty"`
	Date     string `json:"date,omitempty"`
	From     string `json:"from,omitempty"`
	What     string `json:"what,omitempty"`
}

// ResultInput is the editable part of a result.
type ResultInput struct {
	PersonName    string     `json:"person_name"`
	EventID       *int64     `json:"event_id"`
	RaceName      string     `json:"race_name"`
	RaceDate      string     `json:"race_date"`
	DateExact     *bool      `json:"date_exact"`
	DistanceM     int        `json:"distance_m"`
	City          string     `json:"city"`
	Country       string     `json:"country"`
	FinishSeconds *int       `json:"finish_seconds"`
	TimeSource    string     `json:"time_source"`
	TimeURL       string     `json:"time_url"`
	Bib           string     `json:"bib"`
	Status        string     `json:"status"`
	Confidence    string     `json:"confidence"`
	Evidence      []Evidence `json:"evidence"`
	Notes         string     `json:"notes"`
}

func (in *ResultInput) normalize() error {
	in.PersonName = strings.TrimSpace(in.PersonName)
	in.RaceName = strings.TrimSpace(in.RaceName)
	in.City = strings.TrimSpace(in.City)
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	in.Bib = strings.TrimSpace(in.Bib)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Status == "" {
		in.Status = "confirmed"
	}
	switch {
	case in.RaceName == "" || len([]rune(in.RaceName)) > maxNameLen:
		return invalid("race_name is required (max %d characters)", maxNameLen)
	case len([]rune(in.PersonName)) > 80:
		return invalid("person_name too long")
	case !validDate(in.RaceDate):
		return invalid("race_date must be YYYY-MM-DD")
	case in.DistanceM <= 0 || in.DistanceM > 500_000:
		return invalid("distance_m must be between 1 and 500000")
	case in.Country != "" && !countryPattern.MatchString(in.Country):
		return invalid("country must be a two-letter code")
	case in.FinishSeconds != nil && (*in.FinishSeconds <= 0 || *in.FinishSeconds > 48*3600):
		return invalid("finish_seconds must be a positive number of seconds")
	case !validTimeSource[in.TimeSource]:
		return invalid("unknown time_source %q", in.TimeSource)
	case !validResultStatus[in.Status]:
		return invalid("unknown status %q", in.Status)
	case !validConfidence[in.Confidence]:
		return invalid("unknown confidence %q", in.Confidence)
	case len([]rune(in.Notes)) > maxNotesLen:
		return invalid("notes too long")
	}
	if in.FinishSeconds != nil && in.TimeSource == "" {
		in.TimeSource = "manual"
	}
	return nil
}

// --- series keys for results -------------------------------------------

func fold(s string) string {
	return slugReplacer.Replace(strings.ToLower(strings.TrimSpace(s)))
}

// cityKeys maps a (folded) city name to the series race key for marathons
// and for halves (SuperHalfs).
var marathonCityKeys = map[string]string{
	"tokyo": "tokyo", "boston": "boston", "london": "london", "sydney": "sydney", "berlin": "berlin",
	"chicago": "chicago", "new york": "nyc", "new york city": "nyc", "roma": "rome", "rome": "rome",
	"wien": "vienna", "vienna": "vienna", "madrid": "madrid", "kobenhavn": "copenhagen", "copenhagen": "copenhagen",
	"warszawa": "warsaw", "warsaw": "warsaw", "lisboa": "lisbon", "lisbon": "lisbon", "frankfurt": "frankfurt",
}

var halfCityKeys = map[string]string{
	"lisboa": "lisbon_half", "lisbon": "lisbon_half", "praha": "prague_half", "prague": "prague_half",
	"kobenhavn": "copenhagen_half", "copenhagen": "copenhagen_half", "cardiff": "cardiff_half",
	"berlin": "berlin_half", "valencia": "valencia_half",
}

// resultSeriesKey works out which series race a result is, from its
// catalog race when linked, else from distance + city.
func resultSeriesKey(slug, city string, distance int) string {
	if slug != "" {
		if k := SeriesKeyForSlug(slug); k != "" {
			return k
		}
	}
	c := fold(city)
	switch {
	case math.Abs(float64(distance-42195)) < 400:
		return marathonCityKeys[c]
	case math.Abs(float64(distance-21097)) < 300:
		return halfCityKeys[c]
	}
	return ""
}

// --- store --------------------------------------------------------------

const resultColumns = `r.id, r.person_name, r.event_id, r.race_name, r.race_date, r.date_exact, r.distance_m, r.city, r.country,
	r.finish_seconds, r.time_source, r.time_url, r.bib, r.status, r.confidence, r.source, r.evidence, r.notes,
	COALESCE(e.slug, ''), r.created_at, r.updated_at`

func scanResult(row scanner) (Result, error) {
	var res Result
	var eventID sql.NullInt64
	var finish sql.NullInt64
	var evidence, notes, slug string
	if err := row.Scan(&res.ID, &res.PersonName, &eventID, &res.RaceName, &res.RaceDate, &res.DateExact, &res.DistanceM,
		&res.City, &res.Country, &finish, &res.TimeSource, &res.TimeURL, &res.Bib, &res.Status, &res.Confidence, &res.Source,
		&evidence, &notes, &slug, &res.CreatedAt, &res.UpdatedAt); err != nil {
		return res, err
	}
	if eventID.Valid {
		res.EventID = &eventID.Int64
	}
	if finish.Valid {
		f := int(finish.Int64)
		res.FinishSeconds = &f
	}
	res.Evidence = []Evidence{}
	if dec := encryption.DecryptLenient(evidence); dec != "" {
		_ = json.Unmarshal([]byte(dec), &res.Evidence)
	}
	res.Notes = encryption.DecryptLenient(notes)
	res.SeriesKey = resultSeriesKey(slug, res.City, res.DistanceM)
	return res, nil
}

func encryptJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return encryption.EncryptField(string(b))
}

func encryptText(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return encryption.EncryptField(s)
}

// ListResults returns the user's results (theirs and their guests'), newest first.
func ListResults(ctx context.Context, db *sql.DB, userID int64) ([]Result, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+resultColumns+` FROM race_results r
		LEFT JOIN race_events e ON e.id = r.event_id WHERE r.user_id = ? ORDER BY r.race_date DESC, r.id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list results: %w", err)
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		res, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// GetResult returns one of the user's results.
func GetResult(ctx context.Context, db *sql.DB, userID, id int64) (*Result, error) {
	res, err := scanResult(db.QueryRowContext(ctx, `SELECT `+resultColumns+` FROM race_results r
		LEFT JOIN race_events e ON e.id = r.event_id WHERE r.user_id = ? AND r.id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// SaveResult creates (id 0) or replaces a result. It returns ErrDuplicate
// when the user already has a result for that person, day and distance.
func SaveResult(ctx context.Context, db *sql.DB, userID, id int64, in ResultInput, source string) (*Result, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	evidence, err := encryptJSON(in.Evidence)
	if err != nil {
		return nil, fmt.Errorf("encrypt evidence: %w", err)
	}
	notes, err := encryptText(in.Notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt notes: %w", err)
	}
	exact := true
	if in.DateExact != nil {
		exact = *in.DateExact
	}
	ts := now()
	if id == 0 {
		var dup int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_results WHERE user_id = ? AND person_name = ? AND race_date = ?
			AND ABS(distance_m - ?) < 500`, userID, in.PersonName, in.RaceDate, in.DistanceM).Scan(&dup); err != nil {
			return nil, err
		}
		if dup > 0 {
			return nil, ErrDuplicate
		}
		resIns, err := db.ExecContext(ctx, `INSERT INTO race_results (user_id, person_name, event_id, race_name, race_date, date_exact,
			distance_m, city, country, finish_seconds, time_source, time_url, bib, status, confidence, source, evidence, notes,
			created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, in.PersonName, in.EventID, in.RaceName, in.RaceDate, exact, in.DistanceM, in.City, in.Country,
			in.FinishSeconds, in.TimeSource, in.TimeURL, in.Bib, in.Status, in.Confidence, source, evidence, notes, ts, ts)
		if err != nil {
			return nil, fmt.Errorf("insert result: %w", err)
		}
		id, _ = resIns.LastInsertId()
	} else {
		res, err := db.ExecContext(ctx, `UPDATE race_results SET person_name = ?, event_id = ?, race_name = ?, race_date = ?,
			date_exact = ?, distance_m = ?, city = ?, country = ?, finish_seconds = ?, time_source = ?, time_url = ?, bib = ?,
			status = ?, confidence = ?, evidence = ?, notes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
			in.PersonName, in.EventID, in.RaceName, in.RaceDate, exact, in.DistanceM, in.City, in.Country, in.FinishSeconds,
			in.TimeSource, in.TimeURL, in.Bib, in.Status, in.Confidence, evidence, notes, ts, id, userID)
		if err != nil {
			return nil, fmt.Errorf("update result: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	matchWorkoutTimes(ctx, db, userID)
	return GetResult(ctx, db, userID, id)
}

// ErrDuplicate: the user already has this result.
var ErrDuplicate = errors.New("a result for that day and distance already exists")

// DeleteResult removes a result.
func DeleteResult(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM race_results WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ConfirmResults marks pending results confirmed: the given ids, or with
// minConfidence "high", every high-confidence pending result.
func ConfirmResults(ctx context.Context, db *sql.DB, userID int64, ids []int64, allHigh bool) (int, error) {
	var res sql.Result
	var err error
	if allHigh {
		res, err = db.ExecContext(ctx, `UPDATE race_results SET status = 'confirmed', updated_at = ? WHERE user_id = ?
			AND status = 'pending' AND confidence = 'high'`, now(), userID)
	} else {
		if len(ids) == 0 {
			return 0, nil
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := []any{now(), userID}
		for _, id := range ids {
			args = append(args, id)
		}
		res, err = db.ExecContext(ctx, `UPDATE race_results SET status = 'confirmed', updated_at = ? WHERE user_id = ?
			AND status = 'pending' AND id IN (`+ph+`)`, args...)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// resultFromCompletedWatch records a confirmed result when a catalog race
// is marked completed (called by SetWatch).
func resultFromCompletedWatch(ctx context.Context, db *sql.DB, userID, eventID int64) {
	e, err := GetEvent(ctx, db, eventID)
	if err != nil {
		return
	}
	city := ""
	if t, ok := e.Texts["en"]; ok {
		city = strings.TrimSpace(strings.Split(t.Place, ",")[0])
	}
	id := eventID
	_, err = SaveResult(ctx, db, userID, 0, ResultInput{EventID: &id, RaceName: e.Name, RaceDate: e.RaceDate, DistanceM: e.DistanceM,
		City: city, Country: e.Country, Status: "confirmed", Confidence: "high"}, "catalog")
	if err != nil && !errors.Is(err, ErrDuplicate) {
		log.Printf("races: result from completed watch: %v", err)
	}
}

// matchWorkoutTimes fills missing times of the user's own results from a
// running workout on the same day within 4% of the distance.
func matchWorkoutTimes(ctx context.Context, db *sql.DB, userID int64) {
	rows, err := db.QueryContext(ctx, `SELECT id, race_date, distance_m FROM race_results
		WHERE user_id = ? AND person_name = '' AND finish_seconds IS NULL`, userID)
	if err != nil {
		return
	}
	type miss struct {
		id   int64
		date string
		dist int
	}
	var misses []miss
	for rows.Next() {
		var m miss
		if rows.Scan(&m.id, &m.date, &m.dist) == nil {
			misses = append(misses, m)
		}
	}
	rows.Close()
	for _, m := range misses {
		var dur int
		err := db.QueryRowContext(ctx, `SELECT duration_seconds FROM workouts WHERE user_id = ? AND sport = 'running'
			AND substr(started_at, 1, 10) = ? AND ABS(distance_meters - ?) <= ? * 0.04 AND duration_seconds > 0
			ORDER BY ABS(distance_meters - ?) LIMIT 1`, userID, m.date, m.dist, m.dist, m.dist).Scan(&dur)
		if err != nil {
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE race_results SET finish_seconds = ?, time_source = 'workout', updated_at = ?
			WHERE id = ? AND finish_seconds IS NULL`, dur, now(), m.id); err != nil {
			log.Printf("races: workout time for result %d: %v", m.id, err)
		}
	}
}

// --- hall of fame summary ----------------------------------------------

// StandardDistances are the distances personal bests are kept for.
var StandardDistances = []struct {
	Key    string
	Meters int
	Tol    int
}{
	{"3k", 3000, 60}, {"5k", 5000, 100}, {"10k", 10000, 200}, {"half", 21097, 300}, {"marathon", 42195, 400},
}

func standardKey(distance int) string {
	for _, d := range StandardDistances {
		if int(math.Abs(float64(distance-d.Meters))) <= d.Tol {
			return d.Key
		}
	}
	return ""
}

// PB is a personal best at one standard distance.
type PB struct {
	Distance      string `json:"distance"`
	ResultID      int64  `json:"result_id"`
	FinishSeconds int    `json:"finish_seconds"`
	RaceName      string `json:"race_name"`
	RaceDate      string `json:"race_date"`
	Count         int    `json:"count"` // races at this distance
}

// HallOfFame is the user's summary.
type HallOfFame struct {
	PBs       []PB           `json:"pbs"`
	Races     int            `json:"races"`
	Countries []string       `json:"countries"`
	PerYear   map[string]int `json:"per_year"`
	FirstYear int            `json:"first_year"`
	Pending   int            `json:"pending"`
}

// Summarize computes PBs and counts over the user's own confirmed results.
func Summarize(results []Result) HallOfFame {
	h := HallOfFame{PBs: []PB{}, Countries: []string{}, PerYear: map[string]int{}}
	best := map[string]*PB{}
	countries := map[string]bool{}
	for _, r := range results {
		if r.Status == "pending" {
			if r.PersonName == "" {
				h.Pending++
			}
			continue
		}
		if r.PersonName != "" {
			continue
		}
		h.Races++
		year := r.RaceDate[:4]
		h.PerYear[year]++
		if y, _ := strconv.Atoi(year); h.FirstYear == 0 || y < h.FirstYear {
			h.FirstYear = y
		}
		if r.Country != "" {
			countries[r.Country] = true
		}
		k := standardKey(r.DistanceM)
		if k == "" {
			continue
		}
		pb := best[k]
		if pb == nil {
			pb = &PB{Distance: k}
			best[k] = pb
		}
		pb.Count++
		if r.FinishSeconds != nil && (pb.FinishSeconds == 0 || *r.FinishSeconds < pb.FinishSeconds) {
			pb.FinishSeconds, pb.ResultID, pb.RaceName, pb.RaceDate = *r.FinishSeconds, r.ID, r.RaceName, r.RaceDate
		}
	}
	for _, d := range StandardDistances {
		if pb := best[d.Key]; pb != nil {
			h.PBs = append(h.PBs, *pb)
		}
	}
	for c := range countries {
		h.Countries = append(h.Countries, c)
	}
	sort.Strings(h.Countries)
	return h
}

// --- import -------------------------------------------------------------

// ImportCandidate is one race from an import file (the Gmail backfill).
type ImportCandidate struct {
	Person        string     `json:"person"`
	RaceName      string     `json:"race_name"`
	Date          string     `json:"date"`
	DateExact     *bool      `json:"date_exact"`
	DistanceM     int        `json:"distance_m"`
	City          string     `json:"city"`
	Country       string     `json:"country"`
	FinishSeconds *int       `json:"finish_seconds"`
	TimeSource    string     `json:"time_source"`
	Bib           string     `json:"bib"`
	Confidence    string     `json:"confidence"`
	Evidence      []Evidence `json:"evidence"`
	Notes         string     `json:"notes"`
}

// ImportResults adds candidates as pending results for the user. selfName
// is the user's first name: candidates for that person become their own
// results, anyone else is kept as a guest runner. Returns added/skipped.
func ImportResults(ctx context.Context, db *sql.DB, userID int64, selfName string, cands []ImportCandidate) (added, skipped int, errs []string) {
	for _, c := range cands {
		person := strings.TrimSpace(c.Person)
		if strings.EqualFold(person, selfName) || person == "" {
			person = ""
		}
		src := c.TimeSource
		switch {
		case strings.Contains(src, "strava"):
			src = "strava"
		case strings.Contains(src, "email") || strings.Contains(src, "organizer"):
			src = "email"
		case c.FinishSeconds == nil:
			src = ""
		default:
			src = "email"
		}
		in := ResultInput{PersonName: person, RaceName: c.RaceName, RaceDate: c.Date, DateExact: c.DateExact, DistanceM: c.DistanceM,
			City: c.City, Country: c.Country, FinishSeconds: c.FinishSeconds, TimeSource: src, Bib: c.Bib, Status: "pending",
			Confidence: c.Confidence, Evidence: c.Evidence, Notes: c.Notes}
		if _, err := SaveResult(ctx, db, userID, 0, in, "gmail"); err != nil {
			if errors.Is(err, ErrDuplicate) {
				skipped++
				continue
			}
			errs = append(errs, fmt.Sprintf("%s %s: %v", c.Date, c.RaceName, err))
			continue
		}
		added++
	}
	return added, skipped, errs
}

// --- time lookup on public results sites -------------------------------

// timeLookupPrompt asks Claude to find a public result.
func timeLookupPrompt(r *Result, runner string) string {
	return fmt.Sprintf(`Find the official finish time of the runner %q in the race %q on %s (%d m) in %s.
Search the web for the race's public results (timing providers such as EQ Timing, Sportstiming, Onreg/Ultimate, Mika Timing, RunCzech, SuperHalfs, the organizer's site) and open the results page. Match the runner by full name (and bib %q if given). Prefer chip/net time when both are listed.
Never guess: if you can't find this exact runner in this exact race, say so.

Reply with ONLY this JSON object, no text before it:
{"found": true, "finish_seconds": 6668, "time_text": "1:51:08", "kind": "chip" | "gun" | "unknown", "url": "<results page you opened>", "note": "<one short English sentence>"}`,
		runner, r.RaceName, r.RaceDate, r.DistanceM, r.City, r.Bib)
}

type timeLookupResult struct {
	Found         bool   `json:"found"`
	FinishSeconds int    `json:"finish_seconds"`
	URL           string `json:"url"`
	Note          string `json:"note"`
}

// StartTimeLookup queues a results-site lookup for one result. It shares the
// research queue, budget and log (kind "time").
func (r *Researcher) StartTimeLookup(ctx context.Context, userID, resultID int64) (*ResearchRun, error) {
	res, err := GetResult(ctx, r.DB, userID, resultID)
	if err != nil {
		return nil, err
	}
	runner, err := r.runnerName(ctx, userID, res)
	if err != nil {
		return nil, err
	}
	cfg, err := r.checkBudget(ctx)
	if err != nil {
		return nil, err
	}
	runID, err := r.beginRun(ctx, "time", nil, "manual", userID)
	if err != nil {
		return nil, err
	}
	go r.lookupTime(context.Background(), cfg, runID, userID, res, runner)
	return r.GetRun(ctx, runID)
}

// runnerName is the full name to search results lists for.
func (r *Researcher) runnerName(ctx context.Context, userID int64, res *Result) (string, error) {
	var name string
	if err := r.DB.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return "", err
	}
	if res.PersonName == "" {
		return name, nil
	}
	if parts := strings.Fields(name); len(parts) > 1 && !strings.Contains(res.PersonName, " ") {
		return res.PersonName + " " + parts[len(parts)-1], nil
	}
	return res.PersonName, nil
}

// lookupTime does one lookup for an already-begun run.
func (r *Researcher) lookupTime(ctx context.Context, cfg *training.ClaudeConfig, runID, userID int64, res *Result, runner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, raceResearchTimeout)
	defer cancel()
	reply, cost, err := r.Run(callCtx, cfg, timeLookupPrompt(res, runner))
	if err != nil {
		r.finishRun(runID, "failed", cost, 0, res.RaceName, nil, err.Error())
		return
	}
	var out timeLookupResult
	if err := decodeFirstJSON(reply, &out); err != nil {
		r.finishRun(runID, "failed", cost, 0, res.RaceName, nil, "could not read the reply: "+err.Error())
		return
	}
	if !out.Found || out.FinishSeconds <= 0 || out.FinishSeconds > 48*3600 {
		r.finishRun(runID, "skipped", cost, 0, res.RaceName+": "+out.Note, []string{out.URL}, "time not found")
		return
	}
	if _, err := r.DB.Exec(`UPDATE race_results SET finish_seconds = ?, time_source = 'results_site', time_url = ?, updated_at = ?
		WHERE id = ? AND user_id = ? AND finish_seconds IS NULL`, out.FinishSeconds, out.URL, ts(time.Now()), res.ID, userID); err != nil {
		r.finishRun(runID, "failed", cost, 0, res.RaceName, nil, err.Error())
		return
	}
	r.finishRun(runID, "done", cost, 1, res.RaceName+": "+out.Note, []string{out.URL}, "")
}

// StartMissingTimeLookups looks up every confirmed result without a time,
// one after another in the background, stopping when the budget runs out.
// It returns how many lookups were queued.
func (r *Researcher) StartMissingTimeLookups(ctx context.Context, userID int64) (int, error) {
	results, err := ListResults(ctx, r.DB, userID)
	if err != nil {
		return 0, err
	}
	var todo []Result
	for _, res := range results {
		if res.Status == "confirmed" && res.FinishSeconds == nil && len(todo) < 60 {
			todo = append(todo, res)
		}
	}
	if len(todo) == 0 {
		return 0, nil
	}
	if _, err := r.checkBudget(ctx); err != nil {
		return 0, err
	}
	go func() {
		bg := context.Background()
		for i := range todo {
			res := todo[i]
			cfg, err := r.checkBudget(bg)
			if err != nil {
				log.Printf("races: time lookups stopped: %v", err)
				return
			}
			runner, err := r.runnerName(bg, userID, &res)
			if err != nil {
				return
			}
			runID, err := r.beginRun(bg, "time", nil, "manual", userID)
			if err != nil {
				return
			}
			r.lookupTime(bg, cfg, runID, userID, &res, runner)
		}
	}()
	return len(todo), nil
}
