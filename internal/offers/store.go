package offers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// UpsertOffers replaces or inserts the given offers in one transaction,
// stamping fetched_at. Calling it repeatedly with the same ids is idempotent.
func UpsertOffers(ctx context.Context, db *sql.DB, offers []Offer) error {
	if len(offers) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("offers upsert: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO shop_offers (id, dealer_id, dealer_name, heading, description, price, pre_price,
			currency, unit_price, unit_label, image_url, run_from, run_till, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			dealer_name = excluded.dealer_name,
			heading     = excluded.heading,
			description = excluded.description,
			price       = excluded.price,
			pre_price   = excluded.pre_price,
			currency    = excluded.currency,
			unit_price  = excluded.unit_price,
			unit_label  = excluded.unit_label,
			image_url   = excluded.image_url,
			run_from    = excluded.run_from,
			run_till    = excluded.run_till,
			fetched_at  = excluded.fetched_at
	`)
	if err != nil {
		return fmt.Errorf("offers upsert: prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, o := range offers {
		if _, err := stmt.ExecContext(ctx, o.ID, o.DealerID, o.DealerName, o.Heading, o.Description,
			o.Price, o.PrePrice, o.Currency, o.UnitPrice, o.UnitLabel, o.ImageURL, o.RunFrom, o.RunTill, now); err != nil {
			return fmt.Errorf("offers upsert %s: %w", o.ID, err)
		}
	}
	return tx.Commit()
}

// ListCurrent returns all offers whose validity window includes today,
// unranked (ranking is per-user).
func ListCurrent(db *sql.DB) ([]Offer, error) {
	today := time.Now().UTC().Format("2006-01-02")
	rows, err := db.Query(`
		SELECT id, dealer_id, dealer_name, heading, description, price, pre_price,
		       currency, unit_price, unit_label, image_url, run_from, run_till, fetched_at
		FROM shop_offers
		WHERE run_till >= ? AND run_from <= ?
		ORDER BY dealer_id ASC, heading ASC
	`, today, today)
	if err != nil {
		return nil, fmt.Errorf("query current offers: %w", err)
	}
	defer rows.Close()

	out := []Offer{}
	for rows.Next() {
		var o Offer
		var fetchedAt string
		if err := rows.Scan(&o.ID, &o.DealerID, &o.DealerName, &o.Heading, &o.Description, &o.Price,
			&o.PrePrice, &o.Currency, &o.UnitPrice, &o.UnitLabel, &o.ImageURL, &o.RunFrom, &o.RunTill, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan offer: %w", err)
		}
		o.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
		out = append(out, o)
	}
	return out, rows.Err()
}

// PurgeExpired deletes offers whose validity ended more than seven days ago.
func PurgeExpired(ctx context.Context, db *sql.DB) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")
	res, err := db.ExecContext(ctx, "DELETE FROM shop_offers WHERE run_till < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge expired offers: %w", err)
	}
	return res.RowsAffected()
}

// LastFetchedAt returns the most recent fetch timestamp, or the zero time when
// the table is empty.
func LastFetchedAt(db *sql.DB) (time.Time, error) {
	var raw sql.NullString
	if err := db.QueryRow("SELECT MAX(fetched_at) FROM shop_offers").Scan(&raw); err != nil {
		return time.Time{}, fmt.Errorf("query last fetched_at: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse fetched_at %q: %w", raw.String, err)
	}
	return t, nil
}

// --- Watchlist ---

// ListWatchlist returns the user's priority keywords, oldest first.
func ListWatchlist(db *sql.DB, userID int64) ([]WatchlistEntry, error) {
	rows, err := db.Query(`
		SELECT id, user_id, keyword, created_at
		FROM offer_watchlist
		WHERE user_id = ?
		ORDER BY created_at ASC, id ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query watchlist: %w", err)
	}
	defer rows.Close()

	out := []WatchlistEntry{}
	for rows.Next() {
		var w WatchlistEntry
		var createdAt string
		if err := rows.Scan(&w.ID, &w.UserID, &w.Keyword, &createdAt); err != nil {
			return nil, fmt.Errorf("scan watchlist entry: %w", err)
		}
		if w.Keyword, err = encryption.DecryptField(w.Keyword); err != nil {
			return nil, fmt.Errorf("decrypt watchlist keyword: %w", err)
		}
		w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddWatchlist inserts a keyword for the user and returns the stored entry.
// Duplicate keywords (case-insensitive) are rejected with ErrDuplicateKeyword.
func AddWatchlist(db *sql.DB, userID int64, keyword string) (WatchlistEntry, error) {
	existing, err := ListWatchlist(db, userID)
	if err != nil {
		return WatchlistEntry{}, err
	}
	for _, w := range existing {
		if equalFold(w.Keyword, keyword) {
			return WatchlistEntry{}, ErrDuplicateKeyword
		}
	}
	encKeyword, err := encryption.EncryptField(keyword)
	if err != nil {
		return WatchlistEntry{}, fmt.Errorf("encrypt watchlist keyword: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO offer_watchlist (user_id, keyword, created_at) VALUES (?, ?, ?)
	`, userID, encKeyword, now)
	if err != nil {
		return WatchlistEntry{}, fmt.Errorf("insert watchlist entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WatchlistEntry{}, fmt.Errorf("last insert id: %w", err)
	}
	entry := WatchlistEntry{ID: id, UserID: userID, Keyword: keyword}
	entry.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return entry, nil
}

// ErrDuplicateKeyword is returned when adding a keyword the user already has.
var ErrDuplicateKeyword = fmt.Errorf("keyword already on watchlist")

// DeleteWatchlist removes a keyword, scoped to the owner.
func DeleteWatchlist(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM offer_watchlist WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("delete watchlist entry: %w", err)
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
