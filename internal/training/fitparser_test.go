package training

import (
	"bytes"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

func TestExtractWorkoutName_SessionSportProfileName(t *testing.T) {
	act := &filedef.Activity{
		Sessions: []*mesgdef.Session{
			{SportProfileName: "Morning 5K"},
		},
	}
	got := extractWorkoutName(act)
	if got != "Morning 5K" {
		t.Errorf("expected %q, got %q", "Morning 5K", got)
	}
}

func TestExtractWorkoutName_FileIdProductName(t *testing.T) {
	act := &filedef.Activity{
		FileId:   mesgdef.FileId{ProductName: "Coros Pace 3"},
		Sessions: []*mesgdef.Session{{SportProfileName: ""}},
	}
	got := extractWorkoutName(act)
	if got != "Coros Pace 3" {
		t.Errorf("expected %q, got %q", "Coros Pace 3", got)
	}
}

func TestExtractWorkoutName_NoName(t *testing.T) {
	act := &filedef.Activity{
		Sessions: []*mesgdef.Session{{SportProfileName: ""}},
	}
	got := extractWorkoutName(act)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractWorkoutName_SessionTakesPriority(t *testing.T) {
	act := &filedef.Activity{
		FileId:   mesgdef.FileId{ProductName: "Device Name"},
		Sessions: []*mesgdef.Session{{SportProfileName: "Workout Name"}},
	}
	got := extractWorkoutName(act)
	if got != "Workout Name" {
		t.Errorf("expected %q, got %q", "Workout Name", got)
	}
}

func TestExtractWorkoutName_WhitespaceOnly(t *testing.T) {
	act := &filedef.Activity{
		Sessions: []*mesgdef.Session{{SportProfileName: "   "}},
	}
	got := extractWorkoutName(act)
	if got != "" {
		t.Errorf("expected empty string for whitespace-only name, got %q", got)
	}
}

func TestExtractWorkoutName_NoSessions(t *testing.T) {
	act := &filedef.Activity{
		FileId: mesgdef.FileId{ProductName: "Fallback Name"},
	}
	got := extractWorkoutName(act)
	if got != "Fallback Name" {
		t.Errorf("expected %q, got %q", "Fallback Name", got)
	}
}

func TestSessionDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected float64
	}{
		{"valid 60s", 60000, 60.0},
		{"zero", 0, 0.0},
		{"invalid", basetype.Uint32Invalid, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &mesgdef.Session{TotalElapsedTime: tt.input}
			if got := sessionDuration(s); got != tt.expected {
				t.Errorf("sessionDuration() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSessionDistance(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected float64
	}{
		{"valid 500m", 50000, 500.0},
		{"zero", 0, 0.0},
		{"invalid", basetype.Uint32Invalid, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &mesgdef.Session{TotalDistance: tt.input}
			if got := sessionDistance(s); got != tt.expected {
				t.Errorf("sessionDistance() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLapDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected float64
	}{
		{"valid 30s", 30000, 30.0},
		{"zero", 0, 0.0},
		{"invalid", basetype.Uint32Invalid, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &mesgdef.Lap{TotalElapsedTime: tt.input}
			if got := lapDuration(l); got != tt.expected {
				t.Errorf("lapDuration() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLapDistance(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected float64
	}{
		{"valid 1000m", 100000, 1000.0},
		{"zero", 0, 0.0},
		{"invalid", basetype.Uint32Invalid, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &mesgdef.Lap{TotalDistance: tt.input}
			if got := lapDistance(l); got != tt.expected {
				t.Errorf("lapDistance() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRecordSpeed(t *testing.T) {
	t.Run("enhanced speed takes priority", func(t *testing.T) {
		r := &mesgdef.Record{EnhancedSpeed: 3000, Speed: 2000}
		if got := recordSpeed(r); got != 3.0 {
			t.Errorf("recordSpeed() = %v, want 3.0", got)
		}
	})
	t.Run("falls back to Speed when EnhancedSpeed invalid", func(t *testing.T) {
		r := &mesgdef.Record{EnhancedSpeed: basetype.Uint32Invalid, Speed: 2500}
		if got := recordSpeed(r); got != 2.5 {
			t.Errorf("recordSpeed() = %v, want 2.5", got)
		}
	})
	t.Run("returns -1 when both invalid", func(t *testing.T) {
		r := &mesgdef.Record{EnhancedSpeed: basetype.Uint32Invalid, Speed: basetype.Uint16Invalid}
		if got := recordSpeed(r); got != -1 {
			t.Errorf("recordSpeed() = %v, want -1", got)
		}
	})
}

func TestRecordAltitude(t *testing.T) {
	t.Run("enhanced altitude takes priority", func(t *testing.T) {
		// EnhancedAltitude = (alt + 500) * 5; for alt=100m: (100+500)*5 = 3000
		r := &mesgdef.Record{EnhancedAltitude: 3000, Altitude: 2000}
		if got := recordAltitude(r); got != 100.0 {
			t.Errorf("recordAltitude() = %v, want 100.0", got)
		}
	})
	t.Run("falls back to Altitude when EnhancedAltitude invalid", func(t *testing.T) {
		// Altitude = (alt + 500) * 5; for alt=0m: (0+500)*5 = 2500
		r := &mesgdef.Record{EnhancedAltitude: basetype.Uint32Invalid, Altitude: 2500}
		if got := recordAltitude(r); got != 0.0 {
			t.Errorf("recordAltitude() = %v, want 0.0", got)
		}
	})
	t.Run("returns sentinel when both invalid", func(t *testing.T) {
		r := &mesgdef.Record{EnhancedAltitude: basetype.Uint32Invalid, Altitude: basetype.Uint16Invalid}
		if got := recordAltitude(r); got != -501 {
			t.Errorf("recordAltitude() = %v, want -501", got)
		}
	})
}

// TestParseFIT_Integration encodes a minimal Activity FIT file in memory and
// verifies that ParseFIT correctly extracts session, lap, and record fields.
func TestParseFIT_Integration(t *testing.T) {
	now := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)

	act := &filedef.Activity{
		FileId: mesgdef.FileId{
			Type:        typedef.FileActivity,
			Manufacturer: typedef.ManufacturerGarmin,
			Product:      1,
			SerialNumber: 12345,
			TimeCreated:  now,
		},
		Activity: &mesgdef.Activity{
			Timestamp:   now.Add(30 * time.Minute),
			NumSessions: 1,
			Type:        typedef.ActivityManual,
			Event:       typedef.EventActivity,
			EventType:   typedef.EventTypeStop,
		},
		Sessions: []*mesgdef.Session{
			{
				Timestamp:        now.Add(30 * time.Minute),
				StartTime:        now,
				TotalElapsedTime: 1800000, // 1800s
				TotalDistance:    10000,   // 100m
				Sport:            typedef.SportRunning,
				Event:            typedef.EventSession,
				EventType:        typedef.EventTypeStopDisableAll,
				SportProfileName: "Morning Run",
				AvgHeartRate:     150,
				MaxHeartRate:     175,
				TotalCalories:    200,
			},
		},
		Laps: []*mesgdef.Lap{
			{
				Timestamp:        now.Add(15 * time.Minute),
				StartTime:        now,
				TotalElapsedTime: 900000,  // 900s
				TotalDistance:    5000,    // 50m
				Event:            typedef.EventLap,
				EventType:        typedef.EventTypeStop,
				AvgHeartRate:     145,
				MaxHeartRate:     165,
			},
		},
		Records: func() []*mesgdef.Record {
			// Use NewRecord(nil) so unset uint fields default to their
			// invalid sentinel values rather than Go's zero value.
			r := mesgdef.NewRecord(nil)
			r.Timestamp = now.Add(1 * time.Second)
			r.HeartRate = 140
			r.EnhancedSpeed = 3000    // 3 m/s
			r.EnhancedAltitude = 2500 // 0m altitude
			return []*mesgdef.Record{r}
		}(),
	}

	fit := act.ToFIT(nil)
	var buf bytes.Buffer
	enc := encoder.New(&buf)
	if err := enc.Encode(&fit); err != nil {
		t.Fatalf("failed to encode FIT: %v", err)
	}

	pw, hash, err := ParseFIT(&buf)
	if err != nil {
		t.Fatalf("ParseFIT() error = %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash")
	}
	if pw.Title != "Morning Run" {
		t.Errorf("Title = %q, want %q", pw.Title, "Morning Run")
	}
	if pw.Sport != "running" {
		t.Errorf("Sport = %q, want %q", pw.Sport, "running")
	}
	if pw.DurationSeconds != 1800 {
		t.Errorf("DurationSeconds = %v, want 1800", pw.DurationSeconds)
	}
	if pw.DistanceMeters != 100.0 {
		t.Errorf("DistanceMeters = %v, want 100.0", pw.DistanceMeters)
	}
	if pw.AvgHeartRate != 150 {
		t.Errorf("AvgHeartRate = %v, want 150", pw.AvgHeartRate)
	}
	if len(pw.Laps) != 1 {
		t.Fatalf("len(Laps) = %v, want 1", len(pw.Laps))
	}
	if pw.Laps[0].DurationSeconds != 900.0 {
		t.Errorf("Laps[0].DurationSeconds = %v, want 900.0", pw.Laps[0].DurationSeconds)
	}
	if len(pw.Samples) != 1 {
		t.Fatalf("len(Samples) = %v, want 1", len(pw.Samples))
	}
	if pw.Samples[0].HeartRate != 140 {
		t.Errorf("Samples[0].HeartRate = %v, want 140", pw.Samples[0].HeartRate)
	}
	if pw.Samples[0].SpeedMPerS != 3.0 {
		t.Errorf("Samples[0].SpeedMPerS = %v, want 3.0", pw.Samples[0].SpeedMPerS)
	}
}
