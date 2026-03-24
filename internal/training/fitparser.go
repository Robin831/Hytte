package training

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// ParseFIT decodes a .fit file and extracts workout data.
func ParseFIT(r io.Reader) (*ParsedWorkout, string, error) {
	// Read all bytes to compute hash and then decode.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", fmt.Errorf("read fit data: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	lis := filedef.NewListener()
	dec := decoder.New(bytes.NewReader(data), decoder.WithMesgListener(lis))
	if _, err := dec.Decode(); err != nil {
		return nil, "", fmt.Errorf("decode fit file: %w", err)
	}

	file := lis.File()
	act, ok := file.(*filedef.Activity)
	if !ok {
		return nil, "", fmt.Errorf("decoded FIT file has unexpected type %T, want *filedef.Activity", file)
	}

	pw := &ParsedWorkout{}

	// Extract workout name from FIT metadata.
	pw.Title = extractWorkoutName(act)

	// Extract session-level summary.
	if len(act.Sessions) > 0 {
		sess := act.Sessions[0]
		pw.Sport = sportString(sess.Sport)
		pw.SubSport = subSportString(sess.SubSport)
		pw.StartedAt = sess.StartTime
		pw.DurationSeconds = int(sessionDuration(sess))
		pw.DistanceMeters = sessionDistance(sess)
		pw.AvgHeartRate = int(sess.AvgHeartRate)
		pw.MaxHeartRate = int(sess.MaxHeartRate)
		pw.AvgCadence = int(sess.AvgCadence)
		pw.Calories = int(sess.TotalCalories)
		pw.AscentMeters = float64(sess.TotalAscent)
		pw.DescentMeters = float64(sess.TotalDescent)
	} else if act.Activity != nil {
		pw.Sport = "other"
		pw.StartedAt = act.Activity.Timestamp
	}

	// Extract laps.
	var activityStart time.Time
	if !pw.StartedAt.IsZero() {
		activityStart = pw.StartedAt
	}
	for _, lap := range act.Laps {
		pl := ParsedLap{
			DurationSeconds: lapDuration(lap),
			DistanceMeters:  lapDistance(lap),
			AvgHeartRate:    int(lap.AvgHeartRate),
			MaxHeartRate:    int(lap.MaxHeartRate),
			AvgSpeedMPerS:   lapAvgSpeed(lap),
			AvgCadence:      int(lap.AvgCadence),
			LapTrigger:      lapTriggerString(lap.LapTrigger),
		}
		if !activityStart.IsZero() && !lap.StartTime.IsZero() {
			pl.StartOffsetMs = lap.StartTime.Sub(activityStart).Milliseconds()
		}
		pw.Laps = append(pw.Laps, pl)
	}

	// Extract records as time-series samples and detect GPS presence.
	hasGPS := false
	for _, rec := range act.Records {
		if !hasGPS && rec.PositionLat != basetype.Sint32Invalid && rec.PositionLong != basetype.Sint32Invalid {
			hasGPS = true
		}
		s := Sample{}
		if !activityStart.IsZero() && !rec.Timestamp.IsZero() {
			s.OffsetMs = rec.Timestamp.Sub(activityStart).Milliseconds()
		}
		if rec.HeartRate != basetype.Uint8Invalid {
			s.HeartRate = int(rec.HeartRate)
		}
		if spd := recordSpeed(rec); spd >= 0 {
			s.SpeedMPerS = spd
		}
		if rec.Cadence != basetype.Uint8Invalid {
			s.Cadence = int(rec.Cadence)
		}
		if alt := recordAltitude(rec); alt >= -500 {
			s.AltitudeM = alt
		}
		if rec.Distance != basetype.Uint32Invalid {
			s.DistanceM = float64(rec.Distance) / 100.0
		}
		pw.Samples = append(pw.Samples, s)
	}
	pw.HasGPS = hasGPS

	return pw, hash, nil
}

// sessionDuration returns total elapsed time in seconds from a Session message.
// TotalElapsedTime is stored as uint32 with scale 1000 (milliseconds).
func sessionDuration(s *mesgdef.Session) float64 {
	if s.TotalElapsedTime == basetype.Uint32Invalid {
		return 0
	}
	return float64(s.TotalElapsedTime) / 1000.0
}

// sessionDistance returns total distance in meters from a Session message.
// TotalDistance is stored as uint32 with scale 100 (centimeters).
func sessionDistance(s *mesgdef.Session) float64 {
	if s.TotalDistance == basetype.Uint32Invalid {
		return 0
	}
	return float64(s.TotalDistance) / 100.0
}

// lapDuration returns total elapsed time in seconds from a Lap message.
func lapDuration(l *mesgdef.Lap) float64 {
	if l.TotalElapsedTime == basetype.Uint32Invalid {
		return 0
	}
	return float64(l.TotalElapsedTime) / 1000.0
}

// lapDistance returns total distance in meters from a Lap message.
func lapDistance(l *mesgdef.Lap) float64 {
	if l.TotalDistance == basetype.Uint32Invalid {
		return 0
	}
	return float64(l.TotalDistance) / 100.0
}

// lapAvgSpeed returns average speed in m/s from a Lap message.
// Prefers EnhancedAvgSpeed (scale 1000, uint32) over AvgSpeed (scale 1000, uint16).
func lapAvgSpeed(l *mesgdef.Lap) float64 {
	if l.EnhancedAvgSpeed != basetype.Uint32Invalid {
		return float64(l.EnhancedAvgSpeed) / 1000.0
	}
	if l.AvgSpeed != basetype.Uint16Invalid {
		return float64(l.AvgSpeed) / 1000.0
	}
	return 0
}

// recordSpeed returns speed in m/s from a Record message, or -1 if not present.
// Prefers EnhancedSpeed (scale 1000, uint32) over Speed (scale 1000, uint16).
func recordSpeed(r *mesgdef.Record) float64 {
	if r.EnhancedSpeed != basetype.Uint32Invalid {
		return float64(r.EnhancedSpeed) / 1000.0
	}
	if r.Speed != basetype.Uint16Invalid {
		return float64(r.Speed) / 1000.0
	}
	return -1
}

// recordAltitude returns altitude in meters from a Record message, or -501 if not present.
// Prefers EnhancedAltitude (scale 5, offset 500, uint32) over Altitude (scale 5, offset 500, uint16).
func recordAltitude(r *mesgdef.Record) float64 {
	if r.EnhancedAltitude != basetype.Uint32Invalid {
		return float64(r.EnhancedAltitude)/5.0 - 500.0
	}
	if r.Altitude != basetype.Uint16Invalid {
		return float64(r.Altitude)/5.0 - 500.0
	}
	return -501 // sentinel: no altitude present
}

func sportString(s typedef.Sport) string {
	switch s {
	case typedef.SportRunning:
		return "running"
	case typedef.SportCycling:
		return "cycling"
	case typedef.SportSwimming:
		return "swimming"
	case typedef.SportWalking:
		return "walking"
	case typedef.SportHiking:
		return "hiking"
	case typedef.SportTraining:
		return "strength"
	case typedef.SportRowing:
		return "rowing"
	case typedef.SportCrossCountrySkiing:
		return "cross_country_skiing"
	default:
		return "other"
	}
}

func subSportString(s typedef.SubSport) string {
	switch s {
	case typedef.SubSportTreadmill:
		return "treadmill"
	case typedef.SubSportIndoorRunning:
		return "indoor_running"
	case typedef.SubSportIndoorCycling:
		return "indoor_cycling"
	case typedef.SubSportTrail:
		return "trail"
	case typedef.SubSportTrack:
		return "track"
	case typedef.SubSportVirtualActivity:
		return "virtual"
	default:
		if s == typedef.SubSportInvalid || s == typedef.SubSportGeneric {
			return ""
		}
		return strings.ToLower(s.String())
	}
}

// lapTriggerString converts a FIT LapTrigger enum value to a lowercase string.
// Returns "" for invalid/unknown values.
func lapTriggerString(t typedef.LapTrigger) string {
	switch t {
	case typedef.LapTriggerManual:
		return "manual"
	case typedef.LapTriggerDistance:
		return "distance"
	case typedef.LapTriggerTime:
		return "time"
	case typedef.LapTriggerSessionEnd:
		return "session_end"
	case typedef.LapTriggerInvalid:
		return ""
	default:
		return strings.ToLower(t.String())
	}
}

// extractWorkoutName checks FIT metadata fields for a user-set workout name.
// It returns the first non-empty name found, or empty string if none exists.
//
//   - Session.SportProfileName — where COROS and other devices write the user-defined workout name.
//   - FileId.ProductName       — free-form device/model string used as fallback.
func extractWorkoutName(act *filedef.Activity) string {
	if len(act.Sessions) > 0 {
		if name := strings.TrimSpace(act.Sessions[0].SportProfileName); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(act.FileId.ProductName); name != "" {
		return name
	}
	return ""
}
