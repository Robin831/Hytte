package budget

import "time"

// AccountType represents the kind of financial account.
type AccountType string

const (
	AccountTypeChecking AccountType = "checking"
	AccountTypeSavings  AccountType = "savings"
	AccountTypeCredit   AccountType = "credit"
	AccountTypeCash     AccountType = "cash"
)

// Frequency represents how often a recurring transaction repeats.
type Frequency string

const (
	FrequencyMonthly   Frequency = "monthly"
	FrequencyQuarterly Frequency = "quarterly"
	FrequencyWeekly    Frequency = "weekly"
	FrequencyYearly    Frequency = "yearly"
)

// SplitType controls how a recurring expense is split between two parties.
type SplitType string

const (
	SplitTypePercentage   SplitType = "percentage"
	SplitTypeEqual        SplitType = "equal"
	SplitTypeFixedYou     SplitType = "fixed_you"
	SplitTypeFixedPartner SplitType = "fixed_partner"
)

// Account represents a financial account owned by a user.
type Account struct {
	ID          int64       `json:"id"`
	UserID      int64       `json:"user_id"`
	Name        string      `json:"name"`
	Type        AccountType `json:"type"`
	Currency    string      `json:"currency"`
	Balance     float64     `json:"balance"`
	Icon        string      `json:"icon"`
	CreditLimit float64     `json:"credit_limit"`
}

// Category represents a budget category for classifying transactions.
type Category struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	GroupName string `json:"group_name"`
	Icon      string `json:"icon"`
	Color     string `json:"color"`
	IsIncome  bool   `json:"is_income"`
}

// Transaction represents a single financial transaction.
// Date is stored as a YYYY-MM-DD string matching the TEXT column in the DB.
type Transaction struct {
	ID           int64    `json:"id"`
	UserID       int64    `json:"user_id"`
	AccountID    int64    `json:"account_id"`
	CategoryID   *int64   `json:"category_id"`
	Amount       float64  `json:"amount"`
	Description  string   `json:"description"`
	Date         string   `json:"date"`
	Tags         []string `json:"tags"`
	IsTransfer   bool     `json:"is_transfer"`
	TransferToID *int64   `json:"transfer_to"`
}

// BudgetLimit defines how much should be spent in a category per period.
// EffectiveFrom is YYYY-MM-DD (first day of a month); queries pick the latest
// limit whose EffectiveFrom <= the requested month.
type BudgetLimit struct {
	ID            int64   `json:"id"`
	UserID        int64   `json:"user_id"`
	CategoryID    int64   `json:"category_id"`
	Amount        float64 `json:"amount"`
	Period        string  `json:"period"`
	EffectiveFrom string  `json:"effective_from"`
}

// Loan represents a mortgage or other loan tracked by the user.
type Loan struct {
	ID               int64   `json:"id"`
	UserID           int64   `json:"user_id"`
	Name             string  `json:"name"`
	Principal        float64 `json:"principal"`
	CurrentBalance   float64 `json:"current_balance"`
	AnnualRate       float64 `json:"annual_rate"`
	MonthlyPayment   float64 `json:"monthly_payment"`
	StartDate        string  `json:"start_date"`
	FirstPaymentDate string  `json:"first_payment_date"`
	TermMonths       int     `json:"term_months"`
	PaymentDay       int     `json:"payment_day"`
	PropertyValue    float64 `json:"property_value"`
	PropertyName     string  `json:"property_name"`
	Notes            string  `json:"notes"`
}

// LoanRateChange represents a historical interest rate change on a loan.
// The rate applies from EffectiveDate onwards until the next rate change.
type LoanRateChange struct {
	ID            int64   `json:"id"`
	LoanID        int64   `json:"loan_id"`
	EffectiveDate string  `json:"effective_date"`
	AnnualRate    float64 `json:"annual_rate"`
}

// AmortizationRow is one row in a loan amortization schedule.
type AmortizationRow struct {
	PaymentNum       int     `json:"payment_num"`
	Date             string  `json:"date"`
	Payment          float64 `json:"payment"`
	Principal        float64 `json:"principal"`
	Interest         float64 `json:"interest"`
	RemainingBalance float64 `json:"remaining_balance"`
	Rate             float64 `json:"rate"`
}

// VariableBill represents a named variable expense group (e.g. "Electricity").
// It may optionally link to a recurring rule via RecurringID.
type VariableBill struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"user_id"`
	Name        string  `json:"name"`
	RecurringID *int64  `json:"recurring_id"`
	Entries     []VariableEntry `json:"entries"`
}

// VariableEntry is one sub-item line under a VariableBill for a given month.
type VariableEntry struct {
	ID         int64   `json:"id"`
	VariableID int64   `json:"variable_id"`
	Month      string  `json:"month"`
	SubName    string  `json:"sub_name"`
	Amount     float64 `json:"amount"`
}

// Recurring represents a recurring transaction rule.
// EndDate and LastGenerated are stored as TEXT NOT NULL in the database,
// so an empty string means the value is unset.
type Recurring struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	AccountID     int64     `json:"account_id"`
	CategoryID    *int64    `json:"category_id"`
	Amount        float64   `json:"amount"`
	Description   string    `json:"description"`
	Frequency     Frequency `json:"frequency"`
	DayOfMonth    int       `json:"day_of_month"`
	StartDate     time.Time `json:"start_date"`
	EndDate       string    `json:"end_date"`
	LastGenerated string    `json:"last_generated"`
	Active        bool      `json:"active"`
	SplitType     SplitType `json:"split_type"`
	SplitPct      *float64  `json:"split_pct"`
}
