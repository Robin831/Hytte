package salary

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// nullInt64Ptr converts a sql.NullInt64 to *int64 (nil if not valid).
func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// GetConfig returns the active salary config for a user (latest by effective_from).
// Returns nil, nil if no config exists.
func GetConfig(db *sql.DB, userID int64) (*Config, error) {
	var c Config
	err := db.QueryRow(`
		SELECT id, user_id, base_salary, hourly_rate, internal_hourly_rate, standard_hours, currency, effective_from
		FROM salary_config
		WHERE user_id = ?
		ORDER BY effective_from DESC
		LIMIT 1
	`, userID).Scan(&c.ID, &c.UserID, &c.BaseSalary, &c.HourlyRate, &c.InternalHourlyRate, &c.StandardHours, &c.Currency, &c.EffectiveFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetConfigForMonth returns the active salary config for a user effective for
// the given month (YYYY-MM). It picks the latest config where effective_from
// is on or before the last day of that month. Returns nil, nil if none exists.
func GetConfigForMonth(db *sql.DB, userID int64, month string) (*Config, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month %q: %w", month, err)
	}
	endOfMonth := t.AddDate(0, 1, -1).Format("2006-01-02")

	var c Config
	err = db.QueryRow(`
		SELECT id, user_id, base_salary, hourly_rate, internal_hourly_rate, standard_hours, currency, effective_from
		FROM salary_config
		WHERE user_id = ? AND effective_from <= ?
		ORDER BY effective_from DESC
		LIMIT 1
	`, userID, endOfMonth).Scan(&c.ID, &c.UserID, &c.BaseSalary, &c.HourlyRate, &c.InternalHourlyRate, &c.StandardHours, &c.Currency, &c.EffectiveFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConfig inserts or updates a salary config (upsert on user_id + effective_from).
// Sets c.ID to the row's id after the upsert.
func SaveConfig(db *sql.DB, c *Config) error {
	err := db.QueryRow(`
		INSERT INTO salary_config (user_id, base_salary, hourly_rate, internal_hourly_rate, standard_hours, currency, effective_from)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, effective_from) DO UPDATE SET
			base_salary           = excluded.base_salary,
			hourly_rate           = excluded.hourly_rate,
			internal_hourly_rate  = excluded.internal_hourly_rate,
			standard_hours        = excluded.standard_hours,
			currency              = excluded.currency
		RETURNING id
	`, c.UserID, c.BaseSalary, c.HourlyRate, c.InternalHourlyRate, c.StandardHours, c.Currency, c.EffectiveFrom).Scan(&c.ID)
	return err
}

// SeedDefaultTiers inserts the four standard Norwegian commission tiers for a
// newly created config. Existing rows are left unchanged (INSERT OR IGNORE).
func SeedDefaultTiers(db *sql.DB, configID int64) error {
	tiers := []struct {
		floor, ceiling, rate float64
	}{
		{0, 60000, 0},
		{60000, 80000, 0.20},
		{80000, 100000, 0.40},
		{100000, 0, 0.50},
	}
	for _, t := range tiers {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO salary_commission_tiers (config_id, floor, ceiling, rate)
			VALUES (?, ?, ?, ?)
		`, configID, t.floor, t.ceiling, t.rate)
		if err != nil {
			return fmt.Errorf("seed commission tier floor=%.0f: %w", t.floor, err)
		}
	}
	return nil
}

// GetCommissionTiers returns all tiers for the given config, ordered by floor ascending.
func GetCommissionTiers(db *sql.DB, configID int64) ([]CommissionTier, error) {
	rows, err := db.Query(`
		SELECT id, config_id, floor, ceiling, rate
		FROM salary_commission_tiers
		WHERE config_id = ?
		ORDER BY floor
	`, configID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tiers []CommissionTier
	for rows.Next() {
		var t CommissionTier
		if err := rows.Scan(&t.ID, &t.ConfigID, &t.Floor, &t.Ceiling, &t.Rate); err != nil {
			return nil, err
		}
		tiers = append(tiers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tiers == nil {
		tiers = []CommissionTier{}
	}
	return tiers, nil
}

// GetRecordForMonth returns the salary record for a user and month (YYYY-MM),
// or nil if no record exists.
func GetRecordForMonth(db *sql.DB, userID int64, month string) (*Record, error) {
	var r Record
	var isEstimate int
	var btxID sql.NullInt64
	err := db.QueryRow(`
		SELECT id, user_id, month, working_days, hours_worked, billable_hours, internal_hours,
		       base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate,
		       budget_transaction_id
		FROM salary_records
		WHERE user_id = ? AND month = ?
	`, userID, month).Scan(
		&r.ID, &r.UserID, &r.Month, &r.WorkingDays, &r.HoursWorked, &r.BillableHours, &r.InternalHours,
		&r.BaseAmount, &r.Commission, &r.Gross, &r.Tax, &r.Net,
		&r.VacationDays, &r.SickDays, &isEstimate, &btxID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.IsEstimate = isEstimate != 0
	r.BudgetTransactionID = nullInt64Ptr(btxID)
	return &r, nil
}

// SaveRecord inserts or updates a salary record and sets r.ID.
// Uses INSERT ... ON CONFLICT(user_id, month) DO UPDATE to allow re-saving
// records for the same month by updating the existing row in place.
// This intentionally does not use REPLACE, which would delete and reinsert
// the row and could affect row identity, foreign keys, or related triggers.
func SaveRecord(db *sql.DB, r *Record) error {
	isEstimate := 0
	if r.IsEstimate {
		isEstimate = 1
	}
	err := db.QueryRow(`
		INSERT INTO salary_records
			(user_id, month, working_days, hours_worked, billable_hours, internal_hours,
			 base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, month) DO UPDATE SET
			working_days   = excluded.working_days,
			hours_worked   = excluded.hours_worked,
			billable_hours = excluded.billable_hours,
			internal_hours = excluded.internal_hours,
			base_amount    = excluded.base_amount,
			commission     = excluded.commission,
			gross          = excluded.gross,
			tax            = excluded.tax,
			net            = excluded.net,
			vacation_days  = excluded.vacation_days,
			sick_days      = excluded.sick_days,
			is_estimate    = excluded.is_estimate
		RETURNING id
	`, r.UserID, r.Month, r.WorkingDays, r.HoursWorked, r.BillableHours, r.InternalHours,
		r.BaseAmount, r.Commission, r.Gross, r.Tax, r.Net,
		r.VacationDays, r.SickDays, isEstimate).Scan(&r.ID)
	return err
}

// GetRecords returns all salary records for a user in the given year, ordered by month.
func GetRecords(db *sql.DB, userID int64, year int64) ([]Record, error) {
	prefix := fmt.Sprintf("%04d-%%", year)
	rows, err := db.Query(`
		SELECT id, user_id, month, working_days, hours_worked, billable_hours, internal_hours,
		       base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate,
		       budget_transaction_id
		FROM salary_records
		WHERE user_id = ? AND month LIKE ?
		ORDER BY month
	`, userID, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		var isEstimate int
		var btxID sql.NullInt64
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Month, &r.WorkingDays, &r.HoursWorked, &r.BillableHours, &r.InternalHours,
			&r.BaseAmount, &r.Commission, &r.Gross, &r.Tax, &r.Net,
			&r.VacationDays, &r.SickDays, &isEstimate, &btxID,
		); err != nil {
			return nil, err
		}
		r.IsEstimate = isEstimate != 0
		r.BudgetTransactionID = nullInt64Ptr(btxID)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if records == nil {
		records = []Record{}
	}
	return records, nil
}

// SaveTaxBrackets replaces all tax brackets for the given user and year
// atomically. Callers are responsible for validating whether empty bracket
// sets are allowed before invoking this store operation.
func SaveTaxBrackets(db *sql.DB, userID int64, year int64, brackets []TaxBracket) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`DELETE FROM salary_tax_brackets WHERE user_id = ? AND year = ?`,
		userID, year,
	); err != nil {
		return err
	}
	for _, b := range brackets {
		if _, err := tx.Exec(
			`INSERT INTO salary_tax_brackets (user_id, year, income_from, income_to, rate)
			 VALUES (?, ?, ?, ?, ?)`,
			userID, year, b.IncomeFrom, b.IncomeTo, b.Rate,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetOrSeedTaxBrackets returns the tax brackets for a user and year. If none
// exist, the Norwegian default brackets for that year are seeded and returned.
func GetOrSeedTaxBrackets(db *sql.DB, userID int64, year int64) ([]TaxBracket, error) {
	brackets, err := GetTaxBrackets(db, userID, year)
	if err != nil {
		return nil, err
	}
	if len(brackets) > 0 {
		return brackets, nil
	}
	// No brackets found — seed Norwegian defaults.
	defaults := DefaultTaxBrackets(userID, year)
	if err := SaveTaxBrackets(db, userID, year, defaults); err != nil {
		return nil, err
	}
	return GetTaxBrackets(db, userID, year)
}

// GetRecord returns the salary record for a user and month (YYYY-MM).
// Returns nil, nil if no record exists.
func GetRecord(db *sql.DB, userID int64, month string) (*Record, error) {
	var r Record
	var isEstimate int
	var btxID sql.NullInt64
	err := db.QueryRow(`
		SELECT id, user_id, month, working_days, hours_worked, billable_hours, internal_hours,
		       base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate,
		       budget_transaction_id
		FROM salary_records
		WHERE user_id = ? AND month = ?
	`, userID, month).Scan(
		&r.ID, &r.UserID, &r.Month, &r.WorkingDays, &r.HoursWorked, &r.BillableHours, &r.InternalHours,
		&r.BaseAmount, &r.Commission, &r.Gross, &r.Tax, &r.Net,
		&r.VacationDays, &r.SickDays, &isEstimate, &btxID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.IsEstimate = isEstimate != 0
	r.BudgetTransactionID = nullInt64Ptr(btxID)
	return &r, nil
}

// SetBudgetTransactionID stores or clears the linked budget transaction ID for
// the salary record at the given month. A nil id clears the link.
func SetBudgetTransactionID(db *sql.DB, userID int64, month string, id *int64) error {
	var val interface{}
	if id != nil {
		val = *id
	}
	_, err := db.Exec(
		`UPDATE salary_records SET budget_transaction_id = ? WHERE user_id = ? AND month = ?`,
		val, userID, month,
	)
	return err
}

// GetTaxBrackets returns all tax brackets for a user and year, ordered by income_from.
func GetTaxBrackets(db *sql.DB, userID int64, year int64) ([]TaxBracket, error) {
	rows, err := db.Query(`
		SELECT id, user_id, year, income_from, income_to, rate
		FROM salary_tax_brackets
		WHERE user_id = ? AND year = ?
		ORDER BY income_from
	`, userID, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var brackets []TaxBracket
	for rows.Next() {
		var b TaxBracket
		if err := rows.Scan(&b.ID, &b.UserID, &b.Year, &b.IncomeFrom, &b.IncomeTo, &b.Rate); err != nil {
			return nil, err
		}
		brackets = append(brackets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if brackets == nil {
		brackets = []TaxBracket{}
	}
	return brackets, nil
}

// getHoursWorkedBoth computes total and internal hours in a single query pass
// for a given user and month (YYYY-MM format). Returns (totalHours, internalHours).
func getHoursWorkedBoth(db *sql.DB, userID int64, month string) (float64, float64, error) {
	rows, err := db.Query(`
		SELECT ws.start_time, ws.end_time, ws.is_internal
		FROM work_sessions ws
		JOIN work_days wd ON wd.id = ws.day_id
		WHERE wd.user_id = ? AND wd.date LIKE ?
	`, userID, month+"-%")
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	totalMinutes := 0
	internalMinutes := 0
	for rows.Next() {
		var start, end string
		var isInternal bool
		if err := rows.Scan(&start, &end, &isInternal); err != nil {
			return 0, 0, err
		}
		startMin, err := parseHHMMToMinutes(start)
		if err != nil {
			return 0, 0, fmt.Errorf("parse start time %q: %w", start, err)
		}
		endMin, err := parseHHMMToMinutes(end)
		if err != nil {
			return 0, 0, fmt.Errorf("parse end time %q: %w", end, err)
		}
		if endMin > startMin {
			d := endMin - startMin
			totalMinutes += d
			if isInternal {
				internalMinutes += d
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	return float64(totalMinutes) / 60.0, float64(internalMinutes) / 60.0, nil
}

// GetHoursWorked computes total hours worked from work_sessions for a given user
// and month (YYYY-MM format). It sums all session durations from work_days in
// that month, using the HH:MM times stored in work_sessions.
func GetHoursWorked(db *sql.DB, userID int64, month string) (float64, error) {
	total, _, err := getHoursWorkedBoth(db, userID, month)
	return total, err
}

// GetInternalHoursWorked computes hours spent in internal sessions (is_internal=1)
// for a given user and month (YYYY-MM format). Internal hours are company meetings
// and admin time that is billable at the internal_hourly_rate.
func GetInternalHoursWorked(db *sql.DB, userID int64, month string) (float64, error) {
	_, internal, err := getHoursWorkedBoth(db, userID, month)
	return internal, err
}

// parseHHMMToMinutes parses a "HH:MM" string into total minutes since midnight.
func parseHHMMToMinutes(t string) (int, error) {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format %q", t)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hour in %q", t)
	}
	if h < 0 || h > 23 {
		return 0, fmt.Errorf("hour out of range in %q", t)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minute in %q", t)
	}
	if m < 0 || m > 59 {
		return 0, fmt.Errorf("minute out of range in %q", t)
	}
	return h*60 + m, nil
}
