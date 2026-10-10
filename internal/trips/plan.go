package trips

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Tentative dates: a trip can start as a window ("a weekend in Paris in
// November–January, 2 nights, leaving Friday"). Candidates are every
// departure in the window on an allowed weekday; each is checked against
// the travellers' calendars, their races and their other trips. Choosing
// one fixes the trip's dates but keeps the window, so later price checks
// can still compare the alternatives.

// Flex is a trip's tentative date window.
type Flex struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Nights     int    `json:"nights"`
	DepartDays []int  `json:"depart_days"` // 0 = Sunday … 6 = Saturday; empty = any day
	Chosen     bool   `json:"chosen"`      // a candidate was picked; start/end hold it
}

const (
	maxFlexNights  = 30
	maxCandidates  = 120
	maxWindowDays  = 400
	flexDateLayout = "2006-01-02"
)

func (f *Flex) normalize() error {
	from, err1 := time.Parse(flexDateLayout, f.From)
	to, err2 := time.Parse(flexDateLayout, f.To)
	switch {
	case err1 != nil || err2 != nil:
		return invalid("the date window needs from and to dates (YYYY-MM-DD)")
	case f.Nights < 1 || f.Nights > maxFlexNights:
		return invalid("nights must be between 1 and %d", maxFlexNights)
	case to.Before(from.AddDate(0, 0, f.Nights)):
		return invalid("the date window is shorter than the stay")
	case to.Sub(from) > maxWindowDays*24*time.Hour:
		return invalid("the date window can be at most %d days", maxWindowDays)
	}
	seen := map[int]bool{}
	days := []int{}
	for _, d := range f.DepartDays {
		if d < 0 || d > 6 {
			return invalid("departure days are 0 (Sunday) to 6 (Saturday)")
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Ints(days)
	f.DepartDays = days
	return nil
}

// Candidate is one possible start/end inside the window, with what clashes.
type Candidate struct {
	Start   string  `json:"start"`
	End     string  `json:"end"`
	Clashes []Clash `json:"clashes"`
}

// Clash is something already on the calendar during a candidate.
type Clash struct {
	Kind      string `json:"kind"` // calendar, race or trip
	Title     string `json:"title,omitempty"`
	TitleI18n I18n   `json:"title_i18n,omitempty"` // trips: the other trip's title
	Start     string `json:"start"`
	End       string `json:"end"`
	Who       string `json:"who,omitempty"` // whose calendar or race
	ID        int64  `json:"id,omitempty"`  // race event or trip ID
}

// candidateDates lists the departures a window allows.
func candidateDates(f Flex) [][2]string {
	from, _ := time.Parse(flexDateLayout, f.From)
	to, _ := time.Parse(flexDateLayout, f.To)
	allowed := map[int]bool{}
	for _, d := range f.DepartDays {
		allowed[d] = true
	}
	var out [][2]string
	for d := from; !d.AddDate(0, 0, f.Nights).After(to) && len(out) < maxCandidates; d = d.AddDate(0, 0, 1) {
		if len(allowed) > 0 && !allowed[int(d.Weekday())] {
			continue
		}
		out = append(out, [2]string{d.Format(flexDateLayout), d.AddDate(0, 0, f.Nights).Format(flexDateLayout)})
	}
	return out
}

var oslo = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Oslo"); err == nil {
		return loc
	}
	return time.UTC
}()

// eventDays is the inclusive local date span of a calendar event. All-day
// events end at the next midnight (exclusive), so their last day is one less.
func eventDays(start, end string, allDay bool) (string, string, bool) {
	s, err1 := time.Parse(time.RFC3339, start)
	e, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return "", "", false
	}
	if allDay {
		last := e.AddDate(0, 0, -1)
		if last.Before(s) {
			last = s
		}
		return s.UTC().Format(flexDateLayout), last.UTC().Format(flexDateLayout), true
	}
	last := e.Add(-time.Second)
	if last.Before(s) {
		last = s
	}
	return s.In(oslo).Format(flexDateLayout), last.In(oslo).Format(flexDateLayout), true
}

func overlaps(aStart, aEnd, bStart, bEnd string) bool {
	return aStart <= bEnd && bStart <= aEnd
}

// Candidates lists a tentative trip's possible dates with clashes for the
// viewer and every traveller with a Hytte account.
func Candidates(ctx context.Context, db *sql.DB, tripID, userID int64) ([]Candidate, error) {
	t, err := GetTrip(ctx, db, tripID, userID)
	if err != nil {
		return nil, err
	}
	if t.Doc.Flex == nil {
		return []Candidate{}, nil
	}
	f := *t.Doc.Flex
	dates := candidateDates(f)
	cands := make([]Candidate, len(dates))
	for i, d := range dates {
		cands[i] = Candidate{Start: d[0], End: d[1], Clashes: []Clash{}}
	}
	if len(cands) == 0 {
		return cands, nil
	}

	users := map[int64]string{userID: ""}
	for _, tr := range t.Doc.Travellers {
		if tr.UserID != nil {
			users[*tr.UserID] = tr.Name
		}
	}
	var clashes []Clash
	winStart, winEnd := f.From, f.To

	for uid, who := range users {
		if who == "" {
			var full string
			_ = db.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, uid).Scan(&full)
			who = firstName(full)
		}
		// Calendar events overlapping the window (a day's slack each side for time zones).
		rows, err := db.QueryContext(ctx, `SELECT title, start_time, end_time, all_day FROM calendar_events
			WHERE user_id = ? AND status != 'cancelled' AND start_time <= ? AND end_time >= ?`,
			uid, winEnd+"T23:59:59Z", winStart+"T00:00:00Z")
		if err != nil {
			return nil, fmt.Errorf("calendar clashes: %w", err)
		}
		for rows.Next() {
			var enc, s, e string
			var allDay bool
			if err := rows.Scan(&enc, &s, &e, &allDay); err != nil {
				rows.Close()
				return nil, err
			}
			ds, de, ok := eventDays(s, e, allDay)
			if !ok {
				continue
			}
			clashes = append(clashes, Clash{Kind: "calendar", Title: encryption.DecryptLenient(enc), Start: ds, End: de, Who: who})
		}
		rows.Close()

		// Races they're going to.
		rrows, err := db.QueryContext(ctx, `SELECT e.id, e.name, e.race_date FROM race_watch w JOIN race_events e ON e.id = w.event_id
			WHERE w.user_id = ? AND w.state IN ('planning', 'lottery_entered', 'got_place', 'registered')
			  AND e.race_date BETWEEN ? AND ?`, uid, winStart, winEnd)
		if err != nil {
			return nil, fmt.Errorf("race clashes: %w", err)
		}
		for rrows.Next() {
			var c Clash
			if err := rrows.Scan(&c.ID, &c.Title, &c.Start); err != nil {
				rrows.Close()
				return nil, err
			}
			c.Kind, c.End, c.Who = "race", c.Start, who
			clashes = append(clashes, c)
		}
		rrows.Close()
	}

	// The viewer's other trips with fixed dates.
	others, err := ListTrips(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	for _, o := range others {
		if o.ID == tripID || o.Tentative || !overlaps(o.StartDate, o.EndDate, winStart, winEnd) {
			continue
		}
		clashes = append(clashes, Clash{Kind: "trip", TitleI18n: o.Title, Start: o.StartDate, End: o.EndDate, ID: o.ID})
	}

	for i := range cands {
		for _, c := range clashes {
			if overlaps(cands[i].Start, cands[i].End, c.Start, c.End) {
				cands[i].Clashes = append(cands[i].Clashes, c)
			}
		}
	}
	return cands, nil
}
