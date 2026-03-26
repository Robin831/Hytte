package stars

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// BeatParentStatus holds the weekly distance comparison between a child and their parent.
// The child's raw distance is scaled by (parent_age / child_age) to account for the
// age difference, leveling the playing field.
type BeatParentStatus struct {
	ChildDistanceRaw    float64 `json:"child_distance_raw"`
	ChildDistanceScaled float64 `json:"child_distance_scaled"`
	ParentDistance      float64 `json:"parent_distance"`
	IsBeatingParent     bool    `json:"is_beating_parent"`
}

// GetBeatMyParentStatus returns the current ISO week's distance comparison between
// the child and their parent. The child's raw distance is multiplied by
// (parent_age / child_age) so younger children compete on equal terms.
//
// Birthdays are read from the "kids_stars_birthday" user preference (YYYY-MM-DD).
// If either birthday is absent or unparseable the scaling factor defaults to 1.0.
// Distances are returned in meters.
func GetBeatMyParentStatus(db *sql.DB, childID, parentID int64) (BeatParentStatus, error) {
	now := time.Now().UTC()
	year, week := now.ISOWeek()
	mon := firstDayOfISOWeek(year, week)
	weekStart := mon.Format(time.RFC3339)
	weekEnd := mon.AddDate(0, 0, 7).Format(time.RFC3339)

	childDistM, err := weeklyDistanceMeters(db, childID, weekStart, weekEnd)
	if err != nil {
		return BeatParentStatus{}, fmt.Errorf("beat parent: child distance: %w", err)
	}

	parentDistM, err := weeklyDistanceMeters(db, parentID, weekStart, weekEnd)
	if err != nil {
		return BeatParentStatus{}, fmt.Errorf("beat parent: parent distance: %w", err)
	}

	scale := ageScalingFactor(db, childID, parentID, now)
	childDistScaled := childDistM * scale

	return BeatParentStatus{
		ChildDistanceRaw:    childDistM,
		ChildDistanceScaled: childDistScaled,
		ParentDistance:      parentDistM,
		IsBeatingParent:     childDistScaled > parentDistM,
	}, nil
}

// AwardBeatParentBonus returns a 25-star StarAward if the child's age-scaled weekly
// distance exceeds the parent's distance for the ISO week containing anyDateInWeek.
// Returns nil (no award) when the child is not ahead or no parent is linked.
// The caller (EvaluateWeeklyBonuses) is responsible for idempotency and recording.
func AwardBeatParentBonus(ctx context.Context, db *sql.DB, childID, parentID int64, anyDateInWeek time.Time) (*StarAward, error) {
	year, week := anyDateInWeek.UTC().ISOWeek()
	mon := firstDayOfISOWeek(year, week)
	weekStart := mon.Format(time.RFC3339)
	weekEnd := mon.AddDate(0, 0, 7).Format(time.RFC3339)

	childDistM, err := weeklyDistanceMeters(db, childID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("beat parent bonus: child distance: %w", err)
	}

	parentDistM, err := weeklyDistanceMeters(db, parentID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("beat parent bonus: parent distance: %w", err)
	}

	scale := ageScalingFactor(db, childID, parentID, anyDateInWeek.UTC())
	childDistScaled := childDistM * scale

	if childDistScaled <= parentDistM {
		return nil, nil
	}

	key := weekKey(anyDateInWeek)
	return &StarAward{
		Amount:      25,
		Reason:      fmt.Sprintf("beat_parent_%s", key),
		Description: fmt.Sprintf("Beat your parent this week! (%.1f km scaled vs %.1f km)", childDistScaled/1000, parentDistM/1000),
	}, nil
}

// weeklyDistanceMeters sums the distance_meters for a user within [weekStart, weekEnd).
func weeklyDistanceMeters(db *sql.DB, userID int64, weekStart, weekEnd string) (float64, error) {
	var dist float64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(distance_meters), 0)
		FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at < ?
	`, userID, weekStart, weekEnd).Scan(&dist)
	return dist, err
}

// ageScalingFactor returns parent_age / child_age computed from the "kids_stars_birthday"
// user preference on each account. Returns 1.0 if either age cannot be determined.
func ageScalingFactor(db *sql.DB, childID, parentID int64, now time.Time) float64 {
	childAge := userAgeYears(db, childID, now)
	parentAge := userAgeYears(db, parentID, now)
	if childAge <= 0 || parentAge <= 0 {
		return 1.0
	}
	return float64(parentAge) / float64(childAge)
}

// userAgeYears returns the full years of age for a user by reading the
// "kids_stars_birthday" preference (format YYYY-MM-DD). Returns 0 on any error
// or when the preference is absent.
func userAgeYears(db *sql.DB, userID int64, now time.Time) int {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("stars: beat-parent load prefs user %d: %v", userID, err)
		return 0
	}
	bdStr, ok := prefs["kids_stars_birthday"]
	if !ok || bdStr == "" {
		return 0
	}
	bd, parseErr := time.Parse("2006-01-02", bdStr)
	if parseErr != nil {
		log.Printf("stars: beat-parent parse birthday user %d %q: %v", userID, bdStr, parseErr)
		return 0
	}
	age := now.Year() - bd.Year()
	if now.Month() < bd.Month() || (now.Month() == bd.Month() && now.Day() < bd.Day()) {
		age--
	}
	return age
}
