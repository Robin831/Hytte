package budget

import (
	"database/sql"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// RegningItem represents one recurring expense in the monthly bill-split calculation.
type RegningItem struct {
	ID           int64     `json:"id"`
	Description  string    `json:"description"`
	Amount       float64   `json:"amount"`
	Monthly      float64   `json:"monthly"`
	SplitType    SplitType `json:"split_type"`
	SplitPct     *float64  `json:"split_pct"`
	YourShare    float64   `json:"your_share"`
	PartnerShare float64   `json:"partner_share"`
	NextDue      string    `json:"next_due"` // business-day-adjusted next due date (YYYY-MM-DD)
}

// RegningResponse is the full monthly bill-split result.
type RegningResponse struct {
	Expenses          []RegningItem `json:"expenses"`
	TotalYourShare    float64       `json:"total_your_share"`
	TotalPartnerShare float64       `json:"total_partner_share"`
	YourIncome        float64       `json:"your_income"`
	PartnerIncome     float64       `json:"partner_income"`
	YourRemaining     float64       `json:"your_remaining"`
	PartnerRemaining  float64       `json:"partner_remaining"`
	IncomeSplitPct    int           `json:"income_split_pct"`
	YourIncomeDay     int           `json:"your_income_day"`
	PartnerIncomeDay  int           `json:"partner_income_day"`
	YourIncomeDue     string        `json:"your_income_due"`  // business-day-adjusted next payday (YYYY-MM-DD)
	PartnerIncomeDue  string        `json:"partner_income_due"` // business-day-adjusted next payday (YYYY-MM-DD)
}

// RegningHandler computes the monthly recurring-expense split across both partners.
// For each active recurring transaction it applies split_type/split_pct, falling
// back to the global income_split_percentage preference when no explicit split is set.
// GET /api/budget/regning
func RegningHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		globalSplitPct, err := GetIncomeSplit(db, user.ID)
		if err != nil {
			log.Printf("budget: regning: get income split for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get income split"})
			return
		}

		partnerIncomeRaw, err := GetPartnerIncome(db, user.ID)
		if err != nil {
			log.Printf("budget: regning: get partner income for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get partner income"})
			return
		}
		partnerIncome := float64(partnerIncomeRaw)

		yourIncomeDay, err := GetIncomeDay(db, user.ID)
		if err != nil {
			log.Printf("budget: regning: get income day for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get income day"})
			return
		}
		partnerIncomeDayVal, err := GetPartnerIncomeDay(db, user.ID)
		if err != nil {
			log.Printf("budget: regning: get partner income day for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get partner income day"})
			return
		}

		// Fetch user's base monthly salary from salary_config (most recent entry).
		// Missing salary config is allowed and leaves yourIncome at 0.
		var yourIncome float64
		err = db.QueryRow(
			`SELECT base_salary FROM salary_config WHERE user_id = ? ORDER BY effective_from DESC, id DESC LIMIT 1`,
			user.ID,
		).Scan(&yourIncome)
		if err != nil {
			if err != sql.ErrNoRows {
				log.Printf("budget: regning: get salary config for user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get salary config"})
				return
			}
		}

		recurrings, err := listActiveRecurring(db, user.ID)
		if err != nil {
			log.Printf("budget: regning: list recurring for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list recurring transactions"})
			return
		}

		items := make([]RegningItem, 0, len(recurrings))
		var totalYour, totalPartner float64

		for _, rec := range recurrings {
			monthly := regningMonthly(rec.Amount, rec.Frequency)
			yourShare, partnerShare := regningComputeSplit(monthly, rec.SplitType, rec.SplitPct, globalSplitPct)
			var nextDue string
			if next, err := nextRecurringDueDate(rec); err == nil {
				nextDue = nextBusinessDay(next).Format("2006-01-02")
			}
			items = append(items, RegningItem{
				ID:           rec.ID,
				Description:  rec.Description,
				Amount:       rec.Amount,
				Monthly:      monthly,
				SplitType:    rec.SplitType,
				SplitPct:     rec.SplitPct,
				YourShare:    yourShare,
				PartnerShare: partnerShare,
				NextDue:      nextDue,
			})
			totalYour += yourShare
			totalPartner += partnerShare
		}

		now := time.Now()
		writeJSON(w, http.StatusOK, RegningResponse{
			Expenses:          items,
			TotalYourShare:    totalYour,
			TotalPartnerShare: totalPartner,
			YourIncome:        yourIncome,
			PartnerIncome:     partnerIncome,
			YourRemaining:     yourIncome - totalYour,
			PartnerRemaining:  partnerIncome - totalPartner,
			IncomeSplitPct:    globalSplitPct,
			YourIncomeDay:     yourIncomeDay,
			PartnerIncomeDay:  partnerIncomeDayVal,
			YourIncomeDue:     incomeNextDue(yourIncomeDay, now),
			PartnerIncomeDue:  incomeNextDue(partnerIncomeDayVal, now),
		})
	}
}

// incomeNextDue returns the next adjusted payday for the given day-of-month.
// Income dates move BACKWARDS when they fall on a weekend or Norwegian holiday
// (the opposite of expense due dates which move forwards). If this month's
// adjusted date is already in the past, the next month's date is returned.
func incomeNextDue(dayOfMonth int, now time.Time) string {
	year, month, _ := now.Date()
	loc := now.Location()
	today := time.Date(year, month, now.Day(), 0, 0, 0, 0, loc)

	for _, offset := range []int{0, 1} {
		m := time.Month(int(month) + offset)
		// Clamp day to last day of month: day 0 of the following month = last day of m.
		lastDay := time.Date(year, m+1, 0, 0, 0, 0, 0, loc).Day()
		d := dayOfMonth
		if d > lastDay {
			d = lastDay
		}
		raw := time.Date(year, m, d, 0, 0, 0, 0, loc)
		adjusted := previousBusinessDay(raw)
		if !adjusted.Before(today) {
			return adjusted.Format("2006-01-02")
		}
	}

	// Absolute fallback: return next month's adjusted date.
	m := time.Month(int(month) + 1)
	lastDay := time.Date(year, m+1, 0, 0, 0, 0, 0, loc).Day()
	d := dayOfMonth
	if d > lastDay {
		d = lastDay
	}
	return previousBusinessDay(time.Date(year, m, d, 0, 0, 0, 0, loc)).Format("2006-01-02")
}

// regningMonthly normalises a recurring amount to a per-month figure.
// Weekly amounts are rounded to the nearest øre (2 decimal places) to avoid
// float imprecision; yearly amounts are left as-is (exact division for most values).
func regningMonthly(amount float64, freq Frequency) float64 {
	switch freq {
	case FrequencyWeekly:
		return roundCents(amount * 52 / 12)
	case FrequencyQuarterly:
		return roundCents(amount / 3)
	case FrequencyYearly:
		return amount / 12
	default: // monthly
		return amount
	}
}

// roundCents rounds a monetary value to 2 decimal places.
func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// regningComputeSplit returns (yourShare, partnerShare) for a normalised monthly amount.
//
// Split rules:
//   - equal         — 50/50
//   - fixed_you     — split_pct NOK for you, remainder for partner (clamped to [0, monthly])
//   - fixed_partner — split_pct NOK for partner, remainder for you (clamped to [0, monthly])
//   - percentage    — split_pct % for you (or globalSplitPct if nil)
//
// Any fixed type with a nil split_pct falls back to the percentage logic.
// Shares are rounded to 2 decimal places to avoid float imprecision in JSON.
func regningComputeSplit(monthly float64, splitType SplitType, splitPct *float64, globalSplitPct int) (float64, float64) {
	switch splitType {
	case SplitTypeEqual:
		half := roundCents(monthly / 2)
		return half, half
	case SplitTypeFixedYou:
		if splitPct != nil {
			fixed := math.Max(0, math.Min(*splitPct, monthly))
			return roundCents(fixed), roundCents(monthly - fixed)
		}
	case SplitTypeFixedPartner:
		if splitPct != nil {
			fixed := math.Max(0, math.Min(*splitPct, monthly))
			return roundCents(monthly - fixed), roundCents(fixed)
		}
	}
	// SplitTypePercentage (default) or fixed types with nil split_pct.
	pct := float64(globalSplitPct) / 100
	if splitType == SplitTypePercentage && splitPct != nil {
		pct = *splitPct / 100
	}
	yourShare := roundCents(monthly * pct)
	return yourShare, roundCents(monthly - yourShare)
}
