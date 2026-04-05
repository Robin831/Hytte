package salary

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/budget"
)

var validCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// countWeekdays returns the number of Mon–Fri days in the given year/month.
func countWeekdays(year, month int) int {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	count := 0
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			count++
		}
	}
	return count
}

// countWeekdaysUpTo returns the number of Mon–Fri days from the 1st of the
// month up to and including the given day number.
func countWeekdaysUpTo(year, month, day int) int {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	count := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			count++
		}
	}
	return count
}

// ConfigResponse wraps Config with its CommissionTiers.
type ConfigResponse struct {
	Config
	CommissionTiers []CommissionTier `json:"commission_tiers"`
}

// ConfigGetHandler handles GET /api/salary/config.
func ConfigGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		cfg, err := GetConfig(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if cfg == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}
		tiers, err := GetCommissionTiers(db, cfg.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, ConfigResponse{Config: *cfg, CommissionTiers: tiers})
	}
}

// ConfigPutRequest is the request body for PUT /api/salary/config.
type ConfigPutRequest struct {
	BaseSalary           float64          `json:"base_salary"`
	HourlyRate           float64          `json:"hourly_rate"`
	InternalHourlyRate   float64          `json:"internal_hourly_rate"`
	StandardHours        float64          `json:"standard_hours"`
	Currency             string           `json:"currency"`
	EffectiveFrom        string           `json:"effective_from"` // YYYY-MM-DD; defaults to today
	CommissionTiers      []CommissionTier `json:"commission_tiers"`
}

// ConfigPutHandler handles PUT /api/salary/config.
func ConfigPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		var body ConfigPutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if body.EffectiveFrom == "" {
			body.EffectiveFrom = time.Now().Format("2006-01-02")
		} else if _, parseErr := time.Parse("2006-01-02", body.EffectiveFrom); parseErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "effective_from must be YYYY-MM-DD"})
			return
		}
		if body.Currency == "" {
			body.Currency = "NOK"
		} else if !validCurrency.MatchString(body.Currency) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "currency must be a 3-letter ISO-4217 code"})
			return
		}
		if body.BaseSalary < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base_salary must not be negative"})
			return
		}
		if body.HourlyRate < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hourly_rate must not be negative"})
			return
		}
		if body.InternalHourlyRate < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "internal_hourly_rate must not be negative"})
			return
		}
		if body.StandardHours <= 0 {
			body.StandardHours = 7.5
		}

		tx, txErr := db.Begin()
		if txErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		var cfgID int64
		err := tx.QueryRow(`
			INSERT INTO salary_config (user_id, base_salary, hourly_rate, internal_hourly_rate, standard_hours, currency, effective_from)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, effective_from) DO UPDATE SET
				base_salary          = excluded.base_salary,
				hourly_rate          = excluded.hourly_rate,
				internal_hourly_rate = excluded.internal_hourly_rate,
				standard_hours       = excluded.standard_hours,
				currency             = excluded.currency
			RETURNING id
		`, user.ID, body.BaseSalary, body.HourlyRate, body.InternalHourlyRate, body.StandardHours, body.Currency, body.EffectiveFrom).Scan(&cfgID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		if len(body.CommissionTiers) == 0 {
			// Seed default tiers for a fresh config.
			defaultTiers := []struct{ floor, ceiling, rate float64 }{
				{0, 60000, 0},
				{60000, 80000, 0.20},
				{80000, 100000, 0.40},
				{100000, 0, 0.50},
			}
			for _, dt := range defaultTiers {
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO salary_commission_tiers (config_id, floor, ceiling, rate) VALUES (?, ?, ?, ?)`,
					cfgID, dt.floor, dt.ceiling, dt.rate,
				); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
			}
		} else {
			// Replace all tiers.
			if _, err := tx.Exec(`DELETE FROM salary_commission_tiers WHERE config_id = ?`, cfgID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			for _, t := range body.CommissionTiers {
				if _, err := tx.Exec(
					`INSERT INTO salary_commission_tiers (config_id, floor, ceiling, rate) VALUES (?, ?, ?, ?)`,
					cfgID, t.Floor, t.Ceiling, t.Rate,
				); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
			}
		}

		if err := tx.Commit(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		cfg := Config{
			ID:                 cfgID,
			UserID:             user.ID,
			BaseSalary:         body.BaseSalary,
			HourlyRate:         body.HourlyRate,
			InternalHourlyRate: body.InternalHourlyRate,
			StandardHours:      body.StandardHours,
			Currency:           body.Currency,
			EffectiveFrom:      body.EffectiveFrom,
		}

		tiers, err := GetCommissionTiers(db, cfg.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, ConfigResponse{Config: cfg, CommissionTiers: tiers})
	}
}

// EstimateResponse is the response for /api/salary/estimate/*.
type EstimateResponse struct {
	Month                   string           `json:"month"`
	Config                  Config           `json:"config"`
	CommissionTiers         []CommissionTier `json:"commission_tiers"`
	AdjustedCommissionTiers []CommissionTier `json:"adjusted_commission_tiers"`
	Estimate                Record           `json:"estimate"`
	WorkingDays             int              `json:"working_days"`
	WorkingDaysDone         int              `json:"working_days_done"`
	WorkingDaysRemaining    int              `json:"working_days_remaining"`
	HoursWorked             float64          `json:"hours_worked"`
	InternalHoursWorked     float64          `json:"internal_hours_worked"`
	StandardHoursTotal      float64          `json:"standard_hours_total"`
	BillableRevenue         float64          `json:"billable_revenue"`
	InternalRevenue         float64          `json:"internal_revenue"`
	AbsenceCostPerDay       float64          `json:"absence_cost_per_day"`
}

// buildEstimate produces an EstimateResponse for the given YYYY-MM month string.
// today is used to split working days into done vs remaining.
// buildEstimateWithParams computes a monthly estimate using pre-loaded trekktabell
// params, avoiding a redundant DB round-trip when the caller already holds them
// (e.g. EstimateYearHandler, which fetches params once per year).
func buildEstimateWithParams(db *sql.DB, userID int64, month string, today time.Time, taxParams TrekktabellParams) (*EstimateResponse, error) {
	cfg, err := GetConfigForMonth(db, userID, month)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}

	tiers, err := GetCommissionTiers(db, cfg.ID)
	if err != nil {
		return nil, err
	}

	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month %q: %w", month, err)
	}
	year := t.Year()
	mon := int(t.Month())

	hoursWorked, internalHoursWorked, err := getHoursWorkedBoth(db, userID, month)
	if err != nil {
		return nil, err
	}

	totalDays := countWeekdays(year, mon)

	var doneDays int
	monthStart := time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC)
	switch {
	case year == today.Year() && mon == int(today.Month()):
		doneDays = countWeekdaysUpTo(year, mon, today.Day())
	case monthStart.Before(today):
		// Past month — all days are done.
		doneDays = totalDays
	default:
		// Future month — 0 days done.
		doneDays = 0
	}
	remainingDays := totalDays - doneDays

	// Fetch vacation/sick days: first check if a confirmed record has overrides,
	// otherwise count from work_leave_days (the work hours page).
	var vacationDays, sickDays int
	existing, err := GetRecordForMonth(db, userID, month)
	if err != nil {
		return nil, err
	}
	if existing != nil && !existing.IsEstimate && (existing.VacationDays > 0 || existing.SickDays > 0) {
		vacationDays = int(existing.VacationDays)
		sickDays = int(existing.SickDays)
	} else {
		// Count from work_leave_days (registered in the work hours page).
		v, s, leaveErr := GetLeaveDaysForMonth(db, userID, month)
		if leaveErr != nil {
			return nil, leaveErr
		}
		vacationDays = v
		sickDays = s
	}

	billableHours := hoursWorked - internalHoursWorked
	billableRevenue := billableHours * cfg.HourlyRate
	internalRevenue := internalHoursWorked * cfg.InternalHourlyRate

	record := EstimateMonth(*cfg, tiers, taxParams, hoursWorked, billableRevenue, internalRevenue, totalDays, vacationDays, sickDays)
	record.Month = month
	record.BillableHours = billableHours
	record.InternalHours = internalHoursWorked

	absenceDays := vacationDays + sickDays
	adjustedTiers := ScaleTiersForAbsence(tiers, totalDays, absenceDays)
	absenceCostPerDay := AbsenceDayCost(*cfg, totalDays, 1)
	standardHoursTotal := float64(totalDays) * cfg.StandardHours

	return &EstimateResponse{
		Month:                   month,
		Config:                  *cfg,
		CommissionTiers:         tiers,
		AdjustedCommissionTiers: adjustedTiers,
		Estimate:                record,
		WorkingDays:             totalDays,
		WorkingDaysDone:         doneDays,
		WorkingDaysRemaining:    remainingDays,
		HoursWorked:             hoursWorked,
		InternalHoursWorked:     internalHoursWorked,
		StandardHoursTotal:      standardHoursTotal,
		BillableRevenue:         billableRevenue,
		InternalRevenue:         internalRevenue,
		AbsenceCostPerDay:       absenceCostPerDay,
	}, nil
}

func buildEstimate(db *sql.DB, userID int64, month string, today time.Time) (*EstimateResponse, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month %q: %w", month, err)
	}
	// Use GetOrSeedTrekktabellParams so Norwegian defaults are seeded on first use.
	taxParams, err := GetOrSeedTrekktabellParams(db, userID, int64(t.Year()))
	if err != nil {
		return nil, err
	}
	return buildEstimateWithParams(db, userID, month, today, taxParams)
}

// buildEstimateResponseFromRecord builds an EstimateResponse from a saved
// confirmed (non-estimate) record, reusing the config and tiers for context.
func buildEstimateResponseFromRecord(db *sql.DB, userID int64, month string, today time.Time, rec *Record) (*EstimateResponse, error) {
	cfg, err := GetConfigForMonth(db, userID, month)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}

	tiers, err := GetCommissionTiers(db, cfg.ID)
	if err != nil {
		return nil, err
	}

	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month %q: %w", month, err)
	}
	year := t.Year()
	mon := int(t.Month())
	totalDays := countWeekdays(year, mon)
	standardHoursTotal := float64(totalDays) * cfg.StandardHours
	billableRevenue := rec.BillableHours * cfg.HourlyRate
	internalRevenue := rec.InternalHours * cfg.InternalHourlyRate
	absenceCostPerDay := AbsenceDayCost(*cfg, totalDays, 1)

	monthStart := time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC)
	var doneDays int
	switch {
	case year == today.Year() && mon == int(today.Month()):
		doneDays = countWeekdaysUpTo(year, mon, today.Day())
	case monthStart.Before(today):
		doneDays = totalDays
	default:
		doneDays = 0
	}

	absenceDays := int(rec.VacationDays) + int(rec.SickDays)
	adjustedTiers := ScaleTiersForAbsence(tiers, totalDays, absenceDays)

	return &EstimateResponse{
		Month:                   month,
		Config:                  *cfg,
		CommissionTiers:         tiers,
		AdjustedCommissionTiers: adjustedTiers,
		Estimate:                *rec,
		WorkingDays:             totalDays,
		WorkingDaysDone:         doneDays,
		WorkingDaysRemaining:    totalDays - doneDays,
		HoursWorked:             rec.HoursWorked,
		InternalHoursWorked:     rec.InternalHours,
		StandardHoursTotal:      standardHoursTotal,
		BillableRevenue:         billableRevenue,
		InternalRevenue:         internalRevenue,
		AbsenceCostPerDay:       absenceCostPerDay,
	}, nil
}

// EstimateCurrentHandler handles GET /api/salary/estimate/current.
func EstimateCurrentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now()
		month := today.Format("2006-01")

		resp, err := buildEstimate(db, user.ID, month, today)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if resp == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// EstimateMonthHandler handles GET /api/salary/estimate/month?month=YYYY-MM.
// For past months with a confirmed (non-estimate) record, the saved record is
// returned directly instead of recalculating from work sessions.
func EstimateMonthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := r.URL.Query().Get("month")
		if month == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month parameter required (YYYY-MM)"})
			return
		}
		if _, parseErr := time.Parse("2006-01", month); parseErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format, expected YYYY-MM"})
			return
		}

		today := time.Now()
		currentMonth := today.Format("2006-01")

		// For past months, check if a confirmed record exists and return it directly.
		if month < currentMonth {
			rec, recErr := GetRecord(db, user.ID, month)
			if recErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			if rec != nil && !rec.IsEstimate {
				resp, respErr := buildEstimateResponseFromRecord(db, user.ID, month, today, rec)
				if respErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				if resp != nil {
					writeJSON(w, http.StatusOK, resp)
					return
				}
			}
		}

		resp, err := buildEstimate(db, user.ID, month, today)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if resp == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// AbsenceCostResponse is the response for GET /api/salary/absence-cost.
type AbsenceCostResponse struct {
	Month       string  `json:"month"`
	WorkingDays int     `json:"working_days"`
	CostPerDay  float64 `json:"cost_per_day"`
	Currency    string  `json:"currency"`
}

// AbsenceCostHandler handles GET /api/salary/absence-cost.
// Returns the salary cost of one absence day in the current month.
func AbsenceCostHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now()

		cfg, err := GetConfig(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if cfg == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}

		totalDays := countWeekdays(today.Year(), int(today.Month()))
		costPerDay := AbsenceDayCost(*cfg, totalDays, 1)

		writeJSON(w, http.StatusOK, AbsenceCostResponse{
			Month:       today.Format("2006-01"),
			WorkingDays: totalDays,
			CostPerDay:  costPerDay,
			Currency:    cfg.Currency,
		})
	}
}

// MonthProjection holds projected or actual salary data for a single month in
// the year overview.
type MonthProjection struct {
	Month              string  `json:"month"`
	WorkingDays        int     `json:"working_days"`
	HoursWorked        float64 `json:"hours_worked"`
	StandardHoursTotal float64 `json:"standard_hours_total"`
	BillableRevenue    float64 `json:"billable_revenue"`
	UtilizationPct     float64 `json:"utilization_pct"`
	BaseAmount         float64 `json:"base_amount"`
	Commission         float64 `json:"commission"`
	Gross              float64 `json:"gross"`
	Tax                float64 `json:"tax"`
	Net                float64 `json:"net"`
	IsEstimate         bool    `json:"is_estimate"`
	IsCurrent          bool    `json:"is_current"`
	IsFuture           bool    `json:"is_future"`
	RecordID           *int64  `json:"record_id,omitempty"`
}

// YearTotals sums the key financial fields across all 12 months.
type YearTotals struct {
	HoursWorked     float64 `json:"hours_worked"`
	BillableRevenue float64 `json:"billable_revenue"`
	BaseAmount      float64 `json:"base_amount"`
	Commission      float64 `json:"commission"`
	Gross           float64 `json:"gross"`
	Tax             float64 `json:"tax"`
	Net             float64 `json:"net"`
}

// YearEstimateResponse is the response for GET /api/salary/estimate/year.
type YearEstimateResponse struct {
	Year   int               `json:"year"`
	Months []MonthProjection `json:"months"`
	Totals YearTotals        `json:"totals"`
}

// EstimateYearHandler handles GET /api/salary/estimate/year?year=YYYY.
// Past months without a confirmed record use actual hours from work sessions.
// The current month uses the live estimate. Future months are projected at
// full utilization (working_days × standard_hours).
func EstimateYearHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now()

		year := today.Year()
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.Atoi(yearStr)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}

		// Fetch all saved records for the year to detect confirmed actuals.
		saved, err := GetRecords(db, user.ID, int64(year))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		recordMap := make(map[string]Record, len(saved))
		for _, rec := range saved {
			recordMap[rec.Month] = rec
		}

		// Fetch trekktabell params once for the entire year and reuse across all months.
		// GetOrSeedTrekktabellParams seeds Norwegian defaults on first use for the year.
		yearParams, err := GetOrSeedTrekktabellParams(db, user.ID, int64(year))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		todayMonthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)

		months := make([]MonthProjection, 0, 12)
		for m := 1; m <= 12; m++ {
			month := fmt.Sprintf("%04d-%02d", year, m)
			monthStart := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			isCurrent := monthStart.Equal(todayMonthStart)
			isFuture := monthStart.After(todayMonthStart)
			workingDays := countWeekdays(year, m)

			// Use confirmed actual record if one exists.
			if rec, ok := recordMap[month]; ok && !rec.IsEstimate {
				cfg, cfgErr := GetConfigForMonth(db, user.ID, month)
				if cfgErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				standardHoursTotal := 0.0
				billableRevenue := 0.0
				if cfg != nil {
					standardHoursTotal = float64(workingDays) * cfg.StandardHours
					billableRevenue = rec.BillableHours * cfg.HourlyRate
				}
				utilPct := 0.0
				if standardHoursTotal > 0 {
					utilPct = (rec.BillableHours / standardHoursTotal) * 100
				}
				id := rec.ID
				months = append(months, MonthProjection{
					Month:              month,
					WorkingDays:        workingDays,
					HoursWorked:        rec.HoursWorked,
					StandardHoursTotal: standardHoursTotal,
					BillableRevenue:    billableRevenue,
					UtilizationPct:     utilPct,
					BaseAmount:         rec.BaseAmount,
					Commission:         rec.Commission,
					Gross:              rec.Gross,
					Tax:                rec.Tax,
					Net:                rec.Net,
					IsEstimate:         false,
					IsCurrent:          isCurrent,
					IsFuture:           isFuture,
					RecordID:           &id,
				})
				continue
			}

			var proj MonthProjection
			if isFuture {
				// Project future months at 100% utilization using only config + tiers.
				// Skip GetHoursWorked — it is always zero for future months.
				cfg, cfgErr := GetConfigForMonth(db, user.ID, month)
				if cfgErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				if cfg == nil {
					months = append(months, MonthProjection{
						Month:       month,
						WorkingDays: workingDays,
						IsEstimate:  true,
						IsCurrent:   false,
						IsFuture:    true,
					})
					continue
				}
				tiers, tiersErr := GetCommissionTiers(db, cfg.ID)
				if tiersErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				fullHours := float64(workingDays) * cfg.StandardHours
				fullRevenue := fullHours * cfg.HourlyRate
				rec := EstimateMonth(*cfg, tiers, yearParams, fullHours, fullRevenue, 0, workingDays, 0, 0)
				proj = MonthProjection{
					Month:              month,
					WorkingDays:        workingDays,
					HoursWorked:        fullHours,
					StandardHoursTotal: fullHours,
					BillableRevenue:    fullRevenue,
					UtilizationPct:     100,
					BaseAmount:         rec.BaseAmount,
					Commission:         rec.Commission,
					Gross:              rec.Gross,
					Tax:                rec.Tax,
					Net:                rec.Net,
					IsEstimate:         true,
					IsCurrent:          false,
					IsFuture:           true,
				}
			} else {
				// Past or current month — use actual hours from work sessions.
				est, estErr := buildEstimateWithParams(db, user.ID, month, today, yearParams)
				if estErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				if est == nil {
					months = append(months, MonthProjection{
						Month:       month,
						WorkingDays: workingDays,
						IsEstimate:  true,
						IsCurrent:   isCurrent,
						IsFuture:    false,
					})
					continue
				}
				billableHours := 0.0
				if est.Config.HourlyRate > 0 {
					billableHours = est.BillableRevenue / est.Config.HourlyRate
				}
				utilPct := 0.0
				if est.StandardHoursTotal > 0 {
					utilPct = (billableHours / est.StandardHoursTotal) * 100
				}
				proj = MonthProjection{
					Month:              month,
					WorkingDays:        workingDays,
					HoursWorked:        est.HoursWorked,
					StandardHoursTotal: est.StandardHoursTotal,
					BillableRevenue:    est.BillableRevenue,
					UtilizationPct:     utilPct,
					BaseAmount:         est.Estimate.BaseAmount,
					Commission:         est.Estimate.Commission,
					Gross:              est.Estimate.Gross,
					Tax:                est.Estimate.Tax,
					Net:                est.Estimate.Net,
					IsEstimate:         true,
					IsCurrent:          isCurrent,
					IsFuture:           false,
				}
				if rec, ok := recordMap[month]; ok {
					id := rec.ID
					proj.RecordID = &id
				}
			}
			months = append(months, proj)
		}

		var totals YearTotals
		for _, mp := range months {
			totals.HoursWorked += mp.HoursWorked
			totals.BillableRevenue += mp.BillableRevenue
			totals.BaseAmount += mp.BaseAmount
			totals.Commission += mp.Commission
			totals.Gross += mp.Gross
			totals.Tax += mp.Tax
			totals.Net += mp.Net
		}

		writeJSON(w, http.StatusOK, YearEstimateResponse{
			Year:   year,
			Months: months,
			Totals: totals,
		})
	}
}

// RecordsGetHandler handles GET /api/salary/records?year=YYYY.
// Returns all salary records for the user in the given year.
func RecordsGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now()

		year := today.Year()
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.Atoi(yearStr)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}

		records, err := GetRecords(db, user.ID, int64(year))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, records)
	}
}

// RecordPutRequest is the request body for PUT /api/salary/records/{month}.
type RecordPutRequest struct {
	HoursWorked   float64 `json:"hours_worked"`
	BillableHours float64 `json:"billable_hours"`
	InternalHours float64 `json:"internal_hours"`
	BaseAmount    float64 `json:"base_amount"`
	Commission    float64 `json:"commission"`
	Gross         float64 `json:"gross"`
	Tax           float64 `json:"tax"`
	Net           float64 `json:"net"`
	VacationDays  int64   `json:"vacation_days"`
	SickDays      int64   `json:"sick_days"`
}

// RecordsPutHandler handles PUT /api/salary/records/{month}.
// Saves actual payslip data for the given month, marking the record as non-estimate.
func RecordsPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := chi.URLParam(r, "month")

		if _, err := time.Parse("2006-01", month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format, expected YYYY-MM"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
		var body RecordPutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if body.HoursWorked < 0 || body.BillableHours < 0 || body.InternalHours < 0 ||
			body.BaseAmount < 0 || body.Commission < 0 || body.Gross < 0 ||
			body.Tax < 0 || body.Net < 0 ||
			body.VacationDays < 0 || body.SickDays < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fields must not be negative"})
			return
		}
		if body.BillableHours+body.InternalHours > body.HoursWorked {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "billable_hours + internal_hours cannot exceed hours_worked"})
			return
		}

		t, _ := time.Parse("2006-01", month)
		workingDays := countWeekdays(t.Year(), int(t.Month()))

		rec := Record{
			UserID:        user.ID,
			Month:         month,
			WorkingDays:   int64(workingDays),
			HoursWorked:   body.HoursWorked,
			BillableHours: body.BillableHours,
			InternalHours: body.InternalHours,
			BaseAmount:    body.BaseAmount,
			Commission:    body.Commission,
			Gross:         body.Gross,
			Tax:           body.Tax,
			Net:           body.Net,
			VacationDays:  body.VacationDays,
			SickDays:      body.SickDays,
			IsEstimate:    false,
		}

		if err := SaveRecord(db, &rec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// RecordsConfirmHandler handles POST /api/salary/records/{month}/confirm.
// Marks the current estimate for the month as an actual (confirmed) record.
func RecordsConfirmHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := chi.URLParam(r, "month")

		if _, err := time.Parse("2006-01", month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format, expected YYYY-MM"})
			return
		}

		today := time.Now()
		currentMonth := today.Format("2006-01")
		if month >= currentMonth {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "can only confirm completed past months"})
			return
		}

		est, err := buildEstimate(db, user.ID, month, today)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if est == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}

		rec := est.Estimate
		rec.UserID = user.ID
		rec.Month = month
		rec.WorkingDays = int64(est.WorkingDays)
		rec.IsEstimate = false

		if err := SaveRecord(db, &rec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// TaxTableResponse is the response for GET /api/salary/tax-table.
type TaxTableResponse struct {
	Year     int64        `json:"year"`
	Brackets []TaxBracket `json:"brackets"`
}

// TaxTableGetHandler handles GET /api/salary/tax-table?year=YYYY.
// If no brackets exist for the requested year, Norwegian defaults are seeded
// and returned so the user always sees a populated table.
func TaxTableGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		year := int64(time.Now().Year())
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.ParseInt(yearStr, 10, 64)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}

		brackets, err := GetOrSeedTaxBrackets(db, user.ID, year)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, TaxTableResponse{Year: year, Brackets: brackets})
	}
}

// TaxTableDefaultsHandler handles GET /api/salary/tax-table/defaults?year=YYYY.
// Returns the built-in Norwegian tax brackets for the given year without
// reading or writing the database. This is the single source of truth for
// default bracket values; frontends must use this endpoint instead of
// hardcoding bracket data.
func TaxTableDefaultsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		year := int64(time.Now().Year())
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.ParseInt(yearStr, 10, 64)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}
		// userID 0: DefaultTaxBrackets will set the real user ID when the
		// caller saves the returned brackets; for defaults we don't need one.
		brackets := DefaultTaxBrackets(0, year)
		writeJSON(w, http.StatusOK, TaxTableResponse{Year: year, Brackets: brackets})
	}
}

// TaxTablePutRequest is the request body for PUT /api/salary/tax-table.
type TaxTablePutRequest struct {
	Year     int64        `json:"year"`
	Brackets []TaxBracket `json:"brackets"`
}

// TaxTablePutHandler handles PUT /api/salary/tax-table.
// Replaces all brackets for the given year.
func TaxTablePutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
		var body TaxTablePutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if body.Year < 2000 || body.Year > 2100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "year must be between 2000 and 2100"})
			return
		}
		if len(body.Brackets) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brackets must not be empty"})
			return
		}
		for _, b := range body.Brackets {
			if b.Rate < 0 || b.Rate > 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bracket rate must be between 0 and 1"})
				return
			}
			if b.IncomeFrom < 0 || (b.IncomeTo != 0 && b.IncomeTo <= b.IncomeFrom) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bracket income range"})
				return
			}
		}

		// Sort brackets by income_from before validating the set as a whole.
		sort.Slice(body.Brackets, func(i, j int) bool {
			return body.Brackets[i].IncomeFrom < body.Brackets[j].IncomeFrom
		})
		if body.Brackets[0].IncomeFrom != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "first bracket must start at income_from = 0"})
			return
		}
		for i := 1; i < len(body.Brackets); i++ {
			prev := body.Brackets[i-1]
			if prev.IncomeTo == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only the last bracket may be unbounded (income_to = 0)"})
				return
			}
			if body.Brackets[i].IncomeFrom < prev.IncomeTo {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brackets must not overlap"})
				return
			}
		}

		// Apply user ID and year to all brackets.
		for i := range body.Brackets {
			body.Brackets[i].UserID = user.ID
			body.Brackets[i].Year = body.Year
		}

		if err := SaveTaxBrackets(db, user.ID, body.Year, body.Brackets); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		brackets, err := GetTaxBrackets(db, user.ID, body.Year)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, TaxTableResponse{Year: body.Year, Brackets: brackets})
	}
}

// TrekktabellGetHandler handles GET /api/salary/trekktabell?year=YYYY.
// If no params exist for the requested year, Norwegian defaults are seeded
// and returned so the user always sees populated trekktabell params.
func TrekktabellGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		year := int64(time.Now().Year())
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.ParseInt(yearStr, 10, 64)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}

		params, err := GetOrSeedTrekktabellParams(db, user.ID, year)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, params)
	}
}

// TrekktabellDefaultsHandler handles GET /api/salary/trekktabell/defaults?year=YYYY.
// Returns the built-in Norwegian trekktabell params for the given year without
// reading or writing the database.
func TrekktabellDefaultsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		year := int64(time.Now().Year())
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.ParseInt(yearStr, 10, 64)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}
		// userID 0: caller sets the real user ID when saving; for defaults we don't need one.
		params := DefaultTrekktabellParams(0, year)
		writeJSON(w, http.StatusOK, params)
	}
}

// TrekktabellPutHandler handles PUT /api/salary/trekktabell.
// Replaces the trekktabell params for the given year.
func TrekktabellPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
		var body TrekktabellParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if body.Year < 2000 || body.Year > 2100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "year must be between 2000 and 2100"})
			return
		}
		if body.MinstefradragRate < 0 || body.MinstefradragRate > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minstefradrag_rate must be between 0 and 1"})
			return
		}
		if body.AlminneligSkattRate < 0 || body.AlminneligSkattRate > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alminnelig_skatt_rate must be between 0 and 1"})
			return
		}
		if body.Trygdeavgift < 0 || body.Trygdeavgift > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trygdeavgift must be between 0 and 1"})
			return
		}
		if body.MinstefradragMin < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minstefradrag_min must not be negative"})
			return
		}
		if body.MinstefradragMax < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minstefradrag_max must not be negative"})
			return
		}
		if body.Personfradrag < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "personfradrag must not be negative"})
			return
		}
		if body.MinstefradragMax < body.MinstefradragMin {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minstefradrag_max must be greater than or equal to minstefradrag_min"})
			return
		}
		for _, tier := range body.TrinnskattTiers {
			if tier.Rate < 0 || tier.Rate > 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trinnskatt tier rate must be between 0 and 1"})
				return
			}
			if tier.IncomeFrom < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trinnskatt tier income_from must not be negative"})
				return
			}
		}

		sort.Slice(body.TrinnskattTiers, func(i, j int) bool {
			return body.TrinnskattTiers[i].IncomeFrom < body.TrinnskattTiers[j].IncomeFrom
		})
		for i := 1; i < len(body.TrinnskattTiers); i++ {
			if body.TrinnskattTiers[i].IncomeFrom <= body.TrinnskattTiers[i-1].IncomeFrom {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trinnskatt tier income_from values must be strictly increasing"})
				return
			}
		}
		body.UserID = user.ID
		if err := SaveTrekktabellParams(db, body); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		saved, err := GetTrekktabellParams(db, user.ID, body.Year)
		if err != nil || saved == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

// VacationResponse holds vacation tracking data for a year.
type VacationResponse struct {
	Year               int     `json:"year"`
	DaysAllowance      int     `json:"days_allowance"`      // statutory default: 25 days (Norway)
	DaysUsed           int     `json:"days_used"`           // sum from salary_records for the year
	DaysRemaining      int     `json:"days_remaining"`      // allowance - used (clamped to 0)
	GrossYTD           float64 `json:"gross_ytd"`           // sum of gross from confirmed records
	FeriepengerPct     float64 `json:"feriepenger_pct"`     // 10.2 (standard Norwegian rate)
	FeriepengerAccrued float64 `json:"feriepenger_accrued"` // gross_ytd × feriepenger_pct / 100
}

const (
	vacationDaysAllowance = 25    // Norwegian statutory minimum (ferieloven)
	feriepengerRate       = 0.102 // 10.2% of gross — standard Norwegian rate
)

// VacationHandler handles GET /api/salary/vacation?year=YYYY.
// Returns feriedager used/remaining and feriepenger accrued for the year.
func VacationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		year := int64(time.Now().Year())
		if yearStr := r.URL.Query().Get("year"); yearStr != "" {
			y, err := strconv.ParseInt(yearStr, 10, 64)
			if err != nil || y < 2000 || y > 2100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
				return
			}
			year = y
		}

		records, err := GetRecords(db, user.ID, year)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		var daysUsed int
		var grossYTD float64
		for _, rec := range records {
			daysUsed += int(rec.VacationDays)
			// Feriepenger accrues on actual (confirmed) gross earnings only.
			if !rec.IsEstimate {
				grossYTD += rec.Gross
			}
		}

		remaining := vacationDaysAllowance - daysUsed
		if remaining < 0 {
			remaining = 0
		}

		writeJSON(w, http.StatusOK, VacationResponse{
			Year:               int(year),
			DaysAllowance:      vacationDaysAllowance,
			DaysUsed:           daysUsed,
			DaysRemaining:      remaining,
			GrossYTD:           grossYTD,
			FeriepengerPct:     feriepengerRate * 100,
			FeriepengerAccrued: math.Round(grossYTD*feriepengerRate*100) / 100,
		})
	}
}

// SyncBudgetResponse is returned by POST /api/salary/records/{month}/sync-budget.
type SyncBudgetResponse struct {
	Month               string  `json:"month"`
	NetIncome           float64 `json:"net_income"`
	BudgetTransactionID int64   `json:"budget_transaction_id"`
	CategoryID          int64   `json:"category_id"`
	AccountID           int64   `json:"account_id"`
}

// SyncBudgetHandler handles POST /api/salary/records/{month}/sync-budget.
// Creates or replaces a budget income transaction for the month's net salary.
// Requires at least one non-credit budget account. If no income category named
// "Salary" exists, one is created automatically.
func SyncBudgetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := chi.URLParam(r, "month")

		if _, err := time.Parse("2006-01", month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format, expected YYYY-MM"})
			return
		}

		// Determine net income: prefer confirmed record, fall back to estimate.
		today := time.Now()
		est, err := buildEstimate(db, user.ID, month, today)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if est == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no salary config found"})
			return
		}
		netIncome := est.Estimate.Net

		// Prefer confirmed record's net if available.
		t, _ := time.Parse("2006-01", month)
		records, err := GetRecords(db, user.ID, int64(t.Year()))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		var existingRecord *Record
		for i := range records {
			if records[i].Month == month {
				existingRecord = &records[i]
				if !records[i].IsEstimate {
					netIncome = records[i].Net
				}
				break
			}
		}

		// Find first non-credit account.
		accounts, err := budget.ListAccounts(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		var accountID int64
		for _, a := range accounts {
			if a.Type != budget.AccountTypeCredit {
				accountID = a.ID
				break
			}
		}
		if accountID == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "no checking or savings account found; please add a budget account first"})
			return
		}

		// Find or create a "Salary" income category.
		categories, err := budget.ListCategories(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		var categoryID int64
		for _, c := range categories {
			if c.IsIncome && c.Name == "Salary" {
				categoryID = c.ID
				break
			}
		}
		if categoryID == 0 {
			cat := &budget.Category{
				Name:      "Salary",
				GroupName: "Income",
				IsIncome:  true,
				Icon:      "wallet",
				Color:     "#22c55e",
			}
			if err := budget.CreateCategory(db, user.ID, cat); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			categoryID = cat.ID
		}

		// Delete the previously synced transaction if tracked.
		if existingRecord != nil && existingRecord.BudgetTransactionID != nil {
			// Ignore delete errors — transaction may have been manually removed.
			_ = budget.DeleteTransaction(db, user.ID, *existingRecord.BudgetTransactionID)
		}

		// Create the new income transaction on the first day of the month.
		catIDRef := categoryID
		txn := &budget.Transaction{
			AccountID:   accountID,
			CategoryID:  &catIDRef,
			Amount:      netIncome,
			Description: fmt.Sprintf("Salary %s", month),
			Date:        fmt.Sprintf("%s-01", month),
			Tags:        []string{"salary-sync"},
		}
		if err := budget.CreateTransaction(db, user.ID, txn); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		// Ensure a salary_records row exists, then link the transaction.
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO salary_records (user_id, month, working_days) VALUES (?, ?, ?)`,
			user.ID, month, countWeekdays(t.Year(), int(t.Month())),
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		if err := SetBudgetTransactionID(db, user.ID, month, &txn.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, SyncBudgetResponse{
			Month:               month,
			NetIncome:           netIncome,
			BudgetTransactionID: txn.ID,
			CategoryID:          categoryID,
			AccountID:           accountID,
		})
	}
}
