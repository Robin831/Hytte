package stride

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// macroPriorityRuleVerbatim is the half-marathon priority rule exactly as the
// epic states it. It is duplicated here on purpose: the test is what stops the
// constant being reworded, so it must not read the constant it checks.
const macroPriorityRuleVerbatim = "improving half-marathon performance is always the main priority. No races -> a half-marathon development block with a concrete target HM time and a benchmark session. 5 km / 10 km on the calendar -> B/C races embedded in HM training: sharpen at most 1-2 weeks, no full taper, never restructure the block around them. Only a half marathon (or longer) A-priority race may define the peak and taper."

// approxTokens is a rough token count for budget assertions: English prose runs
// about 4 characters per token, which is close enough to catch a prompt that
// has doubled in size without making the test brittle.
func approxTokens(s string) int { return len(s) / 4 }

// setPref writes a user preference, encrypting it when the key is one Stride
// stores encrypted at rest.
func setPref(t *testing.T, db *sql.DB, userID int64, key, value string) {
	t.Helper()
	if key == "stride_custom_prompt" || key == treadmillCalibrationPref {
		enc, err := encryption.EncryptField(value)
		if err != nil {
			t.Fatalf("encrypt %s: %v", key, err)
		}
		value = enc
	}
	if _, err := db.Exec(
		"INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)",
		userID, key, value,
	); err != nil {
		t.Fatalf("set pref %s: %v", key, err)
	}
}

func TestMacroSystemPromptSections(t *testing.T) {
	prompt := macroSystemPrompt()

	if !strings.Contains(prompt, bakkenPhilosophy) {
		t.Error("macro system prompt does not embed bakkenPhilosophy")
	}
	for _, section := range []string{
		"## Periodisation",
		"## Half-Marathon Priority",
		"## Output Format",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("macro system prompt is missing section %q", section)
		}
	}

	// The periodisation doctrine the block is judged against.
	for _, rule := range []string{
		"3 progressive build weeks followed by 1 deload week",
		"60-70%",
		"base -> build -> peak -> taper",
		"no more than +10%",
		"2-week taper",
	} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("periodisation section is missing rule %q", rule)
		}
	}

	// The JSON contract sub-task 2 mirrors in its response types.
	for _, field := range []string{
		`"goal"`, `"mesocycles"`, `"weeks"`,
		"primary_focus", "target_hm_time", "anchor_race_id",
		"start_week", "week_start", "load", "target_km", "key_sessions",
		"library_id", "race_id",
	} {
		if !strings.Contains(prompt, field) {
			t.Errorf("output contract is missing field %q", field)
		}
	}
	if !strings.Contains(prompt, "exactly 26 objects") {
		t.Error("output contract does not pin the block to 26 weeks")
	}
}

func TestMacroSystemPromptPriorityRuleVerbatim(t *testing.T) {
	if !strings.Contains(macroHalfMarathonRule, macroPriorityRuleVerbatim) {
		t.Errorf("macroHalfMarathonRule does not contain the rule verbatim.\ngot:\n%s", macroHalfMarathonRule)
	}
	if !strings.Contains(macroSystemPrompt(), macroPriorityRuleVerbatim) {
		t.Error("assembled macro system prompt does not contain the priority rule verbatim")
	}
}

// seedMacroInputFixtures populates the athlete data buildMacroInputs reads,
// plus the three things it must ignore: treadmill calibration, the legacy
// goal_race_* preferences and stride notes.
func seedMacroInputFixtures(t *testing.T, db *sql.DB, userID int64, startWeek string) {
	t.Helper()
	ctx := context.Background()

	setPref(t, db, userID, "max_hr", "185")
	setPref(t, db, userID, "threshold_hr", "166")
	setPref(t, db, userID, "threshold_pace", "268")
	setPref(t, db, userID, "stride_available_days", "5")
	setPref(t, db, userID, "stride_weekly_distance_cap", "70")
	setPref(t, db, userID, "stride_custom_prompt", "CUSTOM-PROMPT-MARKER: no hills on Tuesdays.")

	// Must NOT reach the prompt.
	setPref(t, db, userID, treadmillCalibrationPref, "CALIBRATION-MARKER: belt offset 3%.")
	setPref(t, db, userID, "goal_race_name", "GOAL-RACE-PREF-MARKER")
	setPref(t, db, userID, "goal_race_date", "2027-05-01")

	encNote, err := encryption.EncryptField("STRIDE-NOTE-MARKER: calf felt tight.")
	if err != nil {
		t.Fatalf("encrypt note: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO stride_notes (user_id, content, target_date, scope, created_at)
		VALUES (?, ?, '', 'any', ?)`,
		userID, encNote, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	start, err := parseWeekDate(startWeek)
	if err != nil {
		t.Fatalf("parse start week: %v", err)
	}

	// A race inside the horizon, one just past the lookahead, and one already run.
	inHorizon := start.AddDate(0, 0, 7*20).Format(dateLayout)
	beyond := start.AddDate(0, 0, 7*(MacroBlockWeeks+macroRaceLookaheadWeeks+3)).Format(dateLayout)
	target := 5040
	if _, err := CreateRace(db, userID, "Oslo Half Marathon", inHorizon, 21097.5, &target, "A", ""); err != nil {
		t.Fatalf("create in-horizon race: %v", err)
	}
	if _, err := CreateRace(db, userID, "FAR-FUTURE-RACE-MARKER", beyond, 10000, nil, "C", ""); err != nil {
		t.Fatalf("create beyond-horizon race: %v", err)
	}

	pastRace, err := CreateRace(db, userID, "Bergen Half", start.AddDate(0, 0, -7*30).Format(dateLayout), 21097.5, nil, "A", "")
	if err != nil {
		t.Fatalf("create past race: %v", err)
	}
	if _, err := db.Exec("UPDATE stride_races SET result_time = ? WHERE id = ?", 5220, pastRace.ID); err != nil {
		t.Fatalf("set result time: %v", err)
	}
	// listRaceResults only counts races with a linked workout.
	if _, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, started_at, duration_seconds, distance_meters, avg_heart_rate, race_id, fit_file_hash)
		VALUES (?, 'running', ?, 5220, 21097.5, 172, ?, 'macro-race')`,
		userID, start.AddDate(0, 0, -7*30).Format(time.RFC3339), pastRace.ID,
	); err != nil {
		t.Fatalf("insert race workout: %v", err)
	}

	// Two completed weeks of running so the history table has real rows.
	for _, weeksBack := range []int{2, 3} {
		monday := start.AddDate(0, 0, -7*weeksBack)
		for day := 0; day < 3; day++ {
			// fit_file_hash is UNIQUE per user and defaults to '', so every
			// seeded workout needs its own value.
			if _, err := db.Exec(`
				INSERT INTO workouts (user_id, sport, started_at, duration_seconds, distance_meters, avg_heart_rate, training_load, fit_file_hash)
				VALUES (?, 'running', ?, 3600, 12000, 145, 90, ?)`,
				userID, monday.AddDate(0, 0, day).Format(time.RFC3339),
				fmt.Sprintf("macro-w%d-d%d", weeksBack, day),
			); err != nil {
				t.Fatalf("insert workout: %v", err)
			}
		}
		// A stride plan covering the same week, so planned-vs-completed merges in.
		if _, err := db.Exec(`
			INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			userID,
			monday.Format(dateLayout),
			monday.AddDate(0, 0, 6).Format(dateLayout),
			`[{"date":"`+monday.Format(dateLayout)+`","rest_day":false,"session":{"main_set":"6x6min"}},`+
				`{"date":"`+monday.AddDate(0, 0, 2).Format(dateLayout)+`","rest_day":false,"session":{"main_set":"easy"}},`+
				`{"date":"`+monday.AddDate(0, 0, 4).Format(dateLayout)+`","rest_day":true}]`,
			time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			t.Fatalf("insert stride plan: %v", err)
		}
	}

	if err := SeedReferenceWorkout(ctx, db, userID); err != nil {
		t.Fatalf("seed reference workout: %v", err)
	}
}

func TestBuildMacroInputsSections(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	startWeek, _ := upcomingWeek()
	seedMacroInputFixtures(t, db, 1, startWeek)

	inputs, err := buildMacroInputs(ctx, db, 1, startWeek, MacroModeInitial)
	if err != nil {
		t.Fatalf("buildMacroInputs: %v", err)
	}

	for _, section := range []string{
		"## Planning Request",
		"## Athlete Profile",
		"## Current Fitness Estimate",
		"## Upcoming Races",
		"## Race Results",
		"## Athlete Constraints",
		"## Training History (last 26 weeks)",
		"## Current Training Load",
		"## VO2max Trend",
		"## Recent Lactate Tests",
		"## Workout Library",
		"## Additional Instructions",
	} {
		if !strings.Contains(inputs, section) {
			t.Errorf("inputs are missing section %q", section)
		}
	}

	// The request itself.
	if !strings.Contains(inputs, "Block start week (Monday): "+startWeek) {
		t.Errorf("inputs do not state the block start week %q", startWeek)
	}
	start, err := parseWeekDate(startWeek)
	if err != nil {
		t.Fatalf("parse start week: %v", err)
	}
	endWeek := start.AddDate(0, 0, 7*(MacroBlockWeeks-1)).Format(dateLayout)
	if !strings.Contains(inputs, endWeek) {
		t.Errorf("inputs do not state the block end week %q", endWeek)
	}
	if !strings.Contains(inputs, "Mode: initial") {
		t.Error("inputs do not state the generation mode")
	}

	// Every one of the 26 history weeks gets a row, data or not.
	for i := 1; i <= macroHistoryWeeks; i++ {
		week := start.AddDate(0, 0, -7*i).Format(dateLayout)
		if !strings.Contains(inputs, "| "+week+" |") {
			t.Errorf("history table is missing a row for week %s", week)
		}
	}
	// Two of them carry a plan, so planned-vs-completed merged in.
	if !strings.Contains(inputs, "| 2/0 |") {
		t.Error("history table does not carry planned/completed session counts")
	}

	// Verbatim blocks.
	for _, want := range []string{
		"Max HR: 185 bpm",
		"Threshold HR: 166 bpm",
		"Training days per week: 5",
		"Weekly distance cap: 70 km",
		"CUSTOM-PROMPT-MARKER",
		"Oslo Half Marathon",
		"priority A",
		"Bergen Half",
		"6x6min threshold (reference)", // the seeded library workout
		"[WEEKLY REFERENCE]",
	} {
		if !strings.Contains(inputs, want) {
			t.Errorf("inputs are missing %q", want)
		}
	}

	// Deliberate omissions.
	for _, unwanted := range []string{
		"CALIBRATION-MARKER",
		treadmillCalibrationTitle,
		"GOAL-RACE-PREF-MARKER",
		"Goal Race:",
		"STRIDE-NOTE-MARKER",
		"FAR-FUTURE-RACE-MARKER",
	} {
		if strings.Contains(inputs, unwanted) {
			t.Errorf("inputs must not contain %q", unwanted)
		}
	}

	// The library is names and types only — no session bodies.
	if strings.Contains(inputs, "warmup:") || strings.Contains(inputs, "main set:") {
		t.Error("workout library section leaked full session bodies")
	}
}

func TestBuildMacroInputsExtensionMode(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "initial goal"); err != nil {
		t.Fatalf("create macro plan: %v", err)
	}

	// The next block starts the week after the stored block ends.
	nextStart := mondayAfter(testBlockStart, MacroBlockWeeks)

	extension, err := buildMacroInputs(ctx, db, 1, nextStart, MacroModeExtension)
	if err != nil {
		t.Fatalf("buildMacroInputs(extension): %v", err)
	}
	if !strings.Contains(extension, "## Previous Block") {
		t.Fatal("extension mode did not embed the previous block")
	}
	if !strings.Contains(extension, "Mode: extension") {
		t.Error("extension mode is not stated in the planning request")
	}
	// Goal and periodisation, verbatim.
	if !strings.Contains(extension, "Run 1:24:00 for the half marathon") {
		t.Error("extension mode did not embed the previous goal")
	}
	if !strings.Contains(extension, `"target_hm_time_s":5040`) {
		t.Error("extension mode did not embed the previous goal as JSON")
	}
	if !strings.Contains(extension, "threshold density") {
		t.Error("extension mode did not embed the previous periodisation")
	}
	// The last 8 week specs, and only those: week intents are unique per week.
	for i := 18; i <= 25; i++ {
		intent := "Week " + mondayAfter(testBlockStart, i) + ": build aerobic base"
		if !strings.Contains(extension, intent) {
			t.Errorf("extension mode is missing the week spec for seq %d", i+1)
		}
	}
	for _, i := range []int{0, 17} {
		intent := "Week " + mondayAfter(testBlockStart, i) + ": build aerobic base"
		if strings.Contains(extension, intent) {
			t.Errorf("extension mode embedded week seq %d — only the last 8 belong", i+1)
		}
	}

	// Initial mode must not carry any of it.
	initial, err := buildMacroInputs(ctx, db, 1, nextStart, MacroModeInitial)
	if err != nil {
		t.Fatalf("buildMacroInputs(initial): %v", err)
	}
	if strings.Contains(initial, "## Previous Block") {
		t.Error("initial mode embedded the previous block")
	}
	if strings.Contains(initial, "Run 1:24:00 for the half marathon") {
		t.Error("initial mode embedded the previous goal")
	}
}

func TestBuildMacroInputsExtensionModeWithoutPreviousBlock(t *testing.T) {
	db := setupTestDB(t)
	startWeek, _ := upcomingWeek()

	inputs, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroModeExtension)
	if err != nil {
		t.Fatalf("buildMacroInputs: %v", err)
	}
	if !strings.Contains(inputs, "No previous macro block found") {
		t.Error("extension mode with no previous block should say so, not fail")
	}
}

func TestBuildMacroInputsEmptyAthlete(t *testing.T) {
	db := setupTestDB(t)
	startWeek, _ := upcomingWeek()

	// An athlete with no data at all still yields a usable input block: every
	// optional source degrades to a "none recorded" line rather than erroring.
	inputs, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroModeInitial)
	if err != nil {
		t.Fatalf("buildMacroInputs: %v", err)
	}
	for _, want := range []string{
		"No race prediction recorded.",
		"No races on the calendar within the horizon.",
		"No completed races recorded.",
		"No VO2max estimates recorded.",
		"No lactate tests recorded.",
		"No library workouts recorded.",
		"Training days per week: 5 (default)",
		"Weekly distance cap: none set.",
	} {
		if !strings.Contains(inputs, want) {
			t.Errorf("empty-athlete inputs are missing %q", want)
		}
	}
	if strings.Contains(inputs, "## Additional Instructions") {
		t.Error("no custom prompt is set, so the section should be omitted")
	}
}

func TestBuildMacroInputsInvalidStartWeek(t *testing.T) {
	db := setupTestDB(t)
	if _, err := buildMacroInputs(context.Background(), db, 1, "not-a-date", MacroModeInitial); err == nil {
		t.Fatal("expected an error for an unparseable start week")
	}
}

func TestMacroPromptSizeBudget(t *testing.T) {
	db := setupTestDB(t)
	startWeek, _ := upcomingWeek()
	seedMacroInputFixtures(t, db, 1, startWeek)

	inputs, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroModeInitial)
	if err != nil {
		t.Fatalf("buildMacroInputs: %v", err)
	}

	// Ceilings, not targets: the inputs aim at ~6-7k tokens and the whole
	// prompt lands well under the limits below. They catch a section that has
	// stopped summarising (raw workouts, full library bodies), not normal drift.
	if got := approxTokens(inputs); got > 9000 {
		t.Errorf("macro inputs ≈%d tokens, over the 9000 ceiling", got)
	}
	full := macroSystemPrompt() + "\n\n" + inputs
	if got := approxTokens(full); got > 14000 {
		t.Errorf("full macro prompt ≈%d tokens, over the 14000 ceiling", got)
	}
	if approxTokens(full) < 1000 {
		t.Errorf("full macro prompt ≈%d tokens — suspiciously small", approxTokens(full))
	}
	t.Logf("macro prompt ≈%d tokens (inputs ≈%d)", approxTokens(full), approxTokens(inputs))
}

// Guard the helper the history table depends on: the 26 weeks must be the ones
// directly before the block start, oldest first.
func TestMacroHistoryWeekOrdering(t *testing.T) {
	db := setupTestDB(t)
	start, err := parseWeekDate("2026-08-31")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	table, err := buildMacroHistoryTable(db, 1, start)
	if err != nil {
		t.Fatalf("buildMacroHistoryTable: %v", err)
	}
	var lastIdx = -1
	for i := macroHistoryWeeks; i >= 1; i-- {
		week := start.AddDate(0, 0, -7*i).Format(dateLayout)
		idx := strings.Index(table, "| "+week+" |")
		if idx < 0 {
			t.Fatalf("history table is missing week %s", week)
		}
		if idx < lastIdx {
			t.Fatalf("history table is not oldest-first at week %s", week)
		}
		lastIdx = idx
	}
	if strings.Contains(table, "| "+start.Format(dateLayout)+" |") {
		t.Error("history table must not include the block's own start week")
	}
	rows := strings.Count(table, "\n| 20")
	// One header row starts with "|------", so only data rows match "| 20".
	if rows != macroHistoryWeeks {
		t.Errorf("history table has %d data rows, want %d", rows, macroHistoryWeeks)
	}
	if !strings.Contains(table, fmt.Sprintf("last %d weeks", macroHistoryWeeks)) {
		t.Error("history table heading does not state the window")
	}
}
