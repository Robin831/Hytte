package trips

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSerp struct {
	mu    sync.Mutex
	calls []map[string]string
}

func (f *fakeSerp) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		f.mu.Lock()
		f.calls = append(f.calls, q)
		f.mu.Unlock()
		if q["engine"] == "google_flights" {
			_, _ = w.Write([]byte(`{"best_flights":[{"flights":[{"departure_airport":{"id":"BGO","time":"2026-11-20 12:55"},
				"arrival_airport":{"id":"CDG","time":"2026-11-20 16:40"},"airline":"KLM","flight_number":"KL 1176"}],
				"layovers":[{"id":"AMS"}],"total_duration":225,"price":5174}],
				"other_flights":[{"flights":[{"departure_airport":{"id":"BGO","time":"2026-11-20 06:00"},"arrival_airport":{"id":"ORY","time":"2026-11-20 09:00"},
				"airline":"Norwegian","flight_number":"DY 1234"}],"total_duration":180,"price":4822}],
				"price_insights":{"lowest_price":4822,"price_level":"typical","typical_price_range":[4300,6400]},
				"search_metadata":{"google_flights_url":"https://www.google.com/travel/flights?x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"properties":[
			{"name":"Cheap and grim","overall_rating":3.1,"total_rate":{"extracted_lowest":1200},"rate_per_night":{"extracted_lowest":600}},
			{"name":"Hôtel Crayon Rouge","hotel_class":"4-star hotel","overall_rating":4.6,"reviews":301,"link":"http://crayon.example",
			 "total_rate":{"extracted_lowest":4327},"rate_per_night":{"extracted_lowest":2164}}],
			"search_metadata":{"google_hotels_url":"https://www.google.com/travel/hotels?y"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func parisTrip(t *testing.T, ctx context.Context, d interface{}) TripInput {
	in := sampleInput()
	in.Kind = "holiday"
	in.Doc.Flights, in.Doc.Days = nil, nil
	in.Doc.Title = I18n{"en": "Paris weekend"}
	in.Doc.Destination = &Destination{City: "Paris", Airports: []string{"cdg", "ORY"}}
	in.Doc.Flex = &Flex{From: "2026-11-01", To: "2026-11-30", Nights: 2, DepartDays: []int{5}}
	return in
}

func waitDone(t *testing.T, s *Searcher, tripID int64) *PriceView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v, err := s.Prices(context.Background(), tripID, 1)
		if err != nil {
			t.Fatal(err)
		}
		pending := false
		for _, q := range v.Quotes {
			pending = pending || q.Status == "pending"
		}
		if !pending {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("searches never finished")
	return nil
}

func TestPriceSearchForCandidates(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	people, _ := ListPeople(ctx, d)
	wy := 2017
	_ = UpdatePerson(ctx, d, people[1].ID, PersonInput{BirthYear: &wy})
	in := parisTrip(t, ctx, d)
	in.Doc.Travellers = []Traveller{{PersonID: &people[0].ID}, {PersonID: &people[1].ID}}
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSerp{}
	srv := fake.server(t)
	s := &Searcher{DB: d, HTTP: srv.Client(), Base: srv.URL, Key: "k", Limit: 20, Now: func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }}

	n, err := s.Start(ctx, id, 1, SearchOptions{})
	if err != nil || n != 8 { // 4 Fridays × flights + hotels
		t.Fatalf("queued %d, %v", n, err)
	}
	v := waitDone(t, s, id)
	if len(v.Quotes) != 8 || v.Quota.Used != 8 {
		t.Fatalf("quotes = %d, quota = %+v", len(v.Quotes), v.Quota)
	}
	f := v.Quotes[0]
	if f.Kind != "flights" || f.Start != "2026-11-06" || f.Cheapest == nil || *f.Cheapest != 4822 || f.PriceLevel != "typical" || *f.TypicalHigh != 6400 {
		t.Fatalf("flights quote = %+v", f)
	}
	var fo []FlightOption
	_ = json.Unmarshal(f.Options, &fo)
	if len(fo) != 2 || fo[0].Legs[0].FlightNo != "DY 1234" || fo[1].Stops != 1 {
		t.Fatalf("flight options = %+v", fo)
	}
	h := v.Quotes[1]
	var ho []HotelOption
	_ = json.Unmarshal(h.Options, &ho)
	if h.Kind != "hotels" || len(ho) != 2 || ho[0].Name != "Hôtel Crayon Rouge" || *h.Cheapest != 1200 {
		t.Fatalf("hotels quote = %+v / %+v", h, ho)
	}
	// Passenger mix: Robin adult, William (9) a child, in both searches.
	first := fake.calls[0]
	if first["engine"] != "google_flights" || first["departure_id"] != "BGO" || first["arrival_id"] != "CDG,ORY" ||
		first["adults"] != "1" || first["children"] != "1" || first["currency"] != "NOK" || first["return_date"] != "2026-11-08" {
		t.Fatalf("flights params = %+v", first)
	}
	if hp := fake.calls[1]; hp["q"] != "Paris hotels" || hp["children_ages"] != "9" || hp["check_out_date"] != "2026-11-08" {
		t.Fatalf("hotels params = %+v", hp)
	}

	// The same search within half a day is reused, not repeated.
	if n, err := s.Start(ctx, id, 1, SearchOptions{}); err != nil || n != 0 {
		t.Fatalf("cached search queued %d, %v", n, err)
	}
	// Specific dates; over the monthly limit is refused.
	s.Limit = 9
	if _, err := s.Start(ctx, id, 1, SearchOptions{Dates: []DatePair{{"2026-12-04", "2026-12-06"}}}); !errors.Is(err, ErrSearchQuota) {
		t.Fatalf("quota: %v", err)
	}
}

func TestPriceSearchNeedsSetup(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := parisTrip(t, ctx, d)
	in.Doc.Destination = nil
	id, _ := CreateTrip(ctx, d, 1, in)
	s := &Searcher{DB: d, Key: "", Limit: 20, Now: time.Now}
	if _, err := s.Start(ctx, id, 1, SearchOptions{}); !errors.Is(err, ErrNoSerpAPI) {
		t.Fatalf("no key: %v", err)
	}
	s.Key = "k"
	if _, err := s.Start(ctx, id, 1, SearchOptions{}); !errors.Is(err, ErrNoDestination) {
		t.Fatalf("no destination: %v", err)
	}
	if _, err := s.Start(ctx, id, 3, SearchOptions{}); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
	bad := parisTrip(t, ctx, d)
	bad.Doc.Destination.Airports = []string{"Paris"}
	if _, err := CreateTrip(ctx, d, 1, bad); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad airport: %v", err)
	}
}

func TestCostSummary(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO currency_rates (pair, observed, rate, fetched_at) VALUES ('EUR/NOK', '2026-10-10', 11.5, '2026-10-10T16:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	in := sampleInput()
	in.Doc.Costs = []Cost{
		{Category: "flights", Label: I18n{"en": "KLM"}, Amount: 5174, Currency: "nok", Status: "booked"},
		{Category: "stay", Label: I18n{"en": "Hotel"}, Amount: 200, Currency: "EUR"},
		{Category: "food", Label: I18n{"en": "Food"}, Amount: 50, Currency: "XYZ"},
	}
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	cs := got.CostSummary
	if cs.TotalNOK != 7474 || cs.BookedNOK != 5174 || cs.ByCategory["stay"] != 2300 || cs.PerPersonNOK != 3737 || len(cs.Unconverted) != 1 {
		t.Fatalf("summary = %+v", cs)
	}
	if !strings.EqualFold(got.Doc.Costs[0].Currency, "NOK") || got.Doc.Costs[1].Status != "estimate" {
		t.Fatalf("normalized costs = %+v", got.Doc.Costs)
	}
}
