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
	if got.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}
	if _, err := time.Parse(time.RFC3339, got.CreatedAt); err != nil {
		t.Fatalf("created_at %q is not RFC3339: %v", got.CreatedAt, err)
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

	// Only one active block per (user_id, start_week).
	dup, dupWeeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, dup, dupWeeks, "Initial"); err == nil {
		t.Fatal("expected a second active block on the same Monday to be rejected")
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

// A Regenerate replaces the block covering the current week, so the new block
// starts on the same Monday as the one it replaces. Setting PreviousPlanID
// demotes the old block in the same transaction that inserts the new one, which
// frees the slot; its rows and goal history survive.
func TestRegenerateReplacesBlockOnTheSameMonday(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	first, firstWeeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, first, firstWeeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	if err := SetMacroPlanStale(ctx, db, first.ID, 1, MacroStaleRacesChanged); err != nil {
		t.Fatalf("SetMacroPlanStale: %v", err)
	}

	regenerated, regeneratedWeeks := sampleMacroPlan(1)
	regenerated.GeneratedBy = MacroGeneratedByManual
	regenerated.PreviousPlanID = &first.ID
	regenerated.Goal.Statement = "Run 1:23:00 for the half marathon"
	if err := CreateMacroPlan(ctx, db, regenerated, regeneratedWeeks, "Regenerated after the race calendar changed"); err != nil {
		t.Fatalf("regenerate on the same start_week: %v", err)
	}

	got, err := GetActiveMacroPlan(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got == nil || got.ID != regenerated.ID {
		t.Fatalf("expected the regenerated block to be active, got %+v", got)
	}
	if got.StaleReason != "" || got.GeneratedBy != MacroGeneratedByManual {
		t.Fatalf("stale_reason = %q, generated_by = %q", got.StaleReason, got.GeneratedBy)
	}

	// The superseded block keeps its rows and its goal history.
	old, err := GetMacroPlanByID(ctx, db, first.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if old.Status != MacroPlanStatusSuperseded || len(old.Weeks) != 26 {
		t.Fatalf("superseded block = status %q with %d weeks", old.Status, len(old.Weeks))
	}
	revs, err := ListGoalRevisions(ctx, db, first.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revs) != 1 || revs[0].Goal.Statement != "Run 1:24:00 for the half marathon" {
		t.Fatalf("superseded goal history = %+v", revs)
	}

	// Two superseded blocks may share a Monday — history is not deduplicated.
	third, thirdWeeks := sampleMacroPlan(1)
	third.PreviousPlanID = &regenerated.ID
	if err := CreateMacroPlan(ctx, db, third, thirdWeeks, "Regenerated again"); err != nil {
		t.Fatalf("second regenerate: %v", err)
	}
	var superseded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_macro_plans WHERE user_id = 1 AND start_week = ? AND status = ?`,
		testBlockStart, MacroPlanStatusSuperseded).Scan(&superseded); err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	if superseded != 2 {
		t.Fatalf("superseded blocks on %s = %d, want 2", testBlockStart, superseded)
	}
}

// A regenerate that fails must not demote the block it was replacing: the two
// writes are one transaction, so the user is never left with zero active
// blocks and no way back.
func TestFailedRegenerateLeavesThePreviousBlockActive(t *testing.T) {
	ctx := context.Background()

	// Each case makes CreateMacroPlan fail *after* the point where a
	// supersede-then-create protocol would already have demoted the old block.
	cases := []struct {
		name   string
		mutate func(*MacroPlan, []MacroWeek)
	}{
		{"foreign race reference", func(p *MacroPlan, w []MacroWeek) {
			foreign := int64(20)
			p.Goal.AnchorRaceID = &foreign
		}},
		{"duplicate week Monday", func(p *MacroPlan, w []MacroWeek) {
			w[1].WeekStart = w[0].WeekStart
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')"); err != nil {
				t.Fatalf("insert second user: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO stride_races (id, user_id, name, date, distance_m, priority, created_at)
				VALUES (20, 2, 'Someone else''s HM', '2027-02-01', 21097.5, 'A', '2026-08-01T00:00:00Z')`); err != nil {
				t.Fatalf("insert race: %v", err)
			}

			first, firstWeeks := sampleMacroPlan(1)
			if err := CreateMacroPlan(ctx, db, first, firstWeeks, "Initial"); err != nil {
				t.Fatalf("CreateMacroPlan: %v", err)
			}

			doomed, doomedWeeks := sampleMacroPlan(1)
			doomed.GeneratedBy = MacroGeneratedByManual
			doomed.PreviousPlanID = &first.ID
			tc.mutate(doomed, doomedWeeks)
			if err := CreateMacroPlan(ctx, db, doomed, doomedWeeks, "Regenerated"); err == nil {
				t.Fatal("expected the regenerate to fail")
			}

			got, err := GetActiveMacroPlan(ctx, db, 1, testBlockStart)
			if err != nil {
				t.Fatalf("GetActiveMacroPlan: %v", err)
			}
			if got == nil {
				t.Fatal("a failed regenerate left the user with no active block")
			}
			if got.ID != first.ID || got.Status != MacroPlanStatusActive {
				t.Fatalf("active block = %+v, want the original %d still active", got, first.ID)
			}
			// The weekly generator still sees its macro context.
			week, err := GetMacroWeek(ctx, db, 1, testBlockStart)
			if err != nil {
				t.Fatalf("GetMacroWeek: %v", err)
			}
			if week == nil || week.MacroPlanID != first.ID {
				t.Fatalf("macro week = %+v, want one from plan %d", week, first.ID)
			}
			var plans int
			if err := db.QueryRow("SELECT COUNT(*) FROM stride_macro_plans").Scan(&plans); err != nil {
				t.Fatalf("count plans: %v", err)
			}
			if plans != 1 {
				t.Fatalf("plan count = %d, want 1 — the failed block left rows behind", plans)
			}
		})
	}
}

// PreviousPlanID is a write, so it is scoped like every other one: a caller
// cannot retire somebody else's block by naming it as the one being replaced.
func TestCreateMacroPlanRejectsForeignPreviousPlan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	victim, victimWeeks := sampleMacroPlan(2)
	if err := CreateMacroPlan(ctx, db, victim, victimWeeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan for user 2: %v", err)
	}

	attacker, attackerWeeks := sampleMacroPlan(1)
	attacker.PreviousPlanID = &victim.ID
	if err := CreateMacroPlan(ctx, db, attacker, attackerWeeks, "Regenerated"); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("CreateMacroPlan with a foreign previous_plan_id = %v, want ErrMacroPlanNotFound", err)
	}

	got, err := GetActiveMacroPlan(ctx, db, 2, testBlockStart)
	if err != nil {
		t.Fatalf("GetActiveMacroPlan: %v", err)
	}
	if got == nil || got.ID != victim.ID {
		t.Fatalf("user 2's block = %+v, want %d still active", got, victim.ID)
	}

	// A previous_plan_id that does not exist at all fails the same way.
	missing := int64(999999)
	orphan, orphanWeeks := sampleMacroPlan(1)
	orphan.PreviousPlanID = &missing
	if err := CreateMacroPlan(ctx, db, orphan, orphanWeeks, "Regenerated"); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("CreateMacroPlan with a missing previous_plan_id = %v, want ErrMacroPlanNotFound", err)
	}
}

// seedForeignReferenceFixtures gives user 1 race 10 / library workout 30 and
// user 2 race 20 / library workout 40, so a test can point a block at a row it
// does not own.
func seedForeignReferenceFixtures(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stride_races (id, user_id, name, date, distance_m, priority, created_at)
		VALUES (10, 1, 'Oslo HM', '2027-02-01', 21097.5, 'A', '2026-08-01T00:00:00Z'),
		       (20, 2, 'Someone else''s HM', '2027-02-01', 21097.5, 'A', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert races: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stride_workouts (id, user_id, name, created_at)
		VALUES (30, 1, 'Threshold 3x3km', '2026-08-01T00:00:00Z'),
		       (40, 2, 'Someone else''s session', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert library workouts: %v", err)
	}
	return db
}

// idPtr is the pointer form the optional FK fields (AnchorRaceID, RaceID,
// LibraryID) take.
func idPtr(v int64) *int64 { return &v }

func TestCreateMacroPlanRejectsForeignReferences(t *testing.T) {
	ctx := context.Background()

	seed := seedForeignReferenceFixtures
	id := idPtr

	tests := []struct {
		name    string
		mutate  func(*MacroPlan, []MacroWeek)
		wantErr bool
	}{
		{"own race and library workout", func(p *MacroPlan, w []MacroWeek) {
			p.Goal.AnchorRaceID = id(10)
			p.Periodisation[3].RaceID = id(10)
			w[25].RaceID = id(10)
			w[0].KeySessions[0].LibraryID = id(30)
		}, false},
		{"week race_id owned by another user", func(p *MacroPlan, w []MacroWeek) {
			w[25].RaceID = id(20)
		}, true},
		{"anchor race owned by another user", func(p *MacroPlan, w []MacroWeek) {
			p.Goal.AnchorRaceID = id(20)
		}, true},
		{"mesocycle race owned by another user", func(p *MacroPlan, w []MacroWeek) {
			p.Periodisation[3].RaceID = id(20)
		}, true},
		{"key session library workout owned by another user", func(p *MacroPlan, w []MacroWeek) {
			w[0].KeySessions[0].LibraryID = id(40)
		}, true},
		{"race that does not exist", func(p *MacroPlan, w []MacroWeek) {
			w[25].RaceID = id(999999)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := seed(t)
			plan, weeks := sampleMacroPlan(1)
			tc.mutate(plan, weeks)

			err := CreateMacroPlan(ctx, db, plan, weeks, "Initial")
			if tc.wantErr {
				if !errors.Is(err, ErrForeignReference) {
					t.Fatalf("CreateMacroPlan = %v, want ErrForeignReference", err)
				}
				var plans int
				if qerr := db.QueryRow("SELECT COUNT(*) FROM stride_macro_plans").Scan(&plans); qerr != nil {
					t.Fatalf("count plans: %v", qerr)
				}
				if plans != 0 {
					t.Fatalf("rejected block still wrote %d plan rows", plans)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateMacroPlan: %v", err)
			}
		})
	}
}

func TestCreateMacroPlanDefaultsLoadLevel(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	weeks[0].LoadLevel = "" // empty means "use the column default", like Status
	weeks[0].Status = ""
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	if plan.Weeks[0].LoadLevel != LoadLevelNormal || plan.Weeks[0].Status != MacroWeekStatusPlanned {
		t.Fatalf("week defaults = %q/%q", plan.Weeks[0].LoadLevel, plan.Weeks[0].Status)
	}

	got, err := GetMacroWeek(ctx, db, 1, testBlockStart)
	if err != nil {
		t.Fatalf("GetMacroWeek: %v", err)
	}
	if got.LoadLevel != LoadLevelNormal || got.Status != MacroWeekStatusPlanned {
		t.Fatalf("stored defaults = %q/%q", got.LoadLevel, got.Status)
	}
}

func TestValidateStaleReason(t *testing.T) {
	if err := ValidateStaleReason(""); err != nil {
		t.Fatalf("empty reason (clears the flag) = %v, want nil", err)
	}
	if err := ValidateStaleReason(MacroStaleRacesChanged); err != nil {
		t.Fatalf("races_changed = %v, want nil", err)
	}
	if err := ValidateStaleReason("races-changed"); err == nil {
		t.Fatal("expected a typo'd stale reason to be rejected")
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

	// A new 26-week block is appended, never a rolling top-up. PreviousPlanID
	// retires the block it follows as part of the same write.
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

	retired, err := GetMacroPlanByID(ctx, db, first.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if retired.Status != MacroPlanStatusSuperseded {
		t.Fatalf("previous block status = %q, want superseded", retired.Status)
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

	if err := SetMacroPlanStale(ctx, db, plan.ID, 1, "races-changed"); err == nil {
		t.Fatal("expected an unknown stale reason to be rejected")
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

	base, err := time.Parse(time.RFC3339, plan.CreatedAt)
	if err != nil {
		t.Fatalf("parse plan created_at: %v", err)
	}
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
		CreatedAt:   base.Add(time.Hour).Format(time.RFC3339),
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
		CreatedAt:   base.Add(2 * time.Hour).Format(time.RFC3339),
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
	}
	assertChronological(t, revs)
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

func TestAddGoalRevisionRejectsForeignPlan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}

	// User 2 tries to append to user 1's goal history.
	foreign := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      2,
		WeekStart:   mondayAfter(testBlockStart, 4),
		Goal:        plan.Goal,
		Reason:      "Injected by another user.",
		Source:      GoalRevisionSourceManual,
	}
	if err := AddGoalRevision(ctx, db, foreign); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("cross-user AddGoalRevision = %v, want ErrMacroPlanNotFound", err)
	}
	if foreign.ID != 0 {
		t.Fatalf("rejected revision got an ID (%d) — a row was written", foreign.ID)
	}

	// A revision for a plan that does not exist at all is rejected the same way.
	if err := AddGoalRevision(ctx, db, &GoalRevision{
		MacroPlanID: 999999,
		UserID:      1,
		WeekStart:   testBlockStart,
		Source:      GoalRevisionSourceWeekly,
	}); !errors.Is(err, ErrMacroPlanNotFound) {
		t.Fatalf("missing-plan AddGoalRevision = %v, want ErrMacroPlanNotFound", err)
	}

	// Nothing landed: only the initial revision from CreateMacroPlan remains.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_goal_revisions`).Scan(&n); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if n != 1 {
		t.Fatalf("goal revision count = %d, want 1", n)
	}
}

// The "a block can never pin another user's race" invariant has to survive
// every write to the goal, not just the block's first one — AddGoalRevision is
// the path the weekly and manual goal-drift flows use.
func TestAddGoalRevisionRejectsForeignReferences(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		anchor  *int64
		wantErr error
	}{
		{"no anchor race", nil, nil},
		{"own anchor race", idPtr(10), nil},
		{"anchor race owned by another user", idPtr(20), ErrForeignReference},
		{"anchor race that does not exist", idPtr(999999), ErrForeignReference},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := seedForeignReferenceFixtures(t)
			plan, weeks := sampleMacroPlan(1)
			if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
				t.Fatalf("CreateMacroPlan: %v", err)
			}

			goal := plan.Goal
			goal.AnchorRaceID = tc.anchor
			rev := &GoalRevision{
				MacroPlanID: plan.ID,
				UserID:      1,
				WeekStart:   mondayAfter(testBlockStart, 4),
				Goal:        goal,
				Reason:      "Goal drifted with the race calendar.",
				Source:      GoalRevisionSourceWeekly,
			}
			err := AddGoalRevision(ctx, db, rev)

			var revisions int
			if qerr := db.QueryRow(`SELECT COUNT(*) FROM stride_goal_revisions WHERE macro_plan_id = ?`, plan.ID).Scan(&revisions); qerr != nil {
				t.Fatalf("count revisions: %v", qerr)
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("AddGoalRevision = %v, want %v", err, tc.wantErr)
				}
				if rev.ID != 0 {
					t.Fatalf("rejected revision got an ID (%d) — a row was written", rev.ID)
				}
				if revisions != 1 {
					t.Fatalf("revision count = %d, want 1 (just the initial one)", revisions)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddGoalRevision: %v", err)
			}
			if revisions != 2 {
				t.Fatalf("revision count = %d, want 2", revisions)
			}
			revs, err := ListGoalRevisions(ctx, db, plan.ID, 1)
			if err != nil {
				t.Fatalf("ListGoalRevisions: %v", err)
			}
			got := revs[1].Goal.AnchorRaceID
			if (got == nil) != (tc.anchor == nil) || (got != nil && *got != *tc.anchor) {
				t.Fatalf("stored anchor_race_id = %v, want %v", got, tc.anchor)
			}
		})
	}
}

// created_at is a TEXT column sorted with ORDER BY, so every write normalises
// to UTC RFC3339: a same-instant value written with a +02:00 offset must sort
// by the instant it names, not by its leading digits.
func TestCreatedAtNormalisedToUTC(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	plan.CreatedAt = "2026-08-26T16:37:00+02:00" // 14:37Z
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial block goal"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	if plan.CreatedAt != "2026-08-26T14:37:00Z" {
		t.Fatalf("plan.CreatedAt = %q, want the UTC rendering", plan.CreatedAt)
	}
	stored, err := GetMacroPlanByID(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("GetMacroPlanByID: %v", err)
	}
	if stored.CreatedAt != "2026-08-26T14:37:00Z" {
		t.Fatalf("stored created_at = %q, want the UTC rendering", stored.CreatedAt)
	}

	// 15:00+02:00 is 13:00Z — one hour *before* the initial revision. Stored
	// verbatim it would sort last; normalised it sorts first.
	earlier := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      1,
		WeekStart:   mondayAfter(testBlockStart, 4),
		Goal:        plan.Goal,
		Reason:      "chronologically earlier",
		Source:      GoalRevisionSourceWeekly,
		CreatedAt:   "2026-08-26T15:00:00+02:00",
	}
	if err := AddGoalRevision(ctx, db, earlier); err != nil {
		t.Fatalf("AddGoalRevision: %v", err)
	}
	if earlier.CreatedAt != "2026-08-26T13:00:00Z" {
		t.Fatalf("revision CreatedAt = %q, want the UTC rendering", earlier.CreatedAt)
	}

	revs, err := ListGoalRevisions(ctx, db, plan.ID, 1)
	if err != nil {
		t.Fatalf("ListGoalRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revs))
	}
	if revs[0].Reason != "chronologically earlier" {
		t.Fatalf("revisions out of chronological order: %q then %q (%s, %s)",
			revs[0].Reason, revs[1].Reason, revs[0].CreatedAt, revs[1].CreatedAt)
	}
	assertChronological(t, revs)
}

// A created_at that is not a timestamp is rejected at write time rather than
// round-tripping out of the store unflagged.
func TestCreatedAtRejectsMalformedTimestamps(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, bad := range []string{"not a timestamp", "2026-08-26", "2026-08-26 14:37:00"} {
		plan, weeks := sampleMacroPlan(1)
		plan.CreatedAt = bad
		if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err == nil {
			t.Fatalf("CreateMacroPlan with created_at %q = nil, want an error", bad)
		}
	}
	var plans int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_macro_plans").Scan(&plans); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if plans != 0 {
		t.Fatalf("rejected plans still wrote %d rows", plans)
	}

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "Initial"); err != nil {
		t.Fatalf("CreateMacroPlan: %v", err)
	}
	rev := &GoalRevision{
		MacroPlanID: plan.ID,
		UserID:      1,
		WeekStart:   mondayAfter(testBlockStart, 4),
		Goal:        plan.Goal,
		Source:      GoalRevisionSourceWeekly,
		CreatedAt:   "not a timestamp",
	}
	if err := AddGoalRevision(ctx, db, rev); err == nil {
		t.Fatal("AddGoalRevision with a malformed created_at = nil, want an error")
	}
	if rev.ID != 0 {
		t.Fatalf("rejected revision got an ID (%d) — a row was written", rev.ID)
	}
	var revisions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stride_goal_revisions`).Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revisions != 1 {
		t.Fatalf("revision count = %d, want 1 (just the initial one)", revisions)
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

// assertChronological parses each revision's created_at and checks the list is
// ordered by instant. Comparing the raw strings would only re-assert the SQL
// ORDER BY against itself and could never fail.
func assertChronological(t *testing.T, revs []GoalRevision) {
	t.Helper()
	for i, r := range revs {
		at, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			t.Fatalf("revision %d has an unparseable created_at %q: %v", i, r.CreatedAt, err)
		}
		if i == 0 {
			continue
		}
		prev, err := time.Parse(time.RFC3339, revs[i-1].CreatedAt)
		if err != nil {
			t.Fatalf("revision %d has an unparseable created_at %q: %v", i-1, revs[i-1].CreatedAt, err)
		}
		if at.Before(prev) {
			t.Fatalf("revisions are not in chronological order at %d: %s before %s", i, r.CreatedAt, revs[i-1].CreatedAt)
		}
	}
}
