package lactate

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Test represents a lactate step test with its protocol configuration.
type Test struct {
	ID                int64   `json:"id"`
	UserID            int64   `json:"user_id"`
	Date              string  `json:"date"`
	Comment           string  `json:"comment"`
	ProtocolType      string  `json:"protocol_type"`
	WarmupDurationMin int     `json:"warmup_duration_min"`
	StageDurationMin  int     `json:"stage_duration_min"`
	StartSpeedKmh     float64 `json:"start_speed_kmh"`
	SpeedIncrementKmh float64 `json:"speed_increment_kmh"`
	Stages            []Stage `json:"stages"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// Stage represents a single step in a lactate test.
type Stage struct {
	ID           int64   `json:"id"`
	TestID       int64   `json:"test_id"`
	StageNumber  int     `json:"stage_number"`
	SpeedKmh     float64 `json:"speed_kmh"`
	LactateMmol  float64 `json:"lactate_mmol"`
	HeartRateBpm int     `json:"heart_rate_bpm"`
	RPE          *int    `json:"rpe"`
	Notes        string  `json:"notes"`
}

// List returns all lactate tests for a user, ordered by date descending.
// Stages are not included; use GetByID for full test details.
func List(db *sql.DB, userID int64) ([]Test, error) {
	rows, err := db.Query(`
		SELECT id, user_id, date, comment, protocol_type,
		       warmup_duration_min, stage_duration_min,
		       start_speed_kmh, speed_increment_kmh,
		       created_at, updated_at
		FROM lactate_tests
		WHERE user_id = ?
		ORDER BY date DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tests []Test
	for rows.Next() {
		var t Test
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Date, &t.Comment, &t.ProtocolType,
			&t.WarmupDurationMin, &t.StageDurationMin,
			&t.StartSpeedKmh, &t.SpeedIncrementKmh,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if t.Comment, err = encryption.DecryptField(t.Comment); err != nil {
			return nil, fmt.Errorf("decrypt comment: %w", err)
		}
		t.Stages = []Stage{}
		tests = append(tests, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tests == nil {
		tests = []Test{}
	}
	return tests, nil
}

// GetByID returns a single lactate test with all its stages.
func GetByID(db *sql.DB, id, userID int64) (*Test, error) {
	var t Test
	err := db.QueryRow(`
		SELECT id, user_id, date, comment, protocol_type,
		       warmup_duration_min, stage_duration_min,
		       start_speed_kmh, speed_increment_kmh,
		       created_at, updated_at
		FROM lactate_tests
		WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(
		&t.ID, &t.UserID, &t.Date, &t.Comment, &t.ProtocolType,
		&t.WarmupDurationMin, &t.StageDurationMin,
		&t.StartSpeedKmh, &t.SpeedIncrementKmh,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if t.Comment, err = encryption.DecryptField(t.Comment); err != nil {
		return nil, fmt.Errorf("decrypt comment: %w", err)
	}

	stages, err := getStages(db, t.ID)
	if err != nil {
		return nil, fmt.Errorf("get stages: %w", err)
	}
	t.Stages = stages

	return &t, nil
}

// Create inserts a new lactate test with its stages.
func Create(db *sql.DB, userID int64, t *Test) (*Test, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encComment, err := encryption.EncryptField(t.Comment)
	if err != nil {
		return nil, fmt.Errorf("encrypt comment: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(`
		INSERT INTO lactate_tests (user_id, date, comment, protocol_type,
		       warmup_duration_min, stage_duration_min,
		       start_speed_kmh, speed_increment_kmh,
		       created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, t.Date, encComment, t.ProtocolType,
		t.WarmupDurationMin, t.StageDurationMin,
		t.StartSpeedKmh, t.SpeedIncrementKmh,
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert test: %w", err)
	}

	testID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	if err := insertStages(tx, testID, t.Stages); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetByID(db, testID, userID)
}

// Update modifies an existing lactate test and replaces all its stages.
func Update(db *sql.DB, id, userID int64, t *Test) (*Test, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encComment, err := encryption.EncryptField(t.Comment)
	if err != nil {
		return nil, fmt.Errorf("encrypt comment: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(`
		UPDATE lactate_tests
		SET date = ?, comment = ?, protocol_type = ?,
		    warmup_duration_min = ?, stage_duration_min = ?,
		    start_speed_kmh = ?, speed_increment_kmh = ?,
		    updated_at = ?
		WHERE id = ? AND user_id = ?`,
		t.Date, encComment, t.ProtocolType,
		t.WarmupDurationMin, t.StageDurationMin,
		t.StartSpeedKmh, t.SpeedIncrementKmh,
		now, id, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("update test: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	// Replace all stages.
	if _, err := tx.Exec("DELETE FROM lactate_test_stages WHERE test_id = ?", id); err != nil {
		return nil, fmt.Errorf("clear stages: %w", err)
	}
	if err := insertStages(tx, id, t.Stages); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetByID(db, id, userID)
}

// Delete removes a lactate test owned by the given user.
func Delete(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM lactate_tests WHERE id = ? AND user_id = ?", id, userID)
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

// insertStages inserts stages within an existing transaction.
func insertStages(tx *sql.Tx, testID int64, stages []Stage) error {
	for _, s := range stages {
		encNotes, err := encryption.EncryptField(s.Notes)
		if err != nil {
			return fmt.Errorf("encrypt stage %d notes: %w", s.StageNumber, err)
		}
		_, err = tx.Exec(`
			INSERT INTO lactate_test_stages (test_id, stage_number, speed_kmh,
			       lactate_mmol, heart_rate_bpm, rpe, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			testID, s.StageNumber, s.SpeedKmh,
			s.LactateMmol, s.HeartRateBpm, s.RPE, encNotes,
		)
		if err != nil {
			return fmt.Errorf("insert stage %d: %w", s.StageNumber, err)
		}
	}
	return nil
}

// getStages returns all stages for a test, ordered by stage number.
func getStages(db *sql.DB, testID int64) ([]Stage, error) {
	rows, err := db.Query(`
		SELECT id, test_id, stage_number, speed_kmh, lactate_mmol,
		       heart_rate_bpm, rpe, notes
		FROM lactate_test_stages
		WHERE test_id = ?
		ORDER BY stage_number`,
		testID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []Stage
	for rows.Next() {
		var s Stage
		if err := rows.Scan(
			&s.ID, &s.TestID, &s.StageNumber, &s.SpeedKmh, &s.LactateMmol,
			&s.HeartRateBpm, &s.RPE, &s.Notes,
		); err != nil {
			return nil, err
		}
		if s.Notes, err = encryption.DecryptField(s.Notes); err != nil {
			return nil, fmt.Errorf("decrypt stage notes: %w", err)
		}
		stages = append(stages, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if stages == nil {
		stages = []Stage{}
	}
	return stages, nil
}
