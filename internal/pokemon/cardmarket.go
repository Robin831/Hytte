package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// CardmarketPriceGuideURL is Cardmarket's free daily price export for
// Pokémon (game 6): every product's avg/low/trend prices, plain and holo,
// in EUR. It's what TCGdex's prices come from, in one ~16 MB download
// instead of one request per card. Cardmarket offers no API to private
// apps; these exports are its replacement.
const CardmarketPriceGuideURL = "https://downloads.s3.cardmarket.com/productCatalog/priceGuide/price_guide_6.json"

// cardmarketGuideURL is a variable so tests can point it at a fake server.
var cardmarketGuideURL = CardmarketPriceGuideURL

// CardmarketProductURL is the card's product page on Cardmarket, filtered
// to English Near Mint offers.
func CardmarketProductURL(idProduct int64) string {
	if idProduct <= 0 {
		return ""
	}
	return "https://www.cardmarket.com/en/Pokemon/Products?idProduct=" + strconv.FormatInt(idProduct, 10) + "&language=1&minCondition=2"
}

type priceGuideEntry struct {
	IDProduct int64    `json:"idProduct"`
	Avg       *float64 `json:"avg"`
	Trend     *float64 `json:"trend"`
	AvgHolo   *float64 `json:"avg-holo"`
	TrendHolo *float64 `json:"trend-holo"`
}

// fetchPriceGuide downloads the price export and indexes it by idProduct.
func fetchPriceGuide(ctx context.Context, hc *http.Client) (map[int64]priceGuideEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardmarketGuideURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download price guide: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download price guide: HTTP %d", resp.StatusCode)
	}
	var file struct {
		PriceGuides []priceGuideEntry `json:"priceGuides"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("decode price guide: %w", err)
	}
	out := make(map[int64]priceGuideEntry, len(file.PriceGuides))
	for _, e := range file.PriceGuides {
		out[e.IDProduct] = e
	}
	return out, nil
}

// applyPriceGuide updates the price of every card with a Cardmarket id:
// normal = trend (else avg), reverse holo = trend-holo (else avg-holo), the
// same picks as pickTCGdexPrice. Returns how many cards got a price.
func applyPriceGuide(ctx context.Context, db *sql.DB, guide map[int64]priceGuideEntry) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, cm_product_id FROM pokemon_cards WHERE cm_product_id > 0`)
	if err != nil {
		return 0, err
	}
	var details []cardDetail
	for rows.Next() {
		var id string
		var pid int64
		if err := rows.Scan(&id, &pid); err != nil {
			rows.Close()
			return 0, err
		}
		e, ok := guide[pid]
		if !ok {
			continue
		}
		n, r := firstPositive(e.Trend, e.Avg), firstPositive(e.TrendHolo, e.AvgHolo)
		if n > 0 || r > 0 {
			details = append(details, cardDetail{ourID: id, normal: n, rev: r})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return len(details), applyCardDetails(ctx, db, details, false)
}
