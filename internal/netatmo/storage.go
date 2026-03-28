package netatmo

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

const retentionDays = 7

// Reading holds a single persisted sensor value.
type Reading struct {
	Timestamp  time.Time
	ModuleType string
	Metric     string
	Value      float64
}

// StoreReadings flattens a ModuleReadings snapshot into individual rows in the
// netatmo_readings table, then removes rows older than 7 days.
func StoreReadings(db *sql.DB, userID int64, readings ModuleReadings) error {
	ts := readings.FetchedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	tsStr := ts.UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("netatmo: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	insert := `INSERT INTO netatmo_readings (user_id, timestamp, module_type, metric, value)
	           VALUES (?, ?, ?, ?, ?)`

	if r := readings.Indoor; r != nil {
		rows := []struct {
			metric string
			value  float64
		}{
			{"temperature", r.Temperature},
			{"humidity", float64(r.Humidity)},
			{"co2", float64(r.CO2)},
			{"noise", float64(r.Noise)},
			{"pressure", r.Pressure},
		}
		for _, row := range rows {
			if row.value == 0 {
				continue
			}
			if _, err := tx.Exec(insert, userID, tsStr, "indoor", row.metric, row.value); err != nil {
				return fmt.Errorf("netatmo: insert indoor %s: %w", row.metric, err)
			}
		}
	}

	if r := readings.Outdoor; r != nil {
		rows := []struct {
			metric string
			value  float64
		}{
			{"temperature", r.Temperature},
			{"humidity", float64(r.Humidity)},
		}
		for _, row := range rows {
			if row.value == 0 {
				continue
			}
			if _, err := tx.Exec(insert, userID, tsStr, "outdoor", row.metric, row.value); err != nil {
				return fmt.Errorf("netatmo: insert outdoor %s: %w", row.metric, err)
			}
		}
	}

	if r := readings.Wind; r != nil {
		rows := []struct {
			metric string
			value  float64
		}{
			{"speed", r.Speed},
			{"gust", r.Gust},
			{"direction", float64(r.Direction)},
		}
		for _, row := range rows {
			if row.value == 0 {
				continue
			}
			if _, err := tx.Exec(insert, userID, tsStr, "wind", row.metric, row.value); err != nil {
				return fmt.Errorf("netatmo: insert wind %s: %w", row.metric, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("netatmo: commit readings: %w", err)
	}

	if err := deleteOldReadings(db, userID); err != nil {
		// Non-fatal: log and continue rather than failing the write.
		log.Printf("netatmo: cleanup old readings: %v", err)
	}

	return nil
}

// QueryHistory returns all readings for userID within the last hours hours,
// ordered by timestamp ascending.
func QueryHistory(db *sql.DB, userID int64, hours int) ([]Reading, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT timestamp, module_type, metric, value
		FROM netatmo_readings
		WHERE user_id = ? AND timestamp >= ?
		ORDER BY timestamp ASC`, userID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("netatmo: query history: %w", err)
	}
	defer rows.Close()

	results := make([]Reading, 0)
	for rows.Next() {
		var tsStr, moduleType, metric string
		var value float64
		if err := rows.Scan(&tsStr, &moduleType, &metric, &value); err != nil {
			return nil, fmt.Errorf("netatmo: scan reading: %w", err)
		}
		ts, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			return nil, fmt.Errorf("netatmo: parse timestamp %q: %w", tsStr, err)
		}
		results = append(results, Reading{
			Timestamp:  ts,
			ModuleType: moduleType,
			Metric:     metric,
			Value:      value,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netatmo: iterate readings: %w", err)
	}

	return results, nil
}

// deleteOldReadings removes rows for userID older than retentionDays days.
func deleteOldReadings(db *sql.DB, userID int64) error {
	cutoff := time.Now().UTC().Add(-retentionDays * 24 * time.Hour).Format(time.RFC3339)
	_, err := db.Exec(`DELETE FROM netatmo_readings WHERE user_id = ? AND timestamp < ?`, userID, cutoff)
	if err != nil {
		return fmt.Errorf("netatmo: delete old readings: %w", err)
	}
	return nil
}
