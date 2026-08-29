package stride

import (
	"context"
	"strings"
	"testing"
)

// macroBlockFixture is a small four-week block with two mesocycles, enough to
// exercise the renderer's goal, mesocycle, target-week and neighbour-week
// sections without seeding a database.
func macroBlockFixture() *MacroPlan {
	block := &MacroPlan{
		ID:        7,
		UserID:    42,
		StartWeek: "2026-04-06",
		EndWeek:   "2026-04-27",
		Status:    MacroPlanStatusActive,
		Goal: MacroGoal{
			PrimaryFocus:  "half_marathon",
			Statement:     "BLOCK-GOAL-MARKER: run 1:24:00 for the half marathon",
			TargetHMTimeS: 5040,
			Benchmark:     "BLOCK-BENCHMARK-MARKER: 3 x 3 km at threshold",
			Rationale:     "BLOCK-RATIONALE-MARKER: the model predicts 1:27 today.",
		},
		Periodisation: []Mesocycle{
			{Name: "Base 1", Phase: MacroPhaseBase, StartWeek: "2026-04-06", Weeks: 2, Focus: "aerobic volume"},
			{Name: "Build 1", Phase: MacroPhaseBuild, StartWeek: "2026-04-20", Weeks: 2, Focus: "threshold density"},
		},
	}

	specs := []struct {
		weekStart string
		mesocycle string
		phase     string
	}{
		{"2026-04-06", "Base 1", MacroPhaseBase},
		{"2026-04-13", "Base 1", MacroPhaseBase},
		{"2026-04-20", "Build 1", MacroPhaseBuild},
		{"2026-04-27", "Build 1", MacroPhaseBuild},
	}
	for i, s := range specs {
		block.Weeks = append(block.Weeks, MacroWeek{
			ID:             int64(i + 1),
			MacroPlanID:    block.ID,
			UserID:         block.UserID,
			WeekStart:      s.weekStart,
			Seq:            i + 1,
			Phase:          s.phase,
			Mesocycle:      s.mesocycle,
			LoadLevel:      LoadLevelNormal,
			TargetKm:       50 + float64(i),
			TargetSessions: 5,
			KeySessions: []KeySession{
				{Type: "threshold", Focus: "KEY-SESSION-MARKER: controlled tempo"},
			},
			Intent: "WEEK-INTENT-MARKER-" + s.weekStart,
			Status: MacroWeekStatusPlanned,
		})
	}
	return block
}

func TestRenderMacroPlanBlock_ContainsGoalMesocycleAndWeekSpecs(t *testing.T) {
	block := macroBlockFixture()
	target, ok := macroWeekAt(block, "2026-04-13")
	if !ok {
		t.Fatal("fixture has no week starting 2026-04-13")
	}

	got := renderMacroPlanBlock(block, target, block.Goal, "revision 2, set 2026-04-13, source weekly")

	for _, want := range []string{
		"## Macro Block",
		"- Block: 2026-04-06 through 2026-04-27 (4 weeks), status active",
		"### Block Goal (revision 2, set 2026-04-13, source weekly)",
		"BLOCK-GOAL-MARKER",
		"- Target half-marathon time: 1:24:00",
		"BLOCK-BENCHMARK-MARKER",
		"BLOCK-RATIONALE-MARKER",
		"### Current mesocycle\nBase 1, week 2 of 2, focus aerobic volume",
		"### Target week — 2026-04-13 (week 2 of 4)",
		"- Phase: base — NEVER change this",
		"- Target distance: 51.0 km",
		"- Target sessions: 5",
		"KEY-SESSION-MARKER",
		"WEEK-INTENT-MARKER-2026-04-13",
		"### Previous macro week — 2026-04-06 (week 1 of 4)",
		"### Next macro week — 2026-04-20 (week 3 of 4)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered block is missing %q\n---\n%s", want, got)
		}
	}
}

func TestRenderMacroPlanBlock_BlockBoundariesOmitNeighbours(t *testing.T) {
	block := macroBlockFixture()

	first, _ := macroWeekAt(block, "2026-04-06")
	firstOut := renderMacroPlanBlock(block, first, block.Goal, "initial")
	if !strings.Contains(firstOut, "### Previous macro week\nNone — the target week is the first week of the block.") {
		t.Errorf("first week should have no previous macro week:\n%s", firstOut)
	}
	if !strings.Contains(firstOut, "### Next macro week — 2026-04-13") {
		t.Errorf("first week should still have a next macro week:\n%s", firstOut)
	}

	last, _ := macroWeekAt(block, "2026-04-27")
	lastOut := renderMacroPlanBlock(block, last, block.Goal, "initial")
	if !strings.Contains(lastOut, "### Next macro week\nNone — the target week is the last week of the block.") {
		t.Errorf("last week should have no next macro week:\n%s", lastOut)
	}
	if !strings.Contains(lastOut, "### Previous macro week — 2026-04-20") {
		t.Errorf("last week should still have a previous macro week:\n%s", lastOut)
	}
}

func TestRenderMacroPlanBlock_StaleBlockSaysSo(t *testing.T) {
	block := macroBlockFixture()
	block.StaleReason = "races_changed"
	target, _ := macroWeekAt(block, "2026-04-13")

	got := renderMacroPlanBlock(block, target, block.Goal, "initial")
	if !strings.Contains(got, "Block is marked stale (races_changed)") {
		t.Errorf("stale block should be flagged:\n%s", got)
	}
}

func TestLoadMacroPlanBlock_NoActivePlan(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	if got := loadMacroPlanBlock(context.Background(), db, 1, "2026-04-13"); got != "" {
		t.Errorf("expected empty block for an athlete with no macro plan, got:\n%s", got)
	}
}

func TestLoadMacroPlanBlock_RendersActiveBlock(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	const userID int64 = 1
	blockStart := "2026-04-06"
	seedAdjustBlock(t, db, userID, blockStart)
	targetWeek := mondayAfter(blockStart, 2)

	got := loadMacroPlanBlock(context.Background(), db, userID, targetWeek)
	for _, want := range []string{
		"## Macro Block",
		"### Block Goal (revision 1, set " + blockStart + ", source initial)",
		"GOAL-STATEMENT-MARKER",
		"### Target week — " + targetWeek + " (week 3 of 26)",
		"INTENT-MARKER-3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("loaded block is missing %q\n---\n%s", want, got)
		}
	}
}

func TestLoadMacroPlanBlock_WeekOutsideBlock(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	const userID int64 = 1
	blockStart := "2026-04-06"
	seedAdjustBlock(t, db, userID, blockStart)

	// A Monday before the block starts is covered by no week of it.
	if got := loadMacroPlanBlock(context.Background(), db, userID, mondayAfter(blockStart, -3)); got != "" {
		t.Errorf("expected empty block for a week outside the horizon, got:\n%s", got)
	}
}
