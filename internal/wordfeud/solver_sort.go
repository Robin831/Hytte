package wordfeud

import (
	"sort"
	"time"
)

// Sort orders for SolveSorted.
const (
	SortScore       = "score"        // highest score first (default)
	SortLeastVowels = "least_vowels" // fewest vowels played from the rack first
	SortMostTiles   = "most_tiles"   // most new tiles placed first
	SortBlock       = "block"        // minimise the opponent's best possible reply
)

// blockCandidateLimit bounds how many of our top-scoring moves are run through
// the defensive simulation. Each candidate triggers a full opponent solve, so
// this caps the worst-case latency of the block sort.
const blockCandidateLimit = 30

// SolveSorted is like Solve but orders the results by the given sort mode before
// applying the result cap. An unknown or empty sortMode falls back to score
// order.
//
// For SortBlock, oppRack must hold the opponent's known tiles: each of our top
// candidate moves is played out and ranked by the opponent's best possible
// reply (lowest first), so moves that block the opponent rank highest. Only the
// top blockCandidateLimit candidates by our own score are simulated, so the
// block result is a shortlist rather than the full move set.
func SolveSorted(board *SolverBoard, rackStr string, trie *Trie, sortMode, oppRack string) *SolveResult {
	start := time.Now()
	scored := solveAll(board, rackStr, trie)

	switch sortMode {
	case SortLeastVowels:
		sort.SliceStable(scored, func(i, j int) bool {
			if scored[i].VowelsUsed != scored[j].VowelsUsed {
				return scored[i].VowelsUsed < scored[j].VowelsUsed
			}
			return lessByScore(scored[i], scored[j])
		})
	case SortMostTiles:
		sort.SliceStable(scored, func(i, j int) bool {
			if scored[i].TilesUsed != scored[j].TilesUsed {
				return scored[i].TilesUsed > scored[j].TilesUsed
			}
			return lessByScore(scored[i], scored[j])
		})
	case SortBlock:
		scored = sortByBlock(board, scored, trie, oppRack)
	case SortScore, "":
		// solveAll already returns score order
	}

	if len(scored) > maxSolveResults {
		scored = scored[:maxSolveResults]
	}
	return &SolveResult{
		Moves:     scored,
		ElapsedMs: time.Since(start).Milliseconds(),
	}
}

// sortByBlock reorders the strongest candidate moves by how well they suppress
// the opponent's best reply. For each candidate it plays the move on a board
// copy, solves for the opponent's rack, and records their best reply score in
// OppBest. Moves are then ranked by lowest opponent reply, breaking ties by our
// own score. Only a shortlist (blockCandidateLimit) is simulated and returned.
func sortByBlock(board *SolverBoard, scored []ScoredMove, trie *Trie, oppRack string) []ScoredMove {
	limit := blockCandidateLimit
	if len(scored) < limit {
		limit = len(scored)
	}
	candidates := scored[:limit]

	for i := range candidates {
		next := applyMove(board, candidates[i])
		reply := Solve(next, oppRack, trie)
		best := 0
		if len(reply.Moves) > 0 {
			best = reply.Moves[0].Score
		}
		b := best
		candidates[i].OppBest = &b
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		bi, bj := oppBestOrZero(candidates[i]), oppBestOrZero(candidates[j])
		if bi != bj {
			return bi < bj
		}
		return lessByScore(candidates[i], candidates[j])
	})
	return candidates
}

func oppBestOrZero(m ScoredMove) int {
	if m.OppBest == nil {
		return 0
	}
	return *m.OppBest
}

// applyMove returns a copy of the board with the move's new tiles placed. Cells
// already filled on the board are left untouched (the word reuses them).
func applyMove(board *SolverBoard, m ScoredMove) *SolverBoard {
	next := *board // Cells and Layout are arrays, copied by value
	dr, dc := 0, 1
	if m.Direction == "vertical" {
		dr, dc = 1, 0
	}
	blanks := make(map[int]bool, len(m.BlankTiles))
	for _, bi := range m.BlankTiles {
		blanks[bi] = true
	}
	for i, ch := range []rune(m.Word) {
		r := m.Row + i*dr
		c := m.Col + i*dc
		if r < 0 || c < 0 || r >= BoardSize || c >= BoardSize {
			continue
		}
		if !next.Cells[r][c].Filled {
			next.Cells[r][c] = SolverCell{Letter: ch, IsBlank: blanks[i], Filled: true}
		}
	}
	return &next
}

// countNewVowels counts the vowel tiles a move draws from the rack — newly
// placed cells whose letter is a vowel and that are not played as blanks (a
// blank used as a vowel does not consume a vowel tile).
func countNewVowels(board *SolverBoard, m rawMove) int {
	dr, dc := 0, 1
	if m.dir == dirVertical {
		dr, dc = 1, 0
	}
	n := 0
	for i, ch := range m.word {
		r := m.row + i*dr
		c := m.col + i*dc
		if board.Cells[r][c].Filled {
			continue // existing board tile, not from our rack
		}
		if i < len(m.isBlank) && m.isBlank[i] {
			continue // blank tile does not spend a vowel
		}
		if isVowelLetter(ch) {
			n++
		}
	}
	return n
}

// isVowelLetter reports whether r is a vowel in the Norwegian alphabet (matches
// the VOWELS set in the frontend).
func isVowelLetter(r rune) bool {
	switch r {
	case 'A', 'E', 'I', 'O', 'U', 'Y', 'Æ', 'Ø', 'Å':
		return true
	}
	return false
}

// lessByScore orders moves by score descending, then by stable tiebreakers
// (word, direction, position, tiles, blanks) for deterministic output.
func lessByScore(a, b ScoredMove) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Word != b.Word {
		return a.Word < b.Word
	}
	if a.Direction != b.Direction {
		return a.Direction < b.Direction
	}
	if a.Row != b.Row {
		return a.Row < b.Row
	}
	if a.Col != b.Col {
		return a.Col < b.Col
	}
	if a.TilesUsed != b.TilesUsed {
		return a.TilesUsed < b.TilesUsed
	}
	if len(a.BlankTiles) != len(b.BlankTiles) {
		return len(a.BlankTiles) < len(b.BlankTiles)
	}
	for k := 0; k < len(a.BlankTiles); k++ {
		if a.BlankTiles[k] != b.BlankTiles[k] {
			return a.BlankTiles[k] < b.BlankTiles[k]
		}
	}
	return false
}
