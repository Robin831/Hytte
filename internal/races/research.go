package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// Research keeps the shared catalog current: Claude, allowed only WebSearch
// and WebFetch, re-checks races against the organizers' pages and looks for
// missing races. Results go through UpdateEvent/CreateDeadline with source
// "research", so they land in the change history and reach watchers as
// change notifications like any manual edit.

const (
	// DefaultResearchModel is used unless the admin sets races_research_model.
	DefaultResearchModel = "claude-sonnet-5-5"
	PrefResearchModel    = "races_research_model"
	// PrefResearchBudget is the admin's daily research spend cap in USD.
	PrefResearchBudget = "races_research_daily_usd"
	defaultDailyBudget = 5.0

	raceResearchTimeout     = 8 * time.Minute
	discoverResearchTimeout = 15 * time.Minute

	// Nightly selection: watched races are re-checked every few days (daily
	// when a deadline is near), the rest of the upcoming catalog fortnightly.
	nightlyMaxRaces = 12
	watchedRecheck  = 3 * 24 * time.Hour
	otherRecheck    = 14 * 24 * time.Hour
	deadlineSoon    = 30 * 24 * time.Hour
	recentRunGap    = 20 * time.Hour

	// manualCooldown limits how often a non-admin can trigger a check of
	// the same race; admins are not limited (the daily budget still is).
	manualCooldown = 12 * time.Hour

	discoverMaxNew = 8
	discoverEvery  = 6 * 24 * time.Hour
)

var (
	ErrResearchBusy     = errors.New("a check of this race is already running")
	ErrResearchCooldown = errors.New("this race was checked recently")
	ErrResearchBudget   = errors.New("today's research budget is used up")
	ErrResearchNoClaude = errors.New("no admin has Claude enabled")
)

// ResearchRun is one research call as stored in race_research_runs.
type ResearchRun struct {
	ID         int64    `json:"id"`
	Kind       string   `json:"kind"`
	EventID    *int64   `json:"event_id"`
	EventName  string   `json:"event_name,omitempty"`
	Trigger    string   `json:"trigger"`
	Status     string   `json:"status"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	CostUSD    float64  `json:"cost_usd"`
	Changes    int      `json:"changes"`
	Summary    string   `json:"summary"`
	Sources    []string `json:"sources"`
	Error      string   `json:"error"`
}

// Researcher runs research calls one at a time. Its function fields are
// injectable for tests.
type Researcher struct {
	DB     *sql.DB
	Run    func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error)
	Config func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, float64, error)
	Now    func() time.Time

	mu sync.Mutex // serializes Claude calls: scheduled and manual share it
}

var (
	defaultResearcher     *Researcher
	defaultResearcherOnce sync.Once
)

// DefaultResearcher returns the process-wide researcher, so the nightly job
// and manual triggers share one queue.
func DefaultResearcher(db *sql.DB) *Researcher {
	defaultResearcherOnce.Do(func() {
		defaultResearcher = &Researcher{DB: db, Run: training.RunPromptWithWebTools, Config: researchConfig, Now: time.Now}
	})
	return defaultResearcher
}

// researchConfig borrows the first Claude-enabled admin's CLI setup, with
// the research model (default Sonnet: the job runs nightly over many races)
// and the daily budget from their preferences.
func researchConfig(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, float64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE is_admin = 1 ORDER BY id`)
	if err != nil {
		return nil, 0, err
	}
	var admins []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		admins = append(admins, id)
	}
	rows.Close()
	for _, id := range admins {
		cfg, err := training.LoadClaudeConfig(db, id)
		if err != nil || !cfg.Enabled {
			continue
		}
		prefs, _ := auth.GetPreferences(db, id)
		cfg.Model = DefaultResearchModel
		if m := prefs[PrefResearchModel]; m != "" {
			cfg.Model = m
		}
		budget := defaultDailyBudget
		if b, err := strconv.ParseFloat(prefs[PrefResearchBudget], 64); err == nil && b >= 0 {
			budget = b
		}
		return cfg, budget, nil
	}
	return nil, 0, ErrResearchNoClaude
}

func (r *Researcher) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// osloDayStart is midnight today in Oslo, the boundary of the daily budget.
func osloDayStart(now time.Time) time.Time {
	loc, err := time.LoadLocation(defaultZone)
	if err != nil {
		loc = time.UTC
	}
	n := now.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
}

// SpentToday is the research cost since midnight in Oslo.
func (r *Researcher) SpentToday(ctx context.Context) (float64, error) {
	var spent float64
	err := r.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM race_research_runs WHERE started_at >= ?`,
		ts(osloDayStart(r.now()))).Scan(&spent)
	return spent, err
}

func (r *Researcher) checkBudget(ctx context.Context) (*training.ClaudeConfig, error) {
	cfg, budget, err := r.Config(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	spent, err := r.SpentToday(ctx)
	if err != nil {
		return nil, err
	}
	if spent >= budget {
		return nil, ErrResearchBudget
	}
	return cfg, nil
}

func (r *Researcher) beginRun(ctx context.Context, kind string, eventID *int64, trigger string, userID int64) (int64, error) {
	res, err := r.DB.ExecContext(ctx, `INSERT INTO race_research_runs (kind, event_id, trigger, user_id, status, started_at)
		VALUES (?, ?, ?, ?, 'running', ?)`, kind, eventID, trigger, nullableUser(userID), ts(r.now()))
	if err != nil {
		return 0, fmt.Errorf("begin research run: %w", err)
	}
	return res.LastInsertId()
}

func (r *Researcher) finishRun(id int64, status string, cost float64, changes int, summary string, sources []string, errText string) {
	src, _ := json.Marshal(sources)
	if sources == nil {
		src = []byte("[]")
	}
	if _, err := r.DB.Exec(`UPDATE race_research_runs SET status = ?, finished_at = ?, cost_usd = ?, changes = ?,
		summary = ?, sources = ?, error = ? WHERE id = ?`,
		status, ts(r.now()), cost, changes, truncate(summary, 500), string(src), truncate(errText, 1000), id); err != nil {
		log.Printf("races: finish research run %d: %v", id, err)
	}
}

// MarkInterrupted fails runs left "running" by a restart.
func (r *Researcher) MarkInterrupted(ctx context.Context) {
	if _, err := r.DB.ExecContext(ctx, `UPDATE race_research_runs SET status = 'failed', finished_at = ?,
		error = 'interrupted by a server restart' WHERE status = 'running'`, ts(r.now())); err != nil {
		log.Printf("races: mark interrupted research runs: %v", err)
	}
}

const runColumns = `r.id, r.kind, r.event_id, COALESCE(e.name, ''), r.trigger, r.status, r.started_at, r.finished_at,
	r.cost_usd, r.changes, r.summary, r.sources, r.error`

func scanRun(row scanner) (ResearchRun, error) {
	var run ResearchRun
	var eventID sql.NullInt64
	var sources string
	if err := row.Scan(&run.ID, &run.Kind, &eventID, &run.EventName, &run.Trigger, &run.Status, &run.StartedAt,
		&run.FinishedAt, &run.CostUSD, &run.Changes, &run.Summary, &sources, &run.Error); err != nil {
		return run, err
	}
	if eventID.Valid {
		run.EventID = &eventID.Int64
	}
	run.Sources = []string{}
	_ = json.Unmarshal([]byte(sources), &run.Sources)
	return run, nil
}

// GetRun returns one research run.
func (r *Researcher) GetRun(ctx context.Context, id int64) (*ResearchRun, error) {
	run, err := scanRun(r.DB.QueryRowContext(ctx, `SELECT `+runColumns+` FROM race_research_runs r
		LEFT JOIN race_events e ON e.id = r.event_id WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &run, err
}

// LatestRunForEvent returns the newest research run for a race, if any.
func LatestRunForEvent(ctx context.Context, db *sql.DB, eventID int64) (*ResearchRun, error) {
	run, err := scanRun(db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM race_research_runs r
		LEFT JOIN race_events e ON e.id = r.event_id WHERE r.event_id = ? ORDER BY r.started_at DESC, r.id DESC LIMIT 1`, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns the newest research runs, for the admin log.
func ListRuns(ctx context.Context, db *sql.DB, limit int) ([]ResearchRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+runColumns+` FROM race_research_runs r
		LEFT JOIN race_events e ON e.id = r.event_id ORDER BY r.started_at DESC, r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list research runs: %w", err)
	}
	defer rows.Close()
	runs := []ResearchRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// StartEventResearch queues a manual check of one race and returns its run
// at once; the Claude call happens in the background. Non-admins are held
// to manualCooldown per race.
func (r *Researcher) StartEventResearch(ctx context.Context, eventID, userID int64, admin bool) (*ResearchRun, error) {
	if _, err := GetEvent(ctx, r.DB, eventID); err != nil {
		return nil, err
	}
	var running int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_research_runs WHERE event_id = ? AND status = 'running'`,
		eventID).Scan(&running); err != nil {
		return nil, err
	}
	if running > 0 {
		return nil, ErrResearchBusy
	}
	if !admin {
		var recent int
		if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_research_runs WHERE event_id = ? AND started_at >= ?`,
			eventID, ts(r.now().Add(-manualCooldown))).Scan(&recent); err != nil {
			return nil, err
		}
		if recent > 0 {
			return nil, ErrResearchCooldown
		}
	}
	cfg, err := r.checkBudget(ctx)
	if err != nil {
		return nil, err
	}
	id := eventID
	runID, err := r.beginRun(ctx, "race", &id, "manual", userID)
	if err != nil {
		return nil, err
	}
	go r.researchEvent(context.Background(), cfg, runID, eventID)
	return r.GetRun(ctx, runID)
}

// StartDiscovery queues a discovery pass (admin).
func (r *Researcher) StartDiscovery(ctx context.Context, userID int64) (*ResearchRun, error) {
	var running int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_research_runs WHERE kind = 'discover' AND status = 'running'`).
		Scan(&running); err != nil {
		return nil, err
	}
	if running > 0 {
		return nil, ErrResearchBusy
	}
	cfg, err := r.checkBudget(ctx)
	if err != nil {
		return nil, err
	}
	runID, err := r.beginRun(ctx, "discover", nil, "manual", userID)
	if err != nil {
		return nil, err
	}
	go r.discover(context.Background(), cfg, runID)
	return r.GetRun(ctx, runID)
}

// promptEvent is the catalog entry as shown to (and returned by) the model.
type promptEvent struct {
	EventInput
	Deadlines []promptDeadline `json:"deadlines"`
}

type promptDeadline struct {
	ID *int64 `json:"id"`
	DeadlineInput
}

func eventToInput(e *Event) EventInput {
	return EventInput{Name: e.Name, EditionYear: e.EditionYear, RaceDate: e.RaceDate, DatePrecision: e.DatePrecision,
		Country: e.Country, DistanceM: e.DistanceM, Status: e.Status, EntryType: e.EntryType, Travel: e.Travel,
		URL: e.URL, Series: e.Series, Texts: e.Texts}
}

const factRules = `Field meanings:
- status: "open" = registration or lottery open right now; "later" = opens later or unclear; "closed" = sold out or lottery over.
- entry_type: lottery | fcfs (first come, first served) | qualifier | unknown.
- travel (from Bergen airport BGO): direct | nearby (connection, or nearby airport + train) | none.
- date_precision: day (confirmed) | approx (likely date) | early | mid | late (part of month) | month.
- texts: per language nb (Norwegian bokmål, primary), en (English), th (Thai): place, participants (field size with year/source caveat), course (profile), travel (how to get there from Bergen), how (registration/lottery status and how to get in — the most important field), price (fees with currency exactly as published and price steps with dates).
- Thai text: natural Thai, Gregorian years (never Buddhist-era years like 2570), race/brand names in Latin script.
- deadlines kinds: entry_opens, entry_closes, lottery_opens, lottery_closes, lottery_results, payment_due, price_increase, waitlist_closes, other. price_increase due_date = the LAST day at the old price, and the text says so. due_time (HH:MM) + tz (IANA zone) only when the organizer gives a clock time. expected=true when inferred from earlier years. Only deadlines after today.`

func raceResearchPrompt(e *Event, today string) (string, error) {
	pe := promptEvent{EventInput: eventToInput(e)}
	for i := range e.Deadlines {
		d := e.Deadlines[i]
		id := d.ID
		pe.Deadlines = append(pe.Deadlines, promptDeadline{ID: &id, DeadlineInput: DeadlineInput{Kind: d.Kind, DueDate: d.DueDate,
			DatePrecision: d.DatePrecision, DueTime: d.DueTime, TZ: d.TZ, Expected: d.Expected, Texts: d.Texts}})
	}
	current, err := json.MarshalIndent(pe, "", " ")
	if err != nil {
		return "", err
	}
	return `You maintain a shared race catalog for a family of runners based in Bergen, Norway. Today is ` + today + `.
Re-check this race on the web and return its CURRENT facts.

Current catalog entry:
` + string(current) + `

Research: use WebSearch, then open (WebFetch) the organizer's site and any page you take a fact from. Focus on what changes: registration/lottery status right now, the date of this edition, entry fees and price steps, and every upcoming deadline. Then field size, course and travel.

Rules:
- Change a fact only when a page you opened supports it; prefer the organizer. Keep hedges ("not verified", "sources disagree") when they still apply.
- Copy every text you are not changing EXACTLY, character for character. Never rephrase, reformat or "improve" unchanged text.
- When a fact changes, update it in nb, en and th alike, in the existing style.
- edition_year and distance_m must stay as they are (a new edition is a new catalog entry, not an edit).
- deadlines: return the full list you believe in. Keep "id" for existing ones (copy unchanged ones exactly); use "id": null for new ones. Put ids of deadlines that are wrong or no longer apply in remove_deadline_ids.
- If you could not reach reliable sources, set "confident": false — nothing will be applied.

` + factRules + `

Reply with ONLY this JSON object, no text before it:
{"event": {<all fields of the entry except deadlines>}, "deadlines": [<deadline objects with id>], "remove_deadline_ids": [], "summary": "<one short English sentence: what changed, or No changes>", "sources": ["<urls you opened>"], "confident": true}`, nil
}

type raceResearchResult struct {
	Event             EventInput       `json:"event"`
	Deadlines         []promptDeadline `json:"deadlines"`
	RemoveDeadlineIDs []int64          `json:"remove_deadline_ids"`
	Summary           string           `json:"summary"`
	Sources           []string         `json:"sources"`
	Confident         bool             `json:"confident"`
}

// decodeFirstJSON reads the first JSON object in a model reply, ignoring
// any prose around it (the CLI appends a "Sources:" list after web searches).
func decodeFirstJSON(text string, v any) error {
	i := strings.Index(text, "{")
	if i < 0 {
		return errors.New("no JSON object in reply")
	}
	return json.NewDecoder(strings.NewReader(text[i:])).Decode(v)
}

func sameText(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// guardEventInput keeps what research must not change and drops noise:
// edition year and distance stay, blank or whitespace-only differences
// keep the stored text, missing languages keep theirs.
func guardEventInput(old *Event, in EventInput) EventInput {
	in.EditionYear = old.EditionYear
	in.DistanceM = old.DistanceM
	if strings.TrimSpace(in.Name) == "" {
		in.Name = old.Name
	}
	if in.Texts == nil {
		in.Texts = map[string]EventText{}
	}
	for lang, o := range old.Texts {
		n, ok := in.Texts[lang]
		if !ok {
			in.Texts[lang] = o
			continue
		}
		keep := func(newV *string, oldV string) {
			if strings.TrimSpace(*newV) == "" || sameText(*newV, oldV) {
				*newV = oldV
			}
		}
		keep(&n.Place, o.Place)
		keep(&n.Participants, o.Participants)
		keep(&n.Course, o.Course)
		keep(&n.Travel, o.Travel)
		keep(&n.How, o.How)
		keep(&n.Price, o.Price)
		in.Texts[lang] = n
	}
	return in
}

func deadlineDiffers(old Deadline, in DeadlineInput) bool {
	if old.Kind != in.Kind || old.DueDate != in.DueDate || old.DatePrecision != in.DatePrecision ||
		old.DueTime != in.DueTime || old.TZ != in.TZ || old.Expected != in.Expected {
		return true
	}
	for lang, t := range in.Texts {
		if !sameText(t.What, old.Texts[lang].What) {
			return true
		}
	}
	return false
}

// applyRaceResearch writes a research result through the normal edit paths
// and returns how many changes were logged plus per-item problems.
func (r *Researcher) applyRaceResearch(ctx context.Context, old *Event, res raceResearchResult) (int, []string) {
	var problems []string
	changes := 0
	today := r.now().In(zoneOrUTC()).Format("2006-01-02")

	in := guardEventInput(old, res.Event)
	_, diffs, err := UpdateEvent(ctx, r.DB, old.ID, in, "research", 0)
	if err != nil {
		problems = append(problems, "event: "+err.Error())
	} else {
		changes += len(diffs)
	}

	existing := map[int64]Deadline{}
	for _, d := range old.Deadlines {
		existing[d.ID] = d
	}
	for _, rd := range res.Deadlines {
		d := rd.DeadlineInput
		if rd.ID != nil {
			if o, ok := existing[*rd.ID]; ok {
				for lang, t := range o.Texts {
					if n, sent := d.Texts[lang]; !sent || strings.TrimSpace(n.What) == "" {
						if d.Texts == nil {
							d.Texts = map[string]DeadlineText{}
						}
						d.Texts[lang] = t
					}
				}
				if !deadlineDiffers(o, d) {
					continue
				}
				if _, err := UpdateDeadline(ctx, r.DB, o.ID, d, "research", 0); err != nil {
					problems = append(problems, fmt.Sprintf("deadline %d: %v", o.ID, err))
					continue
				}
				changes++
				continue
			}
		}
		if d.DueDate < today {
			continue
		}
		if _, err := CreateDeadline(ctx, r.DB, old.ID, d, "research", 0); err != nil {
			problems = append(problems, "new deadline: "+err.Error())
			continue
		}
		changes++
	}
	for _, id := range res.RemoveDeadlineIDs {
		if _, ok := existing[id]; !ok {
			continue
		}
		if err := DeleteDeadline(ctx, r.DB, id, "research", 0); err != nil {
			problems = append(problems, fmt.Sprintf("remove deadline %d: %v", id, err))
			continue
		}
		changes++
	}
	return changes, problems
}

func zoneOrUTC() *time.Location {
	loc, err := time.LoadLocation(defaultZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// researchEvent does one race check for an already-begun run.
func (r *Researcher) researchEvent(ctx context.Context, cfg *training.ClaudeConfig, runID, eventID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	old, err := GetEvent(ctx, r.DB, eventID)
	if err != nil {
		r.finishRun(runID, "failed", 0, 0, "", nil, err.Error())
		return
	}
	prompt, err := raceResearchPrompt(old, r.now().In(zoneOrUTC()).Format("2006-01-02"))
	if err != nil {
		r.finishRun(runID, "failed", 0, 0, "", nil, err.Error())
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, raceResearchTimeout)
	defer cancel()
	reply, cost, err := r.Run(callCtx, cfg, prompt)
	if err != nil {
		r.finishRun(runID, "failed", cost, 0, "", nil, err.Error())
		return
	}
	var res raceResearchResult
	if err := decodeFirstJSON(reply, &res); err != nil {
		r.finishRun(runID, "failed", cost, 0, "", nil, "could not read the research reply: "+err.Error())
		return
	}
	if !res.Confident {
		r.finishRun(runID, "skipped", cost, 0, res.Summary, res.Sources, "research was not confident; nothing applied")
		return
	}
	changes, problems := r.applyRaceResearch(ctx, old, res)
	r.finishRun(runID, "done", cost, changes, res.Summary, res.Sources, strings.Join(problems, "; "))
}

// candidate is a race the nightly job might re-check.
type candidate struct {
	id        int64
	checkedAt time.Time
	watched   bool
	soon      bool
	lastRun   time.Time
}

// pickNightly chooses which races to re-check tonight, most urgent first:
// watched races with a deadline coming up, then other watched races, then
// the rest of the upcoming catalog by staleness.
func (r *Researcher) pickNightly(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	today := now.In(zoneOrUTC()).Format("2006-01-02")
	soonDate := now.Add(deadlineSoon).In(zoneOrUTC()).Format("2006-01-02")
	rows, err := r.DB.QueryContext(ctx, `
		SELECT e.id, e.checked_at,
		       EXISTS (SELECT 1 FROM race_watch w WHERE w.event_id = e.id AND w.state NOT IN ('completed', 'skipped')),
		       EXISTS (SELECT 1 FROM race_deadlines d WHERE d.event_id = e.id AND d.due_date >= ? AND d.due_date <= ?),
		       COALESCE((SELECT MAX(started_at) FROM race_research_runs x WHERE x.event_id = e.id), '')
		FROM race_events e WHERE e.race_date >= ?`, today, soonDate, today)
	if err != nil {
		return nil, fmt.Errorf("pick races to research: %w", err)
	}
	defer rows.Close()
	var cands []candidate
	for rows.Next() {
		var c candidate
		var checked, last string
		if err := rows.Scan(&c.id, &checked, &c.watched, &c.soon, &last); err != nil {
			return nil, err
		}
		c.checkedAt, _ = time.Parse(time.RFC3339, checked)
		c.lastRun, _ = time.Parse(time.RFC3339, last)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rank := func(c candidate) int {
		switch {
		case c.watched && c.soon:
			return 0
		case c.watched:
			return 1
		default:
			return 2
		}
	}
	var due []candidate
	for _, c := range cands {
		if now.Sub(c.lastRun) < recentRunGap {
			continue
		}
		age := now.Sub(c.checkedAt)
		if (c.watched && (c.soon || age >= watchedRecheck)) || age >= otherRecheck {
			due = append(due, c)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		if rank(due[i]) != rank(due[j]) {
			return rank(due[i]) < rank(due[j])
		}
		return due[i].checkedAt.Before(due[j].checkedAt)
	})
	var ids []int64
	for i := 0; i < len(due) && i < limit; i++ {
		ids = append(ids, due[i].id)
	}
	return ids, nil
}

// Nightly re-checks due races within the budget and, about weekly, runs a
// discovery pass. It returns when done or when the budget runs out.
func (r *Researcher) Nightly(ctx context.Context) error {
	ids, err := r.pickNightly(ctx, r.now(), nightlyMaxRaces)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		cfg, err := r.checkBudget(ctx)
		if err != nil {
			return err
		}
		eid := id
		runID, err := r.beginRun(ctx, "race", &eid, "scheduled", 0)
		if err != nil {
			return err
		}
		r.researchEvent(ctx, cfg, runID, id)
	}

	var last string
	if err := r.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(started_at), '') FROM race_research_runs WHERE kind = 'discover'`).
		Scan(&last); err != nil {
		return err
	}
	if t, _ := time.Parse(time.RFC3339, last); r.now().Sub(t) >= discoverEvery {
		cfg, err := r.checkBudget(ctx)
		if err != nil {
			return err
		}
		runID, err := r.beginRun(ctx, "discover", nil, "scheduled", 0)
		if err != nil {
			return err
		}
		r.discover(ctx, cfg, runID)
	}
	return nil
}

// NextResearchRun is the next 03:30 in Oslo after now.
func NextResearchRun(now time.Time) time.Time {
	loc := zoneOrUTC()
	n := now.In(loc)
	next := time.Date(n.Year(), n.Month(), n.Day(), 3, 30, 0, 0, loc)
	if !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunResearchLoop runs the nightly research at 03:30 Oslo until ctx ends.
func RunResearchLoop(ctx context.Context, db *sql.DB) {
	r := DefaultResearcher(db)
	r.MarkInterrupted(ctx)
	for {
		timer := time.NewTimer(time.Until(NextResearchRun(time.Now())))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := r.Nightly(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("races: nightly research: %v", err)
		}
	}
}

func discoverPrompt(existing []Event, today, until string) string {
	var b strings.Builder
	for _, e := range existing {
		fmt.Fprintf(&b, "- %s | %s | %s | %d m\n", e.Slug, e.Name, e.RaceDate, e.DistanceM)
	}
	return `You maintain a shared race catalog for a family of runners based in Bergen, Norway (one family member is Thai). Today is ` + today + `.
Scope: World Marathon Majors; European Marathon Classics; big European marathons and half marathons popular with Norwegian runners; major races in Norway, Sweden, Denmark, Finland and the Baltics; the main road races in Thailand. Distances: marathon (42195) and half marathon (21097) only.

Already in the catalog — do not add these again:
` + b.String() + `
Find up to ` + strconv.Itoa(discoverMaxNew) + ` races that are missing, taking place between ` + today + ` and ` + until + `:
1. the next edition of a catalog race whose date has passed or is close, if that next edition's date or registration is already announced;
2. notable races in scope that are missing entirely.
Research each one on the web (WebSearch, then WebFetch the organizer's page and pages you take facts from). Never invent facts; say what you could not find, in the text.

` + factRules + `

Reply with ONLY this JSON object, no text before it:
{"events": [{"name": "...", "edition_year": 2027, "race_date": "YYYY-MM-DD", "date_precision": "day", "country": "NL", "distance_m": 42195, "status": "later", "entry_type": "fcfs", "travel": "direct", "url": "https://...", "series": [], "texts": {"nb": {"place": "", "participants": "", "course": "", "travel": "", "how": "", "price": ""}, "en": {...}, "th": {...}}, "deadlines": [{"kind": "entry_opens", "due_date": "YYYY-MM-DD", "date_precision": "approx", "due_time": "", "tz": "", "expected": true, "texts": {"nb": {"what": ""}, "en": {"what": ""}, "th": {"what": ""}}}]}], "summary": "<one short English sentence>", "sources": ["<urls>"]}`
}

type discoverResult struct {
	Events []struct {
		EventInput
		Deadlines []DeadlineInput `json:"deadlines"`
	} `json:"events"`
	Summary string   `json:"summary"`
	Sources []string `json:"sources"`
}

// discover asks for missing races and adds the ones that aren't in the
// catalog yet (same name and edition year counts as present).
func (r *Researcher) discover(ctx context.Context, cfg *training.ClaudeConfig, runID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, err := ListEvents(ctx, r.DB)
	if err != nil {
		r.finishRun(runID, "failed", 0, 0, "", nil, err.Error())
		return
	}
	now := r.now().In(zoneOrUTC())
	prompt := discoverPrompt(existing, now.Format("2006-01-02"), now.AddDate(0, 15, 0).Format("2006-01-02"))
	callCtx, cancel := context.WithTimeout(ctx, discoverResearchTimeout)
	defer cancel()
	reply, cost, err := r.Run(callCtx, cfg, prompt)
	if err != nil {
		r.finishRun(runID, "failed", cost, 0, "", nil, err.Error())
		return
	}
	var res discoverResult
	if err := decodeFirstJSON(reply, &res); err != nil {
		r.finishRun(runID, "failed", cost, 0, "", nil, "could not read the discovery reply: "+err.Error())
		return
	}

	present := map[string]bool{}
	for _, e := range existing {
		present[strings.ToLower(strings.TrimSpace(e.Name))+"|"+strconv.Itoa(e.EditionYear)] = true
	}
	today := now.Format("2006-01-02")
	added := 0
	var problems []string
	for i, ne := range res.Events {
		if i >= discoverMaxNew {
			break
		}
		in := ne.EventInput
		if in.DistanceM != 21097 && in.DistanceM != 42195 {
			problems = append(problems, in.Name+": not a marathon or half")
			continue
		}
		if err := in.Normalize(); err != nil {
			problems = append(problems, in.Name+": "+err.Error())
			continue
		}
		key := strings.ToLower(in.Name) + "|" + strconv.Itoa(in.EditionYear)
		if present[key] || in.RaceDate < today {
			continue
		}
		created, err := CreateEvent(ctx, r.DB, in, "", "research", 0)
		if err != nil {
			problems = append(problems, in.Name+": "+err.Error())
			continue
		}
		present[key] = true
		added++
		for _, d := range ne.Deadlines {
			if d.DueDate < today {
				continue
			}
			if _, err := CreateDeadline(ctx, r.DB, created.ID, d, "research", 0); err != nil {
				problems = append(problems, created.Name+" deadline: "+err.Error())
			}
		}
	}
	r.finishRun(runID, "done", cost, added, res.Summary, res.Sources, strings.Join(problems, "; "))
}
