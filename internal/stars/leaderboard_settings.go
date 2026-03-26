package stars

import (
	"database/sql"

	"github.com/Robin831/Hytte/internal/auth"
)

// LeaderboardSettings holds parent-configurable leaderboard display options.
type LeaderboardSettings struct {
	LeaderboardVisible bool `json:"leaderboard_visible"`
	ParentParticipates bool `json:"parent_participates"`
}

// GetLeaderboardSettings reads a parent's leaderboard preferences.
// Both fields default to true when not set.
func GetLeaderboardSettings(db *sql.DB, parentID int64) (LeaderboardSettings, error) {
	prefs, err := auth.GetPreferences(db, parentID)
	if err != nil {
		return LeaderboardSettings{}, err
	}

	s := LeaderboardSettings{
		LeaderboardVisible: true,
		ParentParticipates: true,
	}

	if v, ok := prefs["kids_stars_leaderboard_visible"]; ok {
		s.LeaderboardVisible = v != "false" && v != "0"
	}
	if v, ok := prefs["kids_stars_parent_participates"]; ok {
		s.ParentParticipates = v != "false" && v != "0"
	}

	return s, nil
}

// SetLeaderboardSetting stores a single leaderboard preference for a parent user.
func SetLeaderboardSetting(db *sql.DB, parentID int64, key, value string) error {
	return auth.SetPreference(db, parentID, key, value)
}
