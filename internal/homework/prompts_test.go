package homework

import (
	"strings"
	"testing"
)

func TestDetectSubject(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{"math keywords", "I need help to solve this equation", "math"},
		{"writing keywords", "Help me write an essay with a good thesis", "writing"},
		{"reading keywords", "Tell me about the character in this book chapter", "reading"},
		{"no keywords", "hello can you help me", "general"},
		{"empty string", "", "general"},
		{"case insensitive", "SOLVE the EQUATION please", "math"},
		{"math wins by count", "solve the equation and calculate the fraction for my math homework", "math"},
		{"writing wins by count", "write an essay with a thesis paragraph and good grammar", "writing"},
		{"reading wins by count", "I read a book about a character in a novel and the plot was great", "reading"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectSubject(tt.input)
			if got != tt.want {
				t.Errorf("DetectSubject(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildSystemPrompt_Preamble(t *testing.T) {
	profile := HomeworkProfile{Age: 10, GradeLevel: "5"}
	prompt := BuildSystemPrompt(profile, HelpLevelHint, "general")

	if !strings.Contains(prompt, "friendly, patient homework tutor") {
		t.Error("prompt missing tutor framing preamble")
	}
}

func TestBuildSystemPrompt_AntiCheating(t *testing.T) {
	profile := HomeworkProfile{Age: 10, GradeLevel: "5"}
	prompt := BuildSystemPrompt(profile, HelpLevelHint, "general")

	if !strings.Contains(prompt, "Never provide complete answers directly") {
		t.Error("prompt missing anti-cheating guardrails")
	}
}

func TestBuildSystemPrompt_AgeCalibration(t *testing.T) {
	young := HomeworkProfile{Age: 7, GradeLevel: "2"}
	middle := HomeworkProfile{Age: 12, GradeLevel: "7"}
	older := HomeworkProfile{Age: 16, GradeLevel: "10"}

	youngPrompt := BuildSystemPrompt(young, HelpLevelHint, "general")
	middlePrompt := BuildSystemPrompt(middle, HelpLevelHint, "general")
	olderPrompt := BuildSystemPrompt(older, HelpLevelHint, "general")

	if !strings.Contains(youngPrompt, "simple, clear language") {
		t.Error("young profile should use simple language")
	}
	if !strings.Contains(middlePrompt, "moderate vocabulary") {
		t.Error("middle profile should use moderate vocabulary")
	}
	if !strings.Contains(olderPrompt, "advanced vocabulary") {
		t.Error("older profile should use advanced vocabulary")
	}
}

func TestBuildSystemPrompt_GradeSuffixes(t *testing.T) {
	tests := []struct {
		grade string
		want  string
	}{
		{"1st", "simple"},
		{"2nd", "simple"},
		{"3rd", "simple"},
		{"5th", "moderate"},
		{"grade 7", "moderate"},
		{"12th", "advanced"},
	}

	for _, tt := range tests {
		t.Run(tt.grade, func(t *testing.T) {
			profile := HomeworkProfile{Age: 0, GradeLevel: tt.grade}
			prompt := BuildSystemPrompt(profile, HelpLevelHint, "general")
			if !strings.Contains(strings.ToLower(prompt), tt.want) {
				t.Errorf("grade %q: expected prompt to contain %q", tt.grade, tt.want)
			}
		})
	}
}

func TestBuildSystemPrompt_SubjectRules(t *testing.T) {
	profile := HomeworkProfile{Age: 12, GradeLevel: "7"}

	mathPrompt := BuildSystemPrompt(profile, HelpLevelHint, "math")
	if !strings.Contains(mathPrompt, "step-by-step reasoning") {
		t.Error("math prompt missing step-by-step enforcement")
	}

	writingPrompt := BuildSystemPrompt(profile, HelpLevelHint, "writing")
	if !strings.Contains(writingPrompt, "Never write essays") {
		t.Error("writing prompt missing essay prohibition")
	}

	readingPrompt := BuildSystemPrompt(profile, HelpLevelHint, "reading")
	if !strings.Contains(readingPrompt, "discussion questions") {
		t.Error("reading prompt missing discussion questions rule")
	}

	generalPrompt := BuildSystemPrompt(profile, HelpLevelHint, "general")
	if strings.Contains(generalPrompt, "MATH TUTORING") || strings.Contains(generalPrompt, "WRITING TUTORING") || strings.Contains(generalPrompt, "READING TUTORING") {
		t.Error("general prompt should not contain subject-specific rules")
	}
}

func TestBuildSystemPrompt_HelpLevels(t *testing.T) {
	profile := HomeworkProfile{Age: 12, GradeLevel: "7"}

	tests := []struct {
		level HelpLevel
		want  string
	}{
		{HelpLevelHint, "hints only"},
		{HelpLevelExplain, "concepts and principles"},
		{HelpLevelWalkthrough, "guided walkthrough"},
		{HelpLevelAnswer, "full solution step-by-step"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			prompt := BuildSystemPrompt(profile, tt.level, "general")
			if !strings.Contains(strings.ToLower(prompt), strings.ToLower(tt.want)) {
				t.Errorf("help level %q: expected prompt to contain %q", tt.level, tt.want)
			}
		})
	}
}

func TestParseGrade(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"3", 3},
		{"5th", 5},
		{"1st", 1},
		{"2nd", 2},
		{"3rd", 3},
		{"grade 7", 7},
		{"grade7", 7},
		{"12th", 12},
		{"invalid", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseGrade(tt.input)
			if got != tt.want {
				t.Errorf("parseGrade(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
