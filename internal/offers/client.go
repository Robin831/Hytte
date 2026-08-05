package offers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// The Tjek "squid" API is the JSON backend that mattilbud.no's own frontend
// calls from the browser. We mirror it once daily, which is far lighter than a
// single human browsing session, and identify ourselves via User-Agent.
const (
	tjekBaseURL = "https://squid-api.tjek.com"

	// defaultAPIKey is the public web API key embedded in mattilbud.no's JS
	// bundle (the same one every browser visiting the site uses). Override
	// with OFFERS_TJEK_API_KEY if it rotates.
	defaultAPIKey = "QPq_vh"

	userAgent = "Hytte/1.0 (+https://robinedvardsmith.com)"

	// Home location for regional catalog selection (Bergen). National chains
	// return the same catalog regardless; a real location keeps regional
	// dealers (e.g. Obs) accurate.
	homeLat    = "60.3913"
	homeLng    = "5.3221"
	homeRadius = "100000"

	pageLimit = 100
	// maxPages bounds pagination per dealer; the largest chain currently has
	// ~300 offers, so 10 pages (1000 offers) is comfortable headroom.
	maxPages = 10

	maxResponseBytes int64 = 4 << 20
	maxRetries             = 3
)

// Dealers maps the Tjek dealer id of every Norwegian chain on mattilbud.no to
// its display name. Swedish border shops are deliberately excluded.
var Dealers = map[string]string{
	"257bxm": "KIWI",
	"faa0Ym": "REMA 1000",
	"80742m": "Extra",
	"51dawm": "Obs",
	"5b11sm": "Bunnpris",
	"4333pm": "MENY",
	"c062vm": "SPAR",
	"b3e8Fm": "Joker",
	"e857Mm": "Europris",
	"f5d5lm": "Coop Prix",
	"de79dm": "Coop Mega",
	"2686gD": "Matkroken",
	"b18dq1": "Jacobs",
	"pR2h9x": "Holdbart",
	"5vk-xt": "Gigaboks",
	"5861Qq": "Nærbutikken",
}

// httpClient is the HTTP client used for upstream requests. Tests can replace
// it (typically together with overrideBaseURL) to point at a httptest server.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// overrideBaseURL, when non-empty, replaces tjekBaseURL. Used by tests.
var overrideBaseURL string

// requestPause is the delay between successive page requests, keeping the
// daily sweep polite. Injectable so tests don't wait.
var requestPause = func(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(300 * time.Millisecond):
	}
}

func apiKey() string {
	if k := os.Getenv("OFFERS_TJEK_API_KEY"); k != "" {
		return k
	}
	return defaultAPIKey
}

// FetchDealerOffers pages through all current offers for one dealer.
func FetchDealerOffers(ctx context.Context, dealerID string) ([]Offer, error) {
	base := tjekBaseURL
	if overrideBaseURL != "" {
		base = overrideBaseURL
	}

	var out []Offer
	for page := 0; page < maxPages; page++ {
		q := url.Values{
			"dealer_ids": {dealerID},
			"api_key":    {apiKey()},
			"r_lat":      {homeLat},
			"r_lng":      {homeLng},
			"r_radius":   {homeRadius},
			"limit":      {strconv.Itoa(pageLimit)},
			"offset":     {strconv.Itoa(page * pageLimit)},
		}
		batch, err := fetchPage(ctx, base+"/v2/offers?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("dealer %s page %d: %w", dealerID, page, err)
		}
		for _, raw := range batch {
			out = append(out, convertOffer(raw))
		}
		if len(batch) < pageLimit {
			break
		}
		requestPause(ctx)
	}
	return out, nil
}

// fetchPage performs one GET with bounded retries on 429/5xx.
func fetchPage(ctx context.Context, fullURL string) ([]tjekOffer, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			requestPause(ctx)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build tjek request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch tjek: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1024)) //nolint:errcheck
			resp.Body.Close()
			lastErr = fmt.Errorf("tjek: HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("tjek: HTTP %d: %s", resp.StatusCode, string(body))
		}

		var batch []tjekOffer
		err = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&batch)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode tjek response: %w", err)
		}
		return batch, nil
	}
	return nil, lastErr
}

// unitConversions maps a lowercased, trimmed Tjek unit symbol to the base unit
// we express prices in and the factor that converts a raw size into that base
// unit. Sub-units are scaled up so a 500 g item and a 1 kg item are both priced
// per kg and stay comparable side by side in the same list. Symbols outside
// this table (including "pcs"/"stk") deliberately yield no unit price.
var unitConversions = map[string]struct {
	label  string
	factor float64
}{
	"g":  {"kg", 1.0 / 1000},
	"kg": {"kg", 1},
	"ml": {"l", 1.0 / 1000},
	"cl": {"l", 1.0 / 100},
	"dl": {"l", 1.0 / 10},
	"l":  {"l", 1},
}

// normaliseUnit resolves a raw Tjek unit symbol to a base unit label and the
// multiplier converting the raw size into that base unit. ok is false for empty
// or unrecognised symbols, which must not produce a misleading unit price.
func normaliseUnit(symbol string) (label string, factor float64, ok bool) {
	c, ok := unitConversions[strings.ToLower(strings.TrimSpace(symbol))]
	if !ok {
		return "", 0, false
	}
	return c.label, c.factor, true
}

// convertOffer maps a Tjek offer onto our storage model, deriving the unit
// price (price per kg or per liter when the quantity is known, else per piece
// for multi-packs) and normalizing validity timestamps to dates.
func convertOffer(t tjekOffer) Offer {
	o := Offer{
		ID:          t.ID,
		DealerID:    t.DealerID,
		DealerName:  t.Dealer.Name,
		Heading:     t.Heading,
		Description: t.Description,
		Price:       t.Pricing.Price,
		Currency:    t.Pricing.Currency,
		ImageURL:    t.Images.Thumb,
		RunFrom:     dateOnly(t.RunFrom),
		RunTill:     dateOnly(t.RunTill),
	}
	if o.DealerName == "" {
		o.DealerName = Dealers[o.DealerID]
	}
	if t.Pricing.PrePrice != nil && *t.Pricing.PrePrice > t.Pricing.Price {
		o.PrePrice = *t.Pricing.PrePrice
	}
	size := 0.0
	if t.Quantity.Size.From != nil {
		size = *t.Quantity.Size.From
	}
	pieces := 0.0
	if t.Quantity.Pieces.From != nil {
		pieces = *t.Quantity.Pieces.From
	}
	if t.Quantity.Unit != nil && size > 0 && (size != 1 || t.Quantity.Unit.Symbol != "pcs") {
		if label, factor, ok := normaliseUnit(t.Quantity.Unit.Symbol); ok {
			if base := size * factor; base > 0 {
				o.UnitPrice = round2(o.Price / base)
				o.UnitLabel = label
			}
		}
	}
	// Unknown or missing units fall through to the per-piece price so
	// multi-packs still get something comparable.
	if o.UnitLabel == "" && pieces > 1 {
		o.UnitPrice = round2(o.Price / pieces)
		o.UnitLabel = "stk"
	}
	return o
}

func dateOnly(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
