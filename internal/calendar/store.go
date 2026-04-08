package calendar

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// UpsertEvents inserts or replaces cached events for a user.
// Sensitive fields (title, description, location) are encrypted at rest.
func UpsertEvents(db *sql.DB, userID int64, events []Event) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
		INSERT INTO calendar_events (id, user_id, calendar_id, title, description, location, start_time, end_time, all_day, status, color, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, user_id, calendar_id) DO UPDATE SET
			title       = excluded.title,
			description = excluded.description,
			location    = excluded.location,
			start_time  = excluded.start_time,
			end_time    = excluded.end_time,
			all_day     = excluded.all_day,
			status      = excluded.status,
			color       = excluded.color,
			updated_at  = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range events {
		encTitle, encErr := encryption.EncryptField(e.Title)
		if encErr != nil {
			return encErr
		}
		encDesc, encErr := encryption.EncryptField(e.Description)
		if encErr != nil {
			return encErr
		}
		encLoc, encErr := encryption.EncryptField(e.Location)
		if encErr != nil {
			return encErr
		}

		allDay := 0
		if e.AllDay {
			allDay = 1
		}

		if _, execErr := stmt.Exec(e.ID, userID, e.CalendarID, encTitle, encDesc, encLoc,
			e.StartTime, e.EndTime, allDay, e.Status, e.Color, now); execErr != nil {
			return execErr
		}
	}

	return tx.Commit()
}

// DeleteEvents removes specific events by ID for a user and calendar.
func DeleteEvents(db *sql.DB, userID int64, calendarID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`DELETE FROM calendar_events WHERE id = ? AND user_id = ? AND calendar_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range eventIDs {
		if _, execErr := stmt.Exec(id, userID, calendarID); execErr != nil {
			return execErr
		}
	}
	return tx.Commit()
}

// QueryEvents returns cached events for a user within the given time range.
// Only events from the specified calendarIDs are returned. If calendarIDs is
// empty, events from all calendars are returned.
func QueryEvents(db *sql.DB, userID int64, calendarIDs []string, start, end string) ([]Event, error) {
	var rows *sql.Rows
	var err error

	if len(calendarIDs) == 0 {
		rows, err = db.Query(`
			SELECT id, calendar_id, title, description, location, start_time, end_time, all_day, status, color
			FROM calendar_events
			WHERE user_id = ? AND end_time >= ? AND start_time <= ?
			ORDER BY start_time`,
			userID, start, end)
	} else {
		// Build placeholders for IN clause.
		query := `
			SELECT id, calendar_id, title, description, location, start_time, end_time, all_day, status, color
			FROM calendar_events
			WHERE user_id = ? AND end_time >= ? AND start_time <= ? AND calendar_id IN (`
		args := make([]any, 0, 3+len(calendarIDs))
		args = append(args, userID, start, end)
		for i, cid := range calendarIDs {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, cid)
		}
		query += `) ORDER BY start_time`
		rows, err = db.Query(query, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var allDay int
		var encTitle, encDesc, encLoc string
		if err := rows.Scan(&e.ID, &e.CalendarID, &encTitle, &encDesc, &encLoc,
			&e.StartTime, &e.EndTime, &allDay, &e.Status, &e.Color); err != nil {
			return nil, err
		}
		e.AllDay = allDay != 0

		if e.Title, err = encryption.DecryptField(encTitle); err != nil {
			return nil, fmt.Errorf("decrypt event title %s: %w", e.ID, err)
		}
		if e.Description, err = encryption.DecryptField(encDesc); err != nil {
			return nil, fmt.Errorf("decrypt event description %s: %w", e.ID, err)
		}
		if e.Location, err = encryption.DecryptField(encLoc); err != nil {
			return nil, fmt.Errorf("decrypt event location %s: %w", e.ID, err)
		}

		events = append(events, e)
	}
	return events, rows.Err()
}

// DeleteAllEvents removes all cached events for a user.
func DeleteAllEvents(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM calendar_events WHERE user_id = ?`, userID)
	return err
}

// SaveSyncToken persists the incremental sync token for a calendar.
func SaveSyncToken(db *sql.DB, userID int64, calendarID, syncToken string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO calendar_sync_state (user_id, calendar_id, sync_token, synced_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, calendar_id) DO UPDATE SET
			sync_token = excluded.sync_token,
			synced_at  = excluded.synced_at`,
		userID, calendarID, syncToken, now)
	return err
}

// LoadSyncToken returns the stored sync token for a calendar, or "" if none.
func LoadSyncToken(db *sql.DB, userID int64, calendarID string) (string, error) {
	var token string
	err := db.QueryRow(
		`SELECT sync_token FROM calendar_sync_state WHERE user_id = ? AND calendar_id = ?`,
		userID, calendarID,
	).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return token, err
}

// ClearSyncState removes all sync tokens for a user.
func ClearSyncState(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM calendar_sync_state WHERE user_id = ?`, userID)
	return err
}
