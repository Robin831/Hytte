package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
)

// Badge represents an achievement badge earned by a user.
type Badge struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	BadgeKey    string `json:"badge_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
	XPReward    int    `json:"xp_reward"`
	EarnedAt    string `json:"earned_at"`
	WorkoutID   *int64 `json:"workout_id,omitempty"`
}

type badgeDef struct {
	Key         string
	Name        string
	Description string
	Category    string
	Icon        string
	XPReward    int
}

// allBadges lists all badge definitions used by SeedBadges.
// Categories: distance, consistency, speed, variety, heart, fun, secret.
var allBadges = []badgeDef{
	// --- Distance (7) ---
	{Key: "badge_first_km", Name: "First Kilometer", Description: "Complete your first 1km workout.", Category: "distance", Icon: "🏃", XPReward: 5},
	{Key: "badge_5k", Name: "5K Finisher", Description: "Complete a workout covering 5km.", Category: "distance", Icon: "🥈", XPReward: 10},
	{Key: "badge_10k", Name: "10K Hero", Description: "Complete a workout covering 10km.", Category: "distance", Icon: "🥇", XPReward: 20},
	{Key: "badge_half_marathon", Name: "Half Marathon", Description: "Complete a half marathon distance (21.1km).", Category: "distance", Icon: "🦸", XPReward: 40},
	{Key: "badge_marathon", Name: "Marathon", Description: "Complete a full marathon distance (42.2km).", Category: "distance", Icon: "🏅", XPReward: 80},
	{Key: "badge_ultramarathon", Name: "Ultra Distance", Description: "Complete 50km or more in a single workout.", Category: "distance", Icon: "🌟", XPReward: 100},
	{Key: "badge_century_cumulative", Name: "Century Club", Description: "Accumulate 100km of total distance.", Category: "distance", Icon: "🌍", XPReward: 30},

	// --- Consistency (7) ---
	{Key: "badge_streak_3", Name: "On Fire", Description: "Achieve a 3-day workout streak.", Category: "consistency", Icon: "🔥", XPReward: 10},
	{Key: "badge_streak_7", Name: "Week Warrior", Description: "Achieve a 7-day workout streak.", Category: "consistency", Icon: "🌟", XPReward: 25},
	{Key: "badge_streak_14", Name: "Fortnight Fighter", Description: "Achieve a 14-day workout streak.", Category: "consistency", Icon: "💪", XPReward: 50},
	{Key: "badge_streak_30", Name: "Monthly Marvel", Description: "Achieve a 30-day workout streak.", Category: "consistency", Icon: "🏆", XPReward: 100},
	{Key: "badge_25_workouts", Name: "Regular", Description: "Complete 25 workouts.", Category: "consistency", Icon: "🎯", XPReward: 20},
	{Key: "badge_50_workouts", Name: "Dedicated", Description: "Complete 50 workouts.", Category: "consistency", Icon: "🌠", XPReward: 40},
	{Key: "badge_100_workouts", Name: "Centurion", Description: "Complete 100 workouts.", Category: "consistency", Icon: "💫", XPReward: 80},

	// --- Speed (6) ---
	{Key: "badge_sub_6_pace", Name: "Quick Pacer", Description: "Average pace under 6:00 min/km.", Category: "speed", Icon: "🐇", XPReward: 10},
	{Key: "badge_sub_5_pace", Name: "Fast Runner", Description: "Average pace under 5:00 min/km.", Category: "speed", Icon: "⚡", XPReward: 20},
	{Key: "badge_sub_4_30_pace", Name: "Speed Racer", Description: "Average pace under 4:30 min/km.", Category: "speed", Icon: "🚀", XPReward: 30},
	{Key: "badge_sub_4_pace", Name: "Rocket Runner", Description: "Average pace under 4:00 min/km.", Category: "speed", Icon: "💨", XPReward: 40},
	{Key: "badge_sub_3_30_pace", Name: "Lightning", Description: "Average pace under 3:30 min/km.", Category: "speed", Icon: "🌩️", XPReward: 60},
	{Key: "badge_speed_demon", Name: "Speed Demon", Description: "Average pace under 3:00 min/km.", Category: "speed", Icon: "🏎️", XPReward: 100},

	// --- Variety (6) ---
	{Key: "badge_2_sports", Name: "Multisport Starter", Description: "Complete workouts in 2 different sports.", Category: "variety", Icon: "🎭", XPReward: 10},
	{Key: "badge_3_sports", Name: "Triathlete Spirit", Description: "Complete workouts in 3 different sports.", Category: "variety", Icon: "🎪", XPReward: 20},
	{Key: "badge_5_sports", Name: "All-Rounder", Description: "Complete workouts in 5 different sports.", Category: "variety", Icon: "🌈", XPReward: 40},
	{Key: "badge_cross_trainer_week", Name: "Cross Trainer", Description: "Complete 3 different sports in one week.", Category: "variety", Icon: "🔄", XPReward: 25},
	{Key: "badge_indoor_explorer", Name: "Indoor Explorer", Description: "Complete 5 indoor workouts.", Category: "variety", Icon: "🏋️", XPReward: 15},
	{Key: "badge_outdoor_adventurer", Name: "Outdoor Adventurer", Description: "Complete 10 outdoor workouts.", Category: "variety", Icon: "🌿", XPReward: 20},

	// --- Heart (7) ---
	{Key: "badge_zone_commander", Name: "Zone Commander", Description: "Spend 80% or more of a workout in a single HR zone.", Category: "heart", Icon: "🎯", XPReward: 15},
	{Key: "badge_zone_explorer", Name: "Zone Explorer", Description: "Hit all 5 HR zones in a single workout.", Category: "heart", Icon: "🗺️", XPReward: 20},
	{Key: "badge_easy_day_hero", Name: "Easy Day Hero", Description: "Complete a full workout staying in Zone 1-2.", Category: "heart", Icon: "😌", XPReward: 10},
	{Key: "badge_threshold_master", Name: "Threshold Master", Description: "Spend 20 or more minutes in Zone 4.", Category: "heart", Icon: "💪", XPReward: 25},
	{Key: "badge_red_zone", Name: "Red Zone", Description: "Reach Zone 5 heart rate in a workout.", Category: "heart", Icon: "🔴", XPReward: 15},
	{Key: "badge_big_heart", Name: "Big Heart", Description: "Average HR over 150bpm in a 60-minute or longer workout.", Category: "heart", Icon: "❤️", XPReward: 20},
	{Key: "badge_hr_warrior", Name: "HR Warrior", Description: "Max heart rate over 190bpm in a workout.", Category: "heart", Icon: "💗", XPReward: 15},

	// --- Fun (6) ---
	{Key: "badge_early_bird", Name: "Early Bird", Description: "Complete a workout before 6:00 AM.", Category: "fun", Icon: "🌅", XPReward: 15},
	{Key: "badge_night_owl", Name: "Night Owl", Description: "Complete a workout after 10:00 PM.", Category: "fun", Icon: "🦉", XPReward: 15},
	{Key: "badge_calorie_crusher", Name: "Calorie Crusher", Description: "Burn 1000 or more calories in one workout.", Category: "fun", Icon: "🔥", XPReward: 20},
	{Key: "badge_mountain_climber", Name: "Mountain Climber", Description: "Accumulate 1000m or more of ascent in one workout.", Category: "fun", Icon: "⛰️", XPReward: 25},
	{Key: "badge_summit_seeker", Name: "Summit Seeker", Description: "Accumulate 2000m or more of ascent in one workout.", Category: "fun", Icon: "🏔️", XPReward: 50},
	{Key: "badge_long_hauler", Name: "Long Hauler", Description: "Complete a workout lasting 3 or more hours.", Category: "fun", Icon: "⏱️", XPReward: 20},

	// --- Secret (6) ---
	{Key: "badge_christmas_spirit", Name: "Christmas Spirit", Description: "Complete a workout on December 25.", Category: "secret", Icon: "🎄", XPReward: 25},
	{Key: "badge_new_year_hero", Name: "New Year Hero", Description: "Complete a workout on January 1.", Category: "secret", Icon: "🎆", XPReward: 25},
	{Key: "badge_palindrome_pacer", Name: "Palindrome Pacer", Description: "Run at a pace whose min:sec digits read the same forwards and backwards.", Category: "secret", Icon: "🔢", XPReward: 20},
	{Key: "badge_perfect_hour", Name: "Perfect Hour", Description: "Complete a workout lasting almost exactly 1 hour.", Category: "secret", Icon: "⏰", XPReward: 20},
	{Key: "badge_lucky_7", Name: "Lucky Seven", Description: "Complete a workout of almost exactly 7.0km.", Category: "secret", Icon: "🍀", XPReward: 20},
	{Key: "badge_midnight_runner", Name: "Midnight Runner", Description: "Start a workout right around midnight.", Category: "secret", Icon: "🌙", XPReward: 20},
}

// SeedBadges inserts all badge definitions into badge_definitions using
// INSERT OR IGNORE on the key column so re-runs are idempotent.
func SeedBadges(db *sql.DB) error {
	for _, b := range allBadges {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO badge_definitions (key, name, description, category, icon, xp_reward)
			VALUES (?, ?, ?, ?, ?, ?)`,
			b.Key, b.Name, b.Description, b.Category, b.Icon, b.XPReward)
		if err != nil {
			return fmt.Errorf("seed badge %s: %w", b.Key, err)
		}
	}
	return nil
}

// EvaluateBadges checks which badges the user has newly earned from the given
// workout, inserts them into user_badges, awards XP, and fires push
// notifications. Only badges not already held are returned.
func EvaluateBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) ([]Badge, error) {
	// Load all badge keys the user already holds in one query.
	earned, err := loadUserBadgeKeys(ctx, db, userID)
	if err != nil {
		return nil, fmt.Errorf("load user badges: %w", err)
	}

	// Load workout metadata (started_at) needed by fun/secret checkers.
	var startedAt string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(started_at,'') FROM workouts WHERE id = ?`, w.ID).
		Scan(&startedAt)

	var newKeys []string

	distKeys, err := checkDistanceBadges(ctx, db, userID, w, earned)
	if err != nil {
		return nil, fmt.Errorf("distance badges: %w", err)
	}
	newKeys = append(newKeys, distKeys...)

	consKeys, err := checkConsistencyBadges(ctx, db, userID, w, earned)
	if err != nil {
		return nil, fmt.Errorf("consistency badges: %w", err)
	}
	newKeys = append(newKeys, consKeys...)

	speedKeys, err := checkSpeedBadges(ctx, db, userID, w, earned)
	if err != nil {
		return nil, fmt.Errorf("speed badges: %w", err)
	}
	newKeys = append(newKeys, speedKeys...)

	varKeys, err := checkVarietyBadges(ctx, db, userID, w, earned)
	if err != nil {
		return nil, fmt.Errorf("variety badges: %w", err)
	}
	newKeys = append(newKeys, varKeys...)

	heartKeys, err := checkHeartBadges(ctx, db, userID, w, earned)
	if err != nil {
		return nil, fmt.Errorf("heart badges: %w", err)
	}
	newKeys = append(newKeys, heartKeys...)

	funKeys, err := checkFunBadges(ctx, db, userID, w, startedAt, earned)
	if err != nil {
		return nil, fmt.Errorf("fun badges: %w", err)
	}
	newKeys = append(newKeys, funKeys...)

	secretKeys, err := checkSecretBadges(ctx, db, userID, w, startedAt, earned)
	if err != nil {
		return nil, fmt.Errorf("secret badges: %w", err)
	}
	newKeys = append(newKeys, secretKeys...)

	// Persist and notify for each newly earned badge.
	awarded := make([]Badge, 0)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, key := range newKeys {
		badge, err := awardBadge(ctx, db, userID, key, w.ID, now)
		if err != nil {
			log.Printf("stars: award badge %s to user %d: %v", key, userID, err)
			continue
		}
		awarded = append(awarded, badge)
		go sendBadgeNotification(db, userID, badge)
	}

	return awarded, nil
}

// loadUserBadgeKeys returns a set of badge keys the user has already earned.
func loadUserBadgeKeys(ctx context.Context, db *sql.DB, userID int64) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT badge_key FROM user_badges WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	earned := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		earned[key] = true
	}
	return earned, rows.Err()
}

// awardBadge inserts the badge into user_badges, awards XP, and returns the
// full Badge with definition fields populated.
func awardBadge(ctx context.Context, db *sql.DB, userID int64, badgeKey string, workoutID int64, now string) (Badge, error) {
	var widArg interface{}
	if workoutID > 0 {
		widArg = workoutID
	}
	result, err := db.ExecContext(ctx,
		`INSERT INTO user_badges (user_id, badge_key, workout_id, earned_at) VALUES (?, ?, ?, ?)`,
		userID, badgeKey, widArg, now)
	if err != nil {
		return Badge{}, fmt.Errorf("insert user_badge %s: %w", badgeKey, err)
	}

	badgeID, err := result.LastInsertId()
	if err != nil {
		return Badge{}, fmt.Errorf("last insert id: %w", err)
	}

	var b Badge
	var wid sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT ub.id, ub.user_id, ub.badge_key,
		       bd.name, bd.description, bd.category, bd.icon, bd.xp_reward,
		       ub.earned_at, ub.workout_id
		FROM user_badges ub
		JOIN badge_definitions bd ON bd.key = ub.badge_key
		WHERE ub.id = ?`, badgeID).
		Scan(&b.ID, &b.UserID, &b.BadgeKey,
			&b.Name, &b.Description, &b.Category, &b.Icon, &b.XPReward,
			&b.EarnedAt, &wid)
	if err != nil {
		return Badge{}, fmt.Errorf("load awarded badge: %w", err)
	}
	if wid.Valid {
		b.WorkoutID = &wid.Int64
	}

	// Award XP bonus for earning the badge.
	if b.XPReward > 0 {
		if _, err := AddXP(ctx, db, userID, b.XPReward); err != nil {
			log.Printf("stars: award xp for badge %s to user %d: %v", badgeKey, userID, err)
		}
	}

	return b, nil
}

// sendBadgeNotification fires an async push notification to the user.
func sendBadgeNotification(db *sql.DB, userID int64, b Badge) {
	payload := push.Notification{
		Title: "New Badge Earned!",
		Body:  b.Name + " — " + b.Description,
		Icon:  "/icon-192.png",
		Tag:   b.BadgeKey,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("stars: marshal badge notification: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, userID, data); err != nil {
		log.Printf("stars: send badge push to user %d: %v", userID, err)
	}
}

// checkDistanceBadges evaluates per-workout and cumulative distance badges.
func checkDistanceBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, earned map[string]bool) ([]string, error) {
	var keys []string

	type distBadge struct {
		key       string
		minMeters float64
	}
	thresholds := []distBadge{
		{"badge_ultramarathon", 50000},
		{"badge_marathon", 42195},
		{"badge_half_marathon", 21100},
		{"badge_10k", 10000},
		{"badge_5k", 5000},
		{"badge_first_km", 1000},
	}
	for _, t := range thresholds {
		if !earned[t.key] && w.DistanceMeters >= t.minMeters {
			keys = append(keys, t.key)
		}
	}

	// Cumulative 100km badge — query includes the current workout.
	if !earned["badge_century_cumulative"] {
		var total float64
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(distance_meters), 0) FROM workouts WHERE user_id = ?`, userID).
			Scan(&total); err != nil {
			return nil, err
		}
		if total >= 100000 {
			keys = append(keys, "badge_century_cumulative")
		}
	}

	return keys, nil
}

// checkConsistencyBadges evaluates streak and total-workout-count badges.
func checkConsistencyBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, earned map[string]bool) ([]string, error) {
	var keys []string

	// Streak badges — read the daily_workout streak current count.
	if !earned["badge_streak_3"] || !earned["badge_streak_7"] || !earned["badge_streak_14"] || !earned["badge_streak_30"] {
		var current int
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(current_count, 0) FROM streaks WHERE user_id = ? AND streak_type = 'daily_workout'`,
			userID).Scan(&current); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if !earned["badge_streak_3"] && current >= 3 {
			keys = append(keys, "badge_streak_3")
		}
		if !earned["badge_streak_7"] && current >= 7 {
			keys = append(keys, "badge_streak_7")
		}
		if !earned["badge_streak_14"] && current >= 14 {
			keys = append(keys, "badge_streak_14")
		}
		if !earned["badge_streak_30"] && current >= 30 {
			keys = append(keys, "badge_streak_30")
		}
	}

	// Total workout count badges.
	if !earned["badge_25_workouts"] || !earned["badge_50_workouts"] || !earned["badge_100_workouts"] {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM workouts WHERE user_id = ?`, userID).Scan(&count); err != nil {
			return nil, err
		}
		if !earned["badge_25_workouts"] && count >= 25 {
			keys = append(keys, "badge_25_workouts")
		}
		if !earned["badge_50_workouts"] && count >= 50 {
			keys = append(keys, "badge_50_workouts")
		}
		if !earned["badge_100_workouts"] && count >= 100 {
			keys = append(keys, "badge_100_workouts")
		}
	}

	return keys, nil
}

// checkSpeedBadges evaluates average-pace thresholds.
// Only meaningful for workouts with distance > 2km and a valid pace.
func checkSpeedBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, earned map[string]bool) ([]string, error) {
	if w.DistanceMeters < 2000 || w.AvgPaceSecPerKm <= 0 {
		return nil, nil
	}

	type speedBadge struct {
		key        string
		maxPaceSec float64
	}
	thresholds := []speedBadge{
		{"badge_speed_demon", 180},
		{"badge_sub_3_30_pace", 210},
		{"badge_sub_4_pace", 240},
		{"badge_sub_4_30_pace", 270},
		{"badge_sub_5_pace", 300},
		{"badge_sub_6_pace", 360},
	}

	var keys []string
	for _, t := range thresholds {
		if !earned[t.key] && w.AvgPaceSecPerKm < t.maxPaceSec {
			keys = append(keys, t.key)
		}
	}
	return keys, nil
}

// checkVarietyBadges evaluates sport diversity and indoor/outdoor badges.
func checkVarietyBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, earned map[string]bool) ([]string, error) {
	var keys []string

	// Distinct sport count badges.
	if !earned["badge_2_sports"] || !earned["badge_3_sports"] || !earned["badge_5_sports"] {
		var sportCount int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(DISTINCT sport) FROM workouts WHERE user_id = ?`, userID).
			Scan(&sportCount); err != nil {
			return nil, err
		}
		if !earned["badge_2_sports"] && sportCount >= 2 {
			keys = append(keys, "badge_2_sports")
		}
		if !earned["badge_3_sports"] && sportCount >= 3 {
			keys = append(keys, "badge_3_sports")
		}
		if !earned["badge_5_sports"] && sportCount >= 5 {
			keys = append(keys, "badge_5_sports")
		}
	}

	// Cross trainer: 3 different sports in the last 7 days.
	if !earned["badge_cross_trainer_week"] {
		weekAgo := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
		var weekSports int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(DISTINCT sport) FROM workouts WHERE user_id = ? AND started_at >= ?`,
			userID, weekAgo).Scan(&weekSports); err != nil {
			return nil, err
		}
		if weekSports >= 3 {
			keys = append(keys, "badge_cross_trainer_week")
		}
	}

	// Indoor workout count.
	if !earned["badge_indoor_explorer"] {
		var indoorCount int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM workouts WHERE user_id = ? AND is_indoor = 1`, userID).
			Scan(&indoorCount); err != nil {
			return nil, err
		}
		if indoorCount >= 5 {
			keys = append(keys, "badge_indoor_explorer")
		}
	}

	// Outdoor workout count.
	if !earned["badge_outdoor_adventurer"] {
		var outdoorCount int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM workouts WHERE user_id = ? AND is_indoor = 0`, userID).
			Scan(&outdoorCount); err != nil {
			return nil, err
		}
		if outdoorCount >= 10 {
			keys = append(keys, "badge_outdoor_adventurer")
		}
	}

	return keys, nil
}

// checkHeartBadges evaluates HR zone and heart-rate-based badges.
func checkHeartBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, earned map[string]bool) ([]string, error) {
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

	var keys []string

	if len(w.Samples) > 1 {
		zones := computeTimeInZones(w.Samples, maxHR)
		totalSec := zones[1] + zones[2] + zones[3] + zones[4] + zones[5]

		if totalSec > 0 {
			// Zone commander: 80%+ of workout in a single zone.
			if !earned["badge_zone_commander"] {
				for z := 1; z <= 5; z++ {
					if zones[z]/totalSec >= 0.80 {
						keys = append(keys, "badge_zone_commander")
						break
					}
				}
			}

			// Zone explorer: hit every zone (at least 10 seconds each).
			if !earned["badge_zone_explorer"] {
				allHit := true
				for z := 1; z <= 5; z++ {
					if zones[z] < 10 {
						allHit = false
						break
					}
				}
				if allHit {
					keys = append(keys, "badge_zone_explorer")
				}
			}

			// Easy day hero: nearly all time in Z1-Z2.
			if !earned["badge_easy_day_hero"] {
				easyTime := zones[1] + zones[2]
				hardTime := zones[3] + zones[4] + zones[5]
				// At least 10 min easy and less than 30s above Z2.
				if easyTime >= 600 && hardTime < 30 {
					keys = append(keys, "badge_easy_day_hero")
				}
			}

			// Threshold master: 20+ minutes in Z4.
			if !earned["badge_threshold_master"] && zones[4] >= 1200 {
				keys = append(keys, "badge_threshold_master")
			}

			// Red zone: any time in Z5.
			if !earned["badge_red_zone"] && zones[5] > 0 {
				keys = append(keys, "badge_red_zone")
			}
		}
	}

	// Big heart: average HR > 150 in a 60-minute or longer workout.
	if !earned["badge_big_heart"] && w.AvgHeartRate > 150 && w.DurationSeconds >= 3600 {
		keys = append(keys, "badge_big_heart")
	}

	// HR warrior: max HR > 190.
	if !earned["badge_hr_warrior"] && w.MaxHeartRate > 190 {
		keys = append(keys, "badge_hr_warrior")
	}

	return keys, nil
}

// checkFunBadges evaluates time-of-day, calorie, ascent, and duration badges.
func checkFunBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, startedAt string, earned map[string]bool) ([]string, error) {
	var keys []string

	if startedAt != "" {
		t, err := parseWorkoutTime(startedAt)
		if err == nil {
			hour := t.Hour()
			if !earned["badge_early_bird"] && hour < 6 {
				keys = append(keys, "badge_early_bird")
			}
			if !earned["badge_night_owl"] && hour >= 22 {
				keys = append(keys, "badge_night_owl")
			}
		}
	}

	if !earned["badge_calorie_crusher"] && w.Calories >= 1000 {
		keys = append(keys, "badge_calorie_crusher")
	}

	if !earned["badge_mountain_climber"] && w.AscentMeters >= 1000 {
		keys = append(keys, "badge_mountain_climber")
	}
	if !earned["badge_summit_seeker"] && w.AscentMeters >= 2000 {
		keys = append(keys, "badge_summit_seeker")
	}

	if !earned["badge_long_hauler"] && w.DurationSeconds >= 10800 {
		keys = append(keys, "badge_long_hauler")
	}

	return keys, nil
}

// checkSecretBadges evaluates hidden achievement badges.
func checkSecretBadges(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput, startedAt string, earned map[string]bool) ([]string, error) {
	var keys []string

	if startedAt != "" {
		t, err := parseWorkoutTime(startedAt)
		if err == nil {
			month := t.Month()
			day := t.Day()
			hour := t.Hour()
			minute := t.Minute()

			if !earned["badge_christmas_spirit"] && month == 12 && day == 25 {
				keys = append(keys, "badge_christmas_spirit")
			}
			if !earned["badge_new_year_hero"] && month == 1 && day == 1 {
				keys = append(keys, "badge_new_year_hero")
			}

			// Midnight runner: workout starts within 15 minutes of midnight.
			if !earned["badge_midnight_runner"] {
				totalMin := hour*60 + minute
				if totalMin <= 15 || totalMin >= 24*60-15 {
					keys = append(keys, "badge_midnight_runner")
				}
			}
		}
	}

	// Perfect hour: duration within 30 seconds of exactly 3600s.
	if !earned["badge_perfect_hour"] {
		diff := w.DurationSeconds - 3600
		if diff < 0 {
			diff = -diff
		}
		if diff <= 30 {
			keys = append(keys, "badge_perfect_hour")
		}
	}

	// Lucky seven: distance within 100m of 7.0km.
	if !earned["badge_lucky_7"] {
		diff := w.DistanceMeters - 7000
		if diff < 0 {
			diff = -diff
		}
		if diff <= 100 {
			keys = append(keys, "badge_lucky_7")
		}
	}

	// Palindrome pacer: pace where the concatenated digits are a palindrome
	// (e.g. 5:55 → "555", 4:44 → "444", 1:21 → "121").
	if !earned["badge_palindrome_pacer"] && w.AvgPaceSecPerKm > 0 {
		total := int(w.AvgPaceSecPerKm)
		mins := total / 60
		secs := total % 60
		paceStr := fmt.Sprintf("%d%02d", mins, secs)
		if isPalindromeStr(paceStr) {
			keys = append(keys, "badge_palindrome_pacer")
		}
	}

	return keys, nil
}

// parseWorkoutTime parses a workout timestamp trying RFC3339 first, then a
// bare datetime format.
func parseWorkoutTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05", s)
}

// isPalindromeStr reports whether s reads the same forwards and backwards.
func isPalindromeStr(s string) bool {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		if s[i] != s[j] {
			return false
		}
	}
	return true
}
