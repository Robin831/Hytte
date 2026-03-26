package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
)

// WorkoutInput contains the workout fields needed for star evaluation.
type WorkoutInput struct {
	ID              int64
	DurationSeconds int
	DistanceMeters  float64
	AvgHeartRate    int
	MaxHeartRate    int
	Calories        int
	AscentMeters    float64
	AvgPaceSecPerKm float64
	// HR time-series samples for zone analysis.
	Samples []HRSample
}

// HRSample is a single heart-rate data point with its offset from workout start.
type HRSample struct {
	OffsetMs  int64
	HeartRate int
}

// StarAward records stars earned from a single criterion.
type StarAward struct {
	Amount      int
	Reason      string
	Description string
}

// hrZone returns the HR zone (1-5) for a given heart rate given max HR.
// Zones: Z1 (50-60%), Z2 (60-70%), Z3 (70-80%), Z4 (80-90%), Z5 (90%+).
// Returns 0 if the HR is below 50% of max.
func hrZone(hr, maxHR int) int {
	if maxHR <= 0 || hr <= 0 {
		return 0
	}
	pct := float64(hr) / float64(maxHR) * 100
	switch {
	case pct < 50:
		return 0
	case pct < 60:
		return 1
	case pct < 70:
		return 2
	case pct < 80:
		return 3
	case pct < 90:
		return 4
	default:
		return 5
	}
}

// computeTimeInZones returns seconds spent in each HR zone (index 1-5).
// Each interval [i, i+1] is attributed to samples[i]'s HR zone.
func computeTimeInZones(samples []HRSample, maxHR int) [6]float64 {
	var zones [6]float64
	for i := 0; i < len(samples)-1; i++ {
		durationSec := float64(samples[i+1].OffsetMs-samples[i].OffsetMs) / 1000.0
		if durationSec <= 0 {
			continue
		}
		if samples[i].HeartRate <= 0 {
			continue
		}
		z := hrZone(samples[i].HeartRate, maxHR)
		if z >= 1 && z <= 5 {
			zones[z] += durationSec
		}
	}
	return zones
}

// EvaluateWorkout evaluates a completed workout and records star awards.
// Returns the list of awards granted (may be empty if no criteria met).
// Stars are only awarded to child users in a family link.
func EvaluateWorkout(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) ([]StarAward, error) {
	isChild, err := isChildUser(db, userID)
	if err != nil {
		return nil, err
	}
	if !isChild {
		return nil, nil
	}

	// Minimum workout duration: 10 minutes.
	if w.DurationSeconds < 600 {
		return nil, nil
	}

	// Resolve max HR: prefer user preference, fall back to workout max, then 190.
	maxHR := w.MaxHeartRate
	if prefs, prefErr := auth.GetPreferences(db, userID); prefErr == nil {
		if v, ok := prefs["max_hr"]; ok {
			if parsed, parseErr := strconv.Atoi(v); parseErr == nil && parsed > 0 {
				maxHR = parsed
			}
		}
	}
	if maxHR <= 0 {
		maxHR = 190
	}

	var awards []StarAward

	// Base: Showed Up — 2 stars for any qualifying workout.
	awards = append(awards, StarAward{
		Amount:      2,
		Reason:      "showed_up",
		Description: "Showed up and worked out!",
	})

	// Duration Bonus: +1 per 15 minutes, capped at +8 (2 hours).
	durationMin := w.DurationSeconds / 60
	durationBonus := min(durationMin/15, 8)
	if durationBonus > 0 {
		awards = append(awards, StarAward{
			Amount:      durationBonus,
			Reason:      "duration_bonus",
			Description: fmt.Sprintf("%d minute workout", durationMin),
		})
	}

	// Effort Bonus: +1 to +3 based on average HR zone.
	if w.AvgHeartRate > 0 {
		zone := hrZone(w.AvgHeartRate, maxHR)
		effortBonus := 0
		switch zone {
		case 2:
			effortBonus = 1
		case 3:
			effortBonus = 2
		case 4, 5:
			effortBonus = 3
		}
		if effortBonus > 0 {
			awards = append(awards, StarAward{
				Amount:      effortBonus,
				Reason:      "effort_bonus",
				Description: fmt.Sprintf("Zone %d effort", zone),
			})
		}
	}

	// Distance milestones.
	distAwards, err := checkDistanceMilestones(ctx, db, userID, w)
	if err != nil {
		log.Printf("stars: distance milestone check failed for user %d: %v", userID, err)
	} else {
		awards = append(awards, distAwards...)
	}

	// Personal records.
	prAwards, err := checkPersonalRecords(ctx, db, userID, w)
	if err != nil {
		log.Printf("stars: personal record check failed for user %d: %v", userID, err)
	} else {
		awards = append(awards, prAwards...)
	}

	// HR zone training awards.
	if len(w.Samples) >= 2 {
		zones := computeTimeInZones(w.Samples, maxHR)
		awards = append(awards, checkHRZoneAwards(zones, float64(w.DurationSeconds))...)
	}

	// Load workout start time for time-based and streak awards.
	var startedAt string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(started_at,'') FROM workouts WHERE id = ?`, w.ID).Scan(&startedAt)

	workoutDate := time.Now().UTC()
	if startedAt != "" {
		if t, parseErr := parseWorkoutTime(startedAt); parseErr == nil {
			workoutDate = t
		}
	}

	// Update daily and weekly streaks.
	if updateErr := UpdateStreak(ctx, db, userID, workoutDate); updateErr != nil {
		log.Printf("stars: UpdateStreak failed for user %d: %v", userID, updateErr)
	}

	// Consistency milestone stars (once per lifetime per milestone).
	consistencyAwards, cErr := checkConsistencyStars(ctx, db, userID)
	if cErr != nil {
		log.Printf("stars: consistency stars check failed for user %d: %v", userID, cErr)
	} else {
		awards = append(awards, consistencyAwards...)
	}

	// Time-of-day awards: Early Bird and Night Owl.
	awards = append(awards, checkTimeOfDayStars(w.ID, workoutDate)...)

	// Weekend Warrior: worked out on both Saturday and Sunday this week.
	weekendAwards, wErr := checkWeekendWarrior(ctx, db, userID, workoutDate)
	if wErr != nil {
		log.Printf("stars: weekend warrior check failed for user %d: %v", userID, wErr)
	} else {
		awards = append(awards, weekendAwards...)
	}

	if len(awards) == 0 {
		return nil, nil
	}

	if err := recordAwards(db, userID, w.ID, awards); err != nil {
		return nil, err
	}

	// Sum positive star amounts and award XP.
	totalXP := 0
	for _, a := range awards {
		if a.Amount > 0 {
			totalXP += a.Amount
		}
	}
	if totalXP > 0 {
		result, xpErr := AddXP(ctx, db, userID, totalXP)
		if xpErr != nil {
			log.Printf("stars: AddXP failed for user %d: %v", userID, xpErr)
		} else if result.DidLevelUp {
			go sendLevelUpNotifications(db, userID, result)
		}
	}

	return awards, nil
}

// pushClient is the HTTP client used for sending push notifications.
// It uses a 10-second timeout to prevent goroutine leaks on slow endpoints.
var pushClient = &http.Client{Timeout: 10 * time.Second}

// sendLevelUpNotifications sends push notifications to the child and their parent
// when the child levels up. Errors are logged and not propagated.
func sendLevelUpNotifications(db *sql.DB, childID int64, result *LevelUpResult) {
	childPayload := push.Notification{
		Title: "LEVEL UP!",
		Body:  fmt.Sprintf("You're now a %s (Level %d)! 🎉", result.NewTitle, result.NewLevel),
		Tag:   "level-up",
	}
	childBytes, err := json.Marshal(childPayload)
	if err != nil {
		log.Printf("stars: marshal child level-up payload: %v", err)
		return
	}

	if _, err := push.SendToUser(db, pushClient, childID, childBytes); err != nil {
		log.Printf("stars: send level-up push to child %d: %v", childID, err)
	}

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

	parentPayload := push.Notification{
		Title: fmt.Sprintf("%s leveled up!", nickname),
		Body:  fmt.Sprintf("%s is now Level %d — %s!", nickname, result.NewLevel, result.NewTitle),
		Tag:   "level-up",
	}
	parentBytes, err := json.Marshal(parentPayload)
	if err != nil {
		log.Printf("stars: marshal parent level-up payload: %v", err)
		return
	}

	if _, err := push.SendToUser(db, pushClient, link.ParentID, parentBytes); err != nil {
		log.Printf("stars: send level-up push to parent %d: %v", link.ParentID, err)
	}
}

// checkHRZoneAwards evaluates heart rate zone achievements.
func checkHRZoneAwards(zones [6]float64, totalSec float64) []StarAward {
	var awards []StarAward
	if totalSec <= 0 {
		return nil
	}

	// Zone Commander: 80%+ of workout in one target zone.
	for z := 1; z <= 5; z++ {
		if zones[z]/totalSec >= 0.80 {
			awards = append(awards, StarAward{
				Amount:      5,
				Reason:      "zone_commander",
				Description: fmt.Sprintf("80%% in Zone %d", z),
			})
			break
		}
	}

	// Zone Explorer: hit all 5 HR zones in one workout.
	allZones := true
	for z := 1; z <= 5; z++ {
		if zones[z] <= 0 {
			allZones = false
			break
		}
	}
	if allZones {
		awards = append(awards, StarAward{
			Amount:      8,
			Reason:      "zone_explorer",
			Description: "Hit all 5 HR zones",
		})
	}

	// Easy Day Hero: 95%+ of workout in Zone 1-2, with less than 30s in higher zones.
	z1z2 := zones[1] + zones[2]
	higherZones := zones[3] + zones[4] + zones[5]
	if z1z2/totalSec >= 0.95 && higherZones < 30 {
		awards = append(awards, StarAward{
			Amount:      3,
			Reason:      "easy_day_hero",
			Description: "Easy day in Zone 1-2",
		})
	}

	// Threshold Trainer: 20+ minutes (1200s) in Zone 4.
	if zones[4] >= 1200 {
		awards = append(awards, StarAward{
			Amount:      5,
			Reason:      "threshold_trainer",
			Description: "20+ minutes in Zone 4",
		})
	}

	return awards
}

// recordAwards inserts star transactions and updates the balance atomically.
func recordAwards(db *sql.DB, userID, workoutID int64, awards []StarAward) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	totalAmount := 0

	for _, a := range awards {
		_, err := tx.Exec(`
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, userID, a.Amount, a.Reason, a.Description, workoutID, now)
		if err != nil {
			return err
		}
		totalAmount += a.Amount
	}

	_, err = tx.Exec(`
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, totalAmount)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// isChildUser returns true if userID is linked as a child in family_links.
func isChildUser(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM family_links WHERE child_id = ?`, userID).Scan(&count)
	return count > 0, err
}
