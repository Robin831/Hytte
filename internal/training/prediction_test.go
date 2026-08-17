package training

import (
	"database/sql"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// lapsFor builds predictionLap slices tersely: pairs of (durS, paceSecPerKm),
// with HR fixed.
func lapsFor(hr int, pairs ...float64) []predictionLap {
	laps := make([]predictionLap, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		durS, pace := pairs[i], pairs[i+1]
		laps = append(laps, predictionLap{durS: durS, distM: durS / pace * 1000, hr: hr})
	}
	return laps
}

// TestBestWindowRejectsMixedRepsAndRecoveries pins the bug the coach found:
// a 6x6min interval block with float jogs used to average into a fictional
// "sustained 22 minutes" — the window must be rejected, not smeared.
func TestBestWindowRejectsMixedRepsAndRecoveries(t *testing.T) {
	// 3 x 6min reps at ~290 s/km with 2min jogs at ~385 s/km between: any
	// >=20min contiguous window necessarily spans a jog, so no clean window
	// exists.
	laps := lapsFor(160,
		360, 290, 120, 385, 360, 289, 120, 380, 360, 293,
	)
	if e := bestWindow(laps, 1, "2026-08-04T10:00:00Z"); e != nil {
		t.Fatalf("mixed reps+recovery window must be rejected, got %+v", e)
	}

	// The same session's honest reading comes from the work-lap cluster.
	ie := extractIntervalEffort(laps, 1, "2026-08-04T10:00:00Z")
	if ie == nil {
		t.Fatal("expected an interval work-lap effort")
	}
	if ie.Reps != 3 {
		t.Errorf("expected 3 work reps, got %d", ie.Reps)
	}
	if ie.WorkPaceSecPerKm < 288 || ie.WorkPaceSecPerKm > 293 {
		t.Errorf("work pace should reflect the reps only (~290), got %.1f", ie.WorkPaceSecPerKm)
	}
}

// TestBestWindowAcceptsCleanTempo: a continuous tempo with even laps remains
// a valid sustained anchor.
func TestBestWindowAcceptsCleanTempo(t *testing.T) {
	laps := lapsFor(158, 600, 300, 600, 302, 600, 298)
	e := bestWindow(laps, 2, "2026-08-01T10:00:00Z")
	if e == nil {
		t.Fatal("clean tempo window should qualify")
	}
	// The fastest qualifying window wins; it must be at least the 20-min
	// floor and at the tempo's pace (never a smeared slower average).
	if e.DurationSeconds < 1200 || e.PaceSecPerKm < 295 || e.PaceSecPerKm > 305 {
		t.Errorf("unexpected window: %+v", e)
	}
}

// stripHR returns a copy of laps with every HR zeroed — the shape of a workout
// recorded without a strap.
func stripHR(laps []predictionLap) []predictionLap {
	out := make([]predictionLap, len(laps))
	copy(out, laps)
	for i := range out {
		out[i].hr = 0
	}
	return out
}

// Real lap rows, pasted from the athlete's DB. These are the sessions the
// warmup-domination gates were designed against, so the tests below are a
// before/after diff on production data rather than on invented numbers.
var (
	// workout 94, 2026-08-04: 15:01 warmup then 4x6min at 4:46-4:54/km with
	// 2min jogs, cooldown. The bug: laps 1+2 spread only 10.8%, so the warmup
	// and the first rep were reported as one "4.1 km in 21:00 at 5:08/km".
	lapsAug4Intervals = lapRows(
		900.94, 2839.48, 144, // warmup, 5:17/km
		360.0, 1259.36, 158, // rep 1, 4:46/km
		120.0, 308.25, 141, // jog
		360.0, 1245.0, 158, // rep 2
		120.0, 318.29, 149, // jog
		360.0, 1225.48, 157, // rep 3
		120.0, 287.54, 142, // jog
		360.0, 1255.17, 162, // rep 4
		120.0, 350.0, 153, // jog
		518.11, 1521.06, 149, // cooldown
	)
	// workout 30, 2026-04-12: easy run, HR drifts 136 -> 157 end to end.
	lapsApr12Easy = lapRows(
		331.36, 1000, 136, 329.33, 1000, 141, 339.0, 1000, 137, 350.87, 1000, 142,
		354.46, 1000, 141, 373.67, 1000, 148, 370.27, 1000, 149, 398.8, 1000, 157,
		345.26, 1000, 140, 254.14, 687.35, 140,
	)
	// workout 58, 2026-05-16: easy run, HR 121 -> 151, and the opening km
	// alone steps +15 bpm at unchanged pace as the body warms up.
	lapsMay16Easy = lapRows(
		341.64, 1000, 121, 336.91, 1000, 136, 324.33, 1000, 132, 354.76, 1000, 141,
		356.91, 1000, 137, 348.67, 1000, 137, 387.5, 1000, 143, 320.64, 1000, 151,
		144.6, 412.5, 142,
	)
	// workout 88, 2026-07-23: long easy run whose opening km steps +19 bpm
	// (124 -> 143) while the pace gets SLOWER — the clearest proof that an HR
	// step on its own says nothing about effort.
	lapsJul23Long = lapRows(
		329.66, 1000, 124, 335.0, 1000, 143, 341.09, 1000, 140, 354.86, 1000, 143,
		406.0, 1000, 151, 355.94, 1000, 151, 354.58, 1000, 146, 368.0, 1000, 146,
		439.2, 1000, 146, 316.33, 1000, 160, 516.27, 810.46, 157,
	)
)

// TestBestWindowRejectsWarmupDominatedWindow pins the bug: the 12% pace-spread
// gate catches reps+recovery but not warmup+rep, because it compares the two
// extreme laps and ignores how long each one lasted. On the real 2026-08-04
// session the warmup owned 71% of the winning window's clock and the headline
// fact understated the athlete at 5:08/km.
func TestBestWindowRejectsWarmupDominatedWindow(t *testing.T) {
	date := "2026-08-04T08:43:13Z"

	// The exact two-lap window the live trace picked, in isolation: 21:00 of
	// which 15:01 is warmup. It passes the spread gate (10.8%) and must still
	// be rejected.
	if e := bestWindow(lapsAug4Intervals[:2], 94, date); e != nil {
		t.Errorf("warmup+rep must not be reported as one sustained effort, got %.1f km in %s at %s/km",
			e.DistanceMeters/1000, formatRaceTime(int(e.DurationSeconds)), formatPacePerKm(e.PaceSecPerKm))
	}

	// Same window with no strap. The effort-step gate needs HR on both laps,
	// so here only the duration-weighted drift gate is left — and it must
	// still reject (the warmup owns 71% of the clock either way). Without
	// this the with-HR assertion above passes on the step gate alone and a
	// drift gate that quietly grew an HR dependency would go unnoticed.
	if e := bestWindow(stripHR(lapsAug4Intervals[:2]), 94, date); e != nil {
		t.Errorf("warmup+rep must be rejected without HR too (drift gate is HR-free), got %.1f km at %s/km",
			e.DistanceMeters/1000, formatPacePerKm(e.PaceSecPerKm))
	}

	// And the full session yields no sustained window at all: every longer
	// window spans a recovery jog.
	if e := bestWindow(lapsAug4Intervals, 94, date); e != nil {
		t.Errorf("interval session must not produce a sustained window, got %+v", e)
	}

	// The session's honest reading — and the anchor — is unmoved: it was
	// always work-derived, never the smeared window.
	ie := extractIntervalEffort(lapsAug4Intervals, 94, date)
	if ie == nil {
		t.Fatal("expected the 4x6min work cluster")
	}
	facts := &predictionFacts{
		AsOf: time.Now().UTC(),
		IntervalEfforts: []intervalEffort{{
			WorkoutID: 94, Date: time.Now().UTC().AddDate(0, 0, -12).Format(time.RFC3339),
			Reps: ie.Reps, TotalWorkSeconds: ie.TotalWorkSeconds,
			WorkPaceSecPerKm: ie.WorkPaceSecPerKm, AvgHeartRate: ie.AvgHeartRate,
		}},
		ThresholdHR: 163,
		MaxHR:       186,
	}
	anchor := deriveBaselineAnchor(facts)
	if anchor == nil || !anchor.WorkDerived {
		t.Fatalf("anchor must still come from the work laps, got %+v", anchor)
	}
	// 4x6min at ~4:49/km work pace, HR 158.75 vs threshold 163 → -3 s/km.
	if anchor.ThresholdPaceSecPerKm < 282 || anchor.ThresholdPaceSecPerKm > 290 {
		t.Errorf("anchor pace %.1f moved; the fix must only change the reported effort",
			anchor.ThresholdPaceSecPerKm)
	}
}

// TestBestWindowKeepsEasyRunsWithHRDrift is the guard the bead demanded: the
// warmup gates must not be an HR gate in disguise. Easy runs drift 20-30 bpm
// end to end and step 15-19 bpm across their opening kilometres at unchanged
// pace, so any range gate — and any bare HR-step gate — deletes them. Each
// window here is the one production reported before the fix.
func TestBestWindowKeepsEasyRunsWithHRDrift(t *testing.T) {
	cases := []struct {
		name          string
		laps          []predictionLap
		wantPace      float64 // s/km, as reported before the fix
		wantDurationS float64
	}{
		{"2026-04-12 easy (HR 136-157)", lapsApr12Easy, 337.6, 1350.56},
		{"2026-05-16 easy (HR 121-151, +15 bpm opening step)", lapsMay16Easy, 339.4, 1357.64},
		{"2026-07-23 long (HR 124-160, +19 bpm opening step)", lapsJul23Long, 340.2, 1360.61},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := bestWindow(c.laps, 1, "2026-01-01T00:00:00Z")
			if e == nil {
				t.Fatal("honest easy-run window deleted by the warmup gates")
			}
			if math.Abs(e.PaceSecPerKm-c.wantPace) > 0.5 {
				t.Errorf("pace = %.1f s/km, want %.1f (unchanged by the fix)", e.PaceSecPerKm, c.wantPace)
			}
			if math.Abs(e.DurationSeconds-c.wantDurationS) > 0.5 {
				t.Errorf("duration = %.2f s, want %.2f (unchanged by the fix)", e.DurationSeconds, c.wantDurationS)
			}

			// Same run without a strap: the drift gate is HR-free, so the
			// window must survive identically.
			if noHR := bestWindow(stripHR(c.laps), 1, "2026-01-01T00:00:00Z"); noHR == nil {
				t.Error("HR-less workout became unselectable")
			} else if math.Abs(noHR.PaceSecPerKm-e.PaceSecPerKm) > 0.01 {
				t.Errorf("HR-less pace %.1f differs from %.1f", noHR.PaceSecPerKm, e.PaceSecPerKm)
			}
		})
	}
}

// TestBestWindowRejectsBalancedEffortStep covers the case the duration-weighted
// gate cannot see: a warmup and a rep of EQUAL length. The window average then
// sits close enough to the fastest lap to clear maxWindowPaceDrift, and only
// the simultaneous pace-and-HR step gives the gear change away.
func TestBestWindowRejectsBalancedEffortStep(t *testing.T) {
	// 10:00 at 5:17/km HR 144, then 10:00 at 4:46/km HR 158. Spread 10.8%
	// (inside the 12% gate), window pace 5:01/km — 5.1% off the fastest lap,
	// inside the drift gate.
	stepped := lapRows(
		600.0, 600.0/317.0*1000, 144,
		600.0, 600.0/286.0*1000, 158,
	)
	if e := bestWindow(stepped, 1, "2026-08-04T08:43:13Z"); e != nil {
		t.Errorf("a 14 bpm step onto a 10.8%% faster pace is two efforts, got %+v", e)
	}

	// Change gear on pace alone with no HR recorded and there is nothing left
	// to distinguish it from an honest progression run — that window stands,
	// by design. Documented here so the behaviour is deliberate, not a gap
	// someone "fixes" with a range gate.
	if e := bestWindow(stripHR(stepped), 1, "2026-08-04T08:43:13Z"); e == nil {
		t.Error("HR-less balanced window must still be selectable")
	}
}

// TestBestWindowSeesStepAcrossHRDropout pins that the step baseline survives a
// lap the strap dropped out on. Carrying prevPace/prevHR unconditionally used to
// zero prevHR there, so the very next lap failed its prevHR > 0 guard and a gear
// change straight after a dropout went undetected.
func TestBestWindowSeesStepAcrossHRDropout(t *testing.T) {
	// 10:00 warmup at 5:17/km HR 144, a 2:00 lap at the same pace with no HR
	// sample, then 10:00 at 4:46/km HR 158. Spread is 10.8% and the window pace
	// lands 5.6% off the fastest lap, so both other gates pass — only the step
	// test, comparing the rep against the warmup across the dropout, rejects it.
	laps := lapRows(
		600.0, 600.0/317.0*1000, 144,
		120.0, 120.0/317.0*1000, 0,
		600.0, 600.0/286.0*1000, 158,
	)
	if e := bestWindow(laps, 1, "2026-06-04T10:00:00Z"); e != nil {
		t.Errorf("a gear change after an HR-dropout lap is still two efforts, got %+v", e)
	}
}

// TestBestWindowRecoversFromDriftAtLongerWindow pins why the drift gate uses
// continue and not break, unlike the other two gates in the same loop: drift is
// duration-weighted, so extending a window can ADD fast laps and pull the
// average back toward its fastest lap. Break here would silently drop valid
// efforts, and a "consistency" cleanup would look harmless without this test.
func TestBestWindowRecoversFromDriftAtLongerWindow(t *testing.T) {
	// 15:00 at 5:30/km then 2 x 5:00 at 5:00/km, HR stepping only 7 bpm (under
	// the 8 bpm gate, so the effort-step gate stays out of it).
	//   slow+fast1 = 20:00, pace 5:22/km — 7.3% off the 5:00 lap, drift rejects.
	//   slow+fast2 = 25:00, pace 5:17/km — 5.8% off, drift accepts.
	laps := lapRows(
		900.0, 900.0/330.0*1000, 150,
		300.0, 1000, 157,
		300.0, 1000, 157,
	)
	e := bestWindow(laps, 1, "2026-06-01T10:00:00Z")
	if e == nil {
		t.Fatal("the 25-min window comes back under the drift gate and must be selected")
	}
	if math.Abs(e.DurationSeconds-1500) > 0.5 {
		t.Errorf("duration = %.1f s, want the longer 1500 s window", e.DurationSeconds)
	}
	if math.Abs(e.PaceSecPerKm-317.3) > 1 {
		t.Errorf("pace = %.1f s/km, want ~317.3", e.PaceSecPerKm)
	}
}

// TestBestWindowAcceptsTempoAfterGearStep pins the effort-step latch's scope:
// it is per window START, so a gear change early in the session must not poison
// windows that begin after it. That is the everyday warmup-then-tempo shape,
// where the window worth reporting is the tempo itself.
func TestBestWindowAcceptsTempoAfterGearStep(t *testing.T) {
	// 10:00 warmup at 5:30/km HR 140, then 4 x 5:50 at 5:00/km HR 155. The
	// step (+15 bpm onto a 10% faster pace) kills every window starting at the
	// warmup; the 23:20 tempo starting at lap 1 is clean.
	laps := lapRows(
		600.0, 600.0/330.0*1000, 140,
		350.0, 350.0/300.0*1000, 155,
		350.0, 350.0/300.0*1000, 155,
		350.0, 350.0/300.0*1000, 155,
		350.0, 350.0/300.0*1000, 155,
	)
	e := bestWindow(laps, 1, "2026-06-02T10:00:00Z")
	if e == nil {
		t.Fatal("the steady tempo after the gear step must still be selectable")
	}
	if math.Abs(e.DurationSeconds-1400) > 0.5 {
		t.Errorf("duration = %.1f s, want the 1400 s tempo window (start at lap 1)", e.DurationSeconds)
	}
	if math.Abs(e.PaceSecPerKm-300) > 0.5 {
		t.Errorf("pace = %.1f s/km, want the tempo's 300 (not a warmup blend)", e.PaceSecPerKm)
	}
}

// TestBestWindowStepBaselineSkipsMicroLaps pins the documented micro-lap
// skipping: a stray button press is excluded from the spread, and it must be
// excluded from the effort-step baseline too, or its junk pace/HR fakes a gear
// change against the next real lap.
func TestBestWindowStepBaselineSkipsMicroLaps(t *testing.T) {
	// Two 11:00 laps of identical effort (5:00/km, HR 150) with a 10 s junk lap
	// between them reading 6:40/km at HR 138 — against that neighbour the next
	// real lap looks like +12 bpm onto a 33% faster pace.
	laps := lapRows(
		660.0, 660.0/300.0*1000, 150,
		10.0, 25.0, 138,
		660.0, 660.0/300.0*1000, 150,
	)
	e := bestWindow(laps, 1, "2026-06-03T10:00:00Z")
	if e == nil {
		t.Fatal("a 10 s junk lap must not read as a gear change between two identical laps")
	}
	if math.Abs(e.DurationSeconds-1330) > 0.5 {
		t.Errorf("duration = %.1f s, want the full 1330 s window", e.DurationSeconds)
	}
}

// TestDeriveBaselineAnchorPrefersRecency pins the second coach finding: a
// faster effort from four months ago must not outrank current evidence.
func TestDeriveBaselineAnchorPrefersRecency(t *testing.T) {
	now := time.Now().UTC()
	old := now.AddDate(0, -4, 0).Format(time.RFC3339) // outside 84d
	recent := now.AddDate(0, 0, -7).Format(time.RFC3339)

	facts := &predictionFacts{
		AsOf: now,
		BestEfforts: []sustainedEffort{
			{Date: old, DurationSeconds: 1335, DistanceMeters: 4618, PaceSecPerKm: 289}, // April: faster
			{Date: recent, DurationSeconds: 1800, DistanceMeters: 5960, PaceSecPerKm: 302},
		},
	}
	a := deriveBaselineAnchor(facts)
	if a == nil {
		t.Fatal("expected an anchor")
	}
	if a.Stale {
		t.Error("recent evidence exists; anchor must not be stale")
	}
	if a.ThresholdPaceSecPerKm != 302 {
		t.Errorf("recency must beat magnitude: want the recent 302 s/km anchor, got %.0f", a.ThresholdPaceSecPerKm)
	}

	// With only the old effort, it anchors but is flagged stale.
	facts.BestEfforts = facts.BestEfforts[:1]
	a = deriveBaselineAnchor(facts)
	if a == nil || !a.Stale {
		t.Fatalf("old-only evidence must anchor as stale, got %+v", a)
	}
}

// TestBaselinePredictionsUsesHourAnchor: the Riegel reference is a 60-minute
// effort at threshold pace, so a half-marathon extrapolation stays ~1.6x
// instead of stretching a 20-minute window 4-5x.
func TestBaselinePredictionsUsesHourAnchor(t *testing.T) {
	a := &baselineAnchor{ThresholdPaceSecPerKm: 290} // 4:50/km
	base := baselinePredictions(a)
	hm := base["Half Marathon"]
	// 60 min at 4:50/km covers 12.41 km; Riegel to 21.0975 km ≈ 6362 s
	// (~1:46). Sanity band:
	if hm < 6200 || hm > 6600 {
		t.Errorf("HM baseline out of band: %.0f s", hm)
	}
	// The half prediction must be SLOWER per km than threshold pace — a race
	// nearly twice the anchor duration cannot be at threshold pace.
	if hm/(21.0975) <= 290 {
		t.Errorf("HM pace %.1f s/km should be slower than the 290 s/km threshold anchor", hm/21.0975)
	}
}

// TestFormulaConfidencesNeverHigh: the deterministic path cannot verify a
// maximal effort, so it never grades high — and durability gates the half.
func TestFormulaConfidencesNeverHigh(t *testing.T) {
	facts := &predictionFacts{
		LongestRecent: &longestRun{DurationSeconds: 3600}, // 60 min longest run
	}
	conf := formulaConfidences(facts, &baselineAnchor{})
	for d, c := range conf {
		if c == "high" {
			t.Errorf("formula confidence for %s must never be high", d)
		}
	}
	if conf["Half Marathon"] != "low" {
		t.Errorf("60-min longest run should gate HM to low, got %s", conf["Half Marathon"])
	}
	if conf["Marathon"] != "low" {
		t.Errorf("marathon must be low from the formula, got %s", conf["Marathon"])
	}

	facts.LongestRecent.DurationSeconds = 5400 // 90 min
	conf = formulaConfidences(facts, &baselineAnchor{})
	if conf["Half Marathon"] != "medium" {
		t.Errorf("90-min longest run supports medium HM, got %s", conf["Half Marathon"])
	}

	conf = formulaConfidences(facts, &baselineAnchor{Stale: true})
	if conf["5K"] != "low" {
		t.Errorf("stale anchor must drop everything to low, got %s", conf["5K"])
	}
}

// TestIntervalThresholdAdjustment pins the HR gate: reps at/above threshold
// HR earn the continuous-effort penalty, reps right under it are threshold
// pace as-is, clearly sub-threshold reps correct the other way, and missing
// HR context keeps the conservative default.
func TestIntervalThresholdAdjustment(t *testing.T) {
	cases := []struct {
		workHR, thresholdHR int
		want                float64
	}{
		{165, 163, 6},  // above threshold
		{163, 163, 6},  // at threshold
		{161, 163, 0},  // just under
		{158, 163, -3}, // clearly under — athlete had more
		{0, 163, 6},    // no work HR
		{158, 0, 6},    // no threshold HR
	}
	for _, c := range cases {
		if got := intervalThresholdAdjustment(c.workHR, c.thresholdHR); got != c.want {
			t.Errorf("adjustment(%d,%d) = %v, want %v", c.workHR, c.thresholdHR, got, c.want)
		}
	}
}

// lapRows builds laps from (durS, distM, hr) triples — the shape the lap rows
// come out of the DB in, so real sessions can be pasted straight into a test.
// For indoor sessions the distances are the watch's cadence-derived values,
// deliberately kept so those tests prove the indoor path ignores them.
func lapRows(triples ...float64) []predictionLap {
	laps := make([]predictionLap, 0, len(triples)/3)
	for i := 0; i+2 < len(triples); i += 3 {
		laps = append(laps, predictionLap{durS: triples[i], distM: triples[i+1], hr: int(triples[i+2])})
	}
	return laps
}

// TestExtractIndoorIntervalEffortKeepsSessionPaceClusteringDiscarded pins the
// exact regression: workout 100 (2026-08-14, 3x10min at belt 11.8-12.0 with
// 2min jogs). Pace-keyed clustering threw this session away entirely because
// the belt-9.8 WARMUP read 314 s/km while the threshold reps read 340-350 —
// indoor pace ordering is inverted, so the 8% band anchored on the warmup and
// excluded lap 2 by 0.6 s/km, leaving 2 laps and failing the rep floor.
func TestExtractIndoorIntervalEffortKeepsSessionPaceClusteringDiscarded(t *testing.T) {
	laps := lapRows(
		899.1, 2861.15, 140, // warmup — FASTEST by watch pace (314 s/km)
		600.0, 1762.37, 158, // rep 1 (340 s/km — "slower" than the warmup)
		120.0, 346.72, 149, // jog
		600.0, 1711.96, 160, // rep 2
		120.0, 346.71, 152, // jog
		600.0, 1726.36, 163, // rep 3
		120.0, 340.54, 153, // jog — on the HR floor, excluded by duration
		542.06, 1624.19, 143, // cooldown
	)
	got := extractIndoorIntervalEffort(laps, 100, "2026-08-14T10:30:27Z", indoorWorkHRFloor(163, 186))
	if got == nil {
		t.Fatal("threshold session discarded; this is the bug")
	}
	if got.Reps != 3 {
		t.Errorf("Reps = %d, want 3 (warmup, jogs and cooldown must not count)", got.Reps)
	}
	if got.TotalWorkSeconds != 1800 {
		t.Errorf("TotalWorkSeconds = %v, want 1800", got.TotalWorkSeconds)
	}
	if got.AvgHeartRate != 160 {
		t.Errorf("AvgHeartRate = %d, want 160 (158/160/163)", got.AvgHeartRate)
	}
	if got.WorkPaceSecPerKm != 0 {
		t.Errorf("WorkPaceSecPerKm = %v, want 0: indoor pace must never be reported", got.WorkPaceSecPerKm)
	}
}

// TestExtractIndoorIntervalEffortRejectsEasyRuns pins the other half of the
// inversion: easy treadmill runs auto-lap every 1 km, so their paces are
// naturally even and pace clustering reported them as interval work — an easy
// run became "8 work reps, 45:01, HR 140" and an easy+strides run became
// "5 work reps, 28:40, HR 132". HR keying rejects both.
func TestExtractIndoorIntervalEffortRejectsEasyRuns(t *testing.T) {
	floor := indoorWorkHRFloor(163, 186)

	easy := lapRows( // workout 99, 2026-08-12
		340.68, 1000, 125, 325.33, 1000, 138, 340.67, 1000, 138, 335.66, 1000, 140,
		341.0, 1000, 142, 340.74, 1000, 144, 338.93, 1000, 144, 338.33, 1000, 146,
	)
	if got := extractIndoorIntervalEffort(easy, 99, "2026-08-12T17:46:13Z", floor); got != nil {
		t.Errorf("easy treadmill run reported as interval work: %+v", got)
	}

	strides := lapRows( // workout 101, 2026-08-15
		369.04, 1000, 118, 342.66, 1000, 129, 356.52, 1000, 131,
		360.26, 1000, 135, 380.26, 1000, 133, 291.96, 820, 149,
	)
	if got := extractIndoorIntervalEffort(strides, 101, "2026-08-15T11:27:11Z", floor); got != nil {
		t.Errorf("easy+strides run reported as interval work: %+v", got)
	}

	long := lapRows( // workout 102, 2026-08-16 progression long run
		4200.5, 11600.47, 141, 719.09, 2003.85, 151, 183.22, 505.4, 143,
	)
	if got := extractIndoorIntervalEffort(long, 102, "2026-08-16T06:58:06Z", floor); got != nil {
		t.Errorf("long run reported as interval work: %+v", got)
	}
}

func TestIndoorWorkHRFloor(t *testing.T) {
	if got := indoorWorkHRFloor(163, 186); got != 153 {
		t.Errorf("threshold path = %d, want 153", got)
	}
	if got := indoorWorkHRFloor(0, 186); got != 164 {
		t.Errorf("maxHR fallback = %d, want 164", got)
	}
	if got := indoorWorkHRFloor(0, 0); got != 0 {
		t.Errorf("no anchor = %d, want 0 (report nothing rather than guess)", got)
	}
}

// TestRegressionAug4OutdoorSession pins the predictor against the athlete's
// real 2026-08-04 outdoor session — the most trustworthy input in the system:
// 4x6min at 12.25-12.59 km/h (286-294 s/km) at HR 157/158/158/162 vs
// threshold HR 163, with a slow warmup and 1-min recovery jogs. The half
// baseline from this must land in the coach-reviewed 1:42-1:48 band, and the
// envelope must make anything at or under 1:40 impossible by construction —
// every predictor bug so far shipped while the generic tests passed, so this
// test is built from the actual lap rows.
func TestRegressionAug4OutdoorSession(t *testing.T) {
	// (durS, paceSecPerKm) pairs: warmup, 4 work reps with 60s jogs between,
	// cooldown. Jogs are under the 120s work-lap floor; the warmup pace sits
	// far outside the 8% band of the fastest rep.
	laps := []predictionLap{
		{durS: 900, distM: 900 / 360.0 * 1000, hr: 135}, // 15min warmup ~6:00/km
		{durS: 360, distM: 360 / 294.0 * 1000, hr: 157}, // rep 1: 4:54/km
		{durS: 60, distM: 60 / 400.0 * 1000, hr: 150},   // jog
		{durS: 360, distM: 360 / 292.0 * 1000, hr: 158}, // rep 2
		{durS: 60, distM: 60 / 400.0 * 1000, hr: 151},   // jog
		{durS: 360, distM: 360 / 290.0 * 1000, hr: 158}, // rep 3
		{durS: 60, distM: 60 / 400.0 * 1000, hr: 152},   // jog
		{durS: 360, distM: 360 / 286.0 * 1000, hr: 162}, // rep 4: 4:46/km
		{durS: 600, distM: 600 / 380.0 * 1000, hr: 140}, // cooldown
	}
	date := time.Now().UTC().AddDate(0, 0, -12).Format(time.RFC3339)

	// The sustained-window path must reject this session (mixed paces)...
	if e := bestWindow(laps, 94, date); e != nil {
		t.Fatalf("interval session must not produce a sustained window, got %+v", e)
	}
	// ...and the work-lap cluster must read only the reps.
	ie := extractIntervalEffort(laps, 94, date)
	if ie == nil {
		t.Fatal("expected the 4x6min work cluster")
	}
	if ie.Reps != 4 || ie.WorkPaceSecPerKm < 286 || ie.WorkPaceSecPerKm > 294 {
		t.Fatalf("work cluster wrong: %+v", ie)
	}

	facts := &predictionFacts{
		AsOf:            time.Now().UTC(),
		IntervalEfforts: []intervalEffort{*ie},
		ThresholdHR:     163,
		MaxHR:           186,
	}
	anchor := deriveBaselineAnchor(facts)
	if anchor == nil || anchor.Stale {
		t.Fatalf("expected a fresh anchor, got %+v", anchor)
	}
	base := baselinePredictions(anchor)
	hm := base["Half Marathon"]
	// Coach-reviewed band for this data: ~1:42-1:48.
	if hm < 6120 || hm > 6480 {
		t.Errorf("HM baseline %s outside the 1:42-1:48 band", formatRaceTime(int(hm)))
	}

	// The asymmetric envelope makes sub-1:40 impossible by construction:
	// even a maximally optimistic AI output is clamped to baseline - 3%.
	clamped := clampToEnvelope(5940 /* 1:39:00 */, hm)
	if float64(clamped) < hm*(1-predictionEnvelopeFastPct)-1 {
		t.Errorf("clamp floor violated: %d vs baseline %d", clamped, int(hm))
	}
	if clamped <= 6000 {
		t.Errorf("1:40 or faster must be unreachable, clamp gave %s", formatRaceTime(clamped))
	}
}

// TestTreadmillSpeedFactor pins the calibration-factor parsing and its
// conservative 1.0 default.
func TestTreadmillSpeedFactor(t *testing.T) {
	cases := []struct {
		text string
		want float64
	}{
		{"belt runs slow: x1.03 vs outdoor", 1.03},
		{"measured factor 1.05 on the new belt", 1.05},
		{"belt reads about 3% slow", 1.03},
		{"", 1.0},
		{"no numbers here", 1.0},
		{"x1.90 nonsense out of bounds", 1.0},
	}
	for _, c := range cases {
		if got := treadmillSpeedFactor(c.text); math.Abs(got-c.want) > 0.001 {
			t.Errorf("factor(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestIndoorBeltConversionAnchorsBaseline mirrors the athlete's real W98
// session (5x6min at belt 12.0, work HR ~159 vs threshold 163) with a
// measured 1.03 factor: the converted effort must anchor the baseline and put
// the half in the coach-reviewed band — this is what makes a mostly-indoor
// athlete's predictor see more than one workout.
func TestIndoorBeltConversionAnchorsBaseline(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -4).Format(time.RFC3339)
	facts := &predictionFacts{
		AsOf:        time.Now().UTC(),
		ThresholdHR: 163,
		MaxHR:       186,
		IndoorIntervals: []intervalEffort{{
			WorkoutID: 98, Date: recent, Reps: 5, TotalWorkSeconds: 1800, AvgHeartRate: 159,
		}},
		TreadmillFactor: 1.03,
	}
	// Simulate convertIndoorEfforts having found belt 12.0 via the speed plan.
	facts.IndoorIntervals[0].BeltKmh = 12.0
	facts.IndoorIntervals[0].WorkPaceSecPerKm = 3600.0 / (12.0 * 1.03)
	facts.IndoorIntervals[0].Converted = true

	anchor := deriveBaselineAnchor(facts)
	if anchor == nil || anchor.Stale {
		t.Fatalf("converted indoor effort must anchor, got %+v", anchor)
	}
	// 3600/12.36 = 291.3 s/km, HR gap 4 → -3 → ~288.3 s/km.
	if anchor.ThresholdPaceSecPerKm < 286 || anchor.ThresholdPaceSecPerKm > 291 {
		t.Errorf("anchor pace %.1f outside expected 286-291", anchor.ThresholdPaceSecPerKm)
	}
	hm := baselinePredictions(anchor)["Half Marathon"]
	// Coach-reviewed band ~1:43-1:46 for this evidence.
	if hm < 6180 || hm > 6420 {
		t.Errorf("HM baseline %s outside 1:43-1:47 band", formatRaceTime(int(hm)))
	}

	// An UNCONVERTED indoor effort (no recorded belt speed) must never anchor.
	facts.IndoorIntervals[0].Converted = false
	facts.IndoorIntervals[0].WorkPaceSecPerKm = 200 // poisoned watch pace
	if a := deriveBaselineAnchor(facts); a != nil {
		t.Errorf("unconverted indoor effort must not anchor, got %+v", a)
	}
}

// TestIndoorBeltWorkSpeed pins the structured speed-plan reading: interval
// segments only, time-weighted, repeats expanded.
func TestIndoorBeltWorkSpeed(t *testing.T) {
	plan := []SpeedSegment{
		{Kind: "warmup", SpeedKmph: 9.8, DurationSec: 900},
		{Kind: "interval", SpeedKmph: 11.8, DurationSec: 600, Repeats: 2},
		{Kind: "pause", SpeedKmph: 5.0, DurationSec: 60},
		{Kind: "interval", SpeedKmph: 12.0, DurationSec: 600, Repeats: 1},
	}
	got := indoorBeltWorkSpeed(plan)
	want := (11.8*1200 + 12.0*600) / 1800
	if math.Abs(got-want) > 0.001 {
		t.Errorf("belt speed %v, want %v (warmup and pause must not count)", got, want)
	}
	if indoorBeltWorkSpeed([]SpeedSegment{{Kind: "warmup", SpeedKmph: 10, DurationSec: 600}}) != 0 {
		t.Error("plan without interval segments must yield 0")
	}
}

// TestFeelNotesBeltWorkSpeed pins the free-text belt-speed parsing against
// the athlete's real note formats — including the "25 x 45x15" trap where
// rep-structure numbers must not read as speeds.
func TestFeelNotesBeltWorkSpeed(t *testing.T) {
	cases := []struct {
		notes string
		want  float64 // 0 = no speed extracted
	}{
		{"4x6 @ 12 - 12 - 12 - 11.8", (12 + 12 + 12 + 11.8) / 4},
		{"12.4-12.5-12.6-12.6", (12.4 + 12.5 + 12.6 + 12.6) / 4},
		{"speed 13.3 13.4 13.5 13.3 for the intervals", (13.3 + 13.4 + 13.5 + 13.3) / 4},
		{"flat 10.3 speed", 10.3},
		{"30m@10kmph", 10},
		// Warmup speed listed alongside work: only values within 1 km/h of
		// the max count, so 9.8 must not drag the average down.
		{"warmup 9.8 then 12.0 12.0 11.8", (12.0 + 12.0 + 11.8) / 3},
		// Rep structure is not speed: 25, 45 and 15 must all be ignored
		// (45 out of range, 25/15 glued to 'x'), leaving 11.1.
		{"25 x 45x15\n11min warmiup, starting at 11.1kmph and increasing 0.1 each interval", 11.1},
		{"", 0},
		{"felt great, easy legs", 0},
	}
	for _, c := range cases {
		got := feelNotesBeltWorkSpeed(c.notes)
		if math.Abs(got-c.want) > 0.01 {
			t.Errorf("feelNotes(%q) = %v, want %v", c.notes, got, c.want)
		}
	}
}

// TestTreadmillBeltValidSinceCutoff pins the belt-swap gate: efforts dated
// before the calibration's valid-since date must never convert.
func TestTreadmillBeltValidSinceCutoff(t *testing.T) {
	if got := treadmillBeltValidSince("Belt swapped 2026-05-29; factor 1.03"); got != "2026-05-29" {
		t.Fatalf("valid-since parse: %q", got)
	}
	if got := treadmillBeltValidSince("factor 1.03 only"); got != "" {
		t.Fatalf("expected no date, got %q", got)
	}

	db := setupPredictionContextDB(t)
	efforts := []intervalEffort{
		{WorkoutID: 901, Date: "2026-05-20T10:00:00Z", Reps: 4, TotalWorkSeconds: 1440, AvgHeartRate: 160},
		{WorkoutID: 902, Date: "2026-08-12T10:00:00Z", Reps: 5, TotalWorkSeconds: 1800, AvgHeartRate: 159},
	}
	convertIndoorEfforts(db, 1, efforts, 1.03, "2026-05-29")
	if efforts[0].Converted {
		t.Error("pre-swap effort must not convert")
	}
	if !efforts[1].Converted {
		t.Fatal("post-swap effort with a feel-note speed must convert")
	}
	want := 3600.0 / (11.95 * 1.03)
	if math.Abs(efforts[1].WorkPaceSecPerKm-want) > 0.5 {
		t.Errorf("converted pace %.1f, want ~%.1f", efforts[1].WorkPaceSecPerKm, want)
	}
}

// setupPredictionContextDB builds a minimal DB with workout_context rows
// carrying feel-note belt speeds for both test workouts.
func setupPredictionContextDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	for _, w := range []struct {
		id    int64
		notes string
	}{
		{901, "4x6 @ 13.3 - 13.4 - 13.5 - 13.3"}, // old belt speeds
		{902, "4x6 @ 12 - 12 - 12 - 11.8"},
	} {
		enc, err := encryption.EncryptField(w.notes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO workouts (id, user_id, sport, duration_seconds, distance_meters, started_at, title, fit_file_hash)
			VALUES (?, 1, 'running', 3600, 10000, '2026-08-01T10:00:00Z', 'w', 'h'||?)`, w.id, w.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO workout_context (workout_id, surface, run_type, hr_source, feel_notes, speed_plan, completed_at)
			VALUES (?, 'treadmill', 'intervals', 'strap', ?, '', '2026-08-01T11:00:00Z')`, w.id, enc); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// vo2maxEstimatesFrom builds a chronological run of estimates, one per day from
// 2026-06-01, for the summary tests.
func vo2maxEstimatesFrom(vals ...float64) []VO2maxEstimate {
	out := make([]VO2maxEstimate, 0, len(vals))
	day := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	for i, v := range vals {
		out = append(out, VO2maxEstimate{
			VO2max:      v,
			EstimatedAt: day.AddDate(0, 0, i).Format(time.RFC3339),
		})
	}
	return out
}

// TestFormatVO2maxSummaryReportsMedianNotTrend pins the fix: a scattered run of
// per-workout estimates must never reach the model as a "history" sequence it
// can read a slope out of, nor let the latest single value stand alone.
func TestFormatVO2maxSummaryReportsMedianNotTrend(t *testing.T) {
	// The real-world run from the bug report, in chronological order: the bug
	// report printed it newest-first, so the values are reversed here and 43.3
	// — the last argument, and so the newest date — is the latest estimate.
	out := formatVO2maxSummary(
		vo2maxEstimatesFrom(42.8, 37.2, 56.2, 43.1, 38.9, 49.1, 43.3), 43.3)

	for _, want := range []string{
		"n=7 over 2026-06-01 to 2026-06-07",
		"median 43.1",
		"range 37.2-56.2",
		"19.0-unit spread is estimator noise",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"newest first", "history", "(latest)"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("summary must not mention %q:\n%s", unwanted, out)
		}
	}
	// The raw sequence must be gone: no comma-joined value list.
	if strings.Contains(out, "43.3, 49.1") || strings.Contains(out, "42.8, 37.2") {
		t.Errorf("raw estimate sequence still printed:\n%s", out)
	}
}

// TestFormatVO2maxSummaryTrustsTightCluster keeps the summary useful when the
// estimator actually agrees with itself — that is signal, not noise.
func TestFormatVO2maxSummaryTrustsTightCluster(t *testing.T) {
	out := formatVO2maxSummary(vo2maxEstimatesFrom(48.6, 49.2, 48.9, 49.4, 49.0), 49.0)
	if !strings.Contains(out, "median 49.0") {
		t.Errorf("want median 49.0:\n%s", out)
	}
	if !strings.Contains(out, "cluster tightly") {
		t.Errorf("a 0.8-unit spread must not be called noise:\n%s", out)
	}
}

// TestFormatVO2maxSummaryEvenCountMedian pins the even-length median: with no
// middle element the two central values must be averaged. History length is
// whatever GetVO2maxHistory returns, so even counts are as common as odd.
func TestFormatVO2maxSummaryEvenCountMedian(t *testing.T) {
	out := formatVO2maxSummary(vo2maxEstimatesFrom(40, 42, 44, 46), 46)
	for _, want := range []string{"n=4", "median 43.0", "range 40.0-46.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

// TestFormatVO2maxSummarySkipsNonPositive covers the zero-value filtering: a
// missing estimate stored as 0 must not drag the median down or widen the
// range, and a history that is entirely nonpositive must fall through to the
// single-estimate fallback rather than reporting an empty run.
func TestFormatVO2maxSummarySkipsNonPositive(t *testing.T) {
	out := formatVO2maxSummary(vo2maxEstimatesFrom(0, 48.6, 49.2, 48.9), 48.9)
	for _, want := range []string{
		"n=3 over 2026-06-02 to 2026-06-04",
		"median 48.9",
		"range 48.6-49.2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("zero estimate not excluded, missing %q:\n%s", want, out)
		}
	}

	allZero := formatVO2maxSummary(vo2maxEstimatesFrom(0), 44.2)
	if !strings.Contains(allZero, "single per-workout estimate of 44.2") {
		t.Errorf("all-nonpositive history must use the latest-only fallback: %q", allZero)
	}
}

// TestFormatVO2maxSummaryEdgeCases covers no data and the history-query-failed
// fallback where only a latest value survives.
func TestFormatVO2maxSummaryEdgeCases(t *testing.T) {
	if got := formatVO2maxSummary(nil, 0); got != "" {
		t.Errorf("no data must render nothing, got %q", got)
	}
	if got := formatVO2maxSummary(nil, 44.2); !strings.Contains(got, "single per-workout estimate of 44.2") {
		t.Errorf("latest-only fallback: %q", got)
	}
	single := formatVO2maxSummary(vo2maxEstimatesFrom(44.2), 44.2)
	if !strings.Contains(single, "n=1 on 2026-06-01") || !strings.Contains(single, "weak evidence") {
		t.Errorf("single estimate: %q", single)
	}
}
