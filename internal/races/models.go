// Package races is the race catalog: races out in the world (shared by all
// users, kept current by manual edits and later by scheduled research) and
// each user's relationship to them — watching, in a lottery, registered.
package races

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Languages the catalog's translatable text is kept in. The UI picks the
// viewer's language and falls back through this order.
var Languages = []string{"nb", "en", "th"}

// Scope: a local race is within reach of home (no trip needed); an away race
// is one you travel to.
const (
	ScopeLocal = "local"
	ScopeAway  = "away"
)

// Distance is one distance an event offers. Kids marks a children's race.
type Distance struct {
	M     int    `json:"m"`
	Label string `json:"label"`
	Kids  bool   `json:"kids"`
}

const (
	StatusOpen   = "open"   // registration or lottery open now
	StatusLater  = "later"  // opens later, or status unclear
	StatusClosed = "closed" // sold out, lottery over
)

var (
	validStatus     = set(StatusOpen, StatusLater, StatusClosed)
	validEntryType  = set("lottery", "fcfs", "qualifier", "unknown")
	validTravel     = set("direct", "nearby", "none", "")
	validScope      = set(ScopeLocal, ScopeAway)
	validPrecision  = set("day", "approx", "early", "mid", "late", "month")
	validSeries     = set("majors", "emc", "superhalfs")
	validWatchState = set("watching", "planning", "lottery_entered", "got_place", "not_selected", "registered", "completed", "skipped")
	validKind       = set("entry_opens", "entry_closes", "lottery_opens", "lottery_closes", "lottery_results",
		"payment_due", "price_increase", "waitlist_closes", "other")
	validLanguage = set(Languages...)

	datePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timePattern    = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// EventText is the translatable part of a race, one per language.
type EventText struct {
	Place        string `json:"place"`
	Participants string `json:"participants"`
	Course       string `json:"course"`
	Travel       string `json:"travel"`
	How          string `json:"how"`
	Price        string `json:"price"`
}

// DeadlineText is the translatable part of a deadline.
type DeadlineText struct {
	What string `json:"what"`
}

// Event is one race at one distance in one edition (e.g. Berlin Marathon 2027).
type Event struct {
	ID            int64                `json:"id"`
	Slug          string               `json:"slug"`
	Name          string               `json:"name"`
	EditionYear   int                  `json:"edition_year"`
	RaceDate      string               `json:"race_date"`
	DatePrecision string               `json:"date_precision"`
	Country       string               `json:"country"`
	DistanceM     int                  `json:"distance_m"`
	Status        string               `json:"status"`
	EntryType     string               `json:"entry_type"`
	Travel        string               `json:"travel"`
	URL           string               `json:"url"`
	Series        []string             `json:"series"`
	Texts         map[string]EventText `json:"texts"`
	Scope         string               `json:"scope"`
	Distances     []Distance           `json:"distances"`
	Place         string               `json:"place"`
	Lat           *float64             `json:"lat"`
	Lng           *float64             `json:"lng"`
	Source        string               `json:"source"` // "" (manual/seed/research) or "kondis"
	SourceID      string               `json:"source_id"`
	CheckedAt     string               `json:"checked_at"`
	CreatedAt     string               `json:"created_at"`
	UpdatedAt     string               `json:"updated_at"`
	Deadlines     []Deadline           `json:"deadlines"`
}

// Deadline is a dated milestone for a race. DueAt is derived: the exact
// instant (UTC) when the organizer gave a clock time and time zone.
type Deadline struct {
	ID            int64                   `json:"id"`
	EventID       int64                   `json:"event_id"`
	Kind          string                  `json:"kind"`
	DueDate       string                  `json:"due_date"`
	DatePrecision string                  `json:"date_precision"`
	DueTime       string                  `json:"due_time"`
	TZ            string                  `json:"tz"`
	DueAt         *time.Time              `json:"due_at,omitempty"`
	Expected      bool                    `json:"expected"`
	Texts         map[string]DeadlineText `json:"texts"`
}

// Change is one logged field change on a catalog race.
type Change struct {
	ID        int64  `json:"id"`
	EventID   int64  `json:"event_id"`
	Field     string `json:"field"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

// Watch is a user's relationship to a catalog race.
type Watch struct {
	EventID      int64  `json:"event_id"`
	State        string `json:"state"`
	Notes        string `json:"notes"`
	StrideRaceID *int64 `json:"stride_race_id"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// EventInput is the editable part of an event, as sent by the admin editor
// and by the seed. Deadlines are edited separately.
type EventInput struct {
	Name          string               `json:"name"`
	EditionYear   int                  `json:"edition_year"`
	RaceDate      string               `json:"race_date"`
	DatePrecision string               `json:"date_precision"`
	Country       string               `json:"country"`
	DistanceM     int                  `json:"distance_m"`
	Status        string               `json:"status"`
	EntryType     string               `json:"entry_type"`
	Travel        string               `json:"travel"`
	URL           string               `json:"url"`
	Series        []string             `json:"series"`
	Texts         map[string]EventText `json:"texts"`
	Scope         string               `json:"scope"`
	Distances     []Distance           `json:"distances"`
	Place         string               `json:"place"`
}

// DeadlineInput is the editable part of a deadline.
type DeadlineInput struct {
	Kind          string                  `json:"kind"`
	DueDate       string                  `json:"due_date"`
	DatePrecision string                  `json:"date_precision"`
	DueTime       string                  `json:"due_time"`
	TZ            string                  `json:"tz"`
	Expected      bool                    `json:"expected"`
	Texts         map[string]DeadlineText `json:"texts"`
}

// WatchInput is the body of PUT /api/races/{id}/watch.
type WatchInput struct {
	State string `json:"state"`
	Notes string `json:"notes"`
}

const (
	maxNameLen  = 200
	maxTextLen  = 4000
	maxNotesLen = 4000
	// maxDistances caps an event's distance list (local races list kids'
	// age classes separately).
	maxDistances = 20
)

// ErrValidation wraps every input validation failure so handlers can map it
// to 400 and pass the message through.
var ErrValidation = errors.New("invalid input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

func validDate(s string) bool {
	if !datePattern.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Normalize trims the input and checks every field, so what is stored is
// always something the UI and the research job can rely on.
func (in *EventInput) Normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	in.URL = strings.TrimSpace(in.URL)
	if in.DatePrecision == "" {
		in.DatePrecision = "day"
	}
	if in.EntryType == "" {
		in.EntryType = "unknown"
	}
	// Scope and place may be left empty: a new race is then away, and an
	// update keeps what is stored (research replies don't carry them).
	in.Place = strings.TrimSpace(in.Place)

	switch {
	case in.Name == "" || len([]rune(in.Name)) > maxNameLen:
		return invalid("name is required (max %d characters)", maxNameLen)
	case !validDate(in.RaceDate):
		return invalid("race_date must be YYYY-MM-DD")
	case !validPrecision[in.DatePrecision]:
		return invalid("unknown date_precision %q", in.DatePrecision)
	case in.Country != "" && !countryPattern.MatchString(in.Country):
		return invalid("country must be a two-letter code")
	case in.DistanceM <= 0 || in.DistanceM > 1_000_000:
		return invalid("distance_m must be between 1 and 1000000")
	case !validStatus[in.Status]:
		return invalid("unknown status %q", in.Status)
	case !validEntryType[in.EntryType]:
		return invalid("unknown entry_type %q", in.EntryType)
	case !validTravel[in.Travel]:
		return invalid("unknown travel %q", in.Travel)
	case in.Scope != "" && !validScope[in.Scope]:
		return invalid("unknown scope %q", in.Scope)
	case len([]rune(in.Place)) > 120:
		return invalid("place too long (max 120 characters)")
	case len(in.Distances) > maxDistances:
		return invalid("too many distances (max %d)", maxDistances)
	}
	distances := make([]Distance, 0, len(in.Distances))
	for _, d := range in.Distances {
		d.Label = strings.TrimSpace(d.Label)
		if d.M <= 0 || d.M > 1_000_000 {
			return invalid("distance must be between 1 and 1000000 m")
		}
		if len([]rune(d.Label)) > 60 {
			return invalid("distance label too long (max 60 characters)")
		}
		distances = append(distances, d)
	}
	in.Distances = distances
	if in.EditionYear == 0 {
		in.EditionYear, _ = strconv.Atoi(in.RaceDate[:4])
	}
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("url must be an http(s) link")
		}
	}

	series := make([]string, 0, len(in.Series))
	seen := map[string]bool{}
	for _, s := range in.Series {
		s = strings.TrimSpace(s)
		if !validSeries[s] {
			return invalid("unknown series %q", s)
		}
		if !seen[s] {
			seen[s] = true
			series = append(series, s)
		}
	}
	in.Series = series

	if in.Texts == nil {
		in.Texts = map[string]EventText{}
	}
	for lang, t := range in.Texts {
		if !validLanguage[lang] {
			return invalid("unknown language %q", lang)
		}
		for _, f := range []*string{&t.Place, &t.Participants, &t.Course, &t.Travel, &t.How, &t.Price} {
			*f = strings.TrimSpace(*f)
			if len([]rune(*f)) > maxTextLen {
				return invalid("text too long (max %d characters)", maxTextLen)
			}
		}
		in.Texts[lang] = t
	}
	return nil
}

// Normalize checks a deadline, including that due_time and tz come together
// and that tz is a real IANA zone.
func (in *DeadlineInput) Normalize() error {
	in.DueTime = strings.TrimSpace(in.DueTime)
	in.TZ = strings.TrimSpace(in.TZ)
	if in.DatePrecision == "" {
		in.DatePrecision = "day"
	}
	switch {
	case !validKind[in.Kind]:
		return invalid("unknown kind %q", in.Kind)
	case !validDate(in.DueDate):
		return invalid("due_date must be YYYY-MM-DD")
	case !validPrecision[in.DatePrecision]:
		return invalid("unknown date_precision %q", in.DatePrecision)
	case in.DueTime != "" && !timePattern.MatchString(in.DueTime):
		return invalid("due_time must be HH:MM")
	case (in.DueTime == "") != (in.TZ == ""):
		return invalid("due_time and tz must be given together")
	}
	if in.TZ != "" {
		if _, err := time.LoadLocation(in.TZ); err != nil {
			return invalid("unknown time zone %q", in.TZ)
		}
	}
	if in.Texts == nil {
		in.Texts = map[string]DeadlineText{}
	}
	for lang, t := range in.Texts {
		if !validLanguage[lang] {
			return invalid("unknown language %q", lang)
		}
		t.What = strings.TrimSpace(t.What)
		if len([]rune(t.What)) > maxTextLen {
			return invalid("text too long (max %d characters)", maxTextLen)
		}
		in.Texts[lang] = t
	}
	return nil
}

// Normalize checks a watch update.
func (in *WatchInput) Normalize() error {
	in.Notes = strings.TrimSpace(in.Notes)
	if in.State == "" {
		in.State = "watching"
	}
	if !validWatchState[in.State] {
		return invalid("unknown state %q", in.State)
	}
	if len([]rune(in.Notes)) > maxNotesLen {
		return invalid("notes too long (max %d characters)", maxNotesLen)
	}
	return nil
}

// dueAt resolves a deadline's exact instant when it has a clock time.
func dueAt(date, clock, tz string) *time.Time {
	if clock == "" || tz == "" {
		return nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
