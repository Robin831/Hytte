package budget

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// CategoryTrend holds spending for a single category within a month trend period.
type CategoryTrend struct {
	CategoryID   int64   `json:"category_id"`
	CategoryName string  `json:"category_name"`
	Color        string  `json:"color"`
	IsIncome     bool    `json:"is_income"`
	Amount       float64 `json:"amount"`
}

// MonthlyTrend holds aggregated income/expenses for a single month.
type MonthlyTrend struct {
	Month      string          `json:"month"`
	Income     float64         `json:"income"`
	Expenses   float64         `json:"expenses"`
	Net        float64         `json:"net"`
	ByCategory []CategoryTrend `json:"by_category"`
}

// NetWorthPoint is a (month, value) pair used for the net worth line chart.
type NetWorthPoint struct {
	Month string  `json:"month"`
	Value float64 `json:"value"`
}

// YoYMonth holds current vs previous year expenses for a given calendar month.
type YoYMonth struct {
	Month    int     `json:"month"`
	Current  float64 `json:"current"`
	Previous float64 `json:"previous"`
}

// YearOverYear bundles year-over-year spending data.
type YearOverYear struct {
	CurrentYear  int        `json:"current_year"`
	PreviousYear int        `json:"previous_year"`
	Monthly      []YoYMonth `json:"monthly"`
}

// TrendsResponse is the full response body for TrendsHandler.
type TrendsResponse struct {
	Months       []MonthlyTrend `json:"months"`
	NetWorth     []NetWorthPoint `json:"net_worth"`
	YearOverYear *YearOverYear  `json:"year_over_year"`
}

// GetTrends queries transaction data and returns aggregated trend data for the
// given number of past months (including the current month).
func GetTrends(db *sql.DB, userID int64, months int) (*TrendsResponse, error) {
	if months < 1 {
		months = 6
	}
	if months > 36 {
		months = 36
	}

	now := time.Now()

	// Build the list of months we want to report on (oldest first).
	type monthSlot struct {
		year  int
		month time.Month
		label string // YYYY-MM
	}
	slots := make([]monthSlot, months)
	for i := 0; i < months; i++ {
		t := now.AddDate(0, -(months-1-i), 0)
		slots[i] = monthSlot{
			year:  t.Year(),
			month: t.Month(),
			label: fmt.Sprintf("%d-%02d", t.Year(), int(t.Month())),
		}
	}

	startDate := fmt.Sprintf("%d-%02d-01", slots[0].year, int(slots[0].month))
	endDate := monthLastDay(slots[months-1].year, int(slots[months-1].month))

	// Load categories for name/color lookup.
	cats, err := ListCategories(db, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	catMap := make(map[int64]Category, len(cats))
	for _, c := range cats {
		catMap[c.ID] = c
	}

	// Load all transactions in the period.
	txns, err := ListTransactions(db, userID, TransactionFilter{
		FromDate: startDate,
		ToDate:   endDate,
	})
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	// Aggregate by month.
	type monthData struct {
		income   float64
		expenses float64
		// category_id (0 = uncategorised) → amount
		byCat map[int64]float64
	}
	mdata := make(map[string]*monthData, months)
	for _, s := range slots {
		mdata[s.label] = &monthData{byCat: make(map[int64]float64)}
	}

	for _, t := range txns {
		if t.IsTransfer {
			continue
		}
		label := t.Date[:7]
		md, ok := mdata[label]
		if !ok {
			continue
		}
		if t.Amount > 0 {
			md.income += t.Amount
		} else {
			md.expenses += -t.Amount
		}
		var catID int64
		if t.CategoryID != nil {
			catID = *t.CategoryID
		}
		md.byCat[catID] += t.Amount
	}

	// Build the ordered months response.
	monthsOut := make([]MonthlyTrend, 0, months)
	for _, s := range slots {
		md := mdata[s.label]
		trend := MonthlyTrend{
			Month:    s.label,
			Income:   md.income,
			Expenses: md.expenses,
			Net:      md.income - md.expenses,
		}
		// Build sorted by_category list (expenses only, descending amount).
		type catEntry struct {
			id     int64
			amount float64
		}
		var entries []catEntry
		for catID, amt := range md.byCat {
			entries = append(entries, catEntry{id: catID, amount: amt})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].amount < entries[j].amount // most negative first (biggest expense)
		})
		for _, e := range entries {
			ct := CategoryTrend{
				CategoryID: e.id,
				Amount:     e.amount,
			}
			if cat, ok := catMap[e.id]; ok {
				ct.CategoryName = cat.Name
				ct.Color = cat.Color
				ct.IsIncome = cat.IsIncome
			}
			trend.ByCategory = append(trend.ByCategory, ct)
		}
		if trend.ByCategory == nil {
			trend.ByCategory = []CategoryTrend{}
		}
		monthsOut = append(monthsOut, trend)
	}

	// Compute net worth over time.
	// We get the current total account balance and subtract future net transactions
	// to derive the historical balance at end of each past month.
	accounts, err := ListAccounts(db, userID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	currentNetWorth := 0.0
	for _, a := range accounts {
		if a.Type == AccountTypeCredit {
			currentNetWorth -= a.Balance
		} else {
			currentNetWorth += a.Balance
		}
	}

	// Load all non-transfer transactions to compute historical net worth.
	allTxns, err := ListTransactions(db, userID, TransactionFilter{})
	if err != nil {
		return nil, fmt.Errorf("list all transactions: %w", err)
	}

	// Build net worth points by working backwards from current net worth.
	// netAfter[i] = sum of non-transfer transaction amounts after end of slots[i]
	netWorthPoints := make([]NetWorthPoint, months)
	for i := months - 1; i >= 0; i-- {
		endOfMonth := monthLastDay(slots[i].year, int(slots[i].month))
		var netAfter float64
		for _, t := range allTxns {
			if t.IsTransfer {
				continue
			}
			if t.Date > endOfMonth {
				netAfter += t.Amount
			}
		}
		netWorthPoints[i] = NetWorthPoint{
			Month: slots[i].label,
			Value: currentNetWorth - netAfter,
		}
	}

	// Year-over-year comparison: current year vs previous year (months 1-12).
	currentYear := now.Year()
	previousYear := currentYear - 1

	// Load transactions for both years.
	yoyTxns, err := ListTransactions(db, userID, TransactionFilter{
		FromDate: fmt.Sprintf("%d-01-01", previousYear),
		ToDate:   fmt.Sprintf("%d-12-31", currentYear),
	})
	if err != nil {
		return nil, fmt.Errorf("list yoy transactions: %w", err)
	}

	currentYearByMonth := make(map[int]float64)
	previousYearByMonth := make(map[int]float64)
	for _, t := range yoyTxns {
		if t.IsTransfer || t.Amount >= 0 {
			continue // expenses only
		}
		if len(t.Date) < 7 {
			continue
		}
		year, _ := strconv.Atoi(t.Date[:4])
		monthNum, _ := strconv.Atoi(t.Date[5:7])
		switch year {
		case currentYear:
			currentYearByMonth[monthNum] += -t.Amount
		case previousYear:
			previousYearByMonth[monthNum] += -t.Amount
		}
	}

	yoyMonths := make([]YoYMonth, 12)
	for i := 0; i < 12; i++ {
		yoyMonths[i] = YoYMonth{
			Month:    i + 1,
			Current:  currentYearByMonth[i+1],
			Previous: previousYearByMonth[i+1],
		}
	}

	return &TrendsResponse{
		Months:   monthsOut,
		NetWorth: netWorthPoints,
		YearOverYear: &YearOverYear{
			CurrentYear:  currentYear,
			PreviousYear: previousYear,
			Monthly:      yoyMonths,
		},
	}, nil
}

// TrendsHandler returns trend data for the authenticated user.
// Query params: months (default 6, max 36).
func TrendsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		months := 6
		if raw := r.URL.Query().Get("months"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "months must be a positive integer"})
				return
			}
			months = n
		}
		resp, err := GetTrends(db, user.ID, months)
		if err != nil {
			log.Printf("budget: trends for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load trends"})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
