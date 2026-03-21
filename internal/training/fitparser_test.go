package training

import (
	"testing"

	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
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
