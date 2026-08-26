package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
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
	prompt := macroInstructions

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
		"one hard session every 1-2 weeks",
		"2-week taper",
	} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("periodisation section is missing rule %q", rule)
		}
	}

	// The JSON contract sub-task 2 mirrors in its response types.
	for _, field := range []string{
		`"goal"`, `"mesocycles"`, `"weeks"`,
		"primary_focus", "target_hm_time_s", "anchor_race_id",
		"start_week", "week_start", "load_level", "target_km", "key_sessions",
		"target_sessions", "library_id", "race_id",
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
	if !strings.Contains(macroInstructions, macroPriorityRuleVerbatim) {
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

	inputs, err := buildMacroInputs(ctx, db, 1, startWeek, MacroModeScheduled)
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
	if !strings.Contains(inputs, "Mode: scheduled") {
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
	if !strings.Contains(inputs, "| 2/? |") {
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
	initial, err := buildMacroInputs(ctx, db, 1, nextStart, MacroModeScheduled)
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
	inputs, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroModeScheduled)
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
	if _, err := buildMacroInputs(context.Background(), db, 1, "not-a-date", MacroModeScheduled); err == nil {
		t.Fatal("expected an error for an unparseable start week")
	}
}

func TestMacroPromptSizeBudget(t *testing.T) {
	db := setupTestDB(t)
	startWeek, _ := upcomingWeek()
	seedMacroInputFixtures(t, db, 1, startWeek)

	inputs, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroModeScheduled)
	if err != nil {
		t.Fatalf("buildMacroInputs: %v", err)
	}

	// Ceilings, not targets: the inputs aim at ~6-7k tokens and the whole
	// prompt lands well under the limits below. They catch a section that has
	// stopped summarising (raw workouts, full library bodies), not normal drift.
	if got := approxTokens(inputs); got > 9000 {
		t.Errorf("macro inputs ≈%d tokens, over the 9000 ceiling", got)
	}
	full := macroInstructions + "\n\n" + inputs
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

// The extension prompt embeds the persisted MacroGoal as JSON right next to the
// output contract. If the contract asked for different key names the model
// would echo the ones it just saw, and the validator would read a zero target —
// so every persisted key must be the key the contract asks for.
func TestMacroOutputContractMatchesPersistedJSONTags(t *testing.T) {
	goal, err := json.Marshal(MacroGoal{})
	if err != nil {
		t.Fatalf("marshal goal: %v", err)
	}
	week, err := json.Marshal(MacroWeek{})
	if err != nil {
		t.Fatalf("marshal week: %v", err)
	}
	for _, key := range []string{
		"primary_focus", "target_hm_time_s", "anchor_race_id", "benchmark",
		"week_start", "phase", "mesocycle", "load_level", "target_km",
		"target_sessions", "key_sessions", "race_id",
	} {
		if !strings.Contains(string(goal), `"`+key+`"`) && !strings.Contains(string(week), `"`+key+`"`) {
			t.Fatalf("test key %q is not a persisted JSON tag — fix the test", key)
		}
		if !strings.Contains(macroOutputContract, `"`+key+`"`) {
			t.Errorf("output contract does not use the persisted key %q", key)
		}
	}
}

func TestStripGoalRaceSection(t *testing.T) {
	cases := []struct {
		name  string
		block string
		want  string
	}{
		{"empty", "", ""},
		{"header only", "User Profile:\nGoal Race: 2026-05-01\n", ""},
		{
			"keeps body",
			"User Profile:\n- Max HR: 190 bpm\nGoal Race:\n- Date: 2026-05-01\n",
			"User Profile:\n- Max HR: 190 bpm\n",
		},
		// The goal race is trailing today, but nothing in the training package
		// promises that. Anything after it must survive the strip — including
		// the plain top-level bullets BuildUserProfileBlock actually emits,
		// which share the goal race's "- key: value" shape.
		{
			"goal race in the middle",
			"User Profile:\n- Max HR: 190 bpm\nGoal Race:\n- Event: Oslo\n- Date: 2026-05-01\n- Threshold Pace: 4:28/km\n- Training Zones (custom):\n  Zone 1 (Recovery): 100-138 bpm\n",
			"User Profile:\n- Max HR: 190 bpm\n- Threshold Pace: 4:28/km\n- Training Zones (custom):\n  Zone 1 (Recovery): 100-138 bpm\n",
		},
		{
			"inline goal race in the middle",
			"User Profile:\n- Max HR: 190 bpm\nGoal Race: 2026-05-01\n- Threshold Pace: 4:28/km\n",
			"User Profile:\n- Max HR: 190 bpm\n- Threshold Pace: 4:28/km\n",
		},
		// A blank separator after the goal race must end the section too, not
		// carry it across the gap.
		{
			"blank line ends the section",
			"User Profile:\n- Max HR: 190 bpm\nGoal Race:\n- Event: Oslo\n\n- Threshold Pace: 4:28/km\n",
			"User Profile:\n- Max HR: 190 bpm\n\n- Threshold Pace: 4:28/km\n",
		},
		{
			"no goal race",
			"User Profile:\n- Max HR: 190 bpm\n- Threshold HR: 166 bpm\n",
			"User Profile:\n- Max HR: 190 bpm\n- Threshold HR: 166 bpm\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripGoalRaceSection(tc.block); got != tc.want {
				t.Errorf("stripGoalRaceSection(%q) = %q, want %q", tc.block, got, tc.want)
			}
		})
	}
}

// The strip recognises the goal race by its own bullet vocabulary, so that
// vocabulary has to match what training.BuildUserProfileBlock actually emits —
// hand-written fixtures cannot pin that. A goal-race field added upstream and
// not added to goalRaceBullets would leak into the prompt and contradict the
// block's own goal, which is the whole reason this strip exists.
func TestStripGoalRaceSectionMatchesProducerVocabulary(t *testing.T) {
	db := setupTestDB(t)
	setPref(t, db, 1, "goal_race_name", "Oslo Half")
	setPref(t, db, 1, "goal_race_date", "2027-05-01")
	setPref(t, db, 1, "goal_race_distance", "21.1")
	setPref(t, db, 1, "goal_race_target_time", "1:27:00")

	block := training.BuildUserProfileBlock(db, 1)
	if !strings.Contains(block, "Goal Race:") {
		t.Fatalf("producer emitted no goal race to strip:\n%s", block)
	}
	// Goal race only: every line of the block is either the profile header or a
	// goal-race line, so a complete vocabulary leaves nothing behind.
	if got := stripGoalRaceSection(block); got != "" {
		t.Errorf("goal-race lines survived the strip — goalRaceBullets is missing a key the producer emits\nblock:\n%s\nkept:\n%s", block, got)
	}
}

// The producer emits the goal race last today. Nothing enforces that, so the
// strip is tested against the producer's real output with the goal race moved
// to the front: every remaining line — top-level "- key: value" bullets and the
// indented zone lines — must survive.
func TestStripGoalRaceSectionKeepsProducerBulletsAfterGoalRace(t *testing.T) {
	db := setupTestDB(t)
	setPref(t, db, 1, "max_hr", "190")
	setPref(t, db, 1, "threshold_hr", "166")
	setPref(t, db, 1, "threshold_pace", "268")
	setPref(t, db, 1, "goal_race_name", "Oslo Half")
	setPref(t, db, 1, "goal_race_date", "2027-05-01")
	setPref(t, db, 1, "goal_race_target_time", "1:27:00")

	block := training.BuildUserProfileBlock(db, 1)
	idx := strings.Index(block, "Goal Race:")
	if idx < 0 {
		t.Fatalf("producer emitted no goal race:\n%s", block)
	}
	// Rebuild the producer's own output with the goal race directly under the
	// "User Profile:" header instead of at the end.
	header := "User Profile:\n"
	body := strings.TrimPrefix(block[:idx], header)
	moved := header + block[idx:] + body

	got := stripGoalRaceSection(moved)
	for _, want := range []string{
		"- Max HR: 190 bpm",
		"- Threshold Pace: 4:28/km",
		"- Training Zones (Olympiatoppen, estimated from max HR):",
		"  Zone 1 (I1 - Recovery): 0-114 bpm",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("strip swallowed %q when the goal race came first\ninput:\n%s\nkept:\n%s", want, moved, got)
		}
	}
	for _, unwanted := range []string{"Goal Race:", "Oslo Half", "1:27:00", "2027-05-01"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("goal-race line %q survived the strip\nkept:\n%s", unwanted, got)
		}
	}
}

// macroBlockWeeksStr is hand-maintained because a const expression cannot call
// strconv. Nothing else notices when it drifts from MacroBlockWeeks: the prompt
// would keep asking for "exactly 26 objects" while the validator expected a
// different count, and every generation would fail validation.
func TestMacroBlockWeeksStrMatchesConstant(t *testing.T) {
	if macroBlockWeeksStr != strconv.Itoa(MacroBlockWeeks) {
		t.Fatalf("macroBlockWeeksStr = %q but MacroBlockWeeks = %d — update both", macroBlockWeeksStr, MacroBlockWeeks)
	}
	if macroHistoryWeeks != MacroBlockWeeks {
		t.Errorf("macroHistoryWeeks = %d, want it to track MacroBlockWeeks (%d)", macroHistoryWeeks, MacroBlockWeeks)
	}
	if !strings.Contains(macroOutputContract, "exactly "+strconv.Itoa(MacroBlockWeeks)+" objects") {
		t.Error("output contract does not pin the block to MacroBlockWeeks weeks")
	}
}

// MacroMode must stay one-to-one with the generated_by vocabulary the store
// validates, so a mode can be persisted as string(mode) with no mapping.
func TestMacroModeMatchesGeneratedBy(t *testing.T) {
	for _, tc := range []struct {
		mode        MacroMode
		generatedBy string
	}{
		{MacroModeScheduled, MacroGeneratedByScheduled},
		{MacroModeExtension, MacroGeneratedByExtension},
		{MacroModeManual, MacroGeneratedByManual},
	} {
		if string(tc.mode) != tc.generatedBy {
			t.Errorf("mode %q does not match generated_by %q", tc.mode, tc.generatedBy)
		}
		if !tc.mode.valid() {
			t.Errorf("mode %q reports itself invalid", tc.mode)
		}
		if tc.mode.describe() == "" {
			t.Errorf("mode %q has no prompt description", tc.mode)
		}
		// The store must accept the mode verbatim as generated_by.
		plan := &MacroPlan{UserID: 1, StartWeek: testBlockStart, EndWeek: testBlockStart, GeneratedBy: string(tc.mode)}
		if err := validateMacroPlanRow(plan); err != nil {
			t.Errorf("validateMacroPlanRow rejected generated_by %q: %v", tc.mode, err)
		}
	}
	if MacroMode("initial").valid() {
		t.Error("\"initial\" is not a generated_by value and must not be a valid mode")
	}
}

func TestBuildMacroInputsRejectsUnknownMode(t *testing.T) {
	db := setupTestDB(t)
	startWeek, _ := upcomingWeek()
	if _, err := buildMacroInputs(context.Background(), db, 1, startWeek, MacroMode("initial")); err == nil {
		t.Fatal("expected an error for a mode outside the generated_by vocabulary")
	}
}

// A start week that is not a Monday matches no summary and no plan key, so the
// whole history table would render as dashes. That must be an error, not a
// silently empty table the coach reads as "this athlete has not trained".
func TestBuildMacroInputsRejectsNonMondayStartWeek(t *testing.T) {
	db := setupTestDB(t)
	// 2026-09-02 is a Wednesday.
	_, err := buildMacroInputs(context.Background(), db, 1, "2026-09-02", MacroModeScheduled)
	if err == nil {
		t.Fatal("expected an error for a start week that is not a Monday")
	}
	if !strings.Contains(err.Error(), "Monday") {
		t.Errorf("error should name the Monday requirement, got %v", err)
	}

	wednesday, perr := parseWeekDate("2026-09-02")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if _, err := buildMacroHistoryTable(db, 1, wednesday); err == nil {
		t.Fatal("buildMacroHistoryTable should reject a non-Monday window start")
	}
}

// seedMondayWorkout inserts one running workout on the given date. hash keeps
// the per-user UNIQUE(fit_file_hash) constraint satisfied.
func seedWorkout(t *testing.T, db *sql.DB, userID int64, date string, hr int, hash string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, started_at, duration_seconds, distance_meters, avg_heart_rate, training_load, fit_file_hash)
		VALUES (?, 'running', ?, 3600, 10000, ?, 90, ?)`,
		userID, date+"T10:00:00Z", hr, hash,
	)
	if err != nil {
		t.Fatalf("insert workout %s: %v", date, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("workout id: %v", err)
	}
	return id
}

// seedStridePlan inserts a plan for the week starting at monday with n
// non-rest sessions.
func seedStridePlan(t *testing.T, db *sql.DB, userID int64, monday time.Time, sessions int) int64 {
	t.Helper()
	days := make([]string, 0, sessions)
	for i := 0; i < sessions; i++ {
		days = append(days, `{"date":"`+monday.AddDate(0, 0, i).Format(dateLayout)+`","rest_day":false,"session":{"main_set":"easy"}}`)
	}
	res, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		userID, monday.Format(dateLayout), monday.AddDate(0, 0, 6).Format(dateLayout),
		"["+strings.Join(days, ",")+"]", time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert plan for %s: %v", monday.Format(dateLayout), err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("plan id: %v", err)
	}
	return id
}

// historyRow returns the rendered history row for a week, without the trailing
// newline, so a test can assert on the whole row rather than a substring that
// might match another column.
func historyRow(t *testing.T, table, week string) string {
	t.Helper()
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| "+week+" |") {
			return line
		}
	}
	t.Fatalf("history table has no row for week %s\n%s", week, table)
	return ""
}

// training.WeeklySummaries groups by strftime('%Y-%W') but derives week_start
// as the Monday, so a week straddling New Year comes back as two rows sharing
// one week_start. The merge in buildMacroHistoryTable has to accumulate them:
// assigning would drop half the week's volume and report the last row's HR as
// the week's average.
func TestMacroHistoryMergesDuplicateWeekStarts(t *testing.T) {
	db := setupTestDB(t)

	// 2026-12-28 is a Monday. '%Y-%W' puts 2026-12-28/29 in 2026-52 and
	// 2027-01-01 in 2027-00, but both derive week_start 2026-12-28.
	seedWorkout(t, db, 1, "2026-12-28", 140, "ny-1")
	seedWorkout(t, db, 1, "2026-12-29", 140, "ny-2")
	seedWorkout(t, db, 1, "2027-01-01", 170, "ny-3")

	start, err := parseMondayWeek("2027-01-18")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	table, err := buildMacroHistoryTable(db, 1, start)
	if err != nil {
		t.Fatalf("buildMacroHistoryTable: %v", err)
	}

	row := historyRow(t, table, "2026-12-28")
	// 3 x 10 km, 3 x 1 h, and a workout-count-weighted HR of (140+140+170)/3.
	for _, want := range []string{"| 30.0 |", "| 3.0 |", "| 3 |", "| 150 |"} {
		if !strings.Contains(row, want) {
			t.Errorf("merged New Year row missing %q\nrow: %s", want, row)
		}
	}
	if strings.Contains(row, "| 170 |") {
		t.Errorf("avg HR is the last summary row's value, not the weighted mean\nrow: %s", row)
	}
}

// seedEvaluation inserts one stride evaluation for a plan, encrypting eval_json
// the way the production writer does.
func seedEvaluation(t *testing.T, db *sql.DB, userID, planID int64, workoutID *int64, plannedType, compliance string) {
	t.Helper()
	evalJSON, err := encryption.EncryptField(fmt.Sprintf(
		`{"planned_type":%q,"actual_type":"easy","compliance":%q}`, plannedType, compliance))
	if err != nil {
		t.Fatalf("encrypt eval: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		userID, planID, workoutID, evalJSON, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert evaluation: %v", err)
	}
}

// The Evaluated column has two different zeros: a plan nobody evaluated, and a
// plan that was evaluated and matched nothing. Only the first is unknown. The
// second — an athlete who trains every week but replaces every prescription —
// is the strongest signal a 26-week block gets, so it must render as a real 0
// and never be collapsed into "?".
func TestMacroHistoryAdherenceDistinguishesUnevaluatedFromMissed(t *testing.T) {
	db := setupTestDB(t)
	start, err := parseMondayWeek("2026-08-31")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}

	// Week A: plan + workouts, never evaluated.
	weekA := start.AddDate(0, 0, -7*3)
	seedStridePlan(t, db, 1, weekA, 2)
	seedWorkout(t, db, 1, weekA.Format(dateLayout), 145, "adh-a1")
	seedWorkout(t, db, 1, weekA.AddDate(0, 0, 2).Format(dateLayout), 145, "adh-a2")

	// Week B: plan + workouts, one session evaluated as compliant.
	weekB := start.AddDate(0, 0, -7*4)
	planB := seedStridePlan(t, db, 1, weekB, 2)
	workoutB := seedWorkout(t, db, 1, weekB.Format(dateLayout), 150, "adh-b1")
	seedEvaluation(t, db, 1, planB, &workoutB, "threshold", "compliant")

	// Week C: plan, no workouts, no evaluations — nothing was ever assessed.
	weekC := start.AddDate(0, 0, -7*5)
	seedStridePlan(t, db, 1, weekC, 2)

	// Week D: plan + workouts, both sessions evaluated as non-compliant. The
	// week IS known: the athlete trained and matched none of the plan.
	weekD := start.AddDate(0, 0, -7*6)
	planD := seedStridePlan(t, db, 1, weekD, 2)
	workoutD1 := seedWorkout(t, db, 1, weekD.Format(dateLayout), 140, "adh-d1")
	workoutD2 := seedWorkout(t, db, 1, weekD.AddDate(0, 0, 2).Format(dateLayout), 140, "adh-d2")
	seedEvaluation(t, db, 1, planD, &workoutD1, "threshold", "non_compliant")
	seedEvaluation(t, db, 1, planD, &workoutD2, "threshold", "non_compliant")

	table, err := buildMacroHistoryTable(db, 1, start)
	if err != nil {
		t.Fatalf("buildMacroHistoryTable: %v", err)
	}
	if got := historyRow(t, table, weekA.Format(dateLayout)); !strings.Contains(got, "| 2/? |") {
		t.Errorf("unevaluated week should read 2/?, got: %s", got)
	}
	if got := historyRow(t, table, weekB.Format(dateLayout)); !strings.Contains(got, "| 2/1 |") {
		t.Errorf("evaluated week should read 2/1, got: %s", got)
	}
	if got := historyRow(t, table, weekC.Format(dateLayout)); !strings.Contains(got, "| 2/? |") {
		t.Errorf("week whose plan has no evaluations at all should read 2/?, got: %s", got)
	}
	if got := historyRow(t, table, weekD.Format(dateLayout)); !strings.Contains(got, "| 2/0 |") {
		t.Errorf("evaluated week that matched nothing must read 2/0, not 2/? — total prescription drift is a known week, got: %s", got)
	}
	// The legend has to explain the column, or the coach reads it as adherence.
	for _, want := range []string{"Planned/Evaluated", "never evaluated", "matched none of the plan"} {
		if !strings.Contains(table, want) {
			t.Errorf("history legend is missing %q", want)
		}
	}
}

// The history merge assigns one plan row per week instead of deduplicating,
// which is only correct because stride_plans is unique on (user_id,
// week_start) — a regenerated week upserts in place. If that constraint were
// ever dropped, a regenerated week would arrive as two rows and the merge would
// quote whichever one the query happened to return last.
func TestStridePlansAreUniquePerUserWeek(t *testing.T) {
	db := setupTestDB(t)
	monday, err := parseMondayWeek("2026-08-17")
	if err != nil {
		t.Fatalf("parse monday: %v", err)
	}
	seedStridePlan(t, db, 1, monday, 3)

	_, err = db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at)
		VALUES (?, ?, ?, '[]', ?)`,
		1, monday.Format(dateLayout), monday.AddDate(0, 0, 6).Format(dateLayout),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err == nil {
		t.Fatal("a second plan for the same user and week was accepted — the macro history merge assumes one plan per week")
	}

	// A different user may hold the same week.
	if _, err := db.Exec(
		"INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')",
	); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	seedStridePlan(t, db, 2, monday, 3)
}

// GetPlanHistory pages back from today by rows, not from the block start by
// weeks. A manual regeneration whose start week is in the past has plan rows
// newer than the window sitting in front of it, so the window's oldest weeks
// only survive if the loader pages until it reaches them.
func TestMacroHistoryPlanWindowReachesOldestWeek(t *testing.T) {
	db := setupTestDB(t)

	// A block start ten weeks in the past, with a plan for every week from the
	// window's oldest week up to last week: 26 window weeks plus 10 newer ones.
	thisMonday := time.Now().UTC()
	thisMonday = thisMonday.AddDate(0, 0, -(int(thisMonday.Weekday()+6) % 7))
	start := thisMonday.AddDate(0, 0, -7*10)
	for i := 1; i <= macroHistoryWeeks+10; i++ {
		monday := start.AddDate(0, 0, -7*i)
		if monday.After(thisMonday.AddDate(0, 0, -7)) {
			continue
		}
		seedStridePlan(t, db, 1, monday, 3)
	}
	for i := 1; i <= 10; i++ {
		monday := start.AddDate(0, 0, 7*i)
		if !monday.Before(thisMonday) {
			continue
		}
		seedStridePlan(t, db, 1, monday, 3)
	}

	table, err := buildMacroHistoryTable(db, 1, start)
	if err != nil {
		t.Fatalf("buildMacroHistoryTable: %v", err)
	}
	oldest := start.AddDate(0, 0, -7*macroHistoryWeeks).Format(dateLayout)
	// 3 planned, never evaluated: the plan columns are only populated at all if
	// the loader paged back this far.
	if got := historyRow(t, table, oldest); !strings.Contains(got, "| 3/? |") {
		t.Errorf("oldest window week lost its plan columns — the page did not reach it\nrow: %s", got)
	}
}

// fakePlanPager serves plan-history pages from a fixed newest-first list of
// week starts, recording every call. It exists because GetPlanHistory's own
// 156-week depth cap makes the loop's page cap unreachable through the
// database, and because the window bound must be pinned independently of how
// many rows a real athlete happens to have.
type fakePlanPager struct {
	weeks    []string // newest first
	calls    [][2]int // {limit, offset} per call
	infinite bool     // never stop advertising more pages
}

func (f *fakePlanPager) page(limit, offset int) ([]WeekSummary, bool, error) {
	f.calls = append(f.calls, [2]int{limit, offset})
	if f.infinite {
		out := make([]WeekSummary, limit)
		for i := range out {
			// Always newer than any window bound a caller will pass.
			out[i] = WeekSummary{WeekStart: "2099-01-04", PlanID: int64(offset + i + 1)}
		}
		return out, true, nil
	}
	if offset >= len(f.weeks) {
		return nil, false, nil
	}
	end := offset + limit
	if end > len(f.weeks) {
		end = len(f.weeks)
	}
	out := make([]WeekSummary, 0, end-offset)
	for i := offset; i < end; i++ {
		out = append(out, WeekSummary{WeekStart: f.weeks[i], PlanID: int64(i + 1)})
	}
	return out, end < len(f.weeks), nil
}

// weeksBack returns n week starts counting back from a Monday, newest first.
func weeksBack(monday time.Time, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, monday.AddDate(0, 0, -7*i).Format(dateLayout))
	}
	return out
}

// The bound is the OLDEST week of the window. Bounding on the newest week (the
// block start minus one) would be satisfied by the last row of the very first
// page and the loop would stop there, silently dropping every older week.
func TestCollectMacroPlanWeeksPagesToOldestWeek(t *testing.T) {
	monday, err := parseMondayWeek("2026-08-31")
	if err != nil {
		t.Fatalf("parse monday: %v", err)
	}
	// 60 plan rows, one per week back from last week: more than two pages.
	pager := &fakePlanPager{weeks: weeksBack(monday.AddDate(0, 0, -7), 60)}
	windowStart := monday.AddDate(0, 0, -7*macroHistoryWeeks).Format(dateLayout)

	got, err := collectMacroPlanWeeks(pager.page, windowStart, 1)
	if err != nil {
		t.Fatalf("collectMacroPlanWeeks: %v", err)
	}
	if len(pager.calls) < 2 {
		t.Fatalf("stopped after %d page(s) — the loop bound on the newest week, not the oldest", len(pager.calls))
	}
	var reached bool
	for _, w := range got {
		if w.WeekStart == windowStart {
			reached = true
		}
	}
	if !reached {
		t.Errorf("paging never reached the window's oldest week %s (got %d rows)", windowStart, len(got))
	}
	// It must also stop once it has: pages are 26 rows, so week 26 of the
	// window lands on page 2 and page 3 must never be requested.
	if len(pager.calls) != 2 {
		t.Errorf("expected exactly 2 pages, got %d: %v", len(pager.calls), pager.calls)
	}
	if pager.calls[1] != [2]int{macroPlanHistoryPage, macroPlanHistoryPage} {
		t.Errorf("second page did not advance by the page size: %v", pager.calls[1])
	}
}

// hasMore=false is the end of the athlete's history (or GetPlanHistory's own
// depth cap). The loop must return on it rather than asking for a page past
// the end.
func TestCollectMacroPlanWeeksStopsWhenNoMorePages(t *testing.T) {
	monday, err := parseMondayWeek("2026-08-31")
	if err != nil {
		t.Fatalf("parse monday: %v", err)
	}
	// Fewer rows than the window is deep, all inside it: the bound is never
	// crossed, so only hasMore can end the loop.
	pager := &fakePlanPager{weeks: weeksBack(monday.AddDate(0, 0, -7), 5)}
	windowStart := monday.AddDate(0, 0, -7*macroHistoryWeeks).Format(dateLayout)

	got, err := collectMacroPlanWeeks(pager.page, windowStart, 1)
	if err != nil {
		t.Fatalf("collectMacroPlanWeeks: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("got %d weeks, want all 5", len(got))
	}
	if len(pager.calls) != 1 {
		t.Errorf("queried %d pages for a single short page: %v", len(pager.calls), pager.calls)
	}
}

// A pager that keeps advertising more pages of in-window rows must not spin:
// the loop stops at macroPlanHistoryMaxPages and truncates. GetPlanHistory's
// depth cap makes this unreachable in production today, which is exactly why
// nothing else would notice if the cap regressed.
func TestCollectMacroPlanWeeksStopsAtPageCap(t *testing.T) {
	pager := &fakePlanPager{infinite: true}

	got, err := collectMacroPlanWeeks(pager.page, "2026-01-05", 1)
	if err != nil {
		t.Fatalf("collectMacroPlanWeeks: %v", err)
	}
	if len(pager.calls) != macroPlanHistoryMaxPages {
		t.Errorf("made %d page calls, want the cap of %d", len(pager.calls), macroPlanHistoryMaxPages)
	}
	if want := macroPlanHistoryPage * macroPlanHistoryMaxPages; len(got) != want {
		t.Errorf("got %d rows, want %d", len(got), want)
	}
}

// Every caller that computes a block start — the Monday cron, the extension
// path, a manual start from the UI — routes through NormaliseMacroStartWeek,
// because buildMacroInputs rejects a non-Monday outright.
func TestNormaliseMacroStartWeek(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-08-31", "2026-08-31"}, // Monday: unchanged
		{"2026-09-01", "2026-08-31"}, // Tuesday
		{"2026-09-06", "2026-08-31"}, // Sunday snaps back, not forward
		{"2026-09-05", "2026-08-31"}, // Saturday
	}
	for _, tc := range cases {
		got, err := NormaliseMacroStartWeek(tc.in)
		if err != nil {
			t.Fatalf("NormaliseMacroStartWeek(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("NormaliseMacroStartWeek(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := NormaliseMacroStartWeek("not-a-date"); err == nil {
		t.Error("a date that is not a date must still be an error")
	}
}

// The extension path derives its start from a previous block's EndWeek plus 7
// days, and macro_store never validates that EndWeek is a Monday. Normalising
// first is what keeps a stored Sunday EndWeek from failing generation.
func TestNormaliseMacroStartWeekCoversExtensionDerivedStart(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	// A previous block whose stored end week is a Sunday.
	prevEndWeek := "2026-08-30"
	normalised, err := NormaliseMacroStartWeek(prevEndWeek)
	if err != nil {
		t.Fatalf("normalise end week: %v", err)
	}
	end, err := parseMondayWeek(normalised)
	if err != nil {
		t.Fatalf("normalised end week is not a Monday: %v", err)
	}
	startWeek := end.AddDate(0, 0, 7).Format(dateLayout)

	if _, err := buildMacroInputs(ctx, db, 1, startWeek, MacroModeExtension); err != nil {
		t.Fatalf("extension-derived start %s was rejected: %v", startWeek, err)
	}
	// Without the normalisation the derived start is the Sunday plus 7 days,
	// which buildMacroInputs rejects — that is the failure this guards.
	raw, err := parseWeekDate(prevEndWeek)
	if err != nil {
		t.Fatalf("parse raw end week: %v", err)
	}
	if _, err := buildMacroInputs(ctx, db, 1, raw.AddDate(0, 0, 7).Format(dateLayout), MacroModeExtension); err == nil {
		t.Error("a non-Monday derived start must be rejected, so callers cannot skip NormaliseMacroStartWeek")
	}
}

// The VO2max section keeps the last estimate of each of the six most recent
// months, oldest first. Every part of that depends on GetVO2maxHistory's
// ascending order, which nothing else in this package pins.
func TestMacroVO2maxTrendMonthlyReduction(t *testing.T) {
	db := setupTestDB(t)

	// Eight months, two estimates each: the second of each month is the one
	// that must be shown.
	months := []string{"2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08"}
	for i, m := range months {
		for j, day := range []string{"05", "20"} {
			wID := seedWorkout(t, db, 1, m+"-"+day, 150, fmt.Sprintf("vo2-%d-%d", i, j))
			if err := training.SaveVO2maxEstimate(db, &training.VO2maxEstimate{
				UserID:      1,
				WorkoutID:   wID,
				VO2max:      50 + float64(i) + float64(j)/2,
				Method:      "hr_ratio",
				EstimatedAt: m + "-" + day + "T10:00:00Z",
			}); err != nil {
				t.Fatalf("save vo2max: %v", err)
			}
		}
	}

	section := renderMacroVO2maxTrend(db, 1)
	for _, dropped := range []string{"2026-01", "2026-02"} {
		if strings.Contains(section, dropped) {
			t.Errorf("VO2max trend kept month %s — only the six newest belong\n%s", dropped, section)
		}
	}
	var lines []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "- 20") {
			lines = append(lines, line)
		}
	}
	want := []string{
		"- 2026-03: 52.5", "- 2026-04: 53.5", "- 2026-05: 54.5",
		"- 2026-06: 55.5", "- 2026-07: 56.5", "- 2026-08: 57.5",
	}
	if len(lines) != len(want) {
		t.Fatalf("VO2max trend has %d months, want %d\n%s", len(lines), len(want), section)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("VO2max line %d = %q, want %q (oldest first, month's last value)", i, lines[i], w)
		}
	}
}

// seedLactateTest inserts a test with the given stages (speed, lactate, HR).
func seedLactateTest(t *testing.T, db *sql.DB, userID int64, date string, stages [][3]float64) {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO lactate_tests (user_id, date, comment, protocol_type, created_at, updated_at)
		VALUES (?, ?, '', 'standard', ?, ?)`,
		userID, date, date, date,
	)
	if err != nil {
		t.Fatalf("insert lactate test %s: %v", date, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("lactate id: %v", err)
	}
	for i, s := range stages {
		if _, err := db.Exec(`
			INSERT INTO lactate_test_stages (test_id, stage_number, speed_kmh, lactate_mmol, heart_rate_bpm, notes)
			VALUES (?, ?, ?, ?, ?, '')`,
			id, i+1, s[0], s[1], int(s[2]),
		); err != nil {
			t.Fatalf("insert stage: %v", err)
		}
	}
}

// The lactate section is the block's threshold anchor: it must show the three
// NEWEST tests (lactate.List is date DESC), convert km/h to min/km, and survive
// a test whose threshold could not be derived.
func TestMacroLactateTestsRendersNewestThree(t *testing.T) {
	db := setupTestDB(t)

	// Newest first once ordered: 2026-08-01 .. 2026-05-01.
	seedLactateTest(t, db, 1, "2026-08-01", [][3]float64{{12, 2.0, 150}, {13, 4.0, 165}})
	seedLactateTest(t, db, 1, "2026-07-01", [][3]float64{{12, 2.0, 148}, {14, 4.0, 168}})
	// No stage reaches 4 mmol and there are too few stages for the curve
	// methods, so no threshold can be derived.
	seedLactateTest(t, db, 1, "2026-06-01", [][3]float64{{10, 1.0, 130}, {11, 1.5, 138}})
	seedLactateTest(t, db, 1, "2026-05-01", [][3]float64{{12, 2.0, 145}, {13, 4.0, 160}})

	section := renderMacroLactateTests(db, 1)
	if strings.Contains(section, "2026-05-01") {
		t.Errorf("the fourth-newest test must be dropped\n%s", section)
	}
	// 13.0 km/h -> 3600/13 = 276.9 s/km -> 4:37/km.
	if !strings.Contains(section, "- 2026-08-01: threshold 13.0 km/h (4:37/km), HR 165 bpm") {
		t.Errorf("newest test line is wrong\n%s", section)
	}
	// 14.0 km/h -> 257.1 s/km -> 4:17/km.
	if !strings.Contains(section, "- 2026-07-01: threshold 14.0 km/h (4:17/km), HR 168 bpm") {
		t.Errorf("second test line is wrong\n%s", section)
	}
	if !strings.Contains(section, "- 2026-06-01: no valid threshold derived") {
		t.Errorf("a test without a derivable threshold must say so, not crash or be skipped\n%s", section)
	}
	if strings.Index(section, "2026-08-01") > strings.Index(section, "2026-07-01") {
		t.Error("lactate tests are not newest-first")
	}
}

// The prediction snapshot is what a target HM time is judged against when the
// block has no A-race. Only its empty path was covered.
func TestMacroRacePredictionRendersSnapshot(t *testing.T) {
	db := setupTestDB(t)

	preds := `[{"distance":"Half Marathon","distance_m":21097.5,"time_seconds":5220,"predicted_time":"1:27:00","pace_per_km":"4:07","confidence":"medium"},
	           {"distance":"10K","distance_m":10000,"time_seconds":2400,"predicted_time":"40:00","pace_per_km":"4:00"}]`
	rationale := "Threshold pace from the 2026-08-01 lactate test."
	// predictions_json and rationale are encrypted at rest. Seeding plaintext
	// would still render, via the legacy-plaintext fallback in decryptOrRaw, and
	// the section would never be exercised against a real row.
	encPreds, err := encryption.EncryptField(preds)
	if err != nil {
		t.Fatalf("encrypt predictions: %v", err)
	}
	encRationale, err := encryption.EncryptField(rationale)
	if err != nil {
		t.Fatalf("encrypt rationale: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO race_predictions (user_id, created_at, method, predictions_json, rationale)
		VALUES (?, ?, 'ai', ?, ?)`,
		1, "2026-08-20T06:15:00Z", encPreds, encRationale,
	); err != nil {
		t.Fatalf("insert prediction: %v", err)
	}
	// Guard the guard: if the columns ever stopped being ciphertext, the
	// assertions below would pass without any decryption happening.
	var stored string
	if err := db.QueryRow("SELECT predictions_json FROM race_predictions WHERE user_id = 1").Scan(&stored); err != nil {
		t.Fatalf("read back prediction: %v", err)
	}
	if strings.Contains(stored, "Half Marathon") {
		t.Fatal("predictions_json was stored as plaintext — the test would not catch a decrypt regression")
	}

	section := renderMacroRacePrediction(db, 1)
	if !strings.Contains(section, "Race predictions as of 2026-08-20:") {
		t.Errorf("prediction date is not truncated to YYYY-MM-DD\n%s", section)
	}
	if !strings.Contains(section, "- Half Marathon: 1:27:00 (4:07/km, confidence medium)") {
		t.Errorf("half-marathon prediction line is wrong\n%s", section)
	}
	if !strings.Contains(section, "- 10K: 40:00 (4:00/km)") {
		t.Errorf("a prediction without a confidence must still render\n%s", section)
	}
	if !strings.Contains(section, "Prediction rationale: Threshold pace from the 2026-08-01 lactate test.") {
		t.Errorf("prediction rationale is missing\n%s", section)
	}
}

// An athlete who lets a block lapse and regenerates weeks later has no plan
// covering the week before the new start, so the fallback query is what keeps
// the extension from restarting from base. It must pick the most recent ACTIVE
// block belonging to THIS user.
func TestLoadPreviousMacroPlanFallsBackToMostRecentActiveBlock(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	newer, newerWeeks := sampleMacroPlan(1)
	newer.Goal.Statement = "NEWER-BLOCK-MARKER"
	if err := CreateMacroPlan(ctx, db, newer, newerWeeks, "initial goal"); err != nil {
		t.Fatalf("create newer block: %v", err)
	}

	older, olderWeeks := sampleMacroPlan(1)
	older.StartWeek = mondayAfter(testBlockStart, -MacroBlockWeeks)
	older.EndWeek = mondayAfter(testBlockStart, -1)
	older.Goal.Statement = "OLDER-BLOCK-MARKER"
	for i := range olderWeeks {
		olderWeeks[i].WeekStart = mondayAfter(older.StartWeek, i)
	}
	if err := CreateMacroPlan(ctx, db, older, olderWeeks, "initial goal"); err != nil {
		t.Fatalf("create older block: %v", err)
	}

	// A superseded block that ends later than either of the two above: status
	// must exclude it even though start_week DESC would pick it first.
	superseded, supersededWeeks := sampleMacroPlan(1)
	superseded.StartWeek = mondayAfter(testBlockStart, MacroBlockWeeks)
	superseded.EndWeek = mondayAfter(testBlockStart, 2*MacroBlockWeeks-1)
	superseded.Status = MacroPlanStatusSuperseded
	superseded.Goal.Statement = "SUPERSEDED-BLOCK-MARKER"
	for i := range supersededWeeks {
		supersededWeeks[i].WeekStart = mondayAfter(superseded.StartWeek, i)
	}
	if err := CreateMacroPlan(ctx, db, superseded, supersededWeeks, "initial goal"); err != nil {
		t.Fatalf("create superseded block: %v", err)
	}

	// Another athlete's active block, newer than anything above.
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	foreign, foreignWeeks := sampleMacroPlan(2)
	foreign.StartWeek = mondayAfter(testBlockStart, MacroBlockWeeks)
	foreign.EndWeek = mondayAfter(testBlockStart, 2*MacroBlockWeeks-1)
	foreign.Goal.Statement = "FOREIGN-BLOCK-MARKER"
	for i := range foreignWeeks {
		foreignWeeks[i].UserID = 2
		foreignWeeks[i].WeekStart = mondayAfter(foreign.StartWeek, i)
	}
	if err := CreateMacroPlan(ctx, db, foreign, foreignWeeks, "initial goal"); err != nil {
		t.Fatalf("create foreign block: %v", err)
	}

	// Start six weeks after the newer block ended: nothing covers the week
	// before it, so only the fallback query can find anything.
	lapsedStart := mondayAfter(testBlockStart, MacroBlockWeeks+6)
	prev, err := loadPreviousMacroPlan(ctx, db, 1, lapsedStart)
	if err != nil {
		t.Fatalf("loadPreviousMacroPlan: %v", err)
	}
	if prev == nil {
		t.Fatal("fallback did not find the lapsed block — an extension would restart from base")
	}
	if prev.StartWeek != testBlockStart {
		t.Errorf("fallback picked the block starting %s, want the most recent active one (%s)", prev.StartWeek, testBlockStart)
	}

	startTime, err := parseMondayWeek(lapsedStart)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	section := renderMacroPreviousBlock(ctx, db, 1, startTime)
	if !strings.Contains(section, "NEWER-BLOCK-MARKER") {
		t.Errorf("previous-block section does not name the most recent active block\n%s", section)
	}
	for _, unwanted := range []string{"OLDER-BLOCK-MARKER", "SUPERSEDED-BLOCK-MARKER", "FOREIGN-BLOCK-MARKER"} {
		if strings.Contains(section, unwanted) {
			t.Errorf("previous-block section embedded %s", unwanted)
		}
	}
}

// Library names and macro goal/periodisation text are encrypted at rest and
// decrypted by their loaders. If a loader ever stops decrypting, the prompt
// would embed ciphertext and the coach would plan against garbage — silently,
// since nothing else validates the text.
func TestMacroLibraryAndPreviousBlockAreDecrypted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if err := SeedReferenceWorkout(ctx, db, 1); err != nil {
		t.Fatalf("seed reference workout: %v", err)
	}
	plan, weeks := sampleMacroPlan(1)
	if err := CreateMacroPlan(ctx, db, plan, weeks, "initial goal"); err != nil {
		t.Fatalf("create macro plan: %v", err)
	}

	// The columns really are ciphertext — otherwise the assertions below would
	// pass even with no decryption anywhere.
	var storedName, storedGoal string
	if err := db.QueryRow("SELECT name FROM stride_workouts WHERE user_id = 1").Scan(&storedName); err != nil {
		t.Fatalf("read stored name: %v", err)
	}
	if err := db.QueryRow("SELECT goal_json FROM stride_macro_plans WHERE user_id = 1").Scan(&storedGoal); err != nil {
		t.Fatalf("read stored goal: %v", err)
	}
	if strings.Contains(storedName, "threshold") {
		t.Fatal("library name is stored in plaintext — fix the store, not this test")
	}
	if strings.Contains(storedGoal, "half marathon") {
		t.Fatal("macro goal is stored in plaintext — fix the store, not this test")
	}

	library := renderMacroWorkoutLibrary(ctx, db, 1)
	if !strings.Contains(library, "6x6min threshold (reference)") {
		t.Errorf("library section does not carry the decrypted workout name\n%s", library)
	}
	start, err := parseMondayWeek(mondayAfter(testBlockStart, MacroBlockWeeks))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	previous := renderMacroPreviousBlock(ctx, db, 1, start)
	if !strings.Contains(previous, "Run 1:24:00 for the half marathon") {
		t.Errorf("previous-block section does not carry the decrypted goal\n%s", previous)
	}
	if !strings.Contains(previous, "threshold density") {
		t.Errorf("previous-block section does not carry the decrypted periodisation\n%s", previous)
	}
}
