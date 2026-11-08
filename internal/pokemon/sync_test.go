package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// fakeTCGdex serves /sets, /sets/{id} and /cards/{id} from maps and counts
// card fetches. Unknown ids 404, like the real API.
type fakeTCGdex struct {
	mu            sync.Mutex
	sets          []TCGdexSet
	cards         map[string]TCGdexCard
	cardHits      map[string]int
	failSetsAt    int             // when >0, GET /sets returns 500 this many times first
	missingImages map[string]bool // image URLs that 404
	guide         []priceGuideEntry
}

func (f *fakeTCGdex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.URL.Path == "/sets":
		if f.failSetsAt > 0 {
			f.failSetsAt--
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var briefs []TCGdexSetBrief
		for _, s := range f.sets {
			briefs = append(briefs, TCGdexSetBrief{ID: s.ID, Name: s.Name})
		}
		write(briefs)
	case strings.HasPrefix(r.URL.Path, "/sets/"):
		id := strings.TrimPrefix(r.URL.Path, "/sets/")
		for _, s := range f.sets {
			if s.ID == id {
				write(s)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, "/cards/"):
		id := strings.TrimPrefix(r.URL.Path, "/cards/")
		f.cardHits[id]++
		c, ok := f.cards[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		write(c)
	case r.URL.Path == "/price_guide_6.json":
		write(map[string]any{"version": 1, "priceGuides": f.guide})
	default:
		http.NotFound(w, r)
	}
}

// prices builds a card-level Cardmarket block; 0 leaves a field null.
func prices(trend, avg, trendHolo, avgHolo float64) TCGdexPricing {
	ptr := func(v float64) *float64 {
		if v <= 0 {
			return nil
		}
		return &v
	}
	return TCGdexPricing{Cardmarket: &TCGdexCardmarket{Trend: ptr(trend), Avg: ptr(avg), TrendHolo: ptr(trendHolo), AvgHolo: ptr(avgHolo)}}
}

func tcgSet(id, name, date, serieID, serieName string, cards ...TCGdexCardSummary) TCGdexSet {
	s := TCGdexSet{ID: id, Name: name, ReleaseDate: date, Cards: cards,
		Logo: "https://assets.tcgdex.net/en/x/" + id + "/logo", Symbol: "https://assets.tcgdex.net/univ/x/" + id + "/symbol"}
	s.Serie.ID, s.Serie.Name = serieID, serieName
	s.CardCount = TCGdexCardCount{Total: len(cards), Official: len(cards)}
	return s
}

func summary(id, local, name string) TCGdexCardSummary {
	return TCGdexCardSummary{ID: id, LocalID: local, Name: name, Image: "https://assets.tcgdex.net/en/x/" + id}
}

// newFakeTCGdex is the catalogue the sync tests run against: a set we have
// under a pokemontcg.io id (151), one that only matches by id (Base), a new
// release (Delta Reign), a TCG Pocket set and an old trainer kit.
func newFakeTCGdex() *fakeTCGdex {
	return &fakeTCGdex{
		sets: []TCGdexSet{
			tcgSet("sv03.5", "151", "2023-09-22", "sv", "Scarlet & Violet",
				summary("sv03.5-006", "006", "Charizard ex"), summary("sv03.5-210", "210", "Zapdos ex")),
			tcgSet("base1", "Base Set", "1999-01-09", "base", "Base", summary("base1-4", "4", "Charizard")),
			tcgSet("me06", "Delta Reign", "2026-11-06", "me", "Mega Evolution", summary("me06-001", "001", "Pikachu")),
			tcgSet("A1", "Genetic Apex", "2024-10-30", "tcgp", "Pokémon TCG Pocket", summary("A1-001", "001", "Bulbasaur")),
			tcgSet("tk-xy-b", "XY trainer Kit (Bisharp)", "2014-11-01", "tk", "Trainer kits", summary("tk-xy-b-1", "1", "Bisharp")),
		},
		cards: map[string]TCGdexCard{
			"sv03.5-006": {ID: "sv03.5-006", Rarity: "Double rare", Pricing: prices(2.5, 3, 0, 4)},
			"sv03.5-210": {ID: "sv03.5-210", Rarity: "Special illustration rare", Pricing: prices(40, 0, 0, 0)},
			"me06-001":   {ID: "me06-001", Rarity: "Common", Pricing: prices(0, 0.2, 0.5, 0)},
			// base1-4 has no detail: the card keeps its old data.
		},
		cardHits: map[string]int{},
	}
}

func newFakeClient(t *testing.T, f *fakeTCGdex) *TCGdexClient {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := NewTCGdexClient().WithTCGdexBaseURL(srv.URL).WithHTTPClient(srv.Client())
	c.sleep = func(context.Context, time.Duration) error { return nil }
	c.imageOK = func(_ context.Context, u string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return !f.missingImages[u]
	}
	prevGuide := cardmarketGuideURL
	cardmarketGuideURL = srv.URL + "/price_guide_6.json"
	t.Cleanup(func() { cardmarketGuideURL = prevGuide })
	return c
}

func TestRefreshPrices_UsesCardmarketGuide(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	seedPokemontcgCatalog(t, d)
	f := newFakeTCGdex()
	c := f.cards["sv03.5-210"]
	c.Pricing.Cardmarket.IDProduct = 733700
	c.Pricing.TCGPlayer = map[string]json.RawMessage{"unit": json.RawMessage(`"USD"`), "holofoil": json.RawMessage(`{"productId":502600}`)}
	f.cards["sv03.5-210"] = c
	if err := SyncAll(ctx, d, newFakeClient(t, f)); err != nil {
		t.Fatal(err)
	}
	var cm, tp int64
	if err := d.QueryRow(`SELECT cm_product_id, tcgplayer_id FROM pokemon_cards WHERE id = 'sv3pt5-210'`).Scan(&cm, &tp); err != nil || cm != 733700 || tp != 502600 {
		t.Fatalf("ids = %d %d, %v", cm, tp, err)
	}

	// The daily refresh prices the (unowned) card from the export without a
	// per-card TCGdex call.
	f2 := newFakeTCGdex()
	trend, holo := 55.5, 7.0
	f2.guide = []priceGuideEntry{{IDProduct: 733700, Trend: &trend, TrendHolo: &holo}}
	if err := RefreshPrices(ctx, d, newFakeClient(t, f2)); err != nil {
		t.Fatal(err)
	}
	if p := variantPrice(t, d, "sv3pt5-210", "normal"); p != 55.5 {
		t.Fatalf("normal = %v", p)
	}
	if p := variantPrice(t, d, "sv3pt5-210", "reverse_holofoil"); p != 7 {
		t.Fatalf("reverse = %v", p)
	}
	if f2.cardHits["sv03.5-210"] != 0 {
		t.Fatalf("card fetched from TCGdex: %v", f2.cardHits)
	}
	if got := CardmarketProductURL(733700); got != "https://www.cardmarket.com/en/Pokemon/Products?idProduct=733700&language=1&minCondition=2" {
		t.Fatalf("url = %s", got)
	}
}

// seedPokemontcgCatalog inserts sets and cards the way the old
// pokemontcg.io sync stored them.
func seedPokemontcgCatalog(t *testing.T, d *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	for _, s := range [][]string{
		{"sv3pt5", "151", "Scarlet & Violet", "2023/09/22"},
		{"base1", "Base", "Base", "1999/01/09"},
	} {
		if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, printed_total, symbol_url, logo_url, synced_at)
			VALUES (?, ?, ?, ?, 0, 0, 'old-symbol', 'old-logo', ?)`, s[0], s[1], s[2], s[3], now); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range [][]string{
		{"sv3pt5-6", "sv3pt5", "Charizard ex", "6", "Double Rare"},
		{"base1-4", "base1", "Charizard", "4", "Rare Holo"},
	} {
		if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
			VALUES (?, ?, ?, ?, ?, 'old-small', 'old-large', ?)`, c[0], c[1], c[2], c[3], c[4], now); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(`INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at) VALUES (?, 'normal', 1, ?)`, c[0], now); err != nil {
			t.Fatal(err)
		}
	}
}

func variantPrice(t *testing.T, d *sql.DB, cardID, kind string) float64 {
	t.Helper()
	var p float64
	if err := d.QueryRow(`SELECT price_eur FROM pokemon_card_variants WHERE card_id = ? AND kind = ?`, cardID, kind).Scan(&p); err != nil {
		t.Fatalf("price %s/%s: %v", cardID, kind, err)
	}
	return p
}

func TestSyncAll_LinksExistingCatalogAndAddsNewSets(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	seedPokemontcgCatalog(t, d)
	if first, err := NeedsTCGdexSync(ctx, d); err != nil || !first {
		t.Fatalf("NeedsTCGdexSync before = %v, %v", first, err)
	}
	f := newFakeTCGdex()
	f.failSetsAt = 1 // a transient 502 is retried
	f.missingImages = map[string]bool{"https://assets.tcgdex.net/en/x/base1-4/low.webp": true}
	if err := SyncAll(ctx, d, newFakeClient(t, f)); err != nil {
		t.Fatal(err)
	}

	// Existing sets keep their ids and names and gain the TCGdex link.
	for our, want := range map[string]string{"sv3pt5": "sv03.5", "base1": "base1"} {
		var tcg, name, logo string
		if err := d.QueryRow(`SELECT tcgdex_id, name, logo_url FROM pokemon_sets WHERE id = ?`, our).Scan(&tcg, &name, &logo); err != nil {
			t.Fatal(err)
		}
		if tcg != want || logo != "https://assets.tcgdex.net/en/x/"+want+"/logo.png" {
			t.Fatalf("%s: tcgdex_id=%q logo=%q", our, tcg, logo)
		}
		if our == "base1" && name != "Base" {
			t.Fatalf("base1 renamed to %q", name)
		}
	}

	// The existing card is linked by collector number ("006" = "6"), gets the
	// TCGdex image and price (trend first; holo stands in for normal), and
	// keeps its rarity label.
	var tcg, small, rarity string
	if err := d.QueryRow(`SELECT tcgdex_id, image_small_url, rarity FROM pokemon_cards WHERE id = 'sv3pt5-6'`).Scan(&tcg, &small, &rarity); err != nil {
		t.Fatal(err)
	}
	if tcg != "sv03.5-006" || small != "https://assets.tcgdex.net/en/x/sv03.5-006/low.webp" || rarity != "Double Rare" {
		t.Fatalf("sv3pt5-6 = %q %q %q", tcg, small, rarity)
	}
	if p := variantPrice(t, d, "sv3pt5-6", "normal"); p != 2.5 {
		t.Fatalf("sv3pt5-6 normal = %v", p)
	}
	if p := variantPrice(t, d, "sv3pt5-6", "reverse_holofoil"); p != 4 {
		t.Fatalf("sv3pt5-6 reverse = %v", p)
	}

	// A card we didn't have joins the existing set in pokemontcg.io style.
	var no string
	if err := d.QueryRow(`SELECT collector_no, rarity FROM pokemon_cards WHERE id = 'sv3pt5-210'`).Scan(&no, &rarity); err != nil {
		t.Fatal(err)
	}
	if no != "210" || rarity != "Special Illustration Rare" {
		t.Fatalf("sv3pt5-210 = %q %q", no, rarity)
	}

	// A TCGdex image whose file is missing doesn't replace a working one.
	var baseImg string
	if err := d.QueryRow(`SELECT image_small_url FROM pokemon_cards WHERE id = 'base1-4'`).Scan(&baseImg); err != nil || baseImg != "old-small" {
		t.Fatalf("base1-4 image = %q, %v", baseImg, err)
	}

	// A card with no detail on TCGdex keeps its price and rarity.
	if p := variantPrice(t, d, "base1-4", "normal"); p != 1 {
		t.Fatalf("base1-4 price changed to %v", p)
	}

	// Delta Reign is added under its TCGdex id; Pocket and old kits aren't.
	var name, series, date string
	if err := d.QueryRow(`SELECT name, series, release_date FROM pokemon_sets WHERE id = 'me06'`).Scan(&name, &series, &date); err != nil {
		t.Fatal(err)
	}
	if name != "Delta Reign" || series != "Mega Evolution" || date != "2026/11/06" {
		t.Fatalf("me06 = %q %q %q", name, series, date)
	}
	if err := d.QueryRow(`SELECT rarity FROM pokemon_cards WHERE id = 'me06-1'`).Scan(&rarity); err != nil || rarity != "Common" {
		t.Fatalf("me06-1 rarity = %q, %v", rarity, err)
	}
	if p := variantPrice(t, d, "me06-1", "normal"); p != 0.2 {
		t.Fatalf("me06-1 normal = %v", p)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pokemon_sets WHERE id IN ('A1', 'tk-xy-b')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pocket/kit sets added: %d, %v", n, err)
	}
	if first, _ := NeedsTCGdexSync(ctx, d); first {
		t.Fatal("NeedsTCGdexSync still true after sync")
	}

	// A second run links by tcgdex_id and adds nothing.
	var before, after int
	_ = d.QueryRow(`SELECT COUNT(*) FROM pokemon_cards`).Scan(&before)
	if err := SyncAll(ctx, d, newFakeClient(t, newFakeTCGdex())); err != nil {
		t.Fatal(err)
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM pokemon_cards`).Scan(&after)
	if before != after || before != 4 {
		t.Fatalf("cards before=%d after=%d", before, after)
	}
}

func TestRefreshPrices_OnlyOwnedCards(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	seedPokemontcgCatalog(t, d)
	if err := SyncAll(ctx, d, newFakeClient(t, newFakeTCGdex())); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'k@example.com', 'k', 'k')`); err != nil {
		t.Fatal(err)
	}
	var vid int64
	if err := d.QueryRow(`SELECT id FROM pokemon_card_variants WHERE card_id = 'sv3pt5-6' AND kind = 'normal'`).Scan(&vid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at)
		VALUES (1, 'sv3pt5-6', ?, 1, '', ?)`, vid, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	f := newFakeTCGdex()
	c := f.cards["sv03.5-006"]
	c.Pricing = prices(9, 0, 0, 0)
	f.cards["sv03.5-006"] = c
	f.cards["sv03.5-210"] = TCGdexCard{ID: "sv03.5-210", Pricing: prices(99, 0, 0, 0)}
	if err := RefreshPrices(ctx, d, newFakeClient(t, f)); err != nil {
		t.Fatal(err)
	}
	if p := variantPrice(t, d, "sv3pt5-6", "normal"); p != 9 {
		t.Fatalf("owned card price = %v", p)
	}
	if p := variantPrice(t, d, "sv3pt5-210", "normal"); p != 40 {
		t.Fatalf("unowned card refreshed: %v", p)
	}
	if f.cardHits["sv03.5-210"] != 0 || f.cardHits["sv03.5-006"] != 1 {
		t.Fatalf("card fetches = %v", f.cardHits)
	}
}

func TestMatchSetsPrefersStrongRules(t *testing.T) {
	ours := []ourSet{
		{id: "swsh12pt5", name: "Crown Zenith", releaseDate: "2023/01/20"},
		{id: "swsh12pt5gg", name: "Crown Zenith Galarian Gallery", releaseDate: "2023/01/20"},
		{id: "fut20", name: "Pokémon Futsal Collection", releaseDate: "2020/09/11"},
		{id: "me1", name: "Mega Evolution", releaseDate: "2025/09/26", tcgdexID: "me01"},
	}
	mk := func(id, name, date string) *TCGdexSet { return &TCGdexSet{ID: id, Name: name, ReleaseDate: date} }
	got := matchSets([]*TCGdexSet{
		mk("swsh12.5", "Crown Zenith", "2023-01-20"),
		mk("fut2020", "Pokémon Futsal 2020", "2020-09-11"),
		mk("me01", "Mega Evolution", "2025-09-26"),
		mk("me06", "Delta Reign", "2026-11-06"),
	}, ours)
	want := map[string]string{"swsh12.5": "swsh12pt5", "fut2020": "fut20", "me01": "me1"}
	if len(got) != len(want) {
		t.Fatalf("matchSets = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("matchSets[%s] = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestPickTCGdexPrice(t *testing.T) {
	// Trend first, avg as fallback, per lane.
	n, r := pickTCGdexPrice(prices(1.5, 2, 0, 3).Cardmarket)
	if n != 1.5 || r != 3 {
		t.Fatalf("normal/reverse = %v/%v", n, r)
	}
	n, r = pickTCGdexPrice(prices(0, 7, 0.4, 0.5).Cardmarket)
	if n != 7 || r != 0.4 {
		t.Fatalf("avg fallback = %v/%v", n, r)
	}
	if n, r = pickTCGdexPrice(nil); n != 0 || r != 0 {
		t.Fatalf("empty = %v/%v", n, r)
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{"me2pt5": "me02.5", "sv1": "sv01", "svp": "svp", "base1": "base01"} {
		if got := ourSetIDToTCGdex(in); got != want {
			t.Errorf("ourSetIDToTCGdex(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"021": "21", "TG01": "TG1", "SWSH050": "SWSH50", "!": "!"} {
		if got := collectorKey(in); got != want {
			t.Errorf("collectorKey(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Double rare": "Double Rare", "ACE SPEC Rare": "ACE SPEC Rare", "None": "", "": ""} {
		if got := rarityLabel(in); got != want {
			t.Errorf("rarityLabel(%q) = %q, want %q", in, got, want)
		}
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

func TestSyncAll_ReprintSetMatchesByNameAndRepairsDuplicates(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, synced_at) VALUES
		('me55c', '30th Celebration: Classic Collection', 'Mega Evolution', '2026/09/16', ?)`, now); err != nil {
		t.Fatal(err)
	}
	// pokemontcg.io kept the original numbers; the first TCGdex sync then
	// linked Charizard #4 to TCGdex #004 (Genesect EX) and added Charizard
	// #001 as an imageless duplicate.
	for _, c := range [][]string{
		{"me55c-4", "Charizard", "4", "30th-c-004", "old"},
		{"me55c-149", "Lugia", "149", "", "old"},
		{"me55c-1", "Charizard", "1", "30th-c-001", ""},
	} {
		if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, image_small_url, image_large_url, synced_at, tcgdex_id)
			VALUES (?, 'me55c', ?, ?, ?, ?, ?, ?)`, c[0], c[1], c[2], c[4], c[4], now, c[3]); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeTCGdex{
		sets: []TCGdexSet{tcgSet("30th-c", "30th Classic Collection", "2026-09-16", "me", "Mega Evolution",
			summary("30th-c-001", "001", "Charizard"), summary("30th-c-004", "004", "Genesect EX"), summary("30th-c-029", "029", "Lugia"))},
		cards:    map[string]TCGdexCard{},
		cardHits: map[string]int{},
	}
	if err := SyncAll(ctx, d, newFakeClient(t, f)); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	rows, err := d.Query(`SELECT id, tcgdex_id FROM pokemon_cards WHERE set_id = 'me55c'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, tcg string
		if err := rows.Scan(&id, &tcg); err != nil {
			t.Fatal(err)
		}
		got[id] = tcg
	}
	rows.Close()
	// Charizard and Lugia link by name; the duplicate is gone; Genesect,
	// which we didn't have, is added under TCGdex's number.
	want := map[string]string{"me55c-4": "30th-c-001", "me55c-149": "30th-c-029", "me55c-1": "", "30th-c-004": "30th-c-004"}
	if len(got) != 3 || got["me55c-4"] != want["me55c-4"] || got["me55c-149"] != want["me55c-149"] {
		t.Fatalf("cards = %v", got)
	}
	if first, _ := NeedsTCGdexSync(ctx, d); first {
		t.Fatal("repair still pending after sync")
	}
}

func TestNamesCompatible(t *testing.T) {
	for _, c := range []struct {
		a, b string
		ok   bool
	}{
		{"Empoleon LV.X", "Empoleon", true},
		{"Basic Fire Energy", "Fire Energy", true},
		{"Imposter Professor Oak", "Impostor Professor Oak", true},
		{"Buzzwole-GX", "Buzzwole GX", true},
		{"Charizard", "Genesect EX", false},
		{"Dark Tyranitar", "Darkrai & Cresselia LEGEND", false},
		{"Antique Cover Fossil", "Escavalier", false},
	} {
		if got := namesCompatible(c.a, c.b); got != c.ok {
			t.Errorf("namesCompatible(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestRepairBrokenImages(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, synced_at) VALUES ('me55', '30th Celebration', 'Mega Evolution', '2026/09/16', ?)`, now); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][]string{
		{"me55-B", "https://assets.tcgdex.net/en/me/30th/B/low.webp"},   // broken, old picture exists
		{"me55-1", "https://assets.tcgdex.net/en/me/30th/001/low.webp"}, // fine
		{"me06-1", "https://assets.tcgdex.net/en/me/me06/001/low.webp"}, // broken, no old picture
	} {
		if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, image_small_url, image_large_url, synced_at)
			VALUES (?, 'me55', 'x', '1', ?, ?, ?)`, c[0], c[1], c[1], now); err != nil {
			t.Fatal(err)
		}
	}
	f := newFakeTCGdex()
	f.missingImages = map[string]bool{
		"https://assets.tcgdex.net/en/me/30th/B/low.webp":   true,
		"https://assets.tcgdex.net/en/me/me06/001/low.webp": true,
		"https://images.scrydex.com/pokemon/me06-1/small":   true,
	}
	if err := repairBrokenImages(ctx, d, newFakeClient(t, f)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"me55-B": "https://images.scrydex.com/pokemon/me55-B/small",
		"me55-1": "https://assets.tcgdex.net/en/me/30th/001/low.webp",
		"me06-1": "",
	} {
		var got string
		if err := d.QueryRow(`SELECT image_small_url FROM pokemon_cards WHERE id = ?`, id).Scan(&got); err != nil || got != want {
			t.Errorf("%s image = %q (%v), want %q", id, got, err, want)
		}
	}
}
