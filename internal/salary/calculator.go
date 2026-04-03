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
// It prorate the monthly base salary by the number of working days.
func AbsenceDayCost(config Config, workingDays int, absenceDays int) float64 {
	if workingDays <= 0 || absenceDays <= 0 {
		return 0
	}
	dailyRate := config.BaseSalary / float64(workingDays)
	return dailyRate * float64(absenceDays)
}

// EstimateMonth produces a salary record estimate for the given month.
//
//   - hoursWorked is the total tracked hours for the month (pulled from work_days).
//   - billableRevenue is the total revenue attributed to billable work, used for
//     commission calculation.
//   - workingDays is the number of scheduled working days in the month.
//   - vacationDays / sickDays are absence counts for the month.
//
// The returned Record has IsEstimate = true and is not persisted by this function.
func EstimateMonth(
	config Config,
	tiers []CommissionTier,
	brackets []TaxBracket,
	hoursWorked float64,
	billableRevenue float64,
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

	commission := CalculateCommission(billableRevenue, tiers)
	gross := baseAmount + commission
	tax := calculateTax(gross, brackets)
	net := gross - tax

	return Record{
		UserID:        config.UserID,
		WorkingDays:   workingDays,
		HoursWorked:   hoursWorked,
		BillableHours: hoursWorked,
		BaseAmount:    baseAmount,
		Commission:    commission,
		Gross:         gross,
		Tax:           tax,
		Net:           net,
		VacationDays:  vacationDays,
		SickDays:      sickDays,
		IsEstimate:    true,
	}
}
