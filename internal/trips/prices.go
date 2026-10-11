package trips

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/currency"
	"github.com/Robin831/Hytte/internal/races"
)

// Prices: flight and hotel prices for a trip's dates, from Google Flights
// and Google Hotels through SerpApi. One search covers one kind (flights or
// hotels) for one start/end pair and the trip's passenger mix; every search
// is kept, so later checks of the same dates form a price history. SerpApi's
// free plan is 250 searches a month, so searches are cached for half a day
// and capped per month across all trips.

// SerpAPIBase is the SerpApi endpoint.
const SerpAPIBase = "https://serpapi.com/search.json"

const (
	priceCacheFor      = 12 * time.Hour
	defaultSearchLimit = 200 // a month, leaving room under the free plan's 250
	maxSearchDates     = 6
	maxOptions         = 8
	searchTimeout      = 60 * time.Second
)

var (
	ErrNoDestination = errors.New("the trip has no destination airports yet")
	ErrNoSerpAPI     = errors.New("price search is not set up (SERPAPI_KEY)")
	ErrSearchQuota   = errors.New("this month's price searches are used up")
)

// Passengers is the fare mix for a trip.
type Passengers struct {
	Adults    int   `json:"adults"`
	ChildAges []int `json:"child_ages"` // 2–17
	Infants   int   `json:"infants"`    // under 2, on a lap
}

// passengersFor works out the mix from the travellers on the trip's first
// day: roster birth years when known, else the child flag (as a 10-year-old).
func passengersFor(ctx context.Context, db *sql.DB, t *Trip) Passengers {
	years := map[int64]int{}
	if people, err := ListPeople(ctx, db); err == nil {
		for _, p := range people {
			if p.BirthYear != nil {
				years[p.ID] = *p.BirthYear
			}
		}
	}
	var px Passengers
	for _, tr := range t.Doc.Travellers {
		age := -1
		if tr.PersonID != nil {
			if y, ok := years[*tr.PersonID]; ok {
				age = ageOn(y, t.StartDate)
			}
		}
		if age < 0 && tr.Child {
			age = 10
		}
		switch {
		case age >= 0 && age < 2:
			px.Infants++
		case age >= 2 && age < 18:
			px.ChildAges = append(px.ChildAges, age)
		default:
			px.Adults++
		}
	}
	if px.Adults == 0 {
		px.Adults = 1
	}
	return px
}

// flightCounts splits the mix the way Google Flights does: 12+ fly as adults.
func (p Passengers) flightCounts() (adults, children, infants int) {
	adults, infants = p.Adults, p.Infants
	for _, a := range p.ChildAges {
		if a >= 12 {
			adults++
		} else {
			children++
		}
	}
	return
}

// Quote is one stored search.
type Quote struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"` // flights or hotels
	Start       string          `json:"start"`
	End         string          `json:"end"`
	Status      string          `json:"status"` // pending, done, failed
	SearchedAt  string          `json:"searched_at"`
	Cheapest    *float64        `json:"cheapest"` // total for everyone, in Currency
	Currency    string          `json:"currency"`
	PriceLevel  string          `json:"price_level,omitempty"` // Google: low, typical, high
	TypicalLow  *float64        `json:"typical_low,omitempty"`
	TypicalHigh *float64        `json:"typical_high,omitempty"`
	Options     json.RawMessage `json:"options"`
	Link        string          `json:"link,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// FlightOption is one priced itinerary (outbound legs; Google prices the round trip).
type FlightOption struct {
	Price    float64     `json:"price"`
	Minutes  int         `json:"minutes"`
	Stops    int         `json:"stops"`
	Airlines []string    `json:"airlines"`
	Legs     []FlightLeg `json:"legs"`
}

// FlightLeg is one flight in an option.
type FlightLeg struct {
	Airline  string `json:"airline"`
	FlightNo string `json:"flight_no"`
	From     string `json:"from"`
	To       string `json:"to"`
	Dep      string `json:"dep"` // local "2026-11-20 12:55"
	Arr      string `json:"arr"`
}

// HotelOption is one priced hotel for the whole stay.
type HotelOption struct {
	Name     string  `json:"name"`
	Class    string  `json:"class,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
	Reviews  int     `json:"reviews,omitempty"`
	Total    float64 `json:"total"`
	PerNight float64 `json:"per_night"`
	Link     string  `json:"link,omitempty"`
	Lat      float64 `json:"lat,omitempty"`
	Lng      float64 `json:"lng,omitempty"`
}

// Searcher runs price searches. HTTP and Key are injectable for tests.
type Searcher struct {
	DB    *sql.DB
	HTTP  *http.Client
	Base  string
	Key   string
	Limit int
	Now   func() time.Time
}

// NewSearcher reads SERPAPI_KEY (and SERPAPI_MONTHLY_LIMIT) from the environment.
func NewSearcher(db *sql.DB) *Searcher {
	limit := defaultSearchLimit
	if n, err := strconv.Atoi(os.Getenv("SERPAPI_MONTHLY_LIMIT")); err == nil && n >= 0 {
		limit = n
	}
	return &Searcher{DB: db, HTTP: &http.Client{Timeout: searchTimeout}, Base: SerpAPIBase,
		Key: strings.TrimSpace(os.Getenv("SERPAPI_KEY")), Limit: limit, Now: time.Now}
}

// Quota is this month's search use.
type Quota struct {
	Used  int  `json:"used"`
	Limit int  `json:"limit"`
	Ready bool `json:"ready"` // a key is configured
}

func monthStart(now time.Time) string {
	t := now.In(oslo)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, oslo).UTC().Format(time.RFC3339)
}

// QuotaNow counts this month's searches across all trips.
func (s *Searcher) QuotaNow(ctx context.Context) Quota {
	q := Quota{Limit: s.Limit, Ready: s.Key != ""}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM trip_price_quotes WHERE searched_at >= ?`, monthStart(s.Now())).Scan(&q.Used)
	return q
}

func paramsHash(kind, start, end, dest string, px Passengers) string {
	b, _ := json.Marshal([]any{kind, start, end, dest, px})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// DatePair is one start/end to price.
type DatePair struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// searchDates picks what to price: the requested pairs, a tentative trip's
// candidates (free ones first), or the trip's own dates.
func (s *Searcher) searchDates(ctx context.Context, t *Trip, userID int64, want []DatePair) ([]DatePair, error) {
	if len(want) > 0 {
		for _, d := range want {
			if !datePat.MatchString(d.Start) || !datePat.MatchString(d.End) || d.End < d.Start {
				return nil, invalid("dates must be YYYY-MM-DD pairs")
			}
		}
		if len(want) > maxSearchDates {
			want = want[:maxSearchDates]
		}
		return want, nil
	}
	if isTentativeTrip(t) {
		cands, err := Candidates(ctx, s.DB, t.ID, userID)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(cands, func(i, j int) bool { return len(cands[i].Clashes) == 0 && len(cands[j].Clashes) > 0 })
		var out []DatePair
		for _, c := range cands {
			if len(out) == maxSearchDates {
				break
			}
			out = append(out, DatePair{c.Start, c.End})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
		return out, nil
	}
	return []DatePair{{t.StartDate, t.EndDate}}, nil
}

func isTentativeTrip(t *Trip) bool { return t.Doc.Flex != nil && !t.Doc.Flex.Chosen }

type searchJob struct {
	id    int64
	kind  string
	dates DatePair
}

// Start queues searches for a trip and runs them in the background. It
// returns how many searches were queued (cached ones are reused).
func (s *Searcher) Start(ctx context.Context, tripID, userID int64, want []DatePair) (int, error) {
	if s.Key == "" {
		return 0, ErrNoSerpAPI
	}
	if err := requireEdit(ctx, s.DB, tripID, userID); err != nil {
		return 0, err
	}
	t, err := GetTrip(ctx, s.DB, tripID, userID)
	if err != nil {
		return 0, err
	}
	dest := t.Doc.Destination
	if dest == nil || len(dest.Airports) == 0 {
		return 0, ErrNoDestination
	}
	dates, err := s.searchDates(ctx, t, userID, want)
	if err != nil {
		return 0, err
	}
	px := passengersFor(ctx, s.DB, t)
	var pending int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM trip_price_quotes WHERE trip_id = ? AND status = 'pending'`, tripID).Scan(&pending); err != nil {
		return 0, err
	}
	if pending > 0 {
		return 0, invalid("a price search is already running for this trip")
	}

	fresh := s.Now().Add(-priceCacheFor).UTC().Format(time.RFC3339)
	var jobs []searchJob
	for _, d := range dates {
		for _, kind := range []string{"flights", "hotels"} {
			if kind == "hotels" && (dest.City == "" || d.End == d.Start) {
				continue
			}
			hash := paramsHash(kind, d.Start, d.End, dest.key(), px)
			var cached int
			_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM trip_price_quotes WHERE trip_id = ? AND params_hash = ?
				AND status = 'done' AND searched_at >= ?`, tripID, hash, fresh).Scan(&cached)
			if cached > 0 {
				continue
			}
			jobs = append(jobs, searchJob{kind: kind, dates: d})
		}
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	if q := s.QuotaNow(ctx); q.Used+len(jobs) > q.Limit {
		return 0, fmt.Errorf("%w (%d of %d used; this needs %d)", ErrSearchQuota, q.Used, q.Limit, len(jobs))
	}
	ts := s.Now().UTC().Format(time.RFC3339)
	for i := range jobs {
		hash := paramsHash(jobs[i].kind, jobs[i].dates.Start, jobs[i].dates.End, dest.key(), px)
		res, err := s.DB.ExecContext(ctx, `INSERT INTO trip_price_quotes (trip_id, kind, start_date, end_date, params_hash, status,
			searched_at, currency, options) VALUES (?, ?, ?, ?, ?, 'pending', ?, 'NOK', '[]')`,
			tripID, jobs[i].kind, jobs[i].dates.Start, jobs[i].dates.End, hash, ts)
		if err != nil {
			return 0, fmt.Errorf("queue price search: %w", err)
		}
		jobs[i].id, _ = res.LastInsertId()
	}
	home := "BGO"
	if set, err := races.LoadResearchSettings(ctx, s.DB); err == nil && set.HomeAirport != "" {
		home = set.HomeAirport
	}
	go s.run(*dest, home, px, jobs)
	return len(jobs), nil
}

func (s *Searcher) run(dest Destination, home string, px Passengers, jobs []searchJob) {
	for _, j := range jobs {
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout+10*time.Second)
		q, err := s.search(ctx, j.kind, dest, home, px, j.dates)
		cancel()
		if err != nil {
			log.Printf("trips: %s search %s–%s: %v", j.kind, j.dates.Start, j.dates.End, err)
			_, _ = s.DB.Exec(`UPDATE trip_price_quotes SET status = 'failed', error = ? WHERE id = ?`, err.Error(), j.id)
			continue
		}
		_, _ = s.DB.Exec(`UPDATE trip_price_quotes SET status = 'done', cheapest = ?, price_level = ?, typical_low = ?,
			typical_high = ?, options = ?, link = ? WHERE id = ?`,
			q.Cheapest, q.PriceLevel, q.TypicalLow, q.TypicalHigh, string(q.Options), q.Link, j.id)
	}
}

func (s *Searcher) get(ctx context.Context, params url.Values, out any) error {
	params.Set("api_key", s.Key)
	params.Set("currency", "NOK")
	params.Set("hl", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Base+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("SerpApi: unreadable reply (HTTP %d)", resp.StatusCode)
	}
	if e, ok := raw["error"]; ok {
		var msg string
		_ = json.Unmarshal(e, &msg)
		return fmt.Errorf("SerpApi: %s", msg)
	}
	b, _ := json.Marshal(raw)
	return json.Unmarshal(b, out)
}

type serpFlight struct {
	Flights []struct {
		DepartureAirport struct {
			ID   string `json:"id"`
			Time string `json:"time"`
		} `json:"departure_airport"`
		ArrivalAirport struct {
			ID   string `json:"id"`
			Time string `json:"time"`
		} `json:"arrival_airport"`
		Airline      string `json:"airline"`
		FlightNumber string `json:"flight_number"`
	} `json:"flights"`
	Layovers      []json.RawMessage `json:"layovers"`
	TotalDuration int               `json:"total_duration"`
	Price         float64           `json:"price"`
}

type serpFlightsReply struct {
	BestFlights   []serpFlight `json:"best_flights"`
	OtherFlights  []serpFlight `json:"other_flights"`
	PriceInsights struct {
		LowestPrice       float64   `json:"lowest_price"`
		PriceLevel        string    `json:"price_level"`
		TypicalPriceRange []float64 `json:"typical_price_range"`
	} `json:"price_insights"`
	SearchMetadata struct {
		GoogleFlightsURL string `json:"google_flights_url"`
	} `json:"search_metadata"`
}

type serpHotelsReply struct {
	Properties []struct {
		Name          string  `json:"name"`
		HotelClass    string  `json:"hotel_class"`
		OverallRating float64 `json:"overall_rating"`
		Reviews       int     `json:"reviews"`
		Link          string  `json:"link"`
		RatePerNight  struct {
			Extracted float64 `json:"extracted_lowest"`
		} `json:"rate_per_night"`
		TotalRate struct {
			Extracted float64 `json:"extracted_lowest"`
		} `json:"total_rate"`
		GPS struct {
			Lat float64 `json:"latitude"`
			Lng float64 `json:"longitude"`
		} `json:"gps_coordinates"`
	} `json:"properties"`
	SearchMetadata struct {
		GoogleHotelsURL string `json:"google_hotels_url"`
	} `json:"search_metadata"`
}

func ptr(f float64) *float64 { return &f }

func (s *Searcher) search(ctx context.Context, kind string, dest Destination, home string, px Passengers, d DatePair) (*Quote, error) {
	q := &Quote{Kind: kind, Start: d.Start, End: d.End}
	if kind == "flights" {
		adults, children, infants := px.flightCounts()
		p := url.Values{}
		p.Set("engine", "google_flights")
		p.Set("departure_id", home)
		p.Set("arrival_id", strings.Join(dest.Airports, ","))
		p.Set("outbound_date", d.Start)
		if d.End != d.Start {
			p.Set("return_date", d.End)
		} else {
			p.Set("type", "2") // one way
		}
		p.Set("adults", strconv.Itoa(adults))
		if children > 0 {
			p.Set("children", strconv.Itoa(children))
		}
		if infants > 0 {
			p.Set("infants_on_lap", strconv.Itoa(infants))
		}
		var r serpFlightsReply
		if err := s.get(ctx, p, &r); err != nil {
			return nil, err
		}
		var opts []FlightOption
		for _, f := range append(r.BestFlights, r.OtherFlights...) {
			if f.Price <= 0 {
				continue
			}
			o := FlightOption{Price: f.Price, Minutes: f.TotalDuration, Stops: len(f.Layovers)}
			seen := map[string]bool{}
			for _, l := range f.Flights {
				o.Legs = append(o.Legs, FlightLeg{Airline: l.Airline, FlightNo: l.FlightNumber, From: l.DepartureAirport.ID,
					To: l.ArrivalAirport.ID, Dep: l.DepartureAirport.Time, Arr: l.ArrivalAirport.Time})
				if !seen[l.Airline] {
					seen[l.Airline] = true
					o.Airlines = append(o.Airlines, l.Airline)
				}
			}
			opts = append(opts, o)
		}
		sort.SliceStable(opts, func(i, j int) bool { return opts[i].Price < opts[j].Price })
		if len(opts) > maxOptions {
			opts = opts[:maxOptions]
		}
		if len(opts) > 0 {
			q.Cheapest = ptr(opts[0].Price)
		}
		if pi := r.PriceInsights; pi.PriceLevel != "" {
			q.PriceLevel = pi.PriceLevel
			if len(pi.TypicalPriceRange) == 2 {
				q.TypicalLow, q.TypicalHigh = ptr(pi.TypicalPriceRange[0]), ptr(pi.TypicalPriceRange[1])
			}
		}
		q.Options, _ = json.Marshal(opts)
		q.Link = r.SearchMetadata.GoogleFlightsURL
		return q, nil
	}

	query := dest.City + " hotels"
	if dest.HotelArea != "" {
		query = dest.HotelArea + ", " + dest.City
	}
	p := url.Values{}
	p.Set("engine", "google_hotels")
	p.Set("q", query)
	p.Set("check_in_date", d.Start)
	p.Set("check_out_date", d.End)
	p.Set("gl", "no")
	adults := px.Adults
	var ages []string
	for _, a := range px.ChildAges {
		ages = append(ages, strconv.Itoa(a))
	}
	p.Set("adults", strconv.Itoa(adults))
	if len(ages) > 0 {
		p.Set("children", strconv.Itoa(len(ages)))
		p.Set("children_ages", strings.Join(ages, ","))
	}
	var r serpHotelsReply
	if err := s.get(ctx, p, &r); err != nil {
		return nil, err
	}
	var opts []HotelOption
	for _, h := range r.Properties {
		if h.TotalRate.Extracted <= 0 {
			continue
		}
		opts = append(opts, HotelOption{Name: h.Name, Class: h.HotelClass, Rating: h.OverallRating, Reviews: h.Reviews,
			Total: h.TotalRate.Extracted, PerNight: h.RatePerNight.Extracted, Link: h.Link, Lat: h.GPS.Lat, Lng: h.GPS.Lng})
	}
	// Well-rated places first (4.0+), cheapest first within each group.
	sort.SliceStable(opts, func(i, j int) bool {
		gi, gj := opts[i].Rating >= 4, opts[j].Rating >= 4
		if gi != gj {
			return gi
		}
		return opts[i].Total < opts[j].Total
	})
	if len(opts) > maxOptions {
		opts = opts[:maxOptions]
	}
	for _, o := range opts {
		if q.Cheapest == nil || o.Total < *q.Cheapest {
			q.Cheapest = ptr(o.Total)
		}
	}
	q.Options, _ = json.Marshal(opts)
	q.Link = r.SearchMetadata.GoogleHotelsURL
	return q, nil
}

// PriceView is what the trip page shows: the latest search per kind and
// dates, plus the month's quota.
type PriceView struct {
	Quotes     []Quote    `json:"quotes"`
	Quota      Quota      `json:"quota"`
	Passengers Passengers `json:"passengers"`
	Home       string     `json:"home"`
}

// Prices returns the newest quote per kind and dates (and its history count).
func (s *Searcher) Prices(ctx context.Context, tripID, userID int64) (*PriceView, error) {
	t, err := GetTrip(ctx, s.DB, tripID, userID)
	if err != nil {
		return nil, err
	}
	v := &PriceView{Quotes: []Quote{}, Quota: s.QuotaNow(ctx), Passengers: passengersFor(ctx, s.DB, t), Home: "BGO"}
	if set, err := races.LoadResearchSettings(ctx, s.DB); err == nil && set.HomeAirport != "" {
		v.Home = set.HomeAirport
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, start_date, end_date, status, searched_at, cheapest, currency,
		price_level, typical_low, typical_high, options, link, error FROM trip_price_quotes q
		WHERE trip_id = ? AND id = (SELECT MAX(id) FROM trip_price_quotes x WHERE x.trip_id = q.trip_id AND x.kind = q.kind
			AND x.start_date = q.start_date AND x.end_date = q.end_date)
		ORDER BY start_date, kind`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list prices: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var q Quote
		var cheapest, low, high sql.NullFloat64
		var opts string
		if err := rows.Scan(&q.ID, &q.Kind, &q.Start, &q.End, &q.Status, &q.SearchedAt, &cheapest, &q.Currency, &q.PriceLevel,
			&low, &high, &opts, &q.Link, &q.Error); err != nil {
			return nil, err
		}
		if cheapest.Valid {
			q.Cheapest = &cheapest.Float64
		}
		if low.Valid {
			q.TypicalLow = &low.Float64
		}
		if high.Valid {
			q.TypicalHigh = &high.Float64
		}
		q.Options = json.RawMessage(opts)
		v.Quotes = append(v.Quotes, q)
	}
	return v, rows.Err()
}

// CostSummary totals a trip's costs in NOK.
type CostSummary struct {
	TotalNOK     float64            `json:"total_nok"`
	PerPersonNOK float64            `json:"per_person_nok"`
	ByCategory   map[string]float64 `json:"by_category"`
	BookedNOK    float64            `json:"booked_nok"` // booked or paid
	Unconverted  []string           `json:"unconverted,omitempty"`
}

// SummarizeCosts converts and totals doc.costs.
func SummarizeCosts(ctx context.Context, db *sql.DB, d Doc) CostSummary {
	sum := CostSummary{ByCategory: map[string]float64{}}
	if len(d.Costs) == 0 {
		return sum
	}
	rates, err := currency.LatestNOKRates(ctx, db)
	if err != nil {
		rates = map[string]float64{"NOK": 1}
	}
	for _, c := range d.Costs {
		rate, ok := rates[c.Currency]
		if !ok {
			sum.Unconverted = append(sum.Unconverted, c.Currency)
			continue
		}
		nok := math.Round(c.Amount * rate)
		sum.TotalNOK += nok
		sum.ByCategory[c.Category] += nok
		if c.Status == "booked" || c.Status == "paid" {
			sum.BookedNOK += nok
		}
	}
	if n := len(d.Travellers); n > 0 {
		sum.PerPersonNOK = math.Round(sum.TotalNOK / float64(n))
	}
	return sum
}
