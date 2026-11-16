package stride

import (
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/training"
)

func TestTraceBucketSec(t *testing.T) {
	cases := map[int64]int64{600: 10, 3191: 10, 3600: 10, 3700: 15, 7200: 20, 3 * 3600: 30}
	for dur, want := range cases {
		if got := traceBucketSec(dur); got != want {
			t.Errorf("traceBucketSec(%d) = %d, want %d", dur, got, want)
		}
	}
}

func TestWriteSampleTrace(t *testing.T) {
	// 40 s at 1 Hz: HR 150 for 20 s, then a spike to 164 for 5 s inside the
	// second lap, speed 3.333 m/s (5:00 /km).
	var pts []training.Sample
	for s := int64(0); s < 40; s++ {
		hr := 150
		if s >= 22 && s < 27 {
			hr = 164
		}
		pts = append(pts, training.Sample{OffsetMs: s * 1000, HeartRate: hr, SpeedMPerS: 1000.0 / 300})
	}
	laps := []training.Lap{{LapNumber: 1, StartOffsetMs: 0}, {LapNumber: 2, StartOffsetMs: 20000}}
	var sb strings.Builder
	writeSampleTrace(&sb, &training.Samples{Points: pts}, laps)
	out := sb.String()

	for _, want := range []string{
		"## Heart Rate & Pace Trace",
		"10-second windows",
		"-- lap 1 starts 0:00 --\n0:00 150/150 5:00\n",
		"-- lap 2 starts 0:20 --\n0:20 157/164 5:00\n",
		"0:30 150/150 5:00\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trace missing %q:\n%s", want, out)
		}
	}

	// No heart rate at all: nothing to show.
	sb.Reset()
	writeSampleTrace(&sb, &training.Samples{Points: []training.Sample{{OffsetMs: 0, SpeedMPerS: 3}}}, nil)
	if sb.Len() != 0 {
		t.Errorf("trace without HR = %q", sb.String())
	}
}

func TestEvalPromptIncludesTrace(t *testing.T) {
	w := training.Workout{
		ID: 1, Sport: "running", DurationSeconds: 30, AvgHeartRate: 150,
		Samples: &training.Samples{Points: []training.Sample{{OffsetMs: 0, HeartRate: 150}, {OffsetMs: 1000, HeartRate: 151}}},
	}
	prompt := buildEvalPrompt(w, nil, Plan{}, training.UserTrainingProfile{}, nil, "")
	if !strings.Contains(prompt, "## Heart Rate & Pace Trace") || !strings.Contains(prompt, "0:00 151/151") {
		t.Errorf("eval prompt has no trace:\n%s", prompt)
	}
}
