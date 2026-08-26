package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/lactate"
	"github.com/Robin831/Hytte/internal/training"
)

// MacroBlockWeeks is the fixed horizon of a macro training block: 26 weeks
// (roughly six months) of contiguous Mondays starting at the block's start
// week. The prompt, the validator and the store all assume this length.
const MacroBlockWeeks = 26

// macroRaceLookaheadWeeks is how far past the block's end week the race
// calendar is still shown to the coach. Races just beyond the horizon change
// how the last weeks should be shaped (a race two weeks after end_week means
// the block should not finish flat), so they are included as context even
// though the block cannot plan for them.
const macroRaceLookaheadWeeks = 4

// macroHistoryWeeks is how many completed weeks of training history the input
// table carries. Matching the block length keeps the coach's view of the past
// the same size as the future it is asked to plan.
const macroHistoryWeeks = MacroBlockWeeks

// MacroMode selects which macro block is being generated. It changes the
// inputs, not the instructions: extension mode additionally embeds the block
// being continued so the new block starts where the old one ended instead of
// restarting from base.
//
// The values are exactly the MacroGeneratedBy* vocabulary that macro plans are
// persisted with, so a mode is written to generated_by as string(mode) with no
// mapping table in between — there is one vocabulary for "how this block came
// to exist", not two.
//
// The type and the buildMacroInputs signature are consumed by
// GenerateMacroPlan — keep both stable.
type MacroMode string

const (
	// MacroModeScheduled is the Monday cron's block: the athlete's first block,
	// or a block generated when no previous one is available to continue.
	MacroModeScheduled MacroMode = MacroGeneratedByScheduled
	// MacroModeExtension appends a fresh block to the one that is running out.
	MacroModeExtension MacroMode = MacroGeneratedByExtension
	// MacroModeManual is a user-triggered regeneration of the current horizon.
	MacroModeManual MacroMode = MacroGeneratedByManual
)

// describe expands a mode into the sentence the prompt shows the coach. The
// stored vocabulary ("scheduled") is terse for a column and opaque in a prompt,
// so the prompt carries both rather than inventing a second set of names.
func (m MacroMode) describe() string {
	switch m {
	case MacroModeScheduled:
		return "the athlete's first block, or a fresh block with no previous one to continue"
	case MacroModeExtension:
		return "a continuation of the block that is running out — its goal and last weeks are given below"
	case MacroModeManual:
		return "a user-requested regeneration of the current horizon"
	default:
		return ""
	}
}

// valid reports whether m is one of the three modes. buildMacroInputs rejects
// anything else rather than emitting an undefined "Mode:" line the prompt
// vocabulary does not cover.
func (m MacroMode) valid() bool {
	switch m {
	case MacroModeScheduled, MacroModeExtension, MacroModeManual:
		return true
	default:
		return false
	}
}

// macroPeriodisation is the periodisation doctrine the macro coach plans by.
// bakkenPhilosophy covers what a single week looks like; this covers how 26 of
// them are shaped into a block.
const macroPeriodisation = `## Periodisation
You are planning a ` + macroBlockWeeksStr + `-week training block, not a single week. Shape it as follows.

### Mesocycle rhythm
- The block is built from mesocycles of 4 weeks: 3 progressive build weeks followed by 1 deload week.
- A deload week is 60-70% of the preceding peak week's volume. Intensity is kept, volume is cut — drop a quality session rather than watering every session down.
- Name each mesocycle for what it develops, and give it exactly one job.

### Block arc
- The block runs base -> build -> peak -> taper. Base establishes aerobic volume and threshold frequency, build raises threshold volume and specificity, peak sharpens at race specificity with volume trimmed, taper sheds volume while keeping intensity.
- A block with no A-priority race still runs base -> build -> peak; it ends with a benchmark session instead of a taper and a race.

### Load rules
- Weekly volume increases by no more than +10% over the previous week. The only exception is the week directly after a deload, which may return to the pre-deload level.
- At most ONE hard (above-threshold) session per week, and in most weeks none — aim for one hard session every 1-2 weeks. Never two hard sessions in the same week.
- Threshold work is the steady backbone across every phase; it is the volume of threshold work that changes between phases, not whether it is present.
- Never plan a week above the athlete's weekly distance cap, and never plan more sessions than the athlete's available training days.

### Races
- An A-priority race gets a 2-week taper: the two weeks directly before the race week are taper weeks, and the race week itself is a race week.
- B and C races are run as quality sessions inside normal training. They get no taper week and never restructure the block around them; at most sharpen for 1-2 weeks around them.
- The week after any race is a recovery week.
`

// macroHalfMarathonRule is the standing priority rule. The sentence below is
// reproduced verbatim from the epic and must appear unchanged in every prompt
// that plans — do not reword it.
const macroHalfMarathonRule = `## Half-Marathon Priority
improving half-marathon performance is always the main priority. No races -> a half-marathon development block with a concrete target HM time and a benchmark session. 5 km / 10 km on the calendar -> B/C races embedded in HM training: sharpen at most 1-2 weeks, no full taper, never restructure the block around them. Only a half marathon (or longer) A-priority race may define the peak and taper.
`

// macroOutputContract describes the JSON the macro coach must return. It is the
// single source of truth for the response shape: the Go response types and
// their validator mirror this contract, so change both together.
const macroOutputContract = `## Output Format
Return ONLY a single JSON object. No markdown, no explanation, no code fences.

{
  "goal": {
    "primary_focus": string — short label for what the block develops, e.g. "half-marathon development"
    "statement": string — one sentence stating what the athlete is training for
    "target_hm_time_s": integer — target half-marathon time in SECONDS
    "benchmark": string — the race or session that tests the goal at the end of the block
    "anchor_race_id": integer or null — id of the A-priority race the block is built around; null when the block has no A-race
    "rationale": string — 2-3 sentences justifying the target from the data given
  },
  "mesocycles": [
    {
      "name": string — short name, referenced by every week it covers
      "phase": string — one of "base", "build", "peak", "taper", "race", "recovery"
      "start_week": string — "YYYY-MM-DD", the Monday the mesocycle starts
      "weeks": integer — how many weeks it spans
      "focus": string — one sentence on what this mesocycle develops
    }
  ],
  "weeks": [
    {
      "week_start": string — "YYYY-MM-DD", a Monday
      "seq": integer — 1-based position in the block, 1..` + macroBlockWeeksStr + `
      "phase": string — one of "base", "build", "peak", "taper", "race", "recovery"
      "mesocycle": string — the "name" of the mesocycle this week belongs to
      "load_level": string — one of "deload", "normal", "build", "peak", "taper"
      "target_km": number — planned running distance for the week, in km
      "target_sessions": integer — planned number of sessions in the week
      "key_sessions": [
        {
          "type": string — e.g. "threshold", "long_run", "hard", "easy", "race"
          "focus": string — one sentence on what the session is for
          "library_id": integer or null — the id of a Workout Library entry from the inputs below when this session is that workout; null when it is not from the library
        }
      ],
      "intent": string — 1-2 sentences on what the week is for and how it follows from the previous one
      "race_id": integer or null — id of the race this week contains, null otherwise
    }
  ]
}

Contract rules:
- "weeks" MUST contain exactly ` + macroBlockWeeksStr + ` objects, one per week, in ascending order.
- The first "week_start" MUST equal the requested block start week, and every following week_start MUST be exactly 7 days after the previous one.
- Every week's "mesocycle" MUST match the "name" of one of the returned mesocycles, and the mesocycles MUST cover the whole block with no gaps or overlaps.
- "library_id" MUST be an id listed in the Workout Library section of the inputs, or null. Never invent an id.
- "race_id" and "anchor_race_id" MUST be ids listed in the Upcoming Races section, or null. Never invent an id.
`

// macroBlockWeeksStr is MacroBlockWeeks rendered for the prompt constants,
// which are compile-time strings and so cannot call strconv. The two are kept
// in step by TestMacroBlockWeeksStrMatchesConstant — change one, change both.
const macroBlockWeeksStr = "26"

// macroInstructions is the static instruction block every macro prompt opens
// with: the coaching philosophy, the periodisation doctrine, the standing
// half-marathon priority rule, and the JSON output contract. Callers pair it
// with buildMacroInputs to form the full prompt, the same way the weekly path
// pairs weeklyInstructions with its inputs.
const macroInstructions = bakkenPhilosophy + "\n\n" +
	macroPeriodisation + "\n" +
	macroHalfMarathonRule + "\n" +
	macroOutputContract

// buildMacroInputs assembles the athlete-specific half of the macro prompt for
// a block of MacroBlockWeeks weeks starting at startWeek (a Monday, formatted
// YYYY-MM-DD).
//
// Verbatim blocks (profile, race calendar, race results, constraints, the
// custom prompt and — in extension mode — the previous block) are passed
// through as-is. Everything else is summarised in Go so the whole input stays
// around 6-7k tokens: 26 weeks of training history collapse to one table row
// each, VO2max to 6 monthly points, lactate to the last 3 tests.
//
// Deliberately omitted: treadmill calibration and raw workouts (a macro block
// prescribes weeks, not session texts), the legacy goal_race_* preferences
// (superseded by stride_races and the block's own goal), and stride notes
// (they belong to the weekly generator, which consumes them).
//
// Hard failures — an unparseable or non-Monday start week, a mode outside the
// MacroMode vocabulary, preferences, the race calendar and training history —
// return an error. Optional context that a new athlete simply has none of (race
// predictions, ACR, VO2max, lactate, the workout library, the previous block)
// degrades to a short "none recorded" line so a first block can still be built.
//
// The non-Monday rejection is a backstop, not the place to handle a computed
// date: there is no production caller yet (GenerateMacroPlan lands separately),
// and every entry point that will compute a start — the Monday cron, the
// extension path deriving a start from a previous block's EndWeek, the
// manual/API path — must pass it through NormaliseMacroStartWeek first, which
// snaps any date to its containing Monday.
func buildMacroInputs(ctx context.Context, db *sql.DB, userID int64, startWeek string, mode MacroMode) (string, error) {
	if !mode.valid() {
		return "", fmt.Errorf("invalid macro mode %q", mode)
	}
	// A block start that is not a Monday would silently mismatch every key in
	// the history table (summaries and plans are keyed by Monday), so it is a
	// hard error here rather than a table of dashes in the prompt.
	start, err := parseMondayWeek(startWeek)
	if err != nil {
		return "", fmt.Errorf("parse start week: %w", err)
	}
	endWeek := start.AddDate(0, 0, 7*(MacroBlockWeeks-1)).Format(dateLayout)
	raceHorizon := start.AddDate(0, 0, 7*(MacroBlockWeeks-1+macroRaceLookaheadWeeks)).Format(dateLayout)

	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return "", fmt.Errorf("load preferences: %w", err)
	}

	var sb strings.Builder

	// The ask, so the coach knows the exact horizon it is filling.
	sb.WriteString("## Planning Request\n")
	fmt.Fprintf(&sb, "Build a %d-week macro training block for this athlete.\n", MacroBlockWeeks)
	fmt.Fprintf(&sb, "- Block start week (Monday): %s\n", startWeek)
	fmt.Fprintf(&sb, "- Block end week (Monday of the final week): %s\n", endWeek)
	fmt.Fprintf(&sb, "- Mode: %s (%s)\n\n", mode, mode.describe())

	// Athlete profile: HR zones, threshold HR/pace, easy pace range. The
	// goal-race section is stripped — Stride's goal comes from the block itself
	// and from stride_races, never from the legacy goal_race_* preferences.
	sb.WriteString("## Athlete Profile\n")
	if profile := stripGoalRaceSection(training.BuildUserProfileBlock(db, userID)); profile != "" {
		sb.WriteString(profile)
	} else {
		sb.WriteString("No profile recorded.\n")
	}
	sb.WriteString("\n")

	// Current fitness: the stored race-prediction snapshot. This is what a
	// target half-marathon time has to be judged against when the block has no
	// A-race to test it.
	sb.WriteString(renderMacroRacePrediction(db, userID))

	// Race calendar through the horizon plus a lookahead.
	races, err := ListRaces(db, userID)
	if err != nil {
		return "", fmt.Errorf("list races: %w", err)
	}
	sb.WriteString(renderMacroUpcomingRaces(races, startWeek, raceHorizon))

	// Completed races — the honest record of what the athlete has actually run.
	results, err := listRaceResults(ctx, db, userID)
	if err != nil {
		return "", fmt.Errorf("list race results: %w", err)
	}
	sb.WriteString(renderMacroRaceResults(results))

	// Hard constraints the plan must respect.
	sb.WriteString(renderMacroConstraints(prefs))

	// 26 weeks of history: volume merged with plan adherence and intensity mix.
	history, err := buildMacroHistoryTable(db, userID, start)
	if err != nil {
		return "", err
	}
	sb.WriteString(history)

	// Load status right now, so the first weeks start from where the athlete is.
	sb.WriteString(renderMacroTrainingLoad(db, userID))

	// Longer-range fitness direction.
	sb.WriteString(renderMacroVO2maxTrend(db, userID))
	sb.WriteString(renderMacroLactateTests(db, userID))

	// The library, names and types only — enough for key_sessions.library_id to
	// reference an entry without spending tokens on full session bodies.
	sb.WriteString(renderMacroWorkoutLibrary(ctx, db, userID))

	// Extension mode: the block being continued, verbatim.
	if mode == MacroModeExtension {
		sb.WriteString(renderMacroPreviousBlock(ctx, db, userID, start))
	}

	// The athlete's own standing instructions, last so they override.
	if custom := decryptCustomPrompt(prefs["stride_custom_prompt"]); custom != "" {
		sb.WriteString("## Additional Instructions\n")
		sb.WriteString(custom)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

// dateLayout is the YYYY-MM-DD layout every week_start in Stride uses.
const dateLayout = "2006-01-02"

// parseWeekDate parses a YYYY-MM-DD week start in UTC.
func parseWeekDate(date string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, date, time.UTC)
}

// parseMondayWeek parses a YYYY-MM-DD week start and rejects any day that is
// not a Monday. Every week key in Stride — stride_plans.week_start,
// training.WeeklySummaries, the macro block's own weeks — is a Monday, so a
// start on any other weekday matches nothing and degrades silently.
func parseMondayWeek(date string) (time.Time, error) {
	t, err := parseWeekDate(date)
	if err != nil {
		return time.Time{}, err
	}
	if t.Weekday() != time.Monday {
		return time.Time{}, fmt.Errorf("week %q is a %s, not a Monday", date, t.Weekday())
	}
	return t, nil
}

// NormaliseMacroStartWeek snaps any YYYY-MM-DD date to the Monday of the week
// containing it. It is the single entry point every caller that *computes* a
// block start must route through, because buildMacroInputs rejects a non-Monday
// start outright rather than emitting a table of dashes:
//
//   - the Monday cron already runs on a Monday, but takes its date from the
//     scheduler's clock and time zone;
//   - the extension path derives the next start as a previous block's EndWeek
//     plus 7 days, and nothing in macro_store validates that EndWeek is a
//     Monday — a Sunday EndWeek yields a Sunday start;
//   - a manual/API start week is whatever date the UI sent.
//
// Snapping is the right call for all three: a date inside week N means week N,
// never "fail the generation". The error is reserved for a date that is not a
// date at all.
func NormaliseMacroStartWeek(date string) (string, error) {
	t, err := parseWeekDate(date)
	if err != nil {
		return "", fmt.Errorf("parse week %q: %w", date, err)
	}
	// Weekday(): Sunday=0..Saturday=6, so Monday is (weekday+6)%7 days back —
	// 0 for Monday itself, 6 for Sunday.
	daysBack := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -daysBack).Format(dateLayout), nil
}

// stripGoalRaceSection removes the "Goal Race:" block that
// BuildUserProfileBlock appends from the legacy goal_race_* preferences. Stride
// macro planning derives its goal from stride_races and the block's own goal
// object, so those preferences must not leak into the prompt and contradict it.
//
// Only the goal-race section itself is dropped, not everything after it: the
// section is the "Goal Race:" header line plus the bullets the goal race
// itself emits (goalRaceBullets). The first line that is not one of those ends
// the section, so a profile that emitted the goal race before the rest of its
// bullets — "- Threshold Pace: 4:28/km", "- Training Zones (custom):" and the
// indented zone lines under it — keeps every one of them.
// BuildUserProfileBlock happens to emit the goal race last today, but nothing
// in this package enforces that.
func stripGoalRaceSection(block string) string {
	// Match on the line prefix rather than the whole "Goal Race:\n" header so a
	// block that renders the goal inline on the same line is stripped too.
	lines := strings.Split(block, "\n")
	kept := make([]string, 0, len(lines))
	inGoalRace := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Goal Race:"):
			inGoalRace = true
		case inGoalRace && isGoalRaceBullet(line):
			// Still inside the goal-race section — drop this line too.
		default:
			inGoalRace = false
			kept = append(kept, line)
		}
	}
	block = strings.TrimRight(strings.Join(kept, "\n"), "\n")
	// An athlete with only goal_race_* preferences and no HR/zone data leaves
	// nothing but the header behind — treat that as no profile at all.
	if block == "" || block == "User Profile:" {
		return ""
	}
	return block + "\n"
}

// goalRaceBullets is the complete bullet vocabulary
// training.buildUserProfileFromPrefs emits under "Goal Race:". Matching this
// exact set — rather than "any bullet" — is what stops the strip from
// swallowing the profile's other top-level bullets, which use the same "- key:
// value" shape. TestStripGoalRaceSectionMatchesProducerVocabulary pins the set
// against the producer, so a new goal-race field fails a test rather than
// leaking into the prompt.
var goalRaceBullets = []string{
	"- Event:",
	"- Date:",
	"- Distance:",
	"- Target Time:",
}

// isGoalRaceBullet reports whether a line is one of the goal race's own
// bullets. Anything else — another top-level bullet, a blank line, a new
// section header — ends the goal-race section.
func isGoalRaceBullet(line string) bool {
	for _, prefix := range goalRaceBullets {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// renderMacroRacePrediction renders the latest stored race-prediction snapshot.
// Non-fatal: an athlete with no snapshot yet gets a "none recorded" line.
func renderMacroRacePrediction(db *sql.DB, userID int64) string {
	pred, err := training.GetLatestRacePrediction(db, userID)
	if err != nil {
		log.Printf("stride: load race prediction for user %d: %v", userID, err)
		pred = nil
	}
	var sb strings.Builder
	sb.WriteString("## Current Fitness Estimate\n")
	if pred == nil || len(pred.Predictions) == 0 {
		sb.WriteString("No race prediction recorded.\n\n")
		return sb.String()
	}
	asOf := pred.CreatedAt
	if len(asOf) >= 10 {
		asOf = asOf[:10]
	}
	fmt.Fprintf(&sb, "Race predictions as of %s:\n", asOf)
	for _, p := range pred.Predictions {
		fmt.Fprintf(&sb, "- %s: %s (%s/km", p.Distance, p.PredictedTime, p.PacePerKm)
		if p.Confidence != "" {
			fmt.Fprintf(&sb, ", confidence %s", p.Confidence)
		}
		sb.WriteString(")\n")
	}
	if pred.Rationale != "" {
		fmt.Fprintf(&sb, "Prediction rationale: %s\n", pred.Rationale)
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroUpcomingRaces lists unfinished races between the block start and
// the lookahead horizon, with the id the response must reference, the priority
// that decides whether the block tapers for it, and the target time.
func renderMacroUpcomingRaces(races []Race, from, through string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Upcoming Races (through %s)\n", through)

	// Races between today and the block start still matter: they constrain how
	// the first weeks may be loaded.
	today := time.Now().UTC().Format(dateLayout)
	if today < from {
		from = today
	}

	count := 0
	for _, r := range races {
		if r.ResultTime != nil || r.Date < from || r.Date > through {
			continue
		}
		count++
		fmt.Fprintf(&sb, "- id=%d %q on %s — %.1f km, priority %s", r.ID, r.Name, r.Date, r.DistanceM/1000, r.Priority)
		if r.TargetTime != nil {
			fmt.Fprintf(&sb, ", target %s", formatRaceTime(*r.TargetTime))
			if r.DistanceM > 0 {
				fmt.Fprintf(&sb, " (%s/km)", formatPaceSecPerKm(float64(*r.TargetTime)/(r.DistanceM/1000)))
			}
		} else {
			sb.WriteString(", no target time set")
		}
		sb.WriteString("\n")
	}
	if count == 0 {
		sb.WriteString("No races on the calendar within the horizon. Build a half-marathon development block.\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroRaceResults lists completed races, most recent first.
func renderMacroRaceResults(results []RaceResult) string {
	var sb strings.Builder
	sb.WriteString("## Race Results\n")
	if len(results) == 0 {
		sb.WriteString("No completed races recorded.\n\n")
		return sb.String()
	}
	for _, r := range results {
		fmt.Fprintf(&sb, "- %q on %s — %.1f km in %s", r.Name, r.Date, r.DistanceM/1000, formatRaceTime(r.TimeSecs))
		if r.DistanceM > 0 {
			fmt.Fprintf(&sb, " (%s/km)", formatPaceSecPerKm(float64(r.TimeSecs)/(r.DistanceM/1000)))
		}
		fmt.Fprintf(&sb, ", priority %s\n", r.Priority)
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroConstraints renders the hard limits every week of the block has to
// respect. Both are plain preference values, never encrypted.
func renderMacroConstraints(prefs map[string]string) string {
	var sb strings.Builder
	sb.WriteString("## Athlete Constraints\n")
	if days := prefs["stride_available_days"]; days != "" {
		fmt.Fprintf(&sb, "- Training days per week: %s\n", days)
	} else {
		sb.WriteString("- Training days per week: 5 (default)\n")
	}
	if distanceCap := prefs["stride_weekly_distance_cap"]; distanceCap != "" {
		fmt.Fprintf(&sb, "- Weekly distance cap: %s km — no week may exceed this.\n", distanceCap)
	} else {
		sb.WriteString("- Weekly distance cap: none set.\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// macroHistoryRow is one week of the training-history table: measured volume
// from the workout log merged with plan adherence and intensity split from the
// stride plan that covered the same week.
type macroHistoryRow struct {
	weekStart    string
	km           float64
	seconds      int
	workouts     int
	avgHR        float64
	hrWeight     int
	hasVolume    bool
	planID       int64
	planned      int
	completed    int
	hasPlan      bool
	hasEvals     bool
	easyMin      int
	thresholdMin int
	hardMin      int
}

// buildMacroHistoryTable renders the last macroHistoryWeeks completed weeks
// before the block start as one compact row per week — the single biggest input
// block, so it stays one line per week no matter how much data exists.
//
// Weeks with no data are still emitted (as dashes) so the coach can see the
// gaps rather than reading a shorter table as unbroken training.
func buildMacroHistoryTable(db *sql.DB, userID int64, start time.Time) (string, error) {
	if start.Weekday() != time.Monday {
		return "", fmt.Errorf("history window start %s is a %s, not a Monday", start.Format(dateLayout), start.Weekday())
	}
	summaries, err := training.WeeklySummaries(db, userID)
	if err != nil {
		return "", fmt.Errorf("load weekly summaries: %w", err)
	}

	// order runs oldest week first (i counts down from macroHistoryWeeks to 1),
	// which is the order the table renders in.
	rows := make(map[string]*macroHistoryRow, macroHistoryWeeks)
	order := make([]string, 0, macroHistoryWeeks)
	for i := macroHistoryWeeks; i >= 1; i-- {
		week := start.AddDate(0, 0, -7*i).Format(dateLayout)
		order = append(order, week)
		rows[week] = &macroHistoryRow{weekStart: week}
	}
	// The plan loader has to page back to the OLDEST week of the window, so the
	// bound is computed from start rather than read off order — an oldest-first
	// order makes that order[0], but that is a property of the loop above, not
	// something the loader should depend on.
	windowStart := start.AddDate(0, 0, -7*macroHistoryWeeks).Format(dateLayout)

	weeks, err := loadMacroPlanWeeks(db, userID, windowStart)
	if err != nil {
		return "", err
	}

	for _, s := range summaries {
		// WeeklySummaries groups by ISO-ish year-week but derives week_start as
		// the Monday, so a week straddling New Year yields two rows sharing one
		// week_start. Accumulate rather than assign, or the second row would
		// silently drop the first one's volume.
		if r, ok := rows[s.WeekStart]; ok {
			r.km += s.TotalDistance / 1000
			r.seconds += s.TotalDuration
			r.workouts += s.WorkoutCount
			if s.AvgHeartRate > 0 && s.WorkoutCount > 0 {
				r.avgHR = (r.avgHR*float64(r.hrWeight) + s.AvgHeartRate*float64(s.WorkoutCount)) / float64(r.hrWeight+s.WorkoutCount)
				r.hrWeight += s.WorkoutCount
			}
			r.hasVolume = true
		}
	}
	for _, w := range weeks {
		// One plan per week, guaranteed by stride_plans' UNIQUE(user_id,
		// week_start): a regenerated week upserts the existing row rather than
		// adding a second one (generate.go's ON CONFLICT DO UPDATE), so this
		// assigns rather than merging or deduplicating.
		// TestStridePlansAreUniquePerUserWeek pins that invariant.
		if r, ok := rows[w.WeekStart]; ok {
			r.planID = w.PlanID
			r.planned = w.SessionsPlanned
			r.completed = w.SessionsCompleted
			r.hasPlan = true
			r.easyMin = w.EasySeconds / 60
			r.thresholdMin = w.ThresholdSeconds / 60
			r.hardMin = w.HardSeconds / 60
		}
	}

	// Which of those plans were evaluated at all. SessionsCompleted counts only
	// evaluations that matched a prescription, so it cannot distinguish "never
	// evaluated" from "evaluated, matched nothing" on its own.
	planIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.hasPlan {
			planIDs = append(planIDs, r.planID)
		}
	}
	evaluated, err := plansWithEvaluations(db, userID, planIDs)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		r.hasEvals = r.hasPlan && evaluated[r.planID]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Training History (last %d weeks)\n", macroHistoryWeeks)
	sb.WriteString("km/Hours/Sessions are logged volume across ALL sports, not running only — treat them as an upper bound on running volume.\n")
	sb.WriteString("Planned/Evaluated counts sessions from that week's Stride plan: Planned is what the plan prescribed, Evaluated is how many of those the coach's session evaluations matched to a logged workout.\n")
	sb.WriteString("`?` means that week's plan was never evaluated at all, so nothing can be said about adherence — read the Sessions column for what the athlete actually trained, and never read `?` as skipped training.\n")
	sb.WriteString("A number IS a real signal: the plan was evaluated, and that many prescriptions were matched. A `0` next to logged sessions means the athlete trained but matched none of the plan — prescription drift, not missing data.\n")
	sb.WriteString("Easy/Thr/Hard are minutes in the matching HR zones.\n\n")
	sb.WriteString("| Week | km (all sports) | Hours (all sports) | Sessions (all sports) | Avg HR | Planned/Evaluated | Easy/Thr/Hard min |\n")
	sb.WriteString("|------|-----------------|--------------------|-----------------------|--------|-------------------|-------------------|\n")
	for _, week := range order {
		r := rows[week]
		if !r.hasVolume && !r.hasPlan {
			fmt.Fprintf(&sb, "| %s | -- | -- | -- | -- | -- | -- |\n", r.weekStart)
			continue
		}
		hr := "--"
		if r.avgHR > 0 {
			hr = fmt.Sprintf("%.0f", r.avgHR)
		}
		adherence := "--"
		if r.hasPlan {
			// SessionsCompleted counts only evaluations that matched a
			// prescription, so a plain "0" is ambiguous: either the plan was
			// never evaluated, or it was evaluated and the athlete matched
			// nothing. Only the first is unknown, and it is decided by whether
			// any evaluation rows exist for the plan — not by the all-sports
			// volume column, which would hide the second case (chronic
			// prescription drift) exactly when the coach most needs to see it.
			done := fmt.Sprintf("%d", r.completed)
			if !r.hasEvals {
				done = "?"
			}
			adherence = fmt.Sprintf("%d/%s", r.planned, done)
		}
		mix := "--"
		if r.hasPlan {
			mix = fmt.Sprintf("%d/%d/%d", r.easyMin, r.thresholdMin, r.hardMin)
		}
		fmt.Fprintf(&sb, "| %s | %.1f | %.1f | %d | %s | %s | %s |\n",
			r.weekStart, r.km, float64(r.seconds)/3600, r.workouts, hr, adherence, mix)
	}
	sb.WriteString("\n")
	return sb.String(), nil
}

// macroPlanHistoryPage is how many plan-history rows loadMacroPlanWeeks reads
// per call, and macroPlanHistoryMaxPages bounds the loop so a pathological
// history (or a GetPlanHistory that stops advertising the end) can never spin.
// Six pages already exceed GetPlanHistory's own 156-week depth cap.
const (
	macroPlanHistoryPage     = MacroBlockWeeks
	macroPlanHistoryMaxPages = 12
)

// loadMacroPlanWeeks returns every plan-history week back to windowStart.
//
// GetPlanHistory pages by rows — one per stored stride plan, newest first —
// not by calendar week, and it pages back from today rather than from the
// block start. A single call with a guessed row count therefore drops the
// oldest weeks of the window whenever plan rows newer than the window sit in
// front of it — a manual regeneration whose start week is in the past, or a
// block starting in the future. Paging until a row older than the window
// appears is what makes the window explicit.
//
// windowStart is the OLDEST week of the window (start - macroHistoryWeeks), not
// the block start: bounding on the newest week would stop after one page.
func loadMacroPlanWeeks(db *sql.DB, userID int64, windowStart string) ([]WeekSummary, error) {
	return collectMacroPlanWeeks(func(limit, offset int) ([]WeekSummary, bool, error) {
		weeks, _, hasMore, err := GetPlanHistory(db, userID, limit, offset)
		return weeks, hasMore, err
	}, windowStart, userID)
}

// macroPlanPager reads one page of plan history. It exists so the paging loop
// below can be driven without a database — the loop's exits (a short page,
// hasMore=false, the window bound, the page cap) are otherwise reachable only
// through GetPlanHistory's own 156-week depth cap.
type macroPlanPager func(limit, offset int) ([]WeekSummary, bool, error)

// collectMacroPlanWeeks is loadMacroPlanWeeks's paging loop over any pager.
func collectMacroPlanWeeks(page macroPlanPager, windowStart string, userID int64) ([]WeekSummary, error) {
	var out []WeekSummary
	offset := 0
	for p := 0; p < macroPlanHistoryMaxPages; p++ {
		weeks, hasMore, err := page(macroPlanHistoryPage, offset)
		if err != nil {
			return nil, fmt.Errorf("load plan history: %w", err)
		}
		out = append(out, weeks...)
		// Rows come back week_start DESC, so the last row of a page is the
		// oldest one seen: once it predates the window, so does everything
		// after it.
		if len(weeks) == 0 || !hasMore || weeks[len(weeks)-1].WeekStart < windowStart {
			return out, nil
		}
		offset += len(weeks)
	}
	log.Printf("stride: macro history stopped paging plan history for user %d after %d pages", userID, macroPlanHistoryMaxPages)
	return out, nil
}

// plansWithEvaluations returns the subset of planIDs that have at least one
// stride_evaluations row, whatever its compliance.
//
// This is the difference between "the plan was never evaluated" and "the plan
// was evaluated and the athlete matched none of it": WeekSummary.
// SessionsCompleted counts only compliant/partial evaluations of a prescribed
// session, so both cases arrive as zero.
func plansWithEvaluations(db *sql.DB, userID int64, planIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(planIDs))
	if len(planIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(planIDs)+1)
	args = append(args, userID)
	placeholders := make([]string, len(planIDs))
	for i, id := range planIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(
		`SELECT DISTINCT plan_id FROM stride_evaluations WHERE user_id = ? AND plan_id IN (`+
			strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query evaluated plans: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan evaluated plan id: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// renderMacroTrainingLoad renders the acute:chronic ratio and the classified
// training status as one line. Non-fatal: an athlete without enough load
// history gets "insufficient data".
func renderMacroTrainingLoad(db *sql.DB, userID int64) string {
	var sb strings.Builder
	sb.WriteString("## Current Training Load\n")

	acr, acute, chronic, err := training.ComputeACR(db, userID, time.Now().UTC())
	if err != nil {
		log.Printf("stride: compute ACR for user %d: %v", userID, err)
		acr = nil
	}
	loads, err := training.GetWeeklyLoads(db, userID, 8)
	if err != nil {
		log.Printf("stride: load weekly loads for user %d: %v", userID, err)
		loads = nil
	}
	status := training.ClassifyTrainingStatus(loads, acr)

	if acr == nil {
		fmt.Fprintf(&sb, "- ACR: insufficient data — status: %s\n\n", status)
		return sb.String()
	}
	fmt.Fprintf(&sb, "- ACR: %.2f (acute=%.1f, chronic=%.1f) — status: %s\n\n", *acr, acute, chronic, status)
	return sb.String()
}

// renderMacroVO2maxTrend reduces the VO2max estimate history to at most 6
// monthly points (the last estimate of each month), oldest first — enough to
// read a direction without spending tokens on every estimate.
func renderMacroVO2maxTrend(db *sql.DB, userID int64) string {
	var sb strings.Builder
	sb.WriteString("## VO2max Trend\n")

	// 200 estimates comfortably covers the last 6 months for any training
	// frequency; the reduction below is what bounds the output, not this limit.
	history, err := training.GetVO2maxHistory(db, userID, 200)
	if err != nil {
		log.Printf("stride: load VO2max history for user %d: %v", userID, err)
		history = nil
	}
	if len(history) == 0 {
		sb.WriteString("No VO2max estimates recorded.\n\n")
		return sb.String()
	}

	// History is ascending, so the last write per month wins.
	byMonth := map[string]float64{}
	var months []string
	for _, e := range history {
		if len(e.EstimatedAt) < 7 {
			continue
		}
		month := e.EstimatedAt[:7]
		if _, seen := byMonth[month]; !seen {
			months = append(months, month)
		}
		byMonth[month] = e.VO2max
	}
	if len(months) == 0 {
		sb.WriteString("No VO2max estimates recorded.\n\n")
		return sb.String()
	}
	if len(months) > 6 {
		months = months[len(months)-6:]
	}
	sb.WriteString("Monthly VO2max (ml/kg/min), oldest first:\n")
	for _, m := range months {
		fmt.Fprintf(&sb, "- %s: %.1f\n", m, byMonth[m])
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroLactateTests renders the three most recent lactate tests as date,
// threshold speed and threshold HR. Stage data is deliberately left out: the
// macro block needs the threshold anchor, not the curve.
func renderMacroLactateTests(db *sql.DB, userID int64) string {
	var sb strings.Builder
	sb.WriteString("## Recent Lactate Tests\n")

	tests, err := lactate.List(db, userID)
	if err != nil {
		log.Printf("stride: load lactate tests for user %d: %v", userID, err)
		tests = nil
	}
	if len(tests) > 3 {
		tests = tests[:3]
	}
	if len(tests) == 0 {
		sb.WriteString("No lactate tests recorded.\n\n")
		return sb.String()
	}
	for _, t := range tests {
		pt := t.PrimaryThreshold
		if pt == nil || !pt.Valid {
			fmt.Fprintf(&sb, "- %s: no valid threshold derived\n", t.Date)
			continue
		}
		fmt.Fprintf(&sb, "- %s: threshold %.1f km/h", t.Date, pt.SpeedKmh)
		if pt.SpeedKmh > 0 {
			fmt.Fprintf(&sb, " (%s/km)", formatPaceSecPerKm(3600/pt.SpeedKmh))
		}
		if pt.HeartRateBpm > 0 {
			fmt.Fprintf(&sb, ", HR %d bpm", pt.HeartRateBpm)
		}
		if pt.Method != "" {
			fmt.Fprintf(&sb, " (%s)", pt.Method)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroWorkoutLibrary lists the library as ids, names and types only.
// The macro block never writes session texts, so the full workout bodies the
// weekly prompt carries would be wasted tokens here — the ids are what
// key_sessions.library_id needs to reference.
//
// Names are encrypted at rest and decrypted by ListLibraryWorkouts (via
// decryptField in workouts.go), so w.Name is plaintext here and must not be
// decrypted again; workout_type is a queryable enum column and is never
// encrypted. TestMacroLibraryAndPreviousBlockAreDecrypted pins both.
func renderMacroWorkoutLibrary(ctx context.Context, db *sql.DB, userID int64) string {
	var sb strings.Builder
	sb.WriteString("## Workout Library\n")

	workouts, err := ListLibraryWorkouts(ctx, db, userID, false)
	if err != nil {
		log.Printf("stride: load workout library for user %d: %v", userID, err)
		workouts = nil
	}
	if len(workouts) == 0 {
		sb.WriteString("No library workouts recorded. Leave every key_sessions.library_id null.\n\n")
		return sb.String()
	}
	sb.WriteString("Names and types only — reference an entry from a key session by setting its library_id to the id here.\n")
	for _, w := range workouts {
		marker := ""
		if w.IsReference {
			marker = " [WEEKLY REFERENCE]"
		}
		workoutType := w.WorkoutType
		if workoutType == "" {
			workoutType = "unspecified"
		}
		fmt.Fprintf(&sb, "- id=%d%s %q (%s)\n", w.ID, marker, w.Name, workoutType)
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderMacroPreviousBlock embeds the block being continued: its goal, its
// periodisation and its last 8 week specs, verbatim as JSON. This is what makes
// an extension continue the athlete's training instead of restarting from base.
// Non-fatal: with no previous block the section says so and the coach plans a
// fresh block.
//
// goal_json, periodisation_json, key_sessions_json and intent are encrypted at
// rest and decrypted by scanMacroPlan/scanMacroWeek, so the values marshalled
// below are plaintext — the store layer is the single decrypt point and this
// renderer must not add a second one.
func renderMacroPreviousBlock(ctx context.Context, db *sql.DB, userID int64, start time.Time) string {
	var sb strings.Builder
	sb.WriteString("## Previous Block\n")

	prev, err := loadPreviousMacroPlan(ctx, db, userID, start.Format(dateLayout))
	if err != nil {
		log.Printf("stride: load previous macro plan for user %d: %v", userID, err)
		prev = nil
	}
	if prev == nil {
		sb.WriteString("No previous macro block found — plan this block as a fresh start.\n\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "The block being continued ran %s to %s. Continue from where it ends; do not restart from base.\n\n",
		prev.StartWeek, prev.EndWeek)

	sb.WriteString("Previous goal:\n```json\n")
	sb.WriteString(marshalForPrompt(prev.Goal))
	sb.WriteString("\n```\n\n")

	sb.WriteString("Previous periodisation:\n```json\n")
	sb.WriteString(marshalForPrompt(prev.Periodisation))
	sb.WriteString("\n```\n\n")

	tail := prev.Weeks
	if len(tail) > 8 {
		tail = tail[len(tail)-8:]
	}
	fmt.Fprintf(&sb, "Previous block, last %d week specs:\n```json\n", len(tail))
	sb.WriteString(marshalForPrompt(tail))
	sb.WriteString("\n```\n\n")
	return sb.String()
}

// loadPreviousMacroPlan returns the active macro plan the block starting at
// startWeek continues: the one covering the week directly before it, falling
// back to the most recent active block that ended before startWeek. Returns
// nil, nil when the athlete has no previous block.
func loadPreviousMacroPlan(ctx context.Context, db *sql.DB, userID int64, startWeek string) (*MacroPlan, error) {
	start, err := parseWeekDate(startWeek)
	if err != nil {
		return nil, fmt.Errorf("parse start week: %w", err)
	}
	prevWeek := start.AddDate(0, 0, -7).Format(dateLayout)

	if plan, err := GetActiveMacroPlan(ctx, db, userID, prevWeek); err != nil {
		return nil, err
	} else if plan != nil {
		return plan, nil
	}

	var id int64
	err = db.QueryRowContext(ctx, `
		SELECT id
		FROM stride_macro_plans
		WHERE user_id = ? AND status = ? AND start_week < ?
		ORDER BY start_week DESC
		LIMIT 1
	`, userID, MacroPlanStatusActive, startWeek).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return GetMacroPlanByID(ctx, db, id, userID)
}

// marshalForPrompt renders a value as compact JSON for embedding in a prompt.
// A marshal failure degrades to "null" rather than failing the whole prompt —
// the block is context, not a hard requirement.
func marshalForPrompt(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		log.Printf("stride: marshal macro prompt value: %v", err)
		return "null"
	}
	return string(raw)
}

// formatRaceTime renders a race time in seconds as h:mm:ss (or m:ss under an
// hour), the form race times are normally quoted in.
func formatRaceTime(secs int) string {
	h, m, s := secondsToHMS(secs)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatPaceSecPerKm renders a pace in seconds per km as m:ss.
func formatPaceSecPerKm(secPerKm float64) string {
	if secPerKm <= 0 {
		return "--"
	}
	total := int(secPerKm + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}
