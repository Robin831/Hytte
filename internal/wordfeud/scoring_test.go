package wordfeud

import (
	"testing"
)

func TestScoreWordSimple(t *testing.T) {
	tests := []struct {
		word string
		want int
	}{
		{"HEST", 3 + 1 + 1 + 1},   // H=3, E=1, S=1, T=1
		{"ÆRE", 8 + 1 + 1},        // Æ=8, R=1, E=1
		{"ØST", 5 + 1 + 1},        // Ø=5, S=1, T=1
		{"ÅR", 4 + 1},             // Å=4, R=1
		{"QZ", 10 + 10},           // Q=10, Z=10
	}

	for _, tt := range tests {
		if got := ScoreWordSimple(tt.word); got != tt.want {
			t.Errorf("ScoreWordSimple(%q) = %d, want %d", tt.word, got, tt.want)
		}
	}
}

func TestScoreWordWithBlanks(t *testing.T) {
	// HEST with H as blank: 0 + 1 + 1 + 1 = 3
	blanks := map[int]bool{0: true}
	got := ScoreWord("HEST", blanks)
	if got != 3 {
		t.Errorf("ScoreWord(HEST, blank at 0) = %d, want 3", got)
	}

	// HEST with no blanks: 3 + 1 + 1 + 1 = 6
	got = ScoreWord("HEST", nil)
	if got != 6 {
		t.Errorf("ScoreWord(HEST, no blanks) = %d, want 6", got)
	}
}

func TestNorwegianTilesTotal(t *testing.T) {
	total := 0
	for _, ti := range NorwegianTiles {
		total += ti.Count
	}
	if total != 104 {
		t.Errorf("total tiles = %d, want 104", total)
	}
}

func TestLetterValueCompleteness(t *testing.T) {
	// Every letter A-Z plus Æ, Ø, Å should have a value.
	for r := 'A'; r <= 'Z'; r++ {
		if _, ok := LetterValue[r]; !ok {
			t.Errorf("LetterValue missing entry for %q", r)
		}
	}
	for _, r := range []rune{'Æ', 'Ø', 'Å'} {
		if _, ok := LetterValue[r]; !ok {
			t.Errorf("LetterValue missing entry for %q", r)
		}
	}
}
