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
// The update is performed inside a transaction to prevent lost increments under
// concurrent calls; INSERT OR IGNORE ensures row creation is idempotent.
func AddXP(ctx context.Context, db *sql.DB, userID int64, xpAmount int) (*LevelUpResult, error) {
	if xpAmount < 0 {
		xpAmount = 0
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Ensure row exists; safe under concurrency because OR IGNORE is idempotent.
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_levels (user_id, xp, level, title)
		VALUES (?, 0, 1, 'Rookie Runner')
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("ensure user_levels: %w", err)
	}

	// Read current level (needed to detect a level-up).
	var prevLevel int64
	err = tx.QueryRowContext(ctx, `
		SELECT level FROM user_levels WHERE user_id = ?
	`, userID).Scan(&prevLevel)
	if err != nil {
		return nil, fmt.Errorf("load user_levels: %w", err)
	}

	// Atomically increment XP and return the post-increment total.
	var newXP int64
	err = tx.QueryRowContext(ctx, `
		UPDATE user_levels SET xp = xp + ? WHERE user_id = ? RETURNING xp
	`, xpAmount, userID).Scan(&newXP)
	if err != nil {
		return nil, fmt.Errorf("increment xp: %w", err)
	}

	newLevel, newTitle := CalculateLevel(int(newXP))

	_, err = tx.ExecContext(ctx, `
		UPDATE user_levels SET level = ?, title = ? WHERE user_id = ?
	`, newLevel, newTitle, userID)
	if err != nil {
		return nil, fmt.Errorf("update level: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &LevelUpResult{
		PreviousLevel: int(prevLevel),
		NewLevel:      newLevel,
		NewTitle:      newTitle,
		NewEmoji:      levelDefByNumber(newLevel).Emoji,
		DidLevelUp:    newLevel > int(prevLevel),
	}, nil
}

// GetLevelInfo returns full level progress information for the user.
// If no user_levels row exists, it creates one at level 1 with 0 XP.
func GetLevelInfo(ctx context.Context, db *sql.DB, userID int64) (*LevelInfo, error) {
	var xpRaw, levelRaw int64
	var title string
	err := db.QueryRowContext(ctx, `
		SELECT xp, level, title FROM user_levels WHERE user_id = ?
	`, userID).Scan(&xpRaw, &levelRaw, &title)
	if err == sql.ErrNoRows {
		// Use INSERT OR IGNORE to avoid UNIQUE constraint errors under concurrency,
		// then reload the row to get current values (whether created here or elsewhere).
		_, err = db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_levels (user_id, xp, level, title)
			VALUES (?, 0, 1, 'Rookie Runner')
		`, userID)
		if err != nil {
			return nil, fmt.Errorf("create user_levels: %w", err)
		}
		err = db.QueryRowContext(ctx, `
			SELECT xp, level, title FROM user_levels WHERE user_id = ?
		`, userID).Scan(&xpRaw, &levelRaw, &title)
		if err != nil {
			return nil, fmt.Errorf("load user_levels after create: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("load user_levels: %w", err)
	}

	xp := int(xpRaw)
	level := int(levelRaw)

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
