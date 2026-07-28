package offers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

	if err := UpsertOffers(ctx, db, collected); err != nil {
		return err
	}
	purged, err := PurgeExpired(ctx, db)
	if err != nil {
		return err
	}
	log.Printf("offers: synced %d offers from %d/%d dealers (purged %d expired)",
		len(collected), len(Dealers)-failures, len(Dealers), purged)
	return nil
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
