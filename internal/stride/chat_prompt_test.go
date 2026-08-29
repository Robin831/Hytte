package stride

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

func buildTestPromptInputs() (training.UserTrainingProfile, Plan, []EvaluationRecord, []Race, *float64, float64, float64, []Note) {
	profile := training.UserTrainingProfile{
		Block: `Max HR: 190 bpm
Resting HR: 48 bpm
Threshold HR: 166 bpm
Threshold Pace: 4:30 /km
Zone 1: 100-138 bpm
Zone 2: 138-155 bpm
Zone 3: 155-166 bpm
Zone 4: 166-178 bpm
Zone 5: 178-190 bpm
Weekly volume target: 50 km
Sessions per week: 4
Current block: Build
Current phase: Threshold development`,
		ThresholdHR: 166,
		HasGoalRace: true,
	}

	days := []DayPlan{
		{Date: "2026-04-13", RestDay: false, Session: &Session{
			Warmup:      "15 min easy jog",
			MainSet:     "6x1000m at threshold pace",
			Cooldown:    "10 min easy jog",
			Strides:     "",
			TargetHRCap: 165,
			Description: "Threshold intervals session.",
		}},
		{Date: "2026-04-14", RestDay: true},
		{Date: "2026-04-15", RestDay: false, Session: &Session{
			Warmup:      "10 min easy jog",
			MainSet:     "50 min easy run",
			Cooldown:    "5 min walk",
			Strides:     "4x20s strides",
			TargetHRCap: 138,
			Description: "Easy recovery run with strides.",
		}},
	}
	planJSON, _ := json.Marshal(days)
	plan := Plan{
		ID:        1,
		UserID:    42,
		WeekStart: "2026-04-13",
		WeekEnd:   "2026-04-19",
		Phase:     "threshold_development",
		Plan:      planJSON,
		Model:     "claude-sonnet-4-6",
		CreatedAt: "2026-04-13T02:00:00Z",
	}

	evaluations := []EvaluationRecord{
		{
			ID:     1,
			UserID: 42,
			PlanID: 1,
			Eval: Evaluation{
				PlannedType: "threshold",
				ActualType:  "threshold",
				Compliance:  "compliant",
				Notes:       "HR avg 162, within target range.",
				Date:        "2026-04-13",
			},
			CreatedAt: "2026-04-13T22:00:00Z",
		},
		{
			ID:     2,
			UserID: 42,
			PlanID: 1,
			Eval: Evaluation{
				PlannedType: "easy",
				ActualType:  "easy",
				Compliance:  "partial",
				Notes:       "Pace 5:10 vs target 5:30, slightly fast.",
				Date:        "2026-04-15",
			},
			CreatedAt: "2026-04-15T20:00:00Z",
		},
	}

	targetTime := 5400 // 90 minutes
	races := []Race{
		{
			ID:         1,
			UserID:     42,
			Name:       "Oslo Half Marathon",
			Date:       "2026-05-10",
			DistanceM:  21097,
			TargetTime: &targetTime,
			Priority:   "A",
		},
		{
			ID:        2,
			UserID:    42,
			Name:      "Park Run 5K",
			Date:      "2026-04-25",
			DistanceM: 5000,
			Priority:  "C",
		},
	}

	acr := 1.15
	acute := 320.0
	chronic := 278.0

	notes := []Note{
		{
			ID:         1,
			UserID:     42,
			Content:    "Left knee feels tight after long runs",
			TargetDate: "2026-04-13",
		},
		{
			ID:         2,
			UserID:     42,
			Content:    "Busy week at work, may need extra rest",
			TargetDate: "2026-04-15",
		},
	}

	return profile, plan, evaluations, races, &acr, acute, chronic, notes
}

func TestBuildChatSystemPrompt_ContainsCurrentPlan(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	// Should contain plan dates and session details
	for _, want := range []string{
		"2026-04-13",
		"2026-04-14",
		"2026-04-15",
		"6x1000m at threshold pace",
		"threshold_development",
		"2026-04-13 to 2026-04-19",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsWorkoutFormatGuidance(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	// Chat edits must follow the same treadmill-friendly dual-unit format as
	// initial generation.
	for _, want := range []string{
		"Workout Description Formatting",
		"km/h",
		"4x2000m (or 4x9min)",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("chat prompt should contain workout format guidance %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsTreadmillSpeedCaveat(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	// Editing a session over chat must not reintroduce the bug where an
	// outdoor km/h figure is handed over as a treadmill belt setting.
	for _, want := range []string{
		"Treadmill speeds are NOT the same number as outdoor speeds",
		"belt setting",
		"TIME and BELT SPEED",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("chat prompt should contain treadmill speed caveat %q, but it does not", want)
		}
	}
}

// Chat can represcribe belt speeds, so the persisted calibration must reach the
// chat prompt too — otherwise the coach re-derives it mid-conversation and
// contradicts the numbers it used when generating the plan.
func TestBuildChatSystemPrompt_ContainsTreadmillCalibration(t *testing.T) {
	const calibration = "Belt sits ~3% below outdoor km/h at matched HR."
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()

	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, calibration, "", "")
	if !strings.Contains(result, treadmillCalibrationHeading) {
		t.Errorf("chat prompt should contain %q when a calibration is set", treadmillCalibrationHeading)
	}
	if !strings.Contains(result, calibration) {
		t.Error("chat prompt should carry the calibration text verbatim")
	}

	without := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")
	if strings.Contains(without, treadmillCalibrationHeading) {
		t.Error("chat prompt should omit the calibration section when none is set")
	}
}

func TestBuildChatSystemPrompt_ContainsProfile(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"Threshold HR: 166",
		"Zone 1: 100-138",
		"Threshold Pace: 4:30",
		"Weekly volume target: 50 km",
		"Athlete Profile",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain profile data %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsEvaluations(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"Completed Sessions This Week",
		"threshold — compliant",
		"HR avg 162",
		"easy — partial",
		"slightly fast",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain evaluation data %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsRaces(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"Upcoming Races",
		"Oslo Half Marathon",
		"21097m",
		"priority A",
		"target 90:00",
		"Park Run 5K",
		"priority C",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain race data %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsModificationInstructions(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"When modifying the plan, output the FULL updated 7-day plan",
		"DayPlan Schema",
		`"rest_day": boolean`,
		`"main_set": string`,
		`"target_hr_cap": integer`,
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain modification instruction %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_OmitsMariusBakkenFullInstructions(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	// These are distinctive phrases from the full Marius Bakken generation instructions
	// that should NOT appear in the chat prompt.
	forbidden := []string{
		"This is NOT 80/20 polarized training",
		"Increase weekly distance by no more than 10% per week",
		"Return ONLY a JSON array of day objects for the requested week",
		"adapted for recreational runners doing 3-5 sessions per week",
	}
	for _, phrase := range forbidden {
		if strings.Contains(result, phrase) {
			t.Errorf("prompt must NOT contain full Marius Bakken instruction phrase %q, but it does", phrase)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsTrainingLoad(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"Training Load",
		"ACR (acute:chronic ratio): 1.15",
		"Acute load: 320",
		"Chronic load: 278",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain training load data %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_ContainsNotes(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, want := range []string{
		"Athlete Notes",
		"Left knee feels tight after long runs",
		"Busy week at work",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt should contain note data %q, but it does not", want)
		}
	}
}

func TestBuildChatSystemPrompt_NilACR(t *testing.T) {
	profile, plan, evals, races, _, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, nil, acute, chronic, notes, "", "", "")

	if strings.Contains(result, "ACR") {
		t.Error("prompt should not contain ACR when acr is nil")
	}
	if !strings.Contains(result, "Acute load: 320") {
		t.Error("prompt should still contain acute load when ACR is nil")
	}
}

func TestBuildChatSystemPrompt_EmptyOptionalSections(t *testing.T) {
	profile := training.UserTrainingProfile{Block: "Threshold HR: 166"}
	plan := Plan{
		WeekStart: "2026-04-13",
		WeekEnd:   "2026-04-19",
		Phase:     "base",
		Plan:      json.RawMessage(`[]`),
	}

	result := BuildChatSystemPrompt(profile, plan, nil, nil, nil, 0, 0, nil, "", "", "")

	// Should NOT contain optional section headers when data is empty
	if strings.Contains(result, "Completed Sessions This Week") {
		t.Error("prompt should not contain evaluations header when there are no evaluations")
	}
	if strings.Contains(result, "Upcoming Races") {
		t.Error("prompt should not contain races header when there are no races")
	}
	if strings.Contains(result, "Athlete Notes") {
		t.Error("prompt should not contain notes header when there are no notes")
	}
	// Should still contain required sections
	if !strings.Contains(result, "Current Weekly Plan") {
		t.Error("prompt should always contain the current plan section")
	}
	if !strings.Contains(result, "Training Load") {
		t.Error("prompt should always contain the training load section")
	}
}

// The athlete's custom prompt shapes plan generation, so the chat coach must see
// the same durable context in a clearly-labelled, precedence-setting section.
func TestBuildChatSystemPrompt_ContainsCustomPrompt(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	custom := "Treadmill belt reads 3% slow. Never prescribe doubles."

	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", custom, "")

	if !strings.Contains(result, custom) {
		t.Errorf("prompt should contain the custom prompt text %q, but it does not", custom)
	}
	if !strings.Contains(result, "Athlete's Standing Coaching Instructions") {
		t.Error("prompt should label the custom prompt with its own section header")
	}
	if !strings.Contains(result, "OVERRIDE your generic coaching defaults") {
		t.Error("custom prompt section should state that it overrides generic defaults")
	}
}

func TestBuildChatSystemPrompt_OmitsCustomPromptSectionWhenEmpty(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()

	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	if strings.Contains(result, "Athlete's Standing Coaching Instructions") {
		t.Error("prompt should not contain the custom prompt header when no custom prompt is set")
	}
	// The rest of the prompt must be unaffected.
	if !strings.Contains(result, "Current Weekly Plan") {
		t.Error("prompt should still contain the current plan section")
	}
}

// A chat edit and the weekly adjustment must be held to the same contract, so
// the chat prompt carries the very block AdjustWeek plans inside — rendered by
// the shared renderer, not restated here.
func TestBuildChatSystemPrompt_ContainsMacroBlock(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	block := macroBlockFixture()
	target, ok := macroWeekAt(block, plan.WeekStart)
	if !ok {
		t.Fatalf("fixture block has no week starting %s", plan.WeekStart)
	}
	macroBlock := renderMacroPlanBlock(block, target, block.Goal, "revision 1, set 2026-04-06, source initial")

	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", macroBlock)

	if !strings.Contains(result, macroBlock) {
		t.Errorf("prompt should contain the rendered macro block verbatim:\n%s", macroBlock)
	}
	for _, want := range []string{
		"## Macro Block",
		"BLOCK-GOAL-MARKER",
		"### Current mesocycle\nBase 1, week 2 of 2, focus aerobic volume",
		"### Target week — 2026-04-13 (week 2 of 4)",
		"### Previous macro week — 2026-04-06",
		"### Next macro week — 2026-04-20",
		adjustHalfMarathonRule,
		"Every edit you make in this conversation stays inside this block.",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("prompt is missing %q", truncate(want, 120))
		}
	}
	// The block frames the plan, so it must be rendered before it.
	if strings.Index(result, "## Macro Block") > strings.Index(result, "## Current Weekly Plan") {
		t.Error("macro block should be rendered before the current weekly plan")
	}
}

func TestBuildChatSystemPrompt_OmitsMacroBlockWhenNone(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()

	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes, "", "", "")

	for _, unwanted := range []string{"## Macro Block", "## Half-Marathon Priority", "stays inside this block"} {
		if strings.Contains(result, unwanted) {
			t.Errorf("prompt should not contain %q when no macro block is active", unwanted)
		}
	}
	// No stray header or blank line where the block would have gone: the plan
	// section follows the instructions exactly as it did before macro planning.
	if !strings.Contains(result, workoutFormatGuidance+"\n\n## Current Weekly Plan\n\n") {
		t.Error("prompt without a macro block should join the instructions straight to the plan section")
	}
}

func TestLoadCustomPrompt_DecryptsStoredPreference(t *testing.T) {
	db := setupTestDB(t)

	want := "Long run always on Sunday. Knee flares up on back-to-back hard days."
	enc, err := encryption.EncryptField(want)
	if err != nil {
		t.Fatalf("encrypt custom prompt: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_custom_prompt', ?)`, enc,
	); err != nil {
		t.Fatalf("insert preference: %v", err)
	}

	if got := loadCustomPrompt(db, 1); got != want {
		t.Errorf("loadCustomPrompt = %q, want %q", got, want)
	}
}

func TestLoadCustomPrompt_AbsentPreference(t *testing.T) {
	db := setupTestDB(t)

	if got := loadCustomPrompt(db, 1); got != "" {
		t.Errorf("loadCustomPrompt = %q, want empty string when no preference is set", got)
	}
}

// A corrupt or key-rotated ciphertext must degrade to no custom instructions
// rather than propagating an error and failing the chat turn.
func TestLoadCustomPrompt_DecryptFailureDegradesToEmpty(t *testing.T) {
	db := setupTestDB(t)

	enc, err := encryption.EncryptField("Secret coaching preferences")
	if err != nil {
		t.Fatalf("encrypt custom prompt: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_custom_prompt', ?)`, enc,
	); err != nil {
		t.Fatalf("insert preference: %v", err)
	}

	// Switch to a different encryption key so decryption fails.
	encryption.ResetEncryptionKey()
	t.Setenv("ENCRYPTION_KEY", "different-key-causes-decrypt-fail")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	if got := loadCustomPrompt(db, 1); got != "" {
		t.Errorf("loadCustomPrompt = %q, want empty string when decryption fails", got)
	}
}

// End-to-end: the chat handler's context builder must surface the decrypted
// custom prompt in the system prompt it hands to Claude.
func TestBuildChatContext_IncludesCustomPrompt(t *testing.T) {
	db := setupTestDB(t)

	custom := "Prefers morning sessions; avoid hills on Tuesdays."
	enc, err := encryption.EncryptField(custom)
	if err != nil {
		t.Fatalf("encrypt custom prompt: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'stride_custom_prompt', ?)`, enc,
	); err != nil {
		t.Fatalf("insert preference: %v", err)
	}

	plan := Plan{
		ID:        1,
		UserID:    1,
		WeekStart: "2026-04-13",
		WeekEnd:   "2026-04-19",
		Phase:     "base",
		Plan:      json.RawMessage(`[]`),
	}

	result := buildChatContext(context.Background(), db, 1, plan)

	if !strings.Contains(result, custom) {
		t.Errorf("chat context should contain the custom prompt %q, but it does not", custom)
	}
	if !strings.Contains(result, "Athlete's Standing Coaching Instructions") {
		t.Error("chat context should contain the custom prompt section header")
	}
}

// End-to-end: the chat handler's context builder must load the active macro
// block covering the plan's week, and render nothing when there is none.
func TestBuildChatContext_IncludesMacroBlock(t *testing.T) {
	db := setupTestDB(t)

	blockStart := "2026-04-06"
	seedAdjustBlock(t, db, 1, blockStart)
	weekStart := mondayAfter(blockStart, 2)

	plan := Plan{
		ID:        1,
		UserID:    1,
		WeekStart: weekStart,
		WeekEnd:   shiftDays(weekStart, 6),
		Phase:     "base",
		Plan:      json.RawMessage(`[]`),
	}

	result := buildChatContext(context.Background(), db, 1, plan)

	for _, want := range []string{
		"## Macro Block",
		"### Target week — " + weekStart,
		"INTENT-MARKER-3",
		adjustHalfMarathonRule,
		"Every edit you make in this conversation stays inside this block.",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("chat context is missing %q", truncate(want, 120))
		}
	}
}

func TestBuildChatContext_OmitsMacroBlockWithoutActivePlan(t *testing.T) {
	db := setupTestDB(t)

	plan := Plan{
		ID:        1,
		UserID:    1,
		WeekStart: "2026-04-13",
		WeekEnd:   "2026-04-19",
		Phase:     "base",
		Plan:      json.RawMessage(`[]`),
	}

	result := buildChatContext(context.Background(), db, 1, plan)

	if strings.Contains(result, "## Macro Block") {
		t.Error("chat context should not render a macro block when the athlete has none")
	}
}
