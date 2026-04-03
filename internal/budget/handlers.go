package budget

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("budget: writeJSON encode error: %v", err)
	}
}

// -- Accounts --

// AccountsListHandler returns all accounts for the authenticated user.
func AccountsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		accounts, err := ListAccounts(db, user.ID)
		if err != nil {
			log.Printf("budget: list accounts for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list accounts"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
	}
}

// AccountsCreateHandler creates a new account for the authenticated user.
func AccountsCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var a Account
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if a.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if a.Currency == "" {
			a.Currency = "NOK"
		}
		if err := CreateAccount(db, user.ID, &a); err != nil {
			log.Printf("budget: create account for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create account"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"account": a})
	}
}

// AccountsUpdateHandler updates an existing account owned by the authenticated user.
func AccountsUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var a Account
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		a.ID = id
		if a.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if a.Currency == "" {
			a.Currency = "NOK"
		}
		if err := UpdateAccount(db, user.ID, &a); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
				return
			}
			log.Printf("budget: update account %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update account"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account": a})
	}
}

// AccountsDeleteHandler removes an account owned by the authenticated user.
func AccountsDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if err := DeleteAccount(db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
				return
			}
			log.Printf("budget: delete account %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete account"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// -- Categories --

// CategoriesListHandler seeds the default categories only when the user has no
// categories yet, then returns the user's full category list.
func CategoriesListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		cats, err := ListCategories(db, user.ID)
		if err != nil {
			log.Printf("budget: list categories for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list categories"})
			return
		}

		if len(cats) == 0 {
			if err := SeedDefaultCategories(db, user.ID); err != nil {
				log.Printf("budget: seed categories for user %d: %v", user.ID, err)
				// Continue — returning an empty list is better than a hard failure.
			} else {
				cats, err = ListCategories(db, user.ID)
				if err != nil {
					log.Printf("budget: list categories for user %d after seed: %v", user.ID, err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list categories"})
					return
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
	}
}

// CategoriesCreateHandler creates a new category for the authenticated user.
func CategoriesCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var c Category
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if c.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if err := CreateCategory(db, user.ID, &c); err != nil {
			log.Printf("budget: create category for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create category"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"category": c})
	}
}

// CategoriesUpdateHandler updates an existing category owned by the authenticated user.
func CategoriesUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var c Category
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		c.ID = id
		if c.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if err := UpdateCategory(db, user.ID, &c); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
				return
			}
			log.Printf("budget: update category %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update category"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"category": c})
	}
}

// CategoriesDeleteHandler removes a category owned by the authenticated user.
func CategoriesDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if err := DeleteCategory(db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
				return
			}
			log.Printf("budget: delete category %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete category"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// -- Transactions --

// TransactionsListHandler returns transactions for the authenticated user with optional
// query parameter filters: month (YYYY-MM), category_id, account_id.
func TransactionsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		q := r.URL.Query()

		var f TransactionFilter

		if month := q.Get("month"); month != "" {
			y, mo, err := parseYearMonth(month)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month must be YYYY-MM"})
				return
			}
			f.FromDate = month + "-01"
			f.ToDate = monthLastDay(y, mo)
		}
		if raw := q.Get("account_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account_id"})
				return
			}
			f.AccountID = &id
		}
		if raw := q.Get("category_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category_id"})
				return
			}
			f.CategoryID = &id
		}

		txns, err := ListTransactions(db, user.ID, f)
		if err != nil {
			log.Printf("budget: list transactions for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list transactions"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"transactions": txns})
	}
}

// TransactionsCreateHandler creates a new transaction for the authenticated user.
func TransactionsCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var t Transaction
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if t.AccountID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
			return
		}
		if t.Date == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", t.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be in YYYY-MM-DD format"})
			return
		}
		if err := CreateTransaction(db, user.ID, &t); err != nil {
			log.Printf("budget: create transaction for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create transaction"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"transaction": t})
	}
}

// TransactionsUpdateHandler updates an existing transaction owned by the authenticated user.
func TransactionsUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var t Transaction
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		t.ID = id
		if t.AccountID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
			return
		}
		if t.Date == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", t.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be in YYYY-MM-DD format"})
			return
		}
		if err := UpdateTransaction(db, user.ID, &t); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction not found"})
				return
			}
			log.Printf("budget: update transaction %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"transaction": t})
	}
}

// TransactionsDeleteHandler removes a transaction owned by the authenticated user.
func TransactionsDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if err := DeleteTransaction(db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction not found"})
				return
			}
			log.Printf("budget: delete transaction %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete transaction"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// -- Summary --

// CategorySummary holds totals for a single category within a month.
type CategorySummary struct {
	CategoryID   *int64  `json:"category_id"`
	CategoryName string  `json:"category_name"`
	Color        string  `json:"color"`
	IsIncome     bool    `json:"is_income"`
	Total        float64 `json:"total"`
	// BudgetAmount is the monthly limit for this category (0 if no limit set).
	BudgetAmount float64 `json:"budget_amount"`
	// BudgetPct is the percentage of the budget used (0 if no budget set).
	// For expense categories: |Total| / BudgetAmount * 100.
	BudgetPct float64 `json:"budget_pct"`
}

// MonthlySummary is the response body for SummaryHandler.
type MonthlySummary struct {
	Month        string            `json:"month"`
	IncomeTotal  float64           `json:"income_total"`
	ExpenseTotal float64           `json:"expense_total"`
	Net          float64           `json:"net"`
	IncomeSplit  int               `json:"income_split"`
	ByCategory   []CategorySummary `json:"by_category"`
}

// SummaryHandler returns a monthly budget summary for ?month=YYYY-MM.
// Shows income_total, expense_total, net (igjen), income split ratio, and
// per-category totals.
func SummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := r.URL.Query().Get("month")
		if month == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month query parameter is required (YYYY-MM)"})
			return
		}
		y, mo, err := parseYearMonth(month)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month must be YYYY-MM"})
			return
		}

		txns, err := ListTransactions(db, user.ID, TransactionFilter{
			FromDate: month + "-01",
			ToDate:   monthLastDay(y, mo),
		})
		if err != nil {
			log.Printf("budget: summary list transactions for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load transactions"})
			return
		}

		cats, err := ListCategories(db, user.ID)
		if err != nil {
			log.Printf("budget: summary list categories for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load categories"})
			return
		}
		catMap := make(map[int64]Category, len(cats))
		for _, c := range cats {
			catMap[c.ID] = c
		}

		incomeSplit, err := GetIncomeSplit(db, user.ID)
		if err != nil {
			log.Printf("budget: get income split for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load income split"})
			return
		}

		limits, err := GetBudgetLimits(db, user.ID, month)
		if err != nil {
			log.Printf("budget: get budget limits for user %d month %s: %v", user.ID, month, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load budget limits"})
			return
		}

		// Aggregate totals by category, preserving first-seen order.
		type catKey struct {
			id    int64
			valid bool
		}
		type agg struct {
			catID    *int64
			name     string
			color    string
			isIncome bool
			total    float64
		}
		aggMap := map[catKey]*agg{}
		var aggOrder []catKey

		var incomeTotal, expenseTotal float64
		for _, t := range txns {
			var k catKey
			if t.CategoryID != nil {
				k = catKey{id: *t.CategoryID, valid: true}
			}
			if _, seen := aggMap[k]; !seen {
				a := &agg{catID: t.CategoryID}
				if t.CategoryID != nil {
					if cat, ok := catMap[*t.CategoryID]; ok {
						a.name = cat.Name
						a.color = cat.Color
						a.isIncome = cat.IsIncome
					}
				}
				aggMap[k] = a
				aggOrder = append(aggOrder, k)
			}
			aggMap[k].total += t.Amount
			if t.Amount > 0 {
				incomeTotal += t.Amount
			} else {
				expenseTotal += -t.Amount
			}
		}

		byCat := make([]CategorySummary, 0, len(aggOrder))
		for _, k := range aggOrder {
			a := aggMap[k]
			cs := CategorySummary{
				CategoryID:   a.catID,
				CategoryName: a.name,
				Color:        a.color,
				IsIncome:     a.isIncome,
				Total:        a.total,
			}
			if a.catID != nil {
				if lim, ok := limits[*a.catID]; ok {
					cs.BudgetAmount = lim.Amount
					if lim.Amount > 0 {
						cs.BudgetPct = (math.Abs(a.total) / lim.Amount) * 100
					}
				}
			}
			byCat = append(byCat, cs)
		}

		writeJSON(w, http.StatusOK, MonthlySummary{
			Month:        month,
			IncomeTotal:  incomeTotal,
			ExpenseTotal: expenseTotal,
			Net:          incomeTotal - expenseTotal,
			IncomeSplit:  incomeSplit,
			ByCategory:   byCat,
		})
	}
}

// -- Budget limits --

// LimitsGetHandler returns budget limits for the given ?month=YYYY-MM.
// Returns an object keyed by category_id.
func LimitsGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := r.URL.Query().Get("month")
		if month == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month query parameter is required (YYYY-MM)"})
			return
		}
		if _, _, err := parseYearMonth(month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month must be YYYY-MM"})
			return
		}
		lims, err := GetBudgetLimits(db, user.ID, month)
		if err != nil {
			log.Printf("budget: get limits for user %d month %s: %v", user.ID, month, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load budget limits"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"limits": lims, "month": month})
	}
}

// limitInput is a single category limit in a PUT request.
type limitInput struct {
	CategoryID int64   `json:"category_id"`
	Amount     float64 `json:"amount"`
}

// LimitsPutHandler sets/replaces budget limits for the given month.
// Body: {"month":"YYYY-MM","limits":[{"category_id":1,"amount":5000},...]}
func LimitsPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var req struct {
			Month  string       `json:"month"`
			Limits []limitInput `json:"limits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if req.Month == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month is required (YYYY-MM)"})
			return
		}
		if _, _, err := parseYearMonth(req.Month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month must be YYYY-MM"})
			return
		}
		for _, li := range req.Limits {
			if li.CategoryID <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category_id must be positive"})
				return
			}
			lim := BudgetLimit{
				CategoryID:    li.CategoryID,
				Amount:        li.Amount,
				Period:        "monthly",
				EffectiveFrom: req.Month,
			}
			if err := SetBudgetLimit(db, user.ID, &lim); err != nil {
				log.Printf("budget: set limit for user %d category %d: %v", user.ID, li.CategoryID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save budget limit"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// -- helpers --

// parseYearMonth parses a YYYY-MM string into year and month numbers.
func parseYearMonth(s string) (int, int, error) {
	if len(s) != 7 || s[4] != '-' {
		return 0, 0, fmt.Errorf("invalid month format %q", s)
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid year in %q", s)
	}
	mo, err := strconv.Atoi(s[5:])
	if err != nil || mo < 1 || mo > 12 {
		return 0, 0, fmt.Errorf("invalid month in %q", s)
	}
	return y, mo, nil
}

// monthLastDay returns the last day of the given year/month as YYYY-MM-DD.
// It uses time.Date month overflow: day 0 of month+1 equals last day of month.
func monthLastDay(year, month int) string {
	last := time.Date(year, time.Month(month+1), 0, 0, 0, 0, 0, time.UTC)
	return last.Format("2006-01-02")
}
