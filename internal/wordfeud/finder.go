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

// SearchWords finds dictionary words matching a pattern in the given mode.
// Supported modes: "starts_with", "ends_with", "contains".
// Results are sorted by score descending, then alphabetically, capped at limit.
func SearchWords(trie *Trie, pattern string, mode string, limit int) []FoundWord {
	pattern = strings.ToUpper(pattern)
	var words []string

	switch mode {
	case "starts_with":
		words = trie.WordsWithPrefix(pattern, limit)
	case "ends_with":
		trie.WalkWords(func(w string) bool {
			if strings.HasSuffix(w, pattern) {
				words = append(words, w)
			}
			return len(words) < limit*2
		})
	case "contains":
		trie.WalkWords(func(w string) bool {
			if strings.Contains(w, pattern) {
				words = append(words, w)
			}
			return len(words) < limit*2
		})
	default:
		return nil
	}

	results := make([]FoundWord, 0, len(words))
	for _, w := range words {
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

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}
