package pokemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The catalogue comes from TCGdex. Sets and cards we had before the switch
// keep their pokemontcg.io-style ids (collections, scans and variants point
// at them); the sync matches each TCGdex set and card to them and stores
// the TCGdex id alongside in tcgdex_id. Sets TCGdex has that we don't are
// only added from newSetCutoff on, so the catalogue grows with new releases
// without pulling in the digital TCG Pocket sets or the trainer kits and
// oddities pokemontcg.io never listed.

// newSetCutoff is the earliest release date (YYYY-MM-DD) of a TCGdex set we
// add when it isn't in the catalogue yet: the start of the Mega Evolution
// era, so its energy and promo sets come in too.
const newSetCutoff = "2025-09-01"

// skippedSeries are TCGdex series we never add: the digital TCG Pocket game.
var skippedSeries = map[string]bool{"tcgp": true}

// setAliases pairs TCGdex sets with ours where neither name nor id lines up.
var setAliases = map[string]string{
	"fut2020": "fut20", // Pokémon Futsal 2020 / Pokémon Futsal Collection
	"30th-c":  "me55c", // 30th Classic Collection / 30th Celebration: Classic Collection
}

// tcgdexParallelism caps concurrent per-card detail fetches. TCGdex doesn't
// publish a rate limit; 6 in flight is gentle and does the ~20k-card weekly
// pass in about ten minutes.
const tcgdexParallelism = 6

// SyncAll refreshes the whole catalogue: every set, every card, and every
// card's rarity and prices. Runs weekly (and on demand from the admin
// endpoint). Per-set failures are logged and skipped.
func SyncAll(ctx context.Context, db *sql.DB, client *TCGdexClient) error {
	if client == nil {
		client = NewTCGdexClient()
	}
	briefs, err := client.ListSets(ctx)
	if err != nil {
		return fmt.Errorf("list tcgdex sets: %w", err)
	}

	details := make([]*TCGdexSet, 0, len(briefs))
	for _, b := range briefs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d, err := client.GetSet(ctx, b.ID)
		if err != nil {
			log.Printf("pokemon: tcgdex set %s: %v", b.ID, err)
			continue
		}
		if skippedSeries[d.Serie.ID] {
			continue
		}
		details = append(details, d)
	}

	ours, err := loadOurSets(ctx, db)
	if err != nil {
		return err
	}
	mapping := matchSets(details, ours)

	added := 0
	for _, d := range details {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ourID, known := mapping[d.ID]
		if !known {
			if releaseDate(d.ReleaseDate) == "" || releaseDate(d.ReleaseDate) < strings.ReplaceAll(newSetCutoff, "-", "/") {
				continue
			}
			ourID = d.ID
			added++
		}
		if err := syncSet(ctx, db, client, d, ourID, !known); err != nil {
			log.Printf("pokemon: sync set %s (%s): %v", ourID, d.ID, err)
		}
	}
	if added > 0 {
		log.Printf("pokemon: added %d new sets from TCGdex", added)
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

// RefreshPrices updates prices for the cards someone owns. Runs daily; the
// weekly SyncAll refreshes every other card.
func RefreshPrices(ctx context.Context, db *sql.DB, client *TCGdexClient) error {
	if client == nil {
		client = NewTCGdexClient()
	}
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT c.id, c.tcgdex_id FROM pokemon_cards c
		JOIN pokemon_collections col ON col.card_id = c.id
		WHERE c.tcgdex_id != ''`)
	if err != nil {
		return fmt.Errorf("list owned cards: %w", err)
	}
	var cards []cardRef
	for rows.Next() {
		var r cardRef
		if err := rows.Scan(&r.ourID, &r.tcgdexID); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return applyCardDetails(ctx, db, fetchCardDetails(ctx, client, cards), false)
}

// NeedsTCGdexSync reports whether the catalogue has never been synced from
// TCGdex (no set carries a tcgdex_id yet), so the server can run the first
// sync at startup instead of waiting for Sunday.
func NeedsTCGdexSync(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pokemon_sets WHERE tcgdex_id != ''`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// --- set matching ------------------------------------------------------

type ourSet struct {
	id, name, releaseDate, tcgdexID string
}

func loadOurSets(ctx context.Context, db *sql.DB) ([]ourSet, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, release_date, tcgdex_id FROM pokemon_sets`)
	if err != nil {
		return nil, fmt.Errorf("load sets: %w", err)
	}
	defer rows.Close()
	var out []ourSet
	for rows.Next() {
		var s ourSet
		if err := rows.Scan(&s.id, &s.name, &s.releaseDate, &s.tcgdexID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

// normName folds a set name for comparison ("Scarlet & Violet—151" ≈
// "Scarlet and Violet 151").
func normName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", "and")
	s = strings.ReplaceAll(s, "é", "e")
	return nonAlnum.ReplaceAllString(s, "")
}

// releaseDate converts TCGdex's "YYYY-MM-DD" to our stored "YYYY/MM/DD".
func releaseDate(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "-", "/")
}

// matchSets maps TCGdex set ids to our set ids. Rules run strongest first
// over all sets, and a rule only assigns when it finds exactly one free
// candidate, so a weak rule can't steal a set a stronger one would match:
//
//  1. a set already linked by tcgdex_id
//  2. a hand-written alias
//  3. same name and release date
//  4. id translates (sv3pt5 → sv03.5) and same name
//  5. same name, unique
//  6. id translates, names differ ("Base" vs "Base Set")
func matchSets(details []*TCGdexSet, ours []ourSet) map[string]string {
	mapping := map[string]string{}
	claimed := map[string]bool{}
	byID := map[string]ourSet{}
	for _, s := range ours {
		byID[s.id] = s
		if s.tcgdexID != "" {
			mapping[s.tcgdexID] = s.id
			claimed[s.id] = true
		}
	}
	for tcg, our := range setAliases {
		if _, done := mapping[tcg]; done || claimed[our] {
			continue
		}
		if _, ok := byID[our]; ok {
			mapping[tcg] = our
			claimed[our] = true
		}
	}
	rules := []func(d *TCGdexSet, s ourSet) bool{
		func(d *TCGdexSet, s ourSet) bool {
			return normName(d.Name) == normName(s.name) && releaseDate(d.ReleaseDate) == s.releaseDate
		},
		func(d *TCGdexSet, s ourSet) bool {
			return ourSetIDToTCGdex(s.id) == d.ID && normName(d.Name) == normName(s.name)
		},
		func(d *TCGdexSet, s ourSet) bool { return normName(d.Name) == normName(s.name) },
		func(d *TCGdexSet, s ourSet) bool { return ourSetIDToTCGdex(s.id) == d.ID || s.id == d.ID },
	}
	for _, rule := range rules {
		for _, d := range details {
			if _, done := mapping[d.ID]; done {
				continue
			}
			var hit []string
			for _, s := range ours {
				if !claimed[s.id] && s.tcgdexID == "" && rule(d, s) {
					hit = append(hit, s.id)
				}
			}
			if len(hit) == 1 {
				mapping[d.ID] = hit[0]
				claimed[hit[0]] = true
			}
		}
	}
	return mapping
}

// --- per-set sync ------------------------------------------------------

// tcgdexAsset turns a TCGdex asset base URL into a fetchable file.
func tcgdexAsset(base, suffix string) string {
	if base == "" {
		return ""
	}
	return base + suffix
}

// syncSet upserts one set and its cards, then fetches every card's detail
// for rarity and prices. Existing sets keep their name, series and release
// date; counts, logo and symbol follow TCGdex.
func syncSet(ctx context.Context, db *sql.DB, client *TCGdexClient, d *TCGdexSet, ourID string, isNew bool) error {
	now := time.Now().UTC()
	total, printed := d.CardCount.Total, d.CardCount.Official
	if total == 0 {
		total = len(d.Cards)
	}
	if isNew {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, printed_total, symbol_url, logo_url, synced_at, tcgdex_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`,
			ourID, d.Name, d.Serie.Name, releaseDate(d.ReleaseDate), total, printed,
			tcgdexAsset(d.Symbol, ".png"), tcgdexAsset(d.Logo, ".png"), now, d.ID); err != nil {
			return fmt.Errorf("insert set: %w", err)
		}
	} else {
		if _, err := db.ExecContext(ctx, `
			UPDATE pokemon_sets SET
				tcgdex_id     = ?,
				total_cards   = CASE WHEN ? > 0 THEN ? ELSE total_cards END,
				printed_total = CASE WHEN ? > 0 THEN ? ELSE printed_total END,
				symbol_url    = CASE WHEN ? != '' THEN ? ELSE symbol_url END,
				logo_url      = CASE WHEN ? != '' THEN ? ELSE logo_url END,
				synced_at     = ?
			WHERE id = ?`,
			d.ID, total, total, printed, printed,
			tcgdexAsset(d.Symbol, ".png"), tcgdexAsset(d.Symbol, ".png"),
			tcgdexAsset(d.Logo, ".png"), tcgdexAsset(d.Logo, ".png"), now, ourID); err != nil {
			return fmt.Errorf("update set: %w", err)
		}
	}

	cards, err := upsertSetCards(ctx, db, d, ourID, now)
	if err != nil {
		return err
	}
	return applyCardDetails(ctx, db, fetchCardDetails(ctx, client, cards), true)
}

type cardRef struct{ ourID, tcgdexID string }

var collectorPattern = regexp.MustCompile(`^([A-Za-z]*)0*(\d+)([A-Za-z]*)$`)

// collectorKey folds a collector number for matching: "021" ≈ "21",
// "TG01" ≈ "TG1".
func collectorKey(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if m := collectorPattern.FindStringSubmatch(s); m != nil {
		return m[1] + m[2] + m[3]
	}
	return s
}

// collectorNo is how a new card's number is stored: pokemontcg.io's plain
// style, without the leading zeros TCGdex uses ("021" → "21").
func collectorNo(localID string) string {
	if n, err := strconv.Atoi(localID); err == nil {
		return strconv.Itoa(n)
	}
	return localID
}

// upsertSetCards links TCGdex's cards to ours (by tcgdex_id, else by
// collector number), refreshes their images, and inserts cards we don't
// have. Returns the cards to fetch details for.
func upsertSetCards(ctx context.Context, db *sql.DB, d *TCGdexSet, ourSetID string, now time.Time) ([]cardRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, collector_no, tcgdex_id FROM pokemon_cards WHERE set_id = ?`, ourSetID)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	byTCG := map[string]string{}
	byNo := map[string]string{}
	ids := map[string]bool{}
	for rows.Next() {
		var id, no, tcg string
		if err := rows.Scan(&id, &no, &tcg); err != nil {
			rows.Close()
			return nil, err
		}
		ids[id] = true
		if tcg != "" {
			byTCG[tcg] = id
		} else {
			byNo[collectorKey(no)] = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var out []cardRef
	for _, c := range d.Cards {
		small, large := tcgdexAsset(c.Image, "/low.webp"), tcgdexAsset(c.Image, "/high.webp")
		ourID := byTCG[c.ID]
		if ourID == "" {
			ourID = byNo[collectorKey(c.LocalID)]
			delete(byNo, collectorKey(c.LocalID))
		}
		if ourID != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE pokemon_cards SET
					tcgdex_id       = ?,
					image_small_url = CASE WHEN ? != '' THEN ? ELSE image_small_url END,
					image_large_url = CASE WHEN ? != '' THEN ? ELSE image_large_url END,
					synced_at       = ?
				WHERE id = ?`, c.ID, small, small, large, large, now, ourID); err != nil {
				return nil, fmt.Errorf("update card %s: %w", ourID, err)
			}
		} else {
			ourID = ourSetID + "-" + collectorNo(c.LocalID)
			if ids[ourID] {
				// A card with this id but another number already exists;
				// fall back to TCGdex's id rather than overwrite it.
				ourID = c.ID
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at, tcgdex_id)
				VALUES (?, ?, ?, ?, '', ?, ?, ?, ?)
				ON CONFLICT(id) DO NOTHING`,
				ourID, ourSetID, c.Name, collectorNo(c.LocalID), small, large, now, c.ID); err != nil {
				return nil, fmt.Errorf("insert card %s: %w", ourID, err)
			}
			ids[ourID] = true
		}
		if err := ensureNormalVariantTx(ctx, tx, ourID); err != nil {
			return nil, err
		}
		out = append(out, cardRef{ourID: ourID, tcgdexID: c.ID})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// --- card details (rarity + prices) ------------------------------------

type cardDetail struct {
	ourID       string
	rarity      string
	normal, rev float64
}

// fetchCardDetails fetches each card's detail concurrently (bounded by
// tcgdexParallelism). Failures (404 for a card TCGdex lists but hasn't
// detailed yet, network errors) are skipped; the card keeps its old data.
func fetchCardDetails(ctx context.Context, client *TCGdexClient, cards []cardRef) []cardDetail {
	var (
		mu  sync.Mutex
		out = make([]cardDetail, 0, len(cards))
		wg  sync.WaitGroup
		sem = make(chan struct{}, tcgdexParallelism)
	)
	for _, c := range cards {
		if ctx.Err() != nil {
			break
		}
		c := c
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			card, err := client.GetCard(ctx, c.tcgdexID)
			if err != nil {
				return
			}
			n, r := pickTCGdexPrice(card.Pricing.Cardmarket)
			mu.Lock()
			out = append(out, cardDetail{ourID: c.ourID, rarity: rarityLabel(card.Rarity), normal: n, rev: r})
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// rarityLabel capitalises each word of TCGdex's rarity ("Double rare" →
// "Double Rare") to match the labels the catalogue already uses. "None" is
// TCGdex's placeholder for unrated cards.
func rarityLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return ""
	}
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// applyCardDetails stores fetched prices and, when withRarity, fills in a
// missing rarity (an existing rarity is kept, so labels don't churn).
func applyCardDetails(ctx context.Context, db *sql.DB, details []cardDetail, withRarity bool) error {
	if len(details) == 0 {
		return nil
	}
	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, d := range details {
		if withRarity && d.rarity != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE pokemon_cards SET rarity = ? WHERE id = ? AND rarity = ''`, d.rarity, d.ourID); err != nil {
				return fmt.Errorf("set rarity for %s: %w", d.ourID, err)
			}
		}
		for _, v := range []struct {
			kind string
			eur  float64
		}{{"normal", d.normal}, {"reverse_holofoil", d.rev}} {
			if v.eur <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(card_id, kind) DO UPDATE SET
					price_eur = excluded.price_eur,
					price_at  = excluded.price_at`, d.ourID, v.kind, v.eur, now); err != nil {
				return fmt.Errorf("upsert %s price for %s: %w", v.kind, d.ourID, err)
			}
		}
	}
	return tx.Commit()
}

// ensureNormalVariantTx inserts a placeholder normal-kind row with price 0
// if none exists, so every card has a variant the user can mark as owned
// even before it's priced. DO NOTHING keeps an existing price.
func ensureNormalVariantTx(ctx context.Context, tx *sql.Tx, cardID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
		VALUES (?, 'normal', 0, NULL)
		ON CONFLICT(card_id, kind) DO NOTHING`, cardID)
	return err
}

// backfillNormalVariants ensures every card has a kind='normal' variant row
// by inserting a placeholder (price 0) for any card that lacks one. This is
// the self-healing pass run at the end of SyncAll.
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
