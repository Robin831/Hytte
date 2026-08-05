package offers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-offers-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'a@example.com', 'A', '', 'g1', ''), (2, 'b@example.com', 'B', '', 'g2', '')`)
	if err != nil {
		t.Fatalf("insert test users: %v", err)
	}
	return database
}

// noPause disables the politeness delay for the duration of a test.
func noPause(t *testing.T) {
	t.Helper()
	orig := requestPause
	requestPause = func(context.Context) {}
	t.Cleanup(func() { requestPause = orig })
}

func testOffer(id string, price, prePrice float64) Offer {
	today := time.Now().UTC()
	return Offer{
		ID: id, DealerID: "faa0Ym", DealerName: "REMA 1000",
		Heading: "Item " + id, Price: price, PrePrice: prePrice, Currency: "NOK",
		RunFrom: today.AddDate(0, 0, -1).Format("2006-01-02"),
		RunTill: today.AddDate(0, 0, 5).Format("2006-01-02"),
	}
}

func TestFetchDealerOffersPaginatesAndConverts(t *testing.T) {
	noPause(t)
	pre := 49.9
	size := 1.75
	pieces := 24.0

	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		offset := r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		if offset == "0" {
			// Full first page: repeat one offer to trigger pagination.
			batch := make([]map[string]any, pageLimit)
			for i := range batch {
				batch[i] = map[string]any{"id": fmt.Sprintf("a%d", i), "heading": "X",
					"pricing":  map[string]any{"price": 10.0, "currency": "NOK"},
					"run_from": "2026-07-26T22:00:00+0000", "run_till": "2026-08-01T21:59:59+0000",
					"dealer_id": "faa0Ym"}
			}
			json.NewEncoder(w).Encode(batch) //nolint:errcheck
			return
		}
		// Short second page with unit-price and multipack cases.
		fmt.Fprintf(w, `[
			{"id":"milk","heading":"TINE HELMELK","pricing":{"price":36.4,"pre_price":%v,"currency":"NOK"},
			 "quantity":{"unit":{"symbol":"l"},"size":{"from":%v},"pieces":{"from":1}},
			 "images":{"thumb":"https://img/milk"},"run_from":"2026-07-26T22:00:00+0000","run_till":"2026-08-01T21:59:59+0000",
			 "dealer_id":"faa0Ym","dealer":{"name":"REMA 1000"}},
			{"id":"tp","heading":"TOALETTPAPIR 24PK","pricing":{"price":79.9,"currency":"NOK"},
			 "quantity":{"pieces":{"from":%v},"size":{}},
			 "run_from":"2026-07-26T22:00:00+0000","run_till":"2026-08-01T21:59:59+0000","dealer_id":"faa0Ym"}
		]`, pre, size, pieces)
	}))
	defer srv.Close()
	overrideBaseURL = srv.URL
	t.Cleanup(func() { overrideBaseURL = "" })

	got, err := FetchDealerOffers(context.Background(), "faa0Ym")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != pageLimit+2 {
		t.Fatalf("got %d offers, want %d", len(got), pageLimit+2)
	}
	if len(requests) != 2 {
		t.Fatalf("made %d requests, want 2", len(requests))
	}

	milk := got[pageLimit]
	if milk.PrePrice != 49.9 || milk.UnitPrice != 20.8 || milk.UnitLabel != "l" || milk.RunTill != "2026-08-01" || milk.ImageURL != "https://img/milk" {
		t.Errorf("milk conversion wrong: %+v", milk)
	}
	tp := got[pageLimit+1]
	if tp.PrePrice != 0 || tp.UnitLabel != "stk" || tp.UnitPrice != 3.33 {
		t.Errorf("multipack conversion wrong: %+v", tp)
	}
	// Dealer name falls back to the Dealers map when the payload omits it.
	if tp.DealerName != "REMA 1000" {
		t.Errorf("dealer name fallback = %q", tp.DealerName)
	}
}

func TestFetchDealerOffersRetriesOn429(t *testing.T) {
	noPause(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	overrideBaseURL = srv.URL
	t.Cleanup(func() { overrideBaseURL = "" })

	got, err := FetchDealerOffers(context.Background(), "faa0Ym")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 0 || calls != 2 {
		t.Errorf("got %d offers after %d calls, want 0 offers on 2nd call", len(got), calls)
	}
}

func TestUpsertListPurge(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	offers := []Offer{testOffer("a", 10, 20), testOffer("b", 5, 0)}
	if err := UpsertOffers(ctx, d, offers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Second upsert with a changed price replaces, not duplicates.
	offers[0].Price = 12
	if err := UpsertOffers(ctx, d, offers); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	current, err := ListCurrent(d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(current) != 2 {
		t.Fatalf("got %d current offers, want 2", len(current))
	}
	for _, o := range current {
		if o.ID == "a" && o.Price != 12 {
			t.Errorf("upsert did not replace price: %+v", o)
		}
	}

	// An offer that expired long ago is neither listed nor kept after purge.
	old := testOffer("old", 1, 0)
	old.RunFrom = "2026-01-01"
	old.RunTill = "2026-01-07"
	if err := UpsertOffers(ctx, d, []Offer{old}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	current, _ = ListCurrent(d)
	if len(current) != 2 {
		t.Errorf("expired offer leaked into current list (%d)", len(current))
	}
	purged, err := PurgeExpired(ctx, d)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged %d rows, want 1", purged)
	}

	last, err := LastFetchedAt(d)
	if err != nil || last.IsZero() {
		t.Errorf("LastFetchedAt = %v, %v; want recent time", last, err)
	}
}

func TestRank(t *testing.T) {
	offers := []Offer{
		{ID: "1", Heading: "TINE HELMELK", Price: 36.4, PrePrice: 49.9},
		{ID: "2", Heading: "Grandiosa Pizza", Price: 39.9, PrePrice: 49.9},
		{ID: "3", Heading: "OMO Tøyvaskemiddel", Description: "flytende", Price: 89},
		{ID: "4", Heading: "Melkesjokolade", Price: 25, PrePrice: 50},
		{ID: "5", Heading: "Bananer", Price: 19.9},
	}
	ranked := Rank(offers, []string{"melk", "vaskemiddel"})

	// Watchlist matches first: helmelk (compound match) and vaskemiddel.
	// Melkesjokolade must NOT match "melk" despite its 50% discount.
	if len(ranked[0].MatchedKeywords) == 0 || len(ranked[1].MatchedKeywords) == 0 {
		t.Fatalf("watchlist matches not ranked first: %+v", ranked[:2])
	}
	matched := map[string]bool{}
	for _, r := range ranked[:2] {
		matched[r.ID] = true
	}
	if !matched["1"] || !matched["3"] {
		t.Errorf("expected offers 1 and 3 on top, got %+v", ranked[:2])
	}
	// Then best discount: melkesjokolade 50% before pizza 20%.
	if ranked[2].ID != "4" || ranked[2].DiscountPct != 50 {
		t.Errorf("ranked[2] = %+v, want melkesjokolade at 50%%", ranked[2])
	}
	if ranked[3].ID != "2" || ranked[3].DiscountPct != 20 {
		t.Errorf("ranked[3] = %+v, want pizza at 20%%", ranked[3])
	}
	// Discount-less offers last.
	if ranked[4].ID != "5" || ranked[4].DiscountPct != 0 {
		t.Errorf("ranked[4] = %+v, want bananer last", ranked[4])
	}
}

func TestNextDailyRun(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	before := time.Date(2026, 7, 28, 5, 0, 0, 0, oslo)
	next := NextDailyRun(before, oslo)
	if next.Day() != 28 || next.Hour() != 6 || next.Minute() != 30 {
		t.Errorf("before run: next = %v, want same-day 06:30", next)
	}
	after := time.Date(2026, 7, 28, 7, 0, 0, 0, oslo)
	next = NextDailyRun(after, oslo)
	if next.Day() != 29 || next.Hour() != 6 || next.Minute() != 30 {
		t.Errorf("after run: next = %v, want next-day 06:30", next)
	}
}

func TestWatchlistCRUD(t *testing.T) {
	d := setupTestDB(t)

	entry, err := AddWatchlist(d, 1, "melk")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := AddWatchlist(d, 1, "Melk"); err != ErrDuplicateKeyword {
		t.Errorf("duplicate add = %v, want ErrDuplicateKeyword", err)
	}
	// Same keyword for another user is fine.
	if _, err := AddWatchlist(d, 2, "melk"); err != nil {
		t.Errorf("other user add: %v", err)
	}

	// Keyword encrypted at rest.
	var raw string
	if err := d.QueryRow("SELECT keyword FROM offer_watchlist WHERE id = ?", entry.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if raw == "melk" {
		t.Error("keyword stored in plaintext")
	}

	if err := DeleteWatchlist(d, entry.ID, 2); err != sql.ErrNoRows {
		t.Errorf("delete as other user = %v, want sql.ErrNoRows", err)
	}
	if err := DeleteWatchlist(d, entry.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ := ListWatchlist(d, 1)
	if len(list) != 0 {
		t.Errorf("watchlist has %d entries after delete, want 0", len(list))
	}
}

// quantityOffer builds a tjekOffer with just the pricing and quantity fields
// convertOffer derives the unit price from. A nil symbol means the payload had
// no unit object at all.
func quantityOffer(price float64, symbol *string, size, pieces float64) tjekOffer {
	var t tjekOffer
	t.Pricing.Price = price
	if symbol != nil {
		t.Quantity.Unit = &struct {
			Symbol string `json:"symbol"`
		}{Symbol: *symbol}
	}
	if size > 0 {
		t.Quantity.Size.From = &size
	}
	if pieces > 0 {
		t.Quantity.Pieces.From = &pieces
	}
	return t
}

func TestConvertOfferUnitPriceNormalisation(t *testing.T) {
	sym := func(s string) *string { return &s }

	tests := []struct {
		name      string
		price     float64
		symbol    *string
		size      float64
		pieces    float64
		wantPrice float64
		wantLabel string
	}{
		{"grams scale to kg", 45, sym("g"), 500, 0, 90, "kg"},
		{"kg passes through", 89, sym("kg"), 1, 0, 89, "kg"},
		{"multi-kg divides", 60, sym("kg"), 2.5, 0, 24, "kg"},
		{"millilitres scale to litre", 20, sym("ml"), 500, 0, 40, "l"},
		{"centilitres scale to litre", 15, sym("cl"), 33, 0, 45.45, "l"},
		{"decilitres scale to litre", 30, sym("dl"), 5, 0, 60, "l"},
		{"litres pass through", 36.4, sym("l"), 1.75, 0, 20.8, "l"},
		{"symbol matching ignores case and whitespace", 45, sym(" G "), 500, 0, 90, "kg"},
		{"upper-case kg", 89, sym(" KG "), 1, 0, 89, "kg"},
		{"unknown symbol falls back to pieces", 79.9, sym("bundt"), 3, 24, 3.33, "stk"},
		{"empty symbol falls back to pieces", 79.9, sym(""), 3, 24, 3.33, "stk"},
		{"missing unit falls back to pieces", 79.9, nil, 0, 24, 3.33, "stk"},
		{"unknown symbol without pieces yields nothing", 79.9, sym("bundt"), 3, 0, 0, ""},
		{"stk symbol without pieces yields nothing", 79.9, sym("stk"), 3, 0, 0, ""},
		{"single piece yields nothing", 12, sym("pcs"), 1, 1, 0, ""},
		{"pcs multipack uses pieces", 79.9, sym("pcs"), 24, 24, 3.33, "stk"},
		{"zero size with single piece yields nothing", 12, sym("kg"), 0, 1, 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convertOffer(quantityOffer(tc.price, tc.symbol, tc.size, tc.pieces))
			if diff := got.UnitPrice - tc.wantPrice; diff > 0.001 || diff < -0.001 {
				t.Errorf("UnitPrice = %v, want %v", got.UnitPrice, tc.wantPrice)
			}
			if got.UnitLabel != tc.wantLabel {
				t.Errorf("UnitLabel = %q, want %q", got.UnitLabel, tc.wantLabel)
			}
		})
	}
}

func TestNormaliseUnit(t *testing.T) {
	for _, tc := range []struct {
		symbol     string
		wantLabel  string
		wantFactor float64
		wantOK     bool
	}{
		{"g", "kg", 0.001, true},
		{"kg", "kg", 1, true},
		{"ml", "l", 0.001, true},
		{"cl", "l", 0.01, true},
		{"dl", "l", 0.1, true},
		{"l", "l", 1, true},
		{"  Dl\t", "l", 0.1, true},
		{"stk", "", 0, false},
		{"pcs", "", 0, false},
		{"", "", 0, false},
		{"bundt", "", 0, false},
	} {
		label, factor, ok := normaliseUnit(tc.symbol)
		if ok != tc.wantOK || label != tc.wantLabel {
			t.Errorf("normaliseUnit(%q) = %q, %v, %v; want %q, %v, %v",
				tc.symbol, label, factor, ok, tc.wantLabel, tc.wantFactor, tc.wantOK)
			continue
		}
		if diff := factor - tc.wantFactor; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("normaliseUnit(%q) factor = %v, want %v", tc.symbol, factor, tc.wantFactor)
		}
	}
}
