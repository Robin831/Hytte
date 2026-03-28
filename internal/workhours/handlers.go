package workhours

import (
	"database/sql"
	"encoding/json"
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
			Date  string `json:"date"`
			Lunch bool   `json:"lunch"`
			Notes string `json:"notes"`
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

		day, err := UpsertDay(db, user.ID, body.Date, body.Lunch, body.Notes)
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
			DayID     int64  `json:"day_id"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
			SortOrder int    `json:"sort_order"`
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

		session, err := AddSession(db, body.DayID, user.ID, body.StartTime, body.EndTime, body.SortOrder)
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
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
			SortOrder int    `json:"sort_order"`
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

		if err := UpdateSession(db, sessionID, user.ID, body.StartTime, body.EndTime, body.SortOrder); err == sql.ErrNoRows {
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

		if err := UpdatePreset(db, presetID, user.ID, body.Name, body.DefaultMinutes, body.Icon, body.Active); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "preset not found"})
			return
		} else if err != nil {
			log.Printf("workhours: update preset: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update preset"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
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
		writeJSON(w, http.StatusOK, map[string]any{
			"week_start": monday.Format("2006-01-02"),
			"week_end":   sunday.Format("2006-01-02"),
			"days":       days,
			"summaries":  summaries,
			"flex":       flex,
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
		writeJSON(w, http.StatusOK, map[string]any{
			"month":     monthStr,
			"days":      days,
			"summaries": summaries,
			"flex":      flex,
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
			fromDate = v
		}
		toDate := time.Now().Format("2006-01-02")

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
		writeJSON(w, http.StatusOK, map[string]any{
			"flex":           flex,
			"reset_date":     fromDate,
			"days_in_pool":   len(summaries),
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
