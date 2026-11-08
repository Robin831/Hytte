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
	"time"
)

// TCGdexBaseURL is the production base for TCGdex's REST API (English
// locale). TCGdex is the catalogue source since pokemontcg.io was deprecated
// in favour of the paid Scrydex API (Nov 2026; offline 1 Mar 2027). It's
// free and needs no key.
const TCGdexBaseURL = "https://api.tcgdex.net/v2/en"

// TCGdexClient is a minimal client for the TCGdex endpoints the sync uses.
// Construct via NewTCGdexClient.
type TCGdexClient struct {
	httpClient *http.Client
	baseURL    string
	maxRetries int
	sleep      func(ctx context.Context, d time.Duration) error
	// imageOK reports whether an image URL loads (HEAD 200). A field so
	// tests can fake it.
	imageOK func(ctx context.Context, u string) bool
}

// NewTCGdexClient returns a client with a 30 s HTTP timeout and the public
// base URL. Override via WithTCGdexBaseURL in tests.
func NewTCGdexClient() *TCGdexClient {
	c := &TCGdexClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    TCGdexBaseURL,
		maxRetries: 2,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
	c.imageOK = c.headOK
	return c
}

// headOK sends a HEAD request and reports whether it returned 200.
func (c *TCGdexClient) headOK(ctx context.Context, u string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

// TCGdexSetBrief is one entry of GET /sets.
type TCGdexSetBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TCGdexSet mirrors GET /sets/{id}. We only declare the fields the sync
// reads; TCGdex returns more.
type TCGdexSet struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Logo        string              `json:"logo"`
	Symbol      string              `json:"symbol"`
	ReleaseDate string              `json:"releaseDate"`
	CardCount   TCGdexCardCount     `json:"cardCount"`
	Cards       []TCGdexCardSummary `json:"cards"`
	Serie       struct {
		ID   string `json:"id"`
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

// TCGdexCardSummary is the per-card entry inside the set response. It has
// no rarity or prices; those need the per-card GetCard fetch.
type TCGdexCardSummary struct {
	ID      string `json:"id"`
	LocalID string `json:"localId"`
	Name    string `json:"name"`
	Image   string `json:"image"`
}

// TCGdexCard is the per-card detail shape. We pluck just the fields we map
// into our DB.
type TCGdexCard struct {
	ID      string        `json:"id"`
	LocalID string        `json:"localId"`
	Name    string        `json:"name"`
	Rarity  string        `json:"rarity"`
	Image   string        `json:"image"`
	Pricing TCGdexPricing `json:"pricing"`
}

// TCGdexPricing surfaces the card's cardmarket block (EUR). TCGdex also
// carries tcgplayer (USD), which we don't use.
//
// We read the card-level block, not variants_detailed: the latter lists
// every print of the card (stamped, gold-border, promo…) and several share
// a type, so picking "the holo entry" there can land on a €60 special print
// of a €0.10 card.
type TCGdexPricing struct {
	Cardmarket *TCGdexCardmarket `json:"cardmarket"`
	// TCGPlayer maps print → {productId,…} plus "unit"/"updated" strings;
	// we only read the productId (the same for every print of a card).
	TCGPlayer map[string]json.RawMessage `json:"tcgplayer"`
}

// tcgplayerProductID returns the card's TCGplayer productId, or 0.
func (p TCGdexPricing) tcgplayerProductID() int64 {
	for _, raw := range p.TCGPlayer {
		var v struct {
			ProductID int64 `json:"productId"`
		}
		if json.Unmarshal(raw, &v) == nil && v.ProductID > 0 {
			return v.ProductID
		}
	}
	return 0
}

// TCGdexCardmarket is the card's Cardmarket product: the plain print's
// prices and the "-holo" lane, which for modern cards is the reverse holo.
// Same model as pokemontcg.io's cardmarket.prices (trendPrice /
// reverseHoloTrend), so prices carry over consistently.
type TCGdexCardmarket struct {
	Updated   string   `json:"updated"`
	IDProduct int64    `json:"idProduct"`
	Avg       *float64 `json:"avg"`
	Trend     *float64 `json:"trend"`
	AvgHolo   *float64 `json:"avg-holo"`
	TrendHolo *float64 `json:"trend-holo"`
}

// ListSets fetches GET /sets — every set, id and name only.
func (c *TCGdexClient) ListSets(ctx context.Context) ([]TCGdexSetBrief, error) {
	var out []TCGdexSetBrief
	if err := c.doRequest(ctx, c.baseURL+"/sets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSet fetches /sets/{id} including the embedded card summaries.
func (c *TCGdexClient) GetSet(ctx context.Context, id string) (*TCGdexSet, error) {
	var out TCGdexSet
	if err := c.doRequest(ctx, c.baseURL+"/sets/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCard fetches /cards/{id} — full detail with rarity and pricing.
func (c *TCGdexClient) GetCard(ctx context.Context, id string) (*TCGdexCard, error) {
	var out TCGdexCard
	if err := c.doRequest(ctx, c.baseURL+"/cards/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// errTCGdexNotFound marks a 404, which isn't worth retrying.
var errTCGdexNotFound = fmt.Errorf("tcgdex: not found")

// doRequest GETs u and decodes the JSON body into out, retrying 429 and 5xx
// responses with a short backoff.
func (c *TCGdexClient) doRequest(ctx context.Context, u string, out any) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, time.Duration(attempt)*2*time.Second); err != nil {
				return err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("http get %s: %w", u, err)
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return fmt.Errorf("%w: %s", errTCGdexNotFound, u)
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("tcgdex %d: %s", resp.StatusCode, u)
			continue
		}
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return fmt.Errorf("tcgdex %d: %s — %s", resp.StatusCode, u, string(body))
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("decode tcgdex response: %w", err)
		}
		return nil
	}
	return lastErr
}

// setIDPattern splits a pokemontcg.io-style set ID into its letter prefix
// and digit-bearing tail (handling the "pt5" suffix). e.g. "me2pt5" →
// ("me", "2", "pt5"); "sv10" → ("sv", "10", ""); "sve" → ("sve", "", "").
var setIDPattern = regexp.MustCompile(`^([a-z]+)(\d+)?(pt5)?$`)

// ourSetIDToTCGdex translates a pokemontcg.io-style set ID (the ids of the
// sets we had before the switch) into TCGdex's zero-padded format. Used as
// one of the set-matching rules. Examples:
//
//	me1     → me01
//	me2pt5  → me02.5
//	sv3pt5  → sv03.5
//	svp     → svp     (no digits; passthrough)
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

// pickTCGdexPrice maps a card's Cardmarket block to our two variants:
// normal = trend (else avg); reverse_holofoil = trend-holo (else avg-holo).
// Trend first, as the pokemontcg.io sync did, so collection values don't
// jump at the switch. Zero means unpriced; the UI shows "—".
func pickTCGdexPrice(cm *TCGdexCardmarket) (normal, reverseHolo float64) {
	if cm == nil {
		return 0, 0
	}
	return firstPositive(cm.Trend, cm.Avg), firstPositive(cm.TrendHolo, cm.AvgHolo)
}

func firstPositive(vals ...*float64) float64 {
	for _, v := range vals {
		if v != nil && *v > 0 {
			return *v
		}
	}
	return 0
}
