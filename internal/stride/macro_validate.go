package stride

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// macroRampLimit is the ceiling on week-over-week volume growth: +10%, the
// load rule stated in macroPeriodisation. The only week exempt from it is the
// one directly after a deload, which may return to the pre-deload level.
const macroRampLimit = 1.10

// macroFloatEpsilon absorbs float rounding so a plan that lands exactly on a
// limit — 60.0 km ramping to 66.0 km, or a week hitting the distance cap on
// the nose — is not rejected by the last binary digit.
const macroFloatEpsilon = 1e-9

// macroTaperMinDistanceM is the shortest race that may define the block's peak
// and taper: a half marathon. It is 21000 rather than the exact 21097.5 so a
// race listed as a round 21.0 km still counts. Anything shorter is trained
// through, per macroHalfMarathonRule.
const macroTaperMinDistanceM = 21000

// macroSharpeningDistancesM are the nominal distances the standing
// half-marathon rule runs as B/C sharpening races inside HM training: 5 km and
// 10 km. The tolerance covers real race listings that measure a little long or
// short (a 5.1 km parkrun-style course is still a 5k).
var macroSharpeningDistancesM = []float64{5000, 10000}

const macroRaceDistanceTolerance = 0.10

// MacroValidationContext is everything outside the model's answer that the
// answer has to agree with: the horizon it was asked for, the athlete's hard
// volume limit, and the race calendar it was shown.
//
// Races is the same list buildMacroInputs rendered into the prompt; races that
// have already been run, or that fall outside the block, are ignored here.
type MacroValidationContext struct {
	// StartWeek is the block's first week, YYYY-MM-DD and a Monday.
	StartWeek string
	// WeeklyDistanceCap is the athlete's stride_weekly_distance_cap in km.
	// Zero means no cap is configured and the check is skipped.
	WeeklyDistanceCap float64
	// Races is the athlete's race calendar.
	Races []Race
}

// ValidateMacroPlan checks a coach response against the contract it was given
// and reports the violations it finds as one human-readable error.
//
// The output is fed back to the model on a retry, so the message names the
// offending week and the expected value rather than just the rule: a caller
// can hand the error text straight back as "you returned this, fix it". That
// is also why the per-week checks accumulate instead of returning on the first
// problem — a retry that fixes one rule and trips the next wastes a round.
// Two inputs do end the pass early, because nothing after them could be
// checked without reporting noise: a nil plan, and a block start week that is
// not a parseable Monday. Otherwise every rule below runs, so a plan that
// comes back clean has been checked against all of them.
//
// What it does not check: that race_id and library_id point at rows the user
// owns. Those are foreign keys, verified against the database by
// verifyMacroReferences at persist time, not against this context.
func ValidateMacroPlan(plan *MacroPlanResponse, in MacroValidationContext) error {
	p := &macroProblems{}
	if plan == nil {
		p.addf("no plan was returned")
		return p.err()
	}

	// Without a usable block start, no week_start, mesocycle span or race week
	// can be placed, so every remaining check would report noise.
	start, err := parseMondayWeek(in.StartWeek)
	if err != nil {
		p.addf("block start week is unusable: %v", err)
		return p.err()
	}

	if len(plan.Weeks) != MacroBlockWeeks {
		p.addf("weeks: expected exactly %d weeks, got %d", MacroBlockWeeks, len(plan.Weeks))
	}

	weekDates := validateMacroCalendar(p, plan.Weeks, start)
	validateMacroWeekFields(p, plan, in)
	validateMacroMesocycles(p, plan.Mesocycles, start)
	validateMacroRamp(p, plan.Weeks)
	validateMacroRaceShape(p, plan.Weeks, weekDates, in.Races)

	return p.err()
}

// macroProblems accumulates validation failures so one call reports them all.
type macroProblems struct {
	items []string
}

func (p *macroProblems) addf(format string, args ...any) {
	p.items = append(p.items, fmt.Sprintf(format, args...))
}

func (p *macroProblems) err() error {
	if len(p.items) == 0 {
		return nil
	}
	noun := "problems"
	if len(p.items) == 1 {
		noun = "problem"
	}
	return fmt.Errorf("macro plan validation failed (%d %s):\n- %s",
		len(p.items), noun, strings.Join(p.items, "\n- "))
}

// macroWeekLabel names a week the way the error messages refer to it: its
// 1-based position plus the date it claims to start on.
func macroWeekLabel(i int, w MacroWeekResponse) string {
	if w.WeekStart == "" {
		return fmt.Sprintf("week %d", i+1)
	}
	return fmt.Sprintf("week %d (%s)", i+1, w.WeekStart)
}

// validateMacroCalendar checks that the weeks are contiguous Mondays starting
// at the requested block start and numbered 1..n in order. It returns the
// parsed week starts, with a zero time for any week whose date did not parse,
// so the race checks can place a race without re-parsing.
func validateMacroCalendar(p *macroProblems, weeks []MacroWeekResponse, start time.Time) []time.Time {
	dates := make([]time.Time, len(weeks))
	for i, w := range weeks {
		if w.Seq != i+1 {
			p.addf("%s: seq is %d, expected %d", macroWeekLabel(i, w), w.Seq, i+1)
		}
		d, err := parseWeekDate(w.WeekStart)
		if err != nil {
			p.addf("%s: week_start %q is not a YYYY-MM-DD date", macroWeekLabel(i, w), w.WeekStart)
			continue
		}
		dates[i] = d
		if d.Weekday() != time.Monday {
			p.addf("%s: week_start is a %s; every week must start on a Monday", macroWeekLabel(i, w), d.Weekday())
		}
		if want := start.AddDate(0, 0, 7*i); !d.Equal(want) {
			p.addf("%s: week_start should be %s — weeks must be contiguous Mondays from %s",
				macroWeekLabel(i, w), want.Format(dateLayout), start.Format(dateLayout))
		}
	}
	return dates
}

// validateMacroWeekFields checks the per-week enums, the mesocycle reference
// and the weekly volume: not negative, and not over the athlete's cap when one
// is configured.
func validateMacroWeekFields(p *macroProblems, plan *MacroPlanResponse, in MacroValidationContext) {
	names := make(map[string]bool, len(plan.Mesocycles))
	known := make([]string, 0, len(plan.Mesocycles))
	for _, m := range plan.Mesocycles {
		if m.Name == "" || names[m.Name] {
			continue
		}
		names[m.Name] = true
		known = append(known, m.Name)
	}

	for i, w := range plan.Weeks {
		label := macroWeekLabel(i, w)
		if !isMacroPhase(w.Phase) {
			p.addf("%s: phase %q is not one of %s", label, w.Phase, joinQuoted(macroPhaseValues))
		}
		if !isMacroLoadLevel(w.LoadLevel) {
			p.addf("%s: load_level %q is not one of %s", label, w.LoadLevel, joinQuoted(macroLoadLevelValues))
		}
		switch {
		case w.Mesocycle == "":
			p.addf("%s: mesocycle is empty; every week must name the mesocycle it belongs to", label)
		case !names[w.Mesocycle]:
			p.addf("%s: mesocycle %q is not one of the returned mesocycles (%s)", label, w.Mesocycle, joinQuoted(known))
		}
		if w.TargetKm < 0 {
			p.addf("%s: target_km is %.1f; weekly volume cannot be negative", label, w.TargetKm)
		}
		if in.WeeklyDistanceCap > 0 && w.TargetKm > in.WeeklyDistanceCap+macroFloatEpsilon {
			p.addf("%s: target_km %.1f exceeds the athlete's weekly distance cap of %.1f km",
				label, w.TargetKm, in.WeeklyDistanceCap)
		}
	}
}

// validateMacroMesocycles checks that the periodisation tiles the block: every
// mesocycle is a uniquely named, valid-phase span inside the horizon, and
// together they cover all MacroBlockWeeks weeks with no gaps and no overlaps.
func validateMacroMesocycles(p *macroProblems, mesocycles []Mesocycle, start time.Time) {
	if len(mesocycles) == 0 {
		p.addf("mesocycles: none returned; the block must be split into named mesocycles covering all %d weeks", MacroBlockWeeks)
		return
	}

	coverage := make([]int, MacroBlockWeeks)
	seen := make(map[string]bool, len(mesocycles))
	for i, m := range mesocycles {
		label := fmt.Sprintf("mesocycle %d", i+1)
		if m.Name != "" {
			label = fmt.Sprintf("mesocycle %q", m.Name)
		}
		switch {
		case m.Name == "":
			p.addf("%s: name is empty; weeks reference their mesocycle by name", label)
		case seen[m.Name]:
			p.addf("%s: name is used by more than one mesocycle; names must be unique", label)
		}
		seen[m.Name] = true

		if !isMacroPhase(m.Phase) {
			p.addf("%s: phase %q is not one of %s", label, m.Phase, joinQuoted(macroPhaseValues))
		}
		if m.Weeks < 1 {
			p.addf("%s: weeks is %d; a mesocycle must span at least one week", label, m.Weeks)
			continue
		}
		d, err := parseWeekDate(m.StartWeek)
		if err != nil {
			p.addf("%s: start_week %q is not a YYYY-MM-DD date", label, m.StartWeek)
			continue
		}
		offsetDays := int(d.Sub(start).Hours() / 24)
		if offsetDays%7 != 0 {
			p.addf("%s: start_week %s is not aligned to the block's Mondays (block starts %s)",
				label, m.StartWeek, start.Format(dateLayout))
			continue
		}
		first := offsetDays / 7
		if first < 0 || first+m.Weeks > MacroBlockWeeks {
			p.addf("%s: spans weeks %d-%d, which falls outside the %d-week block",
				label, first+1, first+m.Weeks, MacroBlockWeeks)
			continue
		}
		for w := first; w < first+m.Weeks; w++ {
			coverage[w]++
		}
	}

	var gaps, overlaps []string
	for i, c := range coverage {
		switch {
		case c == 0:
			gaps = append(gaps, strconv.Itoa(i+1))
		case c > 1:
			overlaps = append(overlaps, strconv.Itoa(i+1))
		}
	}
	if len(gaps) > 0 {
		p.addf("mesocycles: week(s) %s are not covered by any mesocycle", strings.Join(gaps, ", "))
	}
	if len(overlaps) > 0 {
		p.addf("mesocycles: week(s) %s are covered by more than one mesocycle", strings.Join(overlaps, ", "))
	}
}

// validateMacroRamp enforces the +10% week-over-week volume ceiling. The week
// directly after a deload is exempt: it is meant to return to the pre-deload
// level, which is a jump of far more than 10% by design.
func validateMacroRamp(p *macroProblems, weeks []MacroWeekResponse) {
	for i := 1; i < len(weeks); i++ {
		prev, cur := weeks[i-1], weeks[i]
		if prev.LoadLevel == LoadLevelDeload {
			continue
		}
		limit := prev.TargetKm * macroRampLimit
		if cur.TargetKm > limit+macroFloatEpsilon {
			p.addf("%s: target_km %.1f is more than +10%% over the previous week's %.1f km (max %.1f); only the week directly after a deload may jump further",
				macroWeekLabel(i, cur), cur.TargetKm, prev.TargetKm, limit)
		}
	}
}

// validateMacroRaceShape checks how the block is built around the calendar:
//
//   - an A-priority half marathon or longer owns its week (phase "race") and
//     the two weeks before it (phase "taper");
//   - a 5 km or 10 km race is trained through, so no week is tapered on its
//     behalf.
//
// The two rules meet when a short race sits inside a real taper — a 10 km
// tune-up two weeks before the goal half marathon is normal training, not a
// taper for the 10 km. Weeks already claimed by an A race's taper are
// therefore resolved first and skipped by the short-race check.
func validateMacroRaceShape(p *macroProblems, weeks []MacroWeekResponse, dates []time.Time, races []Race) {
	claimed := make(map[int]bool)
	for _, r := range races {
		if !macroRaceCounts(r) || r.Priority != "A" || r.DistanceM < macroTaperMinDistanceM {
			continue
		}
		if idx := macroWeekIndexForDate(dates, r.Date); idx >= 0 {
			for back := 0; back <= 2; back++ {
				if idx-back >= 0 {
					claimed[idx-back] = true
				}
			}
		}
	}

	for _, r := range races {
		if !macroRaceCounts(r) {
			continue
		}
		// A race outside the horizon (the prompt shows a few weeks past the
		// end week as context) has no week to shape, so there is nothing here
		// to be wrong about.
		idx := macroWeekIndexForDate(dates, r.Date)
		if idx < 0 {
			continue
		}
		switch {
		case r.Priority == "A" && r.DistanceM >= macroTaperMinDistanceM:
			if weeks[idx].Phase != MacroPhaseRace {
				p.addf("%s: phase is %q, expected %q — it contains A-priority race %d on %s (%.1f km)",
					macroWeekLabel(idx, weeks[idx]), weeks[idx].Phase, MacroPhaseRace, r.ID, r.Date, r.DistanceM/1000)
			}
			for back := 1; back <= 2; back++ {
				j := idx - back
				// A race in the first two weeks of the block has no room for a
				// full taper inside the horizon; only the weeks that exist are
				// checked.
				if j < 0 {
					continue
				}
				if weeks[j].Phase != MacroPhaseTaper {
					p.addf("%s: phase is %q, expected %q — it is one of the two weeks before A-priority race %d on %s",
						macroWeekLabel(j, weeks[j]), weeks[j].Phase, MacroPhaseTaper, r.ID, r.Date)
				}
			}
		case isMacroSharpeningRace(r):
			for back := 0; back <= 2; back++ {
				j := idx - back
				if j < 0 || claimed[j] {
					continue
				}
				if weeks[j].Phase == MacroPhaseTaper {
					p.addf("%s: phase is %q, but race %d on %s is a %.0f km race — 5 km and 10 km races are run inside normal training and never get a taper week",
						macroWeekLabel(j, weeks[j]), MacroPhaseTaper, r.ID, r.Date, r.DistanceM/1000)
				}
			}
		}
	}
}

// macroRaceCounts reports whether a calendar entry is still a race to plan
// for. A race with a result has already been run, matching what
// renderMacroUpcomingRaces showed the coach.
func macroRaceCounts(r Race) bool {
	return r.ResultTime == nil
}

// isMacroSharpeningRace reports whether r is a 5 km or 10 km race.
func isMacroSharpeningRace(r Race) bool {
	for _, nominal := range macroSharpeningDistancesM {
		if math.Abs(r.DistanceM-nominal) <= nominal*macroRaceDistanceTolerance {
			return true
		}
	}
	return false
}

// macroWeekIndexForDate returns the index of the week whose 7-day span
// contains date, or -1 when no week does (including an unparseable date).
func macroWeekIndexForDate(dates []time.Time, date string) int {
	d, err := parseWeekDate(date)
	if err != nil {
		return -1
	}
	for i, weekStart := range dates {
		if weekStart.IsZero() {
			continue
		}
		if !d.Before(weekStart) && d.Before(weekStart.AddDate(0, 0, 7)) {
			return i
		}
	}
	return -1
}
