package pokemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sort"
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
	if err := repairClassicCollection(ctx, db); err != nil {
		return fmt.Errorf("repair classic collection: %w", err)
	}
	if err := repairBrokenImages(ctx, db, client); err != nil {
		return fmt.Errorf("repair images: %w", err)
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
// TCGdex (no set carries a tcgdex_id yet) or a one-time repair is pending,
// so the server can sync at startup instead of waiting for Sunday.
func NeedsTCGdexSync(ctx context.Context, db *sql.DB) (bool, error) {
	var linked, repaired int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pokemon_sets WHERE tcgdex_id != ''`).Scan(&linked); err != nil {
		return false, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key IN (?, ?)`, repairClassicKey, repairImagesKey).Scan(&repaired); err != nil {
		return false, err
	}
	return linked == 0 || repaired < 2, nil
}

// repairImagesKey marks the one-time check of the TCGdex images the first
// sync stored without verifying them.
const repairImagesKey = "pokemon_tcgdex_repair_images_v2" // v2 also checks the large image

// repairBrokenImages checks every stored TCGdex image once and replaces the
// broken ones with the card's previous picture (pokemontcg.io's images, now
// served by Scrydex at a predictable address, until it shuts down in March
// 2027), or clears them when there is none. From then on verifiedImages
// keeps broken images out.
func repairBrokenImages(ctx context.Context, db *sql.DB, client *TCGdexClient) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, repairImagesKey).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, image_small_url, image_large_url FROM pokemon_cards WHERE image_small_url LIKE 'https://assets.tcgdex.net/%'`)
	if err != nil {
		return err
	}
	type card struct{ id, small, large string }
	var cards []card
	for rows.Next() {
		var c card
		if err := rows.Scan(&c.id, &c.small, &c.large); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	fixes := map[string][2]string{}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 2*tcgdexParallelism)
	)
	for _, c := range cards {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c := c
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if client.imageOK(ctx, c.small) && (!strings.HasPrefix(c.large, "https://assets.tcgdex.net/") || client.imageOK(ctx, c.large)) {
				return
			}
			old := "https://images.scrydex.com/pokemon/" + c.id
			fix := [2]string{"", ""}
			if client.imageOK(ctx, old+"/small") {
				fix = [2]string{old + "/small", old + "/large"}
			}
			mu.Lock()
			fixes[c.id] = fix
			mu.Unlock()
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for id, f := range fixes {
		if _, err := tx.ExecContext(ctx, `UPDATE pokemon_cards SET image_small_url = ?, image_large_url = ? WHERE id = ?`, f[0], f[1], id); err != nil {
			return fmt.Errorf("fix image for %s: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, repairImagesKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(fixes) > 0 {
		log.Printf("pokemon: replaced %d broken TCGdex images", len(fixes))
	}
	return nil
}

// repairClassicKey marks the one-time cleanup after the first TCGdex sync
// matched the 30th Celebration Classic Collection (me55c) by number only:
// the sources number those reprints differently, so 24 duplicates were
// added (without images) and 6 cards were linked to the wrong card.
const repairClassicKey = "pokemon_tcgdex_repair_me55c"

// repairClassicCollection deletes those duplicates — cards the sync added
// to me55c, recognisable by a TCGdex link and no image, never owned or
// scanned — and clears the set's links so linkCards matches it by name.
func repairClassicCollection(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, repairClassicKey).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	dupes := `SELECT id FROM pokemon_cards WHERE set_id = 'me55c' AND tcgdex_id != '' AND image_small_url = ''
		AND id NOT IN (SELECT card_id FROM pokemon_collections)
		AND id NOT IN (SELECT matched_card_id FROM pokemon_scan_jobs WHERE matched_card_id IS NOT NULL)`
	if _, err := tx.ExecContext(ctx, `DELETE FROM pokemon_card_variants WHERE card_id IN (`+dupes+`)`); err != nil {
		return fmt.Errorf("delete me55c duplicate variants: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM pokemon_cards WHERE id IN (`+dupes+`)`)
	if err != nil {
		return fmt.Errorf("delete me55c duplicates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pokemon_cards SET tcgdex_id = '' WHERE set_id = 'me55c'`); err != nil {
		return fmt.Errorf("unlink me55c: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, repairClassicKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("pokemon: removed %d duplicate Classic Collection cards", n)
	}
	return nil
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

	cards, err := upsertSetCards(ctx, db, client, d, ourID, now)
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

// cardName folds a card name for matching ("Buzzwole-GX" ≈ "Buzzwole GX").
func cardName(s string) string { return normName(s) }

// namesCompatible reports whether two card names can be the same card. The
// sources name some cards differently ("Empoleon LV.X" / "Empoleon", "Basic
// Fire Energy" / "Fire Energy", "Imposter" / "Impostor"), so it accepts
// containment or a shared start of five letters, and rejects different
// Pokémon ("Charizard" / "Genesect EX").
func namesCompatible(a, b string) bool {
	a, b = cardName(a), cardName(b)
	if a == "" || b == "" || strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n >= 5
}

type ourCard struct {
	id, no, name, tcgdexID, image string
	linked                        bool
}

// linkCards pairs TCGdex cards with ours, strongest rule first:
//
//  1. the stored tcgdex_id, if the names still agree
//  2. same collector number and compatible names
//  3. same name (exact, then compatible), first free card in number order
//
// Rule 3 covers reprint sets numbered differently by the two sources (the
// 30th Celebration Classic Collection keeps the original numbers on
// pokemontcg.io but runs 1–30 on TCGdex). Unlinked TCGdex cards are new.
func linkCards(tcg []TCGdexCardSummary, ours []*ourCard) map[string]*ourCard {
	links := map[string]*ourCard{}
	link := func(c TCGdexCardSummary, o *ourCard) {
		links[c.ID] = o
		o.linked = true
	}
	for _, c := range tcg {
		for _, o := range ours {
			if !o.linked && o.tcgdexID == c.ID && namesCompatible(o.name, c.Name) {
				link(c, o)
				break
			}
		}
	}
	for _, c := range tcg {
		if links[c.ID] != nil {
			continue
		}
		for _, o := range ours {
			if !o.linked && collectorKey(o.no) == collectorKey(c.LocalID) && namesCompatible(o.name, c.Name) {
				link(c, o)
				break
			}
		}
	}
	for _, exact := range []bool{true, false} {
		for _, c := range tcg {
			if links[c.ID] != nil {
				continue
			}
			for _, o := range ours {
				if o.linked {
					continue
				}
				if (exact && cardName(o.name) == cardName(c.Name)) || (!exact && namesCompatible(o.name, c.Name)) {
					link(c, o)
					break
				}
			}
		}
	}
	return links
}

// upsertSetCards links TCGdex's cards to ours (see linkCards), refreshes
// their images, and inserts cards we don't have. Returns the cards to fetch
// details for.
func upsertSetCards(ctx context.Context, db *sql.DB, client *TCGdexClient, d *TCGdexSet, ourSetID string, now time.Time) ([]cardRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, collector_no, name, tcgdex_id, image_small_url FROM pokemon_cards WHERE set_id = ?`, ourSetID)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	var ours []*ourCard
	ids := map[string]bool{}
	for rows.Next() {
		o := &ourCard{}
		if err := rows.Scan(&o.id, &o.no, &o.name, &o.tcgdexID, &o.image); err != nil {
			rows.Close()
			return nil, err
		}
		ours = append(ours, o)
		ids[o.id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(ours, func(i, j int) bool { return collectorOrder(ours[i].no) < collectorOrder(ours[j].no) })
	links := linkCards(d.Cards, ours)
	images := verifiedImages(ctx, client, d.Cards, links)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Drop links no TCGdex card claimed any more (e.g. a wrong number match
	// from before names were checked), so the card stops taking its prices.
	for _, o := range ours {
		if !o.linked && o.tcgdexID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE pokemon_cards SET tcgdex_id = '' WHERE id = ?`, o.id); err != nil {
				return nil, fmt.Errorf("unlink card %s: %w", o.id, err)
			}
		}
	}

	var out []cardRef
	for _, c := range d.Cards {
		small, large := images[c.ID][0], images[c.ID][1]
		var ourID string
		if o := links[c.ID]; o != nil {
			ourID = o.id
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
				// A card with this id but another name already exists; fall
				// back to TCGdex's id rather than overwrite it.
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

// verifiedImages returns the TCGdex image (small, large) to store per card,
// only for images that exist: TCGdex sometimes lists an image whose file is
// missing (the 30th Celebration Mews B, G and R 404), and switching to it
// would replace a working picture with a broken one. A card already on its
// TCGdex image isn't re-checked; the rest are checked each sync until the
// file appears.
func verifiedImages(ctx context.Context, client *TCGdexClient, cards []TCGdexCardSummary, links map[string]*ourCard) map[string][2]string {
	out := map[string][2]string{}
	var check []TCGdexCardSummary
	for _, c := range cards {
		small := tcgdexAsset(c.Image, "/low.webp")
		if small == "" {
			continue
		}
		if o := links[c.ID]; o != nil && o.image == small {
			out[c.ID] = [2]string{small, tcgdexAsset(c.Image, "/high.webp")}
			continue
		}
		check = append(check, c)
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, tcgdexParallelism)
	)
	for _, c := range check {
		c := c
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			small := tcgdexAsset(c.Image, "/low.webp")
			if client.imageOK(ctx, small) && client.imageOK(ctx, tcgdexAsset(c.Image, "/high.webp")) {
				mu.Lock()
				out[c.ID] = [2]string{small, tcgdexAsset(c.Image, "/high.webp")}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// collectorOrder sorts collector numbers numerically where possible
// ("2" < "10"), so name matches take the lowest-numbered free card.
func collectorOrder(no string) string {
	if m := collectorPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(no))); m != nil {
		n, _ := strconv.Atoi(m[2])
		return fmt.Sprintf("%s%08d%s", m[1], n, m[3])
	}
	return no
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
