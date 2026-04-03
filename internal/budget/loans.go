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
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// -- Loan store --

// CreateLoan inserts a new loan for the given user and sets l.ID.
func CreateLoan(db *sql.DB, userID int64, l *Loan) error {
	encName, err := encryption.EncryptField(l.Name)
	if err != nil {
		return fmt.Errorf("encrypt loan name: %w", err)
	}
	encPropName, err := encryption.EncryptField(l.PropertyName)
	if err != nil {
		return fmt.Errorf("encrypt loan property_name: %w", err)
	}
	encNotes, err := encryption.EncryptField(l.Notes)
	if err != nil {
		return fmt.Errorf("encrypt loan notes: %w", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_loans
		 (user_id, name, principal, current_balance, annual_rate, monthly_payment,
		  start_date, term_months, property_value, property_name, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, encName, l.Principal, l.CurrentBalance, l.AnnualRate,
		l.MonthlyPayment, l.StartDate, l.TermMonths,
		l.PropertyValue, encPropName, encNotes,
	)
	if err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	l.UserID = userID
	return err
}

// GetLoan returns a single loan scoped to the given user.
func GetLoan(db *sql.DB, userID, id int64) (*Loan, error) {
	row := db.QueryRow(
		`SELECT id, user_id, name, principal, current_balance, annual_rate,
		        monthly_payment, start_date, term_months, property_value, property_name, notes
		 FROM budget_loans WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanLoan(row)
}

// ListLoans returns all loans for a user ordered by id.
func ListLoans(db *sql.DB, userID int64) ([]Loan, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, principal, current_balance, annual_rate,
		        monthly_payment, start_date, term_months, property_value, property_name, notes
		 FROM budget_loans WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var loans []Loan
	for rows.Next() {
		l, err := scanLoan(rows)
		if err != nil {
			return nil, err
		}
		loans = append(loans, *l)
	}
	if loans == nil {
		loans = []Loan{}
	}
	return loans, rows.Err()
}

// UpdateLoan replaces the mutable fields of an existing loan.
func UpdateLoan(db *sql.DB, userID int64, l *Loan) error {
	encName, err := encryption.EncryptField(l.Name)
	if err != nil {
		return fmt.Errorf("encrypt loan name: %w", err)
	}
	encPropName, err := encryption.EncryptField(l.PropertyName)
	if err != nil {
		return fmt.Errorf("encrypt loan property_name: %w", err)
	}
	encNotes, err := encryption.EncryptField(l.Notes)
	if err != nil {
		return fmt.Errorf("encrypt loan notes: %w", err)
	}
	res, err := db.Exec(
		`UPDATE budget_loans
		 SET name=?, principal=?, current_balance=?, annual_rate=?, monthly_payment=?,
		     start_date=?, term_months=?, property_value=?, property_name=?, notes=?
		 WHERE id=? AND user_id=?`,
		encName, l.Principal, l.CurrentBalance, l.AnnualRate,
		l.MonthlyPayment, l.StartDate, l.TermMonths,
		l.PropertyValue, encPropName, encNotes,
		l.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM budget_loans WHERE id=? AND user_id=?`, l.ID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

// DeleteLoan removes a loan scoped to the given user.
func DeleteLoan(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM budget_loans WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanLoan(s scanner) (*Loan, error) {
	var l Loan
	if err := s.Scan(
		&l.ID, &l.UserID, &l.Name, &l.Principal, &l.CurrentBalance,
		&l.AnnualRate, &l.MonthlyPayment, &l.StartDate, &l.TermMonths,
		&l.PropertyValue, &l.PropertyName, &l.Notes,
	); err != nil {
		return nil, err
	}
	name, err := encryption.DecryptField(l.Name)
	if err != nil {
		return nil, fmt.Errorf("decrypt loan name: %w", err)
	}
	l.Name = name
	propName, err := encryption.DecryptField(l.PropertyName)
	if err != nil {
		return nil, fmt.Errorf("decrypt loan property_name: %w", err)
	}
	l.PropertyName = propName
	notes, err := encryption.DecryptField(l.Notes)
	if err != nil {
		return nil, fmt.Errorf("decrypt loan notes: %w", err)
	}
	l.Notes = notes
	return &l, nil
}

// -- Amortization --

// BuildAmortization computes the amortization schedule for a loan.
// It generates rows starting from the loan's start_date, capping at maxRows (0 = use term_months).
// If monthly_payment is 0, it is calculated from principal, rate and term_months.
func BuildAmortization(l *Loan, maxRows int) ([]AmortizationRow, error) {
	balance := l.CurrentBalance
	if balance <= 0 {
		return []AmortizationRow{}, nil
	}

	monthlyRate := l.AnnualRate / 12.0
	payment := l.MonthlyPayment
	if payment <= 0 && l.TermMonths > 0 && monthlyRate > 0 {
		// Standard annuity formula: P = B * r / (1 - (1+r)^-n)
		payment = balance * monthlyRate / (1 - math.Pow(1+monthlyRate, -float64(l.TermMonths)))
	}
	if payment <= 0 {
		return []AmortizationRow{}, nil
	}

	limit := maxRows
	if limit <= 0 {
		limit = l.TermMonths
	}
	if limit <= 0 {
		limit = 360 // safety cap: 30 years
	}

	var startTime time.Time
	if l.StartDate != "" {
		var err error
		startTime, err = time.Parse("2006-01-02", l.StartDate)
		if err != nil {
			startTime = time.Now()
		}
	} else {
		startTime = time.Now()
	}

	rows := make([]AmortizationRow, 0, limit)
	for i := 1; i <= limit && balance > 0.005; i++ {
		interest := balance * monthlyRate
		principal := payment - interest
		if principal > balance {
			principal = balance
		}
		balance -= principal

		payDate := startTime.AddDate(0, i, 0)
		rows = append(rows, AmortizationRow{
			PaymentNum:       i,
			Date:             payDate.Format("2006-01-02"),
			Payment:          math.Round((interest+principal)*100) / 100,
			Principal:        math.Round(principal*100) / 100,
			Interest:         math.Round(interest*100) / 100,
			RemainingBalance: math.Round(balance*100) / 100,
			Rate:             l.AnnualRate,
		})
	}
	return rows, nil
}

// LTV returns the loan-to-value ratio (0-1) for a loan with a property value set.
// Returns 0 if property_value is 0.
func LTV(l *Loan) float64 {
	if l.PropertyValue <= 0 {
		return 0
	}
	return l.CurrentBalance / l.PropertyValue
}

// -- Handlers --

// LoansListHandler returns all loans for the authenticated user.
func LoansListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		loans, err := ListLoans(db, user.ID)
		if err != nil {
			log.Printf("budget: list loans for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list loans"})
			return
		}
		// Annotate each loan with its LTV ratio.
		type loanWithLTV struct {
			Loan
			LTVRatio float64 `json:"ltv_ratio"`
		}
		out := make([]loanWithLTV, len(loans))
		for i, l := range loans {
			out[i] = loanWithLTV{Loan: l, LTVRatio: LTV(&loans[i])}
		}
		writeJSON(w, http.StatusOK, map[string]any{"loans": out})
	}
}

// LoansCreateHandler creates a new loan for the authenticated user.
func LoansCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var l Loan
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if l.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if l.StartDate == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", l.StartDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_date must be YYYY-MM-DD"})
			return
		}
		if err := CreateLoan(db, user.ID, &l); err != nil {
			log.Printf("budget: create loan for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create loan"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"loan": l})
	}
}

// LoansUpdateHandler updates an existing loan owned by the authenticated user.
func LoansUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var l Loan
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		l.ID = id
		if l.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if l.StartDate == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", l.StartDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_date must be YYYY-MM-DD"})
			return
		}
		if err := UpdateLoan(db, user.ID, &l); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			log.Printf("budget: update loan %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update loan"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"loan": l})
	}
}

// LoansDeleteHandler removes a loan owned by the authenticated user.
func LoansDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if err := DeleteLoan(db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			log.Printf("budget: delete loan %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete loan"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// LoansAmortizationHandler returns the amortization schedule for a single loan.
// Query param: rows (max rows to return, default 360).
func LoansAmortizationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		loan, err := GetLoan(db, user.ID, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			log.Printf("budget: get loan %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load loan"})
			return
		}

		maxRows := 0
		if raw := r.URL.Query().Get("rows"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rows must be a positive integer"})
				return
			}
			maxRows = n
		}

		rows, err := BuildAmortization(loan, maxRows)
		if err != nil {
			log.Printf("budget: amortization for loan %d user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to compute amortization"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"loan":          loan,
			"amortization":  rows,
			"ltv_ratio":     LTV(loan),
			"ltv_max":       0.85,
		})
	}
}
