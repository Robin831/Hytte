package offers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// NotifyPreferenceKey is the user preference that opts a user into watchlist
// match pushes. Only the value "true" enables them; a missing row means off.
const NotifyPreferenceKey = "offers_notify"

// Sender delivers one notification payload to all of a user's push
// subscriptions. It must return nil only when the push was delivered to at
// least one subscription.
type Sender func(db *sql.DB, userID int64, payload []byte) error

// QuietChecker reports whether the user is inside their quiet hours at now.
type QuietChecker func(db *sql.DB, userID int64, now time.Time) bool

// Notifier sends one push per user when newly stored offers match their
// watchlist. The function fields are injectable so tests run without VAPID
// keys or real push endpoints.
type Notifier struct {
	DB              *sql.DB
	Send            Sender
	InQuietHours    QuietChecker
	VAPIDConfigured func(db *sql.DB) (bool, error)
}

// NewNotifier returns a Notifier wired to the real push and quiet-hours
// implementations.
func NewNotifier(db *sql.DB) *Notifier {
	return &Notifier{
		DB:              db,
		Send:            defaultSend,
		InQuietHours:    quiethours.IsActiveAt,
		VAPIDConfigured: vapidKeysExist,
	}
}

// NotifyNewMatches runs the notify pass with production dependencies.
func NotifyNewMatches(ctx context.Context, db *sql.DB, newOfferIDs []string, now time.Time) error {
	return NewNotifier(db).Notify(ctx, newOfferIDs, now)
}

// defaultSend pushes via push.SendToUser and treats the send as successful
// only when at least one subscription accepted it (2xx), so offers are not
// marked notified when every endpoint failed.
func defaultSend(db *sql.DB, userID int64, payload []byte) error {
	results, err := push.SendToUser(db, push.DefaultHTTPClient, userID, payload)
	if err != nil {
		return err
	}
	var lastErr error
	for _, r := range results {
		if r.Err == nil && r.StatusCode >= 200 && r.StatusCode < 300 {
			return nil
		}
		if r.Err != nil {
			lastErr = r.Err
		} else {
			lastErr = fmt.Errorf("push endpoint returned %d", r.StatusCode)
		}
	}
	if lastErr == nil {
		return fmt.Errorf("no push subscriptions")
	}
	return fmt.Errorf("no subscription accepted the push (%d attempted): %w", len(results), lastErr)
}

// vapidKeysExist reports whether the server's VAPID key pair has been
// created. Keys are generated lazily when a browser first subscribes, so a
// missing row means nobody can have a working subscription yet.
func vapidKeysExist(db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM vapid_keys WHERE id = 1").Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Notify sends at most one push per eligible user summarising which of
// newOfferIDs match their watchlist. Eligible users have the offers feature
// (admins always do), offers_notify = "true", at least one watchlist keyword
// and at least one push subscription.
//
// Matching uses the same keyword logic as Rank, so a push never names an
// offer the page would not highlight. Offers already recorded in
// offer_notifications for the user are dropped, and the remaining ones are
// marked only after a successful send, so a delivered offer is never pushed
// twice and a failed send leaves them unmarked (pushed again only if the same
// ids are passed to a later pass).
//
// Quiet hours: a user inside their quiet window is skipped and the matched
// offers are left unmarked; no retry is scheduled. Only newly inserted offer
// ids are ever considered, so an offer that arrives during quiet hours is not
// pushed later — it is still highlighted on the page. This keeps quiet hours
// silent without a deferred-delivery queue and avoids a flood of stale
// matches being bundled into a later push.
//
// Per-user failures are logged and the loop continues. An error is returned
// only when the shared inputs (offers, candidate users) cannot be loaded.
// Missing VAPID keys are a logged no-op.
func (n *Notifier) Notify(ctx context.Context, newOfferIDs []string, now time.Time) error {
	if len(newOfferIDs) == 0 {
		return nil
	}
	if n.VAPIDConfigured != nil {
		ok, err := n.VAPIDConfigured(n.DB)
		if err != nil {
			log.Printf("offers: notify: check vapid keys: %v", err)
			return nil
		}
		if !ok {
			log.Printf("offers: notify: VAPID keys not configured, skipping %d new offers", len(newOfferIDs))
			return nil
		}
	}

	newOffers, err := loadOffersByID(ctx, n.DB, newOfferIDs, now)
	if err != nil {
		return err
	}
	if len(newOffers) == 0 {
		return nil
	}
	userIDs, err := notifyCandidates(ctx, n.DB)
	if err != nil {
		return err
	}

	for _, userID := range userIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		n.notifyUser(ctx, userID, newOffers, now)
	}
	return nil
}

// notifyUser handles one user; every failure is logged, never returned.
func (n *Notifier) notifyUser(ctx context.Context, userID int64, newOffers []Offer, now time.Time) {
	if n.InQuietHours != nil && n.InQuietHours(n.DB, userID, now) {
		return
	}

	watchlist, err := ListWatchlist(n.DB, userID)
	if err != nil {
		log.Printf("offers: notify user %d: %v", userID, err)
		return
	}
	keywords := make([]string, len(watchlist))
	for i, w := range watchlist {
		keywords[i] = w.Keyword
	}
	lowered := lowerKeywords(keywords)

	matchedIDs := []string{}
	matchedBy := make(map[string][]string)
	for _, o := range newOffers {
		if kws := matchKeywords(o, keywords, lowered); len(kws) > 0 {
			matchedIDs = append(matchedIDs, o.ID)
			matchedBy[o.ID] = kws
		}
	}
	if len(matchedIDs) == 0 {
		return
	}

	already, err := NotifiedOfferIDs(ctx, n.DB, userID, matchedIDs)
	if err != nil {
		log.Printf("offers: notify user %d: %v", userID, err)
		return
	}
	pending := matchedIDs[:0]
	hit := make(map[string]bool)
	for _, id := range matchedIDs {
		if already[id] {
			continue
		}
		pending = append(pending, id)
		for _, k := range matchedBy[id] {
			hit[k] = true
		}
	}
	if len(pending) == 0 {
		return
	}
	// Distinct keywords in watchlist order.
	var hitKeywords []string
	for _, k := range keywords {
		if hit[k] {
			hitKeywords = append(hitKeywords, k)
			delete(hit, k)
		}
	}

	payload, err := json.Marshal(buildNotification(len(pending), hitKeywords))
	if err != nil {
		log.Printf("offers: notify user %d: marshal: %v", userID, err)
		return
	}
	if err := n.Send(n.DB, userID, payload); err != nil {
		log.Printf("offers: notify user %d: send: %v", userID, err)
		return
	}
	if err := MarkOffersNotified(ctx, n.DB, userID, pending, now); err != nil {
		log.Printf("offers: notify user %d: %v", userID, err)
	}
}

// buildNotification renders the push text server-side, e.g.
// "3 new matches: melk, kaffe, laks".
func buildNotification(count int, keywords []string) push.Notification {
	noun := "matches"
	if count == 1 {
		noun = "match"
	}
	return push.Notification{
		Title: "New offer matches",
		Body:  fmt.Sprintf("%d new %s: %s", count, noun, strings.Join(keywords, ", ")),
		URL:   "/offers",
		Tag:   "offers-matches",
	}
}

// loadOffersByID returns the stored offers among ids that have not already
// expired at now. Offers whose window starts in the future are kept: they
// are new catalog entries the user will want to know about.
func loadOffersByID(ctx context.Context, db *sql.DB, ids []string, now time.Time) ([]Offer, error) {
	today := now.UTC().Format("2006-01-02")
	var out []Offer
	for start := 0; start < len(ids); start += notifiedLookupChunk {
		end := min(start+notifiedLookupChunk, len(ids))
		chunk := ids[start:end]

		args := make([]any, 0, len(chunk)+1)
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, today)
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		rows, err := db.QueryContext(ctx, `
			SELECT id, dealer_id, dealer_name, heading, description, price, pre_price,
			       currency, unit_price, unit_label, image_url, run_from, run_till
			FROM shop_offers
			WHERE id IN (`+placeholders+`) AND run_till >= ?
			ORDER BY id ASC
		`, args...)
		if err != nil {
			return nil, fmt.Errorf("offers notify: query new offers: %w", err)
		}
		for rows.Next() {
			var o Offer
			if err := rows.Scan(&o.ID, &o.DealerID, &o.DealerName, &o.Heading, &o.Description, &o.Price,
				&o.PrePrice, &o.Currency, &o.UnitPrice, &o.UnitLabel, &o.ImageURL, &o.RunFrom, &o.RunTill); err != nil {
				rows.Close()
				return nil, fmt.Errorf("offers notify: scan offer: %w", err)
			}
			out = append(out, o)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("offers notify: iterate offers: %w", err)
		}
	}
	return out, nil
}

// notifyCandidates returns the ids of users who have the offers feature
// (admins bypass feature checks), opted in via offers_notify = "true", have a
// non-empty watchlist and at least one push subscription.
func notifyCandidates(ctx context.Context, db *sql.DB) ([]int64, error) {
	featureCond := `EXISTS (SELECT 1 FROM user_features f
		WHERE f.user_id = u.id AND f.feature_key = 'offers' AND f.enabled = 1)`
	if auth.FeatureDefaults["offers"] {
		featureCond = `NOT EXISTS (SELECT 1 FROM user_features f
			WHERE f.user_id = u.id AND f.feature_key = 'offers' AND f.enabled = 0)`
	}
	rows, err := db.QueryContext(ctx, `
		SELECT u.id
		FROM users u
		JOIN user_preferences p ON p.user_id = u.id AND p.key = ? AND p.value = 'true'
		WHERE (u.is_admin = 1 OR `+featureCond+`)
		  AND EXISTS (SELECT 1 FROM offer_watchlist w WHERE w.user_id = u.id)
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = u.id)
		ORDER BY u.id ASC
	`, NotifyPreferenceKey)
	if err != nil {
		return nil, fmt.Errorf("offers notify: query users: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("offers notify: scan user: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
