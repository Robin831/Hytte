package push

import (
	"database/sql"
	"fmt"
	"time"
)

// Subscription represents a Web Push subscription stored for a user.
type Subscription struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	CreatedAt string `json:"created_at"`
}

// SaveSubscription stores or updates a push subscription for a user.
// If the same endpoint already exists for the user, the keys are updated.
func SaveSubscription(db *sql.DB, userID int64, endpoint, p256dh, auth string) (*Subscription, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth
	`, userID, endpoint, p256dh, auth, now)
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
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
