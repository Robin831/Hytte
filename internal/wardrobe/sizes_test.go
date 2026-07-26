package wardrobe

import "testing"

func TestRecommendClothing(t *testing.T) {
	tests := []struct {
		height  float64
		current int
		buy     int
	}{
		{98, 98, 104},   // exactly at size ceiling — buy next
		{99, 104, 104},  // just past a size
		{100, 104, 104}, // comfortably inside 104
		{102, 104, 110}, // within 3 cm of ceiling — buy next
		{51, 56, 56},    // newborn range
		{170, 170, 170}, // top size has no next
	}
	for _, tt := range tests {
		rec := RecommendClothing(tt.height)
		if rec == nil {
			t.Fatalf("RecommendClothing(%v) = nil", tt.height)
		}
		if rec.CurrentSize != tt.current || rec.BuySize != tt.buy {
			t.Errorf("RecommendClothing(%v) = {%d, %d}, want {%d, %d}",
				tt.height, rec.CurrentSize, rec.BuySize, tt.current, tt.buy)
		}
	}

	for _, invalid := range []float64{0, -5, 200} {
		if rec := RecommendClothing(invalid); rec != nil {
			t.Errorf("RecommendClothing(%v) = %+v, want nil", invalid, rec)
		}
	}
}

func TestRecommendShoe(t *testing.T) {
	// foot 160 mm: fits-now = ceil((160+8)*0.15) = ceil(25.2) = 26,
	// buy = ceil((160+14)*0.15) = ceil(26.1) = 27.
	rec := RecommendShoe(160)
	if rec == nil {
		t.Fatal("RecommendShoe(160) = nil")
	}
	if rec.CurrentEU != 26 || rec.BuyEU != 27 {
		t.Errorf("RecommendShoe(160) = {%d, %d}, want {26, 27}", rec.CurrentEU, rec.BuyEU)
	}

	for _, invalid := range []float64{0, -1, 400} {
		if rec := RecommendShoe(invalid); rec != nil {
			t.Errorf("RecommendShoe(%v) = %+v, want nil", invalid, rec)
		}
	}
}

func TestGrowthRate(t *testing.T) {
	height := func(m Measurement) float64 { return m.HeightCM }

	// 3 cm over 90 days = 1 cm / 30 days.
	history := []Measurement{
		{ID: 1, MeasuredAt: "2026-01-01", HeightCM: 100},
		{ID: 2, MeasuredAt: "2026-02-15", HeightCM: 101.5},
		{ID: 3, MeasuredAt: "2026-04-01", HeightCM: 103},
	}
	rate := growthRate(history, height)
	if rate == nil {
		t.Fatal("growthRate = nil, want value")
	}
	if *rate != 1.0 {
		t.Errorf("growthRate = %v, want 1.0", *rate)
	}

	// Single point — no rate.
	if rate := growthRate(history[:1], height); rate != nil {
		t.Errorf("growthRate(single point) = %v, want nil", *rate)
	}

	// Zero heights are skipped: only one usable point remains.
	sparse := []Measurement{
		{ID: 1, MeasuredAt: "2026-01-01", HeightCM: 0},
		{ID: 2, MeasuredAt: "2026-04-01", HeightCM: 103},
	}
	if rate := growthRate(sparse, height); rate != nil {
		t.Errorf("growthRate(sparse) = %v, want nil", *rate)
	}

	// Span below minGrowthSpanDays — no rate.
	short := []Measurement{
		{ID: 1, MeasuredAt: "2026-01-01", HeightCM: 100},
		{ID: 2, MeasuredAt: "2026-01-10", HeightCM: 101},
	}
	if rate := growthRate(short, height); rate != nil {
		t.Errorf("growthRate(short span) = %v, want nil", *rate)
	}
}

func TestRecommendedSizeLabel(t *testing.T) {
	m := &Measurement{HeightCM: 100, FootLengthMM: 160}
	if got := recommendedSizeLabel("clothing", m); got != "104" {
		t.Errorf("clothing label = %q, want %q", got, "104")
	}
	if got := recommendedSizeLabel("shoe", m); got != "EU 27" {
		t.Errorf("shoe label = %q, want %q", got, "EU 27")
	}
	if got := recommendedSizeLabel("none", m); got != "" {
		t.Errorf("none label = %q, want empty", got)
	}
	if got := recommendedSizeLabel("clothing", nil); got != "" {
		t.Errorf("nil measurement label = %q, want empty", got)
	}
}
