package offers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// UpsertOffers replaces or inserts the given offers in one transaction,
// stamping fetched_at (and first_seen_at for new rows). Calling it repeatedly with the same ids leaves one row
// per id. Only the first call reports an id as inserted.
// It returns the ids of offers that did not exist before this call (in input
// order); offers that were merely updated are not included. The slice is only
// returned once the transaction has committed.
func UpsertOffers(ctx context.Context, db *sql.DB, offers []Offer) ([]string, error) {
	if len(offers) == 0 {
		return nil, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("offers upsert: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO shop_offers (id, dealer_id, dealer_name, heading, description, price, pre_price,
			currency, unit_price, unit_label, image_url, run_from, run_till, fetched_at, first_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`)
	if err != nil {
		return nil, fmt.Errorf("offers upsert: prepare insert: %w", err)
	}
	defer insertStmt.Close()

	updateStmt, err := tx.PrepareContext(ctx, `
		UPDATE shop_offers SET
			dealer_name = ?,
			heading     = ?,
			description = ?,
			price       = ?,
			pre_price   = ?,
			currency    = ?,
			unit_price  = ?,
			unit_label  = ?,
			image_url   = ?,
			run_from    = ?,
			run_till    = ?,
			fetched_at  = ?
		WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("offers upsert: prepare update: %w", err)
	}
	defer updateStmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var inserted []string
	for _, o := range offers {
		res, err := insertStmt.ExecContext(ctx, o.ID, o.DealerID, o.DealerName, o.Heading, o.Description,
			o.Price, o.PrePrice, o.Currency, o.UnitPrice, o.UnitLabel, o.ImageURL, o.RunFrom, o.RunTill, now, now)
		if err != nil {
			return nil, fmt.Errorf("offers insert %s: %w", o.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("offers insert %s: rows affected: %w", o.ID, err)
		}
		if n > 0 {
			inserted = append(inserted, o.ID)
			continue
		}
		if _, err := updateStmt.ExecContext(ctx, o.DealerName, o.Heading, o.Description, o.Price, o.PrePrice,
			o.Currency, o.UnitPrice, o.UnitLabel, o.ImageURL, o.RunFrom, o.RunTill, now, o.ID); err != nil {
			return nil, fmt.Errorf("offers update %s: %w", o.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("offers upsert: commit: %w", err)
	}
	return inserted, nil
}

// offerColumns is the shop_offers column list read into an Offer by
// scanOffer. Keep the two in sync.
const offerColumns = `id, dealer_id, dealer_name, heading, description, price, pre_price,
	currency, unit_price, unit_label, image_url, run_from, run_till`

// scanOffer scans one row selected with offerColumns, followed by any extra
// destinations for columns appended after them.
func scanOffer(rows *sql.Rows, extra ...any) (Offer, error) {
	var o Offer
	dest := append([]any{&o.ID, &o.DealerID, &o.DealerName, &o.Heading, &o.Description, &o.Price,
		&o.PrePrice, &o.Currency, &o.UnitPrice, &o.UnitLabel, &o.ImageURL, &o.RunFrom, &o.RunTill}, extra...)
	err := rows.Scan(dest...)
	return o, err
}

// ListCurrent returns all offers whose validity window includes today,
// unranked (ranking is per-user).
func ListCurrent(db *sql.DB) ([]Offer, error) {
	today := time.Now().UTC().Format("2006-01-02")
	rows, err := db.Query(`
		SELECT `+offerColumns+`, fetched_at
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
		var fetchedAt string
		o, err := scanOffer(rows, &fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("scan offer: %w", err)
		}
		o.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
		out = append(out, o)
	}
	return out, rows.Err()
}

// OffersByID returns the stored offers among ids that have not already
// expired at now, ordered by id. Offers whose window starts in the future are
// kept: they are new catalog entries a user will want to know about.
func OffersByID(ctx context.Context, db *sql.DB, ids []string, now time.Time) ([]Offer, error) {
	today := now.UTC().Format("2006-01-02")
	var out []Offer
	err := forEachIDChunk(ids, func(placeholders string, idArgs []any) error {
		rows, err := db.QueryContext(ctx, `
			SELECT `+offerColumns+`
			FROM shop_offers
			WHERE id IN (`+placeholders+`) AND run_till >= ?
			ORDER BY id ASC
		`, append(idArgs, today)...)
		if err != nil {
			return fmt.Errorf("query offers by id: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanOffer(rows)
			if err != nil {
				return fmt.Errorf("scan offer: %w", err)
			}
			out = append(out, o)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate offers by id: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RecentOfferIDs returns the ids of offers first inserted at or after since,
// ordered by id.
func RecentOfferIDs(ctx context.Context, db *sql.DB, since time.Time) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id FROM shop_offers WHERE first_seen_at >= ? ORDER BY id ASC",
		since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query recent offers: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recent offer: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PurgeExpired deletes offers whose validity ended more than seven days ago,
// together with any offer_notifications rows that reference them. It returns
// the number of offers removed.
func PurgeExpired(ctx context.Context, db *sql.DB) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("purge expired offers: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM offer_notifications
		WHERE offer_id IN (SELECT id FROM shop_offers WHERE run_till < ?)
	`, cutoff); err != nil {
		return 0, fmt.Errorf("purge expired offer notifications: %w", err)
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM shop_offers WHERE run_till < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge expired offers: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge expired offers: rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("purge expired offers: commit: %w", err)
	}
	return n, nil
}

// notifiedLookupChunk bounds the number of placeholders per IN (...) query so
// large offer batches stay well under SQLite's bound-variable limit.
const notifiedLookupChunk = 500

// forEachIDChunk calls fn once per chunk of at most notifiedLookupChunk ids,
// passing a "?,?,..." placeholder list and the chunk as query args. It stops
// at the first error.
func forEachIDChunk(ids []string, fn func(placeholders string, args []any) error) error {
	for start := 0; start < len(ids); start += notifiedLookupChunk {
		end := min(start+notifiedLookupChunk, len(ids))
		chunk := ids[start:end]
		args := make([]any, 0, len(chunk)+1)
		for _, id := range chunk {
			args = append(args, id)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		if err := fn(placeholders, args); err != nil {
			return err
		}
	}
	return nil
}

// NotifiedOfferIDs returns the subset of offerIDs the user has already been
// notified about. The map only contains ids that were found (all true).
func NotifiedOfferIDs(ctx context.Context, db *sql.DB, userID int64, offerIDs []string) (map[string]bool, error) {
	out := make(map[string]bool)
	err := forEachIDChunk(offerIDs, func(placeholders string, idArgs []any) error {
		rows, err := db.QueryContext(ctx,
			"SELECT offer_id FROM offer_notifications WHERE user_id = ? AND offer_id IN ("+placeholders+")",
			append([]any{userID}, idArgs...)...)
		if err != nil {
			return fmt.Errorf("query notified offers: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan notified offer: %w", err)
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate notified offers: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkOffersNotified records that the user has been notified about offerIDs.
// Already-marked offers keep their original notified_at, so the call is
// idempotent.
func MarkOffersNotified(ctx context.Context, db *sql.DB, userID int64, offerIDs []string, now time.Time) error {
	if len(offerIDs) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark offers notified: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO offer_notifications (user_id, offer_id, notified_at) VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("mark offers notified: prepare: %w", err)
	}
	defer stmt.Close()

	ts := now.UTC().Format(time.RFC3339)
	for _, id := range offerIDs {
		if _, err := stmt.ExecContext(ctx, userID, id, ts); err != nil {
			return fmt.Errorf("mark offer %s notified: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark offers notified: commit: %w", err)
	}
	return nil
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
