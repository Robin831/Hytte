package pokemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"
)

// PageSize is the per-request page size used against the pokemontcg.io API.
// 250 is the maximum the API allows and minimises total HTTP requests.
const PageSize = 250


// SyncSets paginates /v2/sets and upserts each set into pokemon_sets.
func SyncSets(ctx context.Context, db *sql.DB, client *Client) error {
	if client == nil {
		client = NewClient()
	}
	page := 1
	for {
		var resp SetsResponse
		u := fmt.Sprintf("%s/sets?page=%d&pageSize=%d", client.baseURL, page, PageSize)
		if err := client.doRequest(ctx, u, &resp); err != nil {
			return fmt.Errorf("fetch sets page %d: %w", page, err)
		}
		if len(resp.Data) == 0 {
			break
		}

		now := time.Now().UTC()
		for _, s := range resp.Data {
			if err := upsertSet(ctx, db, s, now); err != nil {
				return fmt.Errorf("upsert set %s: %w", s.ID, err)
			}
		}

		if page*resp.PageSize >= resp.TotalCount || len(resp.Data) < resp.PageSize {
			break
		}
		page++
	}
	return nil
}

func upsertSet(ctx context.Context, db *sql.DB, s Set, now time.Time) error {
	// total_cards is the full set incl. secret rares (used to drive ownership
	// counts). printed_total is what is actually printed on the card face
	// (e.g. "021/142") and is what the search + scan worker match against the
	// denominator the user / Claude reads off a scan.
	total := s.Total
	if total == 0 {
		total = s.PrintedTotal
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, printed_total, symbol_url, logo_url, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name          = excluded.name,
			series        = excluded.series,
			release_date  = excluded.release_date,
			total_cards   = excluded.total_cards,
			printed_total = excluded.printed_total,
			symbol_url    = excluded.symbol_url,
			logo_url      = excluded.logo_url,
			synced_at     = excluded.synced_at
	`, s.ID, s.Name, s.Series, s.ReleaseDate, total, s.PrintedTotal, s.Images.Symbol, s.Images.Logo, now)
	return err
}

// SyncCards paginates /v2/cards?q=set.id:<setID> and upserts each card plus
// its variant pricing.
func SyncCards(ctx context.Context, db *sql.DB, client *Client, setID string) error {
	if client == nil {
		client = NewClient()
	}
	return syncCardsImpl(ctx, db, client, setID, false)
}

// RefreshPrices walks every known set and updates only the variant prices,
// leaving the metadata rows untouched. Use this for the daily price tick.
func RefreshPrices(ctx context.Context, db *sql.DB, client *Client) error {
	if client == nil {
		client = NewClient()
	}
	setIDs, err := listSetIDs(ctx, db)
	if err != nil {
		return fmt.Errorf("list sets for price refresh: %w", err)
	}
	for _, id := range setIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := syncCardsImpl(ctx, db, client, id, true); err != nil {
			log.Printf("pokemon: refresh prices for set %s: %v", id, err)
			continue
		}
	}
	return nil
}

// SyncAll first syncs the set list, then iterates each set and pulls its
// cards. Per-set errors are logged and do not abort the run.
func SyncAll(ctx context.Context, db *sql.DB, client *Client) error {
	if client == nil {
		client = NewClient()
	}
	if err := SyncSets(ctx, db, client); err != nil {
		return fmt.Errorf("sync sets: %w", err)
	}
	setIDs, err := listSetIDs(ctx, db)
	if err != nil {
		return fmt.Errorf("list sets after sync: %w", err)
	}
	for _, id := range setIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := SyncCards(ctx, db, client, id); err != nil {
			log.Printf("pokemon: sync cards for set %s: %v", id, err)
			continue
		}
	}
	n, err := backfillNormalVariants(ctx, db)
	if err != nil {
		return fmt.Errorf("backfill normal variants: %w", err)
	}
	if n > 0 {
		log.Printf("pokemon: backfilled %d normal variant rows for cards with no price data", n)
	}
	return nil
}

func listSetIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM pokemon_sets ORDER BY release_date, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// syncCardsImpl is the shared implementation behind SyncCards and the
// price-only refresh path.
func syncCardsImpl(ctx context.Context, db *sql.DB, client *Client, setID string, pricesOnly bool) error {
	// Hybrid dispatch: certain sets (today: the `me*` Mega Evolution family)
	// have no Cardmarket prices on pokemontcg.io v2 because their scraper
	// bridge isn't wired up for the new IDs. Route those to TCGdex which
	// does carry fresh prices. Card metadata still comes from pokemontcg.io
	// (the rest of syncCardsImplPokemontcg below runs first to keep card
	// rows / set rows consistent with the rest of the catalogue); TCGdex
	// only contributes pricing.
	if shouldUseTCGdex(setID) {
		// Run the pokemontcg.io path first so metadata stays consistent —
		// our card IDs, names, rarities, and image URLs come from
		// pokemontcg.io and don't change.
		if err := syncCardsImplPokemontcg(ctx, db, client, setID, pricesOnly); err != nil {
			return err
		}
		// Then overlay TCGdex prices on top. Failure here is non-fatal —
		// metadata is already good; we just leave variant prices at zero
		// (which the UI renders as "—" per Hytte-bivr).
		if err := overlayTCGdexPrices(ctx, db, setID); err != nil {
			log.Printf("pokemon: TCGdex price overlay for %s: %v", setID, err)
		}
		return nil
	}
	return syncCardsImplPokemontcg(ctx, db, client, setID, pricesOnly)
}

// syncCardsImplPokemontcg is the canonical pokemontcg.io v2 sync path —
// pulls card metadata + (when present) Cardmarket prices for the given set.
func syncCardsImplPokemontcg(ctx context.Context, db *sql.DB, client *Client, setID string, pricesOnly bool) error {
	page := 1
	for {
		q := url.Values{}
		q.Set("q", "set.id:"+setID)
		q.Set("page", strconv.Itoa(page))
		q.Set("pageSize", strconv.Itoa(PageSize))
		u := client.baseURL + "/cards?" + q.Encode()

		var resp CardsResponse
		if err := client.doRequest(ctx, u, &resp); err != nil {
			return fmt.Errorf("fetch cards page %d for set %s: %w", page, setID, err)
		}
		if len(resp.Data) == 0 {
			break
		}

		now := time.Now().UTC()
		for _, c := range resp.Data {
			if !pricesOnly {
				if err := upsertCard(ctx, db, setID, c, now); err != nil {
					return fmt.Errorf("upsert card %s: %w", c.ID, err)
				}
			}
			if err := upsertVariants(ctx, db, c, now); err != nil {
				return fmt.Errorf("upsert variants for %s: %w", c.ID, err)
			}
		}

		if page*resp.PageSize >= resp.TotalCount || len(resp.Data) < resp.PageSize {
			break
		}
		page++
	}
	return nil
}

// overlayTCGdexPrices reads every card row for setID from the DB, translates
// each card id into TCGdex's zero-padded convention, fetches per-card pricing
// in parallel (bounded by tcgdexParallelism), and upserts the resulting
// (normal, reverse_holofoil) prices onto the existing variant rows. Card
// metadata is NOT touched — that path is owned by syncCardsImplPokemontcg.
// Test seam tcgdexClientFn lets tests inject a stub client without exporting
// the dependency through the public sync entry points.
func overlayTCGdexPrices(ctx context.Context, db *sql.DB, setID string) error {
	client := tcgdexClientFn()
	rows, err := db.QueryContext(ctx, `SELECT id FROM pokemon_cards WHERE set_id = ?`, setID)
	if err != nil {
		return fmt.Errorf("list cards for tcgdex overlay: %w", err)
	}
	defer rows.Close()
	var ourIDs, tcgIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan card id: %w", err)
		}
		ourIDs = append(ourIDs, id)
		tcgIDs = append(tcgIDs, ourCardIDToTCGdex(id))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ourIDs) == 0 {
		return nil
	}

	prices := fetchTCGdexCardPrices(ctx, client, setID, tcgIDs, ourIDs)
	if len(prices) == 0 {
		// TCGdex may not have ingested this set yet (the common case for
		// brand-new releases like me4 on its release day). Caller logs
		// nothing; the UI shows "—" until prices appear on the next pass.
		return nil
	}

	now := time.Now().UTC()
	for cardID, p := range prices {
		// Ensure the placeholder normal row exists so the price update has
		// something to update.
		if err := ensureNormalVariant(ctx, db, cardID); err != nil {
			return err
		}
		if p[0] > 0 {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
				VALUES (?, 'normal', ?, ?)
				ON CONFLICT(card_id, kind) DO UPDATE SET
					price_eur = excluded.price_eur,
					price_at  = excluded.price_at
			`, cardID, p[0], now); err != nil {
				return fmt.Errorf("upsert tcgdex normal for %s: %w", cardID, err)
			}
		}
		if p[1] > 0 {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
				VALUES (?, 'reverse_holofoil', ?, ?)
				ON CONFLICT(card_id, kind) DO UPDATE SET
					price_eur = excluded.price_eur,
					price_at  = excluded.price_at
			`, cardID, p[1], now); err != nil {
				return fmt.Errorf("upsert tcgdex reverse for %s: %w", cardID, err)
			}
		}
	}
	return nil
}

// tcgdexClientFn is the test seam for swapping the TCGdex client. Production
// returns a fresh client per call (cheap); tests replace it with a stub.
var tcgdexClientFn = func() *TCGdexClient { return NewTCGdexClient() }

func upsertCard(ctx context.Context, db *sql.DB, setID string, c Card, now time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			set_id          = excluded.set_id,
			name            = excluded.name,
			collector_no    = excluded.collector_no,
			rarity          = excluded.rarity,
			image_small_url = excluded.image_small_url,
			image_large_url = excluded.image_large_url,
			synced_at       = excluded.synced_at
	`, c.ID, setID, c.Name, c.Number, c.Rarity, c.Images.Small, c.Images.Large, now)
	return err
}

// upsertVariants persists Cardmarket prices for a card. The cardmarket.prices
// field is a flat object with named metric keys, so we map the two variant
// kinds we persist (normal, reverse_holofoil) to their corresponding fields.
//
// A placeholder "normal" row is inserted first with price 0 so that every card
// has at least one variant the user can mark as owned, even when Cardmarket
// has no pricing data yet (e.g. brand-new sets). The placeholder uses
// DO NOTHING so it cannot clobber an existing price, while real prices below
// still upsert with DO UPDATE.
func upsertVariants(ctx context.Context, db *sql.DB, c Card, now time.Time) error {
	if err := ensureNormalVariant(ctx, db, c.ID); err != nil {
		return err
	}
	p := c.Cardmarket.Prices
	type variantRow struct {
		kind string
		eur  float64
	}
	variants := []variantRow{
		{"normal", pickPrice(p.TrendPrice, p.AverageSellPrice)},
		{"reverse_holofoil", pickPrice(p.ReverseHoloTrend, p.ReverseHoloSell)},
	}
	for _, v := range variants {
		if v.eur == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(card_id, kind) DO UPDATE SET
				price_eur = excluded.price_eur,
				price_at  = excluded.price_at
		`, c.ID, v.kind, v.eur, now); err != nil {
			return err
		}
	}
	return nil
}

// ensureNormalVariant inserts a placeholder normal-kind row with price 0 if
// none exists for the card. The UNIQUE(card_id, kind) constraint with
// DO NOTHING guarantees an existing priced row is left untouched.
func ensureNormalVariant(ctx context.Context, db *sql.DB, cardID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
		VALUES (?, 'normal', 0, NULL)
		ON CONFLICT(card_id, kind) DO NOTHING
	`, cardID)
	return err
}

// backfillNormalVariants ensures every card has a kind='normal' variant row by
// inserting a placeholder (price 0) for any card that lacks one. Cards that
// already have other variants (e.g. reverse_holofoil) but no normal row are
// also repaired. This is the self-healing pass run at the end of SyncAll.
func backfillNormalVariants(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `
		INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
		SELECT c.id, 'normal', 0, NULL
		FROM pokemon_cards c
		WHERE NOT EXISTS (
			SELECT 1 FROM pokemon_card_variants v WHERE v.card_id = c.id AND v.kind = 'normal'
		)
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// pickPrice returns trend when non-zero, otherwise avg.
func pickPrice(trend, avg float64) float64 {
	if trend > 0 {
		return trend
	}
	return avg
}
