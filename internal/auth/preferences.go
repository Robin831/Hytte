package auth

import "database/sql"

// GetPreferences returns all preferences for a user as a key-value map.
func GetPreferences(db *sql.DB, userID int64) (map[string]string, error) {
	rows, err := db.Query("SELECT key, value FROM user_preferences WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		prefs[k] = v
	}
	return prefs, rows.Err()
}

// SetPreference upserts a single preference for a user.
func SetPreference(db *sql.DB, userID int64, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO user_preferences (user_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
	`, userID, key, value)
	return err
}

// SetPreferences upserts multiple preferences for a user in a single
// transaction. Either all provided key/value pairs are persisted or, on
// error, none are (the transaction is rolled back). Passing an empty map is a
// no-op that returns nil.
func SetPreferences(db *sql.DB, userID int64, prefs map[string]string) error {
	if len(prefs) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Roll back on any error path; Commit below makes this a no-op on success.
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
		INSERT INTO user_preferences (user_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range prefs {
		if _, err := stmt.Exec(userID, key, value); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteAllPreferences removes all preferences for a user.
func DeleteAllPreferences(db *sql.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM user_preferences WHERE user_id = ?", userID)
	return err
}
