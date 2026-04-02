package wordfeud

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// TrieNode is a node in a character trie. Each edge is a single rune.
type TrieNode struct {
	children map[rune]*TrieNode
	isWord   bool
}

// Trie holds the root of a character-based trie for dictionary lookups.
type Trie struct {
	root      *TrieNode
	wordCount int
}

// NewTrie creates an empty trie.
func NewTrie() *Trie {
	return &Trie{root: &TrieNode{children: make(map[rune]*TrieNode)}}
}

// Insert adds a word to the trie. The word should be uppercase.
func (t *Trie) Insert(word string) {
	node := t.root
	for _, r := range word {
		child, ok := node.children[r]
		if !ok {
			child = &TrieNode{children: make(map[rune]*TrieNode)}
			node.children[r] = child
		}
		node = child
	}
	if !node.isWord {
		node.isWord = true
		t.wordCount++
	}
}

// Contains returns true if the word exists in the trie.
func (t *Trie) Contains(word string) bool {
	node := t.root
	for _, r := range word {
		child, ok := node.children[r]
		if !ok {
			return false
		}
		node = child
	}
	return node.isWord
}

// HasPrefix returns true if any word in the trie starts with prefix.
func (t *Trie) HasPrefix(prefix string) bool {
	node := t.root
	for _, r := range prefix {
		child, ok := node.children[r]
		if !ok {
			return false
		}
		node = child
	}
	return true
}

// WordCount returns the number of words in the trie.
func (t *Trie) WordCount() int {
	return t.wordCount
}

// LoadDictionary reads a dictionary file (one word per line) and builds a trie.
// Words are uppercased and filtered to 2-15 letters. Only alphabetic characters
// (including Norwegian Æ, Ø, Å) are accepted.
func LoadDictionary(path string) (*Trie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dictionary: %w", err)
	}
	defer f.Close()

	trie := NewTrie()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if word == "" {
			continue
		}
		upper := strings.ToUpper(word)

		// Filter: 2-15 runes, only letters.
		n := utf8.RuneCountInString(upper)
		if n < 2 || n > 15 {
			continue
		}
		if !isAllLetters(upper) {
			continue
		}
		trie.Insert(upper)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dictionary: %w", err)
	}
	return trie, nil
}

// isAllLetters returns true if every rune in s is an alphabetic letter
// (A-Z, Æ, Ø, Å and their lowercase forms).
func isAllLetters(s string) bool {
	for _, r := range s {
		if !isWordfeudLetter(r) {
			return false
		}
	}
	return true
}

// isWordfeudLetter returns true if r is a letter valid in Wordfeud (Norwegian).
func isWordfeudLetter(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	switch r {
	case 'Æ', 'Ø', 'Å', 'æ', 'ø', 'å':
		return true
	}
	return false
}

// Dictionary is a lazily-loaded singleton trie backed by the NSF dictionary file.
type Dictionary struct {
	mu   sync.Mutex
	trie *Trie
	path string
	err  error
}

// NewDictionary creates a Dictionary that will load from the given file path on first use.
func NewDictionary(path string) *Dictionary {
	return &Dictionary{path: path}
}

// Trie returns the loaded trie, loading it on first call. Thread-safe.
func (d *Dictionary) Trie() (*Trie, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.trie != nil {
		return d.trie, nil
	}
	if d.err != nil {
		return nil, d.err
	}

	d.trie, d.err = LoadDictionary(d.path)
	if d.err != nil {
		return nil, d.err
	}
	return d.trie, nil
}
