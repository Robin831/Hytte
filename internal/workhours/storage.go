package workhours

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// GetDay returns the work day for a user on the given date (YYYY-MM-DD),
// including its sessions and deductions. Returns nil, nil if no entry exists.
func GetDay(db *sql.DB, userID int64, date string) (*WorkDay, error) {
	var day WorkDay
	var encNotes string
	err := db.QueryRow(
		`SELECT id, user_id, date, lunch, notes, created_at
		 FROM work_days WHERE user_id = ? AND date = ?`,
		userID, date,
	).Scan(&day.ID, &day.UserID, &day.Date, &day.Lunch, &encNotes, &day.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	notes, err := encryption.DecryptField(encNotes)
	if err != nil {
		log.Printf("workhours: decrypt work_days.notes id=%d: %v", day.ID, err)
		notes = ""
	}
	day.Notes = notes

	sessions, err := getSessions(db, day.ID)
	if err != nil {
		return nil, err
	}
	day.Sessions = sessions

	deductions, err := getDeductions(db, day.ID)
	if err != nil {
		return nil, err
	}
	day.Deductions = deductions

	return &day, nil
}

// UpsertDay creates or updates the work day record for (userID, date).
// Returns the resulting day with sessions and deductions populated.
func UpsertDay(db *sql.DB, userID int64, date string, lunch bool, notes string) (*WorkDay, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	lunchInt := 0
	if lunch {
		lunchInt = 1
	}

	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt work_days.notes: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO work_days (user_id, date, lunch, notes, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, date) DO UPDATE SET
			lunch = excluded.lunch,
			notes = excluded.notes
	`, userID, date, lunchInt, encNotes, now)
	if err != nil {
		return nil, fmt.Errorf("upsert work_days: %w", err)
	}

	return GetDay(db, userID, date)
}

// DeleteDay removes the work day and all its sessions and deductions
// (cascade handles child rows).
func DeleteDay(db *sql.DB, userID int64, date string) error {
	res, err := db.Exec("DELETE FROM work_days WHERE user_id = ? AND date = ?", userID, date)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddSession adds a new time session to an existing work day. The day must
// belong to the given userID. isInternal marks company meetings/admin time.
func AddSession(db *sql.DB, dayID, userID int64, startTime, endTime string, sortOrder int, isInternal bool) (*WorkSession, error) {
	// Verify ownership.
	if err := verifyDayOwnership(db, dayID, userID); err != nil {
		return nil, err
	}

	isInternalInt := 0
	if isInternal {
		isInternalInt = 1
	}
	res, err := db.Exec(`
		INSERT INTO work_sessions (day_id, start_time, end_time, sort_order, is_internal)
		VALUES (?, ?, ?, ?, ?)
	`, dayID, startTime, endTime, sortOrder, isInternalInt)
	if err != nil {
		return nil, fmt.Errorf("insert work_sessions: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &WorkSession{
		ID:         id,
		DayID:      dayID,
		StartTime:  startTime,
		EndTime:    endTime,
		SortOrder:  sortOrder,
		IsInternal: isInternal,
	}, nil
}

// UpdateSession modifies an existing session. The session must belong to a
// day owned by the given userID. isInternal marks company meetings/admin time.
func UpdateSession(db *sql.DB, sessionID, userID int64, startTime, endTime string, sortOrder int, isInternal bool) error {
	isInternalInt := 0
	if isInternal {
		isInternalInt = 1
	}
	res, err := db.Exec(`
		UPDATE work_sessions SET start_time = ?, end_time = ?, sort_order = ?, is_internal = ?
		WHERE id = ? AND day_id IN (SELECT id FROM work_days WHERE user_id = ?)
	`, startTime, endTime, sortOrder, isInternalInt, sessionID, userID)
	if err != nil {
		return fmt.Errorf("update work_sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSession removes a session. The session must belong to a day owned by
// the given userID.
func DeleteSession(db *sql.DB, sessionID, userID int64) error {
	res, err := db.Exec(`
		DELETE FROM work_sessions
		WHERE id = ? AND day_id IN (SELECT id FROM work_days WHERE user_id = ?)
	`, sessionID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddDeduction adds a custom deduction to a work day.
func AddDeduction(db *sql.DB, dayID, userID int64, name string, minutes int, presetID *int64) (*WorkDeduction, error) {
	if err := verifyDayOwnership(db, dayID, userID); err != nil {
		return nil, err
	}

	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt work_deductions.name: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO work_deductions (day_id, name, minutes, preset_id)
		VALUES (?, ?, ?, ?)
	`, dayID, encName, minutes, presetID)
	if err != nil {
		return nil, fmt.Errorf("insert work_deductions: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &WorkDeduction{
		ID:       id,
		DayID:    dayID,
		Name:     name,
		Minutes:  minutes,
		PresetID: presetID,
	}, nil
}

// DeleteDeduction removes a deduction from a work day. The deduction must
// belong to a day owned by the given userID.
func DeleteDeduction(db *sql.DB, deductionID, userID int64) error {
	res, err := db.Exec(`
		DELETE FROM work_deductions
		WHERE id = ? AND day_id IN (SELECT id FROM work_days WHERE user_id = ?)
	`, deductionID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListPresets returns all active deduction presets for a user.
func ListPresets(db *sql.DB, userID int64) ([]WorkDeductionPreset, error) {
	rows, err := db.Query(`
		SELECT id, user_id, name, default_minutes, icon, sort_order, active
		FROM work_deduction_presets
		WHERE user_id = ? AND active = 1
		ORDER BY sort_order, id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var presets []WorkDeductionPreset
	for rows.Next() {
		var p WorkDeductionPreset
		var encName string
		var activeInt int
		if err := rows.Scan(&p.ID, &p.UserID, &encName, &p.DefaultMinutes, &p.Icon, &p.SortOrder, &activeInt); err != nil {
			return nil, err
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			log.Printf("workhours: decrypt preset name id=%d: %v", p.ID, err)
			name = ""
		}
		p.Name = name
		p.Active = activeInt != 0
		presets = append(presets, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if presets == nil {
		presets = []WorkDeductionPreset{}
	}
	return presets, nil
}

// CreatePreset adds a new deduction preset for a user.
func CreatePreset(db *sql.DB, userID int64, name string, defaultMinutes int, icon string) (*WorkDeductionPreset, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt work_deduction_presets.name: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO work_deduction_presets (user_id, name, default_minutes, icon)
		VALUES (?, ?, ?, ?)
	`, userID, encName, defaultMinutes, icon)
	if err != nil {
		return nil, fmt.Errorf("insert work_deduction_presets: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &WorkDeductionPreset{
		ID:             id,
		UserID:         userID,
		Name:           name,
		DefaultMinutes: defaultMinutes,
		Icon:           icon,
		SortOrder:      0,
		Active:         true,
	}, nil
}

// UpdatePreset modifies an existing preset owned by the given user and returns
// the updated preset. Returns sql.ErrNoRows if the preset was not found.
func UpdatePreset(db *sql.DB, presetID, userID int64, name string, defaultMinutes int, icon string, active bool) (*WorkDeductionPreset, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt work_deduction_presets.name: %w", err)
	}
	activeInt := 0
	if active {
		activeInt = 1
	}

	res, err := db.Exec(`
		UPDATE work_deduction_presets
		SET name = ?, default_minutes = ?, icon = ?, active = ?
		WHERE id = ? AND user_id = ?
	`, encName, defaultMinutes, icon, activeInt, presetID, userID)
	if err != nil {
		return nil, fmt.Errorf("update work_deduction_presets: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	var p WorkDeductionPreset
	var encStoredName string
	var sortOrder int
	var activeStored int
	err = db.QueryRow(`
		SELECT id, user_id, name, default_minutes, icon, sort_order, active
		FROM work_deduction_presets WHERE id = ? AND user_id = ?
	`, presetID, userID).Scan(&p.ID, &p.UserID, &encStoredName, &p.DefaultMinutes, &p.Icon, &sortOrder, &activeStored)
	if err != nil {
		return nil, fmt.Errorf("select updated work_deduction_preset: %w", err)
	}
	storedName, decErr := encryption.DecryptField(encStoredName)
	if decErr != nil {
		log.Printf("workhours: decrypt preset name after update id=%d: %v", p.ID, decErr)
		storedName = name
	}
	p.Name = storedName
	p.SortOrder = sortOrder
	p.Active = activeStored != 0
	return &p, nil
}

// DeletePreset removes a preset owned by the given user.
func DeletePreset(db *sql.DB, presetID, userID int64) error {
	res, err := db.Exec(
		"DELETE FROM work_deduction_presets WHERE id = ? AND user_id = ?",
		presetID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListDaysInRange returns all work days (with sessions and deductions) for a
// user between fromDate and toDate inclusive (YYYY-MM-DD format).
func ListDaysInRange(db *sql.DB, userID int64, fromDate, toDate string) ([]WorkDay, error) {
	rows, err := db.Query(`
		SELECT id, user_id, date, lunch, notes, created_at
		FROM work_days
		WHERE user_id = ? AND date >= ? AND date <= ?
		ORDER BY date
	`, userID, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []WorkDay
	for rows.Next() {
		var d WorkDay
		var encNotes string
		var lunchInt int
		if err := rows.Scan(&d.ID, &d.UserID, &d.Date, &lunchInt, &encNotes, &d.CreatedAt); err != nil {
			return nil, err
		}
		notes, err := encryption.DecryptField(encNotes)
		if err != nil {
			log.Printf("workhours: decrypt work_days.notes id=%d: %v", d.ID, err)
			notes = ""
		}
		d.Notes = notes
		d.Lunch = lunchInt != 0
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(days) == 0 {
		return []WorkDay{}, nil
	}

	// Collect day IDs for batched queries.
	dayIDs := make([]int64, len(days))
	for i, d := range days {
		dayIDs[i] = d.ID
	}

	// Build "?, ?, ?" placeholder string.
	placeholders := make([]byte, 0, len(dayIDs)*3)
	for i := range dayIDs {
		if i > 0 {
			placeholders = append(placeholders, ',', ' ')
		}
		placeholders = append(placeholders, '?')
	}
	ph := string(placeholders)

	args := make([]any, len(dayIDs))
	for i, id := range dayIDs {
		args[i] = id
	}

	// Batch-load sessions for all days.
	sessionsByDay := make(map[int64][]WorkSession, len(dayIDs))
	sRows, err := db.Query(fmt.Sprintf(`
		SELECT id, day_id, start_time, end_time, sort_order, is_internal
		FROM work_sessions
		WHERE day_id IN (%s)
		ORDER BY day_id, sort_order, id
	`, ph), args...)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()
	for sRows.Next() {
		var s WorkSession
		var isInternalInt int
		if err := sRows.Scan(&s.ID, &s.DayID, &s.StartTime, &s.EndTime, &s.SortOrder, &isInternalInt); err != nil {
			return nil, err
		}
		s.IsInternal = isInternalInt != 0
		sessionsByDay[s.DayID] = append(sessionsByDay[s.DayID], s)
	}
	if err := sRows.Err(); err != nil {
		return nil, err
	}

	// Batch-load deductions for all days.
	deductionsByDay := make(map[int64][]WorkDeduction, len(dayIDs))
	dRows, err := db.Query(fmt.Sprintf(`
		SELECT id, day_id, name, minutes, preset_id
		FROM work_deductions
		WHERE day_id IN (%s)
		ORDER BY day_id, id
	`, ph), args...)
	if err != nil {
		return nil, err
	}
	defer dRows.Close()
	for dRows.Next() {
		var d WorkDeduction
		var encName string
		var presetID sql.NullInt64
		if err := dRows.Scan(&d.ID, &d.DayID, &encName, &d.Minutes, &presetID); err != nil {
			return nil, err
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			log.Printf("workhours: decrypt deduction name id=%d: %v", d.ID, err)
			name = ""
		}
		d.Name = name
		if presetID.Valid {
			id := presetID.Int64
			d.PresetID = &id
		}
		deductionsByDay[d.DayID] = append(deductionsByDay[d.DayID], d)
	}
	if err := dRows.Err(); err != nil {
		return nil, err
	}

	// Populate sessions and deductions for each day from batched results.
	for i := range days {
		if sess, ok := sessionsByDay[days[i].ID]; ok {
			days[i].Sessions = sess
		} else {
			days[i].Sessions = []WorkSession{}
		}
		if deds, ok := deductionsByDay[days[i].ID]; ok {
			days[i].Deductions = deds
		} else {
			days[i].Deductions = []WorkDeduction{}
		}
	}

	return days, nil
}

// getSessions returns all sessions for a work day, ordered by sort_order then id.
func getSessions(db *sql.DB, dayID int64) ([]WorkSession, error) {
	rows, err := db.Query(`
		SELECT id, day_id, start_time, end_time, sort_order, is_internal
		FROM work_sessions
		WHERE day_id = ?
		ORDER BY sort_order, id
	`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []WorkSession
	for rows.Next() {
		var s WorkSession
		var isInternalInt int
		if err := rows.Scan(&s.ID, &s.DayID, &s.StartTime, &s.EndTime, &s.SortOrder, &isInternalInt); err != nil {
			return nil, err
		}
		s.IsInternal = isInternalInt != 0
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if sessions == nil {
		sessions = []WorkSession{}
	}
	return sessions, nil
}

// getDeductions returns all deductions for a work day.
func getDeductions(db *sql.DB, dayID int64) ([]WorkDeduction, error) {
	rows, err := db.Query(`
		SELECT id, day_id, name, minutes, preset_id
		FROM work_deductions
		WHERE day_id = ?
		ORDER BY id
	`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deductions []WorkDeduction
	for rows.Next() {
		var d WorkDeduction
		var encName string
		var presetID sql.NullInt64
		if err := rows.Scan(&d.ID, &d.DayID, &encName, &d.Minutes, &presetID); err != nil {
			return nil, err
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			log.Printf("workhours: decrypt deduction name id=%d: %v", d.ID, err)
			name = ""
		}
		d.Name = name
		if presetID.Valid {
			id := presetID.Int64
			d.PresetID = &id
		}
		deductions = append(deductions, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if deductions == nil {
		deductions = []WorkDeduction{}
	}
	return deductions, nil
}

// UpsertLeaveDay creates or replaces the leave record for (userID, date).
func UpsertLeaveDay(db *sql.DB, userID int64, date string, leaveType LeaveType, note string) (*LeaveDay, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	encNote, err := encryption.EncryptField(note)
	if err != nil {
		return nil, fmt.Errorf("encrypt work_leave_days.note: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO work_leave_days (user_id, date, leave_type, note, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, date) DO UPDATE SET
			leave_type = excluded.leave_type,
			note       = excluded.note
	`, userID, date, string(leaveType), encNote, now)
	if err != nil {
		return nil, fmt.Errorf("upsert work_leave_days: %w", err)
	}

	return GetLeaveDay(db, userID, date)
}

// GetLeaveDay returns the leave record for (userID, date), or nil if none exists.
func GetLeaveDay(db *sql.DB, userID int64, date string) (*LeaveDay, error) {
	var ld LeaveDay
	var encNote string
	err := db.QueryRow(
		`SELECT id, user_id, date, leave_type, note, created_at
		 FROM work_leave_days WHERE user_id = ? AND date = ?`,
		userID, date,
	).Scan(&ld.ID, &ld.UserID, &ld.Date, &ld.LeaveType, &encNote, &ld.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	note, err := encryption.DecryptField(encNote)
	if err != nil {
		log.Printf("workhours: decrypt leave note id=%d: %v", ld.ID, err)
		note = ""
	}
	ld.Note = note
	return &ld, nil
}

// DeleteLeaveDay removes the leave record for (userID, date).
// Returns sql.ErrNoRows if the record does not exist.
func DeleteLeaveDay(db *sql.DB, userID int64, date string) error {
	res, err := db.Exec("DELETE FROM work_leave_days WHERE user_id = ? AND date = ?", userID, date)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListLeaveDays returns all leave records for a user in the given date range (inclusive).
func ListLeaveDays(db *sql.DB, userID int64, fromDate, toDate string) ([]LeaveDay, error) {
	rows, err := db.Query(`
		SELECT id, user_id, date, leave_type, note, created_at
		FROM work_leave_days
		WHERE user_id = ? AND date >= ? AND date <= ?
		ORDER BY date
	`, userID, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []LeaveDay
	for rows.Next() {
		var ld LeaveDay
		var encNote string
		if err := rows.Scan(&ld.ID, &ld.UserID, &ld.Date, &ld.LeaveType, &encNote, &ld.CreatedAt); err != nil {
			return nil, err
		}
		note, err := encryption.DecryptField(encNote)
		if err != nil {
			log.Printf("workhours: decrypt leave note id=%d: %v", ld.ID, err)
			note = ""
		}
		ld.Note = note
		days = append(days, ld)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if days == nil {
		days = []LeaveDay{}
	}
	return days, nil
}

// GetLeaveBalance counts leave days by type for a user in the given year.
func GetLeaveBalance(db *sql.DB, userID int64, year int, vacationAllowance int) (LeaveBalance, error) {
	fromDate := fmt.Sprintf("%04d-01-01", year)
	toDate := fmt.Sprintf("%04d-12-31", year)

	rows, err := db.Query(`
		SELECT leave_type, COUNT(*) FROM work_leave_days
		WHERE user_id = ? AND date >= ? AND date <= ?
		GROUP BY leave_type
	`, userID, fromDate, toDate)
	if err != nil {
		return LeaveBalance{}, err
	}
	defer rows.Close()

	balance := LeaveBalance{
		Year:              year,
		VacationAllowance: vacationAllowance,
	}
	for rows.Next() {
		var lt string
		var count int
		if err := rows.Scan(&lt, &count); err != nil {
			return balance, err
		}
		switch LeaveType(lt) {
		case LeaveTypeVacation:
			balance.VacationUsed = count
		case LeaveTypeSick:
			balance.SickUsed = count
		case LeaveTypePersonal:
			balance.PersonalUsed = count
		case LeaveTypePublicHoliday:
			balance.PublicHolidayUsed = count
		}
	}
	return balance, rows.Err()
}

// CreateOpenSession records a punch-in for the given user, replacing any
// previous open session. Returns the saved session.
func CreateOpenSession(db *sql.DB, userID int64, date, startTime string) (*OpenSession, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO work_open_sessions (user_id, date, start_time, punched_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			date       = excluded.date,
			start_time = excluded.start_time,
			punched_at = excluded.punched_at
	`, userID, date, startTime, now)
	if err != nil {
		return nil, fmt.Errorf("upsert work_open_sessions: %w", err)
	}
	return GetOpenSession(db, userID)
}

// GetOpenSession returns the current open punch-in session for a user, or
// nil if no punch-in is in progress.
func GetOpenSession(db *sql.DB, userID int64) (*OpenSession, error) {
	var s OpenSession
	err := db.QueryRow(`
		SELECT id, user_id, date, start_time, punched_at
		FROM work_open_sessions WHERE user_id = ?
	`, userID).Scan(&s.ID, &s.UserID, &s.Date, &s.StartTime, &s.PunchedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteOpenSession removes the open punch-in session for a user without
// creating a completed work session (i.e., cancel punch-in).
func DeleteOpenSession(db *sql.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM work_open_sessions WHERE user_id = ?", userID)
	return err
}

// verifyDayOwnership returns an error if dayID does not belong to userID.
func verifyDayOwnership(db *sql.DB, dayID, userID int64) error {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM work_days WHERE id = ? AND user_id = ?",
		dayID, userID,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
