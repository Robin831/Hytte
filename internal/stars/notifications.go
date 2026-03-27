package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// WasSentRecently checks the notification_log to determine whether a notification
// of the given type and reference was sent to the user within the cooldown duration.
// Returns false on DB errors so the notification is sent rather than silently dropped.
func WasSentRecently(ctx context.Context, db *sql.DB, userID int64, notifType, reference string, cooldown time.Duration) bool {
	cutoff := time.Now().UTC().Add(-cooldown).Format(time.RFC3339)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM notification_log
		WHERE user_id = ? AND notif_type = ? AND reference = ? AND sent_at > ?
	`, userID, notifType, reference, cutoff).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// logNotification records a sent notification in the log for future deduplication.
func logNotification(ctx context.Context, db *sql.DB, userID int64, notifType, reference string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
		INSERT INTO notification_log (user_id, notif_type, reference, sent_at)
		VALUES (?, ?, ?, ?)
	`, userID, notifType, reference, now)
	if err != nil {
		log.Printf("stars: log notification %s for user %d: %v", notifType, userID, err)
	}
}

// dispatchPush marshals a push.Notification and delivers it to the user,
// checking quiet hours first. Returns false if quiet hours are active or the
// send failed.
func dispatchPush(db *sql.DB, userID int64, n push.Notification) bool {
	if quiethours.IsActive(db, userID) {
		return false
	}
	data, err := json.Marshal(n)
	if err != nil {
		log.Printf("stars: marshal push notification for user %d: %v", userID, err)
		return false
	}
	if _, err := push.SendToUser(db, pushClient, userID, data); err != nil {
		log.Printf("stars: send push to user %d: %v", userID, err)
		return false
	}
	return true
}

// SendStarsEarnedNotification sends a push notification to a child when they earn
// stars from a workout. A 1-hour cooldown per workout prevents duplicate notifications
// if evaluation is re-run.
func SendStarsEarnedNotification(db *sql.DB, childID int64, amount int, workoutID int64) {
	ctx := context.Background()
	ref := fmt.Sprintf("workout:%d", workoutID)
	if WasSentRecently(ctx, db, childID, "stars_earned", ref, time.Hour) {
		return
	}
	var body string
	if amount == 1 {
		body = "You earned 1 star from your workout!"
	} else {
		body = fmt.Sprintf("You earned %d stars from your workout!", amount)
	}
	sent := dispatchPush(db, childID, push.Notification{
		Title: "Stars Earned!",
		Body:  body,
		Tag:   "stars-earned",
	})
	if sent {
		logNotification(ctx, db, childID, "stars_earned", ref)
	}
}

// SendLevelUpNotification sends push notifications to the child and their parent
// when the child reaches a new level.
func SendLevelUpNotification(db *sql.DB, childID int64, result *LevelUpResult) {
	ctx := context.Background()
	ref := fmt.Sprintf("level:%d", result.NewLevel)

	// Notify the child — deduplicated per level so reruns don't spam.
	if !WasSentRecently(ctx, db, childID, "level_up", ref, 24*time.Hour) {
		sent := dispatchPush(db, childID, push.Notification{
			Title: "LEVEL UP!",
			Body:  fmt.Sprintf("You're now a %s (Level %d)!", result.NewTitle, result.NewLevel),
			Tag:   "level-up",
		})
		if sent {
			logNotification(ctx, db, childID, "level_up", ref)
		}
	}

	// Notify the parent.
	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent for child %d: %v", childID, err)
		return
	}
	if link == nil {
		return
	}

	parentRef := fmt.Sprintf("child:%d:level:%d", childID, result.NewLevel)
	if !WasSentRecently(ctx, db, link.ParentID, "level_up_parent", parentRef, 24*time.Hour) {
		nickname := link.Nickname
		if nickname == "" {
			nickname = "Your child"
		}
		sent := dispatchPush(db, link.ParentID, push.Notification{
			Title: fmt.Sprintf("%s leveled up!", nickname),
			Body:  fmt.Sprintf("%s is now Level %d — %s!", nickname, result.NewLevel, result.NewTitle),
			Tag:   "level-up",
		})
		if sent {
			logNotification(ctx, db, link.ParentID, "level_up_parent", parentRef)
		}
	}
}

// SendBadgeNotification sends a push notification to the user when they earn a badge.
// The badge key acts as the deduplication reference so a given badge is only notified once.
func SendBadgeNotification(db *sql.DB, userID int64, badge Badge) {
	ctx := context.Background()
	if WasSentRecently(ctx, db, userID, "badge_earned", badge.BadgeKey, 24*time.Hour) {
		return
	}
	sent := dispatchPush(db, userID, push.Notification{
		Title: "New Badge Earned!",
		Body:  badge.Name + " — " + badge.Description,
		Icon:  "/icon-192.png",
		Tag:   badge.BadgeKey,
	})
	if sent {
		logNotification(ctx, db, userID, "badge_earned", badge.BadgeKey)
	}
}

// SendStreakMilestoneNotification sends a push notification when a child reaches
// a notable workout streak length. Milestones: 3, 7, 14, 30, and every 30 thereafter.
func SendStreakMilestoneNotification(db *sql.DB, userID int64, streak int) {
	ctx := context.Background()
	ref := fmt.Sprintf("streak:%d", streak)
	if WasSentRecently(ctx, db, userID, "streak_milestone", ref, 24*time.Hour) {
		return
	}
	body := fmt.Sprintf("You've worked out %d days in a row! Keep it up!", streak)
	sent := dispatchPush(db, userID, push.Notification{
		Title: fmt.Sprintf("%d Day Streak!", streak),
		Body:  body,
		Tag:   "streak-milestone",
	})
	if sent {
		logNotification(ctx, db, userID, "streak_milestone", ref)
	}
}

// SendRewardClaimedNotification notifies the parent that a child has claimed a reward,
// using a short deduplication window to handle accidental double-taps.
func SendRewardClaimedNotification(db *sql.DB, childID, parentID int64, rewardTitle string, starsSpent int, claimID int64) {
	ctx := context.Background()
	ref := fmt.Sprintf("claim:%d", claimID)
	if WasSentRecently(ctx, db, parentID, "reward_claimed", ref, time.Hour) {
		return
	}

	// Find the child's nickname for the parent notification.
	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent link for child %d: %v", childID, err)
		return
	}
	nickname := "Your child"
	if link != nil && link.Nickname != "" {
		nickname = link.Nickname
	}

	sent := dispatchPush(db, parentID, push.Notification{
		Title: "Reward Requested!",
		Body:  fmt.Sprintf("%s wants to claim %q (%d stars). Review the request!", nickname, rewardTitle, starsSpent),
		Tag:   "reward-claimed",
	})
	if sent {
		logNotification(ctx, db, parentID, "reward_claimed", ref)
	}
}

// SendRewardApprovedNotification notifies the child that their reward claim was approved.
func SendRewardApprovedNotification(db *sql.DB, childID int64, rewardTitle string, claimID int64) {
	ctx := context.Background()
	ref := fmt.Sprintf("claim:%d", claimID)
	if WasSentRecently(ctx, db, childID, "reward_approved", ref, time.Hour) {
		return
	}
	sent := dispatchPush(db, childID, push.Notification{
		Title: "Reward Approved!",
		Body:  fmt.Sprintf("Your claim for %q has been approved!", rewardTitle),
		Tag:   "reward-approved",
	})
	if sent {
		logNotification(ctx, db, childID, "reward_approved", ref)
	}
}

// SendRewardDeniedNotification notifies the child that their reward claim was denied.
func SendRewardDeniedNotification(db *sql.DB, childID int64, rewardTitle string, claimID int64) {
	ctx := context.Background()
	ref := fmt.Sprintf("claim:%d", claimID)
	if WasSentRecently(ctx, db, childID, "reward_denied", ref, time.Hour) {
		return
	}
	sent := dispatchPush(db, childID, push.Notification{
		Title: "Reward Not Approved",
		Body:  fmt.Sprintf("Your claim for %q was not approved this time.", rewardTitle),
		Tag:   "reward-denied",
	})
	if sent {
		logNotification(ctx, db, childID, "reward_denied", ref)
	}
}

// SendChallengeCompletedNotification sends push notifications to the child and their
// parent when the child completes a challenge.
func SendChallengeCompletedNotification(db *sql.DB, childID int64, starReward int) {
	ctx := context.Background()

	// Child notification — use a short cooldown so the same completion can't
	// trigger duplicate alerts if the event fires twice in quick succession.
	ref := fmt.Sprintf("challenge-complete:%d:%s", childID, time.Now().UTC().Format("2006-01-02T15"))
	if !WasSentRecently(ctx, db, childID, "challenge_complete", ref, time.Hour) {
		var childBody string
		if starReward > 0 {
			childBody = fmt.Sprintf("You earned %d stars for completing a challenge!", starReward)
		} else {
			childBody = "You completed a challenge!"
		}
		sent := dispatchPush(db, childID, push.Notification{
			Title: "Challenge Complete!",
			Body:  childBody,
			Tag:   "challenge-complete",
		})
		if sent {
			logNotification(ctx, db, childID, "challenge_complete", ref)
		}
	}

	// Parent notification.
	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent for child %d: %v", childID, err)
		return
	}
	if link == nil {
		return
	}

	nickname := link.Nickname
	if nickname == "" {
		nickname = "Your child"
	}
	parentRef := fmt.Sprintf("challenge-complete:parent:%d:%s", childID, time.Now().UTC().Format("2006-01-02T15"))
	if !WasSentRecently(ctx, db, link.ParentID, "challenge_complete_parent", parentRef, time.Hour) {
		var parentBody string
		if starReward > 0 {
			parentBody = fmt.Sprintf("%s earned %d stars!", nickname, starReward)
		} else {
			parentBody = fmt.Sprintf("%s completed a challenge!", nickname)
		}
		sent := dispatchPush(db, link.ParentID, push.Notification{
			Title: fmt.Sprintf("%s completed a challenge!", nickname),
			Body:  parentBody,
			Tag:   "challenge-complete",
		})
		if sent {
			logNotification(ctx, db, link.ParentID, "challenge_complete_parent", parentRef)
		}
	}
}

// SendFamilyWorkoutNotification notifies the parent when their child completes a workout.
// A 1-hour cooldown per child prevents alert fatigue from multiple rapid workouts.
func SendFamilyWorkoutNotification(db *sql.DB, childID int64) {
	ctx := context.Background()

	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent for child %d: %v", childID, err)
		return
	}
	if link == nil {
		return
	}

	// Hourly cooldown keyed on child so multiple children don't suppress each other.
	ref := fmt.Sprintf("child:%d:workout:%s", childID, time.Now().UTC().Format("2006-01-02T15"))
	if WasSentRecently(ctx, db, link.ParentID, "family_workout", ref, time.Hour) {
		return
	}

	nickname := link.Nickname
	if nickname == "" {
		nickname = "Your child"
	}
	sent := dispatchPush(db, link.ParentID, push.Notification{
		Title: fmt.Sprintf("%s worked out!", nickname),
		Body:  fmt.Sprintf("%s just logged a workout. Great job!", nickname),
		Tag:   "family-workout",
	})
	if sent {
		logNotification(ctx, db, link.ParentID, "family_workout", ref)
	}
}
