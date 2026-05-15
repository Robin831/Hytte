package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/training"
)

// scanMaxImageBytes caps the uploaded image size at 5 MB. Anything larger is
// rejected with 400 — Pokémon card photos from a phone camera fit well within
// this even at the highest quality settings.
const scanMaxImageBytes = 5 << 20

// scanParseFormBytes is the multipart parser limit. It sits slightly above the
// per-image cap so a request that includes only the image field never trips
// before scanMaxImageBytes is enforced on the file itself.
const scanParseFormBytes = 10 << 20

// scanConfidenceThreshold is the floor below which a Claude response is
// treated as "could not read the card" — we then return matched:false rather
// than running a doomed DB lookup.
const scanConfidenceThreshold = 0.4

// scanAllowedMIMETypes whitelists the image types we accept. The list is kept
// tight because Claude's Read tool needs to be able to view the file.
var scanAllowedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/heic": true,
	"image/heif": true,
}

// heifBrands is the set of ISOBMFF "ftyp" brand codes that identify a HEIC or
// HEIF still image. http.DetectContentType does not recognise either format
// (it returns application/octet-stream), so detectImageMIME falls back to this
// table when the standard sniffer can't classify the bytes.
var heifBrands = map[string]string{
	"heic": "image/heic",
	"heix": "image/heic",
	"heim": "image/heic",
	"heis": "image/heic",
	"hevc": "image/heic",
	"hevx": "image/heic",
	"hevm": "image/heic",
	"hevs": "image/heic",
	"mif1": "image/heif",
	"msf1": "image/heif",
	"heif": "image/heif",
}

// detectImageMIME classifies the first bytes of an upload. It defers to
// http.DetectContentType for the common cases (JPEG, PNG, WebP, …) and only
// reaches into the HEIF/HEIC ftyp box if the stdlib sniffer fell back to the
// generic application/octet-stream. The buffer must contain at least 12 bytes
// to identify HEIC/HEIF; shorter slices simply return the stdlib result.
func detectImageMIME(buf []byte) string {
	mime := http.DetectContentType(buf)
	if mime != "application/octet-stream" {
		return mime
	}
	if len(buf) < 12 {
		return mime
	}
	if string(buf[4:8]) != "ftyp" {
		return mime
	}
	if heif, ok := heifBrands[strings.ToLower(string(buf[8:12]))]; ok {
		return heif
	}
	return mime
}

// scanPrompt is the exact prompt sent to Claude. It asks for STRICT JSON so
// the response can be parsed without an LLM-grade post-processor.
const scanPrompt = `You will be shown a single Pokémon TCG card photo. Identify three things:
1. The card name, the LARGE text printed at the top of the card (e.g. "Pikachu", "Pansear", "Charizard ex"). This is the most reliable identifier — always fill it in when you can read it. Include suffix words like " ex", " V", " VMAX", " GX" exactly as printed.
2. The set, by reading the small set symbol on the bottom-right AND any visible set name printed near it. The set symbol is tiny and easy to misread — when in doubt, prefer leaving set_id_hint empty over guessing.
3. The collector number on the bottom-left, in the format "025/195" (numerator/denominator).

Respond as STRICT JSON, no markdown fence, no prose:
{
  "card_name": "...",        // exact card name as printed at the top; empty only if unreadable
  "set_name": "...",         // best guess for the set's English name
  "set_id_hint": "...",      // optional pokemontcg.io set id if recognised (e.g. "sv4"), otherwise empty
  "collector_number": "...", // exact string from the card, e.g. "025/195"
  "confidence": 0.95         // 0.0-1.0; 0 means you can't read the card
}`

// claudeScanResult mirrors the STRICT JSON shape we ask Claude to return.
type claudeScanResult struct {
	CardName        string  `json:"card_name"`
	SetName         string  `json:"set_name"`
	SetIDHint       string  `json:"set_id_hint"`
	CollectorNumber string  `json:"collector_number"`
	Confidence      float64 `json:"confidence"`
}

// ScanCandidate is a single matched card the scan endpoint returns. Score is
// currently the same as the model's confidence — over time we can refine it
// per-candidate (e.g. lower when only the set_name matched fuzzily).
type ScanCandidate struct {
	Card  CardDTO `json:"card"`
	Set   *SetDTO `json:"set,omitempty"`
	Score float64 `json:"score"`
}

// scanRunPromptFn is the package-level seam the scan worker uses to invoke
// Claude. Tests override it to stub out the CLI call without touching the
// real claude binary.
var scanRunPromptFn = func(ctx context.Context, cfg *training.ClaudeConfig, prompt, imagePath string) (string, error) {
	return training.RunPromptWithImage(ctx, cfg, prompt, imagePath)
}

// parseClaudeScanResult unmarshals the Claude response into claudeScanResult,
// tolerating a stray markdown code fence the model occasionally adds despite
// being asked not to.
func parseClaudeScanResult(raw string) (*claudeScanResult, error) {
	trimmed := strings.TrimSpace(raw)
	// Strip ```json … ``` fences if present.
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	var res claudeScanResult
	if err := json.Unmarshal([]byte(trimmed), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// findScanCandidates resolves the Claude-identified set + collector number to
// one or more catalogue cards. Match priority follows the bead spec: explicit
// pokemontcg.io set id hint first, then case-insensitive set name substring,
// then unfiltered. The returned reason is included in matched:false responses
// so the UI can surface why nothing came back.
func findScanCandidates(ctx context.Context, db *sql.DB, userID int64, result *claudeScanResult) ([]ScanCandidate, string, error) {
	collector := strings.TrimSpace(result.CollectorNumber)
	if collector == "" {
		return nil, "no collector number identified", nil
	}
	// Claude returns the full printed format from the card face (e.g. "108/142"
	// — numerator/total-in-set), but pokemontcg.io and our DB store just the
	// numerator ("108"). Strip the "/<total>" suffix so the lookup matches.
	// Preserve everything before the slash to keep variants like "025a/195" or
	// promo formats like "SWSH123" — only the trailing /total goes away.
	// Capture the denominator before stripping: it identifies the set's
	// printed_total and lets us reject confidently-wrong matches (a Stellar
	// Crown card prints "108/142", which is sv7's printed_total — using that
	// rules out sv1 / Scarlet & Violet whose printed_total is 198).
	var printedDenom int
	if idx := strings.Index(collector, "/"); idx > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(collector[idx+1:])); err == nil {
			printedDenom = n
		}
		collector = strings.TrimSpace(collector[:idx])
	}
	// Normalize a purely-numeric collector by stripping leading zeros:
	// pokemontcg.io stores plain "21", while Claude reads "021" off the card
	// face. Atoi/Itoa is a safe round-trip when the whole string is digits;
	// leave promo/variant formats like "025a" or "SWSH123" untouched.
	if n, err := strconv.Atoi(collector); err == nil && collector != "" {
		collector = strconv.Itoa(n)
	}

	var setFilter []string
	var setLabel string

	if hint := strings.TrimSpace(result.SetIDHint); hint != "" {
		var exists string
		err := db.QueryRowContext(ctx,
			`SELECT id FROM pokemon_sets WHERE id = ?`, hint,
		).Scan(&exists)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Hint didn't match the catalogue; fall through to name match.
		case err != nil:
			return nil, "", fmt.Errorf("set id lookup: %w", err)
		default:
			setFilter = []string{exists}
			setLabel = exists
		}
	}

	if len(setFilter) == 0 {
		if name := strings.TrimSpace(result.SetName); name != "" {
			rows, err := db.QueryContext(ctx,
				`SELECT id FROM pokemon_sets WHERE LOWER(name) LIKE LOWER(?)`,
				"%"+name+"%",
			)
			if err != nil {
				return nil, "", fmt.Errorf("set name lookup: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return nil, "", fmt.Errorf("scan set id: %w", err)
				}
				setFilter = append(setFilter, id)
			}
			if err := rows.Err(); err != nil {
				return nil, "", fmt.Errorf("set name rows: %w", err)
			}
			setLabel = name
		}
	}

	// If Claude read a printed denominator off the card, intersect the
	// set filter with sets whose printed_total matches. Two cases:
	//   1. setFilter already has candidates from the id-hint or name-match —
	//      keep only those whose printed_total matches the denominator (this
	//      is the disambiguation that rules out a wrong sv1 guess for an
	//      sv7 card).
	//   2. setFilter is empty (no usable hint) — populate it with every set
	//      whose printed_total matches, narrowing what otherwise would have
	//      been an "any set, collector=N" search.
	// Skip when printed_total is unknown (column was 0 because the sync
	// hasn't been re-run since the schema migration) to avoid filtering
	// every set out and producing spurious no_match results.
	if printedDenom > 0 {
		ids, err := loadSetIDsByPrintedTotal(ctx, db, printedDenom)
		if err != nil {
			return nil, "", fmt.Errorf("printed_total lookup: %w", err)
		}
		if len(ids) > 0 {
			if len(setFilter) == 0 {
				setFilter = ids
			} else {
				want := make(map[string]struct{}, len(ids))
				for _, id := range ids {
					want[id] = struct{}{}
				}
				kept := setFilter[:0]
				for _, id := range setFilter {
					if _, ok := want[id]; ok {
						kept = append(kept, id)
					}
				}
				setFilter = kept
			}
		}
	}

	cards, err := loadScanCards(ctx, db, userID, collector, setFilter)
	if err != nil {
		return nil, "", err
	}

	if len(cards) == 0 {
		label := setLabel
		if label == "" {
			label = "any set"
		}
		return nil, fmt.Sprintf("no card matches set '%s' collector '%s'", label, collector), nil
	}

	// Use the card name Claude read off the top of the card as a tiebreaker /
	// sanity check. The name is the largest, most readable element; the set
	// symbol that drove the (collector, set) filter is the smallest. So when
	// Claude returns a name, drop any candidate whose stored name doesn't
	// match it case-insensitively in either direction (this tolerates promo
	// suffixes like " ex" / " V" appearing on one side but not the other).
	// Empty card_name leaves the list as-is — preserves the older response
	// shape for in-flight requests during deploy.
	claudeName := strings.TrimSpace(result.CardName)
	if claudeName != "" {
		// Two-pass narrowing: first try exact case-insensitive match (handles
		// the common ambiguity between "Pikachu" and "Pikachu V" sharing a
		// collector number across sets), and only fall back to the looser
		// substring-either-way match if the exact pass produced nothing
		// (catalogue suffix vs Claude reading differs slightly, e.g. " ex").
		exact := cards[:0]
		loose := make([]CardDTO, 0, len(cards))
		for _, c := range cards {
			if cardNamesEqual(c.Name, claudeName) {
				exact = append(exact, c)
			} else if cardNameLooksRight(c.Name, claudeName) {
				loose = append(loose, c)
			}
		}
		switch {
		case len(exact) > 0:
			cards = exact
		case len(loose) > 0:
			cards = loose
		default:
			label := setLabel
			if label == "" {
				label = "any set"
			}
			return nil, fmt.Sprintf("set '%s' collector '%s' resolved to %s, but the card reads '%s'",
				label, collector, candidateNamesSummary(cards), claudeName), nil
		}
	}

	out := make([]ScanCandidate, 0, len(cards))
	for i := range cards {
		card := cards[i]
		set, err := loadSetByID(ctx, db, userID, card.SetID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("load set %s: %w", card.SetID, err)
		}
		out = append(out, ScanCandidate{
			Card:  card,
			Set:   set,
			Score: result.Confidence,
		})
	}
	return out, "", nil
}

// cardNamesEqual is the strict half of the name check — case-insensitive
// exact match after trimming. Used as the first-pass filter so a Claude
// reading of "Pikachu V" disambiguates between catalogue rows "Pikachu" and
// "Pikachu V" instead of accepting both via the looser substring check.
func cardNamesEqual(dbName, claudeName string) bool {
	a := strings.ToLower(strings.TrimSpace(dbName))
	b := strings.ToLower(strings.TrimSpace(claudeName))
	return a != "" && a == b
}

// cardNameLooksRight returns true when the catalogue card name and the name
// Claude read off the photo are case-insensitively the same or one contains
// the other. The substring tolerance handles the common cases where Claude
// drops/adds a suffix word like " ex", " V", " VMAX", " GX" relative to the
// catalogue entry, without opening up false positives — substrings of less
// than three characters wouldn't reach this code path anyway because the
// candidate list is already narrowed by collector number.
func cardNameLooksRight(dbName, claudeName string) bool {
	a := strings.ToLower(strings.TrimSpace(dbName))
	b := strings.ToLower(strings.TrimSpace(claudeName))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

// candidateNamesSummary renders a short comma-separated list of card-name
// (set-id) pairs, capped at a few items, for surfacing in match-failure
// reasons. Lets the UI explain why a confidently-read collector number was
// rejected: "set 'sv1' collector '025' resolved to Pikachu (sv1), Pikachu V
// (swsh1), but the card reads 'Charizard'".
func candidateNamesSummary(cards []CardDTO) string {
	const max = 3
	parts := make([]string, 0, len(cards))
	for i, c := range cards {
		if i == max {
			parts = append(parts, fmt.Sprintf("+%d more", len(cards)-max))
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Name, c.SetID))
	}
	return strings.Join(parts, ", ")
}

// loadSetIDsByPrintedTotal returns every set whose printed_total matches the
// supplied denominator. Used by the scan worker to disambiguate sets when
// Claude reads the printed "n/total" off the card face.
func loadSetIDsByPrintedTotal(ctx context.Context, db *sql.DB, denom int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM pokemon_sets WHERE printed_total = ?`, denom,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// loadScanCards fetches cards matching the collector number, optionally
// restricted to one or more set ids. Variants and ownership flags are
// hydrated the same way as the other listing endpoints.
//
// When the collector is purely numeric, the SQL compares against
// CAST(collector_no AS INTEGER) so "21", "021" and the DB-stored value
// (which the upstream pokemontcg.io sync stores either zero-padded or not,
// depending on the set) all collapse to the same integer. Non-numeric forms
// like "025a" or "SWSH123" keep their literal string comparison.
func loadScanCards(ctx context.Context, db *sql.DB, userID int64, collector string, setIDs []string) ([]CardDTO, error) {
	collectorClause := "c.collector_no = ?"
	var collectorArg any = collector
	if n, err := strconv.Atoi(collector); err == nil {
		collectorClause = "CAST(c.collector_no AS INTEGER) = ?"
		collectorArg = n
	}
	query := `
		SELECT c.id, c.set_id, c.name, c.collector_no, c.rarity, c.image_small_url, c.image_large_url
		FROM pokemon_cards c
		WHERE ` + collectorClause + `
	`
	args := []any{collectorArg}
	if len(setIDs) > 0 {
		placeholders := strings.Repeat("?,", len(setIDs))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND c.set_id IN (" + placeholders + ")"
		for _, id := range setIDs {
			args = append(args, id)
		}
	}
	query += " ORDER BY c.set_id, c.id"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]CardDTO, 0)
	cardIndex := make(map[string]int)
	for rows.Next() {
		var c CardDTO
		if err := rows.Scan(&c.ID, &c.SetID, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL); err != nil {
			return nil, err
		}
		c.Variants = []VariantDTO{}
		cardIndex[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return cards, nil
	}
	return hydrateVariantsCtx(ctx, db, userID, cards, cardIndex)
}

// loadSetByID is a context-aware variant of loadSet that does not depend on a
// live *http.Request. The scan handler runs inside an HTTP context, but
// helpers that only need a Context (e.g. background callers added later) can
// reuse the same query without faking a request.
func loadSetByID(ctx context.Context, db *sql.DB, userID int64, setID string) (*SetDTO, error) {
	var s SetDTO
	err := db.QueryRowContext(ctx, `
		SELECT s.id, s.name, s.series, s.release_date, s.total_cards, s.symbol_url, s.logo_url,
		       (SELECT COUNT(DISTINCT pc.card_id)
		        FROM pokemon_collections pc
		        JOIN pokemon_cards c ON c.id = pc.card_id
		        WHERE c.set_id = s.id AND pc.user_id = ? AND pc.quantity > 0) AS owned_count
		FROM pokemon_sets s
		WHERE s.id = ?
	`, userID, setID).Scan(&s.ID, &s.Name, &s.Series, &s.ReleaseDate, &s.TotalCards, &s.SymbolURL, &s.LogoURL, &s.OwnedCount)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// hydrateVariantsCtx is the context-only equivalent of hydrateVariants. The
// existing helper takes *http.Request because every other caller already has
// one; the scan handler does too but we want a callable seam for future
// background scans.
func hydrateVariantsCtx(ctx context.Context, db *sql.DB, userID int64, cards []CardDTO, cardIndex map[string]int) ([]CardDTO, error) {
	ids := make([]any, 0, len(cardIndex))
	placeholders := make([]string, 0, len(cardIndex))
	for id := range cardIndex {
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}
	query := `
		SELECT v.id, v.card_id, v.kind, v.price_eur, v.price_at,
		       c.id, c.quantity, c.condition, c.acquired_at, c.notes_enc
		FROM pokemon_card_variants v
		LEFT JOIN pokemon_collections c
		  ON c.variant_id = v.id AND c.user_id = ?
		WHERE v.card_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY v.card_id, v.kind
	`
	args := append([]any{userID}, ids...)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			vID         int64
			cardID      string
			kind        string
			priceEUR    float64
			priceAt     sql.NullString
			collID      sql.NullInt64
			collQty     sql.NullInt64
			collCond    sql.NullString
			collAcq     sql.NullString
			collNotesEn sql.NullString
		)
		if err := rows.Scan(&vID, &cardID, &kind, &priceEUR, &priceAt,
			&collID, &collQty, &collCond, &collAcq, &collNotesEn); err != nil {
			return nil, err
		}
		idx, ok := cardIndex[cardID]
		if !ok {
			continue
		}
		v := VariantDTO{
			ID:       vID,
			Kind:     kind,
			PriceEUR: priceEUR,
		}
		if priceAt.Valid && priceAt.String != "" {
			ts := priceAt.String
			v.PriceAt = &ts
		}
		if collID.Valid {
			id := collID.Int64
			v.Owned = true
			v.OwnedID = &id
			if collQty.Valid {
				v.Quantity = int(collQty.Int64)
			}
			if collCond.Valid {
				v.Condition = collCond.String
			}
			if collAcq.Valid && collAcq.String != "" {
				ts := collAcq.String
				v.AcquiredAt = &ts
			}
			v.Notes = decryptNotes(collNotesEn)
		}
		cards[idx].Variants = append(cards[idx].Variants, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}
