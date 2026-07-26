package wardrobe

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Nordic/EU children's clothing sizes are the child's height in cm, in 6 cm
// steps. A garment labeled 104 fits a child up to ~104 cm tall.
var clothingSizes = []int{50, 56, 62, 68, 74, 80, 86, 92, 98, 104, 110, 116, 122, 128, 134, 140, 146, 152, 158, 164, 170}

const (
	// A garment is "almost outgrown" when the child is within this many cm of
	// the size's ceiling — recommend buying the next size up.
	clothingBuyMarginCM = 3.0

	// EU shoe sizing: internal last length in cm ≈ size / 1.5, so
	// size = (foot length + toe room) in cm × 1.5.
	// shoeFitRoomMM is the minimum toe room for a shoe to count as fitting;
	// shoeBuyRoomMM is the room to aim for when buying new (leaves space to
	// grow into without being unsafe to walk in).
	shoeFitRoomMM = 8.0
	shoeBuyRoomMM = 14.0

	// Growth-rate estimation needs at least this much time between the first
	// and last measurement to be meaningful.
	minGrowthSpanDays = 14
)

// ClothingRec is derived clothing-size guidance from a height measurement.
type ClothingRec struct {
	CurrentSize int `json:"current_size"` // fits now
	BuySize     int `json:"buy_size"`     // what to buy today
}

// ShoeRec is derived EU shoe-size guidance from a foot-length measurement.
type ShoeRec struct {
	CurrentEU int `json:"current_eu"` // fits now (minimum toe room)
	BuyEU     int `json:"buy_eu"`     // what to buy today (growing room)
}

// RecommendClothing maps a height in cm to the size that fits now and the
// size to buy. Returns nil for non-positive or absurd heights.
func RecommendClothing(heightCM float64) *ClothingRec {
	if heightCM <= 0 || heightCM > float64(clothingSizes[len(clothingSizes)-1]) {
		return nil
	}
	idx := sort.SearchInts(clothingSizes, int(math.Ceil(heightCM)))
	if idx >= len(clothingSizes) {
		return nil
	}
	rec := &ClothingRec{CurrentSize: clothingSizes[idx], BuySize: clothingSizes[idx]}
	if float64(clothingSizes[idx])-heightCM < clothingBuyMarginCM && idx+1 < len(clothingSizes) {
		rec.BuySize = clothingSizes[idx+1]
	}
	return rec
}

// RecommendShoe maps a foot length in mm to EU shoe sizes. Returns nil for
// non-positive or absurd lengths.
func RecommendShoe(footLengthMM float64) *ShoeRec {
	if footLengthMM <= 0 || footLengthMM > 300 {
		return nil
	}
	return &ShoeRec{
		CurrentEU: int(math.Ceil((footLengthMM + shoeFitRoomMM) * 0.15)),
		BuyEU:     int(math.Ceil((footLengthMM + shoeBuyRoomMM) * 0.15)),
	}
}

// growthRate returns the average change per 30 days between the earliest and
// latest positive values in the (measured_at-ascending) history, or nil when
// there are fewer than two usable points or they span less than
// minGrowthSpanDays.
func growthRate(history []Measurement, value func(Measurement) float64) *float64 {
	var first, last *Measurement
	for i := range history {
		if value(history[i]) <= 0 {
			continue
		}
		if first == nil {
			first = &history[i]
		}
		last = &history[i]
	}
	if first == nil || last == nil || first.ID == last.ID {
		return nil
	}
	t0, err0 := time.Parse("2006-01-02", first.MeasuredAt)
	t1, err1 := time.Parse("2006-01-02", last.MeasuredAt)
	if err0 != nil || err1 != nil {
		return nil
	}
	days := t1.Sub(t0).Hours() / 24
	if days < minGrowthSpanDays {
		return nil
	}
	rate := (value(*last) - value(*first)) / days * 30
	rate = math.Round(rate*100) / 100
	return &rate
}

// recommendedSizeLabel renders the buy-size for a category's size system from
// a kid's latest measurement, or "" when nothing can be derived.
func recommendedSizeLabel(sizeSystem string, m *Measurement) string {
	if m == nil {
		return ""
	}
	switch sizeSystem {
	case "clothing":
		if rec := RecommendClothing(m.HeightCM); rec != nil {
			return fmt.Sprintf("%d", rec.BuySize)
		}
	case "shoe":
		if rec := RecommendShoe(m.FootLengthMM); rec != nil {
			return fmt.Sprintf("EU %d", rec.BuyEU)
		}
	}
	return ""
}
