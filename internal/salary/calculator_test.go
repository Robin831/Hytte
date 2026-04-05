package salary

import (
	"math"
	"testing"
)

// round rounds a float64 to 2 decimal places for comparison.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

var defaultTiers = []CommissionTier{
	{Floor: 0, Ceiling: 60000, Rate: 0},
	{Floor: 60000, Ceiling: 80000, Rate: 0.20},
	{Floor: 80000, Ceiling: 100000, Rate: 0.40},
	{Floor: 100000, Ceiling: 0, Rate: 0.50},
}

func TestCalculateCommission(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   float64
	}{
		{
			name:   "zero revenue",
			amount: 0,
			want:   0,
		},
		{
			name:   "below first tier threshold",
			amount: 30000,
			want:   0, // 0-60k at 0%
		},
		{
			name:   "exactly at first tier ceiling",
			amount: 60000,
			want:   0,
		},
		{
			name:   "in second tier (75k)",
			amount: 75000,
			// (75000-60000)*0.20 = 3000
			want: 3000,
		},
		{
			name:   "exactly at second tier ceiling (80k)",
			amount: 80000,
			// (80000-60000)*0.20 = 4000
			want: 4000,
		},
		{
			name:   "in third tier (90k)",
			amount: 90000,
			// (80000-60000)*0.20 + (90000-80000)*0.40 = 4000 + 4000 = 8000
			want: 8000,
		},
		{
			name:   "exactly at third tier ceiling (100k)",
			amount: 100000,
			// (80000-60000)*0.20 + (100000-80000)*0.40 = 4000 + 8000 = 12000
			want: 12000,
		},
		{
			name:   "in top tier (120k)",
			amount: 120000,
			// 4000 + 8000 + (120000-100000)*0.50 = 4000 + 8000 + 10000 = 22000
			want: 22000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateCommission(tc.amount, defaultTiers)
			if round2(got) != tc.want {
				t.Errorf("CalculateCommission(%v) = %v, want %v", tc.amount, got, tc.want)
			}
		})
	}
}

func TestCalculateTax(t *testing.T) {
	// Simplified Norwegian-style progressive brackets for testing.
	brackets := []TaxBracket{
		{IncomeFrom: 0, IncomeTo: 200000, Rate: 0.22},
		{IncomeFrom: 200000, IncomeTo: 500000, Rate: 0.32},
		{IncomeFrom: 500000, IncomeTo: 0, Rate: 0.40},
	}

	tests := []struct {
		name  string
		gross float64
		want  float64
	}{
		{
			name:  "zero income",
			gross: 0,
			want:  0,
		},
		{
			name:  "within first bracket (100k)",
			gross: 100000,
			want:  22000, // 100000 * 0.22
		},
		{
			name:  "exactly at first bracket ceiling (200k)",
			gross: 200000,
			want:  44000, // 200000 * 0.22
		},
		{
			name:  "in second bracket (300k)",
			gross: 300000,
			// 200000*0.22 + (300000-200000)*0.32 = 44000 + 32000 = 76000
			want: 76000,
		},
		{
			name:  "in top bracket (600k)",
			gross: 600000,
			// 200000*0.22 + 300000*0.32 + 100000*0.40 = 44000 + 96000 + 40000 = 180000
			want: 180000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateTax(tc.gross, brackets)
			if round2(got) != tc.want {
				t.Errorf("calculateTax(%v) = %v, want %v", tc.gross, got, tc.want)
			}
		})
	}
}

func TestAbsenceDayCost(t *testing.T) {
	cfg := Config{BaseSalary: 50000}

	tests := []struct {
		name        string
		workingDays int
		absenceDays int
		want        float64
	}{
		{
			name:        "no absence",
			workingDays: 22,
			absenceDays: 0,
			want:        0,
		},
		{
			name:        "one sick day",
			workingDays: 22,
			absenceDays: 1,
			// 50000/22 * 1 ≈ 2272.73
			want: round2(50000.0 / 22.0),
		},
		{
			name:        "five vacation days",
			workingDays: 22,
			absenceDays: 5,
			// 50000/22 * 5 ≈ 11363.64
			want: round2(50000.0 / 22.0 * 5),
		},
		{
			name:        "zero working days returns zero",
			workingDays: 0,
			absenceDays: 3,
			want:        0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := round2(AbsenceDayCost(cfg, tc.workingDays, tc.absenceDays))
			if got != tc.want {
				t.Errorf("AbsenceDayCost(workingDays=%d, absenceDays=%d) = %v, want %v",
					tc.workingDays, tc.absenceDays, got, tc.want)
			}
		})
	}
}

func TestScaleTiersForAbsence(t *testing.T) {
	tiers := []CommissionTier{
		{Floor: 0, Ceiling: 60000, Rate: 0},
		{Floor: 60000, Ceiling: 80000, Rate: 0.20},
		{Floor: 80000, Ceiling: 100000, Rate: 0.40},
		{Floor: 100000, Ceiling: 0, Rate: 0.50},
	}

	t.Run("no absence returns copy unchanged", func(t *testing.T) {
		scaled := ScaleTiersForAbsence(tiers, 20, 0)
		for i, got := range scaled {
			if got.Floor != tiers[i].Floor || got.Ceiling != tiers[i].Ceiling {
				t.Errorf("tier %d: got floor=%.0f ceiling=%.0f, want floor=%.0f ceiling=%.0f",
					i, got.Floor, got.Ceiling, tiers[i].Floor, tiers[i].Ceiling)
			}
		}
	})

	t.Run("5 absence days out of 20 scales by 0.75", func(t *testing.T) {
		scaled := ScaleTiersForAbsence(tiers, 20, 5)
		// ratio = 15/20 = 0.75
		expected := []struct{ floor, ceiling float64 }{
			{0, 45000},
			{45000, 60000},
			{60000, 75000},
			{75000, 0}, // unbounded stays unbounded
		}
		for i, got := range scaled {
			if round2(got.Floor) != expected[i].floor {
				t.Errorf("tier %d floor: got %.2f, want %.2f", i, got.Floor, expected[i].floor)
			}
			if round2(got.Ceiling) != expected[i].ceiling {
				t.Errorf("tier %d ceiling: got %.2f, want %.2f", i, got.Ceiling, expected[i].ceiling)
			}
		}
	})

	t.Run("unbounded top tier ceiling stays 0", func(t *testing.T) {
		scaled := ScaleTiersForAbsence(tiers, 20, 10)
		if scaled[3].Ceiling != 0 {
			t.Errorf("unbounded tier ceiling: got %.0f, want 0", scaled[3].Ceiling)
		}
	})

	t.Run("zero working days returns copy unchanged", func(t *testing.T) {
		scaled := ScaleTiersForAbsence(tiers, 0, 5)
		for i, got := range scaled {
			if got.Floor != tiers[i].Floor || got.Ceiling != tiers[i].Ceiling {
				t.Errorf("tier %d: changed unexpectedly", i)
			}
		}
	})

	t.Run("absence equals working days returns copy unchanged (no unbounded non-top tiers)", func(t *testing.T) {
		// ratio would be 0 — scaling would set all bounded ceilings to 0, which
		// CalculateCommission treats as unbounded. Guard must return original tiers.
		scaled := ScaleTiersForAbsence(tiers, 20, 20)
		for i, got := range scaled {
			if got.Floor != tiers[i].Floor || got.Ceiling != tiers[i].Ceiling {
				t.Errorf("tier %d: got floor=%.0f ceiling=%.0f, want floor=%.0f ceiling=%.0f",
					i, got.Floor, got.Ceiling, tiers[i].Floor, tiers[i].Ceiling)
			}
		}
		// Verify commission is not inflated — non-top tiers must not have Ceiling==0.
		for i, got := range scaled[:len(scaled)-1] {
			if got.Ceiling == 0 {
				t.Errorf("tier %d (non-top): Ceiling==0 would be treated as unbounded", i)
			}
		}
	})

	t.Run("absence exceeds working days returns copy unchanged", func(t *testing.T) {
		// absenceDays > workingDays: effectiveDays would be negative, clamped to 0.
		scaled := ScaleTiersForAbsence(tiers, 20, 25)
		for i, got := range scaled {
			if got.Floor != tiers[i].Floor || got.Ceiling != tiers[i].Ceiling {
				t.Errorf("tier %d: changed unexpectedly", i)
			}
		}
	})

	t.Run("commission calculation uses scaled tiers", func(t *testing.T) {
		// 20 working days, 5 absence days → ratio 0.75
		// Adjusted tiers: [0,45000@0%], [45000,60000@20%], [60000,75000@40%], [75000+@50%]
		// Revenue = 67500 (which is 90000 * 0.75 — a "full" month at 90k scaled down)
		scaled := ScaleTiersForAbsence(tiers, 20, 5)
		commission := CalculateCommission(67500, scaled)
		// Expected: (60000-45000)*0.20 + (67500-60000)*0.40 = 3000 + 3000 = 6000
		// This equals CalculateCommission(90000, tiers) = 8000 * 0.75 = 6000
		want := CalculateCommission(90000, tiers) * 0.75
		if round2(commission) != round2(want) {
			t.Errorf("commission = %.2f, want %.2f", commission, want)
		}
	})
}

func TestEstimateMonth(t *testing.T) {
	cfg := Config{
		UserID:        1,
		BaseSalary:    60000,
		StandardHours: 7.5,
	}
	brackets := []TaxBracket{
		{IncomeFrom: 0, IncomeTo: 0, Rate: 0.30}, // flat 30% for simplicity
	}

	t.Run("full month worked no commission", func(t *testing.T) {
		workingDays := 22
		hoursWorked := float64(workingDays) * cfg.StandardHours // 165h = full month

		rec := EstimateMonth(cfg, defaultTiers, brackets, hoursWorked, 0, 0, workingDays, 0, 0)

		if round2(rec.BaseAmount) != 60000 {
			t.Errorf("BaseAmount = %v, want 60000", rec.BaseAmount)
		}
		if rec.Commission != 0 {
			t.Errorf("Commission = %v, want 0", rec.Commission)
		}
		if round2(rec.Gross) != 60000 {
			t.Errorf("Gross = %v, want 60000", rec.Gross)
		}
		// Tax = 60000 * 0.30 = 18000
		if round2(rec.Tax) != 18000 {
			t.Errorf("Tax = %v, want 18000", rec.Tax)
		}
		if round2(rec.Net) != 42000 {
			t.Errorf("Net = %v, want 42000", rec.Net)
		}
		if !rec.IsEstimate {
			t.Error("IsEstimate should be true")
		}
	})

	t.Run("half month worked with commission", func(t *testing.T) {
		workingDays := 22
		hoursWorked := float64(workingDays) * cfg.StandardHours / 2 // 82.5h = half month

		rec := EstimateMonth(cfg, defaultTiers, brackets, hoursWorked, 75000, 0, workingDays, 0, 0)

		// Base = 60000 * 0.5 = 30000
		if round2(rec.BaseAmount) != 30000 {
			t.Errorf("BaseAmount = %v, want 30000", rec.BaseAmount)
		}
		// Commission on 75k = 3000
		if round2(rec.Commission) != 3000 {
			t.Errorf("Commission = %v, want 3000", rec.Commission)
		}
		// Gross = 30000 + 3000 = 33000
		if round2(rec.Gross) != 33000 {
			t.Errorf("Gross = %v, want 33000", rec.Gross)
		}
		if rec.IsEstimate != true {
			t.Error("IsEstimate should be true")
		}
	})

	t.Run("zero working days returns zero amounts", func(t *testing.T) {
		rec := EstimateMonth(cfg, defaultTiers, brackets, 0, 0, 0, 0, 0, 0)
		if rec.BaseAmount != 0 {
			t.Errorf("BaseAmount = %v, want 0", rec.BaseAmount)
		}
	})

	t.Run("non-zero internal revenue adds to commission base", func(t *testing.T) {
		workingDays := 22
		hoursWorked := float64(workingDays) * cfg.StandardHours // full month

		// billableRevenue = 50000, internalRevenue = 15000, total = 65000
		// Commission on 65000 = (65000-60000)*0.20 = 1000
		rec := EstimateMonth(cfg, defaultTiers, brackets, hoursWorked, 50000, 15000, workingDays, 0, 0)

		if round2(rec.BaseAmount) != 60000 {
			t.Errorf("BaseAmount = %v, want 60000", rec.BaseAmount)
		}
		if round2(rec.Commission) != 1000 {
			t.Errorf("Commission = %v, want 1000 (combined 65k revenue)", rec.Commission)
		}
		if round2(rec.Gross) != 61000 {
			t.Errorf("Gross = %v, want 61000", rec.Gross)
		}
	})

	t.Run("vacation and sick days are recorded", func(t *testing.T) {
		workingDays := 22
		hoursWorked := float64(workingDays) * cfg.StandardHours
		rec := EstimateMonth(cfg, defaultTiers, brackets, hoursWorked, 0, 0, workingDays, 5, 2)

		if rec.VacationDays != 5 {
			t.Errorf("VacationDays = %d, want 5", rec.VacationDays)
		}
		if rec.SickDays != 2 {
			t.Errorf("SickDays = %d, want 2", rec.SickDays)
		}
	})

	t.Run("absence scales commission tiers proportionally", func(t *testing.T) {
		// 22 working days, 5 vacation + 2 sick = 7 absence days → 15/22 ratio
		workingDays := 22
		absenceDays := 7
		effectiveDays := workingDays - absenceDays
		ratio := float64(effectiveDays) / float64(workingDays)

		// Revenue = 75000 * ratio (employee billed proportionally to days worked)
		revenue := 75000 * ratio
		rec := EstimateMonth(cfg, defaultTiers, brackets, float64(workingDays)*cfg.StandardHours*ratio,
			revenue, workingDays, 5, 2)

		// Expected commission: scaled tiers at revenue*ratio
		// Adjusted first tier threshold = 60000 * ratio ≈ 40909
		// Since revenue (≈51136) > 40909, commission applies
		scaledTiers := ScaleTiersForAbsence(defaultTiers, workingDays, absenceDays)
		wantCommission := CalculateCommission(revenue, scaledTiers)
		if round2(rec.Commission) != round2(wantCommission) {
			t.Errorf("Commission = %.2f, want %.2f", rec.Commission, wantCommission)
		}
	})
}
