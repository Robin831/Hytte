package training

import (
	"testing"
	"time"
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

// indoorLaps builds indoor laps from (durS, distM, hr) triples. Distances are
// the watch's real cadence-derived values, deliberately kept so these tests
// prove the indoor path ignores them.
func indoorLaps(triples ...float64) []predictionLap {
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
	laps := indoorLaps(
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

	easy := indoorLaps( // workout 99, 2026-08-12
		340.68, 1000, 125, 325.33, 1000, 138, 340.67, 1000, 138, 335.66, 1000, 140,
		341.0, 1000, 142, 340.74, 1000, 144, 338.93, 1000, 144, 338.33, 1000, 146,
	)
	if got := extractIndoorIntervalEffort(easy, 99, "2026-08-12T17:46:13Z", floor); got != nil {
		t.Errorf("easy treadmill run reported as interval work: %+v", got)
	}

	strides := indoorLaps( // workout 101, 2026-08-15
		369.04, 1000, 118, 342.66, 1000, 129, 356.52, 1000, 131,
		360.26, 1000, 135, 380.26, 1000, 133, 291.96, 820, 149,
	)
	if got := extractIndoorIntervalEffort(strides, 101, "2026-08-15T11:27:11Z", floor); got != nil {
		t.Errorf("easy+strides run reported as interval work: %+v", got)
	}

	long := indoorLaps( // workout 102, 2026-08-16 progression long run
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
