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

// Cardmarket is the price block from the API. Prices are keyed by variant
// name (normal, reverseHolofoil, holofoil, 1stEditionHolofoil, ...).
type Cardmarket struct {
	URL       string                    `json:"url"`
	UpdatedAt string                    `json:"updatedAt"`
	Prices    map[string]CardmarketPrice `json:"prices"`
}

// CardmarketPrice contains the price quotes for one variant. We persist
// trendPrice when available and fall back to averageSellPrice.
type CardmarketPrice struct {
	AverageSellPrice float64 `json:"averageSellPrice"`
	TrendPrice       float64 `json:"trendPrice"`
}
