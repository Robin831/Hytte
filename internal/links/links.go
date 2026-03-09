package links

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// Link represents a short link record.
type Link struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Code      string `json:"code"`
	TargetURL string `json:"target_url"`
	Title     string `json:"title"`
	Clicks    int64  `json:"clicks"`
	CreatedAt string `json:"created_at"`
}

// GenerateCode creates a random 6-character hex code.
func GenerateCode() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create inserts a new short link. If code is empty, a random one is generated.
func Create(db *sql.DB, userID int64, code, targetURL, title string) (*Link, error) {
	if code == "" {
		var err error
		code, err = GenerateCode()
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}
	}

	res, err := db.Exec(
		"INSERT INTO short_links (user_id, code, target_url, title) VALUES (?, ?, ?, ?)",
		userID, code, targetURL, title,
	)
	if err != nil {
		return nil, fmt.Errorf("insert link: %w", err)
	}

	id, _ := res.LastInsertId()
	return &Link{
		ID:        id,
		UserID:    userID,
		Code:      code,
		TargetURL: targetURL,
		Title:     title,
		Clicks:    0,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// ListByUser returns all short links for a user, ordered by creation date descending.
func ListByUser(db *sql.DB, userID int64) ([]Link, error) {
	rows, err := db.Query(
		"SELECT id, user_id, code, target_url, title, clicks, created_at FROM short_links WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.UserID, &l.Code, &l.TargetURL, &l.Title, &l.Clicks, &l.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	if links == nil {
		links = []Link{}
	}
	return links, nil
}

// GetByCode fetches a link by its short code (for redirect).
func GetByCode(db *sql.DB, code string) (*Link, error) {
	l := &Link{}
	err := db.QueryRow(
		"SELECT id, user_id, code, target_url, title, clicks, created_at FROM short_links WHERE code = ?",
		code,
	).Scan(&l.ID, &l.UserID, &l.Code, &l.TargetURL, &l.Title, &l.Clicks, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// IncrementClicks bumps the click counter for a link.
func IncrementClicks(db *sql.DB, id int64) error {
	_, err := db.Exec("UPDATE short_links SET clicks = clicks + 1 WHERE id = ?", id)
	return err
}

// Delete removes a short link owned by the given user.
func Delete(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM short_links WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Update modifies a short link's target URL, title, or code.
func Update(db *sql.DB, id, userID int64, code, targetURL, title string) (*Link, error) {
	_, err := db.Exec(
		"UPDATE short_links SET code = ?, target_url = ?, title = ? WHERE id = ? AND user_id = ?",
		code, targetURL, title, id, userID,
	)
	if err != nil {
		return nil, err
	}

	l := &Link{}
	err = db.QueryRow(
		"SELECT id, user_id, code, target_url, title, clicks, created_at FROM short_links WHERE id = ? AND user_id = ?",
		id, userID,
	).Scan(&l.ID, &l.UserID, &l.Code, &l.TargetURL, &l.Title, &l.Clicks, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return l, nil
}
