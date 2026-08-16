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
