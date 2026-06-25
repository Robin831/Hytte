package wordfeud

import (
	"testing"
)

func TestSolveSortedDefaultsToScore(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()

	got := SolveSorted(board, "HEST", trie, "", "")
	want := Solve(board, "HEST", trie)

	if len(got.Moves) != len(want.Moves) {
		t.Fatalf("empty sort mode should match Solve: got %d moves, want %d", len(got.Moves), len(want.Moves))
	}
	for i := range got.Moves {
		if got.Moves[i].Word != want.Moves[i].Word || got.Moves[i].Score != want.Moves[i].Score {
			t.Fatalf("move %d differs: got %q(%d), want %q(%d)", i,
				got.Moves[i].Word, got.Moves[i].Score, want.Moves[i].Word, want.Moves[i].Score)
		}
	}
}

func TestSolveSortedLeastVowels(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()

	result := SolveSorted(board, "HEST", trie, SortLeastVowels, "")
	if len(result.Moves) == 0 {
		t.Fatal("expected moves")
	}

	// VowelsUsed must be non-decreasing across the result list.
	for i := 1; i < len(result.Moves); i++ {
		if result.Moves[i].VowelsUsed < result.Moves[i-1].VowelsUsed {
			t.Errorf("not sorted by vowels: move %d has %d vowels after move %d with %d",
				i, result.Moves[i].VowelsUsed, i-1, result.Moves[i-1].VowelsUsed)
		}
	}

	// The first move should use the fewest vowels of any move.
	min := result.Moves[0].VowelsUsed
	for _, m := range result.Moves {
		if m.VowelsUsed < min {
			t.Errorf("move %q uses %d vowels, fewer than the first move's %d", m.Word, m.VowelsUsed, min)
		}
	}
}

func TestSolveSortedMostTiles(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()

	result := SolveSorted(board, "HEST", trie, SortMostTiles, "")
	if len(result.Moves) == 0 {
		t.Fatal("expected moves")
	}

	// TilesUsed must be non-increasing across the result list.
	for i := 1; i < len(result.Moves); i++ {
		if result.Moves[i].TilesUsed > result.Moves[i-1].TilesUsed {
			t.Errorf("not sorted by tiles: move %d uses %d tiles after move %d with %d",
				i, result.Moves[i].TilesUsed, i-1, result.Moves[i-1].TilesUsed)
		}
	}

	// The longest word HESTER (6 tiles) should sit at or near the top.
	if result.Moves[0].TilesUsed < 4 {
		t.Errorf("expected a high tile count at the top, got %d (%q)", result.Moves[0].TilesUsed, result.Moves[0].Word)
	}
}

func TestVowelsUsedCounting(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()

	result := Solve(board, "HEST", trie)
	for _, m := range result.Moves {
		// In this dictionary E is the only vowel; count expected occurrences in
		// the newly-placed (whole, on an empty board) word.
		want := 0
		for _, ch := range m.Word {
			if ch == 'E' {
				want++
			}
		}
		if m.VowelsUsed != want {
			t.Errorf("move %q: VowelsUsed=%d, want %d", m.Word, m.VowelsUsed, want)
		}
	}
}

func TestSolveSortedBlock(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	// Existing word so our rack has cross/extension options.
	board.Set(7, 7, 'H', false)
	board.Set(7, 8, 'E', false)

	result := SolveSorted(board, "STER", trie, SortBlock, "STER")
	if len(result.Moves) == 0 {
		t.Fatal("expected moves for block sort")
	}

	if len(result.Moves) > blockCandidateLimit {
		t.Errorf("block result should be a shortlist of at most %d, got %d", blockCandidateLimit, len(result.Moves))
	}

	for i, m := range result.Moves {
		if m.OppBest == nil {
			t.Fatalf("move %d (%q) missing OppBest", i, m.Word)
		}
	}

	// Ordered by ascending opponent best reply (lowest = best block).
	for i := 1; i < len(result.Moves); i++ {
		if *result.Moves[i].OppBest < *result.Moves[i-1].OppBest {
			t.Errorf("not sorted by opponent reply: move %d has %d after move %d with %d",
				i, *result.Moves[i].OppBest, i-1, *result.Moves[i-1].OppBest)
		}
	}
}

func TestApplyMovePlacesNewTilesOnly(t *testing.T) {
	board := NewSolverBoard()
	board.Set(7, 7, 'H', false)
	board.Set(7, 8, 'E', false)

	// HEST horizontally at row 7 col 7 reuses H,E and adds S,T.
	next := applyMove(board, ScoredMove{Word: "HEST", Row: 7, Col: 7, Direction: "horizontal"})

	if next.Cells[7][9].Letter != 'S' || !next.Cells[7][9].Filled {
		t.Errorf("expected S placed at (7,9), got %q filled=%v", string(next.Cells[7][9].Letter), next.Cells[7][9].Filled)
	}
	if next.Cells[7][10].Letter != 'T' {
		t.Errorf("expected T placed at (7,10), got %q", string(next.Cells[7][10].Letter))
	}
	// Original board must be untouched.
	if board.Cells[7][9].Filled {
		t.Error("applyMove mutated the original board")
	}
}
