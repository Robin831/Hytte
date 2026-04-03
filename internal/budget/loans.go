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
	if l.PaymentDay < 1 || l.PaymentDay > 28 {
		l.PaymentDay = 1
	}
	res, err := db.Exec(
		`INSERT INTO budget_loans
		 (user_id, name, principal, current_balance, annual_rate, monthly_payment,
		  start_date, term_months, payment_day, property_value, property_name, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, encName, l.Principal, l.CurrentBalance, l.AnnualRate,
		l.MonthlyPayment, l.StartDate, l.TermMonths, l.PaymentDay,
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
		        monthly_payment, start_date, term_months, payment_day, property_value, property_name, notes
		 FROM budget_loans WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanLoan(row)
}

// ListLoans returns all loans for a user ordered by id.
func ListLoans(db *sql.DB, userID int64) ([]Loan, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, principal, current_balance, annual_rate,
		        monthly_payment, start_date, term_months, payment_day, property_value, property_name, notes
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
	if l.PaymentDay < 1 || l.PaymentDay > 28 {
		l.PaymentDay = 1
	}
	res, err := db.Exec(
		`UPDATE budget_loans
		 SET name=?, principal=?, current_balance=?, annual_rate=?, monthly_payment=?,
		     start_date=?, term_months=?, payment_day=?, property_value=?, property_name=?, notes=?
		 WHERE id=? AND user_id=?`,
		encName, l.Principal, l.CurrentBalance, l.AnnualRate,
		l.MonthlyPayment, l.StartDate, l.TermMonths, l.PaymentDay,
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
		&l.PaymentDay, &l.PropertyValue, &l.PropertyName, &l.Notes,
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

// daysIn returns the number of days between two dates.
func daysIn(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

// annuityPayment365 calculates the annuity payment using actual/365 day-count.
// It iterates to find the fixed payment that amortises `balance` over `nMonths`
// starting from `start` on the given `payDay`, using actual days per period.
func annuityPayment365(balance, annualRate float64, nMonths int, start time.Time, payDay int) float64 {
	// Use r/12 as initial estimate, then refine with Newton's method.
	r12 := annualRate / 12.0
	payment := balance * r12 / (1 - math.Pow(1+r12, -float64(nMonths)))

	baseYear, baseMonth, _ := start.Date()
	loc := start.Location()

	// Refine: iterate the schedule with the candidate payment and adjust.
	for iter := 0; iter < 10; iter++ {
		bal := balance
		prevDate := time.Date(baseYear, baseMonth, payDay, 0, 0, 0, 0, loc)
		for m := 1; m <= nMonths && bal > 0.005; m++ {
			tm := int(baseMonth) - 1 + m
			curDate := time.Date(baseYear+tm/12, time.Month(tm%12+1), payDay, 0, 0, 0, 0, loc)
			days := daysIn(prevDate, curDate)
			interest := bal * annualRate * float64(days) / 365.0
			principal := payment - interest
			if principal > bal {
				principal = bal
			}
			bal -= principal
			prevDate = curDate
		}
		// bal is the residual. Spread it across remaining payments.
		if math.Abs(bal) < 0.01 {
			break
		}
		// Adjust payment proportionally.
		payment *= balance / (balance - bal)
	}
	return payment
}

// BuildAmortization computes the amortization schedule for a loan.
// It generates rows starting from the loan's start_date using the original principal,
// capping at maxRows (0 = use term_months). current_balance is informational only
// (used on the loan card); the schedule always starts from principal.
// If monthly_payment is 0, it is calculated from principal, rate and term_months.
// Interest is calculated using actual/365 day-count convention (standard for Norwegian mortgages).
// rateChanges is an optional sorted list of rate changes; when provided the rate switches
// at the effective_date of each change and the payment is recalculated.
func BuildAmortization(l *Loan, maxRows int, rateChanges []LoanRateChange) ([]AmortizationRow, error) {
	balance := l.Principal
	if balance <= 0 {
		return []AmortizationRow{}, nil
	}

	payment := l.MonthlyPayment

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

	payDay := l.PaymentDay
	if payDay < 1 || payDay > 28 {
		payDay = 1
	}

	currentRate := l.AnnualRate

	// Auto-calculate payment using actual/365 if not provided.
	if payment <= 0 && l.TermMonths > 0 && currentRate > 0 {
		payment = annuityPayment365(balance, currentRate, l.TermMonths, startTime, payDay)
	}
	if payment <= 0 {
		return []AmortizationRow{}, nil
	}

	rcIdx := 0 // index into rateChanges

	// Build payment dates by incrementing month from start, using payDay directly.
	// We avoid startTime.AddDate(0, i, 0) followed by day-snapping because Go's
	// AddDate can overflow months (e.g. Jan-31 + 1mo = Mar-03), causing duplicate
	// months after snapping back to payDay.
	baseYear, baseMonth, _ := startTime.Date()
	prevPayDate := time.Date(baseYear, baseMonth, payDay, 0, 0, 0, 0, startTime.Location())

	rows := make([]AmortizationRow, 0, limit)
	for i := 1; i <= limit && balance > 0.005; i++ {
		// Compute target month by adding i months to the base year/month.
		totalMonths := int(baseMonth) - 1 + i
		payDate := time.Date(baseYear+totalMonths/12, time.Month(totalMonths%12+1), payDay, 0, 0, 0, 0, startTime.Location())
		payDateStr := payDate.Format("2006-01-02")

		// Apply any rate changes that take effect on or before this payment date.
		// When rate changes, recalculate payment based on remaining balance and remaining term
		// (this is how banks adjust monthly payments when rates change).
		prevRate := currentRate
		for rcIdx < len(rateChanges) && rateChanges[rcIdx].EffectiveDate <= payDateStr {
			currentRate = rateChanges[rcIdx].AnnualRate
			rcIdx++
		}
		if currentRate != prevRate || (i == 1 && len(rateChanges) > 0 && currentRate != l.AnnualRate) {
			remainingMonths := l.TermMonths - (i - 1)
			if remainingMonths > 0 && currentRate > 0 {
				payment = annuityPayment365(balance, currentRate, remainingMonths, prevPayDate, payDay)
			}
		}

		// Interest based on actual days in this period / 365.
		days := daysIn(prevPayDate, payDate)
		interest := balance * currentRate * float64(days) / 365.0
		if payment <= interest {
			return nil, fmt.Errorf("monthly payment %.2f is less than or equal to monthly interest %.2f; loan would never be repaid", payment, interest)
		}
		principal := payment - interest
		if principal > balance {
			principal = balance
		}
		balance -= principal
		prevPayDate = payDate

		rows = append(rows, AmortizationRow{
			PaymentNum:       i,
			Date:             payDateStr,
			Payment:          math.Round((interest+principal)*100) / 100,
			Principal:        math.Round(principal*100) / 100,
			Interest:         math.Round(interest*100) / 100,
			RemainingBalance: math.Round(balance*100) / 100,
			Rate:             currentRate,
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

// -- Rate change store --

// ListRateChanges returns all rate changes for a loan, ordered by effective_date.
func ListRateChanges(db *sql.DB, loanID int64) ([]LoanRateChange, error) {
	rows, err := db.Query(
		`SELECT id, loan_id, effective_date, annual_rate
		 FROM budget_loan_rate_changes WHERE loan_id = ? ORDER BY effective_date`,
		loanID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var changes []LoanRateChange
	for rows.Next() {
		var rc LoanRateChange
		if err := rows.Scan(&rc.ID, &rc.LoanID, &rc.EffectiveDate, &rc.AnnualRate); err != nil {
			return nil, err
		}
		changes = append(changes, rc)
	}
	if changes == nil {
		changes = []LoanRateChange{}
	}
	return changes, rows.Err()
}

// CreateRateChange inserts a new rate change for a loan.
func CreateRateChange(db *sql.DB, rc *LoanRateChange) error {
	res, err := db.Exec(
		`INSERT INTO budget_loan_rate_changes (loan_id, effective_date, annual_rate) VALUES (?, ?, ?)`,
		rc.LoanID, rc.EffectiveDate, rc.AnnualRate,
	)
	if err != nil {
		return err
	}
	rc.ID, err = res.LastInsertId()
	return err
}

// DeleteRateChange removes a rate change by ID, scoped to the given loan.
func DeleteRateChange(db *sql.DB, loanID, id int64) error {
	res, err := db.Exec(`DELETE FROM budget_loan_rate_changes WHERE id = ? AND loan_id = ?`, id, loanID)
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

// -- Handlers --

// LoansAmortizationHandler returns the amortization schedule for a single loan.
// Query param: rows (max rows to return; when omitted, BuildAmortization uses term_months, capped at 360).
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

		rateChanges, err := ListRateChanges(db, loan.ID)
		if err != nil {
			log.Printf("budget: list rate changes for loan %d: %v", loan.ID, err)
			rateChanges = []LoanRateChange{} // non-fatal, fall back to fixed rate
		}

		rows, err := BuildAmortization(loan, maxRows, rateChanges)
		if err != nil {
			log.Printf("budget: amortization for loan %d user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to compute amortization"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"loan":          loan,
			"amortization":  rows,
			"rate_changes":  rateChanges,
			"ltv_ratio":     LTV(loan),
			"ltv_max":       0.85,
		})
	}
}

// LoanRateChangesListHandler returns rate changes for a loan.
func LoanRateChangesListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		loanID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		// Verify loan ownership.
		if _, err := GetLoan(db, user.ID, loanID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load loan"})
			return
		}
		changes, err := ListRateChanges(db, loanID)
		if err != nil {
			log.Printf("budget: list rate changes for loan %d: %v", loanID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list rate changes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rate_changes": changes})
	}
}

// LoanRateChangesCreateHandler adds a rate change to a loan.
func LoanRateChangesCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		loanID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if _, err := GetLoan(db, user.ID, loanID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load loan"})
			return
		}
		var rc LoanRateChange
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if rc.EffectiveDate == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "effective_date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", rc.EffectiveDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "effective_date must be YYYY-MM-DD"})
			return
		}
		rc.LoanID = loanID
		if err := CreateRateChange(db, &rc); err != nil {
			log.Printf("budget: create rate change for loan %d: %v", loanID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create rate change"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"rate_change": rc})
	}
}

// LoanRateChangesDeleteHandler removes a rate change from a loan.
func LoanRateChangesDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		loanID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid loan id"})
			return
		}
		rcID, err := strconv.ParseInt(chi.URLParam(r, "rateId"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid rate change id"})
			return
		}
		if _, err := GetLoan(db, user.ID, loanID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "loan not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load loan"})
			return
		}
		if err := DeleteRateChange(db, loanID, rcID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "rate change not found"})
				return
			}
			log.Printf("budget: delete rate change %d for loan %d: %v", rcID, loanID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete rate change"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
