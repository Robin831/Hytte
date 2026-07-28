// Package offers mirrors weekly grocery offers from the Tjek API (the backend
// behind mattilbud.no) into SQLite and serves them with per-user priority
// keywords ("watchlist").
package offers

import "time"

// Offer is one stored grocery offer, valid for a date window.
type Offer struct {
	ID          string    `json:"id"`
	DealerID    string    `json:"dealer_id"`
	DealerName  string    `json:"dealer_name"`
	Heading     string    `json:"heading"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	PrePrice    float64   `json:"pre_price,omitempty"` // 0 = no before-price known
	Currency    string    `json:"currency"`
	UnitPrice   float64   `json:"unit_price,omitempty"` // 0 = not derivable
	UnitLabel   string    `json:"unit_label,omitempty"` // e.g. "l", "kg", "stk"
	ImageURL    string    `json:"image_url"`
	RunFrom     string    `json:"run_from"` // YYYY-MM-DD
	RunTill     string    `json:"run_till"` // YYYY-MM-DD
	FetchedAt   time.Time `json:"-"`
}

// RankedOffer is an offer annotated for one user's view.
type RankedOffer struct {
	Offer
	DiscountPct     int      `json:"discount_pct,omitempty"`
	MatchedKeywords []string `json:"matched_keywords,omitempty"`
}

// WatchlistEntry is one per-user priority keyword.
type WatchlistEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"-"`
	Keyword   string    `json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
}

// AddWatchRequest is the JSON body for adding a watchlist keyword.
type AddWatchRequest struct {
	Keyword string `json:"keyword"`
}

// tjekOffer mirrors the fields we consume from the Tjek /v2/offers response.
type tjekOffer struct {
	ID          string `json:"id"`
	Heading     string `json:"heading"`
	Description string `json:"description"`
	Pricing     struct {
		Price    float64  `json:"price"`
		PrePrice *float64 `json:"pre_price"`
		Currency string   `json:"currency"`
	} `json:"pricing"`
	Quantity struct {
		Unit *struct {
			Symbol string `json:"symbol"`
		} `json:"unit"`
		Size struct {
			From *float64 `json:"from"`
		} `json:"size"`
		Pieces struct {
			From *float64 `json:"from"`
		} `json:"pieces"`
	} `json:"quantity"`
	Images struct {
		Thumb string `json:"thumb"`
		View  string `json:"view"`
	} `json:"images"`
	RunFrom  string `json:"run_from"`
	RunTill  string `json:"run_till"`
	DealerID string `json:"dealer_id"`
	Dealer   struct {
		Name string `json:"name"`
	} `json:"dealer"`
}
