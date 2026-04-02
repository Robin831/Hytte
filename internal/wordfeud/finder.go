package wordfeud

import (
	"sort"
	"strings"
)

// FoundWord is a word found by the finder, with its score and blank positions.
type FoundWord struct {
	Word           string `json:"word"`
	Score          int    `json:"score"`
	BlankPositions []int  `json:"blank_positions,omitempty"`
}

// FindWords finds all valid dictionary words that can be formed from the given
// letters. Blanks are represented as '*' in the letters string. Results are
// sorted by score descending, then alphabetically.
func FindWords(trie *Trie, letters string) []FoundWord {
	upper := strings.ToUpper(letters)

	// Count available letters.
	available := make(map[rune]int)
	blanks := 0
	for _, r := range upper {
		if r == '*' {
			blanks++
		} else {
			available[r]++
		}
	}

	var results []FoundWord
	seen := make(map[string]bool)

	// Recursive DFS through the trie, consuming available letters.
	var dfs func(node *TrieNode, word []rune, blankPos []int, avail map[rune]int, blanksLeft int)
	dfs = func(node *TrieNode, word []rune, blankPos []int, avail map[rune]int, blanksLeft int) {
		if node.isWord && len(word) >= 2 {
			w := string(word)
			if !seen[w] {
				seen[w] = true
				bp := make(map[int]bool, len(blankPos))
				for _, p := range blankPos {
					bp[p] = true
				}
				score := ScoreWord(w, bp)
				fw := FoundWord{Word: w, Score: score}
				if len(blankPos) > 0 {
					posCopy := make([]int, len(blankPos))
					copy(posCopy, blankPos)
					fw.BlankPositions = posCopy
				}
				results = append(results, fw)
			}
		}

		pos := len(word)
		for r, child := range node.children {
			// Try using a regular tile.
			if avail[r] > 0 {
				avail[r]--
				dfs(child, append(word, r), blankPos, avail, blanksLeft)
				avail[r]++
			}
			// Try using a blank tile.
			if blanksLeft > 0 {
				dfs(child, append(word, r), append(blankPos, pos), avail, blanksLeft-1)
			}
		}
	}

	dfs(trie.root, nil, nil, available, blanks)

	// Sort: highest score first, then alphabetically.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Word < results[j].Word
	})

	return results
}

// canFormWord checks whether word can be formed from the given letters.
// Blanks are represented as '*'. Returns true if the available letters
// (including blanks as wildcards) are sufficient.
func canFormWord(word string, letters string) bool {
	avail := make(map[rune]int)
	blanks := 0
	for _, r := range letters {
		if r == '*' {
			blanks++
		} else {
			avail[r]++
		}
	}
	for _, r := range word {
		if avail[r] > 0 {
			avail[r]--
		} else if blanks > 0 {
			blanks--
		} else {
			return false
		}
	}
	return true
}

// SearchWords finds dictionary words matching a pattern in the given mode.
// Supported modes: "starts_with", "ends_with", "contains".
// When letters is non-empty, results are filtered to words that can be formed
// from the combined pool of letters + pattern characters (with blank support).
// Results are sorted by score descending, then alphabetically.
// The caller is responsible for capping the result slice.
func SearchWords(trie *Trie, pattern string, mode string, letters string) []FoundWord {
	pattern = strings.ToUpper(pattern)
	letters = strings.ToUpper(letters)
	var words []string

	switch mode {
	case "starts_with":
		// limit=0 means unlimited — collect all prefix matches before scoring.
		words = trie.WordsWithPrefix(pattern, 0)
	case "ends_with":
		trie.WalkWords(func(w string) bool {
			if strings.HasSuffix(w, pattern) {
				words = append(words, w)
			}
			// Always continue walking so we consider all candidates before scoring/sorting.
			return true
		})
	case "contains":
		trie.WalkWords(func(w string) bool {
			if strings.Contains(w, pattern) {
				words = append(words, w)
			}
			// Always continue walking so we consider all candidates before scoring/sorting.
			return true
		})
	default:
		return nil
	}

	// When rack letters are provided, filter to words formable from rack + pattern letters.
	pool := letters + pattern
	filterByRack := letters != ""

	results := make([]FoundWord, 0, len(words))
	for _, w := range words {
		if filterByRack && !canFormWord(w, pool) {
			continue
		}
		results = append(results, FoundWord{
			Word:  w,
			Score: ScoreWordSimple(w),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Word < results[j].Word
	})

	return results
}
