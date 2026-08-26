package stride

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// mondayAfter returns the Monday n weeks after the given Monday.
func mondayAfter(start string, n int) string {
	t, err := time.Parse("2006-01-02", start)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 0, 7*n).Format("2006-01-02")
}

const testBlockStart = "2026-08-31"

// sampleMacroPlan builds an unsaved 26-week block starting at testBlockStart.
func sampleMacroPlan(userID int64) (*MacroPlan, []MacroWeek) {
	plan := &MacroPlan{
		UserID:    userID,
		StartWeek: testBlockStart,
		EndWeek:   mondayAfter(testBlockStart, 25),
		Status:    MacroPlanStatusActive,
		Goal: MacroGoal{
			PrimaryFocus:  "half_marathon",
			Statement:     "Run 1:24:00 for the half marathon",
			TargetHMTimeS: 5040,
			Benchmark:     "3 x 3 km at threshold, 90 s float",
			Rationale:     "Current model predicts 1:27; 3 minutes is reachable over 26 weeks.",
			AnchorRaceID:  nil,
		},
		Periodisation: []Mesocycle{
			{Name: "Base 1", Phase: MacroPhaseBase, StartWeek: testBlockStart, Weeks: 8, Focus: "aerobic volume"},
			{Name: "Build 1", Phase: MacroPhaseBuild, StartWeek: mondayAfter(testBlockStart, 8), Weeks: 10, Focus: "threshold density"},
			{Name: "Peak", Phase: MacroPhasePeak, StartWeek: mondayAfter(testBlockStart, 18), Weeks: 6, Focus: "race-specific"},
			{Name: "Taper", Phase: MacroPhaseTaper, StartWeek: mondayAfter(testBlockStart, 24), Weeks: 2, Focus: "freshen up"},
		},
		Prompt:      "You are Stride. Improving half-marathon performance is always the main priority.",
		Response:    `{"goal":{"target_hm_time_s":5040}}`,
		Model:       "claude-opus-5",
		GeneratedBy: MacroGeneratedByScheduled,
	}

	weeks := make([]MacroWeek, 26)
	for i := range weeks {
		phase := MacroPhaseBase
		load := LoadLevelNormal
		switch {
		case i >= 24:
			phase, load = MacroPhaseTaper, LoadLevelTaper
		case i >= 18:
			phase, load = MacroPhasePeak, LoadLevelPeak
		case i >= 8:
			phase, load = MacroPhaseBuild, LoadLevelBuild
		}
		if (i+1)%4 == 0 && i < 24 {
			load = LoadLevelDeload
		}
		weeks[i] = MacroWeek{
			WeekStart:      mondayAfter(testBlockStart, i),
			Seq:            i + 1,
			Phase:          phase,
			Mesocycle:      plan.Periodisation[0].Name,
			LoadLevel:      load,
			TargetKm:       60 + float64(i),
			TargetSessions: 5,
			KeySessions: []KeySession{
				{Type: "threshold", Focus: "controlled tempo"},
				{Type: "long", Focus: "aerobic durability"},
			},
			Intent: "Week " + mondayAfter(testBlockStart, i) + ": build aerobic base without chasing pace.",
			Status: MacroWeekStatusPlanned,
		}
	}
	return plan, weeks
}

func TestCreateMacroPlanRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	if plan.ID == 0 {
		t.Fatal("expected plan ID to be populated")
	}
	if len(plan.Weeks) != 26 {
		t.Fatalf("expected 26 weeks on the returned plan, got %d", len(plan.Weeks))
	}
	for i, w := range plan.Weeks {
		if w.ID == 0 {
			t.Fatalf("week %d: expected ID to be populated", i)
		}
		if w.MacroPlanID != plan.ID {
			t.Fatalf("week %d: macro_plan_id = %d, want %d", i, w.MacroPlanID, plan.ID)
		}
	}

	got, err := GetActiveMacroPlan(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got == nil {
		t.Fatal("expected an active macro plan")
	}
	if got.ID != plan.ID || got.StartWeek != plan.StartWeek || got.EndWeek != plan.EndWeek {
		t.Fatalf("plan identity mismatch: %+v", got)
	}
	if got.Status != MacroPlanStatusActive || got.StaleReason != "" {
		t.Fatalf("status = %q, stale_reason = %q", got.Status, got.StaleReason)
	}
	if got.Model != "claude-opus-5" || got.GeneratedBy != MacroGeneratedByScheduled {
		t.Fatalf("model = %q, generated_by = %q", got.Model, got.GeneratedBy)
	}
	if got.PreviousPlanID != nil {
		t.Fatalf("previous_plan_id = %v, want nil", *got.PreviousPlanID)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be set")
	}

	// Encrypted blobs survive marshal -> encrypt -> decrypt -> unmarshal.
	if got.Goal != plan.Goal {
		t.Fatalf("goal round-trip mismatch:\n got %+v\nwant %+v", got.Goal, plan.Goal)
	}
	if len(got.Periodisation) != len(plan.Periodisation) {
		t.Fatalf("periodisation length = %d, want %d", len(got.Periodisation), len(plan.Periodisation))
	}
	for i := range plan.Periodisation {
		if got.Periodisation[i] != plan.Periodisation[i] {
			t.Fatalf("mesocycle %d mismatch:\n got %+v\nwant %+v", i, got.Periodisation[i], plan.Periodisation[i])
		}
	}
	if got.Prompt != plan.Prompt || got.Response != plan.Response {
		t.Fatalf("prompt/response round-trip mismatch")
	}

	// Weeks come back in seq order with every field intact.
	if len(got.Weeks) != 26 {
		t.Fatalf("loaded %d weeks, want 26", len(got.Weeks))
	}
	for i, w := range got.Weeks {
		want := weeks[i]
		if w.Seq != i+1 {
			t.Fatalf("week %d: seq = %d, want %d", i, w.Seq, i+1)
		}
		if w.WeekStart != want.WeekStart || w.Phase != want.Phase || w.LoadLevel != want.LoadLevel {
			t.Fatalf("week %d: %+v does not match %+v", i, w, want)
		}
		if w.TargetKm != want.TargetKm || w.TargetSessions != want.TargetSessions {
			t.Fatalf("week %d: targets = %v/%v, want %v/%v", i, w.TargetKm, w.TargetSessions, want.TargetKm, want.TargetSessions)
		}
		if w.Mesocycle != want.Mesocycle || w.Status != MacroWeekStatusPlanned {
			t.Fatalf("week %d: mesocycle = %q, status = %q", i, w.Mesocycle, w.Status)
		}
		if w.Intent != want.Intent {
			t.Fatalf("week %d: intent = %q, want %q", i, w.Intent, want.Intent)
		}
		if len(w.KeySessions) != 2 || w.KeySessions[0] != want.KeySessions[0] || w.KeySessions[1] != want.KeySessions[1] {
			t.Fatalf("week %d: key sessions = %+v, want %+v", i, w.KeySessions, want.KeySessions)
		}
		if w.RaceID != nil {
			t.Fatalf("week %d: race_id = %v, want nil", i, *w.RaceID)
		}
	}

	// The initial goal revision is written in the same transaction.
	revs, err := ListGoalRevisions(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("expected 1 initial revision, got %d", len(revs))
	}
	if revs[0].Source != GoalRevisionSourceInitial {
		t.Fatalf("source = %q, want initial", revs[0].Source)
	}
	if revs[0].Reason != "Initial block goal" {
		t.Fatalf("reason = %q", revs[0].Reason)
	}
	if revs[0].Goal != plan.Goal {
		t.Fatalf("revision goal mismatch: %+v", revs[0].Goal)
	}
	if revs[0].WeekStart != testBlockStart {
		t.Fatalf("revision week_start = %q", revs[0].WeekStart)
	}
}

func TestMacroPlanBlobsStoredEncrypted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	var goalJSON, periodisationJSON, prompt, response string
	if err := db.QueryRow(`SELECT goal_json, periodisation_json, prompt, response FROM stride_macro_plans WHERE id = ?`, plan.ID).
		Scan(&goalJSON, &periodisationJSON, &prompt, &response); err != nil {
		t.Fatalf("read raw plan: %v", err)
	}
	for name, raw := range map[string]string{
		"goal_json":          goalJSON,
		"periodisation_json": periodisationJSON,
		"prompt":             prompt,
		"response":           response,
	} {
		if strings.Contains(raw, "half marathon") || strings.Contains(raw, "target_hm_time_s") || strings.Contains(raw, "Stride") {
			t.Fatalf("%s stored as plaintext: %q", name, raw)
		}
	}

	var intent, keySessions string
	if err := db.QueryRow(`SELECT intent, key_sessions_json FROM stride_macro_weeks WHERE macro_plan_id = ? AND seq = 1`, plan.ID).
		Scan(&intent, &keySessions); err != nil {
		t.Fatalf("read raw week: %v", err)
	}
	if strings.Contains(intent, "aerobic base") || strings.Contains(keySessions, "threshold") {
		t.Fatalf("macro week prose stored as plaintext: intent=%q key_sessions=%q", intent, keySessions)
	}

	// Queryable columns stay plaintext so the UI can filter without decrypting.
	var phase, loadLevel, status string
	if err := db.QueryRow(`SELECT phase, load_level, status FROM stride_macro_weeks WHERE macro_plan_id = ? AND seq = 1`, plan.ID).
		Scan(&phase, &loadLevel, &status); err != nil {
		t.Fatalf("read queryable columns: %v", err)
	}
	if phase != MacroPhaseBase || loadLevel != LoadLevelNormal || status != MacroWeekStatusPlanned {
		t.Fatalf("queryable columns = %q/%q/%q", phase, loadLevel, status)
	}
}

func TestGetActiveMacroPlanBoundaries(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	lastWeek := mondayAfter(testBlockStart, 25)

	tests := []struct {
		name  string
		week  string
		found bool
	}{
		{"first week inclusive", testBlockStart, true},
		{"middle week", mondayAfter(testBlockStart, 13), true},
		{"last week inclusive", lastWeek, true},
		{"before horizon", mondayAfter(testBlockStart, -1), false},
		{"after horizon", mondayAfter(lastWeek, 1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetActiveMacroPlan(ctx, db, 1, tc.week)
			if err != nil {
				t.Fatalf("GetActiveMacroPlan: %v", err)
			}
			if tc.found && got == nil {
				t.Fatalf("expected a plan covering %s", tc.week)
			}
			if !tc.found && got != nil {
				t.Fatalf("expected no plan covering %s, got id %d", tc.week, got.ID)
			}
		})
	}
}

func TestGetActiveMacroPlanIgnoresSupersededAndOtherUsers(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	// Another user never sees this plan.
	got, err := GetActiveMacroPlan(ctx, db, 2, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got != nil {
		t.Fatalf("user 2 got user 1's plan (id %d)", got.ID)
	}

	if err := SupersedeMacroPlan(ctx, db, plan.ID, 1); err != nil {
		t.Fatalf("SupersedeMacroPlan: %v", err)
	}
	got, err = GetActiveMacroPlan(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan after supersede: %v", err)
	}
	if got != nil {
		t.Fatalf("superseded plan still returned as active (id %d)", got.ID)
	}

	// It is still reachable by id for history views.
	byID, err := GetMacroPlanByID(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if byID.Status != MacroPlanStatusSuperseded {
		t.Fatalf("status = %q, want superseded", byID.Status)
	}
	if _, err := GetMacroPlanByID(ctx, db, plan.ID, 2); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("cross-user GetMacroPlanByID = %v, want ErrMacroPlanNotFound", err)
	}
}

func TestGetMacroWeek(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	target := mondayAfter(testBlockStart, 18)
	w, err := GetMacroWeek(ctx, db, 1, target)
	if err != nil {
		t.Fatalf("GetMacroWeek: %v", err)
	}
	if w == nil {
		t.Fatalf("expected a macro week for %s", target)
	}
	if w.Seq != 19 || w.Phase != MacroPhasePeak || w.LoadLevel != LoadLevelPeak {
		t.Fatalf("week = seq %d, phase %q, load %q", w.Seq, w.Phase, w.LoadLevel)
	}
	if len(w.KeySessions) != 2 || w.Intent == "" {
		t.Fatalf("encrypted fields did not decrypt: %+v", w)
	}

	// Outside the block there is no week.
	missing, err := GetMacroWeek(ctx, db, 1, mondayAfter(testBlockStart, 40))
	if err != nil {
		t.Fatalf("GetMacroWeek out of range: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for a week outside the block, got %+v", missing)
	}

	// Superseded blocks never drive plan generation.
	if err := SupersedeMacroPlan(ctx, db, plan.ID, 1); err != nil {
		t.Fatalf("SupersedeMacroPlan: %v", err)
	}
	w, err = GetMacroWeek(ctx, db, 1, target)
	if err != nil {
		t.Fatalf("GetMacroWeek after supersede: %v", err)
	}
	if w != nil {
		t.Fatalf("expected nil for a superseded block's week, got id %d", w.ID)
	}
}

func TestCreateMacroPlanUniqueConstraints(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	// UNIQUE(user_id, start_week).
	dup, dupWeeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, dup, dupWeeks, "Initial"); err == nil {
		t.Fatal("expected a UNIQUE(user_id, start_week) violation")
	}

	// UNIQUE(macro_plan_id, week_start): two weeks with the same Monday.
	clash, clashWeeks := sampleMacroPlan(1)
	clash.StartWeek = mondayAfter(testBlockStart, 26)
	clash.EndWeek = mondayAfter(testBlockStart, 51)
	clashWeeks = clashWeeks[:2]
	clashWeeks[1].WeekStart = clashWeeks[0].WeekStart
	if err := CreateMacroPlan(ctx, db, clash, clashWeeks, "Initial"); err == nil {
		t.Fatal("expected a UNIQUE(macro_plan_id, week_start) violation")
	}
}

func TestCreateMacroPlanRollsBackOnWeekFailure(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	weeks = weeks[:3]
	weeks[2].WeekStart = weeks[1].WeekStart // duplicate Monday -> insert fails
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
		t.Fatal("expected the duplicate week insert to fail")
	}

	var plans, weekRows, revisions int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_macro_plans").Scan(&plans); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_macro_weeks").Scan(&weekRows); err != nil {
		t.Fatalf("count weeks: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_goal_revisions").Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if plans != 0 || weekRows != 0 || revisions != 0 {
		t.Fatalf("rollback left orphans: %d plans, %d weeks, %d revisions", plans, weekRows, revisions)
	}
}

func TestCreateMacroPlanValidation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	t.Run("no weeks", func(t *testing.T) {
		plan, _ := sampleMacroPlan(1)
		if err := CreateMacroPlan(ctx, db, plan, nil, "Initial"); err == nil {
			t.Fatal("expected an error for a block with no weeks")
		}
	})
	t.Run("end before start", func(t *testing.T) {
		plan, weeks := sampleMacroPlan(1)
		plan.EndWeek = mondayAfter(testBlockStart, -1)
		if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
			t.Fatal("expected an error for end_week before start_week")
		}
	})
	t.Run("bad phase", func(t *testing.T) {
		plan, weeks := sampleMacroPlan(1)
		weeks[4].Phase = "sharpening"
		if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
			t.Fatal("expected an error for an unknown phase")
		}
	})
	t.Run("bad load level", func(t *testing.T) {
		plan, weeks := sampleMacroPlan(1)
		weeks[4].LoadLevel = "very hard"
		if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
			t.Fatal("expected an error for an unknown load level")
		}
	})
	t.Run("bad generated_by", func(t *testing.T) {
		plan, weeks := sampleMacroPlan(1)
		plan.GeneratedBy = "vibes"
		if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
			t.Fatal("expected an error for an unknown generated_by")
		}
	})
}

func TestMacroPlanExtensionChain(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	first, firstWeeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, first, firstWeeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	if err := SupersedeMacroPlan(ctx, db, first.ID, 1); err != nil {
		t.Fatalf("SupersedeMacroPlan: %v", err)
	}

	// A new 26-week block is appended, never a rolling top-up.
	next, nextWeeks := sampleMacroPlan(1)
	next.StartWeek = mondayAfter(testBlockStart, 26)
	next.EndWeek = mondayAfter(testBlockStart, 51)
	next.GeneratedBy = MacroGeneratedByExtension
	next.PreviousPlanID = &first.ID
	for i := range nextWeeks {
		nextWeeks[i].WeekStart = mondayAfter(next.StartWeek, i)
	}
	if err := CreateMacroPlan(ctx, db, next, nextWeeks, "Extended horizon"); err != nil {
		t.Fatalf("CreateMacroPlan extension: %v", err)
	}

	got, err := GetActiveMacroPlan(ctx, db, 1, next.StartWeek)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got == nil || got.ID != next.ID {
		t.Fatalf("expected the extension block to be active, got %+v", got)
	}
	if got.PreviousPlanID == nil || *got.PreviousPlanID != first.ID {
		t.Fatalf("previous_plan_id = %v, want %d", got.PreviousPlanID, first.ID)
	}
	if got.GeneratedBy != MacroGeneratedByExtension {
		t.Fatalf("generated_by = %q, want extension", got.GeneratedBy)
	}
}

func TestMarkMacroWeekStatus(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	weekID := plan.Weeks[0].ID

	if err := MarkMacroWeekStatus(ctx, db, weekID, 1, MacroWeekStatusMaterialised); err != nil {
		t.Fatalf("MarkMacroWeekStatus: %v", err)
	}
	got, err := GetMacroWeek(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetMacroWeek: %v", err)
	}
	if got.Status != MacroWeekStatusMaterialised {
		t.Fatalf("status = %q, want materialised", got.Status)
	}

	if err := MarkMacroWeekStatus(ctx, db, weekID, 1, "done"); err == nil {
		t.Fatal("expected an error for an invalid status")
	}
	if err := MarkMacroWeekStatus(ctx, db, weekID, 2, MacroWeekStatusSkipped); !errors.Is(err, ErrMacroWeekNotFound) {
		t.Fatalf("cross-user update = %v, want ErrMacroWeekNotFound", err)
	}
	if err := MarkMacroWeekStatus(ctx, db, 999999, 1, MacroWeekStatusSkipped); !errors.Is(err, ErrMacroWeekNotFound) {
		t.Fatalf("missing row update = %v, want ErrMacroWeekNotFound", err)
	}
}

func TestSetMacroPlanStale(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	if err := SetMacroPlanStale(ctx, db, plan.ID, 1, MacroStaleRacesChanged); err != nil {
		t.Fatalf("SetMacroPlanStale: %v", err)
	}
	got, err := GetActiveMacroPlan(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got == nil {
		t.Fatal("a stale plan must stay active — nothing is auto-regenerated")
	}
	if got.StaleReason != MacroStaleRacesChanged {
		t.Fatalf("stale_reason = %q, want %q", got.StaleReason, MacroStaleRacesChanged)
	}

	// Regenerating clears the flag.
	if err := SetMacroPlanStale(ctx, db, plan.ID, 1, ""); err != nil {
		t.Fatalf("clear stale: %v", err)
	}
	got, err = GetActiveMacroPlan(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got.StaleReason != "" {
		t.Fatalf("stale_reason = %q, want empty", got.StaleReason)
	}

	if err := SetMacroPlanStale(ctx, db, plan.ID, 2, MacroStaleRacesChanged); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("cross-user stale = %v, want ErrMacroPlanNotFound", err)
	}
	if err := SupersedeMacroPlan(ctx, db, 999999, 1); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("missing plan supersede = %v, want ErrMacroPlanNotFound", err)
	}
}

func TestGoalRevisionsAppendOnly(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	base := plan.CreatedAt
	drift := plan.Goal
	drift.TargetHMTimeS = 4980 // within the +/-3% auto-apply band
	drift.Statement = "Run 1:23:00 for the half marathon"
	second := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      1,
		WeekStart:   mondayAfter(testBlockStart, 4),
		Goal:        drift,
		Reason:      "Prediction model improved by 1.2%, inside the auto-apply band.",
		Source:      GoalRevisionSourceWeekly,
		CreatedAt:   base.Add(time.Hour),
	}
	if err := AddGoalRevision(ctx, db, second); err != nil {
		t.Fatalf("AddGoalRevision: %v", err)
	}
	if second.ID == 0 {
		t.Fatal("expected revision ID to be populated")
	}

	manual := drift
	manual.TargetHMTimeS = 4920
	third := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      1,
		WeekStart:   mondayAfter(testBlockStart, 8),
		Goal:        manual,
		Reason:      "Robin accepted the proposed goal change.",
		Source:      GoalRevisionSourceManual,
		CreatedAt:   base.Add(2 * time.Hour),
	}
	if err := AddGoalRevision(ctx, db, third); err != nil {
		t.Fatalf("AddGoalRevision manual: %v", err)
	}

	revs, err := ListGoalRevisions(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(revs))
	}
	wantSources := []string{GoalRevisionSourceInitial, GoalRevisionSourceWeekly, GoalRevisionSourceManual}
	wantTargets := []int{5040, 4980, 4920}
	for i, r := range revs {
		if r.Source != wantSources[i] {
			t.Fatalf("revision %d source = %q, want %q", i, r.Source, wantSources[i])
		}
		if r.Goal.TargetHMTimeS != wantTargets[i] {
			t.Fatalf("revision %d target = %d, want %d", i, r.Goal.TargetHMTimeS, wantTargets[i])
		}
		if i > 0 && r.CreatedAt.Before(revs[i-1].CreatedAt) {
			t.Fatalf("revisions are not in chronological order at %d", i)
		}
	}
	// The original revision is untouched by later appends.
	if revs[0].Reason != "Initial block goal" || revs[0].Goal.Statement != "Run 1:24:00 for the half marathon" {
		t.Fatalf("initial revision was mutated: %+v", revs[0])
	}

	if err := AddGoalRevision(ctx, db, &GoalRevision{MacroPlanID: plan.ID, UserID: 1, WeekStart: testBlockStart, Source: "guess"}); err == nil {
		t.Fatal("expected an error for an unknown revision source")
	}
	if err := AddGoalRevision(ctx, db, &GoalRevision{MacroPlanID: plan.ID, UserID: 1, Source: GoalRevisionSourceWeekly}); err == nil {
		t.Fatal("expected an error for a missing week_start")
	}
}

func TestGoalRevisionReasonEncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	var reason string
	if err := db.QueryRow(`SELECT reason FROM stride_goal_revisions WHERE macro_plan_id = ?`, plan.ID).Scan(&reason); err != nil {
		t.Fatalf("read raw reason: %v", err)
	}
	if strings.Contains(reason, "Initial block goal") {
		t.Fatalf("goal revision reason stored as plaintext: %q", reason)
	}
}

func TestMacroPlanCascades(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	// A weekly plan linked to a macro week is unlinked, not deleted, when the
	// macro week goes away.
	weekID := plan.Weeks[0].ID
	if _, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, model, macro_week_id, created_at)
		VALUES (1, ?, ?, 'base', '{}', 'claude-opus-5', ?, '2026-08-31T02:00:00Z')
	`, testBlockStart, mondayAfter(testBlockStart, 1), weekID); err != nil {
		t.Fatalf("insert stride plan: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM stride_macro_weeks WHERE id = ?`, weekID); err != nil {
		t.Fatalf("delete macro week: %v", err)
	}
	var macroWeekID sql.NullInt64
	if err := db.QueryRow(`SELECT macro_week_id FROM stride_plans WHERE user_id = 1 AND week_start = ?`, testBlockStart).Scan(&macroWeekID); err != nil {
		t.Fatalf("read stride plan: %v", err)
	}
	if macroWeekID.Valid {
		t.Fatalf("macro_week_id = %d, want NULL after the macro week was deleted", macroWeekID.Int64)
	}

	// Deleting the plan removes its remaining weeks and its goal history.
	if _, err := db.Exec(`DELETE FROM stride_macro_plans WHERE id = ?`, plan.ID); err != nil {
		t.Fatalf("delete macro plan: %v", err)
	}
	var weekRows, revisions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_macro_weeks WHERE macro_plan_id = ?`, plan.ID).Scan(&weekRows); err != nil {
		t.Fatalf("count weeks: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_goal_revisions WHERE macro_plan_id = ?`, plan.ID).Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if weekRows != 0 || revisions != 0 {
		t.Fatalf("cascade left %d weeks and %d revisions", weekRows, revisions)
	}
}

func TestPlanCarriesMacroColumns(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	weekID := plan.Weeks[0].ID

	summary, err := encryption.EncryptField("Cut Thursday's threshold to 4 x 6 min — travel week.")
	if err != nil {
		t.Fatalf("encrypt summary: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, model, macro_week_id, adjustment_summary, created_at)
		VALUES (1, ?, ?, 'base', '{"days":[]}', 'claude-opus-5', ?, ?, '2026-08-31T02:00:00Z')
	`, testBlockStart, mondayAfter(testBlockStart, 1), weekID, summary); err != nil {
		t.Fatalf("insert stride plan: %v", err)
	}

	got, err := GetCurrentPlan(db, 1, "2026-09-03")
	if err != nil {
		t.Fatalf("GetCurrentPlan: %v", err)
	}
	if got == nil {
		t.Fatal("expected a current plan")
	}
	if got.MacroWeekID == nil || *got.MacroWeekID != weekID {
		t.Fatalf("macro_week_id = %v, want %d", got.MacroWeekID, weekID)
	}
	if got.AdjustmentSummary != "Cut Thursday's threshold to 4 x 6 min — travel week." {
		t.Fatalf("adjustment_summary = %q", got.AdjustmentSummary)
	}

	// Plans generated without a macro plan keep a NULL link and an empty summary.
	if _, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, model, created_at)
		VALUES (1, ?, ?, 'base', '{"days":[]}', 'claude-opus-5', '2026-09-07T02:00:00Z')
	`, mondayAfter(testBlockStart, 1), mondayAfter(testBlockStart, 2)); err != nil {
		t.Fatalf("insert unlinked stride plan: %v", err)
	}
	unlinked, err := GetCurrentPlan(db, 1, "2026-09-10")
	if err != nil {
		t.Fatalf("GetCurrentPlan unlinked: %v", err)
	}
	if unlinked == nil || unlinked.MacroWeekID != nil || unlinked.AdjustmentSummary != "" {
		t.Fatalf("unlinked plan = %+v", unlinked)
	}
}
