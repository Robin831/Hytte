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
	FrequencyMonthly Frequency = "monthly"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyYearly  Frequency = "yearly"
)

// Account represents a financial account owned by a user.
type Account struct {
	ID       int64       `json:"id"`
	UserID   int64       `json:"user_id"`
	Name     string      `json:"name"`
	Type     AccountType `json:"type"`
	Currency string      `json:"currency"`
	Balance  float64     `json:"balance"`
	Icon     string      `json:"icon"`
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
type Transaction struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	AccountID      int64     `json:"account_id"`
	CategoryID     *int64    `json:"category_id"`
	Amount         float64   `json:"amount"`
	Description    string    `json:"description"`
	Date           time.Time `json:"date"`
	Tags           []string  `json:"tags"`
	IsTransfer     bool      `json:"is_transfer"`
	TransferToID   *int64    `json:"transfer_to"`
}

// Recurring represents a recurring transaction rule.
type Recurring struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	AccountID    int64      `json:"account_id"`
	CategoryID   *int64     `json:"category_id"`
	Amount       float64    `json:"amount"`
	Frequency    Frequency  `json:"frequency"`
	DayOfMonth   int        `json:"day_of_month"`
	StartDate    time.Time  `json:"start_date"`
	EndDate      *time.Time `json:"end_date"`
	LastGenerated *time.Time `json:"last_generated"`
}
