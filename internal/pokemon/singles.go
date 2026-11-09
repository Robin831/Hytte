package pokemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Buying singles: a user picks the sets they're completing; for every card
// they own no print of, the page shows the Cardmarket price and link and
// the Norwegian shops that have it in stock, and a basket works out the
// cheapest way to buy a selection across shops, shipping included.

// --- shops -------------------------------------------------------------

// Shop is a webshop we check for singles.
type Shop struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	BaseURL       string  `json:"base_url"`
	ShippingNOK   float64 `json:"shipping_nok"`
	FreeOverNOK   float64 `json:"free_over_nok"`
	Enabled       bool    `json:"enabled"`
	LastCheckedAt *string `json:"last_checked_at"`
	LastError     string  `json:"last_error"`
}

// defaultShops are the Norwegian Shopify shops found in November 2026 with
// real ranges of English singles. Shipping is the cheapest parcel-locker
// rate to Bergen their checkout quoted then.
var defaultShops = []Shop{
	{Name: "Pokelageret", BaseURL: "https://pokelageret.no", ShippingNOK: 79, FreeOverNOK: 2499},
	{Name: "Pokesingles", BaseURL: "https://pokesingles.no", ShippingNOK: 79},
	{Name: "Pokehandel", BaseURL: "https://pokehandel.no", ShippingNOK: 69},
	{Name: "Laboge", BaseURL: "https://laboge.no", ShippingNOK: 79},
	{Name: "Cardcenter", BaseURL: "https://cardcenter.no", ShippingNOK: 99, FreeOverNOK: 999},
}

const shopsSeedKey = "pokemon_shops_seed_v1"

// seedShops adds the default shops once, so shops removed later stay gone.
func seedShops(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, shopsSeedKey).Scan(&done); err != nil || done > 0 {
		return err
	}
	now := time.Now().UTC()
	for _, s := range defaultShops {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO pokemon_shops (name, base_url, shipping_nok, free_over_nok, enabled, created_at)
			VALUES (?, ?, ?, ?, 1, ?)`, s.Name, s.BaseURL, s.ShippingNOK, s.FreeOverNOK, now); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, shopsSeedKey)
	return err
}

func listShops(ctx context.Context, db *sql.DB, enabledOnly bool) ([]Shop, error) {
	q := `SELECT id, name, base_url, shipping_nok, free_over_nok, enabled, last_checked_at, last_error FROM pokemon_shops`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Shop{}
	for rows.Next() {
		var s Shop
		var checked sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.BaseURL, &s.ShippingNOK, &s.FreeOverNOK, &s.Enabled, &checked, &s.LastError); err != nil {
			return nil, err
		}
		if checked.Valid {
			s.LastCheckedAt = &checked.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// normalizeShopURL turns "pokehandel.no/collections/x" into
// "https://pokehandel.no".
func normalizeShopURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("shop address is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !strings.Contains(u.Host, ".") {
		return "", errors.New("not a web address")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

// --- refreshing offers ---------------------------------------------------

// offersRefreshing is set while a refresh runs, so runs don't overlap.
var offersRefreshing atomic.Bool

// shopHTTPClient is the client shop requests use; tests replace it.
var shopHTTPClient = &http.Client{Timeout: 30 * time.Second}

// RefreshOffers checks every enabled shop for the cards of every wanted set
// (or only setIDs when given). Shops run in parallel, each at one request
// a second. Returns false when a refresh is already running.
func RefreshOffers(ctx context.Context, db *sql.DB, setIDs ...string) (bool, error) {
	if !offersRefreshing.CompareAndSwap(false, true) {
		return false, nil
	}
	defer offersRefreshing.Store(false)

	if err := seedShops(ctx, db); err != nil {
		return true, err
	}
	sets := setIDs
	if len(sets) == 0 {
		rows, err := db.QueryContext(ctx, `SELECT DISTINCT set_id FROM pokemon_wanted_sets`)
		if err != nil {
			return true, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return true, err
			}
			sets = append(sets, id)
		}
		rows.Close()
	}
	if len(sets) == 0 {
		return true, nil
	}
	shops, err := listShops(ctx, db, true)
	if err != nil {
		return true, err
	}
	var wg sync.WaitGroup
	for _, s := range shops {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			refreshShop(ctx, db, s, sets)
		}()
	}
	wg.Wait()
	return true, ctx.Err()
}

func refreshShop(ctx context.Context, db *sql.DB, shop Shop, sets []string) {
	client := newShopifyClient(shop.BaseURL, shopHTTPClient)
	var cols []shopifyCollection
	var errs []string
	for _, setID := range sets {
		if ctx.Err() != nil {
			return
		}
		if err := refreshShopSet(ctx, db, client, shop, setID, &cols); err != nil {
			log.Printf("pokemon: shop %s set %s: %v", shop.Name, setID, err)
			errs = append(errs, err.Error())
		}
	}
	msg := ""
	if len(errs) > 0 {
		msg = errs[0]
	}
	if _, err := db.ExecContext(ctx, `UPDATE pokemon_shops SET last_checked_at = ?, last_error = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), msg, shop.ID); err != nil {
		log.Printf("pokemon: mark shop %s checked: %v", shop.Name, err)
	}
}

// setCards loads a set's cards for matching.
func setCards(ctx context.Context, db *sql.DB, setID string) (string, []setCard, error) {
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM pokemon_sets WHERE id = ?`, setID).Scan(&name); err != nil {
		return "", nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, name, collector_no, tcgplayer_id FROM pokemon_cards WHERE set_id = ?`, setID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var cards []setCard
	for rows.Next() {
		var c setCard
		if err := rows.Scan(&c.id, &c.name, &c.no, &c.tcgplayerID); err != nil {
			return "", nil, err
		}
		cards = append(cards, c)
	}
	return name, cards, rows.Err()
}

// collectionTTL is how long a found (or not found) set collection is
// trusted before the shop's collection list is read again.
const collectionTTL = 7 * 24 * time.Hour

func refreshShopSet(ctx context.Context, db *sql.DB, client *shopifyClient, shop Shop, setID string, cols *[]shopifyCollection) error {
	setName, cards, err := setCards(ctx, db, setID)
	if err != nil {
		return err
	}
	handle, fresh := "", false
	var checked string
	err = db.QueryRowContext(ctx, `SELECT handle, checked_at FROM pokemon_shop_collections WHERE shop_id = ? AND set_id = ?`, shop.ID, setID).Scan(&handle, &checked)
	if err == nil {
		if t, perr := time.Parse(time.RFC3339, checked); perr == nil && time.Since(t) < collectionTTL {
			fresh = true
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !fresh {
		if *cols == nil {
			c, err := client.collections(ctx)
			if err != nil {
				return fmt.Errorf("list collections: %w", err)
			}
			*cols = c
		}
		handle = findSetCollection(*cols, setName)
		if _, err := db.ExecContext(ctx, `INSERT INTO pokemon_shop_collections (shop_id, set_id, handle, checked_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(shop_id, set_id) DO UPDATE SET handle = excluded.handle, checked_at = excluded.checked_at`,
			shop.ID, setID, handle, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}

	var offers []shopOffer
	searched := cards
	if handle != "" {
		products, err := client.collectionProducts(ctx, handle)
		if err != nil {
			return fmt.Errorf("read collection %s: %w", handle, err)
		}
		offers = offersFrom(shop.BaseURL, products, cards)
	} else {
		// No collection for the set: search for the cards someone wants.
		searched, err = wantedMissingCards(ctx, db, setID, cards)
		if err != nil {
			return err
		}
		for _, c := range searched {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			card := c
			products, err := client.search(ctx, c.name+" "+paddedNumber(c.no), func(title string) bool {
				return matchProduct(shopifyProduct{Title: title}, []setCard{card}) != ""
			})
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			offers = append(offers, offersFrom(shop.BaseURL, products, cards)...)
		}
	}
	return storeOffers(ctx, db, shop.ID, searched, offers)
}

// paddedNumber writes a collector number the way shops print it ("2" →
// "002"); non-numeric numbers ("TG05") are kept.
func paddedNumber(no string) string {
	if n, err := strconv.Atoi(no); err == nil {
		return fmt.Sprintf("%03d", n)
	}
	return no
}

// wantedMissingCards returns the cards of a set that at least one user who
// wants the set owns no print of.
func wantedMissingCards(ctx context.Context, db *sql.DB, setID string, cards []setCard) ([]setCard, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id FROM pokemon_cards c
		WHERE c.set_id = ? AND EXISTS (
			SELECT 1 FROM pokemon_wanted_sets w WHERE w.set_id = c.set_id AND NOT EXISTS (
				SELECT 1 FROM pokemon_collections col WHERE col.user_id = w.user_id AND col.card_id = c.id AND col.quantity > 0))`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		want[id] = true
	}
	var out []setCard
	for _, c := range cards {
		if want[c.id] {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// storeOffers replaces the shop's offers for the given cards.
func storeOffers(ctx context.Context, db *sql.DB, shopID int64, cards []setCard, offers []shopOffer) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, c := range cards {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pokemon_shop_offers WHERE shop_id = ? AND card_id = ?`, shopID, c.id); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, o := range offers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pokemon_shop_offers (shop_id, variant_id, card_id, title, print, condition, price_nok, available, url, checked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(shop_id, variant_id) DO UPDATE SET card_id = excluded.card_id, title = excluded.title, print = excluded.print,
				condition = excluded.condition, price_nok = excluded.price_nok, available = excluded.available, url = excluded.url, checked_at = excluded.checked_at`,
			shopID, strconv.FormatInt(o.variantID, 10), o.cardID, o.title, o.print, o.condition, o.priceNOK, o.available, o.url, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StartOffersLoop refreshes shop offers daily at 06:30 Oslo time.
func StartOffersLoop(ctx context.Context, db *sql.DB) {
	if err := seedShops(ctx, db); err != nil {
		log.Printf("pokemon: seed shops: %v", err)
	}
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		oslo = time.UTC
	}
	for {
		now := time.Now().In(oslo)
		next := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, oslo)
		if !now.Before(next) {
			next = next.AddDate(0, 0, 1)
		}
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		rctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		if _, err := RefreshOffers(rctx, db); err != nil {
			log.Printf("pokemon: refresh shop offers: %v", err)
		} else {
			log.Println("pokemon: shop offers refreshed")
		}
		cancel()
	}
}

// --- page data -----------------------------------------------------------

// WantedSet is a set the user is completing.
type WantedSet struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	LogoURL      string `json:"logo_url"`
	SymbolURL    string `json:"symbol_url"`
	TotalCards   int    `json:"total_cards"`
	MissingCount int    `json:"missing_count"`
	SetCode      string `json:"set_code"`
}

// ensureSetCodes fills in the printed set code of the user's wanted sets
// that don't have one yet (sets synced before the code was stored), one
// TCGdex request per set. Best effort: failures leave the code empty.
func ensureSetCodes(ctx context.Context, db *sql.DB, userID int64) {
	rows, err := db.QueryContext(ctx, `SELECT s.id, s.tcgdex_id FROM pokemon_wanted_sets w JOIN pokemon_sets s ON s.id = w.set_id
		WHERE w.user_id = ? AND s.set_code = '' AND s.tcgdex_id != ''`, userID)
	if err != nil {
		return
	}
	type pending struct{ id, tcgdexID string }
	var todo []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.tcgdexID) == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()
	if len(todo) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := tcgdexClientForSetCodes()
	for _, p := range todo {
		d, err := client.GetSet(ctx, p.tcgdexID)
		if err != nil || d.Abbreviation.Official == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE pokemon_sets SET set_code = ? WHERE id = ?`, d.Abbreviation.Official, p.id); err != nil {
			log.Printf("pokemon: store set code for %s: %v", p.id, err)
		}
	}
}

// tcgdexClientForSetCodes is a seam so tests don't call TCGdex.
var tcgdexClientForSetCodes = NewTCGdexClient

// Offer is one shop's listing of a card.
type Offer struct {
	ShopID    int64   `json:"shop_id"`
	ShopName  string  `json:"shop_name"`
	VariantID string  `json:"variant_id"`
	Print     string  `json:"print"`
	Condition string  `json:"condition"`
	PriceNOK  float64 `json:"price_nok"`
	Available bool    `json:"available"`
	URL       string  `json:"url"`
	// Copies counts identical listings merged into this one (some shops
	// list every physical copy as its own variant).
	Copies int `json:"copies"`
}

// mergeOffers folds listings that only differ by variant id (same shop,
// print, condition, price and stock) into one with a copy count.
func mergeOffers(offers []Offer) []Offer {
	out := make([]Offer, 0, len(offers))
	idx := map[string]int{}
	for _, o := range offers {
		key := fmt.Sprintf("%d|%s|%s|%.2f|%t", o.ShopID, o.Print, o.Condition, o.PriceNOK, o.Available)
		if i, ok := idx[key]; ok {
			out[i].Copies++
			continue
		}
		o.Copies = 1
		idx[key] = len(out)
		out = append(out, o)
	}
	return out
}

// MissingCard is a card of a wanted set the user owns no print of.
type MissingCard struct {
	ID              string   `json:"id"`
	SetID           string   `json:"set_id"`
	Name            string   `json:"name"`
	CollectorNo     string   `json:"collector_no"`
	Rarity          string   `json:"rarity"`
	ImageSmallURL   string   `json:"image_small_url"`
	ImageLargeURL   string   `json:"image_large_url"`
	CardmarketEUR   float64  `json:"cardmarket_eur"`
	CardmarketNOK   *float64 `json:"cardmarket_nok"`
	CardmarketURL   string   `json:"cardmarket_url"`
	Offers          []Offer  `json:"offers"`
	cmProductID     int64
	cheapestInStock float64
}

// SinglesPage is the GET /api/pokemon/singles response.
type SinglesPage struct {
	Sets       []WantedSet   `json:"sets"`
	Cards      []MissingCard `json:"cards"`
	Shops      []Shop        `json:"shops"`
	Refreshing bool          `json:"refreshing"`
}

func loadSinglesPage(ctx context.Context, db *sql.DB, userID int64, rate float64, rateOK bool) (*SinglesPage, error) {
	page := &SinglesPage{Sets: []WantedSet{}, Cards: []MissingCard{}, Refreshing: offersRefreshing.Load()}
	shops, err := listShops(ctx, db, false)
	if err != nil {
		return nil, err
	}
	page.Shops = shops
	shopName := map[int64]string{}
	for _, s := range shops {
		shopName[s.ID] = s.Name
	}

	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.name, s.release_date, s.logo_url, s.symbol_url, s.total_cards, s.set_code
		FROM pokemon_wanted_sets w JOIN pokemon_sets s ON s.id = w.set_id
		WHERE w.user_id = ? ORDER BY s.release_date DESC`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s WantedSet
		if err := rows.Scan(&s.ID, &s.Name, &s.ReleaseDate, &s.LogoURL, &s.SymbolURL, &s.TotalCards, &s.SetCode); err != nil {
			rows.Close()
			return nil, err
		}
		page.Sets = append(page.Sets, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Sets) == 0 {
		return page, nil
	}

	rows, err = db.QueryContext(ctx, `
		SELECT c.id, c.set_id, c.name, c.collector_no, c.rarity, c.image_small_url, c.image_large_url, c.cm_product_id,
			COALESCE((SELECT MAX(v.price_eur) FROM pokemon_card_variants v WHERE v.card_id = c.id AND v.kind = 'normal'), 0),
			COALESCE((SELECT MAX(v.price_eur) FROM pokemon_card_variants v WHERE v.card_id = c.id AND v.kind = 'reverse_holofoil'), 0)
		FROM pokemon_cards c
		JOIN pokemon_wanted_sets w ON w.set_id = c.set_id AND w.user_id = ?
		WHERE NOT EXISTS (SELECT 1 FROM pokemon_collections col WHERE col.user_id = ? AND col.card_id = c.id AND col.quantity > 0)`, userID, userID)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for rows.Next() {
		var c MissingCard
		var normal, rev float64
		if err := rows.Scan(&c.ID, &c.SetID, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL, &c.cmProductID, &normal, &rev); err != nil {
			rows.Close()
			return nil, err
		}
		// Either print completes the card, so the cheaper priced one counts.
		c.CardmarketEUR = normal
		if c.CardmarketEUR <= 0 || (rev > 0 && rev < c.CardmarketEUR) {
			c.CardmarketEUR = rev
		}
		if rateOK && c.CardmarketEUR > 0 {
			nok := c.CardmarketEUR * rate
			c.CardmarketNOK = &nok
		}
		c.CardmarketURL = CardmarketProductURL(c.cmProductID)
		c.Offers = []Offer{}
		index[c.ID] = len(page.Cards)
		page.Cards = append(page.Cards, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = db.QueryContext(ctx, `
		SELECT o.card_id, o.shop_id, o.variant_id, o.print, o.condition, o.price_nok, o.available, o.url
		FROM pokemon_shop_offers o
		JOIN pokemon_shops s ON s.id = o.shop_id AND s.enabled = 1
		JOIN pokemon_cards c ON c.id = o.card_id
		JOIN pokemon_wanted_sets w ON w.set_id = c.set_id AND w.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cardID string
		var o Offer
		if err := rows.Scan(&cardID, &o.ShopID, &o.VariantID, &o.Print, &o.Condition, &o.PriceNOK, &o.Available, &o.URL); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := index[cardID]
		if !ok {
			continue
		}
		o.ShopName = shopName[o.ShopID]
		page.Cards[i].Offers = append(page.Cards[i].Offers, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	missing := map[string]int{}
	for i := range page.Cards {
		c := &page.Cards[i]
		missing[c.SetID]++
		sort.SliceStable(c.Offers, func(a, b int) bool {
			if c.Offers[a].Available != c.Offers[b].Available {
				return c.Offers[a].Available
			}
			return c.Offers[a].PriceNOK < c.Offers[b].PriceNOK
		})
		c.Offers = mergeOffers(c.Offers)
	}
	for i := range page.Sets {
		page.Sets[i].MissingCount = missing[page.Sets[i].ID]
	}
	sort.SliceStable(page.Cards, func(a, b int) bool {
		if page.Cards[a].SetID != page.Cards[b].SetID {
			return setOrder(page.Sets, page.Cards[a].SetID) < setOrder(page.Sets, page.Cards[b].SetID)
		}
		return collectorOrder(page.Cards[a].CollectorNo) < collectorOrder(page.Cards[b].CollectorNo)
	})
	return page, nil
}

func setOrder(sets []WantedSet, id string) int {
	for i, s := range sets {
		if s.ID == id {
			return i
		}
	}
	return len(sets)
}

// --- basket --------------------------------------------------------------

// BasketItem is a card bought from a shop.
type BasketItem struct {
	CardID    string  `json:"card_id"`
	Name      string  `json:"name"`
	VariantID string  `json:"variant_id"`
	Print     string  `json:"print"`
	Condition string  `json:"condition"`
	PriceNOK  float64 `json:"price_nok"`
	URL       string  `json:"url"`
}

// BasketShop is one shop's part of the basket.
type BasketShop struct {
	ShopID      int64        `json:"shop_id"`
	ShopName    string       `json:"shop_name"`
	Items       []BasketItem `json:"items"`
	SubtotalNOK float64      `json:"subtotal_nok"`
	ShippingNOK float64      `json:"shipping_nok"`
	TotalNOK    float64      `json:"total_nok"`
	CartURL     string       `json:"cart_url"`
}

// Basket is the cheapest way found to buy the selected cards.
type Basket struct {
	Shops       []BasketShop `json:"shops"`
	Unavailable []string     `json:"unavailable"`
	TotalNOK    float64      `json:"total_nok"`
}

type basketCard struct {
	id, name string
	offers   []Offer
}

// maxBasketShops caps the exhaustive search (2^n shop combinations).
const maxBasketShops = 12

// planBasket picks, among every combination of shops, the one that buys
// the most of the selected cards and, among those, costs least with each
// shop's shipping (free over its threshold). Within a combination each card
// goes to the cheapest shop that has it in stock, preferring Near Mint (or
// unstated) copies over played ones.
func planBasket(cards []basketCard, shops []Shop) Basket {
	byID := map[int64]Shop{}
	for _, s := range shops {
		byID[s.ID] = s
	}
	// best[card][shop] = the offer we'd buy there.
	best := make([]map[int64]Offer, len(cards))
	count := map[int64]int{}
	for i, c := range cards {
		best[i] = map[int64]Offer{}
		for _, o := range c.offers {
			if !o.Available {
				continue
			}
			if _, ok := byID[o.ShopID]; !ok {
				continue
			}
			cur, has := best[i][o.ShopID]
			if !has || betterOffer(o, cur) {
				best[i][o.ShopID] = o
			}
		}
		for sid := range best[i] {
			count[sid]++
		}
	}
	var cand []int64
	for sid := range count {
		cand = append(cand, sid)
	}
	sort.Slice(cand, func(a, b int) bool {
		if count[cand[a]] != count[cand[b]] {
			return count[cand[a]] > count[cand[b]]
		}
		return cand[a] < cand[b]
	})
	if len(cand) > maxBasketShops {
		cand = cand[:maxBasketShops]
	}

	type plan struct {
		covered int
		cost    float64
		assign  []int64 // per card: shop id or 0
	}
	var bestPlan *plan
	for mask := 0; mask < 1<<len(cand); mask++ {
		in := map[int64]bool{}
		for i, sid := range cand {
			if mask&(1<<i) != 0 {
				in[sid] = true
			}
		}
		p := plan{assign: make([]int64, len(cards))}
		sub := map[int64]float64{}
		for i := range cards {
			var pick int64
			for sid, o := range best[i] {
				if !in[sid] {
					continue
				}
				if pick == 0 || betterOffer(o, best[i][pick]) || (o.PriceNOK == best[i][pick].PriceNOK && sid < pick) {
					pick = sid
				}
			}
			if pick != 0 {
				p.assign[i] = pick
				p.covered++
				sub[pick] += best[i][pick].PriceNOK
			}
		}
		if len(sub) != len(in) {
			continue // a shop in the combination gets nothing; a smaller mask covers it
		}
		for sid, total := range sub {
			p.cost += total + shippingFor(byID[sid], total)
		}
		if bestPlan == nil || p.covered > bestPlan.covered || (p.covered == bestPlan.covered && p.cost < bestPlan.cost-0.001) {
			pp := p
			bestPlan = &pp
		}
	}

	out := Basket{Shops: []BasketShop{}, Unavailable: []string{}}
	if bestPlan == nil {
		for _, c := range cards {
			out.Unavailable = append(out.Unavailable, c.id)
		}
		return out
	}
	shopIdx := map[int64]int{}
	for i, c := range cards {
		sid := bestPlan.assign[i]
		if sid == 0 {
			out.Unavailable = append(out.Unavailable, c.id)
			continue
		}
		j, ok := shopIdx[sid]
		if !ok {
			j = len(out.Shops)
			shopIdx[sid] = j
			out.Shops = append(out.Shops, BasketShop{ShopID: sid, ShopName: byID[sid].Name})
		}
		o := best[i][sid]
		out.Shops[j].Items = append(out.Shops[j].Items, BasketItem{CardID: c.id, Name: c.name, VariantID: o.VariantID,
			Print: o.Print, Condition: o.Condition, PriceNOK: o.PriceNOK, URL: o.URL})
		out.Shops[j].SubtotalNOK += o.PriceNOK
	}
	for j := range out.Shops {
		s := &out.Shops[j]
		s.ShippingNOK = shippingFor(byID[s.ShopID], s.SubtotalNOK)
		s.TotalNOK = s.SubtotalNOK + s.ShippingNOK
		out.TotalNOK += s.TotalNOK
		parts := make([]string, len(s.Items))
		for k, it := range s.Items {
			parts[k] = it.VariantID + ":1"
		}
		// Shopify cart permalink: opens the shop's checkout with these items.
		s.CartURL = strings.TrimRight(byID[s.ShopID].BaseURL, "/") + "/cart/" + strings.Join(parts, ",")
	}
	sort.SliceStable(out.Shops, func(a, b int) bool { return out.Shops[a].TotalNOK > out.Shops[b].TotalNOK })
	return out
}

// betterOffer prefers Near Mint/unstated condition, then the lower price.
func betterOffer(a, b Offer) bool {
	ga, gb := goodCondition(a.Condition), goodCondition(b.Condition)
	if ga != gb {
		return ga
	}
	return a.PriceNOK < b.PriceNOK
}

func goodCondition(c string) bool { return c == "" || c == "NM" }

func shippingFor(s Shop, subtotal float64) float64 {
	if s.FreeOverNOK > 0 && subtotal >= s.FreeOverNOK {
		return 0
	}
	return s.ShippingNOK
}
