package salary

// norwegianTaxDefaults maps a year to the Norwegian income tax brackets for
// that year. Each entry combines the flat alminnelig inntektsskatt (22%) with
// the trinnskatt (step tax) into a single combined marginal rate per bracket.
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
