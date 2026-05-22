package pokemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TCGdexBaseURL is the production base for TCGdex's REST API (English locale).
// Unlike pokemontcg.io, TCGdex serves all four TCGs (Pokémon, MTG, Lorcana,
// Yu-Gi-Oh!) under one host with a locale-prefixed path. We use this client
// only as a fallback for sets pokemontcg.io v2 can't price (today: the `me*`
// Mega Evolution series).
const TCGdexBaseURL = "https://api.tcgdex.net/v2/en"

// TCGdexClient is a minimal client for the subset of TCGdex endpoints the
// hybrid sync uses. No auth required on the free public tier; we don't carry
// keys here. Construct via NewTCGdexClient.
type TCGdexClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewTCGdexClient returns a client with a 30 s HTTP timeout and the public
// base URL. Override via WithTCGdexBaseURL in tests.
func NewTCGdexClient() *TCGdexClient {
	return &TCGdexClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    TCGdexBaseURL,
	}
}

// WithTCGdexBaseURL points the client at an httptest server. Test-only.
func (c *TCGdexClient) WithTCGdexBaseURL(u string) *TCGdexClient {
	c.baseURL = u
	return c
}

// WithHTTPClient swaps the underlying *http.Client. Test-only.
func (c *TCGdexClient) WithHTTPClient(hc *http.Client) *TCGdexClient {
	c.httpClient = hc
	return c
}

// TCGdexSet mirrors the response shape of GET /sets/{id}. We only declare
// the fields the sync actually reads; TCGdex returns more.
type TCGdexSet struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Logo      string             `json:"logo"`
	Symbol    string             `json:"symbol"`
	CardCount TCGdexCardCount    `json:"cardCount"`
	Cards     []TCGdexCardSummary `json:"cards"`
	Serie     struct {
		Name string `json:"name"`
	} `json:"serie"`
}

// TCGdexCardCount mirrors set.cardCount. `Total` is the full-set figure
// (including secret rares), `Official` is the printed denominator on the
// card face. Matches our (total_cards, printed_total) pair.
type TCGdexCardCount struct {
	Total    int `json:"total"`
	Official int `json:"official"`
}

// TCGdexCardSummary is the per-card entry inside the set response — id,
// localId, name, image. NO pricing on this shape; pricing requires the
// per-card detail fetch via GetCard.
type TCGdexCardSummary struct {
	ID      string `json:"id"`
	LocalID string `json:"localId"`
	Name    string `json:"name"`
	Image   string `json:"image"`
}

// TCGdexCard is the per-card detail shape. We pluck just the fields we map
// into our DB; everything else (hp, attacks, abilities, types) is ignored
// for now.
type TCGdexCard struct {
	ID              string                  `json:"id"`
	LocalID         string                  `json:"localId"`
	Name            string                  `json:"name"`
	Rarity          string                  `json:"rarity"`
	Image           string                  `json:"image"`
	Variants        TCGdexVariants          `json:"variants"`
	VariantsDetail  []TCGdexVariantDetail   `json:"variants_detailed"`
}

// TCGdexVariants is the boolean-flag summary of which print variants exist.
// Useful as a sanity check when picking which variants_detailed entry to
// treat as our "normal" lane.
type TCGdexVariants struct {
	FirstEdition bool `json:"firstEdition"`
	Holo         bool `json:"holo"`
	Normal       bool `json:"normal"`
	Reverse      bool `json:"reverse"`
	WPromo       bool `json:"wPromo"`
}

// TCGdexVariantDetail is one entry of variants_detailed. Each entry carries
// its OWN pricing block. `Type` is one of {normal, holo, reverse,
// firstEdition, wPromo}; we map to our two-variant model (normal,
// reverse_holofoil) below.
type TCGdexVariantDetail struct {
	Type      string         `json:"type"`
	VariantID string         `json:"variantId"`
	Pricing   TCGdexPricing  `json:"pricing"`
}

// TCGdexPricing surfaces cardmarket + tcgplayer blocks. We only consume
// cardmarket today; tcgplayer is left as a JSON.RawMessage placeholder for
// a future migration step if we ever want USD prices too.
type TCGdexPricing struct {
	Cardmarket *TCGdexCardmarket `json:"cardmarket"`
}

// TCGdexCardmarket is the EUR-denominated Cardmarket price block. Many fields
// are tracked — we only need `avg` (current average) for our normal lane,
// optionally falling back to `trend` (smoothed recent trend) if `avg` is
// nil/zero. `avg-holo` etc. are sibling lanes within this same block;
// per TCGdex's data model they apply when the SAME variant has an additional
// holo printing — we don't currently distinguish that.
type TCGdexCardmarket struct {
	Updated   string   `json:"updated"`
	Unit      string   `json:"unit"`
	IDProduct int64    `json:"idProduct"`
	Avg       *float64 `json:"avg"`
	Low       *float64 `json:"low"`
	Trend     *float64 `json:"trend"`
}

// GetSet fetches /sets/{id} including the embedded cards summary array.
// Used to enumerate which cards to detail-fetch for pricing.
func (c *TCGdexClient) GetSet(ctx context.Context, id string) (*TCGdexSet, error) {
	var out TCGdexSet
	if err := c.doRequest(ctx, c.baseURL+"/sets/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCard fetches /cards/{id} — full detail with pricing.
func (c *TCGdexClient) GetCard(ctx context.Context, id string) (*TCGdexCard, error) {
	var out TCGdexCard
	if err := c.doRequest(ctx, c.baseURL+"/cards/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *TCGdexClient) doRequest(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("tcgdex %d: %s — %s", resp.StatusCode, u, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode tcgdex response: %w", err)
	}
	return nil
}

// tcgdexFallbackSets is the allowlist of pokemontcg.io set IDs for which we
// dispatch pricing fetches to TCGdex instead. Initial coverage: the Mega
// Evolution series where pokemontcg.io v2's Cardmarket bridge is empty. New
// entries land here as additional gaps surface.
var tcgdexFallbackSets = map[string]struct{}{
	"me1":    {},
	"me2":    {},
	"me2pt5": {},
	"me3":    {},
	"me4":    {}, // TCGdex doesn't have me04 yet (just-released today) — the
	// per-card lookups will 404 harmlessly; sync continues. Once TCGdex
	// ingests Chaos Rising, the next RefreshPrices fills it in automatically.
}

// shouldUseTCGdex reports whether a set ID should fetch prices from TCGdex
// instead of pokemontcg.io v2. Exact match — we deliberately don't pattern-
// match because pokemontcg.io's set ID convention has enough irregularity
// (sv3pt5, svp, sve, …) that a regex invites false positives.
func shouldUseTCGdex(setID string) bool {
	_, ok := tcgdexFallbackSets[setID]
	return ok
}

// setIDPattern splits a pokemontcg.io-style set ID into its letter prefix
// and digit-bearing tail (handling the "pt5" suffix). e.g. "me2pt5" →
// ("me", "2", "pt5"); "sv10" → ("sv", "10", ""); "sve" → ("sve", "", "").
var setIDPattern = regexp.MustCompile(`^([a-z]+)(\d+)?(pt5)?$`)

// ourSetIDToTCGdex translates a pokemontcg.io-style set ID (our DB
// convention) into TCGdex's zero-padded format. Examples:
//
//	me1     → me01
//	me2pt5  → me02.5
//	sv1     → sv01
//	sv3pt5  → sv03.5
//	svp     → svp     (no digits; passthrough)
//
// Falls back to the input unchanged if it doesn't fit the pattern — TCGdex
// will then 404 and the sync logs+continues without aborting.
func ourSetIDToTCGdex(id string) string {
	m := setIDPattern.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	prefix, num, pt5 := m[1], m[2], m[3]
	if num == "" {
		return id
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return id
	}
	out := fmt.Sprintf("%s%02d", prefix, n)
	if pt5 != "" {
		out += ".5"
	}
	return out
}

// cardIDPattern splits a pokemontcg.io-style card ID into its set portion
// and collector-number tail. e.g. "me2pt5-21" → ("me2pt5", "21").
var cardIDPattern = regexp.MustCompile(`^(.+)-([^-]+)$`)

// ourCardIDToTCGdex translates one of our card IDs (e.g. "me1-21") to
// TCGdex's zero-padded format (e.g. "me01-021"). Non-numeric collector
// numbers (rare, like "SWSH123") pass through unchanged on the tail.
func ourCardIDToTCGdex(id string) string {
	m := cardIDPattern.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	setPart, collector := m[1], m[2]
	tcgSet := ourSetIDToTCGdex(setPart)
	// Zero-pad purely-numeric collector numbers to 3 digits to match
	// TCGdex's convention. Letter-prefixed forms like "SWSH123" pass
	// through unchanged.
	if n, err := strconv.Atoi(collector); err == nil {
		return fmt.Sprintf("%s-%03d", tcgSet, n)
	}
	return tcgSet + "-" + collector
}

// pickTCGdexPrice converts a TCGdex variants_detailed array into our
// two-variant model. Mapping:
//
//	type=="reverse"      → our "reverse_holofoil" lane
//	type=="normal"       → our "normal" lane
//	type=="holo"         → our "normal" lane *if and only if* no "normal"
//	                       entry exists (some cards — ex/V/VMAX — only
//	                       come as holo, with no plain-print version)
//
// Each lane's price comes from cardmarket.avg, falling back to cardmarket.trend
// if avg is null. Returns zero for any lane without a matching variant. We
// pass `name` only for log noise on the rare cards with no usable pricing.
func pickTCGdexPrice(variants []TCGdexVariantDetail) (normal, reverseHolo float64) {
	var normalEntry, holoEntry, reverseEntry *TCGdexCardmarket
	for i := range variants {
		v := variants[i]
		if v.Pricing.Cardmarket == nil {
			continue
		}
		switch strings.ToLower(v.Type) {
		case "normal":
			normalEntry = v.Pricing.Cardmarket
		case "holo":
			holoEntry = v.Pricing.Cardmarket
		case "reverse":
			reverseEntry = v.Pricing.Cardmarket
		}
	}
	// Prefer the "normal" print, fall back to "holo" when normal doesn't
	// exist for this card (ex / V / VMAX prints that are holo-only).
	normalSrc := normalEntry
	if normalSrc == nil {
		normalSrc = holoEntry
	}
	normal = pickNonZero(normalSrc)
	reverseHolo = pickNonZero(reverseEntry)
	return normal, reverseHolo
}

// pickNonZero returns avg if non-nil and >0, else trend. Returns 0 when both
// are missing — the upsertVariants logic treats 0 as "no price recorded yet"
// and shows "—" in the UI (post Hytte-bivr).
func pickNonZero(c *TCGdexCardmarket) float64 {
	if c == nil {
		return 0
	}
	if c.Avg != nil && *c.Avg > 0 {
		return *c.Avg
	}
	if c.Trend != nil && *c.Trend > 0 {
		return *c.Trend
	}
	return 0
}

// tcgdexParallelism caps the per-card detail fetches we run concurrently
// when populating one set. TCGdex's free tier doesn't publish a hard rate
// limit; 6 in flight is conservative and finishes a 200-card set in well
// under a minute.
const tcgdexParallelism = 6

// fetchTCGdexCardPrices fetches per-card pricing for every card in a set
// concurrently (bounded by tcgdexParallelism) and returns a map from our
// card ID (e.g. "me1-21") to the (normal, reverse_holofoil) EUR prices.
// Per-card failures (404, network, parse) are logged and skipped — the
// remaining cards still land.
func fetchTCGdexCardPrices(
	ctx context.Context,
	client *TCGdexClient,
	setID string,
	tcgdexCardIDs []string,
	ourCardIDs []string,
) map[string][2]float64 {
	type slot struct {
		ourID    string
		tcgdexID string
	}
	work := make([]slot, len(tcgdexCardIDs))
	for i := range tcgdexCardIDs {
		work[i] = slot{ourID: ourCardIDs[i], tcgdexID: tcgdexCardIDs[i]}
	}

	prices := make(map[string][2]float64, len(work))
	var mu sync.Mutex
	sem := make(chan struct{}, tcgdexParallelism)
	var wg sync.WaitGroup

	for _, s := range work {
		if ctx.Err() != nil {
			break
		}
		s := s
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			card, err := client.GetCard(ctx, s.tcgdexID)
			if err != nil {
				// Common: TCGdex 404 for a card it hasn't ingested yet (e.g.
				// me04 today). Skip silently to keep log noise down for the
				// expected case; persistent errors will surface via the
				// missing-price aggregate.
				return
			}
			n, r := pickTCGdexPrice(card.VariantsDetail)
			if n == 0 && r == 0 {
				return
			}
			mu.Lock()
			prices[s.ourID] = [2]float64{n, r}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return prices
}
