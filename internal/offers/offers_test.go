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
