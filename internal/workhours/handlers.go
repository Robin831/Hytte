package workhours

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// loadSettings reads work hours settings from the user's preferences. Falls
// back to defaults for any missing keys.
func loadSettings(db *sql.DB, userID int64) (UserSettings, error) {
	settings := DefaultSettings()

	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return settings, err
	}

	if v, ok := prefs["work_hours_standard_day"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			settings.StandardDayMinutes = n
		}
	}
	if v, ok := prefs["work_hours_rounding"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			settings.RoundingMinutes = n
		}
	}
	if v, ok := prefs["work_hours_lunch_minutes"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			settings.LunchMinutes = n
		}
	}
	return settings, nil
}

// DayGetHandler handles GET /api/workhours/day?date=YYYY-MM-DD.
// Returns the day entry with sessions, deductions, and calculated summary.
func DayGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		if !isValidDate(date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}

		day, err := GetDay(db, user.ID, date)
		if err != nil {
			log.Printf("workhours: get day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load day"})
			return
		}
		if day == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"day":     nil,
				"summary": nil,
			})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		summary, err := CalculateDay(*day, settings)
		if err != nil {
			log.Printf("workhours: calculate day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate summary"})
			return
		}

		// Credit any flex redemptions made for this specific date into reported minutes.
		redeemed, err := SumFlexRedemptionsForDate(db, user.ID, date)
		if err != nil {
			log.Printf("workhours: load day redemptions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load redemptions"})
			return
		}
		if redeemed > 0 {
			summary.RedeemedMinutes = redeemed
			summary.ReportedMinutes += redeemed
			summary.ReportedHours = float64(summary.ReportedMinutes) / 60.0
			summary.BalanceMinutes += redeemed
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"day":     day,
			"summary": summary,
		})
	}
}

// DayPutHandler handles PUT /api/workhours/day.
// Creates or updates the work day record (lunch flag and notes).
func DayPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Date  string  `json:"date"`
			Lunch bool    `json:"lunch"`
			Notes *string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Date == "" {
			body.Date = time.Now().Format("2006-01-02")
		}
		if !isValidDate(body.Date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}

		// Preserve existing notes when not provided by the caller.
		var notesStr string
		if body.Notes != nil {
			notesStr = *body.Notes
		} else if existing, _ := GetDay(db, user.ID, body.Date); existing != nil {
			notesStr = existing.Notes
		}

		day, err := UpsertDay(db, user.ID, body.Date, body.Lunch, notesStr)
		if err != nil {
			log.Printf("workhours: upsert day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save day"})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}
		summary, err := CalculateDay(*day, settings)
		if err != nil {
			log.Printf("workhours: calculate day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate summary"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"day":     day,
			"summary": summary,
		})
	}
}

// DayDeleteHandler handles DELETE /api/workhours/day?date=YYYY-MM-DD.
func DayDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		date := r.URL.Query().Get("date")
		if date == "" || !isValidDate(date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}

		if err := DeleteDay(db, user.ID, date); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "day not found"})
			return
		} else if err != nil {
			log.Printf("workhours: delete day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete day"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SessionAddHandler handles POST /api/workhours/day/session.
func SessionAddHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			DayID      int64  `json:"day_id"`
			StartTime  string `json:"start_time"`
			EndTime    string `json:"end_time"`
			SortOrder  int    `json:"sort_order"`
			IsInternal bool   `json:"is_internal"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := ValidateHHMM(body.StartTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start_time: " + err.Error()})
			return
		}
		if err := ValidateHHMM(body.EndTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end_time: " + err.Error()})
			return
		}
		startParsed, _ := time.Parse("15:04", body.StartTime)
		endParsed, _ := time.Parse("15:04", body.EndTime)
		if !endParsed.After(startParsed) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "end_time must be after start_time"})
			return
		}

		session, err := AddSession(db, body.DayID, user.ID, body.StartTime, body.EndTime, body.SortOrder, body.IsInternal)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "day not found"})
			return
		} else if err != nil {
			log.Printf("workhours: add session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add session"})
			return
		}

		writeJSON(w, http.StatusCreated, session)
	}
}

// SessionUpdateHandler handles PUT /api/workhours/day/session/{id}.
func SessionUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		sessionID, err := parseID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
			return
		}

		var body struct {
			StartTime  string `json:"start_time"`
			EndTime    string `json:"end_time"`
			SortOrder  int    `json:"sort_order"`
			IsInternal bool   `json:"is_internal"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := ValidateHHMM(body.StartTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start_time: " + err.Error()})
			return
		}
		if err := ValidateHHMM(body.EndTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end_time: " + err.Error()})
			return
		}
		startParsed, _ := time.Parse("15:04", body.StartTime)
		endParsed, _ := time.Parse("15:04", body.EndTime)
		if !endParsed.After(startParsed) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "end_time must be after start_time"})
			return
		}

		if err := UpdateSession(db, sessionID, user.ID, body.StartTime, body.EndTime, body.SortOrder, body.IsInternal); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		} else if err != nil {
			log.Printf("workhours: update session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// SessionDeleteHandler handles DELETE /api/workhours/day/session/{id}.
func SessionDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		sessionID, err := parseID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
			return
		}

		if err := DeleteSession(db, sessionID, user.ID); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		} else if err != nil {
			log.Printf("workhours: delete session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeductionAddHandler handles POST /api/workhours/day/deduction.
func DeductionAddHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			DayID    int64  `json:"day_id"`
			Name     string `json:"name"`
			Minutes  int    `json:"minutes"`
			PresetID *int64 `json:"preset_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.Minutes <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minutes must be positive"})
			return
		}

		deduction, err := AddDeduction(db, body.DayID, user.ID, body.Name, body.Minutes, body.PresetID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "day not found"})
			return
		} else if err != nil {
			log.Printf("workhours: add deduction: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add deduction"})
			return
		}

		writeJSON(w, http.StatusCreated, deduction)
	}
}

// DeductionDeleteHandler handles DELETE /api/workhours/day/deduction/{id}.
func DeductionDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		deductionID, err := parseID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid deduction id"})
			return
		}

		if err := DeleteDeduction(db, deductionID, user.ID); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "deduction not found"})
			return
		} else if err != nil {
			log.Printf("workhours: delete deduction: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete deduction"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// PresetsListHandler handles GET /api/workhours/presets.
func PresetsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		presets, err := ListPresets(db, user.ID)
		if err != nil {
			log.Printf("workhours: list presets: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load presets"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"presets": presets})
	}
}

// PresetCreateHandler handles POST /api/workhours/presets.
func PresetCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Name           string `json:"name"`
			DefaultMinutes int    `json:"default_minutes"`
			Icon           string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.DefaultMinutes <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "default_minutes must be positive"})
			return
		}
		if body.Icon == "" {
			body.Icon = "clock"
		}

		preset, err := CreatePreset(db, user.ID, body.Name, body.DefaultMinutes, body.Icon)
		if err != nil {
			log.Printf("workhours: create preset: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create preset"})
			return
		}

		writeJSON(w, http.StatusCreated, preset)
	}
}

// PresetUpdateHandler handles PUT /api/workhours/presets/{id}.
func PresetUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		presetID, err := parseID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid preset id"})
			return
		}

		var body struct {
			Name           string `json:"name"`
			DefaultMinutes int    `json:"default_minutes"`
			Icon           string `json:"icon"`
			Active         bool   `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.DefaultMinutes <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "default_minutes must be positive"})
			return
		}

		updated, err := UpdatePreset(db, presetID, user.ID, body.Name, body.DefaultMinutes, body.Icon, body.Active)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "preset not found"})
			return
		} else if err != nil {
			log.Printf("workhours: update preset: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update preset"})
			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

// PresetDeleteHandler handles DELETE /api/workhours/presets/{id}.
func PresetDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		presetID, err := parseID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid preset id"})
			return
		}

		if err := DeletePreset(db, presetID, user.ID); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "preset not found"})
			return
		} else if err != nil {
			log.Printf("workhours: delete preset: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete preset"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// WeekSummaryHandler handles GET /api/workhours/summary/week?date=YYYY-MM-DD.
// Returns summaries for Mon-Sun of the week containing the given date.
func WeekSummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		dateStr := r.URL.Query().Get("date")
		if dateStr == "" {
			dateStr = time.Now().Format("2006-01-02")
		}
		if !isValidDate(dateStr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}

		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date"})
			return
		}

		// Find Monday of the week.
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7 // treat Sunday as 7
		}
		monday := t.AddDate(0, 0, -(weekday - 1))
		sunday := monday.AddDate(0, 0, 6)

		days, err := ListDaysInRange(db, user.ID, monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
		if err != nil {
			log.Printf("workhours: week summary: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load week"})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		summaries, err := calculateSummaries(days, settings)
		if err != nil {
			log.Printf("workhours: calculate week summaries: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate summaries"})
			return
		}

		flex := CalculateFlexPool(summaries, settings.RoundingMinutes)

		leaveDays, err := ListLeaveDays(db, user.ID, monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
		if err != nil {
			log.Printf("workhours: week leave days: %v", err)
			leaveDays = []LeaveDay{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"week_start":  monday.Format("2006-01-02"),
			"week_end":    sunday.Format("2006-01-02"),
			"days":        days,
			"summaries":   summaries,
			"flex":        flex,
			"leave_days":  leaveDays,
		})
	}
}

// MonthSummaryHandler handles GET /api/workhours/summary/month?month=YYYY-MM.
func MonthSummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		monthStr := r.URL.Query().Get("month")
		if monthStr == "" {
			monthStr = time.Now().Format("2006-01")
		}

		t, err := time.Parse("2006-01", monthStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format, expected YYYY-MM"})
			return
		}

		firstDay := t.Format("2006-01-02")
		lastDay := t.AddDate(0, 1, -1).Format("2006-01-02")

		days, err := ListDaysInRange(db, user.ID, firstDay, lastDay)
		if err != nil {
			log.Printf("workhours: month summary: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load month"})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		summaries, err := calculateSummaries(days, settings)
		if err != nil {
			log.Printf("workhours: calculate month summaries: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate summaries"})
			return
		}

		flex := CalculateFlexPool(summaries, settings.RoundingMinutes)

		leaveDays, err := ListLeaveDays(db, user.ID, firstDay, lastDay)
		if err != nil {
			log.Printf("workhours: month leave days: %v", err)
			leaveDays = []LeaveDay{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"month":      monthStr,
			"days":       days,
			"summaries":  summaries,
			"flex":       flex,
			"leave_days": leaveDays,
		})
	}
}

// FlexPoolHandler handles GET /api/workhours/flex.
// Computes the cumulative flex pool from a start date (the flex_reset_date
// preference) up to today.
func FlexPoolHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		prefs, err := auth.GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("workhours: load prefs for flex: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}

		fromDate := "2000-01-01"
		if v, ok := prefs["work_hours_flex_reset_date"]; ok && v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				fromDate = t.Format("2006-01-02")
			}
		}
		toDate := time.Now().UTC().Format("2006-01-02")

		days, err := ListDaysInRange(db, user.ID, fromDate, toDate)
		if err != nil {
			log.Printf("workhours: flex pool load days: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load days"})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: flex pool load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		summaries, err := calculateSummaries(days, settings)
		if err != nil {
			log.Printf("workhours: flex pool calculate: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate flex pool"})
			return
		}

		flex := CalculateFlexPool(summaries, settings.RoundingMinutes)

		redeemed, err := SumFlexRedemptions(db, user.ID, fromDate)
		if err != nil {
			log.Printf("workhours: flex pool sum redemptions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load redemptions"})
			return
		}
		flex.TotalMinutes -= redeemed
		if flex.TotalMinutes < 0 {
			flex.TotalMinutes = 0
		}
		// Recalculate to_next_interval after subtracting redemptions.
		flex.ToNextInterval = 0
		if flex.TotalMinutes > 0 {
			mod := flex.TotalMinutes % settings.RoundingMinutes
			if mod != 0 {
				flex.ToNextInterval = settings.RoundingMinutes - mod
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"flex":             flex,
			"reset_date":       fromDate,
			"days_in_pool":     len(summaries),
			"rounding_minutes": settings.RoundingMinutes,
		})
	}
}

// FlexResetHandler handles POST /api/workhours/flex/reset.
// Sets the flex_reset_date preference to today, effectively zeroing the pool.
func FlexResetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now().Format("2006-01-02")

		if err := auth.SetPreference(db, user.ID, "work_hours_flex_reset_date", today); err != nil {
			log.Printf("workhours: flex reset: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reset flex pool"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"reset_date": today})
	}
}

// FlexRedeemHandler handles POST /api/workhours/flex/redeem.
// Redeems one rounding interval of flex time, deducting it from the pool.
// Accepts an optional JSON body: {"date": "YYYY-MM-DD"} to specify which day
// receives the redeemed time. Defaults to the server's current date.
func FlexRedeemHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Parse optional date from request body.
		targetDate := time.Now().Format("2006-01-02")
		if r.Body != nil {
			var body struct {
				Date string `json:"date"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && isValidDate(body.Date) {
				targetDate = body.Date
			}
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: flex redeem load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		rounding := settings.RoundingMinutes
		if rounding <= 0 {
			rounding = 30
		}

		// Compute current flex pool up to and including targetDate.
		prefs, err := auth.GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("workhours: flex redeem load prefs: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}

		fromDate := "2000-01-01"
		if v, ok := prefs["work_hours_flex_reset_date"]; ok && v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				fromDate = t.Format("2006-01-02")
			}
		}

		days, err := ListDaysInRange(db, user.ID, fromDate, targetDate)
		if err != nil {
			log.Printf("workhours: flex redeem load days: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load days"})
			return
		}

		summaries, err := calculateSummaries(days, settings)
		if err != nil {
			log.Printf("workhours: flex redeem calculate: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate flex pool"})
			return
		}

		flex := CalculateFlexPool(summaries, settings.RoundingMinutes)

		// Use a serializable transaction to atomically check balance and insert,
		// preventing double-spend from concurrent requests.
		tx, err := db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			log.Printf("workhours: flex redeem begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
			return
		}
		defer tx.Rollback()

		// Re-check balance inside the transaction.
		var totalRedeemed sql.NullInt64
		err = tx.QueryRowContext(r.Context(),
			"SELECT SUM(minutes) FROM work_flex_redemptions WHERE user_id = ? AND date >= ?",
			user.ID, fromDate,
		).Scan(&totalRedeemed)
		if err != nil {
			log.Printf("workhours: flex redeem sum in tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load redemptions"})
			return
		}
		redeemed := int(totalRedeemed.Int64)

		available := flex.TotalMinutes - redeemed
		if available < rounding {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "insufficient flex pool balance"})
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		res, err := tx.ExecContext(r.Context(),
			"INSERT INTO work_flex_redemptions (user_id, date, minutes, created_at) VALUES (?, ?, ?, ?)",
			user.ID, targetDate, rounding, now,
		)
		if err != nil {
			log.Printf("workhours: flex redeem insert: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create redemption"})
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			log.Printf("workhours: flex redeem last insert id: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create redemption"})
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("workhours: flex redeem commit: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit redemption"})
			return
		}

		redemption := &FlexRedemption{
			ID:        id,
			UserID:    user.ID,
			Date:      targetDate,
			Minutes:   rounding,
			CreatedAt: now,
		}

		// Return updated pool.
		newTotal := available - rounding
		toNext := 0
		if newTotal > 0 {
			mod := newTotal % rounding
			if mod != 0 {
				toNext = rounding - mod
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"redemption": redemption,
			"flex": FlexPoolResult{
				TotalMinutes:   newTotal,
				ToNextInterval: toNext,
			},
		})
	}
}

// LeaveDayListHandler handles GET /api/workhours/leave?year=YYYY.
// Returns all leave days for the given year.
func LeaveDayListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		yearStr := r.URL.Query().Get("year")
		if yearStr == "" {
			yearStr = time.Now().Format("2006")
		}
		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 2000 || year > 2100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
			return
		}

		fromDate := fmt.Sprintf("%04d-01-01", year)
		toDate := fmt.Sprintf("%04d-12-31", year)
		days, err := ListLeaveDays(db, user.ID, fromDate, toDate)
		if err != nil {
			log.Printf("workhours: list leave days: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load leave days"})
			return
		}

		prefs, _ := auth.GetPreferences(db, user.ID)
		allowance := 25
		if v, ok := prefs["work_hours_vacation_allowance"]; ok && v != "" {
			if n, err2 := strconv.Atoi(v); err2 == nil && n > 0 {
				allowance = n
			}
		}

		balance, err := GetLeaveBalance(db, user.ID, year, allowance)
		if err != nil {
			log.Printf("workhours: leave balance: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate leave balance"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"leave_days": days,
			"balance":    balance,
		})
	}
}

// LeaveDayPutHandler handles PUT /api/workhours/leave.
// Creates or updates a leave day record.
func LeaveDayPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Date      string `json:"date"`
			LeaveType string `json:"leave_type"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Date == "" {
			body.Date = time.Now().Format("2006-01-02")
		}
		if !isValidDate(body.Date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}
		lt := LeaveType(body.LeaveType)
		if !validLeaveTypes[lt] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "leave_type must be vacation, sick, personal, or public_holiday"})
			return
		}

		ld, err := UpsertLeaveDay(db, user.ID, body.Date, lt, strings.TrimSpace(body.Note))
		if err != nil {
			log.Printf("workhours: upsert leave day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save leave day"})
			return
		}

		writeJSON(w, http.StatusOK, ld)
	}
}

// LeaveDayDeleteHandler handles DELETE /api/workhours/leave?date=YYYY-MM-DD.
func LeaveDayDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		date := r.URL.Query().Get("date")
		if date == "" || !isValidDate(date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}

		if err := DeleteLeaveDay(db, user.ID, date); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "leave day not found"})
			return
		} else if err != nil {
			log.Printf("workhours: delete leave day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete leave day"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// LeaveBalanceHandler handles GET /api/workhours/leave/balance?year=YYYY.
func LeaveBalanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		yearStr := r.URL.Query().Get("year")
		if yearStr == "" {
			yearStr = time.Now().Format("2006")
		}
		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 2000 || year > 2100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
			return
		}

		prefs, _ := auth.GetPreferences(db, user.ID)
		allowance := 25
		if v, ok := prefs["work_hours_vacation_allowance"]; ok && v != "" {
			if n, err2 := strconv.Atoi(v); err2 == nil && n > 0 {
				allowance = n
			}
		}

		balance, err := GetLeaveBalance(db, user.ID, year, allowance)
		if err != nil {
			log.Printf("workhours: leave balance: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate leave balance"})
			return
		}
		writeJSON(w, http.StatusOK, balance)
	}
}

// PunchInHandler handles POST /api/workhours/punch-in.
// Records a punch-in start time, persisting it so the UI survives page reloads.
func PunchInHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Date      string `json:"date"`
			StartTime string `json:"start_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Date == "" {
			body.Date = time.Now().Format("2006-01-02")
		}
		if !isValidDate(body.Date) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}
		if err := ValidateHHMM(body.StartTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start_time: " + err.Error()})
			return
		}

		session, err := CreateOpenSession(db, user.ID, body.Date, body.StartTime)
		if err != nil {
			log.Printf("workhours: punch in: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record punch-in"})
			return
		}

		writeJSON(w, http.StatusCreated, session)
	}
}

// PunchEditHandler handles PUT /api/workhours/punch/edit.
// Updates the start_time of the current open punch-in session.
func PunchEditHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			StartTime string `json:"start_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := ValidateHHMM(body.StartTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start_time: " + err.Error()})
			return
		}

		// Load the open session first so we can validate against its date.
		open, err := GetOpenSession(db, user.ID)
		if err != nil {
			log.Printf("workhours: edit punch start_time: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load punch session"})
			return
		}
		if open == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active punch-in session"})
			return
		}

		// Only reject future times when the open session is for today.
		// Sessions punched in on a different date should allow any valid HH:MM.
		now := time.Now()
		if open.Date == now.Format("2006-01-02") {
			parsed, _ := time.Parse("15:04", body.StartTime)
			sessionStart := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
			if sessionStart.After(now) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_time cannot be in the future"})
				return
			}
		}

		session, err := UpdateOpenSessionStartTime(db, user.ID, body.StartTime)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active punch-in session"})
			return
		}
		if err != nil {
			log.Printf("workhours: edit punch start_time: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update start time"})
			return
		}

		writeJSON(w, http.StatusOK, session)
	}
}

// GetPunchSessionHandler handles GET /api/workhours/punch-session.
// Returns the current open punch-in session, or null if none is in progress.
func GetPunchSessionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		session, err := GetOpenSession(db, user.ID)
		if err != nil {
			log.Printf("workhours: get punch session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load punch session"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"session": session})
	}
}

// DeletePunchSessionHandler handles DELETE /api/workhours/punch-session.
// Cancels the in-progress punch-in without saving a completed work session.
func DeletePunchSessionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if err := DeleteOpenSession(db, user.ID); err != nil {
			log.Printf("workhours: cancel punch: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to cancel punch-in"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// PunchOutHandler handles POST /api/workhours/punch-out.
// Closes the open punch-in session, creating a completed work session record.
func PunchOutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			EndTime string `json:"end_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := ValidateHHMM(body.EndTime); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end_time: " + err.Error()})
			return
		}

		open, err := GetOpenSession(db, user.ID)
		if err != nil {
			log.Printf("workhours: punch out get session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load punch session"})
			return
		}
		if open == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active punch-in session"})
			return
		}

		startParsed, err := time.Parse("15:04", open.StartTime)
		if err != nil {
			log.Printf("workhours: punch out parse stored start_time %q: %v", open.StartTime, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stored start_time is invalid"})
			return
		}

		endParsed, err := time.Parse("15:04", body.EndTime)
		if err != nil {
			log.Printf("workhours: punch out parse end_time %q: %v", body.EndTime, err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end_time"})
			return
		}
		if !endParsed.After(startParsed) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "end_time must be after start_time"})
			return
		}

		// Get the existing day if any, to preserve notes and lunch settings.
		existing, err := GetDay(db, user.ID, open.Date)
		if err != nil {
			log.Printf("workhours: punch out get day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load work day"})
			return
		}

		var day *WorkDay
		if existing != nil {
			day = existing
		} else {
			day, err = UpsertDay(db, user.ID, open.Date, false, "")
			if err != nil {
				log.Printf("workhours: punch out ensure day: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create work day"})
				return
			}
		}

		sortOrder := len(day.Sessions)
		if _, err := AddSession(db, day.ID, user.ID, open.StartTime, body.EndTime, sortOrder, false); err != nil {
			log.Printf("workhours: punch out add session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save session"})
			return
		}

		if err := DeleteOpenSession(db, user.ID); err != nil {
			log.Printf("workhours: punch out delete open session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to close open session"})
			return
		}

		// Return the updated day so the client can refresh its view.
		updatedDay, err := GetDay(db, user.ID, open.Date)
		if err != nil {
			log.Printf("workhours: punch out reload day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload day"})
			return
		}

		settings, err := loadSettings(db, user.ID)
		if err != nil {
			log.Printf("workhours: punch out load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
			return
		}

		summary, err := CalculateDay(*updatedDay, settings)
		if err != nil {
			log.Printf("workhours: punch out calculate day: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate summary"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"day":     updatedDay,
			"summary": summary,
			"date":    open.Date,
		})
	}
}

// calculateSummaries computes DaySummary for each WorkDay.
func calculateSummaries(days []WorkDay, settings UserSettings) ([]DaySummary, error) {
	summaries := make([]DaySummary, 0, len(days))
	for _, d := range days {
		s, err := CalculateDay(d, settings)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// parseID parses a path parameter as an int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// isValidDate returns true if the string is a valid YYYY-MM-DD date.
func isValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
