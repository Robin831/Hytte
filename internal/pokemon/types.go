package pokemon

// SetsResponse is the paginated /v2/sets response from pokemontcg.io.
type SetsResponse struct {
	Data       []Set `json:"data"`
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	Count      int   `json:"count"`
	TotalCount int   `json:"totalCount"`
}

// Set is a Pokémon TCG set as returned by /v2/sets.
type Set struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Series       string    `json:"series"`
	PrintedTotal int       `json:"printedTotal"`
	Total        int       `json:"total"`
	ReleaseDate  string    `json:"releaseDate"`
	Images       SetImages `json:"images"`
}

// SetImages holds the symbol/logo URLs returned for a set.
type SetImages struct {
	Symbol string `json:"symbol"`
	Logo   string `json:"logo"`
}

// CardsResponse is the paginated /v2/cards response from pokemontcg.io.
type CardsResponse struct {
	Data       []Card `json:"data"`
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
	Count      int    `json:"count"`
	TotalCount int    `json:"totalCount"`
}

// Card is a single card record from /v2/cards.
type Card struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Number     string     `json:"number"`
	Rarity     string     `json:"rarity"`
	Images     CardImages `json:"images"`
	Cardmarket Cardmarket `json:"cardmarket"`
}

// CardImages holds small/large image URLs for a card.
type CardImages struct {
	Small string `json:"small"`
	Large string `json:"large"`
}

// Cardmarket is the price block from the API. Prices is a flat object of
// metric fields (not a map of variant objects) — see CardmarketPrices.
type Cardmarket struct {
	URL       string           `json:"url"`
	UpdatedAt string           `json:"updatedAt"`
	Prices    CardmarketPrices `json:"prices"`
}

// CardmarketPrices is the flat price object from cardmarket.prices in the
// pokemontcg.io /v2/cards response. The API returns a single JSON object
// where each key is a price metric, not a map of variant → price objects.
// We capture the fields that map to our two persisted variant kinds.
type CardmarketPrices struct {
	AverageSellPrice float64 `json:"averageSellPrice"`
	TrendPrice       float64 `json:"trendPrice"`
	ReverseHoloSell  float64 `json:"reverseHoloSell"`
	ReverseHoloTrend float64 `json:"reverseHoloTrend"`
}
