package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Robin831/Hytte/internal/training"
)

// The coach used to see only averages and per-lap peaks, so it couldn't tell
// a brief HR spike from a plateau ("did HR sit above 162 in rep 3?") even
// though the page draws the full graph. The trace hands it the same data,
// bucketed small enough to answer that and short enough for the prompt.

// traceMaxRows caps the rows: ~360 rows is a few thousand tokens. Buckets are
// at least 10 s, so a 1 h run is read at 10 s resolution and a 3 h one at 30 s.
const (
	traceMaxRows      = 360
	traceMinBucketSec = 10
)

// loadWorkoutSamples reads a workout's time series; nil when it has none.
func loadWorkoutSamples(ctx context.Context, db *sql.DB, workoutID int64) (*training.Samples, error) {
	var data string
	err := db.QueryRowContext(ctx, `SELECT data FROM workout_samples WHERE workout_id = ?`, workoutID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var points []training.Sample
	if err := json.Unmarshal([]byte(data), &points); err != nil {
		return nil, fmt.Errorf("unmarshal samples: %w", err)
	}
	return &training.Samples{Points: points}, nil
}

// traceBucketSec picks the bucket size for a run of the given length.
func traceBucketSec(durationSec int64) int64 {
	b := int64(traceMinBucketSec)
	for durationSec/b > traceMaxRows {
		b += 5
	}
	return b
}

func clock(sec int64) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec%3600/60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// writeSampleTrace writes the HR/pace trace, marking where each lap starts.
// Nothing is written when there's no heart-rate data.
func writeSampleTrace(sb *strings.Builder, samples *training.Samples, laps []training.Lap) {
	if samples == nil || len(samples.Points) == 0 {
		return
	}
	hasHR := false
	for _, p := range samples.Points {
		if p.HeartRate > 0 {
			hasHR = true
			break
		}
	}
	if !hasHR {
		return
	}
	pts := samples.Points
	bucket := traceBucketSec(pts[len(pts)-1].OffsetMs / 1000)

	fmt.Fprintf(sb, "## Heart Rate & Pace Trace\n")
	fmt.Fprintf(sb, "The full recording behind the HR graph the athlete sees, in %d-second windows: time from start, average/max HR in the window, and pace (min/km, from speed; \"—\" when standing or no speed). Lap starts are marked. Use it to answer questions about how HR or pace behaved within a rep — e.g. how long HR stayed above a cap, whether a peak was a brief spike or a plateau, or how quickly HR recovered — and quote times from it.\n\n", bucket)
	sb.WriteString("time  hr avg/max  pace\n")

	lapIdx := 0
	for start := 0; start < len(pts); {
		bStart := pts[start].OffsetMs / 1000 / bucket * bucket
		end := start
		hrSum, hrN, hrMax := 0, 0, 0
		spdSum, spdN := 0.0, 0
		for end < len(pts) && pts[end].OffsetMs/1000/bucket*bucket == bStart {
			p := pts[end]
			if p.HeartRate > 0 {
				hrSum += p.HeartRate
				hrN++
				hrMax = max(hrMax, p.HeartRate)
			}
			if p.SpeedMPerS > 0 {
				spdSum += p.SpeedMPerS
				spdN++
			}
			end++
		}
		for lapIdx < len(laps) && laps[lapIdx].StartOffsetMs/1000 < bStart+bucket {
			fmt.Fprintf(sb, "-- lap %d starts %s --\n", laps[lapIdx].LapNumber, clock(laps[lapIdx].StartOffsetMs/1000))
			lapIdx++
		}
		hr := "—"
		if hrN > 0 {
			hr = fmt.Sprintf("%d/%d", (hrSum+hrN/2)/hrN, hrMax)
		}
		pace := "—"
		if spdN > 0 {
			if v := spdSum / float64(spdN); v > 0.5 { // slower than ~33 min/km is standing
				s := int(1000/v + 0.5)
				pace = fmt.Sprintf("%d:%02d", s/60, s%60)
			}
		}
		fmt.Fprintf(sb, "%s %s %s\n", clock(bStart), hr, pace)
		start = end
	}
	sb.WriteString("\n")
}

// writeLapsTable writes the per-lap table (three laps or more).
func writeLapsTable(sb *strings.Builder, laps []training.Lap) {
	if len(laps) < 3 {
		return
	}
	sb.WriteString("## Laps\n")
	sb.WriteString("Per-lap data from the watch. When the planned session is interval-based, consult this rather than the averages — the recovery laps will lower the avg pace and avg HR even when the work laps were correctly executed.\n\n")
	sb.WriteString("| # | Dur | Dist | Avg HR | Max HR | Pace | Cad |\n")
	sb.WriteString("|---|-----|------|--------|--------|------|-----|\n")
	for _, l := range laps {
		durMin := int(l.DurationSeconds) / 60
		durSec := int(l.DurationSeconds) % 60
		pace := "—"
		if l.AvgPaceSecPerKm > 0 {
			pMin := int(l.AvgPaceSecPerKm) / 60
			pSec := int(l.AvgPaceSecPerKm) % 60
			pace = fmt.Sprintf("%d:%02d", pMin, pSec)
		}
		fmt.Fprintf(sb, "| %d | %d:%02d | %dm | %d | %d | %s | %d |\n",
			l.LapNumber, durMin, durSec, int(l.DistanceMeters),
			l.AvgHeartRate, l.MaxHeartRate, pace, l.AvgCadence,
		)
	}
	sb.WriteString("\n")
}
