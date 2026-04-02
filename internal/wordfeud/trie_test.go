package wordfeud

import (
	"testing"
)

func TestTrieInsertAndContains(t *testing.T) {
	trie := NewTrie()
	trie.Insert("HEST")
	trie.Insert("HEI")
	trie.Insert("HESTELANSEN")

	tests := []struct {
		word string
		want bool
	}{
		{"HEST", true},
		{"HEI", true},
		{"HESTELANSEN", true},
		{"HES", false},  // prefix only, not a word
		{"HESTER", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := trie.Contains(tt.word); got != tt.want {
			t.Errorf("Contains(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

func TestTrieHasPrefix(t *testing.T) {
	trie := NewTrie()
	trie.Insert("HEST")
	trie.Insert("HESTELANSEN")

	tests := []struct {
		prefix string
		want   bool
	}{
		{"H", true},
		{"HE", true},
		{"HES", true},
		{"HEST", true},
		{"HESTE", true},
		{"X", false},
		{"HESTX", false},
	}

	for _, tt := range tests {
		if got := trie.HasPrefix(tt.prefix); got != tt.want {
			t.Errorf("HasPrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
		}
	}
}

func TestTrieWordCount(t *testing.T) {
	trie := NewTrie()
	if trie.WordCount() != 0 {
		t.Errorf("empty trie WordCount() = %d, want 0", trie.WordCount())
	}

	trie.Insert("HEST")
	trie.Insert("HEI")
	trie.Insert("HEST") // duplicate
	if trie.WordCount() != 2 {
		t.Errorf("WordCount() = %d, want 2", trie.WordCount())
	}
}

func TestTrieNorwegianLetters(t *testing.T) {
	trie := NewTrie()
	trie.Insert("ÆRE")
	trie.Insert("ØST")
	trie.Insert("ÅR")

	for _, w := range []string{"ÆRE", "ØST", "ÅR"} {
		if !trie.Contains(w) {
			t.Errorf("Contains(%q) = false, want true", w)
		}
	}
}

func TestIsWordfeudLetter(t *testing.T) {
	valid := []rune{'A', 'Z', 'Æ', 'Ø', 'Å', 'a', 'z', 'æ', 'ø', 'å'}
	for _, r := range valid {
		if !isWordfeudLetter(r) {
			t.Errorf("isWordfeudLetter(%q) = false, want true", r)
		}
	}

	invalid := []rune{'1', ' ', '-', '.', '!', '*'}
	for _, r := range invalid {
		if isWordfeudLetter(r) {
			t.Errorf("isWordfeudLetter(%q) = true, want false", r)
		}
	}
}
