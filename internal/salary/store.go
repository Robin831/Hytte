package salary

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// GetConfig returns the active salary config for a user (latest by effective_from).
// Returns nil, nil if no config exists.
func GetConfig(db *sql.DB, userID int64) (*Config, error) {
	var c Config
	err := db.QueryRow(`
		SELECT id, user_id, base_salary, hourly_rate, standard_hours, currency, effective_from
		FROM salary_config
		WHERE user_id = ?
		ORDER BY effective_from DESC
		LIMIT 1
	`, userID).Scan(&c.ID, &c.UserID, &c.BaseSalary, &c.HourlyRate, &c.StandardHours, &c.Currency, &c.EffectiveFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConfig inserts or updates a salary config (upsert on user_id + effective_from).
// Sets c.ID on insert.
func SaveConfig(db *sql.DB, c *Config) error {
	res, err := db.Exec(`
		INSERT INTO salary_config (user_id, base_salary, hourly_rate, standard_hours, currency, effective_from)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, effective_from) DO UPDATE SET
			base_salary    = excluded.base_salary,
			hourly_rate    = excluded.hourly_rate,
			standard_hours = excluded.standard_hours,
			currency       = excluded.currency
	`, c.UserID, c.BaseSalary, c.HourlyRate, c.StandardHours, c.Currency, c.EffectiveFrom)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if id != 0 {
		c.ID = id
	}
	return nil
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

// SaveRecord inserts a salary record and sets r.ID.
// Uses REPLACE to allow re-saving estimate records for the same month.
func SaveRecord(db *sql.DB, r *Record) error {
	isEstimate := 0
	if r.IsEstimate {
		isEstimate = 1
	}
	res, err := db.Exec(`
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
	`, r.UserID, r.Month, r.WorkingDays, r.HoursWorked, r.BillableHours, r.InternalHours,
		r.BaseAmount, r.Commission, r.Gross, r.Tax, r.Net,
		r.VacationDays, r.SickDays, isEstimate)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if id != 0 {
		r.ID = id
	}
	return nil
}

// GetRecords returns all salary records for a user in the given year, ordered by month.
func GetRecords(db *sql.DB, userID int64, year int64) ([]Record, error) {
	prefix := fmt.Sprintf("%04d-%%", year)
	rows, err := db.Query(`
		SELECT id, user_id, month, working_days, hours_worked, billable_hours, internal_hours,
		       base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate
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
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Month, &r.WorkingDays, &r.HoursWorked, &r.BillableHours, &r.InternalHours,
			&r.BaseAmount, &r.Commission, &r.Gross, &r.Tax, &r.Net,
			&r.VacationDays, &r.SickDays, &isEstimate,
		); err != nil {
			return nil, err
		}
		r.IsEstimate = isEstimate != 0
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

// GetHoursWorked computes total hours worked from work_sessions for a given user
// and month (YYYY-MM format). It sums all session durations from work_days in
// that month, using the HH:MM times stored in work_sessions.
func GetHoursWorked(db *sql.DB, userID int64, month string) (float64, error) {
	rows, err := db.Query(`
		SELECT ws.start_time, ws.end_time
		FROM work_sessions ws
		JOIN work_days wd ON wd.id = ws.day_id
		WHERE wd.user_id = ? AND wd.date LIKE ?
	`, userID, month+"-%")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	totalMinutes := 0
	for rows.Next() {
		var start, end string
		if err := rows.Scan(&start, &end); err != nil {
			return 0, err
		}
		startMin, err := parseHHMMToMinutes(start)
		if err != nil {
			return 0, fmt.Errorf("parse start time %q: %w", start, err)
		}
		endMin, err := parseHHMMToMinutes(end)
		if err != nil {
			return 0, fmt.Errorf("parse end time %q: %w", end, err)
		}
		if endMin > startMin {
			totalMinutes += endMin - startMin
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	return float64(totalMinutes) / 60.0, nil
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
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minute in %q", t)
	}
	return h*60 + m, nil
}
