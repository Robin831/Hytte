package salary

// Config holds the salary configuration for a user.
type Config struct {
	ID            int64   `json:"id"`
	UserID        int64   `json:"user_id"`
	BaseSalary    float64 `json:"base_salary"`
	HourlyRate    float64 `json:"hourly_rate"`
	StandardHours float64 `json:"standard_hours"` // per day, default 7.5
	Currency      string  `json:"currency"`        // default NOK
	EffectiveFrom string  `json:"effective_from"`  // YYYY-MM-DD
}

// CommissionTier defines one tier of the progressive commission structure.
// Ceiling == 0 means no upper limit (last tier).
type CommissionTier struct {
	ID       int64   `json:"id"`
	ConfigID int64   `json:"config_id"`
	Floor    float64 `json:"floor"`
	Ceiling  float64 `json:"ceiling"` // 0 = unbounded
	Rate     float64 `json:"rate"`    // decimal, e.g. 0.20 for 20%
}

// TaxBracket defines one bracket of the progressive income tax.
// IncomeTo == 0 means no upper limit (top bracket).
type TaxBracket struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Year       int64   `json:"year"`
	IncomeFrom float64 `json:"income_from"`
	IncomeTo   float64 `json:"income_to"` // 0 = unbounded
	Rate       float64 `json:"rate"`      // decimal, e.g. 0.22 for 22%
}

// Record holds a salary record for a single month.
type Record struct {
	ID                   int64   `json:"id"`
	UserID               int64   `json:"user_id"`
	Month                string  `json:"month"`          // YYYY-MM
	WorkingDays          int64   `json:"working_days"`
	HoursWorked          float64 `json:"hours_worked"`
	BillableHours        float64 `json:"billable_hours"`
	InternalHours        float64 `json:"internal_hours"`
	BaseAmount           float64 `json:"base_amount"`
	Commission           float64 `json:"commission"`
	Gross                float64 `json:"gross"`
	Tax                  float64 `json:"tax"`
	Net                  float64 `json:"net"`
	VacationDays         int64   `json:"vacation_days"`
	SickDays             int64   `json:"sick_days"`
	IsEstimate           bool    `json:"is_estimate"`
	BudgetTransactionID  *int64  `json:"budget_transaction_id,omitempty"` // linked budget transaction after sync
}
