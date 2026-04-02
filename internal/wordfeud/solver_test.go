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
	// "TE" starting at (0,4) — (0,4) is TW (bonus=4)
	m := rawMove{
		word:    []rune("TE"),
		isBlank: []bool{false, false},
		row:     0,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,4) TW: val=1, wordMul*=3
	// E at (0,5) none: val=1
	// Main: (1 + 1) * 3 = 6
	if score != 6 {
		t.Errorf("expected score 6 for TE on TW, got %d", score)
	}
}

func TestScoreMoveDoubleLetter(t *testing.T) {
	board := NewSolverBoard()
	// "TEST" starting at (0,4): T(TW), E(none), S(none), T(DL at 0,7)
	m := rawMove{
		word:    []rune("TEST"),
		isBlank: []bool{false, false, false, false},
		row:     0,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,4) TW: val=1, wordMul*=3
	// E at (0,5) none: val=1
	// S at (0,6) none: val=1
	// T at (0,7) DL: val=1*2=2
	// Main: (1 + 1 + 1 + 2) * 3 = 15
	if score != 15 {
		t.Errorf("expected score 15 for TEST at (0,4), got %d", score)
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

func TestScoreMoveDWOnExistingTile(t *testing.T) {
	board := NewSolverBoard()
	// Place 'H' on a DW square at (7,3) — existing tile on DW should NOT activate word multiplier
	board.Set(7, 3, 'H', false)
	// Play "HE" horizontally: H at (7,3) is existing, E at (7,4) is new (TW at 4,0 but row 7 col 4 = 0)
	m := rawMove{
		word:    []rune("HE"),
		isBlank: []bool{false, false},
		row:     7,
		col:     3,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// H at (7,3) DW — existing tile, no multiplier: val=3
	// E at (7,4) none: val=1
	// wordMul = 1 (DW not activated because existing tile is on it)
	// Main: (3 + 1) * 1 = 4
	if score != 4 {
		t.Errorf("expected score 4 for HE with existing H on DW, got %d", score)
	}
}

func TestScoreMoveMultiplierMixedNewExisting(t *testing.T) {
	board := NewSolverBoard()
	// Place 'T' at (0,4) — this is a TW square with an existing tile
	board.Set(0, 4, 'T', false)
	// Play "TE" horizontally starting at (0,4): T is existing on TW, E is new at (0,5)
	m := rawMove{
		word:    []rune("TE"),
		isBlank: []bool{false, false},
		row:     0,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,4) TW — existing tile, no multiplier: val=1
	// E at (0,5) none: val=1
	// wordMul = 1 (TW not activated)
	// Main: (1 + 1) * 1 = 2
	if score != 2 {
		t.Errorf("expected score 2 for TE with existing T on TW, got %d", score)
	}
}

func TestScoreMoveBlankOnMultiplier(t *testing.T) {
	board := NewSolverBoard()
	// Place blank 'T' on TW at (0,4): blank scores 0 even with TW multiplier
	m := rawMove{
		word:    []rune("TE"),
		isBlank: []bool{true, false},
		row:     0,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// T at (0,4) TW: blank=0, wordMul*=3
	// E at (0,5) none: val=1
	// Main: (0 + 1) * 3 = 3
	if score != 3 {
		t.Errorf("expected score 3 for blank T on TW + E, got %d", score)
	}
}

func TestScoreMoveCrossWordWithMultiplier(t *testing.T) {
	board := NewSolverBoard()
	// Place 'E' at (3,7) and 'T' at (5,7) on the board
	board.Set(3, 7, 'E', false)
	board.Set(5, 7, 'T', false)

	// Place 'S' at (4,7) vertically — forms cross-word by itself
	// Also place 'E' at (4,6) horizontally to form "ES" as the main word
	// Main word: "ES" at (4,6) horizontal. S at (4,7) forms cross-word "EST" vertically.
	m := rawMove{
		word:    []rune("ES"),
		isBlank: []bool{false, false},
		row:     4,
		col:     6,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// Layout: (4,6) = DL=1, (4,7) = 0(none)
	// Main: E at (4,6) DL: val=1*2=2, S at (4,7) none: val=1
	// Main: (2 + 1) * 1 = 3
	// Cross at (4,7): E(3,7 existing)=1 + S(4,7 new, none)=1 + T(5,7 existing)=1
	// Cross: (1+1+1)*1 = 3
	// Total: 3 + 3 = 6
	if score != 6 {
		t.Errorf("expected score 6 for ES with EST cross-word, got %d", score)
	}
}

func TestScoreMoveExistingBlankTile(t *testing.T) {
	board := NewSolverBoard()
	// Place blank 'H' on the board at (7,7) center
	board.Set(7, 7, 'H', true)
	// Play "HE" extending right: H at (7,7) is existing blank, E at (7,8) is new
	m := rawMove{
		word:    []rune("HE"),
		isBlank: []bool{false, false},
		row:     7,
		col:     7,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// H at (7,7) center — existing blank tile: val=0 (blank scores 0)
	// E at (7,8) none: val=1
	// wordMul = 1 (center not activated)
	// Main: (0 + 1) * 1 = 1
	if score != 1 {
		t.Errorf("expected score 1 for HE with existing blank H on center, got %d", score)
	}
}

func TestScoreMoveAllTilesBonusNotApplied(t *testing.T) {
	board := NewSolverBoard()
	// Place some existing tiles so only 4 new tiles are needed
	board.Set(7, 6, 'H', false)
	board.Set(7, 7, 'E', false)
	board.Set(7, 8, 'S', false)

	// Play a 7-letter word but only 4 new tiles (H,H at 4,5 and T,T at 9,10)
	m := rawMove{
		word:    []rune("HHHESTT"),
		isBlank: make([]bool, 7),
		row:     7,
		col:     4,
		dir:     dirHorizontal,
	}

	score := scoreMove(board, m)
	// 4 new tiles placed — NOT 7, so no bonus
	// H(7,4)=3 + H(7,5)=3 + H(7,6 existing)=3 + E(7,7 existing)=1 + S(7,8 existing)=1 + T(7,9)=1 + T(7,10)=1 = 13
	if score != 13 {
		t.Errorf("expected score 13 (no all-tiles bonus) with only 4 new tiles, got %d", score)
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
