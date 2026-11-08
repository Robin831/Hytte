package pokemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Most Norwegian card shops run on Shopify, which serves its catalogue as
// public JSON: /collections.json, /collections/<handle>/products.json, and
// /search/suggest.json for search. One generic reader covers all of them;
// a shop is added by its address. We read slowly (one request a second per
// shop) and identify ourselves.

const shopUserAgent = "Hytte-collection/1.0 (private family card collection; +https://robinedvardsmith.com)"

// shopifyClient reads one shop. Requests are spaced by minGap.
type shopifyClient struct {
	base   string
	hc     *http.Client
	minGap time.Duration
	mu     sync.Mutex
	last   time.Time
}

func newShopifyClient(base string, hc *http.Client) *shopifyClient {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &shopifyClient{base: strings.TrimRight(base, "/"), hc: hc, minGap: time.Second}
}

func (c *shopifyClient) get(ctx context.Context, path string, out any) error {
	c.mu.Lock()
	if wait := c.minGap - time.Since(c.last); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			c.mu.Unlock()
			return ctx.Err()
		case <-t.C:
		}
	}
	c.last = time.Now()
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", shopUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s%s: HTTP %d %s", c.base, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return fmt.Errorf("%s%s: not JSON (is this a Shopify shop?)", c.base, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type shopifyCollection struct {
	Handle        string `json:"handle"`
	Title         string `json:"title"`
	ProductsCount int    `json:"products_count"`
}

type shopifyVariant struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Price     string `json:"price"`
	Available bool   `json:"available"`
	SKU       string `json:"sku"`
}

type shopifyProduct struct {
	Title    string           `json:"title"`
	Handle   string           `json:"handle"`
	Tags     []string         `json:"tags"`
	Variants []shopifyVariant `json:"variants"`
}

// UnmarshalJSON accepts tags as a list (products.json) or a comma-separated
// string (some endpoints).
func (p *shopifyProduct) UnmarshalJSON(b []byte) error {
	type alias shopifyProduct
	var raw struct {
		alias
		Tags json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*p = shopifyProduct(raw.alias)
	p.Tags = nil
	var list []string
	if json.Unmarshal(raw.Tags, &list) == nil {
		p.Tags = list
		return nil
	}
	var s string
	if json.Unmarshal(raw.Tags, &s) == nil {
		for _, t := range strings.Split(s, ",") {
			if t = strings.TrimSpace(t); t != "" {
				p.Tags = append(p.Tags, t)
			}
		}
	}
	return nil
}

// collections lists every collection (a few pages at most).
func (c *shopifyClient) collections(ctx context.Context) ([]shopifyCollection, error) {
	var out []shopifyCollection
	for page := 1; page <= 20; page++ {
		var resp struct {
			Collections []shopifyCollection `json:"collections"`
		}
		if err := c.get(ctx, "/collections.json?limit=250&page="+strconv.Itoa(page), &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Collections...)
		if len(resp.Collections) < 250 {
			break
		}
	}
	return out, nil
}

// collectionProducts lists every product in a collection.
func (c *shopifyClient) collectionProducts(ctx context.Context, handle string) ([]shopifyProduct, error) {
	var out []shopifyProduct
	for page := 1; page <= 40; page++ {
		var resp struct {
			Products []shopifyProduct `json:"products"`
		}
		if err := c.get(ctx, "/collections/"+url.PathEscape(handle)+"/products.json?limit=250&page="+strconv.Itoa(page), &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Products...)
		if len(resp.Products) < 250 {
			break
		}
	}
	return out, nil
}

// search finds products by text and loads each hit's variants (the
// suggest endpoint has no variant ids, which cart links need).
func (c *shopifyClient) search(ctx context.Context, q string, keep func(title string) bool) ([]shopifyProduct, error) {
	var resp struct {
		Resources struct {
			Results struct {
				Products []struct {
					Handle string `json:"handle"`
					Title  string `json:"title"`
				} `json:"products"`
			} `json:"results"`
		} `json:"resources"`
	}
	v := url.Values{}
	v.Set("q", q)
	v.Set("resources[type]", "product")
	v.Set("resources[limit]", "10")
	if err := c.get(ctx, "/search/suggest.json?"+v.Encode(), &resp); err != nil {
		return nil, err
	}
	var out []shopifyProduct
	for _, h := range resp.Resources.Results.Products {
		if keep != nil && !keep(h.Title) {
			continue
		}
		var p struct {
			Product shopifyProduct `json:"product"`
		}
		if err := c.get(ctx, "/products/"+url.PathEscape(h.Handle)+".json", &p); err != nil {
			continue
		}
		out = append(out, p.Product)
	}
	return out, nil
}

// findSetCollection picks the shop's collection for a set by its title:
// exact name first, then a title containing the set name ("Mega Perfect
// Order"), preferring the full collection over an in-stock-only one and
// the one with more products. Returns "" when none fits.
func findSetCollection(cols []shopifyCollection, setName string) string {
	want := normName(setName)
	if want == "" {
		return ""
	}
	best, bestScore := "", 0
	for _, c := range cols {
		title := normName(c.Title)
		if !strings.Contains(title, want) && !strings.Contains(normName(c.Handle), want) {
			continue
		}
		score := 1
		if title == want {
			score = 4
		}
		lt := strings.ToLower(c.Title + " " + c.Handle)
		if strings.Contains(lt, "booster") || strings.Contains(lt, "sealed") || strings.Contains(lt, "etb") {
			continue
		}
		if !strings.Contains(lt, "lager") && !strings.Contains(lt, "instock") && !strings.Contains(lt, "in stock") {
			score++
		}
		// Ties go to the bigger collection.
		score = score*100000 + c.ProductsCount
		if score > bestScore {
			best, bestScore = c.Handle, score
		}
	}
	return best
}

// --- matching shop products to cards ----------------------------------

var (
	tcgplayerTag = regexp.MustCompile(`(?i)^tcgplayer[:_-](\d+)$`)
	trailingID   = regexp.MustCompile(`-(\d{5,})$`)
	numberSlash  = regexp.MustCompile(`(?i)\b([a-z]{0,4}\d{1,3}[a-z]?)\s*/\s*([a-z]{0,4}\d{1,3})\b`)
	numberPadded = regexp.MustCompile(`\b(\d{3})\b`)
)

// productTCGplayerID finds a TCGplayer productId on a product: a
// "tcgplayer:<id>" tag, or an id that ends the handle or a SKU.
func productTCGplayerID(p shopifyProduct) int64 {
	for _, t := range p.Tags {
		if m := tcgplayerTag.FindStringSubmatch(strings.TrimSpace(t)); m != nil {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			return n
		}
	}
	cands := []string{p.Handle}
	for _, v := range p.Variants {
		cands = append(cands, v.SKU)
	}
	for _, s := range cands {
		if m := trailingID.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			return n
		}
	}
	return 0
}

// productNumber pulls the collector number from a title: "055/088",
// "Haunter 049 (Reverse Holofoil)", "TG05/TG30".
func productNumber(title string) string {
	if m := numberSlash.FindStringSubmatch(title); m != nil {
		return m[1]
	}
	if m := numberPadded.FindStringSubmatch(title); m != nil {
		return m[1]
	}
	return ""
}

// isForeign reports a product in another language than English.
func isForeign(p shopifyProduct) bool {
	for _, t := range p.Tags {
		lt := strings.ToLower(t)
		if lt == "japanese" || lt == "lang-jp" || lt == "lang-ja" || lt == "japansk" || lt == "korean" || lt == "chinese" {
			return true
		}
	}
	lt := strings.ToLower(p.Title)
	return strings.Contains(lt, "japanese") || strings.Contains(lt, "japansk") || strings.Contains(lt, "(jp)")
}

// printOf names the print from a variant/product title: "reverse",
// "holo", "normal" or "".
func printOf(s string) string {
	ls := strings.ToLower(s)
	switch {
	case strings.Contains(ls, "reverse"):
		return "reverse"
	case strings.Contains(ls, "non holo"), strings.Contains(ls, "non-holo"), strings.Contains(ls, "normal"):
		return "normal"
	case strings.Contains(ls, "holo"):
		return "holo"
	}
	return ""
}

var (
	conditionPhrases = []struct{ phrase, code string }{
		{"near mint", "NM"}, {"lightly played", "LP"}, {"moderately played", "MP"}, {"heavily played", "HP"}, {"damaged", "DMG"},
	}
	conditionCodes = map[string]string{"nm": "NM", "lp": "LP", "mp": "MP", "hp": "HP", "dmg": "DMG"}
	wordSplit      = regexp.MustCompile(`[^a-z]+`)
)

// conditionOf reads a card condition (NM, LP, …) from a variant title.
func conditionOf(s string) string {
	ls := strings.ToLower(s)
	for _, c := range conditionPhrases {
		if strings.Contains(ls, c.phrase) {
			return c.code
		}
	}
	for _, w := range wordSplit.Split(ls, -1) {
		if code, ok := conditionCodes[w]; ok {
			return code
		}
	}
	return ""
}

// setCard is a card of the set being matched.
type setCard struct {
	id, name, no string
	tcgplayerID  int64
}

// matchProduct finds which card of the set a product is: by TCGplayer id,
// else by collector number with a compatible name. "" = not a card of this
// set (sealed product, another language, another set).
func matchProduct(p shopifyProduct, cards []setCard) string {
	if isForeign(p) {
		return ""
	}
	if id := productTCGplayerID(p); id > 0 {
		for _, c := range cards {
			if c.tcgplayerID == id {
				return c.id
			}
		}
	}
	no := productNumber(p.Title)
	if no == "" {
		return ""
	}
	for _, c := range cards {
		if collectorKey(c.no) == collectorKey(no) && strings.Contains(normName(p.Title), normName(c.name)) {
			return c.id
		}
	}
	return ""
}

// shopOffer is one buyable variant of a card.
type shopOffer struct {
	cardID    string
	variantID int64
	title     string
	print     string
	condition string
	priceNOK  float64
	available bool
	url       string
}

// offersFrom turns matched products into offers.
func offersFrom(base string, products []shopifyProduct, cards []setCard) []shopOffer {
	var out []shopOffer
	seen := map[int64]bool{}
	for _, p := range products {
		cardID := matchProduct(p, cards)
		if cardID == "" {
			continue
		}
		for _, v := range p.Variants {
			if seen[v.ID] {
				continue
			}
			seen[v.ID] = true
			price, err := strconv.ParseFloat(v.Price, 64)
			if err != nil || price <= 0 {
				continue
			}
			pr := printOf(v.Title)
			if pr == "" {
				pr = printOf(p.Title)
			}
			if pr == "" {
				// Some shops only say it in tags ("Non-Holo", "Holo Foil").
				pr = printOf(strings.Join(p.Tags, " "))
			}
			out = append(out, shopOffer{
				cardID: cardID, variantID: v.ID, title: p.Title, print: pr, condition: conditionOf(v.Title),
				priceNOK: price, available: v.Available,
				url: strings.TrimRight(base, "/") + "/products/" + p.Handle + "?variant=" + strconv.FormatInt(v.ID, 10),
			})
		}
	}
	return out
}

// shippingEstimate asks the shop's cart what delivery to postcode costs
// for one item and returns the cheapest paid option (pickup and "hold
// until free" options are 0 kr and skipped). Used to prefill a new shop.
func shippingEstimate(ctx context.Context, base string, variantID int64, zip string) (float64, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return 0, err
	}
	hc := &http.Client{Timeout: 30 * time.Second, Jar: jar}
	form := url.Values{"id": {strconv.FormatInt(variantID, 10)}, "quantity": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/cart/add.js", strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", shopUserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	q := url.Values{"shipping_address[zip]": {zip}, "shipping_address[country]": {"Norway"}}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/cart/shipping_rates.json?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", shopUserAgent)
	resp, err = hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var rates struct {
		ShippingRates []struct {
			Price string `json:"price"`
		} `json:"shipping_rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rates); err != nil {
		return 0, err
	}
	best := 0.0
	for _, r := range rates.ShippingRates {
		p, err := strconv.ParseFloat(r.Price, 64)
		if err != nil || p <= 0 {
			continue
		}
		if best == 0 || p < best {
			best = p
		}
	}
	return best, nil
}
