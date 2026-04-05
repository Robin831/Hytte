package salary

// CalculateCommission applies the progressive tier commission structure to the
// given revenue amount. For each tier where amount > floor, the commission is
// (min(amount, ceiling) - floor) * rate. A tier with ceiling == 0 is unbounded.
func CalculateCommission(amount float64, tiers []CommissionTier) float64 {
	commission := 0.0
	for _, t := range tiers {
		if amount <= t.Floor {
			continue
		}
		high := t.Ceiling
		if high == 0 {
			// Unbounded top tier — cap at the actual amount.
			high = amount
		}
		if amount < high {
			high = amount
		}
		commission += (high - t.Floor) * t.Rate
	}
	return commission
}

// calculateTax applies the progressive bracket tax structure to gross income.
// A bracket with income_to == 0 is unbounded (top bracket).
func calculateTax(gross float64, brackets []TaxBracket) float64 {
	tax := 0.0
	for _, b := range brackets {
		if gross <= b.IncomeFrom {
			continue
		}
		high := b.IncomeTo
		if high == 0 {
			// Unbounded top bracket — cap at the actual gross.
			high = gross
		}
		if gross < high {
			high = gross
		}
		tax += (high - b.IncomeFrom) * b.Rate
	}
	return tax
}

// AbsenceDayCost returns the salary cost of the given number of absence days.
// It prorates the monthly base salary by the number of working days.
func AbsenceDayCost(config Config, workingDays int, absenceDays int) float64 {
	if workingDays <= 0 || absenceDays <= 0 {
		return 0
	}
	dailyRate := config.BaseSalary / float64(workingDays)
	return dailyRate * float64(absenceDays)
}

// ScaleTiersForAbsence returns tiers with all boundaries scaled proportionally
// by the ratio of effective working days to total working days. This ensures
// employees are not penalized when absence (vacation or sick leave) reduces
// their working period — the commission thresholds shrink by the same ratio as
// the number of days actually worked.
//
// If workingDays <= 0 or absenceDays <= 0, the original tiers are returned
// unchanged. The returned slice is always a copy; callers may modify it freely.
func ScaleTiersForAbsence(tiers []CommissionTier, workingDays, absenceDays int) []CommissionTier {
	if workingDays <= 0 || absenceDays <= 0 {
		// Return a copy for consistency.
		out := make([]CommissionTier, len(tiers))
		copy(out, tiers)
		return out
	}
	effectiveDays := workingDays - absenceDays
	if effectiveDays <= 0 {
		// No effective working days — return a copy unchanged. Scaling by ratio 0
		// would set all bounded ceilings to 0, which CalculateCommission treats as
		// unbounded, causing incorrect commission calculation.
		out := make([]CommissionTier, len(tiers))
		copy(out, tiers)
		return out
	}
	ratio := float64(effectiveDays) / float64(workingDays)
	scaled := make([]CommissionTier, len(tiers))
	for i, t := range tiers {
		scaled[i] = t
		scaled[i].Floor = t.Floor * ratio
		if t.Ceiling != 0 {
			scaled[i].Ceiling = t.Ceiling * ratio
		}
	}
	return scaled
}

// EstimateMonth produces a salary record estimate for the given month.
//
//   - hoursWorked is the total tracked hours for the month (pulled from work_days).
//   - billableRevenue is the revenue from client-facing billable work.
//   - internalRevenue is the revenue from internal company hours (meetings/admin),
//     billed at the internal_hourly_rate. Both revenues are summed for commission.
//   - workingDays is the number of scheduled working days in the month.
//   - vacationDays / sickDays are absence counts for the month. When non-zero,
//     commission tier boundaries are scaled proportionally so the employee is not
//     penalised for the reduced working period.
//   - taxParams holds the Norwegian trekktabell parameters used to compute the
//     monthly tax withholding via CalculateTrekktabellTax.
//
// The returned Record has IsEstimate = true and is not persisted by this function.
func EstimateMonth(
	config Config,
	tiers []CommissionTier,
	taxParams TrekktabellParams,
	hoursWorked float64,
	billableRevenue float64,
	internalRevenue float64,
	workingDays int,
	vacationDays int,
	sickDays int,
) Record {
	// Prorate base salary by actual hours vs expected hours.
	expectedHours := float64(workingDays) * config.StandardHours
	baseAmount := 0.0
	if expectedHours > 0 {
		baseAmount = config.BaseSalary * (hoursWorked / expectedHours)
	}

	// Scale tier boundaries for absence so thresholds shrink proportionally.
	absenceDays := vacationDays + sickDays
	effectiveTiers := ScaleTiersForAbsence(tiers, workingDays, absenceDays)
	// Commission is calculated on the combined billable + internal revenue.
	totalCommissionRevenue := billableRevenue + internalRevenue
	commission := CalculateCommission(totalCommissionRevenue, effectiveTiers)

	// Sick day addon: for each sick day, add the average daily commission based
	// on the full month's standard revenue (working_days × standard_hours × hourly_rate)
	// against unscaled tiers. This ensures employees receive average commission for
	// sick days rather than zero.
	var sickAddon float64
	if sickDays > 0 && workingDays > 0 {
		fullMonthRevenue := float64(workingDays) * config.StandardHours * config.HourlyRate
		fullMonthCommission := CalculateCommission(fullMonthRevenue, tiers)
		sickAddon = (fullMonthCommission / float64(workingDays)) * float64(sickDays)
	}

	gross := baseAmount + commission + sickAddon + config.TaxableBenefits
	tax := CalculateTrekktabellTax(gross, taxParams)
	net := gross - tax

	return Record{
		UserID:       config.UserID,
		WorkingDays:  int64(workingDays),
		HoursWorked:  hoursWorked,
		BaseAmount:   baseAmount,
		Commission:   commission + sickAddon,
		Gross:        gross,
		Tax:          tax,
		Net:          net,
		VacationDays: int64(vacationDays),
		SickDays:     int64(sickDays),
		IsEstimate:   true,
	}
}
