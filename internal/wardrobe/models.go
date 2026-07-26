package wardrobe

import "time"

// Kid is a child profile in the wardrobe feature. Kids are standalone rows
// owned by a parent user — deliberately not required to be linked users,
// since kindergarten-age children have no Google account. UserID optionally
// links the profile to a real account.
type Kid struct {
	ID          int64     `json:"id"`
	ParentID    int64     `json:"parent_id"`
	UserID      *int64    `json:"user_id,omitempty"`
	Name        string    `json:"name"`
	Birthdate   string    `json:"birthdate"`
	AvatarEmoji string    `json:"avatar_emoji"`
	CreatedAt   time.Time `json:"created_at"`
}

// Measurement is one height/foot-length data point for a kid. History is
// append-only so growth rate (and thus size prediction) can be computed.
type Measurement struct {
	ID           int64     `json:"id"`
	KidID        int64     `json:"kid_id"`
	MeasuredAt   string    `json:"measured_at"` // YYYY-MM-DD
	HeightCM     float64   `json:"height_cm"`
	FootLengthMM float64   `json:"foot_length_mm"`
	WeightKG     float64   `json:"weight_kg"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

// Category groups wardrobe items (outerwear, rain gear, shoes, ...). A row is
// per-user: defaults are seeded on first use and users can add their own.
// TargetQty is the number of this category each kid should have (e.g. the
// kindergarten's "2 sets of spare clothes" rule); 0 means no target and the
// category never appears in the needs list.
type Category struct {
	ID         int64     `json:"id"`
	ParentID   int64     `json:"parent_id"`
	Name       string    `json:"name"`
	Icon       string    `json:"icon"`
	SizeSystem string    `json:"size_system"` // clothing | shoe | none
	TargetQty  int       `json:"target_qty"`
	SortOrder  int       `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
}

// Item is one piece (or bundle, via Quantity) of clothing or gear.
type Item struct {
	ID         int64     `json:"id"`
	ParentID   int64     `json:"parent_id"`
	KidID      int64     `json:"kid_id"`
	CategoryID int64     `json:"category_id"`
	Name       string    `json:"name"`
	SizeLabel  string    `json:"size_label"`
	Quantity   int       `json:"quantity"`
	Condition  string    `json:"condition"` // new | good | worn
	Status     string    `json:"status"`    // active | too_small | stored
	Location   string    `json:"location"`  // home | kindergarten | school | cabin | other
	Season     string    `json:"season"`    // all | summer | winter
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// KidWithStats is a kid plus derived size guidance, returned by the kid list
// endpoint so the frontend never re-implements sizing math.
type KidWithStats struct {
	Kid
	LatestMeasurement    *Measurement `json:"latest_measurement,omitempty"`
	Clothing             *ClothingRec `json:"clothing,omitempty"`
	Shoe                 *ShoeRec     `json:"shoe,omitempty"`
	HeightRateCMPerMonth *float64     `json:"height_rate_cm_per_month,omitempty"`
	FootRateMMPerMonth   *float64     `json:"foot_rate_mm_per_month,omitempty"`
}

// NeedEntry is one gap in a kid's wardrobe: a category with a target quantity
// that active items don't cover.
type NeedEntry struct {
	CategoryID      int64  `json:"category_id"`
	CategoryName    string `json:"category_name"`
	CategoryIcon    string `json:"category_icon"`
	Have            int    `json:"have"`
	Target          int    `json:"target"`
	RecommendedSize string `json:"recommended_size"`
}

// TooSmallEntry is an item flagged as outgrown, paired with the size to buy
// as replacement.
type TooSmallEntry struct {
	Item            Item   `json:"item"`
	RecommendedSize string `json:"recommended_size"`
}

// Request bodies.

type KidRequest struct {
	Name        string `json:"name"`
	Birthdate   string `json:"birthdate"`
	AvatarEmoji string `json:"avatar_emoji"`
}

type MeasurementRequest struct {
	MeasuredAt   string  `json:"measured_at"`
	HeightCM     float64 `json:"height_cm"`
	FootLengthMM float64 `json:"foot_length_mm"`
	WeightKG     float64 `json:"weight_kg"`
	Note         string  `json:"note"`
}

type CategoryRequest struct {
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	SizeSystem string `json:"size_system"`
	TargetQty  int    `json:"target_qty"`
}

type ItemRequest struct {
	KidID      int64  `json:"kid_id"`
	CategoryID int64  `json:"category_id"`
	Name       string `json:"name"`
	SizeLabel  string `json:"size_label"`
	Quantity   int    `json:"quantity"`
	Condition  string `json:"condition"`
	Status     string `json:"status"`
	Location   string `json:"location"`
	Season     string `json:"season"`
	Notes      string `json:"notes"`
}
