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
)

func porCards() []setCard {
	return []setCard{
		{id: "me3-49", name: "Haunter", no: "49", tcgplayerID: 684430},
		{id: "me3-55", name: "Mega Skarmory ex", no: "55", tcgplayerID: 684351},
		{id: "me3-50", name: "Gengar", no: "50"},
		{id: "me3-2", name: "Ariados", no: "2"},
	}
}

func TestMatchProduct_RealShopTitles(t *testing.T) {
	cards := porCards()
	for _, c := range []struct {
		name string
		p    shopifyProduct
		want string
	}{
		// Pokelageret: tcgplayer tag.
		{"tag", shopifyProduct{Title: "Mega Skarmory ex - 055/088", Tags: []string{"english", "tcgplayer:684351"}}, "me3-55"},
		// Pokesingles: id at the end of the SKU.
		{"sku", shopifyProduct{Title: "Haunter 049 (Reverse Holofoil) - ME03: Perfect Order", Handle: "haunter-me03-perfect-order",
			Variants: []shopifyVariant{{SKU: "POR-049/088-EN-Reverse Holofoil-NM-684430"}}}, "me3-49"},
		// Pokehandel: number first.
		{"number first", shopifyProduct{Title: "050/088 Gengar"}, "me3-50"},
		// Laboge: number and set name.
		{"laboge", shopifyProduct{Title: "Ariados 002/088 - Perfect Order"}, "me3-2"},
		// Right number, wrong Pokémon: another set's card.
		{"wrong name", shopifyProduct{Title: "050/088 Pikachu"}, ""},
		// Sealed product and Japanese cards are ignored.
		{"sealed", shopifyProduct{Title: "Perfect Order Booster Box"}, ""},
		{"japanese", shopifyProduct{Title: "050/088 Gengar", Tags: []string{"japanese"}}, ""},
	} {
		if got := matchProduct(c.p, cards); got != c.want {
			t.Errorf("%s: matchProduct = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPrintAndCondition(t *testing.T) {
	for in, want := range map[string]string{"NM / Reverse Holofoil": "reverse", "Non Holo": "normal", "Holo": "holo", "Near Mint": "", "NM / Normal": "normal"} {
		if got := printOf(in); got != want {
			t.Errorf("printOf(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Near Mint": "NM", "NM / Reverse Holofoil": "NM", "LP": "LP", "Lightly Played": "LP", "Holo": "", "DMG": "DMG"} {
		if got := conditionOf(in); got != want {
			t.Errorf("conditionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindSetCollection(t *testing.T) {
	cols := []shopifyCollection{
		{Handle: "perfect-order-booster", Title: "Perfect Order Booster", ProductsCount: 3},
		{Handle: "instock-set-me03", Title: "Perfect Order — på lager", ProductsCount: 100},
		{Handle: "set-me03", Title: "Perfect Order", ProductsCount: 323},
		{Handle: "mega-perfect-order", Title: "Mega Perfect Order", ProductsCount: 121},
	}
	if got := findSetCollection(cols, "Perfect Order"); got != "set-me03" {
		t.Fatalf("got %q", got)
	}
	if got := findSetCollection(cols[3:], "Perfect Order"); got != "mega-perfect-order" {
		t.Fatalf("contains match = %q", got)
	}
	if got := findSetCollection(cols, "Delta Reign"); got != "" {
		t.Fatalf("no match = %q", got)
	}
}

func TestPlanBasket_ShippingDecidesSplit(t *testing.T) {
	shops := []Shop{
		{ID: 1, Name: "A", BaseURL: "https://a.no", ShippingNOK: 79},
		{ID: 2, Name: "B", BaseURL: "https://b.no", ShippingNOK: 69},
	}
	off := func(shop int64, variant string, price float64, avail bool) Offer {
		return Offer{ShopID: shop, VariantID: variant, PriceNOK: price, Available: avail}
	}
	cards := []basketCard{
		{id: "c1", offers: []Offer{off(1, "a1", 10, true), off(2, "b1", 15, true)}},
		{id: "c2", offers: []Offer{off(1, "a2", 12, true), off(2, "b2", 10, true)}},
		{id: "c3", offers: []Offer{off(2, "b3", 5, false)}}, // out of stock everywhere
	}
	b := planBasket(cards, shops)
	// One shop beats two shipments: A = 10+12+79 = 101, B = 15+10+69 = 94.
	if len(b.Shops) != 1 || b.Shops[0].ShopID != 2 || b.TotalNOK != 94 {
		t.Fatalf("basket = %+v", b)
	}
	if b.Shops[0].CartURL != "https://b.no/cart/b1:1,b2:1" {
		t.Fatalf("cart url = %s", b.Shops[0].CartURL)
	}
	if len(b.Unavailable) != 1 || b.Unavailable[0] != "c3" {
		t.Fatalf("unavailable = %v", b.Unavailable)
	}

	// Free shipping over a threshold changes the answer.
	shops[0].FreeOverNOK = 20
	if b := planBasket(cards[:2], shops); b.Shops[0].ShopID != 1 || b.TotalNOK != 22 {
		t.Fatalf("free shipping basket = %+v", b)
	}

	// A played copy loses to a Near Mint one even when cheaper.
	cards = []basketCard{{id: "c1", offers: []Offer{
		{ShopID: 1, VariantID: "lp", PriceNOK: 5, Available: true, Condition: "LP"},
		{ShopID: 1, VariantID: "nm", PriceNOK: 8, Available: true, Condition: "NM"},
	}}}
	if b := planBasket(cards, shops); b.Shops[0].Items[0].VariantID != "nm" {
		t.Fatalf("condition pick = %+v", b.Shops[0].Items)
	}
}

// fakeShop is a Shopify shop: collections, collection products, search.
type fakeShop struct {
	mu          sync.Mutex
	collections []shopifyCollection
	products    map[string][]shopifyProduct // by collection handle
	byHandle    map[string]shopifyProduct   // for search
	hits        int
}

func (f *fakeShop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/collections.json":
		_ = json.NewEncoder(w).Encode(map[string]any{"collections": f.collections})
	case strings.HasPrefix(r.URL.Path, "/collections/") && strings.HasSuffix(r.URL.Path, "/products.json"):
		h := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/collections/"), "/products.json")
		ps := f.products[h]
		if r.URL.Query().Get("page") != "1" {
			ps = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"products": ps})
	case r.URL.Path == "/search/suggest.json":
		q := strings.ToLower(r.URL.Query().Get("q"))
		var res []map[string]string
		for h, p := range f.byHandle {
			if strings.Contains(strings.ToLower(p.Title), strings.Fields(q)[0]) {
				res = append(res, map[string]string{"handle": h, "title": p.Title})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"resources": map[string]any{"results": map[string]any{"products": res}}})
	case strings.HasPrefix(r.URL.Path, "/products/") && strings.HasSuffix(r.URL.Path, ".json"):
		h := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/products/"), ".json")
		_ = json.NewEncoder(w).Encode(map[string]any{"product": f.byHandle[h]})
	default:
		http.NotFound(w, r)
	}
}

func seedPerfectOrder(t *testing.T, d *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'r@example.com', 'r', 'r')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, synced_at) VALUES ('me3', 'Perfect Order', 'Mega Evolution', '2026/03/27', ?)`, now); err != nil {
		t.Fatal(err)
	}
	for _, c := range porCards() {
		if _, err := d.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, synced_at, tcgplayer_id, cm_product_id) VALUES (?, 'me3', ?, ?, ?, ?, 877400)`,
			c.id, c.name, c.no, now, c.tcgplayerID); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(`INSERT INTO pokemon_card_variants (card_id, kind, price_eur) VALUES (?, 'normal', 1), (?, 'reverse_holofoil', 0.5)`, c.id, c.id); err != nil {
			t.Fatal(err)
		}
	}
	// The user owns Haunter's reverse holo, so Haunter isn't missing.
	var vid int64
	if err := d.QueryRow(`SELECT id FROM pokemon_card_variants WHERE card_id = 'me3-49' AND kind = 'reverse_holofoil'`).Scan(&vid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at) VALUES (1, 'me3-49', ?, 1, '', ?)`, vid, now); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshOffersAndSinglesPage(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	seedPerfectOrder(t, d)

	withCollection := &fakeShop{
		collections: []shopifyCollection{{Handle: "perfect-order", Title: "Perfect Order", ProductsCount: 3}},
		products: map[string][]shopifyProduct{"perfect-order": {
			{Title: "Mega Skarmory ex - 055/088", Handle: "mega-skarmory", Tags: []string{"tcgplayer:684351"},
				Variants: []shopifyVariant{{ID: 11, Title: "NM / Normal", Price: "25.00", Available: true}}},
			{Title: "Gengar - 050/088", Handle: "gengar", Variants: []shopifyVariant{
				{ID: 12, Title: "NM / Reverse Holofoil", Price: "9.00", Available: true},
				{ID: 13, Title: "NM / Normal", Price: "6.00", Available: false}}},
			{Title: "Perfect Order Booster Box", Handle: "box", Variants: []shopifyVariant{{ID: 14, Price: "2844.00", Available: true}}},
		}},
	}
	searchOnly := &fakeShop{byHandle: map[string]shopifyProduct{
		"ariados-002-088-perfect-order": {Title: "Ariados 002/088 - Perfect Order", Handle: "ariados-002-088-perfect-order",
			Variants: []shopifyVariant{{ID: 21, Title: "Default Title", Price: "4.00", Available: true}}},
	}}
	srvA := httptest.NewServer(withCollection)
	t.Cleanup(srvA.Close)
	srvB := httptest.NewServer(searchOnly)
	t.Cleanup(srvB.Close)
	prevHC := shopHTTPClient
	shopHTTPClient = srvA.Client()
	t.Cleanup(func() { shopHTTPClient = prevHC })

	// Replace the default shops with the two fakes.
	if err := seedShops(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM pokemon_shops`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO pokemon_shops (id, name, base_url, shipping_nok, enabled, created_at) VALUES (1, 'A', ?, 79, 1, ?), (2, 'B', ?, 69, 1, ?)`,
		srvA.URL, now, srvB.URL, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO pokemon_wanted_sets (user_id, set_id, created_at) VALUES (1, 'me3', ?)`, now); err != nil {
		t.Fatal(err)
	}

	ran, err := RefreshOffers(ctx, d)
	if err != nil || !ran {
		t.Fatalf("refresh = %v, %v", ran, err)
	}

	page, err := loadSinglesPage(ctx, d, 1, 11.5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sets) != 1 || page.Sets[0].MissingCount != 3 {
		t.Fatalf("sets = %+v", page.Sets)
	}
	got := map[string]MissingCard{}
	for _, c := range page.Cards {
		got[c.ID] = c
	}
	if _, owned := got["me3-49"]; owned {
		t.Fatal("owned Haunter listed as missing")
	}
	gengar := got["me3-50"]
	if len(gengar.Offers) != 2 || !gengar.Offers[0].Available || gengar.Offers[0].PriceNOK != 9 || gengar.Offers[0].Print != "reverse" {
		t.Fatalf("gengar offers = %+v", gengar.Offers)
	}
	if gengar.CardmarketNOK == nil || *gengar.CardmarketNOK != 0.5*11.5 || !strings.Contains(gengar.CardmarketURL, "idProduct=877400") {
		t.Fatalf("gengar cardmarket = %v %s", gengar.CardmarketNOK, gengar.CardmarketURL)
	}
	ariados := got["me3-2"]
	if len(ariados.Offers) != 1 || ariados.Offers[0].ShopName != "B" || ariados.Offers[0].PriceNOK != 4 {
		t.Fatalf("ariados offers (search shop) = %+v", ariados.Offers)
	}

	// The basket for all three: Skarmory + Gengar from A (25+9+79), Ariados
	// from B (4+69) — B doesn't stock the others.
	var shops []Shop
	for _, s := range page.Shops {
		shops = append(shops, s)
	}
	var bc []basketCard
	for _, c := range page.Cards {
		bc = append(bc, basketCard{id: c.ID, name: c.Name, offers: c.Offers})
	}
	b := planBasket(bc, shops)
	if len(b.Unavailable) != 0 || b.TotalNOK != 25+9+79+4+69 {
		t.Fatalf("basket = %+v", b)
	}
}

func TestNormalizeShopURL(t *testing.T) {
	for in, want := range map[string]string{
		"pokehandel.no/collections/mega-perfect-order": "https://pokehandel.no",
		"https://www.Poke-Shop.no/":                    "https://www.poke-shop.no",
	} {
		if got, err := normalizeShopURL(in); err != nil || got != want {
			t.Errorf("normalizeShopURL(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := normalizeShopURL("not a url"); err == nil {
		t.Error("accepted a non-url")
	}
}

func TestMergeOffers(t *testing.T) {
	copyOf := func(v string, price float64) Offer {
		return Offer{ShopID: 4, VariantID: v, Condition: "NM", PriceNOK: price, Available: true}
	}
	got := mergeOffers([]Offer{copyOf("1", 9), copyOf("2", 9), copyOf("3", 9), copyOf("4", 25)})
	if len(got) != 2 || got[0].Copies != 3 || got[0].VariantID != "1" || got[1].Copies != 1 {
		t.Fatalf("merged = %+v", got)
	}
}
