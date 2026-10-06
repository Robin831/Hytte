package offers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// NextDailyRun returns the next time the offers sync should fire (daily at
// 06:30 in the given location — after the chains publish their new weekly
// catalogs). Constructed via time.Date in loc on every call so DST transitions
// are handled correctly.
func NextDailyRun(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, loc)
	if now.Before(todayRun) {
		return todayRun
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 6, 30, 0, 0, loc)
}

// StaleAfter is how old the newest stored offer may be before a startup
// warm-run refetches everything. Slightly under a day so a morning deploy
// shortly before 06:30 doesn't double-fetch.
const StaleAfter = 20 * time.Hour

// Sync sweeps all Norwegian dealers, upserts their current offers and purges
// long-expired rows. Individual dealer failures are logged and skipped so one
// flaky chain never blanks the page (news FetchAll precedent). An error is
// returned only when every dealer failed, which usually means the API key
// rotated or the network is down.
func Sync(ctx context.Context, db *sql.DB) error {
	var collected []Offer
	failures := 0
	for dealerID, name := range Dealers {
		if err := ctx.Err(); err != nil {
			return err
		}
		dealerOffers, err := FetchDealerOffers(ctx, dealerID)
		if err != nil {
			log.Printf("offers: fetch %s: %v", name, err)
			failures++
			continue
		}
		collected = append(collected, dealerOffers...)
		requestPause(ctx)
	}
	if failures == len(Dealers) {
		return fmt.Errorf("offers: all %d dealers failed — check OFFERS_TJEK_API_KEY", failures)
	}

	inserted, err := UpsertOffers(ctx, db, collected)
	if err != nil {
		return err
	}
	purged, err := PurgeExpired(ctx, db)
	if err != nil {
		return err
	}
	log.Printf("offers: synced %d offers (%d new) from %d/%d dealers (purged %d expired)",
		len(collected), len(inserted), len(Dealers)-failures, len(Dealers), purged)
	return nil
}

// NotifyInterval is how often the background notify pass runs. It bounds the
// delay between quiet hours ending (or a manual refresh) and the push.
const NotifyInterval = 15 * time.Minute

// notifyTimeout bounds a single notify pass.
const notifyTimeout = 2 * time.Minute

// notifyMu serialises notify passes so the post-sync pass and the periodic
// pass cannot both push the same unmarked offers.
var notifyMu sync.Mutex

// notifyRecent is the pass RunNotifyPass executes. Swappable in tests.
var notifyRecent = NotifyRecent

// RunNotifyPass pushes watchlist matches for offers first seen within
// RecentWindow. Errors are only logged: notifications never affect syncing.
// The pass is bounded by notifyTimeout and stops early when ctx is cancelled
// (server shutdown); anything it did not get to is picked up by a later pass.
func RunNotifyPass(ctx context.Context, db *sql.DB) {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	nctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	if err := notifyRecent(nctx, db, time.Now()); err != nil {
		log.Printf("offers: notify pass: %v", err)
	}
}

// RunNotifyLoop runs RunNotifyPass every interval until ctx is cancelled.
// Sync itself never pushes, so this loop is what delivers matches from the
// startup warm-run and admin refreshes, and what retries matches held back by
// quiet hours or a failed send.
func RunNotifyLoop(ctx context.Context, db *sql.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunNotifyPass(ctx, db)
		}
	}
}

// SyncIfStale runs Sync only when the stored data is older than StaleAfter
// (or absent). Used for the startup warm-run.
func SyncIfStale(ctx context.Context, db *sql.DB) error {
	last, err := LastFetchedAt(db)
	if err != nil {
		return err
	}
	if !last.IsZero() && time.Since(last) < StaleAfter {
		return nil
	}
	return Sync(ctx, db)
}
