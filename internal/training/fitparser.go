package training

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/tormoder/fit"
)

// ParseFIT decodes a .fit file and extracts workout data.
func ParseFIT(r io.Reader) (*ParsedWorkout, string, error) {
	// Read all bytes to compute hash and then decode.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", fmt.Errorf("read fit data: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	// Decode using a bytes reader.
	file, err := fit.Decode(bytesReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode fit file: %w", err)
	}

	activity, err := file.Activity()
	if err != nil {
		return nil, "", fmt.Errorf("get activity: %w", err)
	}

	pw := &ParsedWorkout{}

	// Extract session-level summary.
	if len(activity.Sessions) > 0 {
		sess := activity.Sessions[0]
		pw.Sport = sportString(sess.Sport)
		pw.StartedAt = sess.StartTime
		pw.DurationSeconds = int(scaledOrZero(sess.GetTotalElapsedTimeScaled()))
		pw.DistanceMeters = scaledOrZero(sess.GetTotalDistanceScaled())
		pw.AvgHeartRate = int(sess.AvgHeartRate)
		pw.MaxHeartRate = int(sess.MaxHeartRate)
		pw.AvgCadence = int(sess.AvgCadence)
		pw.Calories = int(sess.TotalCalories)
		pw.AscentMeters = float64(sess.TotalAscent)
		pw.DescentMeters = float64(sess.TotalDescent)
	} else if activity.Activity != nil {
		pw.Sport = "other"
		pw.StartedAt = activity.Activity.Timestamp
	}

	// Extract laps.
	var activityStart time.Time
	if !pw.StartedAt.IsZero() {
		activityStart = pw.StartedAt
	}
	for _, lap := range activity.Laps {
		pl := ParsedLap{
			DurationSeconds: scaledOrZero(lap.GetTotalElapsedTimeScaled()),
			DistanceMeters:  scaledOrZero(lap.GetTotalDistanceScaled()),
			AvgHeartRate:    int(lap.AvgHeartRate),
			MaxHeartRate:    int(lap.MaxHeartRate),
			AvgSpeedMPerS:   scaledOrZero(lap.GetAvgSpeedScaled()),
			AvgCadence:      int(lap.AvgCadence),
		}
		if !activityStart.IsZero() && !lap.StartTime.IsZero() {
			pl.StartOffsetMs = lap.StartTime.Sub(activityStart).Milliseconds()
		}
		pw.Laps = append(pw.Laps, pl)
	}

	// Extract records as time-series samples.
	for _, rec := range activity.Records {
		s := Sample{}
		if !activityStart.IsZero() && !rec.Timestamp.IsZero() {
			s.OffsetMs = rec.Timestamp.Sub(activityStart).Milliseconds()
		}
		if rec.HeartRate != 0xFF { // 0xFF = invalid
			s.HeartRate = int(rec.HeartRate)
		}
		speed := rec.GetEnhancedSpeedScaled()
		if math.IsNaN(speed) {
			speed = rec.GetSpeedScaled()
		}
		if !math.IsNaN(speed) {
			s.SpeedMPerS = speed
		}
		if rec.Cadence != 0xFF {
			s.Cadence = int(rec.Cadence)
		}
		alt := rec.GetEnhancedAltitudeScaled()
		if math.IsNaN(alt) {
			alt = rec.GetAltitudeScaled()
		}
		if !math.IsNaN(alt) {
			s.AltitudeM = alt
		}
		dist := rec.GetDistanceScaled()
		if !math.IsNaN(dist) {
			s.DistanceM = dist
		}
		pw.Samples = append(pw.Samples, s)
	}

	return pw, hash, nil
}

func sportString(s fit.Sport) string {
	switch s {
	case fit.SportRunning:
		return "running"
	case fit.SportCycling:
		return "cycling"
	case fit.SportSwimming:
		return "swimming"
	case fit.SportWalking:
		return "walking"
	case fit.SportHiking:
		return "hiking"
	case fit.SportTraining:
		return "strength"
	case fit.SportRowing:
		return "rowing"
	case fit.SportCrossCountrySkiing:
		return "cross_country_skiing"
	default:
		return "other"
	}
}

func scaledOrZero(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

type bytesReaderWrapper struct {
	data []byte
	pos  int
}

func bytesReader(data []byte) io.Reader {
	return &bytesReaderWrapper{data: data}
}

func (b *bytesReaderWrapper) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
