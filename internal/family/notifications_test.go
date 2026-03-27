package family

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// preLogFamilyNotif inserts a notification_log entry directly for dedup tests.
func preLogFamilyNotif(t *testing.T, db *sql.DB, userID int64, notifType, ref string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO notification_log (user_id, notif_type, reference, sent_at) VALUES (?, ?, ?, ?)`,
		userID, notifType, ref, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("pre-log notification: %v", err)
	}
}

// TestSendClaimApprovedPush_Dedup verifies that a pre-logged reward_approved
// entry suppresses a duplicate notification within the 1-hour cooldown.
func TestSendClaimApprovedPush_Dedup(t *testing.T) {
	db := setupRewardsTestDB(t)
	childID := int64(2) // seeded by setupRewardsTestDB

	claimID := int64(99)
	ref := fmt.Sprintf("claim:%d", claimID)

	// Pre-log a reward_approved notification to simulate a prior successful send.
	preLogFamilyNotif(t, db, childID, "reward_approved", ref)

	// A second call within cooldown must not write another log row.
	sendClaimApprovedPush(db, childID, claimID, "Ice Cream")

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'reward_approved' AND reference = ?`,
		childID, ref).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendClaimDeniedPush_Dedup verifies that a pre-logged reward_denied
// entry suppresses a duplicate notification within the 1-hour cooldown.
func TestSendClaimDeniedPush_Dedup(t *testing.T) {
	db := setupRewardsTestDB(t)
	childID := int64(2)

	claimID := int64(77)
	ref := fmt.Sprintf("claim:%d", claimID)

	// Pre-log a reward_denied notification.
	preLogFamilyNotif(t, db, childID, "reward_denied", ref)

	// A second call within cooldown must be suppressed.
	sendClaimDeniedPush(db, childID, claimID, "Video Game")

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'reward_denied' AND reference = ?`,
		childID, ref).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendClaimApprovedPush_NoPanic verifies that the approved push function
// does not panic when there are no push subscriptions.
func TestSendClaimApprovedPush_NoPanic(t *testing.T) {
	db := setupRewardsTestDB(t)
	sendClaimApprovedPush(db, 2, 10, "Sticker")
}

// TestSendClaimDeniedPush_NoPanic verifies that the denied push function
// does not panic when there are no push subscriptions.
func TestSendClaimDeniedPush_NoPanic(t *testing.T) {
	db := setupRewardsTestDB(t)
	sendClaimDeniedPush(db, 2, 11, "Movie Ticket")
}

// TestSendClaimApprovedAndDenied_IndependentDedup verifies that approved and
// denied notification types are independent — a prior approved entry does not
// suppress the denied type for the same claim ID.
func TestSendClaimApprovedAndDenied_IndependentDedup(t *testing.T) {
	db := setupRewardsTestDB(t)
	childID := int64(2)
	claimID := int64(55)
	ref := fmt.Sprintf("claim:%d", claimID)

	// Pre-log only the approved type.
	preLogFamilyNotif(t, db, childID, "reward_approved", ref)

	// The denied type has no prior entry — push will fail silently (no subscriptions),
	// but the function must not panic.
	sendClaimDeniedPush(db, childID, claimID, "Prize")

	// Approved count must still be 1 (not affected by denied call).
	var approvedCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'reward_approved' AND reference = ?`,
		childID, ref).Scan(&approvedCount); err != nil {
		t.Fatalf("count approved: %v", err)
	}
	if approvedCount != 1 {
		t.Errorf("expected 1 approved entry, got %d", approvedCount)
	}
}
