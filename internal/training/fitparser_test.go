package training

import (
	"testing"

	"github.com/tormoder/fit"
)

func TestExtractWorkoutName_SessionSportProfileName(t *testing.T) {
	file := &fit.File{}
	activity := &fit.ActivityFile{
		Sessions: []*fit.SessionMsg{
			{SportProfileName: "Morning 5K"},
		},
	}

	got := extractWorkoutName(file, activity)
	if got != "Morning 5K" {
		t.Errorf("expected %q, got %q", "Morning 5K", got)
	}
}

func TestExtractWorkoutName_FileIdProductName(t *testing.T) {
	file := &fit.File{}
	file.FileId.ProductName = "Coros Pace 3"
	activity := &fit.ActivityFile{
		Sessions: []*fit.SessionMsg{
			{SportProfileName: ""},
		},
	}

	got := extractWorkoutName(file, activity)
	if got != "Coros Pace 3" {
		t.Errorf("expected %q, got %q", "Coros Pace 3", got)
	}
}

func TestExtractWorkoutName_NoName(t *testing.T) {
	file := &fit.File{}
	activity := &fit.ActivityFile{
		Sessions: []*fit.SessionMsg{
			{SportProfileName: ""},
		},
	}

	got := extractWorkoutName(file, activity)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractWorkoutName_SessionTakesPriority(t *testing.T) {
	file := &fit.File{}
	file.FileId.ProductName = "Device Name"
	activity := &fit.ActivityFile{
		Sessions: []*fit.SessionMsg{
			{SportProfileName: "Workout Name"},
		},
	}

	got := extractWorkoutName(file, activity)
	if got != "Workout Name" {
		t.Errorf("expected %q, got %q", "Workout Name", got)
	}
}

func TestExtractWorkoutName_WhitespaceOnly(t *testing.T) {
	file := &fit.File{}
	activity := &fit.ActivityFile{
		Sessions: []*fit.SessionMsg{
			{SportProfileName: "   "},
		},
	}

	got := extractWorkoutName(file, activity)
	if got != "" {
		t.Errorf("expected empty string for whitespace-only name, got %q", got)
	}
}

func TestExtractWorkoutName_NoSessions(t *testing.T) {
	file := &fit.File{}
	file.FileId.ProductName = "Fallback Name"
	activity := &fit.ActivityFile{}

	got := extractWorkoutName(file, activity)
	if got != "Fallback Name" {
		t.Errorf("expected %q, got %q", "Fallback Name", got)
	}
}
