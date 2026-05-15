package pokemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ScanDailyCap is the default per-user daily cap on /scans/queue submissions.
// 600 scans/day is enough to catalogue a 3000-card binder over five relaxed
// sessions while keeping the worst-case Claude-vision spend bounded (a kid
// who fires the auto-detect loop in a tight loop tops out around $1.50/day).
const ScanDailyCap = 600

// scanDailyCapPrefKey is the user_preferences key that overrides ScanDailyCap
// for a single user. Robin can raise his own ceiling without lifting the
// kids' default. The pref is stored as a base-10 integer string; missing /
// blank / non-positive values fall back to ScanDailyCap.
const scanDailyCapPrefKey = "pokemon_scan_daily_cap"

// scanDedupeWindow is how recently another row with the same image hash must
// have been queued for an incoming upload to be treated as a duplicate. The
// camera UI auto-fires while a card lingers, so without dedupe a single card
// could land 5-10 rows in the queue. 10 s comfortably covers that loop
// without merging two genuinely separate scans of the same printed card.
const scanDedupeWindow = 10 * time.Second

// scanLocalLocation returns the timezone used to determine "start of today"
// for the daily cap. Europe/Oslo matches the rest of the server's scheduled
// jobs (currency, suggestions, Pokémon syncs); falling back to UTC keeps the
// cap functional on a host with a stripped tz database rather than 500'ing.
func scanLocalLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		return time.UTC
	}
	return loc
}

// getUserScanDailyCap returns the per-user daily cap, honouring the optional
// pokemon_scan_daily_cap preference override. A missing, blank, or
// non-positive value silently falls back to ScanDailyCap so a malformed pref
// can never lock a user out below the default.
func getUserScanDailyCap(ctx context.Context, db *sql.DB, userID int64) (int, error) {
	var raw string
	err := db.QueryRowContext(ctx, `
		SELECT value FROM user_preferences WHERE user_id = ? AND key = ?
	`, userID, scanDailyCapPrefKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanDailyCap, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load scan daily cap pref: %w", err)
	}
	n, convErr := strconv.Atoi(raw)
	if convErr != nil || n <= 0 {
		return ScanDailyCap, nil
	}
	return n, nil
}

// countScansToday returns how many scan jobs this user has queued since the
// start of the local day (Europe/Oslo). All statuses count — including
// terminal resolutions (added/discarded) and retries — because every queue
// attempt incurs a Claude call, so caps measured against "what the user
// actually triggered" are the only safe budget.
func countScansToday(ctx context.Context, db *sql.DB, userID int64) (int, error) {
	loc := scanLocalLocation()
	now := time.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pokemon_scan_jobs
		WHERE user_id = ? AND created_at >= ?
	`, userID, startOfDay.UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count scans today: %w", err)
	}
	return n, nil
}

// findRecentDuplicateScan returns the id of the most recent row for this
// (user, image_hash) within scanDedupeWindow, or (0, false, nil) when no such
// row exists. The auto-detect camera loop tends to fire the same frame
// multiple times in a row while a card is held still; this lets the handler
// short-circuit and return the existing job rather than burning another
// Claude call on identical bytes.
func findRecentDuplicateScan(ctx context.Context, db *sql.DB, userID int64, imageHash string) (int64, bool, error) {
	if imageHash == "" {
		return 0, false, nil
	}
	cutoff := time.Now().UTC().Add(-scanDedupeWindow)
	var id int64
	err := db.QueryRowContext(ctx, `
		SELECT id FROM pokemon_scan_jobs
		WHERE user_id = ? AND image_hash = ? AND created_at >= ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, userID, imageHash, cutoff).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("lookup recent duplicate scan: %w", err)
	}
	return id, true, nil
}
