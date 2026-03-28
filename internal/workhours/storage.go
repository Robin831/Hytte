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
		notes = encNotes
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
// belong to the given userID.
func AddSession(db *sql.DB, dayID, userID int64, startTime, endTime string, sortOrder int) (*WorkSession, error) {
	// Verify ownership.
	if err := verifyDayOwnership(db, dayID, userID); err != nil {
		return nil, err
	}

	res, err := db.Exec(`
		INSERT INTO work_sessions (day_id, start_time, end_time, sort_order)
		VALUES (?, ?, ?, ?)
	`, dayID, startTime, endTime, sortOrder)
	if err != nil {
		return nil, fmt.Errorf("insert work_sessions: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &WorkSession{
		ID:        id,
		DayID:     dayID,
		StartTime: startTime,
		EndTime:   endTime,
		SortOrder: sortOrder,
	}, nil
}

// UpdateSession modifies an existing session. The session must belong to a
// day owned by the given userID.
func UpdateSession(db *sql.DB, sessionID, userID int64, startTime, endTime string, sortOrder int) error {
	// Verify the session belongs to the user via the day.
	res, err := db.Exec(`
		UPDATE work_sessions SET start_time = ?, end_time = ?, sort_order = ?
		WHERE id = ? AND day_id IN (SELECT id FROM work_days WHERE user_id = ?)
	`, startTime, endTime, sortOrder, sessionID, userID)
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
			name = encName
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

// UpdatePreset modifies an existing preset owned by the given user.
func UpdatePreset(db *sql.DB, presetID, userID int64, name string, defaultMinutes int, icon string, active bool) error {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return fmt.Errorf("encrypt work_deduction_presets.name: %w", err)
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
		return fmt.Errorf("update work_deduction_presets: %w", err)
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
			notes = encNotes
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
		SELECT id, day_id, start_time, end_time, sort_order
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
		if err := sRows.Scan(&s.ID, &s.DayID, &s.StartTime, &s.EndTime, &s.SortOrder); err != nil {
			return nil, err
		}
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
			name = encName
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
		SELECT id, day_id, start_time, end_time, sort_order
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
		if err := rows.Scan(&s.ID, &s.DayID, &s.StartTime, &s.EndTime, &s.SortOrder); err != nil {
			return nil, err
		}
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
			name = encName
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
