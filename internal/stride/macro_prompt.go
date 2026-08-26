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
const macroHistoryWeeks = 26

// MacroMode selects which macro block is being generated. It changes the
// inputs, not the instructions: extension mode additionally embeds the block
// being continued so the new block starts where the old one ended instead of
// restarting from base.
//
// The type and the buildMacroInputs signature are consumed by
// GenerateMacroPlan — keep both stable.
type MacroMode string

const (
	// MacroModeInitial is the athlete's first block, or a block generated when
	// no previous one is available to continue.
	MacroModeInitial MacroMode = "initial"
	// MacroModeExtension appends a fresh block to the one that is running out.
	MacroModeExtension MacroMode = "extension"
	// MacroModeManual is a user-triggered regeneration of the current horizon.
	MacroModeManual MacroMode = "manual"
)

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
// which are compile-time strings and so cannot call strconv.
const macroBlockWeeksStr = "26"

// macroInstructions is the static instruction block every macro prompt opens
// with: the coaching philosophy, the periodisation doctrine, the standing
// half-marathon priority rule, and the JSON output contract.
const macroInstructions = bakkenPhilosophy + "\n\n" +
	macroPeriodisation + "\n" +
	macroHalfMarathonRule + "\n" +
	macroOutputContract

// macroSystemPrompt returns the static instruction block for macro plan
// generation. Callers pair it with buildMacroInputs to form the full prompt.
func macroSystemPrompt() string {
	return macroInstructions
}

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
// Hard failures — preferences, the race calendar and training history — return
// an error. Optional context that a new athlete simply has none of (race
// predictions, ACR, VO2max, lactate, the workout library, the previous block)
// degrades to a short "none recorded" line so a first block can still be built.
func buildMacroInputs(ctx context.Context, db *sql.DB, userID int64, startWeek string, mode MacroMode) (string, error) {
	start, err := parseWeekDate(startWeek)
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
	fmt.Fprintf(&sb, "- Mode: %s\n\n", mode)

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

// stripGoalRaceSection removes the trailing "Goal Race:" block that
// BuildUserProfileBlock appends from the legacy goal_race_* preferences. Stride
// macro planning derives its goal from stride_races and the block's own goal
// object, so those preferences must not leak into the prompt and contradict it.
func stripGoalRaceSection(block string) string {
	if idx := strings.Index(block, "Goal Race:\n"); idx >= 0 {
		block = block[:idx]
	}
	block = strings.TrimRight(block, "\n")
	// An athlete with only goal_race_* preferences and no HR/zone data leaves
	// nothing but the header behind — treat that as no profile at all.
	if block == "" || block == "User Profile:" {
		return ""
	}
	return block + "\n"
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

// weeksAhead returns how far start lies in the future, in weeks rounded up with
// a week of slack, or 0 when start is today or in the past. It is only used to
// widen a query page, so erring high is free.
func weeksAhead(start time.Time) int {
	gap := start.Sub(time.Now().UTC())
	if gap <= 0 {
		return 0
	}
	return int(gap/(7*24*time.Hour)) + 1
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
	planned      int
	completed    int
	hasPlan      bool
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
	summaries, err := training.WeeklySummaries(db, userID)
	if err != nil {
		return "", fmt.Errorf("load weekly summaries: %w", err)
	}
	// GetPlanHistory pages back from today, but the window we render is the
	// macroHistoryWeeks weeks before start. When start is in the future (the
	// normal case in extension mode) the oldest weeks of the window fall off
	// the page unless the limit covers the gap as well.
	weeks, _, _, err := GetPlanHistory(db, userID, macroHistoryWeeks+weeksAhead(start), 0)
	if err != nil {
		return "", fmt.Errorf("load plan history: %w", err)
	}

	rows := make(map[string]*macroHistoryRow, macroHistoryWeeks)
	order := make([]string, 0, macroHistoryWeeks)
	for i := macroHistoryWeeks; i >= 1; i-- {
		week := start.AddDate(0, 0, -7*i).Format(dateLayout)
		order = append(order, week)
		rows[week] = &macroHistoryRow{weekStart: week}
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
		if r, ok := rows[w.WeekStart]; ok {
			r.planned = w.SessionsPlanned
			r.completed = w.SessionsCompleted
			r.hasPlan = true
			r.easyMin = w.EasySeconds / 60
			r.thresholdMin = w.ThresholdSeconds / 60
			r.hardMin = w.HardSeconds / 60
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Training History (last %d weeks)\n", macroHistoryWeeks)
	sb.WriteString("km/Hours/Sessions are logged volume across ALL sports, not running only — treat them as an upper bound on running volume.\n")
	sb.WriteString("Planned/Done counts sessions from that week's Stride plan; easy/threshold/hard are minutes in the matching HR zones.\n\n")
	sb.WriteString("| Week | km (all sports) | Hours (all sports) | Sessions (all sports) | Avg HR | Planned/Done | Easy/Thr/Hard min |\n")
	sb.WriteString("|------|-----------------|--------------------|-----------------------|--------|--------------|-------------------|\n")
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
			adherence = fmt.Sprintf("%d/%d", r.planned, r.completed)
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
