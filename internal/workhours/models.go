package workhours

// WorkDay represents a single work day entry for a user.
type WorkDay struct {
	ID         int64           `json:"id"`
	UserID     int64           `json:"user_id"`
	Date       string          `json:"date"` // YYYY-MM-DD
	Lunch      bool            `json:"lunch"`
	Notes      string          `json:"notes"`
	CreatedAt  string          `json:"created_at"`
	Sessions   []WorkSession   `json:"sessions"`
	Deductions []WorkDeduction `json:"deductions"`
}

// WorkSession represents a single time block within a work day.
type WorkSession struct {
	ID        int64  `json:"id"`
	DayID     int64  `json:"day_id"`
	StartTime string `json:"start_time"` // HH:MM (24h)
	EndTime   string `json:"end_time"`   // HH:MM (24h)
	SortOrder int    `json:"sort_order"`
}

// WorkDeduction represents a named time deduction applied to a work day.
type WorkDeduction struct {
	ID       int64  `json:"id"`
	DayID    int64  `json:"day_id"`
	Name     string `json:"name"`    // e.g. "Kindergarten drop-off"
	Minutes  int    `json:"minutes"` // duration deducted
	PresetID *int64 `json:"preset_id,omitempty"`
}

// WorkDeductionPreset is a reusable deduction template so users don't have to
// type "Kindergarten" every day.
type WorkDeductionPreset struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`
	Name           string `json:"name"`
	DefaultMinutes int    `json:"default_minutes"`
	Icon           string `json:"icon"`
	SortOrder      int    `json:"sort_order"`
	Active         bool   `json:"active"`
}

// DaySummary contains the calculated totals for a single work day.
type DaySummary struct {
	Date             string  `json:"date"`
	GrossMinutes     int     `json:"gross_minutes"`
	LunchMinutes     int     `json:"lunch_minutes"`
	DeductionMinutes int     `json:"deduction_minutes"`
	NetMinutes       int     `json:"net_minutes"`
	ReportedMinutes  int     `json:"reported_minutes"`
	ReportedHours    float64 `json:"reported_hours"`
	RemainderMinutes int     `json:"remainder_minutes"` // goes to flex pool
	StandardMinutes  int     `json:"standard_minutes"`
	BalanceMinutes   int     `json:"balance_minutes"` // reported - standard
}

// FlexPoolResult holds the current state of the flex pool.
type FlexPoolResult struct {
	TotalMinutes   int `json:"total_minutes"`
	ToNextInterval int `json:"to_next_interval"` // minutes until next rounding threshold
}

// UserSettings holds work hours settings read from user_preferences.
type UserSettings struct {
	StandardDayMinutes int // default 450 (7.5h)
	RoundingMinutes    int // default 30
	LunchMinutes       int // default 30
}

// DefaultSettings returns the default work hours settings.
func DefaultSettings() UserSettings {
	return UserSettings{
		StandardDayMinutes: 450,
		RoundingMinutes:    30,
		LunchMinutes:       30,
	}
}
