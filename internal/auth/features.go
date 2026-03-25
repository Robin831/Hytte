package auth

import (
	"database/sql"
	"maps"
	"sort"
)

// FeatureDefaults defines the canonical set of feature keys and their default
// state for new users. Missing rows in user_features fall back to these values.
var FeatureDefaults = map[string]bool{
	"dashboard":  true,
	"weather":    true,
	"calendar":   true,
	"notes":      false,
	"links":      false,
	"training":   false,
	"lactate":    false,
	"infra":      false,
	"webhooks":   false,
	"claude_ai":  false,
	"chat":       false,
	"kids_stars": false,
}

// FeatureKeys is a sorted list of all known feature keys, used for stable
// iteration order in API responses.
var FeatureKeys []string

func init() {
	FeatureKeys = make([]string, 0, len(FeatureDefaults))
	for k := range FeatureDefaults {
		FeatureKeys = append(FeatureKeys, k)
	}
	sort.Strings(FeatureKeys)
}

// UserFeatureSet holds a user's info along with their feature map.
type UserFeatureSet struct {
	UserID   int64           `json:"user_id"`
	Email    string          `json:"email"`
	Name     string          `json:"name"`
	Picture  string          `json:"picture"`
	IsAdmin  bool            `json:"is_admin"`
	Features map[string]bool `json:"features"`
}

// GetUserFeatures returns the feature map for a user. Missing keys fall back
// to FeatureDefaults. Admin users always get all features enabled.
func GetUserFeatures(db *sql.DB, userID int64, isAdmin bool) (map[string]bool, error) {
	features := make(map[string]bool, len(FeatureDefaults))

	if isAdmin {
		for _, k := range FeatureKeys {
			features[k] = true
		}
		return features, nil
	}

	// Start with defaults.
	maps.Copy(features, FeatureDefaults)

	// Override with user-specific settings from the database.
	rows, err := db.Query("SELECT feature_key, enabled FROM user_features WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var enabled int
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, err
		}
		// Only apply overrides for known feature keys.
		if _, ok := FeatureDefaults[key]; ok {
			features[key] = enabled != 0
		}
	}
	return features, rows.Err()
}

// SetUserFeature sets a single feature for a user (upsert).
func SetUserFeature(db *sql.DB, userID int64, feature string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := db.Exec(`
		INSERT INTO user_features (user_id, feature_key, enabled)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, feature_key) DO UPDATE SET enabled = excluded.enabled
	`, userID, feature, val)
	return err
}

// GetAllUsersFeatures returns features for all users (for the admin page).
func GetAllUsersFeatures(db *sql.DB) ([]UserFeatureSet, error) {
	// Fetch all users.
	userRows, err := db.Query("SELECT id, email, name, picture, is_admin FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer userRows.Close()

	var users []UserFeatureSet
	for userRows.Next() {
		var u UserFeatureSet
		if err := userRows.Scan(&u.UserID, &u.Email, &u.Name, &u.Picture, &u.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := userRows.Err(); err != nil {
		return nil, err
	}

	// Fetch all feature overrides in one query.
	featureRows, err := db.Query("SELECT user_id, feature_key, enabled FROM user_features")
	if err != nil {
		return nil, err
	}
	defer featureRows.Close()

	overrides := make(map[int64]map[string]bool)
	for featureRows.Next() {
		var uid int64
		var key string
		var enabled int
		if err := featureRows.Scan(&uid, &key, &enabled); err != nil {
			return nil, err
		}
		if overrides[uid] == nil {
			overrides[uid] = make(map[string]bool)
		}
		overrides[uid][key] = enabled != 0
	}
	if err := featureRows.Err(); err != nil {
		return nil, err
	}

	// Build feature maps for each user.
	for i := range users {
		features := make(map[string]bool, len(FeatureDefaults))
		if users[i].IsAdmin {
			for _, k := range FeatureKeys {
				features[k] = true
			}
		} else {
			maps.Copy(features, FeatureDefaults)
			if ov, ok := overrides[users[i].UserID]; ok {
				for k, v := range ov {
					if _, known := FeatureDefaults[k]; known {
						features[k] = v
					}
				}
			}
		}
		users[i].Features = features
	}

	return users, nil
}
