package auth

import "database/sql"

// User represents a user record.
type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	GoogleID  string `json:"-"`
	CreatedAt string `json:"created_at"`
	IsAdmin   bool   `json:"is_admin"`
}

// UpsertUser creates or updates a user by Google ID, returning the user record.
// The first user ever inserted (when no admin exists yet) is automatically promoted to admin.
func UpsertUser(db *sql.DB, googleID, email, name, picture string) (*User, error) {
	_, err := db.Exec(`
		INSERT INTO users (google_id, email, name, picture)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(google_id) DO UPDATE SET
			email = excluded.email,
			name = excluded.name,
			picture = excluded.picture
	`, googleID, email, name, picture)
	if err != nil {
		return nil, err
	}

	// Promote this user to admin if no admin exists yet (handles first-user on fresh installs).
	_, err = db.Exec(`
		UPDATE users SET is_admin = 1
		WHERE google_id = ?
		  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = 1)
	`, googleID)
	if err != nil {
		return nil, err
	}

	u := &User{}
	err = db.QueryRow(
		"SELECT id, email, name, picture, google_id, created_at, is_admin FROM users WHERE google_id = ?",
		googleID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.GoogleID, &u.CreatedAt, &u.IsAdmin)
	return u, err
}

// GetUserByID fetches a user by their database ID.
func GetUserByID(db *sql.DB, id int64) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		"SELECT id, email, name, picture, google_id, created_at, is_admin FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.GoogleID, &u.CreatedAt, &u.IsAdmin)
	if err != nil {
		return nil, err
	}
	return u, nil
}
