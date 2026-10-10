package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Local races: a daily import of running races near home from Kondis'
// terminliste (terminlista.kondis.no), the Norwegian race calendar. Imported
// races are local (no trip needed), carry every distance the event offers
// (kids' races marked), and are kept current by the next import instead of
// by research. Edits go through UpdateEvent, so a watched race's moved date
// or cancellation is announced like any other change.

// KondisAPI is the JSON API behind terminlista.kondis.no.
const KondisAPI = "https://api.terminlista.kondis.no"

const (
	kondisPageSize = 200
	kondisMaxPages = 20
	// LocalSyncHour is when (Oslo time) the daily import runs.
	LocalSyncHour = 5
	localSyncKey  = "local_sync_status"
	sourceKondis  = "kondis"
)

// LocalSyncer imports local races from Kondis.
type LocalSyncer struct {
	DB      *sql.DB
	HTTP    *http.Client
	BaseURL string
	Now     func() time.Time
}

// NewLocalSyncer is the production syncer.
func NewLocalSyncer(db *sql.DB) *LocalSyncer {
	return &LocalSyncer{DB: db, HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: KondisAPI, Now: time.Now}
}

// LocalSyncStatus is the outcome of the last import, shown in the admin view.
type LocalSyncStatus struct {
	At      string `json:"at"`
	Fetched int    `json:"fetched"` // events read from Kondis (all of Norway)
	Nearby  int    `json:"nearby"`  // within the radius and kept
	Created int    `json:"created"`
	Updated int    `json:"updated"` // stored races whose facts changed
	Removed int    `json:"removed"` // gone from Kondis or out of range; deleted
	Closed  int    `json:"closed"`  // gone but watched by someone; marked closed
	Pruned  int    `json:"pruned"`  // past races nobody in the family was in; deleted
	Error   string `json:"error,omitempty"`
}

type kondisName struct {
	No string `json:"no"`
}

type kondisDistance struct {
	RangeTo             float64    `json:"rangeTo"`
	FirebaseImportRange int        `json:"firebaseImportRange"`
	Title               kondisName `json:"title"`
	IsChildrenRace      bool       `json:"isChildrenRace"`
	IsUpHillRace        bool       `json:"isUpHillRace"`
}

type kondisEvent struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	FromDate       string   `json:"fromDate"`
	About          string   `json:"about"`
	StartLat       *float64 `json:"startLat"`
	StartLng       *float64 `json:"startLng"`
	Latitude       *float64 `json:"latitude"`
	Longitude      *float64 `json:"longitude"`
	IsVirtualRace  bool     `json:"isVirtualRace"`
	PublicEventURL string   `json:"publicEventUrl"`
	Municipality   *struct {
		Name kondisName `json:"name"`
	} `json:"municipality"`
	SportType *struct {
		Slug string `json:"slug"`
	} `json:"sportType"`
	Distances []kondisDistance `json:"distances"`
}

type kondisPage struct {
	Success bool `json:"success"`
	Data    struct {
		Items      []kondisEvent `json:"items"`
		Pagination struct {
			Total   int `json:"total"`
			Current int `json:"current"`
			Next    int `json:"next"`
		} `json:"pagination"`
	} `json:"data"`
}

// fetchAll reads every upcoming event from today on.
func (s *LocalSyncer) fetchAll(ctx context.Context, from time.Time) ([]kondisEvent, error) {
	var all []kondisEvent
	for page := 1; page <= kondisMaxPages; page++ {
		q := url.Values{}
		q.Set("page", fmt.Sprint(page))
		q.Set("pageSize", fmt.Sprint(kondisPageSize))
		q.Set("sortBy", "fromDate")
		q.Set("sort", "asc")
		q.Set("fromDate", from.Format("2006-01-02")+"T00:00:00.000Z")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/events?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Hytte/1.0 (family race calendar)")
		resp, err := s.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("kondis page %d: %w", page, err)
		}
		var p kondisPage
		err = json.NewDecoder(resp.Body).Decode(&p)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("kondis page %d: HTTP %d", page, resp.StatusCode)
		}
		if err != nil || !p.Success {
			return nil, fmt.Errorf("kondis page %d: unexpected response", page)
		}
		all = append(all, p.Data.Items...)
		if len(p.Data.Items) == 0 || p.Data.Pagination.Next <= p.Data.Pagination.Current || len(all) >= p.Data.Pagination.Total {
			return all, nil
		}
	}
	return all, nil
}

// distanceKM is the great-circle distance between two points.
func distanceKM(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

var (
	// "X Opp" is how Norwegian hill climbs are named (Stoltzekleiven Opp,
	// Lyderhorn Opp); not every one has Kondis' uphill flag set.
	uphillTitle = regexp.MustCompile(`(?i)(^|\s)opp(\s|$)|motbakke|stoltzekleiven`)
	kidsLabel   = regexp.MustCompile(`(?i)barn|kids|mini|år\b|\bG\d|\bJ\d`)
)

// kondisToInput maps a Kondis event to a catalog race, or returns ok=false
// when it isn't one the family wants: not running, virtual, too far away,
// parkrun (unless wanted) or uphill.
func kondisToInput(e kondisEvent, set ResearchSettings) (in EventInput, lat, lng float64, ok bool) {
	if e.SportType != nil && e.SportType.Slug != "running_sport_type" {
		return in, 0, 0, false
	}
	title := strings.TrimSpace(e.Title)
	if e.IsVirtualRace || title == "" || uphillTitle.MatchString(title) {
		return in, 0, 0, false
	}
	if !set.IncludeParkrun && strings.Contains(strings.ToLower(title), "parkrun") {
		return in, 0, 0, false
	}
	switch {
	case e.StartLat != nil && e.StartLng != nil && (*e.StartLat != 0 || *e.StartLng != 0):
		lat, lng = *e.StartLat, *e.StartLng
	case e.Latitude != nil && e.Longitude != nil:
		lat, lng = *e.Latitude, *e.Longitude
	default:
		return in, 0, 0, false
	}
	if distanceKM(set.HomeLat, set.HomeLng, lat, lng) > float64(set.LocalRadiusKM) {
		return in, 0, 0, false
	}
	start, err := time.Parse(time.RFC3339, e.FromDate)
	if err != nil {
		return in, 0, 0, false
	}

	var distances []Distance
	main, longest := 0, 0
	for _, d := range e.Distances {
		if d.IsUpHillRace {
			continue
		}
		m := int(math.Round(d.RangeTo * 1000))
		if m <= 0 {
			m = d.FirebaseImportRange
		}
		if m <= 0 || m > 1_000_000 {
			continue
		}
		label := strings.TrimSpace(d.Title.No)
		if len([]rune(label)) > 60 {
			label = string([]rune(label)[:60])
		}
		kids := d.IsChildrenRace || kidsLabel.MatchString(label) || m <= 1000
		distances = append(distances, Distance{M: m, Label: label, Kids: kids})
		longest = max(longest, m)
		if !kids {
			main = max(main, m)
		}
		if len(distances) == maxDistances {
			break
		}
	}
	if len(distances) == 0 {
		return in, 0, 0, false // nothing left once uphill distances are dropped
	}
	if main == 0 {
		main = longest
	}

	place := ""
	if e.Municipality != nil {
		place = strings.TrimSpace(e.Municipality.Name.No)
	}
	about := strings.TrimSpace(strings.ReplaceAll(e.About, "\r\n", "\n"))
	if len([]rune(about)) > maxTextLen {
		about = string([]rune(about)[:maxTextLen-1]) + "…"
	}
	if len([]rune(title)) > maxNameLen {
		title = string([]rune(title)[:maxNameLen])
	}
	link := ""
	if u, err := url.Parse(e.PublicEventURL); err == nil && u.Scheme == "https" && u.Host != "" {
		link = u.String()
	}
	in = EventInput{
		Name: title, RaceDate: start.In(zoneOrUTC()).Format("2006-01-02"), Country: "NO", DistanceM: main,
		Status: StatusOpen, EntryType: "fcfs", URL: link, Scope: ScopeLocal, Distances: distances, Place: place,
		// Kondis writes in Norwegian; the page falls back to it in every language.
		Texts: map[string]EventText{"nb": {Place: place, Course: about}},
	}
	return in, lat, lng, true
}

// duplicatesCatalog reports whether the catalog already has this race from
// elsewhere (seeded or researched, e.g. Bergen City Marathon): same day, and
// the names start with the same two words.
func (s *LocalSyncer) duplicatesCatalog(ctx context.Context, in EventInput) bool {
	key := nameKey(in.Name)
	rows, err := s.DB.QueryContext(ctx, `SELECT name FROM race_events WHERE race_date = ? AND source = ''`, in.RaceDate)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && key != "" && nameKey(name) == key {
			return true
		}
	}
	return false
}

// nameKey is a race name's first two words, folded ("Bergen City Marathon,
// halvmaraton" -> "bergen city").
func nameKey(name string) string {
	words := strings.Fields(strings.NewReplacer(",", " ", "-", " ", "|", " ").Replace(fold(name)))
	if len(words) > 2 {
		words = words[:2]
	}
	return strings.Join(words, " ")
}

// Sync runs one import, prunes past races and stores the status. Pruning
// runs even when the import is off or fails.
func (s *LocalSyncer) Sync(ctx context.Context) (LocalSyncStatus, error) {
	st, err := s.sync(ctx)
	if err != nil {
		st.Error = err.Error()
	}
	if n, perr := PrunePastRaces(ctx, s.DB, s.Now()); perr != nil {
		log.Printf("races: prune past races: %v", perr)
	} else {
		st.Pruned = n
	}
	if b, merr := json.Marshal(st); merr == nil {
		if _, xerr := s.DB.ExecContext(ctx, `INSERT INTO race_settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			localSyncKey, string(b), now()); xerr != nil {
			log.Printf("races: store local sync status: %v", xerr)
		}
	}
	return st, err
}

// ErrLocalSyncDisabled is returned when the import is switched off.
var ErrLocalSyncDisabled = errors.New("local race import is switched off")

func (s *LocalSyncer) sync(ctx context.Context) (LocalSyncStatus, error) {
	nowT := s.Now()
	st := LocalSyncStatus{At: nowT.UTC().Format(time.RFC3339)}
	set, err := LoadResearchSettings(ctx, s.DB)
	if err != nil {
		return st, err
	}
	if !set.LocalSyncEnabled {
		return st, ErrLocalSyncDisabled
	}
	today := nowT.In(zoneOrUTC())
	events, err := s.fetchAll(ctx, today)
	if err != nil {
		return st, err
	}
	st.Fetched = len(events)

	stored := map[string]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, source_id FROM race_events WHERE source = ?`, sourceKondis)
	if err != nil {
		return st, fmt.Errorf("list imported races: %w", err)
	}
	for rows.Next() {
		var id int64
		var sid string
		if err := rows.Scan(&id, &sid); err != nil {
			rows.Close()
			return st, err
		}
		stored[sid] = id
	}
	rows.Close()

	seen := map[string]bool{}
	for _, ke := range events {
		in, lat, lng, ok := kondisToInput(ke, set)
		if !ok || ke.ID == "" || seen[ke.ID] {
			continue
		}
		seen[ke.ID] = true
		st.Nearby++
		id, exists := stored[ke.ID]
		if !exists && s.duplicatesCatalog(ctx, in) {
			st.Nearby--
			continue
		}
		if !exists {
			e, err := CreateEvent(ctx, s.DB, in, "", sourceKondis, 0)
			if err != nil {
				log.Printf("races: import %q from kondis: %v", ke.Title, err)
				continue
			}
			id = e.ID
			st.Created++
		} else {
			_, diffs, err := UpdateEvent(ctx, s.DB, id, in, sourceKondis, 0)
			if err != nil {
				log.Printf("races: update %q from kondis: %v", ke.Title, err)
				continue
			}
			if len(diffs) > 0 {
				st.Updated++
			}
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE race_events SET source = ?, source_id = ?, lat = ?, lng = ? WHERE id = ?`,
			sourceKondis, ke.ID, lat, lng, id); err != nil {
			return st, fmt.Errorf("tag imported race %d: %w", id, err)
		}
	}

	// Upcoming imports Kondis no longer lists (cancelled, or now outside the
	// radius): delete them, or close them when someone is watching. A sync
	// that would drop more than half of them is distrusted and skipped.
	todayDate := today.Format("2006-01-02")
	var gone []int64
	for sid, id := range stored {
		if seen[sid] {
			continue
		}
		var date string
		if err := s.DB.QueryRowContext(ctx, `SELECT race_date FROM race_events WHERE id = ?`, id).Scan(&date); err != nil || date < todayDate {
			continue
		}
		gone = append(gone, id)
	}
	if upcoming := len(gone) + st.Nearby; len(gone) > 0 && len(gone)*2 > upcoming {
		log.Printf("races: kondis import would remove %d of %d upcoming races; skipped", len(gone), upcoming)
		return st, nil
	}
	for _, id := range gone {
		var watched int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_watch WHERE event_id = ?`, id).Scan(&watched); err != nil {
			return st, err
		}
		if watched == 0 {
			if err := DeleteEvent(ctx, s.DB, id); err == nil {
				st.Removed++
			}
			continue
		}
		e, err := GetEvent(ctx, s.DB, id)
		if err != nil || e.Status == StatusClosed {
			continue
		}
		in := EventAsInput(e)
		in.Status = StatusClosed
		if _, _, err := UpdateEvent(ctx, s.DB, id, in, sourceKondis, 0); err == nil {
			st.Closed++
		}
	}
	return st, nil
}

// pruneGrace is how long a finished race stays, so there is time to mark it
// completed or log a result.
const pruneGrace = 7 * 24 * time.Hour

// PrunePastRaces deletes races that took place more than a week ago and that
// nobody is in: no one watches them (in any state), no result and no trip
// points at them. It returns how many went.
func PrunePastRaces(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	cutoff := now.Add(-pruneGrace).In(zoneOrUTC()).Format("2006-01-02")
	res, err := db.ExecContext(ctx, `DELETE FROM race_events
		WHERE race_date < ?
		  AND NOT EXISTS (SELECT 1 FROM race_watch w WHERE w.event_id = race_events.id)
		  AND NOT EXISTS (SELECT 1 FROM race_results r WHERE r.event_id = race_events.id)
		  AND NOT EXISTS (SELECT 1 FROM trips t WHERE t.race_event_id = race_events.id)`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LoadLocalSyncStatus returns the last import's status (zero before the first).
func LoadLocalSyncStatus(ctx context.Context, db *sql.DB) LocalSyncStatus {
	var st LocalSyncStatus
	var v string
	if err := db.QueryRowContext(ctx, `SELECT value FROM race_settings WHERE key = ?`, localSyncKey).Scan(&v); err == nil {
		_ = json.Unmarshal([]byte(v), &st)
	}
	return st
}

// NextLocalSync is the next daily import time after now.
func NextLocalSync(now time.Time) time.Time {
	loc := zoneOrUTC()
	t := now.In(loc)
	next := time.Date(t.Year(), t.Month(), t.Day(), LocalSyncHour, 30, 0, 0, loc)
	if !next.After(t) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunLocalSyncLoop imports local races daily, and soon after start when the
// last import is more than a day old.
func RunLocalSyncLoop(ctx context.Context, db *sql.DB) {
	s := NewLocalSyncer(db)
	wait := time.Until(NextLocalSync(time.Now()))
	if last, err := time.Parse(time.RFC3339, LoadLocalSyncStatus(ctx, db).At); err != nil || time.Since(last) > 26*time.Hour {
		wait = 2 * time.Minute
	}
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if st, err := s.Sync(ctx); err != nil && !errors.Is(err, ErrLocalSyncDisabled) {
			log.Printf("races: local race import: %v", err)
		} else if err == nil {
			log.Printf("races: local race import: %d nearby (%d new, %d changed, %d removed, %d closed); %d past races pruned",
				st.Nearby, st.Created, st.Updated, st.Removed, st.Closed, st.Pruned)
		}
		wait = time.Until(NextLocalSync(time.Now()))
	}
}
