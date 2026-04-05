package stride

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Race represents an upcoming race in the user's race calendar.
type Race struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`       // encrypted at rest
	Date       string  `json:"date"`       // YYYY-MM-DD
	DistanceM  float64 `json:"distance_m"` // meters
	TargetTime *int    `json:"target_time"` // seconds, nullable
	Priority   string  `json:"priority"`   // A, B, or C
	Notes      string  `json:"notes"`      // encrypted at rest
	ResultTime *int    `json:"result_time"` // seconds, nullable
	CreatedAt  string  `json:"created_at"`
}

// Note represents a short free-text note from the user that feeds into the
// next Stride plan generation.
type Note struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	PlanID    *int64 `json:"plan_id"` // nullable — linked to plan when created during a plan week
	Content   string `json:"content"` // encrypted at rest
	CreatedAt string `json:"created_at"`
}

// ListRaces returns all races for a user ordered by date ascending.
func ListRaces(db *sql.DB, userID int64) ([]Race, error) {
	rows, err := db.Query(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE user_id = ?
		ORDER BY date ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var races []Race
	for rows.Next() {
		var r Race
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
			&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if r.Name, err = encryption.DecryptField(r.Name); err != nil {
			return nil, fmt.Errorf("decrypt race name: %w", err)
		}
		if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
			return nil, fmt.Errorf("decrypt race notes: %w", err)
		}
		races = append(races, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if races == nil {
		races = []Race{}
	}
	return races, nil
}

// CreateRace inserts a new race into the race calendar.
func CreateRace(db *sql.DB, userID int64, name, date string, distanceM float64, targetTime *int, priority, notes string) (*Race, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt race name: %w", err)
	}
	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt race notes: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO stride_races (user_id, name, date, distance_m, target_time, priority, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, encName, date, distanceM, targetTime, priority, encNotes, now)
	if err != nil {
		return nil, fmt.Errorf("insert race: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return GetRaceByID(db, id, userID)
}

// GetRaceByID returns a single race by ID, scoped to the given user.
func GetRaceByID(db *sql.DB, id, userID int64) (*Race, error) {
	var r Race
	err := db.QueryRow(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE id = ? AND user_id = ?
	`, id, userID).Scan(
		&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
		&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if r.Name, err = encryption.DecryptField(r.Name); err != nil {
		return nil, fmt.Errorf("decrypt race name: %w", err)
	}
	if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
		return nil, fmt.Errorf("decrypt race notes: %w", err)
	}
	return &r, nil
}

// UpdateRace updates an existing race owned by the given user.
func UpdateRace(db *sql.DB, id, userID int64, name, date string, distanceM float64, targetTime *int, priority, notes string, resultTime *int) (*Race, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt race name: %w", err)
	}
	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt race notes: %w", err)
	}

	res, err := db.Exec(`
		UPDATE stride_races
		SET name = ?, date = ?, distance_m = ?, target_time = ?, priority = ?, notes = ?, result_time = ?
		WHERE id = ? AND user_id = ?
	`, encName, date, distanceM, targetTime, priority, encNotes, resultTime, id, userID)
	if err != nil {
		return nil, fmt.Errorf("update race: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	return GetRaceByID(db, id, userID)
}

// DeleteRace removes a race owned by the given user.
func DeleteRace(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM stride_races WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListNotes returns notes for a user, optionally filtered by plan_id.
// When planID is nil, all notes for the user are returned.
func ListNotes(db *sql.DB, userID int64, planID *int64) ([]Note, error) {
	query := `
		SELECT id, user_id, plan_id, content, created_at
		FROM stride_notes
		WHERE user_id = ?`
	args := []any{userID}

	if planID != nil {
		query += ` AND plan_id = ?`
		args = append(args, *planID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.CreatedAt); err != nil {
			return nil, err
		}
		if n.Content, err = encryption.DecryptField(n.Content); err != nil {
			return nil, fmt.Errorf("decrypt note content: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

// CreateNote inserts a new note.
func CreateNote(db *sql.DB, userID int64, planID *int64, content string) (*Note, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encContent, err := encryption.EncryptField(content)
	if err != nil {
		return nil, fmt.Errorf("encrypt note content: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO stride_notes (user_id, plan_id, content, created_at)
		VALUES (?, ?, ?, ?)
	`, userID, planID, encContent, now)
	if err != nil {
		return nil, fmt.Errorf("insert note: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return getNoteByID(db, id, userID)
}

// getNoteByID returns a single note by ID, scoped to the given user.
func getNoteByID(db *sql.DB, id, userID int64) (*Note, error) {
	var n Note
	err := db.QueryRow(`
		SELECT id, user_id, plan_id, content, created_at
		FROM stride_notes
		WHERE id = ? AND user_id = ?
	`, id, userID).Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	if n.Content, err = encryption.DecryptField(n.Content); err != nil {
		return nil, fmt.Errorf("decrypt note content: %w", err)
	}
	return &n, nil
}

// DeleteNote removes a note owned by the given user.
func DeleteNote(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM stride_notes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
