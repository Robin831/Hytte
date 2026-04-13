package stride

import (
	"encoding/json"
	"strings"
	"testing"

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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

func TestBuildChatSystemPrompt_ContainsProfile(t *testing.T) {
	profile, plan, evals, races, acr, acute, chronic, notes := buildTestPromptInputs()
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

	// These are distinctive phrases from the full mariusBakkenInstructions constant
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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, acr, acute, chronic, notes)

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
	result := BuildChatSystemPrompt(profile, plan, evals, races, nil, acute, chronic, notes)

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

	result := BuildChatSystemPrompt(profile, plan, nil, nil, nil, 0, 0, nil)

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
