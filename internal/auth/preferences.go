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

// DeleteAllPreferences removes all preferences for a user.
func DeleteAllPreferences(db *sql.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM user_preferences WHERE user_id = ?", userID)
	return err
}
