package stars

import (
	"context"
	"database/sql"
	"fmt"
)

// Level holds the definition for a single level tier.
type Level struct {
	Level     int
	XP        int
	Title     string
	Emoji     string
}

// LevelDefinitions lists all 12 tiers in ascending XP order.
// Level 1 starts at 0 XP; level 12 is the maximum.
var LevelDefinitions = []Level{
	{1, 0, "Rookie Runner", "🐣"},
	{2, 50, "Eager Explorer", "🐤"},
	{3, 150, "Steady Stepper", "🚶"},
	{4, 300, "Power Pacer", "🏃"},
	{5, 500, "Trail Tracker", "🥾"},
	{6, 800, "Rhythm Rider", "🚴"},
	{7, 1200, "Iron Junior", "💪"},
	{8, 1800, "Speed Demon", "⚡"},
	{9, 2500, "Mountain Goat", "🐐"},
	{10, 3500, "Legend", "🏆"},
	{11, 5000, "Mythic Athlete", "🦅"},
	{12, 7000, "Hytte Hero", "👑"},
}

// LevelInfo contains full progress information for a user's level.
type LevelInfo struct {
	Level              int     `json:"level"`
	Title              string  `json:"title"`
	Emoji              string  `json:"emoji"`
	CurrentXP          int     `json:"current_xp"`
	XPForCurrentLevel  int     `json:"xp_for_current_level"`
	XPForNextLevel     int     `json:"xp_for_next_level"`
	ProgressPercent    float64 `json:"progress_percent"`
}

// LevelUpResult is returned by AddXP and describes whether a level-up occurred.
type LevelUpResult struct {
	PreviousLevel int
	NewLevel      int
	NewTitle      string
	NewEmoji      string
	DidLevelUp    bool
}

// CalculateLevel returns the level number and title for the given total XP.
// It iterates the thresholds and returns the highest tier the user qualifies for.
func CalculateLevel(xp int) (level int, title string) {
	current := LevelDefinitions[0]
	for _, def := range LevelDefinitions {
		if xp >= def.XP {
			current = def
		}
	}
	return current.Level, current.Title
}

// levelDefByNumber returns the Level definition for the given level number.
// Returns LevelDefinitions[0] if not found.
func levelDefByNumber(lvl int) Level {
	for _, def := range LevelDefinitions {
		if def.Level == lvl {
			return def
		}
	}
	return LevelDefinitions[0]
}

// AddXP adds xpAmount to the user's total XP, detects level-ups, persists the
// change, and returns a LevelUpResult. Negative xpAmount values are clamped to 0.
func AddXP(ctx context.Context, db *sql.DB, userID int64, xpAmount int) (*LevelUpResult, error) {
	if xpAmount < 0 {
		xpAmount = 0
	}

	// Load or create the user_levels row.
	var currentXP, currentLevel int
	var currentTitle string
	err := db.QueryRowContext(ctx, `
		SELECT xp, level, title FROM user_levels WHERE user_id = ?
	`, userID).Scan(&currentXP, &currentLevel, &currentTitle)
	if err == sql.ErrNoRows {
		_, err = db.ExecContext(ctx, `
			INSERT INTO user_levels (user_id, xp, level, title)
			VALUES (?, 0, 1, 'Rookie Runner')
		`, userID)
		if err != nil {
			return nil, fmt.Errorf("create user_levels: %w", err)
		}
		currentXP = 0
		currentLevel = 1
		currentTitle = "Rookie Runner"
	} else if err != nil {
		return nil, fmt.Errorf("load user_levels: %w", err)
	}

	newXP := currentXP + xpAmount
	newLevel, newTitle := CalculateLevel(newXP)

	result := &LevelUpResult{
		PreviousLevel: currentLevel,
		NewLevel:      newLevel,
		NewTitle:      newTitle,
		NewEmoji:      levelDefByNumber(newLevel).Emoji,
		DidLevelUp:    newLevel > currentLevel,
	}

	_, err = db.ExecContext(ctx, `
		UPDATE user_levels SET xp = ?, level = ?, title = ? WHERE user_id = ?
	`, newXP, newLevel, newTitle, userID)
	if err != nil {
		return nil, fmt.Errorf("update user_levels: %w", err)
	}

	return result, nil
}

// GetLevelInfo returns full level progress information for the user.
// If no user_levels row exists, it creates one at level 1 with 0 XP.
func GetLevelInfo(ctx context.Context, db *sql.DB, userID int64) (*LevelInfo, error) {
	var xp, level int
	var title string
	err := db.QueryRowContext(ctx, `
		SELECT xp, level, title FROM user_levels WHERE user_id = ?
	`, userID).Scan(&xp, &level, &title)
	if err == sql.ErrNoRows {
		_, err = db.ExecContext(ctx, `
			INSERT INTO user_levels (user_id, xp, level, title)
			VALUES (?, 0, 1, 'Rookie Runner')
		`, userID)
		if err != nil {
			return nil, fmt.Errorf("create user_levels: %w", err)
		}
		xp = 0
		level = 1
		title = "Rookie Runner"
	} else if err != nil {
		return nil, fmt.Errorf("load user_levels: %w", err)
	}

	def := levelDefByNumber(level)
	xpForCurrent := def.XP

	maxLevel := LevelDefinitions[len(LevelDefinitions)-1].Level
	var xpForNext int
	var progressPercent float64
	if level >= maxLevel {
		// At max level: show full progress.
		xpForNext = xpForCurrent
		progressPercent = 100.0
	} else {
		nextDef := levelDefByNumber(level + 1)
		xpForNext = nextDef.XP
		span := xpForNext - xpForCurrent
		if span > 0 {
			progressPercent = float64(xp-xpForCurrent) / float64(span) * 100.0
		}
		if progressPercent > 100.0 {
			progressPercent = 100.0
		}
		if progressPercent < 0.0 {
			progressPercent = 0.0
		}
	}

	return &LevelInfo{
		Level:             level,
		Title:             title,
		Emoji:             def.Emoji,
		CurrentXP:         xp,
		XPForCurrentLevel: xpForCurrent,
		XPForNextLevel:    xpForNext,
		ProgressPercent:   progressPercent,
	}, nil
}
