package salary

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

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
		}
		if body.Currency == "" {
			body.Currency = "NOK"
		}
		if body.StandardHours <= 0 {
			body.StandardHours = 7.5
		}

		cfg := Config{
			UserID:        user.ID,
			BaseSalary:    body.BaseSalary,
			HourlyRate:    body.HourlyRate,
			StandardHours: body.StandardHours,
			Currency:      body.Currency,
			EffectiveFrom: body.EffectiveFrom,
		}
		if err := SaveConfig(db, &cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		if len(body.CommissionTiers) == 0 {
			// Seed default tiers for a fresh config.
			if err := SeedDefaultTiers(db, cfg.ID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
		} else {
			// Replace all tiers.
			if _, err := db.Exec(`DELETE FROM salary_commission_tiers WHERE config_id = ?`, cfg.ID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			for _, t := range body.CommissionTiers {
				if _, err := db.Exec(
					`INSERT INTO salary_commission_tiers (config_id, floor, ceiling, rate) VALUES (?, ?, ?, ?)`,
					cfg.ID, t.Floor, t.Ceiling, t.Rate,
				); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
					return
				}
			}
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
	cfg, err := GetConfig(db, userID)
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
