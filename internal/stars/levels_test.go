package stars

import (
	"context"
	"testing"
)

// TestCalculateLevel_Boundaries verifies the level returned at every threshold
// boundary (both just-below and at the threshold).
func TestCalculateLevel_Boundaries(t *testing.T) {
	tests := []struct {
		xp        int
		wantLevel int
		wantTitle string
	}{
		// Just below level 2 threshold (50): still level 1.
		{0, 1, "Rookie Runner"},
		{49, 1, "Rookie Runner"},
		// Exactly at level 2.
		{50, 2, "Eager Explorer"},
		{149, 2, "Eager Explorer"},
		{150, 3, "Steady Stepper"},
		{299, 3, "Steady Stepper"},
		{300, 4, "Power Pacer"},
		{499, 4, "Power Pacer"},
		{500, 5, "Trail Tracker"},
		{799, 5, "Trail Tracker"},
		{800, 6, "Rhythm Rider"},
		{1199, 6, "Rhythm Rider"},
		{1200, 7, "Iron Junior"},
		{1799, 7, "Iron Junior"},
		{1800, 8, "Speed Demon"},
		{2499, 8, "Speed Demon"},
		{2500, 9, "Mountain Goat"},
		{3499, 9, "Mountain Goat"},
		{3500, 10, "Legend"},
		{4999, 10, "Legend"},
		{5000, 11, "Mythic Athlete"},
		{6999, 11, "Mythic Athlete"},
		{7000, 12, "Hytte Hero"},
		{99999, 12, "Hytte Hero"},
	}

	for _, tt := range tests {
		level, title := CalculateLevel(tt.xp)
		if level != tt.wantLevel || title != tt.wantTitle {
			t.Errorf("CalculateLevel(%d) = (%d, %q), want (%d, %q)",
				tt.xp, level, title, tt.wantLevel, tt.wantTitle)
		}
	}
}

// TestAddXP_LevelUp verifies that earning enough XP in one call triggers a level-up.
func TestAddXP_LevelUp(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child@test.com")

	// Start at 0 XP (level 1). Earn 50 XP → should reach level 2.
	result, err := AddXP(context.Background(), db, userID, 50)
	if err != nil {
		t.Fatalf("AddXP: %v", err)
	}
	if !result.DidLevelUp {
		t.Error("expected DidLevelUp = true")
	}
	if result.PreviousLevel != 1 {
		t.Errorf("PreviousLevel = %d, want 1", result.PreviousLevel)
	}
	if result.NewLevel != 2 {
		t.Errorf("NewLevel = %d, want 2", result.NewLevel)
	}
	if result.NewTitle != "Eager Explorer" {
		t.Errorf("NewTitle = %q, want %q", result.NewTitle, "Eager Explorer")
	}
}

// TestAddXP_MultiLevelJump verifies earning a large XP amount at once can
// skip multiple levels.
func TestAddXP_MultiLevelJump(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child2@test.com")

	// 500 XP at once from level 1 should jump to level 5.
	result, err := AddXP(context.Background(), db, userID, 500)
	if err != nil {
		t.Fatalf("AddXP: %v", err)
	}
	if !result.DidLevelUp {
		t.Error("expected DidLevelUp = true")
	}
	if result.NewLevel != 5 {
		t.Errorf("NewLevel = %d, want 5", result.NewLevel)
	}
}

// TestAddXP_MaxLevel verifies that a user at max level receives XP but DidLevelUp
// is false and the level stays at 12.
func TestAddXP_MaxLevel(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child3@test.com")

	// Bring the user to max level.
	_, err := AddXP(context.Background(), db, userID, 7000)
	if err != nil {
		t.Fatalf("AddXP to max level: %v", err)
	}

	// Add more XP; should not level up further.
	result, err := AddXP(context.Background(), db, userID, 1000)
	if err != nil {
		t.Fatalf("AddXP beyond max: %v", err)
	}
	if result.DidLevelUp {
		t.Error("expected DidLevelUp = false at max level")
	}
	if result.NewLevel != 12 {
		t.Errorf("NewLevel = %d, want 12", result.NewLevel)
	}
}

// TestAddXP_NegativeXPGuard ensures that passing a negative xpAmount is treated
// as 0 and does not corrupt the XP total.
func TestAddXP_NegativeXPGuard(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child4@test.com")

	// Earn some XP first.
	_, err := AddXP(context.Background(), db, userID, 60)
	if err != nil {
		t.Fatalf("AddXP initial: %v", err)
	}

	// Attempt negative XP.
	result, err := AddXP(context.Background(), db, userID, -100)
	if err != nil {
		t.Fatalf("AddXP negative: %v", err)
	}
	if result.DidLevelUp {
		t.Error("no level-up expected for negative XP")
	}

	info, err := GetLevelInfo(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("GetLevelInfo: %v", err)
	}
	if info.CurrentXP < 0 {
		t.Errorf("CurrentXP = %d, must not be negative", info.CurrentXP)
	}
	// Should still be 60, unchanged by the negative call.
	if info.CurrentXP != 60 {
		t.Errorf("CurrentXP = %d, want 60", info.CurrentXP)
	}
}

// TestGetLevelInfo_ProgressPercent verifies progress_percent is correct for a
// mid-level XP value.
func TestGetLevelInfo_ProgressPercent(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child5@test.com")

	// Level 2 spans 50–150 XP. Set XP to 100 → 50% through level 2.
	_, err := AddXP(context.Background(), db, userID, 100)
	if err != nil {
		t.Fatalf("AddXP: %v", err)
	}

	info, err := GetLevelInfo(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("GetLevelInfo: %v", err)
	}
	if info.Level != 2 {
		t.Errorf("Level = %d, want 2", info.Level)
	}
	if info.XPForCurrentLevel != 50 {
		t.Errorf("XPForCurrentLevel = %d, want 50", info.XPForCurrentLevel)
	}
	if info.XPForNextLevel != 150 {
		t.Errorf("XPForNextLevel = %d, want 150", info.XPForNextLevel)
	}
	wantPct := 50.0
	if info.ProgressPercent != wantPct {
		t.Errorf("ProgressPercent = %.2f, want %.2f", info.ProgressPercent, wantPct)
	}
}

// TestGetLevelInfo_MaxLevelProgress verifies that a max-level user shows 100%
// progress and XPForNextLevel equals XPForCurrentLevel.
func TestGetLevelInfo_MaxLevelProgress(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child6@test.com")

	_, err := AddXP(context.Background(), db, userID, 9000)
	if err != nil {
		t.Fatalf("AddXP: %v", err)
	}

	info, err := GetLevelInfo(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("GetLevelInfo: %v", err)
	}
	if info.Level != 12 {
		t.Errorf("Level = %d, want 12", info.Level)
	}
	if info.ProgressPercent != 100.0 {
		t.Errorf("ProgressPercent = %.2f, want 100", info.ProgressPercent)
	}
	if info.XPForNextLevel != info.XPForCurrentLevel {
		t.Errorf("XPForNextLevel (%d) should equal XPForCurrentLevel (%d) at max level",
			info.XPForNextLevel, info.XPForCurrentLevel)
	}
}

// TestGetLevelInfo_NewUser verifies that GetLevelInfo auto-creates the row
// for a user with no existing record.
func TestGetLevelInfo_NewUser(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "child7@test.com")

	info, err := GetLevelInfo(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("GetLevelInfo: %v", err)
	}
	if info.Level != 1 {
		t.Errorf("Level = %d, want 1", info.Level)
	}
	if info.CurrentXP != 0 {
		t.Errorf("CurrentXP = %d, want 0", info.CurrentXP)
	}
	if info.ProgressPercent != 0.0 {
		t.Errorf("ProgressPercent = %.2f, want 0", info.ProgressPercent)
	}
}
