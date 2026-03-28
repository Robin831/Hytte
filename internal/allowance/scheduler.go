package allowance

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/push"
)

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

	weekStart := MondayOf(time.Now())
	log.Printf("allowance: generating payouts for %d family link(s) for week %s", len(links), weekStart)

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

		// Send push notification to parent with weekly summary.
		go sendWeeklyPayoutNotification(db, httpClient, link.ParentID, link.ChildID, earnings)
	}
}

// sendWeeklyPayoutNotification fires a push notification to the parent summarising
// a child's weekly earnings. Runs asynchronously so payout generation is not blocked.
func sendWeeklyPayoutNotification(db *sql.DB, httpClient *http.Client, parentID, childID int64, earnings *WeeklyEarnings) {
	payload, err := json.Marshal(push.Notification{
		Title: "Weekly allowance summary",
		Body:  fmt.Sprintf("This week: %.0f %s (base %.0f + chores %.0f + bonus %.0f)", earnings.TotalAmount, earnings.Currency, earnings.BaseAllowance, earnings.ChoreEarnings, earnings.BonusAmount),
		URL:   "/allowance",
		Tag:   fmt.Sprintf("allowance-payout-%d-%s", childID, earnings.WeekStart),
	})
	if err != nil {
		return
	}
	if _, err := push.SendToUser(db, httpClient, parentID, payload); err != nil {
		log.Printf("allowance: payout push parent %d: %v", parentID, err)
	}
}
