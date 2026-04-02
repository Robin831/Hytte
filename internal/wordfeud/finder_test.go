package wordfeud

import (
	"testing"
)

func buildTestTrie() *Trie {
	trie := NewTrie()
	words := []string{
		"EN", "ER", "ET", "RE", "SE", "TE",
		"ERS", "EST", "REN", "SER", "SET", "TRE",
		"REST", "STER", "TRES",
		"HEST", "STEIN",
	}
	for _, w := range words {
		trie.Insert(w)
	}
	return trie
}

func TestFindWordsBasic(t *testing.T) {
	trie := buildTestTrie()
	results := FindWords(trie, "ERST")

	// Letters: E, R, S, T → should find words like ER, RE, SE, EST, ERS, SER, SET, REST, STER, TRES, TRE.
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}

	expected := []string{"ER", "RE", "SE", "TE", "ET", "ERS", "EST", "SER", "SET", "TRE", "REST", "STER", "TRES"}
	for _, w := range expected {
		if !found[w] {
			t.Errorf("FindWords(ERST) missing expected word %q", w)
		}
	}

	// HEST requires H which is not in our letters.
	if found["HEST"] {
		t.Error("FindWords(ERST) should not contain HEST")
	}
}

func TestFindWordsWithBlank(t *testing.T) {
	trie := buildTestTrie()

	// E, R, * → blank can be any letter.
	results := FindWords(trie, "ER*")

	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}

	// ER should be found (no blank needed).
	if !found["ER"] {
		t.Error("FindWords(ER*) should contain ER")
	}
	// RE should be found (no blank needed).
	if !found["RE"] {
		t.Error("FindWords(ER*) should contain RE")
	}
	// REN should be found (blank as N).
	if !found["REN"] {
		t.Error("FindWords(ER*) should contain REN (blank as N)")
	}
	// TRE should be found (blank as T).
	if !found["TRE"] {
		t.Error("FindWords(ER*) should contain TRE (blank as T)")
	}
}

func TestFindWordsSortedByScore(t *testing.T) {
	trie := buildTestTrie()
	results := FindWords(trie, "ERST")

	if len(results) < 2 {
		t.Fatal("expected at least 2 results")
	}

	// Results should be sorted by score descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: %s(%d) > %s(%d)",
				results[i].Word, results[i].Score,
				results[i-1].Word, results[i-1].Score)
		}
	}
}

func TestFindWordsBlankScoresZero(t *testing.T) {
	trie := NewTrie()
	trie.Insert("AB")

	// With letters A and blank (for B): A=1, blank=0 → score 1
	results := FindWords(trie, "A*")

	var abResult *FoundWord
	for i := range results {
		if results[i].Word == "AB" {
			abResult = &results[i]
			break
		}
	}
	if abResult == nil {
		t.Fatal("AB not found in results")
	}
	// A=1, B as blank=0 → 1
	if abResult.Score != 1 {
		t.Errorf("AB with blank score = %d, want 1", abResult.Score)
	}
	if len(abResult.BlankPositions) != 1 || abResult.BlankPositions[0] != 1 {
		t.Errorf("AB blank positions = %v, want [1]", abResult.BlankPositions)
	}
}

func TestFindWordsEmpty(t *testing.T) {
	trie := buildTestTrie()

	// Single letter can't form 2+ letter words.
	results := FindWords(trie, "X")
	if len(results) != 0 {
		t.Errorf("FindWords(X) returned %d results, want 0", len(results))
	}
}

func TestCanFormWord(t *testing.T) {
	tests := []struct {
		name    string
		word    string
		letters string
		want    bool
	}{
		{"exact match", "REST", "REST", true},
		{"extra letters", "REST", "RESTED", true},
		{"missing letter", "REST", "RES", false},
		{"duplicate needed", "SEES", "SEES", true},
		{"duplicate missing", "SEES", "SE", false},
		{"blank fills gap", "REST", "RES*", true},
		{"blank not enough", "REST", "R*", false},
		{"two blanks not enough", "REST", "RE*", false},
		{"two blanks", "AB", "**", true},
		{"case insensitive pool", "rest", "REST", true},
		{"empty word", "", "ABC", true},
		{"empty letters", "AB", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canFormWord(tt.word, tt.letters)
			if got != tt.want {
				t.Errorf("canFormWord(%q, %q) = %v, want %v", tt.word, tt.letters, got, tt.want)
			}
		})
	}
}

func TestSearchWordsStartsWith(t *testing.T) {
	trie := buildTestTrie()

	// "starts_with" RE with no letters → all words starting with RE.
	results := SearchWords(trie, "RE", "starts_with", "")
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}
	if !found["RE"] {
		t.Error("starts_with RE should include RE")
	}
	if !found["REN"] {
		t.Error("starts_with RE should include REN")
	}
	if !found["REST"] {
		t.Error("starts_with RE should include REST")
	}
	if found["ER"] {
		t.Error("starts_with RE should not include ER")
	}
}

func TestSearchWordsStartsWithLetterFilter(t *testing.T) {
	trie := buildTestTrie()

	// Rack "ST", starts_with "RE" → pool is "STRE", can form REST but not REN (no N).
	results := SearchWords(trie, "RE", "starts_with", "ST")
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}
	if !found["REST"] {
		t.Error("starts_with RE with letters ST should include REST")
	}
	if !found["RE"] {
		t.Error("starts_with RE with letters ST should include RE")
	}
	if found["REN"] {
		t.Error("starts_with RE with letters ST should not include REN (no N)")
	}
}

func TestSearchWordsEndsWith(t *testing.T) {
	trie := buildTestTrie()

	// "ends_with" ER with letters "ST" → pool is "STER", can form STER.
	results := SearchWords(trie, "ER", "ends_with", "ST")
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}
	if !found["STER"] {
		t.Error("ends_with ER with letters ST should include STER")
	}
	if !found["ER"] {
		t.Error("ends_with ER with letters ST should include ER")
	}
	if !found["SER"] {
		t.Error("ends_with ER with letters ST should include SER (S available in rack)")
	}
	if found["HEST"] {
		t.Error("ends_with ER with letters ST should not include HEST (does not end with ER)")
	}
}

func TestSearchWordsContains(t *testing.T) {
	trie := buildTestTrie()

	// "contains" ES with letters "RT" → pool is "ESRT", can form REST (contains ES).
	results := SearchWords(trie, "ES", "contains", "RT")
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}
	if !found["REST"] {
		t.Error("contains ES with letters RT should include REST")
	}
	if found["HEST"] {
		t.Error("contains ES with letters RT should not include HEST (no H)")
	}
}

func TestSearchWordsContainsNoLetters(t *testing.T) {
	trie := buildTestTrie()

	// "contains" ES with no rack letters → all words containing ES, unfiltered.
	results := SearchWords(trie, "ES", "contains", "")
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Word] = true
	}
	if !found["HEST"] {
		t.Error("contains ES with no letter filter should include HEST")
	}
	if !found["REST"] {
		t.Error("contains ES with no letter filter should include REST")
	}
}

func TestSearchWordsInvalidMode(t *testing.T) {
	trie := buildTestTrie()
	results := SearchWords(trie, "RE", "invalid_mode", "")
	if results != nil {
		t.Errorf("invalid mode should return nil, got %d results", len(results))
	}
}
