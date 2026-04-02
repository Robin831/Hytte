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
