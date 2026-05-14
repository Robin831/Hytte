package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-pokemon-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	t.Cleanup(func() { d.Close() })
	return d
}

// newTestClient builds a Client pointed at the given test server, with sleeps
// stubbed so 429 retries don't block real wall-clock time.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient().WithBaseURL(srv.URL).WithHTTPClient(srv.Client())
	c.withSleep(func(_ context.Context, _ time.Duration) error { return nil })
	c.maxRetries = 2
	return c
}

func TestSyncSets_SinglePage(t *testing.T) {
	d := setupTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/sets") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resp := SetsResponse{
			Page:       1,
			PageSize:   PageSize,
			Count:      2,
			TotalCount: 2,
			Data: []Set{
				{
					ID:           "base1",
					Name:         "Base Set",
					Series:       "Base",
					PrintedTotal: 102,
					Total:        102,
					ReleaseDate:  "1999/01/09",
					Images:       SetImages{Symbol: "https://example/sym.png", Logo: "https://example/logo.png"},
				},
				{
					ID:          "jungle",
					Name:        "Jungle",
					Series:      "Base",
					Total:       64,
					ReleaseDate: "1999/06/16",
				},
			},
		}
		writeJSON(t, w, resp)
	}))
	defer srv.Close()

	if err := SyncSets(context.Background(), d, newTestClient(srv)); err != nil {
		t.Fatalf("SyncSets: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pokemon_sets`).Scan(&count); err != nil {
		t.Fatalf("count sets: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 sets, got %d", count)
	}

	var name, logo string
	var total int
	if err := d.QueryRow(`SELECT name, total_cards, logo_url FROM pokemon_sets WHERE id = ?`, "base1").Scan(&name, &total, &logo); err != nil {
		t.Fatalf("read base1: %v", err)
	}
	if name != "Base Set" || total != 102 || logo != "https://example/logo.png" {
		t.Fatalf("unexpected base1 row: name=%q total=%d logo=%q", name, total, logo)
	}
}

func TestSyncSets_Pagination(t *testing.T) {
	d := setupTestDB(t)

	totalCount := PageSize*2 + 5 // three pages: full, full, partial
	var hits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		pageStr := r.URL.Query().Get("page")
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}

		var pageData []Set
		switch page {
		case 1, 2:
			pageData = makeSetPage(page, PageSize)
		case 3:
			pageData = makeSetPage(page, 5)
		default:
			pageData = nil
		}
		writeJSON(t, w, SetsResponse{
			Page:       page,
			PageSize:   PageSize,
			Count:      len(pageData),
			TotalCount: totalCount,
			Data:       pageData,
		})
	}))
	defer srv.Close()

	if err := SyncSets(context.Background(), d, newTestClient(srv)); err != nil {
		t.Fatalf("SyncSets: %v", err)
	}
	if hits < 3 {
		t.Fatalf("expected at least 3 page requests, got %d", hits)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pokemon_sets`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != totalCount {
		t.Fatalf("expected %d sets, got %d", totalCount, count)
	}
}

func TestSyncCards_InsertsCardsAndVariants(t *testing.T) {
	d := setupTestDB(t)

	// Seed the set so the cards row's FK passes.
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "swsh1", "Sword & Shield", "Sword & Shield", "2020/02/07", 202, time.Now()); err != nil {
		t.Fatalf("seed set: %v", err)
	}

	card := Card{
		ID:     "swsh1-1",
		Name:   "Celebi V",
		Number: "001",
		Rarity: "Rare Holo V",
		Images: CardImages{
			Small: "https://example/sm.png",
			Large: "https://example/lg.png",
		},
		Cardmarket: Cardmarket{
			Prices: CardmarketPrices{
				AverageSellPrice: 1.5,
				TrendPrice:       1.7,
				ReverseHoloSell:  2.0,
				ReverseHoloTrend: 0, // trend missing -> fallback to ReverseHoloSell
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.QueryUnescape(r.URL.RawQuery)
		if !strings.Contains(q, "q=set.id:swsh1") {
			t.Fatalf("expected set.id filter in query, got %s", q)
		}
		writeJSON(t, w, CardsResponse{
			Page:       1,
			PageSize:   PageSize,
			Count:      1,
			TotalCount: 1,
			Data:       []Card{card},
		})
	}))
	defer srv.Close()

	if err := SyncCards(context.Background(), d, newTestClient(srv), "swsh1"); err != nil {
		t.Fatalf("SyncCards: %v", err)
	}

	var name, rarity string
	if err := d.QueryRow(`SELECT name, rarity FROM pokemon_cards WHERE id = ?`, "swsh1-1").Scan(&name, &rarity); err != nil {
		t.Fatalf("read card: %v", err)
	}
	if name != "Celebi V" || rarity != "Rare Holo V" {
		t.Fatalf("unexpected card row: name=%q rarity=%q", name, rarity)
	}

	rows, err := d.Query(`SELECT kind, price_eur FROM pokemon_card_variants WHERE card_id = ? ORDER BY kind`, "swsh1-1")
	if err != nil {
		t.Fatalf("query variants: %v", err)
	}
	defer rows.Close()

	got := map[string]float64{}
	for rows.Next() {
		var k string
		var p float64
		if err := rows.Scan(&k, &p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[k] = p
	}

	want := map[string]float64{
		"normal":           1.7, // trendPrice preferred
		"reverse_holofoil": 2.0, // reverseHoloTrend=0 -> fallback to reverseHoloSell
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d variants, got %d: %+v", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("variant %s: expected %v, got %v", k, v, got[k])
		}
	}
}

func TestSyncCards_FallbackNormalVariantWithoutPrices(t *testing.T) {
	cases := []struct {
		name   string
		prices CardmarketPrices
		want   map[string]float64
	}{
		{
			name:   "no cardmarket prices -> single normal row at 0",
			prices: CardmarketPrices{},
			want:   map[string]float64{"normal": 0},
		},
		{
			name:   "normal trendPrice present -> placeholder overwritten",
			prices: CardmarketPrices{TrendPrice: 12.34},
			want:   map[string]float64{"normal": 12.34},
		},
		{
			name:   "reverse holo price only -> normal placeholder kept at 0 alongside reverse_holofoil",
			prices: CardmarketPrices{ReverseHoloTrend: 5.0},
			want: map[string]float64{
				"normal":           0,
				"reverse_holofoil": 5.0,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := setupTestDB(t)
			if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
				VALUES (?, ?, ?, ?, ?, ?)`, "me1", "Mega Evolution", "Mega Evolution", "2026/01/01", 1, time.Now()); err != nil {
				t.Fatalf("seed set: %v", err)
			}

			card := Card{
				ID:         "me1-1",
				Name:       "Mega Charizard ex",
				Number:     "1",
				Cardmarket: Cardmarket{Prices: tc.prices},
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, CardsResponse{
					Page: 1, PageSize: PageSize, Count: 1, TotalCount: 1,
					Data: []Card{card},
				})
			}))
			defer srv.Close()

			if err := SyncCards(context.Background(), d, newTestClient(srv), "me1"); err != nil {
				t.Fatalf("SyncCards: %v", err)
			}

			rows, err := d.Query(`SELECT kind, price_eur FROM pokemon_card_variants WHERE card_id = ? ORDER BY kind`, "me1-1")
			if err != nil {
				t.Fatalf("query variants: %v", err)
			}
			defer rows.Close()

			got := map[string]float64{}
			for rows.Next() {
				var k string
				var p float64
				if err := rows.Scan(&k, &p); err != nil {
					t.Fatalf("scan: %v", err)
				}
				got[k] = p
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d variant rows, got %d: %+v", len(tc.want), len(got), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("variant %s: expected %v, got %v", k, v, got[k])
				}
			}
		})
	}
}

func TestSyncAll_BackfillCreatesNormalVariantForOrphanCards(t *testing.T) {
	d := setupTestDB(t)
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "me1", "Mega Evolution", "Mega Evolution", "2026/01/01", 1, now); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"me1-1", "me1", "Mega Charizard ex", "1", "Rare", "", "", now); err != nil {
		t.Fatalf("seed orphan card: %v", err)
	}

	var existing int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pokemon_card_variants WHERE card_id = ?`, "me1-1").Scan(&existing); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if existing != 0 {
		t.Fatalf("expected 0 variants before backfill, got %d", existing)
	}

	n, err := backfillNormalVariants(context.Background(), d)
	if err != nil {
		t.Fatalf("backfillNormalVariants: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row backfilled, got %d", n)
	}

	var kind string
	var price float64
	if err := d.QueryRow(`SELECT kind, price_eur FROM pokemon_card_variants WHERE card_id = ?`, "me1-1").Scan(&kind, &price); err != nil {
		t.Fatalf("read variant: %v", err)
	}
	if kind != "normal" || price != 0 {
		t.Fatalf("expected (normal,0), got (%s,%v)", kind, price)
	}

	// Idempotent: a second pass does not duplicate rows.
	n2, err := backfillNormalVariants(context.Background(), d)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 rows on second pass, got %d", n2)
	}
}

func TestSyncAll_BackfillAddsNormalWhenOnlyNonNormalVariantExists(t *testing.T) {
	d := setupTestDB(t)
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "me2", "Mega Evolution 2", "Mega Evolution", "2026/02/01", 1, now); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"me2-1", "me2", "Mega Blastoise ex", "1", "Rare", "", "", now); err != nil {
		t.Fatalf("seed card: %v", err)
	}
	// Card already has a reverse_holofoil variant but no normal variant.
	if _, err := d.Exec(`INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at) VALUES (?, 'reverse_holofoil', 3.5, ?)`,
		"me2-1", now); err != nil {
		t.Fatalf("seed reverse_holofoil variant: %v", err)
	}

	n, err := backfillNormalVariants(context.Background(), d)
	if err != nil {
		t.Fatalf("backfillNormalVariants: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row backfilled, got %d", n)
	}

	rows, err := d.Query(`SELECT kind, price_eur FROM pokemon_card_variants WHERE card_id = ? ORDER BY kind`, "me2-1")
	if err != nil {
		t.Fatalf("query variants: %v", err)
	}
	defer rows.Close()
	got := map[string]float64{}
	for rows.Next() {
		var k string
		var p float64
		if err := rows.Scan(&k, &p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[k] = p
	}
	want := map[string]float64{
		"normal":           0,
		"reverse_holofoil": 3.5,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d variants, got %d: %+v", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("variant %s: expected %v, got %v", k, v, got[k])
		}
	}

	// Idempotent: reverse_holofoil price must not be touched.
	n2, err := backfillNormalVariants(context.Background(), d)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 rows on second pass, got %d", n2)
	}
}

func TestSyncCards_VariantUpsertUpdatesPrice(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "s1", "S", "S", "2024/01/01", 1, time.Now()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var hits int32
	prices := []CardmarketPrices{
		{TrendPrice: 1.0},
		{TrendPrice: 2.5},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&hits, 1) - 1
		card := Card{
			ID:         "s1-1",
			Name:       "Pikachu",
			Number:     "1",
			Cardmarket: Cardmarket{Prices: prices[idx]},
		}
		writeJSON(t, w, CardsResponse{TotalCount: 1, Count: 1, PageSize: PageSize, Page: 1, Data: []Card{card}})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := SyncCards(context.Background(), d, c, "s1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := SyncCards(context.Background(), d, c, "s1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var p float64
	if err := d.QueryRow(`SELECT price_eur FROM pokemon_card_variants WHERE card_id = ? AND kind = ?`, "s1-1", "normal").Scan(&p); err != nil {
		t.Fatalf("read price: %v", err)
	}
	if p != 2.5 {
		t.Fatalf("expected updated price 2.5, got %v", p)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pokemon_card_variants WHERE card_id = ?`, "s1-1").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one variant row after upsert, got %d", count)
	}
}

func TestDoRequest_RetriesOn429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(t, w, SetsResponse{TotalCount: 0, Count: 0, PageSize: PageSize, Page: 1, Data: []Set{}})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	var resp SetsResponse
	if err := c.doRequest(context.Background(), srv.URL+"/sets?page=1", &resp); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if hits != 2 {
		t.Fatalf("expected 2 attempts (first 429, second OK), got %d", hits)
	}
}

func TestAdminSyncHandler_Returns202(t *testing.T) {
	d := setupTestDB(t)
	handler := adminSyncHandler(d, func(_ context.Context, _ *sql.DB) error {
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/pokemon/admin/sync", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "started" {
		t.Fatalf("expected status=started, got %q", body["status"])
	}
}

func TestAdminSyncHandler_Returns409WhenAlreadyRunning(t *testing.T) {
	d := setupTestDB(t)
	started := make(chan struct{})
	done := make(chan struct{})

	handler := adminSyncHandler(d, func(_ context.Context, _ *sql.DB) error {
		close(started)
		<-done
		return nil
	})

	// First request — starts the sync.
	req1 := httptest.NewRequest(http.MethodPost, "/pokemon/admin/sync", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d", w1.Code)
	}

	// Wait for the goroutine to be inside doSync.
	<-started

	// Second request — should conflict.
	req2 := httptest.NewRequest(http.MethodPost, "/pokemon/admin/sync", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second request: expected 409, got %d", w2.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w2.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected non-empty error field in 409 response")
	}

	// Unblock the sync goroutine.
	close(done)
}

func TestNextWeeklySync_SundayAtFourOslo(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "Sunday before 04:00 returns same day",
			now:  time.Date(2026, 5, 17, 2, 0, 0, 0, oslo), // Sunday
			want: time.Date(2026, 5, 17, 4, 0, 0, 0, oslo),
		},
		{
			name: "Sunday after 04:00 returns next Sunday",
			now:  time.Date(2026, 5, 17, 5, 0, 0, 0, oslo),
			want: time.Date(2026, 5, 24, 4, 0, 0, 0, oslo),
		},
		{
			name: "Wednesday returns upcoming Sunday",
			now:  time.Date(2026, 5, 13, 9, 0, 0, 0, oslo),
			want: time.Date(2026, 5, 17, 4, 0, 0, 0, oslo),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextWeeklySync(tc.now, oslo)
			if !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if got.Weekday() != time.Sunday {
				t.Errorf("expected Sunday, got %v", got.Weekday())
			}
			if h := got.In(oslo).Hour(); h != 4 {
				t.Errorf("expected hour 4, got %d", h)
			}
		})
	}
}

func TestNextDailyPriceRefresh_SevenAMOslo(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	now := time.Date(2026, 5, 13, 8, 0, 0, 0, oslo) // after 07:00
	got := NextDailyPriceRefresh(now, oslo)
	want := time.Date(2026, 5, 14, 7, 0, 0, 0, oslo)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	now2 := time.Date(2026, 5, 13, 6, 0, 0, 0, oslo) // before 07:00
	got2 := NextDailyPriceRefresh(now2, oslo)
	want2 := time.Date(2026, 5, 13, 7, 0, 0, 0, oslo)
	if !got2.Equal(want2) {
		t.Errorf("same-day case: got %v, want %v", got2, want2)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func makeSetPage(page, n int) []Set {
	out := make([]Set, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%d-%d", page, i)
		out[i] = Set{
			ID:          id,
			Name:        id,
			Series:      "Test",
			Total:       1,
			ReleaseDate: "2024/01/01",
		}
	}
	return out
}
