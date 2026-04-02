package wordfeud

import (
	"testing"
)

// testSolverTrie builds a small trie for testing the solver.
func testSolverTrie() *Trie {
	t := NewTrie()
	words := []string{
		"HE", "HER", "HEST", "HI", "HIS",
		"EN", "ER", "ET",
		"SE", "SER", "SET",
		"TE", "TEN", "TEST",
		"RE", "RES", "REST",
		"ST", "STER",
		"ES", "EST",
		"ÆR", "ØR", "ÅR",
		"HESTER",
	}
	for _, w := range words {
		t.Insert(w)
	}
	return t
}

func TestSolveEmptyBoard(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	result := Solve(board, "HEST", trie)

	if len(result.Moves) == 0 {
		t.Fatal("expected moves on empty board")
	}

	// All moves should pass through center (7,7)
	for _, m := range result.Moves {
		if !moveCrossesCenter(m) {
			t.Errorf("move %q at (%d,%d) %s does not cross center",
				m.Word, m.Row, m.Col, m.Direction)
		}
	}

	// Moves should be sorted by score descending
	for i := 1; i < len(result.Moves); i++ {
		if result.Moves[i].Score > result.Moves[i-1].Score {
			t.Errorf("moves not sorted: score %d after %d",
				result.Moves[i].Score, result.Moves[i-1].Score)
		}
	}
}

func moveCrossesCenter(m ScoredMove) bool {
	wordLen := len([]rune(m.Word))
	if m.Direction == "horizontal" {
		return m.Row == 7 && m.Col <= 7 && m.Col+wordLen > 7
	}
	return m.Col == 7 && m.Row <= 7 && m.Row+wordLen > 7
}

func TestSolveWithExistingTiles(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	// Place "HE" horizontally at row 7, starting col 7
	board.Set(7, 7, 'H', false)
	board.Set(7, 8, 'E', false)

	result := Solve(board, "STER", trie)

	if len(result.Moves) == 0 {
		t.Fatal("expected moves with existing tiles")
	}

	// Should find words that extend or cross "HE"
	found := false
	for _, m := range result.Moves {
		if m.Word == "HEST" || m.Word == "HER" || m.Word == "HESTER" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find HEST, HER, or HESTER among moves")
		for i, m := range result.Moves {
			if i < 10 {
				t.Logf("  %s at (%d,%d) %s score=%d", m.Word, m.Row, m.Col, m.Direction, m.Score)
			}
		}
	}
}

func TestSolveWithCrossWords(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	// Place "ER" vertically at col 7: row 7='E', row 8='R'
	board.Set(7, 7, 'E', false)
	board.Set(8, 7, 'R', false)

	result := Solve(board, "HEST", trie)

	if len(result.Moves) == 0 {
		t.Fatal("expected moves crossing existing tiles")
	}

	// Any horizontal move through col 7 must form valid cross-words
	for _, m := range result.Moves {
		if m.Score <= 0 {
			t.Errorf("move %q has non-positive score %d", m.Word, m.Score)
		}
	}
}

func TestScoreMoveTripleWord(t *testing.T) {
	board := NewSolverBoard()
	// "TE" starting at (0,0) — (0,0) is TW (bonus=4)
	m := rawMove{
		word:    []rune("TE"),
		isBlank: []bool{false, false},
		row:     0,
		col:     0,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,0) TW: val=1, wordMul*=3
	// E at (0,1) none: val=1
	// Main: (1 + 1) * 3 = 6
	if score != 6 {
		t.Errorf("expected score 6 for TE on TW, got %d", score)
	}
}

func TestScoreMoveDoubleLetter(t *testing.T) {
	board := NewSolverBoard()
	// "TEST" starting at (0,0): T(TW), E(none), S(none), T(DL)
	m := rawMove{
		word:    []rune("TEST"),
		isBlank: []bool{false, false, false, false},
		row:     0,
		col:     0,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,0) TW: val=1, wordMul*=3
	// E at (0,1) none: val=1
	// S at (0,2) none: val=1
	// T at (0,3) DL: val=1*2=2
	// Main: (1 + 1 + 1 + 2) * 3 = 15
	if score != 15 {
		t.Errorf("expected score 15 for TEST at (0,0), got %d", score)
	}
}

func TestScoreMoveBlankTile(t *testing.T) {
	board := NewSolverBoard()
	// "TE" at (7,7) center (DW), T is a blank
	m := rawMove{
		word:    []rune("TE"),
		isBlank: []bool{true, false},
		row:     7,
		col:     7,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (7,7) center/DW: blank=0, wordMul*=2
	// E at (7,8) none: val=1
	// Main: (0 + 1) * 2 = 2
	if score != 2 {
		t.Errorf("expected score 2 for blank T + E on center, got %d", score)
	}
}

func TestScoreMoveCrossWord(t *testing.T) {
	board := NewSolverBoard()
	// Place 'E' at (6,7) and 'T' at (8,7) on the board
	board.Set(6, 7, 'E', false)
	board.Set(8, 7, 'T', false)

	// Place 'S' horizontally at (7,7) — this forms vertical cross-word E+S+T = "EST"
	m := rawMove{
		word:    []rune("SE"),
		isBlank: []bool{false, false},
		row:     7,
		col:     7,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// Main word "SE": S at (7,7) center/DW val=1 wordMul*=2, E at (7,8) val=1
	// Main: (1+1)*2 = 4
	// Cross-word at (7,7): E(above)+S(new)+T(below) = "EST"
	//   E=1, S at center/DW: val=1 wordMul*=2, T=1
	//   Cross: (1+1+1)*2 = 6
	// Total: 4 + 6 = 10
	if score != 10 {
		t.Errorf("expected score 10 for SE with EST cross-word, got %d", score)
	}
}

func TestScoreMoveAllTilesBonus(t *testing.T) {
	board := NewSolverBoard()
	// 7-letter word: all tiles bonus = 40
	m := rawMove{
		word:    []rune("HHHHHHE"), // fake word for scoring test
		isBlank: make([]bool, 7),
		row:     7,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// All 7 tiles placed (empty board), so +40 bonus applied
	if score < 40 {
		t.Errorf("expected score >= 40 with all-tiles bonus, got %d", score)
	}
}

func TestComputeAnchorsEmptyBoard(t *testing.T) {
	board := NewSolverBoard()
	anchors := computeAnchors(board)

	if !anchors[7][7] {
		t.Error("center cell should be anchor on empty board")
	}

	// No other anchors
	count := 0
	for r := 0; r < BoardSize; r++ {
		for c := 0; c < BoardSize; c++ {
			if anchors[r][c] {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("expected 1 anchor on empty board, got %d", count)
	}
}

func TestComputeAnchorsWithTiles(t *testing.T) {
	board := NewSolverBoard()
	board.Set(7, 7, 'A', false)

	anchors := computeAnchors(board)

	// Should have anchors at (6,7), (8,7), (7,6), (7,8)
	expected := [][2]int{{6, 7}, {8, 7}, {7, 6}, {7, 8}}
	for _, pos := range expected {
		if !anchors[pos[0]][pos[1]] {
			t.Errorf("expected anchor at (%d,%d)", pos[0], pos[1])
		}
	}

	// The filled cell itself should NOT be an anchor
	if anchors[7][7] {
		t.Error("filled cell should not be anchor")
	}
}

func TestComputeCrossChecks(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	// Place 'E' at (6,5) and 'T' at (8,5)
	board.Set(6, 5, 'E', false)
	board.Set(8, 5, 'T', false)

	checks := computeCrossChecks(board, trie)

	// At (7,5): only letters that form valid words E+?+T
	cc := checks[7][5]
	if cc.any {
		t.Error("cross-check at (7,5) should not allow any letter")
	}

	// "EST" (E+S+T) should be valid — 'S' should be allowed
	if !cc.allowed['S'] {
		t.Error("expected 'S' to be allowed at (7,5) for EST")
	}
}

func TestParseRack(t *testing.T) {
	r := parseRack("HeSt*")
	if r.tiles['H'] != 1 || r.tiles['E'] != 1 || r.tiles['S'] != 1 || r.tiles['T'] != 1 {
		t.Errorf("unexpected tiles: %v", r.tiles)
	}
	if r.blanks != 1 {
		t.Errorf("expected 1 blank, got %d", r.blanks)
	}
}

func TestSolveNoMoves(t *testing.T) {
	trie := NewTrie()
	trie.Insert("ZZ") // only word in dictionary
	board := NewSolverBoard()
	result := Solve(board, "ABC", trie)

	if len(result.Moves) != 0 {
		t.Errorf("expected no moves, got %d", len(result.Moves))
	}
}

func TestSolveWithBlankTile(t *testing.T) {
	trie := testSolverTrie()
	board := NewSolverBoard()
	result := Solve(board, "H*", trie)

	if len(result.Moves) == 0 {
		t.Fatal("expected moves with blank tile")
	}

	// Should find "HE", "HI", etc. using the blank
	foundHE := false
	for _, m := range result.Moves {
		if m.Word == "HE" {
			foundHE = true
			break
		}
	}
	if !foundHE {
		t.Error("expected to find HE with blank tile")
	}
}
