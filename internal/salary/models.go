package salary

// Config holds the salary configuration for a user.
type Config struct {
	ID                   int64   `json:"id"`
	UserID               int64   `json:"user_id"`
	BaseSalary           float64 `json:"base_salary"`
	HourlyRate           float64 `json:"hourly_rate"`
	InternalHourlyRate   float64 `json:"internal_hourly_rate"` // rate for internal (meetings/admin) hours; 0 = not billable
	StandardHours        float64 `json:"standard_hours"`       // per day, default 7.5
	Currency             string  `json:"currency"`             // default NOK
	EffectiveFrom        string  `json:"effective_from"`       // YYYY-MM-DD
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

// TrinnskattTier defines one step-tax tier. The rate applies to annual income
// above IncomeFrom up to the next tier's IncomeFrom (or unbounded for the last
// tier). Rates are marginal (not cumulative).
type TrinnskattTier struct {
	IncomeFrom float64 `json:"income_from"`
	Rate       float64 `json:"rate"` // decimal, e.g. 0.017 for 1.7%
}

// TrekktabellParams holds the annual parameters for the Norwegian standard tax
// withholding formula (trekktabell). Norwegian employers use trekktabeller
// published by Skatteetaten to determine the monthly tax to withhold from wages.
type TrekktabellParams struct {
	ID                  int64            `json:"id"`
	UserID              int64            `json:"user_id"`
	Year                int64            `json:"year"`
	MinstefradragRate   float64          `json:"minstefradrag_rate"`    // fraction of gross, e.g. 0.46 (46%)
	MinstefradragMin    float64          `json:"minstefradrag_min"`     // minimum deduction amount
	MinstefradragMax    float64          `json:"minstefradrag_max"`     // maximum deduction amount (0 = no cap)
	Personfradrag       float64          `json:"personfradrag"`         // personal allowance deducted from alminnelig inntekt
	AlminneligSkattRate float64          `json:"alminnelig_skatt_rate"` // rate on net income after deductions, e.g. 0.22
	Trygdeavgift        float64          `json:"trygdeavgift"`          // social security contribution rate, e.g. 0.079
	TrinnskattTiers     []TrinnskattTier `json:"trinnskatt_tiers"`      // step-tax tiers applied to annual gross
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
