package salary

// norwegianTaxDefaults maps a year to the Norwegian income tax brackets for
// that year. Each entry combines the flat alminnelig inntektsskatt (22%) with
// the trinnskatt (step tax) into a single combined marginal rate per bracket.
// These are kept for the legacy custom-bracket API (GET/PUT /api/salary/tax-table).
//
// Sources: Stortinget's annual budget (statsbudsjettet) and Skatteetaten.
// Only years with distinct thresholds are listed; callers fall back to the
// nearest earlier year when the requested year is not present.
var norwegianTaxDefaults = map[int64][]struct {
	IncomeFrom float64
	IncomeTo   float64 // 0 = unbounded top bracket
	Rate       float64
}{
	// 2026 — alminnelig skatt 22% + trinnskatt 2026
	2026: {
		{0, 217400, 0.22},       // base rate only
		{217400, 306050, 0.237}, // + trinn 1 (1.7%)
		{306050, 697150, 0.260}, // + trinn 2 (4.0%)
		{697150, 942400, 0.356}, // + trinn 3 (13.6%)
		{942400, 0, 0.386},      // + trinn 4 (16.6%)
	},
	// 2025 — alminnelig skatt 22% + trinnskatt 2025
	2025: {
		{0, 208050, 0.22},
		{208050, 292850, 0.237},
		{292850, 670000, 0.260},
		{670000, 937900, 0.356},
		{937900, 0, 0.386},
	},
}

// DefaultTaxBrackets returns the Norwegian income tax brackets for the given
// year. If no exact match is found, the closest earlier year is used. The
// returned brackets already have UserID and Year set to the supplied values.
func DefaultTaxBrackets(userID, year int64) []TaxBracket {
	// Find the best matching year (exact match, or latest earlier year).
	best := int64(0)
	for y := range norwegianTaxDefaults {
		if y <= year && y > best {
			best = y
		}
	}
	if best == 0 {
		// All known years are newer — use the oldest available as a fallback.
		for y := range norwegianTaxDefaults {
			if best == 0 || y < best {
				best = y
			}
		}
	}

	src := norwegianTaxDefaults[best]
	brackets := make([]TaxBracket, len(src))
	for i, b := range src {
		brackets[i] = TaxBracket{
			UserID:     userID,
			Year:       year,
			IncomeFrom: b.IncomeFrom,
			IncomeTo:   b.IncomeTo,
			Rate:       b.Rate,
		}
	}
	return brackets
}

// trekktabellDefaults maps a year to the Norwegian trekktabell parameters.
// These are the annual tax parameters used to compute the correct monthly tax
// withholding via the standard Norwegian formula (minstefradrag, personfradrag,
// alminnelig skatt, trinnskatt, and trygdeavgift).
//
// Sources: Stortinget's annual budget (statsbudsjettet) and Skatteetaten.
var trekktabellDefaults = map[int64]TrekktabellParams{
	// 2026 — parameters from Skatteetaten trekktabell 2026
	2026: {
		MinstefradragRate:   0.46,
		MinstefradragMin:    31800,
		MinstefradragMax:    104450,
		Personfradrag:       108550,
		AlminneligSkattRate: 0.22,
		Trygdeavgift:        0.079,
		TrinnskattTiers: []TrinnskattTier{
			{IncomeFrom: 217400, Rate: 0.017}, // trinn 1: 1.7%
			{IncomeFrom: 306050, Rate: 0.040}, // trinn 2: 4.0%
			{IncomeFrom: 697150, Rate: 0.136}, // trinn 3: 13.6%
			{IncomeFrom: 942400, Rate: 0.166}, // trinn 4: 16.6%
		},
	},
	// 2025 — parameters from Skatteetaten trekktabell 2025
	2025: {
		MinstefradragRate:   0.46,
		MinstefradragMin:    31800,
		MinstefradragMax:    104450,
		Personfradrag:       109950,
		AlminneligSkattRate: 0.22,
		Trygdeavgift:        0.078,
		TrinnskattTiers: []TrinnskattTier{
			{IncomeFrom: 208050, Rate: 0.017},
			{IncomeFrom: 292850, Rate: 0.040},
			{IncomeFrom: 670000, Rate: 0.136},
			{IncomeFrom: 937900, Rate: 0.166},
		},
	},
}

// DefaultTrekktabellParams returns the trekktabell parameters for the given
// year. If no exact match is found, the closest earlier year is used. The
// returned params have UserID and Year set to the supplied values.
func DefaultTrekktabellParams(userID, year int64) TrekktabellParams {
	best := int64(0)
	for y := range trekktabellDefaults {
		if y <= year && y > best {
			best = y
		}
	}
	if best == 0 {
		for y := range trekktabellDefaults {
			if best == 0 || y < best {
				best = y
			}
		}
	}

	p := trekktabellDefaults[best]
	// Copy trinnskatt tiers so caller may not mutate the map value.
	tiers := make([]TrinnskattTier, len(p.TrinnskattTiers))
	copy(tiers, p.TrinnskattTiers)
	return TrekktabellParams{
		UserID:              userID,
		Year:                year,
		MinstefradragRate:   p.MinstefradragRate,
		MinstefradragMin:    p.MinstefradragMin,
		MinstefradragMax:    p.MinstefradragMax,
		Personfradrag:       p.Personfradrag,
		AlminneligSkattRate: p.AlminneligSkattRate,
		Trygdeavgift:        p.Trygdeavgift,
		TrinnskattTiers:     tiers,
	}
}

// CalculateTrekktabellTax computes the monthly tax withholding for the given
// monthly gross income using the Norwegian standard trekktabell formula:
//
//  1. Annualise the monthly gross (× 12).
//  2. Deduct minstefradrag (standard deduction): rate × gross, clamped to
//     [MinstefradragMin, MinstefradragMax].
//  3. Compute skatt på alminnelig inntekt: max(0, gross - minstefradrag -
//     personfradrag) × AlminneligSkattRate.
//  4. Add trinnskatt (step tax) on annual gross via progressive tiers.
//  5. Add trygdeavgift (social security): annual gross × Trygdeavgift.
//  6. Divide the total annual tax by 12 for the monthly withholding amount.
//
// This matches the output of the official Skatteetaten trekktabeller.
func CalculateTrekktabellTax(monthlyGross float64, params TrekktabellParams) float64 {
	if monthlyGross <= 0 {
		return 0
	}
	annualGross := monthlyGross * 12

	// Minstefradrag (minimum standard deduction on wage income).
	minstefradrag := annualGross * params.MinstefradragRate
	if minstefradrag < params.MinstefradragMin {
		minstefradrag = params.MinstefradragMin
	}
	if params.MinstefradragMax > 0 && minstefradrag > params.MinstefradragMax {
		minstefradrag = params.MinstefradragMax
	}

	// Skatt på alminnelig inntekt (22% on income net of standard deduction and
	// personal allowance).
	alminneligInntekt := annualGross - minstefradrag
	taxableAlminnelig := alminneligInntekt - params.Personfradrag
	if taxableAlminnelig < 0 {
		taxableAlminnelig = 0
	}
	alminneligSkatt := taxableAlminnelig * params.AlminneligSkattRate

	// Trinnskatt (step tax applied to annual gross; each tier's rate is marginal
	// on the income band between consecutive IncomeFrom values).
	trinnskatt := 0.0
	for i, tier := range params.TrinnskattTiers {
		if annualGross <= tier.IncomeFrom {
			break
		}
		// Upper bound for this tier is the next tier's threshold, or annualGross
		// for the top (unbounded) tier.
		high := annualGross
		if i+1 < len(params.TrinnskattTiers) {
			high = params.TrinnskattTiers[i+1].IncomeFrom
		}
		if annualGross < high {
			high = annualGross
		}
		trinnskatt += (high - tier.IncomeFrom) * tier.Rate
	}

	// Trygdeavgift (social security / national insurance contribution).
	trygdeavgift := annualGross * params.Trygdeavgift

	totalAnnualTax := alminneligSkatt + trinnskatt + trygdeavgift
	return totalAnnualTax / 12
}
