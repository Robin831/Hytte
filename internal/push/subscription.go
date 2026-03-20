package push

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Subscription represents a Web Push subscription stored for a user.
// The P256dh and Auth fields are cryptographic keys used for push encryption
// and are excluded from JSON serialization to prevent exposure in API responses.
type Subscription struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"-"`
	Auth      string `json:"-"`
	CreatedAt string `json:"created_at"`
}

// SaveSubscription stores or updates a push subscription for a user.
// If the same endpoint already exists for the user, the keys are updated.
func SaveSubscription(db *sql.DB, userID int64, endpoint, p256dh, auth string) (*Subscription, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	encP256dh, err := encryption.EncryptField(p256dh)
	if err != nil {
		return nil, fmt.Errorf("encrypt p256dh: %w", err)
	}
	encAuth, err := encryption.EncryptField(auth)
	if err != nil {
		return nil, fmt.Errorf("encrypt auth: %w", err)
	}
	_, err = db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth
	`, userID, endpoint, encP256dh, encAuth, now)
	if err != nil {
		return nil, fmt.Errorf("save subscription: %w", err)
	}

	sub := &Subscription{}
	err = db.QueryRow(`
		SELECT id, user_id, endpoint, p256dh, auth, created_at
		FROM push_subscriptions
		WHERE user_id = ? AND endpoint = ?
	`, userID, endpoint).Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("read saved subscription: %w", err)
	}
	if sub.P256dh, err = encryption.DecryptField(sub.P256dh); err != nil {
		return nil, fmt.Errorf("decrypt p256dh: %w", err)
	}
	if sub.Auth, err = encryption.DecryptField(sub.Auth); err != nil {
		return nil, fmt.Errorf("decrypt auth: %w", err)
	}
	return sub, nil
}

// DeleteSubscription removes a push subscription by endpoint for a user.
func DeleteSubscription(db *sql.DB, userID int64, endpoint string) error {
	res, err := db.Exec("DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?", userID, endpoint)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSubscriptionByID removes a push subscription by its ID, scoped to a user.
// Returns sql.ErrNoRows if the subscription does not exist or belongs to a different user.
func DeleteSubscriptionByID(db *sql.DB, userID int64, subID int64) error {
	res, err := db.Exec("DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?", subID, userID)
	if err != nil {
		return fmt.Errorf("delete subscription by id: %w", err)
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

// GetSubscriptionsByUser returns all push subscriptions for a user.
func GetSubscriptionsByUser(db *sql.DB, userID int64) ([]Subscription, error) {
	rows, err := db.Query(`
		SELECT id, user_id, endpoint, p256dh, auth, created_at
		FROM push_subscriptions
		WHERE user_id = ?
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		if s.P256dh, err = encryption.DecryptField(s.P256dh); err != nil {
			return nil, fmt.Errorf("decrypt p256dh: %w", err)
		}
		if s.Auth, err = encryption.DecryptField(s.Auth); err != nil {
			return nil, fmt.Errorf("decrypt auth: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetAllSubscriptions returns all push subscriptions across all users.
// This is used by the push delivery helper to broadcast notifications.
func GetAllSubscriptions(db *sql.DB) ([]Subscription, error) {
	rows, err := db.Query(`
		SELECT id, user_id, endpoint, p256dh, auth, created_at
		FROM push_subscriptions
		ORDER BY user_id, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query all subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		if s.P256dh, err = encryption.DecryptField(s.P256dh); err != nil {
			return nil, fmt.Errorf("decrypt p256dh: %w", err)
		}
		if s.Auth, err = encryption.DecryptField(s.Auth); err != nil {
			return nil, fmt.Errorf("decrypt auth: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
