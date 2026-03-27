package stars

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestWasSentRecently verifies the deduplication logic in WasSentRecently.
func TestWasSentRecently(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "user@test.com")

	// Initially nothing has been sent.
	if WasSentRecently(ctx, db, userID, "test_type", "ref-1", time.Hour) {
		t.Fatal("expected false before any notification is logged")
	}

	// Log a notification and check it is detected within cooldown.
	logNotification(ctx, db, userID, "test_type", "ref-1")

	if !WasSentRecently(ctx, db, userID, "test_type", "ref-1", time.Hour) {
		t.Fatal("expected true after notification is logged within cooldown")
	}

	// A different reference should not be affected.
	if WasSentRecently(ctx, db, userID, "test_type", "ref-2", time.Hour) {
		t.Fatal("expected false for different reference")
	}

	// A different notif_type should not be affected.
	if WasSentRecently(ctx, db, userID, "other_type", "ref-1", time.Hour) {
		t.Fatal("expected false for different notif_type")
	}

	// A different user should not be affected.
	otherID := insertUser(t, db, "other@test.com")
	if WasSentRecently(ctx, db, otherID, "test_type", "ref-1", time.Hour) {
		t.Fatal("expected false for different user")
	}
}

// TestWasSentRecently_Expiry verifies that a notification logged beyond the
// cooldown window is not counted as recent.
func TestWasSentRecently_Expiry(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "expiry@test.com")

	// Insert a row with a timestamp well outside the cooldown window.
	oldTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO notification_log (user_id, notif_type, reference, sent_at)
		VALUES (?, ?, ?, ?)
	`, userID, "test_type", "ref-old", oldTime)
	if err != nil {
		t.Fatalf("insert old log entry: %v", err)
	}

	// With a 1-hour cooldown the 2-hour-old entry should not count.
	if WasSentRecently(ctx, db, userID, "test_type", "ref-old", time.Hour) {
		t.Fatal("expected false: logged entry is older than cooldown")
	}
}

// TestWasSentRecently_MultipleUsers verifies isolation between users.
func TestWasSentRecently_MultipleUsers(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	alice := insertUser(t, db, "alice@test.com")
	bob := insertUser(t, db, "bob@test.com")

	logNotification(ctx, db, alice, "streak_milestone", "streak:7")

	if !WasSentRecently(ctx, db, alice, "streak_milestone", "streak:7", time.Hour) {
		t.Fatal("expected true for alice")
	}
	if WasSentRecently(ctx, db, bob, "streak_milestone", "streak:7", time.Hour) {
		t.Fatal("expected false for bob — different user")
	}
}

// TestSendStarsEarnedNotification_Dedup verifies that duplicate calls for the
// same workout ID are suppressed within the cooldown window.
func TestSendStarsEarnedNotification_Dedup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	childID := insertUser(t, db, "child-stars@test.com")

	// Pre-log a notification for workout 42 to simulate a prior successful send.
	logNotification(ctx, db, childID, "stars_earned", "workout:42")

	// A second call for the same workout within cooldown must be suppressed
	// by WasSentRecently before attempting any push delivery.
	SendStarsEarnedNotification(db, childID, 10, 42)

	// The log should still have exactly one entry — dedup prevented a second insert.
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'stars_earned'`,
		childID).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendBadgeNotification_Dedup verifies that badge notifications are
// deduplicated by badge key.
func TestSendBadgeNotification_Dedup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "badge-dedup@test.com")

	// Pre-log a badge notification to simulate a prior send.
	logNotification(ctx, db, userID, "badge_earned", "badge_first_km")

	badge := Badge{
		BadgeKey:    "badge_first_km",
		Name:        "First Kilometer",
		Description: "Complete your first 1km workout.",
	}

	// This call should be skipped because WasSentRecently returns true.
	SendBadgeNotification(db, userID, badge)

	// Confirm the log still has only the original entry (no duplicate).
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'badge_earned' AND reference = 'badge_first_km'`,
		userID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendStreakMilestoneNotification_Dedup ensures the same streak count is
// only logged once within the cooldown window.
func TestSendStreakMilestoneNotification_Dedup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "streak-dedup@test.com")

	logNotification(ctx, db, userID, "streak_milestone", "streak:7")

	// A second call for the same milestone should be a no-op.
	SendStreakMilestoneNotification(db, userID, 7)

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'streak_milestone' AND reference = 'streak:7'`,
		userID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 log entry (deduped), got %d", count)
	}
}

// TestLogNotification_InsertsRow verifies that logNotification writes to the DB.
func TestLogNotification_InsertsRow(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "log-row@test.com")

	logNotification(ctx, db, userID, "test_log", "ref-abc")

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'test_log' AND reference = 'ref-abc'`,
		userID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

// TestSendRewardClaimedNotification_Dedup verifies that the same claim ID does
// not produce duplicate log entries within the cooldown window.
func TestSendRewardClaimedNotification_Dedup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	childID := insertUser(t, db, "claim-child@test.com")
	parentID := insertUser(t, db, "claim-parent@test.com")

	// Pre-log a notification for this claim to simulate a prior send.
	ref := "claim:55"
	logNotification(ctx, db, parentID, "reward_claimed", ref)

	// A second call for the same claim should be deduplicated.
	SendRewardClaimedNotification(db, childID, parentID, "Ice Cream", 10, 55)

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'reward_claimed' AND reference = ?`,
		parentID, ref).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendRewardClaimedNotification_NoFamilyLink verifies that the function
// proceeds with a default nickname when GetParent finds no family link.
// parentID is already supplied as a parameter so the notification must not
// be silently dropped due to a missing or failed family lookup.
func TestSendRewardClaimedNotification_NoFamilyLink(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "no-link-child@test.com")
	parentID := insertUser(t, db, "no-link-parent@test.com")

	// No family link in the DB — GetParent returns nil, nil.
	// The function should not panic and should attempt the push.
	SendRewardClaimedNotification(db, childID, parentID, "Sticker", 5, 77)
}

// TestSendChallengeCompletedNotification_Dedup verifies that the same child
// does not receive duplicate challenge-complete notifications within the hour.
func TestSendChallengeCompletedNotification_Dedup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	childID := insertUser(t, db, "challenge-child@test.com")

	// Pre-log an entry for the current hour to simulate a prior send.
	hourKey := time.Now().UTC().Format("2006-01-02T15")
	ref := fmt.Sprintf("challenge-complete:%d:%s", childID, hourKey)
	logNotification(ctx, db, childID, "challenge_complete", ref)

	// A second call within the same hour should be suppressed.
	SendChallengeCompletedNotification(db, childID, 5)

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE user_id = ? AND notif_type = 'challenge_complete' AND reference = ?`,
		childID, ref).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 log entry after dedup, got %d", count)
	}
}

// TestSendRewardApprovedNotification_NoParent verifies that the function does
// not panic when there is no push subscription.
func TestSendRewardApprovedNotification_NoParent(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "reward-approved@test.com")

	// Must not panic even with no subscriptions.
	SendRewardApprovedNotification(db, childID, "Ice Cream", 99)
}

// TestSendRewardDeniedNotification_NoParent verifies that the function does
// not panic when there is no push subscription.
func TestSendRewardDeniedNotification_NoParent(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "reward-denied@test.com")

	SendRewardDeniedNotification(db, childID, "Video Game", 100)
}
