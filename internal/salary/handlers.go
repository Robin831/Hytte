package salary

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Robin831/Hytte/internal/auth"
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
	BaseSalary      float64          `json:"base_salary"`
	HourlyRate      float64          `json:"hourly_rate"`
	StandardHours   float64          `json:"standard_hours"`
	Currency        string           `json:"currency"`
	EffectiveFrom   string           `json:"effective_from"` // YYYY-MM-DD; defaults to today
	CommissionTiers []CommissionTier `json:"commission_tiers"`
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
			INSERT INTO salary_config (user_id, base_salary, hourly_rate, standard_hours, currency, effective_from)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, effective_from) DO UPDATE SET
				base_salary    = excluded.base_salary,
				hourly_rate    = excluded.hourly_rate,
				standard_hours = excluded.standard_hours,
				currency       = excluded.currency
			RETURNING id
		`, user.ID, body.BaseSalary, body.HourlyRate, body.StandardHours, body.Currency, body.EffectiveFrom).Scan(&cfgID)
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
			ID:            cfgID,
			UserID:        user.ID,
			BaseSalary:    body.BaseSalary,
			HourlyRate:    body.HourlyRate,
			StandardHours: body.StandardHours,
			Currency:      body.Currency,
			EffectiveFrom: body.EffectiveFrom,
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
	Month                string           `json:"month"`
	Config               Config           `json:"config"`
	CommissionTiers      []CommissionTier `json:"commission_tiers"`
	Estimate             Record           `json:"estimate"`
	WorkingDays          int              `json:"working_days"`
	WorkingDaysDone      int              `json:"working_days_done"`
	WorkingDaysRemaining int              `json:"working_days_remaining"`
	HoursWorked          float64          `json:"hours_worked"`
	StandardHoursTotal   float64          `json:"standard_hours_total"`
	BillableRevenue      float64          `json:"billable_revenue"`
	AbsenceCostPerDay    float64          `json:"absence_cost_per_day"`
}

// buildEstimate produces an EstimateResponse for the given YYYY-MM month string.
// today is used to split working days into done vs remaining.
func buildEstimate(db *sql.DB, userID int64, month string, today time.Time) (*EstimateResponse, error) {
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

	brackets, err := GetTaxBrackets(db, userID, int64(year))
	if err != nil {
		return nil, err
	}

	hoursWorked, err := GetHoursWorked(db, userID, month)
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

	billableRevenue := hoursWorked * cfg.HourlyRate

	record := EstimateMonth(*cfg, tiers, brackets, hoursWorked, billableRevenue, totalDays, 0, 0)
	record.Month = month
	record.BillableHours = hoursWorked

	absenceCostPerDay := AbsenceDayCost(*cfg, totalDays, 1)
	standardHoursTotal := float64(totalDays) * cfg.StandardHours

	return &EstimateResponse{
		Month:                month,
		Config:               *cfg,
		CommissionTiers:      tiers,
		Estimate:             record,
		WorkingDays:          totalDays,
		WorkingDaysDone:      doneDays,
		WorkingDaysRemaining: remainingDays,
		HoursWorked:          hoursWorked,
		StandardHoursTotal:   standardHoursTotal,
		BillableRevenue:      billableRevenue,
		AbsenceCostPerDay:    absenceCostPerDay,
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
					utilPct = (rec.HoursWorked / standardHoursTotal) * 100
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

			// Build estimate (uses actual work session hours for past/current months).
			est, estErr := buildEstimate(db, user.ID, month, today)
			if estErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			if est == nil {
				// No salary config — include an empty projection placeholder.
				months = append(months, MonthProjection{
					Month:       month,
					WorkingDays: workingDays,
					IsEstimate:  true,
					IsCurrent:   isCurrent,
					IsFuture:    isFuture,
				})
				continue
			}

			var proj MonthProjection
			if isFuture {
				// Project future months at 100% utilization.
				fullHours := est.StandardHoursTotal
				fullRevenue := fullHours * est.Config.HourlyRate
				brackets, bracketsErr := GetTaxBrackets(db, user.ID, int64(year))
				if bracketsErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
				rec := EstimateMonth(est.Config, est.CommissionTiers, brackets, fullHours, fullRevenue, workingDays, 0, 0)
				proj = MonthProjection{
					Month:              month,
					WorkingDays:        workingDays,
					HoursWorked:        fullHours,
					StandardHoursTotal: est.StandardHoursTotal,
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
				utilPct := 0.0
				if est.StandardHoursTotal > 0 {
					utilPct = (est.HoursWorked / est.StandardHoursTotal) * 100
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

		t, _ := time.Parse("2006-01", month)
		workingDays := countWeekdays(t.Year(), int(t.Month()))

		rec := Record{
			UserID:        user.ID,
			Month:         month,
			WorkingDays:   int64(workingDays),
			HoursWorked:   body.HoursWorked,
			BillableHours: body.BillableHours,
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
