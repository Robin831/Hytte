package push

import (
	"database/sql"
	"fmt"
	"time"
)

// Subscription represents a Web Push subscription stored in the database.
type Subscription struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
}

// SaveSubscription stores a push subscription for a user. If the endpoint
// already exists, it updates the keys (the browser may rotate them).
func SaveSubscription(db *sql.DB, userID int64, endpoint, p256dh, auth, userAgent string) (*Subscription, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent
	`, userID, endpoint, p256dh, auth, userAgent, now)
	if err != nil {
		return nil, fmt.Errorf("save subscription: %w", err)
	}

	sub := &Subscription{}
	err = db.QueryRow(
		"SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at FROM push_subscriptions WHERE endpoint = ?",
		endpoint,
	).Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.UserAgent, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("fetch subscription: %w", err)
	}
	return sub, nil
}

// DeleteSubscription removes a push subscription by endpoint for a user.
func DeleteSubscription(db *sql.DB, userID int64, endpoint string) error {
	result, err := db.Exec(
		"DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?",
		userID, endpoint,
	)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSubscriptions returns all push subscriptions for a user.
func GetSubscriptions(db *sql.DB, userID int64) ([]Subscription, error) {
	rows, err := db.Query(
		"SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at FROM push_subscriptions WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetSubscriptionsByUserID returns all subscriptions for a user (used by the
// push delivery helper).
func GetSubscriptionsByUserID(db *sql.DB, userID int64) ([]Subscription, error) {
	return GetSubscriptions(db, userID)
}

// DeleteSubscriptionByEndpoint removes a subscription by endpoint (used when
// the push service returns 410 Gone).
func DeleteSubscriptionByEndpoint(db *sql.DB, endpoint string) error {
	_, err := db.Exec("DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}
