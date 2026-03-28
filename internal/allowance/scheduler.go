package allowance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// maxPayoutNotificationWorkers limits concurrency for weekly payout push notifications.
const maxPayoutNotificationWorkers = 5

// GenerateWeeklyPayouts computes and persists weekly payout records for every
// parent–child pair that has the kids_allowance feature enabled. It should be
// called once per week (e.g. Sunday evening). Calls are idempotent: UpsertPayout
// overwrites any existing record for the same (parent, child, week_start) key.
func GenerateWeeklyPayouts(db *sql.DB, httpClient *http.Client) {
	links, err := GetAllFamilyLinksWithAllowance(db)
	if err != nil {
		log.Printf("allowance: scheduler get family links: %v", err)
		return
	}
	if len(links) == 0 {
		return
	}

	weekStart := MondayOf(time.Now().UTC())
	log.Printf("allowance: generating payouts for %d family link(s) for week %s", len(links), weekStart)

	// Collect notifications to send after all payouts are persisted.
	type pendingNotification struct {
		parentID int64
		childID  int64
		earnings *WeeklyEarnings
	}
	var notifications []pendingNotification

	for _, link := range links {
		earnings, err := CalculateWeeklyEarnings(db, link.ParentID, link.ChildID, weekStart)
		if err != nil {
			log.Printf("allowance: scheduler calc earnings parent %d child %d: %v", link.ParentID, link.ChildID, err)
			continue
		}

		_, err = UpsertPayout(db, link.ParentID, link.ChildID, weekStart,
			earnings.BaseAllowance, earnings.BonusAmount, earnings.TotalAmount)
		if err != nil {
			log.Printf("allowance: scheduler upsert payout parent %d child %d: %v", link.ParentID, link.ChildID, err)
			continue
		}

		notifications = append(notifications, pendingNotification{link.ParentID, link.ChildID, earnings})
	}

	// Send push notifications with bounded concurrency to avoid goroutine bursts.
	sem := make(chan struct{}, maxPayoutNotificationWorkers)
	var wg sync.WaitGroup
	for _, n := range notifications {
		n := n
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			sendWeeklyPayoutNotification(db, httpClient, n.parentID, n.childID, n.earnings)
		}()
	}
	wg.Wait()
}

// sendWeeklyPayoutNotification fires a push notification to the parent summarising
// a child's weekly earnings. Checks quiet-hours preferences and deduplicates per
// (parent, child, week) to avoid re-sending on scheduler restarts.
func sendWeeklyPayoutNotification(db *sql.DB, httpClient *http.Client, parentID, childID int64, earnings *WeeklyEarnings) {
	// Respect quiet hours.
	if quiethours.IsActive(db, parentID) {
		return
	}

	// Deduplicate: skip if we already sent this week's notification.
	notifType := "allowance-payout"
	reference := fmt.Sprintf("%d-%s", childID, earnings.WeekStart)
	ctx := context.Background()
	cutoff := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	var dummy int
	err := db.QueryRowContext(ctx, `
		SELECT 1 FROM notification_log
		WHERE user_id = ? AND notif_type = ? AND reference = ? AND sent_at > ?
		LIMIT 1
	`, parentID, notifType, reference, cutoff).Scan(&dummy)
	if err == nil {
		// Already sent within the last 8 days — skip.
		return
	}
	if err != sql.ErrNoRows {
		log.Printf("allowance: payout push dedup check parent %d: %v", parentID, err)
		// Proceed and send rather than silently dropping.
	}

	payload, err := json.Marshal(push.Notification{
		Title: "Weekly allowance summary",
		Body:  fmt.Sprintf("This week: %.0f %s (base %.0f + chores %.0f + bonus %.0f)", earnings.TotalAmount, earnings.Currency, earnings.BaseAllowance, earnings.ChoreEarnings, earnings.BonusAmount),
		URL:   "/allowance",
		Tag:   fmt.Sprintf("allowance-payout-%d-%s", childID, earnings.WeekStart),
	})
	if err != nil {
		log.Printf("allowance: marshal payout push payload parent %d: %v", parentID, err)
		return
	}
	if _, err := push.SendToUser(db, httpClient, parentID, payload); err != nil {
		log.Printf("allowance: payout push parent %d: %v", parentID, err)
		return
	}

	// Log the notification for future deduplication.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notification_log (user_id, notif_type, reference, sent_at)
		VALUES (?, ?, ?, ?)
	`, parentID, notifType, reference, now); err != nil {
		log.Printf("allowance: log payout notification parent %d: %v", parentID, err)
	}
}
