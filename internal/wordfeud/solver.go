package wordfeud

import (
	"sort"
	"strings"
	"time"
)

const (
	dirHorizontal = 0
	dirVertical   = 1
	allTilesBonus = 40
	maxRackSize   = 7
	maxSolveResults = 200
)

// ScoredMove is a valid move with score and position.
type ScoredMove struct {
	Word       string `json:"word"`
	Row        int    `json:"row"`
	Col        int    `json:"col"`
	Direction  string `json:"direction"`
	Score      int    `json:"score"`
	TilesUsed  int    `json:"tiles_used"`
	BlankTiles []int  `json:"blank_tiles,omitempty"`
}

// SolveResult holds the solver output.
type SolveResult struct {
	Moves     []ScoredMove `json:"moves"`
	ElapsedMs int64        `json:"elapsed_ms"`
}

type rackState struct {
	tiles  map[rune]int
	blanks int
}

type crossCheck struct {
	any     bool          // true = any letter is valid (no perpendicular constraints)
	allowed map[rune]bool // set of valid letters when any==false
}

type rawMove struct {
	word    []rune
	isBlank []bool // true for positions where a blank tile from the rack is used
	row     int
	col     int // start column
	dir     int
}

// rowMoveGen generates moves for a single row using the Appel-Jacobson algorithm.
type rowMoveGen struct {
	board   *SolverBoard
	trie    *Trie
	rack    rackState
	checks  *[BoardSize][BoardSize]crossCheck
	anchors *[BoardSize][BoardSize]bool
	row     int
	moves   []rawMove
}

// Solve finds all valid moves on the board with the given rack tiles.
// Returns moves sorted by score descending, capped at 200 results.
func Solve(board *SolverBoard, rackStr string, trie *Trie) *SolveResult {
	start := time.Now()
	rack := parseRack(rackStr)

	var allMoves []rawMove

	// Horizontal moves
	hChecks := computeCrossChecks(board, trie)
	hAnchors := computeAnchors(board)
	for r := 0; r < BoardSize; r++ {
		gen := &rowMoveGen{
			board:   board,
			trie:    trie,
			rack:    copyRack(rack),
			checks:  &hChecks,
			anchors: &hAnchors,
			row:     r,
		}
		gen.generateRow()
		for i := range gen.moves {
			gen.moves[i].row = r
			gen.moves[i].dir = dirHorizontal
		}
		allMoves = append(allMoves, gen.moves...)
	}

	// Vertical moves via transposed board
	tb := board.Transpose()
	vChecks := computeCrossChecks(tb, trie)
	vAnchors := computeAnchors(tb)
	for r := 0; r < BoardSize; r++ {
		gen := &rowMoveGen{
			board:   tb,
			trie:    trie,
			rack:    copyRack(rack),
			checks:  &vChecks,
			anchors: &vAnchors,
			row:     r,
		}
		gen.generateRow()
		for i := range gen.moves {
			// Un-transpose coordinates
			gen.moves[i].row = gen.moves[i].col
			gen.moves[i].col = r
			gen.moves[i].dir = dirVertical
		}
		allMoves = append(allMoves, gen.moves...)
	}

	// Score all moves against the original board
	scored := make([]ScoredMove, 0, len(allMoves))
	for _, m := range allMoves {
		s := scoreMove(board, m)
		scored = append(scored, ScoredMove{
			Word:       string(m.word),
			Row:        m.row,
			Col:        m.col,
			Direction:  dirString(m.dir),
			Score:      s,
			TilesUsed:  countNewTiles(board, m),
			BlankTiles: blankIndices(m),
		})
	}

	// Sort by score descending, then word alphabetically, then by other fields for stability
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Word != scored[j].Word {
			return scored[i].Word < scored[j].Word
		}
		if scored[i].Direction != scored[j].Direction {
			return scored[i].Direction < scored[j].Direction
		}
		if scored[i].Row != scored[j].Row {
			return scored[i].Row < scored[j].Row
		}
		if scored[i].Col != scored[j].Col {
			return scored[i].Col < scored[j].Col
		}
		if scored[i].TilesUsed != scored[j].TilesUsed {
			return scored[i].TilesUsed < scored[j].TilesUsed
		}
		if len(scored[i].BlankTiles) != len(scored[j].BlankTiles) {
			return len(scored[i].BlankTiles) < len(scored[j].BlankTiles)
		}
		for k := 0; k < len(scored[i].BlankTiles); k++ {
			if scored[i].BlankTiles[k] != scored[j].BlankTiles[k] {
				return scored[i].BlankTiles[k] < scored[j].BlankTiles[k]
			}
		}
		return false
	})

	// Deduplicate: keep highest-scoring variant per (word, row, col, direction)
	type dedupKey struct {
		word string
		row  int
		col  int
		dir  string
	}
	seen := make(map[dedupKey]bool, len(scored))
	unique := make([]ScoredMove, 0, len(scored))
	for _, m := range scored {
		k := dedupKey{m.Word, m.Row, m.Col, m.Direction}
		if seen[k] {
			continue
		}
		seen[k] = true
		unique = append(unique, m)
	}
	scored = unique

	if len(scored) > maxSolveResults {
		scored = scored[:maxSolveResults]
	}

	return &SolveResult{
		Moves:     scored,
		ElapsedMs: time.Since(start).Milliseconds(),
	}
}

// --- Rack parsing ---

func parseRack(s string) rackState {
	r := rackState{tiles: make(map[rune]int)}
	for _, ch := range strings.ToUpper(s) {
		if ch == '*' {
			r.blanks++
		} else if isWordfeudLetter(ch) {
			r.tiles[ch]++
		}
	}
	return r
}

func copyRack(r rackState) rackState {
	c := rackState{tiles: make(map[rune]int, len(r.tiles)), blanks: r.blanks}
	for k, v := range r.tiles {
		c.tiles[k] = v
	}
	return c
}

func rackTotal(r rackState) int {
	n := r.blanks
	for _, v := range r.tiles {
		n += v
	}
	return n
}

// --- Anchor detection ---

func computeAnchors(board *SolverBoard) [BoardSize][BoardSize]bool {
	var anchors [BoardSize][BoardSize]bool

	if board.IsEmpty() {
		anchors[BoardSize/2][BoardSize/2] = true
		return anchors
	}

	for r := 0; r < BoardSize; r++ {
		for c := 0; c < BoardSize; c++ {
			if board.Cells[r][c].Filled {
				continue
			}
			if (r > 0 && board.Cells[r-1][c].Filled) ||
				(r < BoardSize-1 && board.Cells[r+1][c].Filled) ||
				(c > 0 && board.Cells[r][c-1].Filled) ||
				(c < BoardSize-1 && board.Cells[r][c+1].Filled) {
				anchors[r][c] = true
			}
		}
	}
	return anchors
}

// --- Cross-check computation ---

func computeCrossChecks(board *SolverBoard, trie *Trie) [BoardSize][BoardSize]crossCheck {
	var checks [BoardSize][BoardSize]crossCheck
	for r := 0; r < BoardSize; r++ {
		for c := 0; c < BoardSize; c++ {
			if board.Cells[r][c].Filled {
				continue
			}
			checks[r][c] = computeSingleCrossCheck(board, trie, r, c)
		}
	}
	return checks
}

func computeSingleCrossCheck(board *SolverBoard, trie *Trie, row, col int) crossCheck {
	hasAbove := row > 0 && board.Cells[row-1][col].Filled
	hasBelow := row < BoardSize-1 && board.Cells[row+1][col].Filled

	if !hasAbove && !hasBelow {
		return crossCheck{any: true}
	}

	// Find vertical extent of existing tiles
	top := row
	for top > 0 && board.Cells[top-1][col].Filled {
		top--
	}
	bottom := row
	for bottom < BoardSize-1 && board.Cells[bottom+1][col].Filled {
		bottom++
	}

	var prefix, suffix []rune
	for r := top; r < row; r++ {
		prefix = append(prefix, board.Cells[r][col].Letter)
	}
	for r := row + 1; r <= bottom; r++ {
		suffix = append(suffix, board.Cells[r][col].Letter)
	}

	allowed := make(map[rune]bool)
	for _, letter := range allLetters {
		word := string(prefix) + string(letter) + string(suffix)
		if trie.Contains(word) {
			allowed[letter] = true
		}
	}
	return crossCheck{allowed: allowed}
}

// --- Row-based move generation (Appel-Jacobson) ---

func (g *rowMoveGen) generateRow() {
	for c := 0; c < BoardSize; c++ {
		if !g.anchors[g.row][c] {
			continue
		}

		if c > 0 && g.board.Cells[g.row][c-1].Filled {
			// Existing tiles to the left — use them as prefix
			start := c - 1
			for start > 0 && g.board.Cells[g.row][start-1].Filled {
				start--
			}
			node := g.trie.root
			var word []rune
			var isBlank []bool
			valid := true
			for col := start; col < c; col++ {
				cell := g.board.Cells[g.row][col]
				child, ok := node.children[cell.Letter]
				if !ok {
					valid = false
					break
				}
				node = child
				word = append(word, cell.Letter)
				isBlank = append(isBlank, false) // existing tiles
			}
			if valid {
				g.extendRight(c, c, node, word, isBlank, start)
			}
		} else {
			// No existing tiles to the left — build left part from rack
			leftLimit := 0
			col := c - 1
			for col >= 0 && !g.board.Cells[g.row][col].Filled {
				leftLimit++
				col--
			}
			// Limit by rack size (need at least 1 tile for the anchor)
			rt := rackTotal(g.rack)
			if rt > 0 && leftLimit > rt-1 {
				leftLimit = rt - 1
			}
			g.leftPart(c, nil, nil, c, leftLimit)
		}
	}
}

func (g *rowMoveGen) leftPart(anchorCol int, word []rune, isBlank []bool, startCol, limit int) {
	// Walk the trie from root through the word in reading order to find the
	// correct node for extending right. The word is built by prepending letters
	// (placing tiles leftward from the anchor), so we must re-walk the trie
	// each time rather than following children in placement order.
	node := g.trie.root
	valid := true
	for _, r := range word {
		child, ok := node.children[r]
		if !ok {
			valid = false
			break
		}
		node = child
	}
	if valid {
		g.extendRight(anchorCol, anchorCol, node, word, isBlank, startCol)
	}

	if limit <= 0 {
		return
	}

	placeCol := startCol - 1
	if placeCol < 0 {
		return
	}

	// Cross-check at the column where we'd place a tile
	cc := g.checks[g.row][placeCol]

	for _, letter := range allLetters {
		if !cc.any && !cc.allowed[letter] {
			continue
		}

		// Prepend letter to the word
		newWord := make([]rune, len(word)+1)
		newWord[0] = letter
		copy(newWord[1:], word)

		newIsBlank := make([]bool, len(isBlank)+1)
		copy(newIsBlank[1:], isBlank)

		// Try using a regular tile
		if g.rack.tiles[letter] > 0 {
			g.rack.tiles[letter]--
			newIsBlank[0] = false
			g.leftPart(anchorCol, newWord, newIsBlank, placeCol, limit-1)
			g.rack.tiles[letter]++
		}

		// Try using a blank tile
		if g.rack.blanks > 0 {
			g.rack.blanks--
			blankWord := make([]rune, len(newWord))
			copy(blankWord, newWord)
			blankIsBlank := make([]bool, len(newIsBlank))
			copy(blankIsBlank, newIsBlank)
			blankIsBlank[0] = true
			g.leftPart(anchorCol, blankWord, blankIsBlank, placeCol, limit-1)
			g.rack.blanks++
		}
	}
}

func (g *rowMoveGen) extendRight(col, anchorCol int, node *TrieNode, word []rune, isBlank []bool, startCol int) {
	if col >= BoardSize {
		if node.isWord && len(word) >= 2 && col > anchorCol {
			g.recordMove(word, isBlank, startCol)
		}
		return
	}

	cell := g.board.Cells[g.row][col]

	if cell.Filled {
		// Follow existing tile on the board
		child, ok := node.children[cell.Letter]
		if ok {
			g.extendRight(col+1, anchorCol, child,
				append(word, cell.Letter),
				append(isBlank, false),
				startCol)
		}
	} else {
		// Empty cell — check if current word is complete
		if node.isWord && len(word) >= 2 && col > anchorCol {
			g.recordMove(word, isBlank, startCol)
		}

		// Try placing each valid letter
		cc := g.checks[g.row][col]
		for letter, child := range node.children {
			if !cc.any && !cc.allowed[letter] {
				continue
			}

			if g.rack.tiles[letter] > 0 {
				g.rack.tiles[letter]--
				g.extendRight(col+1, anchorCol, child,
					append(word, letter),
					append(isBlank, false),
					startCol)
				g.rack.tiles[letter]++
			}

			if g.rack.blanks > 0 {
				g.rack.blanks--
				g.extendRight(col+1, anchorCol, child,
					append(word, letter),
					append(isBlank, true),
					startCol)
				g.rack.blanks++
			}
		}
	}
}

func (g *rowMoveGen) recordMove(word []rune, isBlank []bool, startCol int) {
	wordCopy := make([]rune, len(word))
	copy(wordCopy, word)
	blankCopy := make([]bool, len(isBlank))
	copy(blankCopy, isBlank)

	g.moves = append(g.moves, rawMove{
		word:    wordCopy,
		isBlank: blankCopy,
		col:     startCol,
	})
}

// --- Scoring engine ---

func scoreMove(board *SolverBoard, m rawMove) int {
	total := 0
	mainScore := 0
	mainWordMul := 1
	tilesPlaced := 0

	dr, dc := 0, 1 // horizontal
	if m.dir == dirVertical {
		dr, dc = 1, 0
	}

	for i, letter := range m.word {
		r := m.row + i*dr
		c := m.col + i*dc

		if board.Cells[r][c].Filled {
			// Existing tile — base value, no multiplier
			if !board.Cells[r][c].IsBlank {
				mainScore += LetterValue[letter]
			}
		} else {
			// New tile from rack — apply multipliers
			tilesPlaced++
			letterVal := 0
			if !m.isBlank[i] {
				letterVal = LetterValue[letter]
			}

			bonus := board.Layout[r][c]
			switch bonus {
			case bonusDL:
				mainScore += letterVal * 2
			case bonusTL:
				mainScore += letterVal * 3
			case bonusDW, bonusCenter:
				mainScore += letterVal
				mainWordMul *= 2
			case bonusTW:
				mainScore += letterVal
				mainWordMul *= 3
			default:
				mainScore += letterVal
			}
		}
	}
	total += mainScore * mainWordMul

	// Score cross-words formed by newly placed tiles
	for i, letter := range m.word {
		r := m.row + i*dr
		c := m.col + i*dc

		if board.Cells[r][c].Filled {
			continue // existing tile, no cross-word to score
		}

		crossScore, hasCross := scoreCrossWord(board, r, c, letter, m.isBlank[i], m.dir)
		if hasCross {
			total += crossScore
		}
	}

	// 40-point bonus for using all 7 rack tiles
	if tilesPlaced >= maxRackSize {
		total += allTilesBonus
	}

	return total
}

func scoreCrossWord(board *SolverBoard, row, col int, newLetter rune, newIsBlank bool, mainDir int) (int, bool) {
	// Cross-word is perpendicular to the main direction
	var dr, dc int
	if mainDir == dirHorizontal {
		dr, dc = 1, 0 // vertical cross-word
	} else {
		dr, dc = 0, 1 // horizontal cross-word
	}

	// Check for perpendicular neighbors
	r1, c1 := row-dr, col-dc
	r2, c2 := row+dr, col+dc
	hasNeighbor := false
	if r1 >= 0 && c1 >= 0 && board.Cells[r1][c1].Filled {
		hasNeighbor = true
	}
	if r2 >= 0 && r2 < BoardSize && c2 >= 0 && c2 < BoardSize && board.Cells[r2][c2].Filled {
		hasNeighbor = true
	}
	if !hasNeighbor {
		return 0, false
	}

	// Walk to start of cross-word
	r, c := row, col
	for {
		pr, pc := r-dr, c-dc
		if pr < 0 || pc < 0 || pr >= BoardSize || pc >= BoardSize || !board.Cells[pr][pc].Filled {
			break
		}
		r, c = pr, pc
	}

	// Score the cross-word
	score := 0
	wordMul := 1

	for r >= 0 && c >= 0 && r < BoardSize && c < BoardSize {
		if r == row && c == col {
			// The newly placed tile
			letterVal := 0
			if !newIsBlank {
				letterVal = LetterValue[newLetter]
			}
			bonus := board.Layout[r][c]
			switch bonus {
			case bonusDL:
				score += letterVal * 2
			case bonusTL:
				score += letterVal * 3
			case bonusDW, bonusCenter:
				score += letterVal
				wordMul *= 2
			case bonusTW:
				score += letterVal
				wordMul *= 3
			default:
				score += letterVal
			}
		} else if board.Cells[r][c].Filled {
			if !board.Cells[r][c].IsBlank {
				score += LetterValue[board.Cells[r][c].Letter]
			}
		} else {
			break // end of cross-word
		}
		r += dr
		c += dc
	}

	return score * wordMul, true
}

// --- Helpers ---

func dirString(d int) string {
	if d == dirVertical {
		return "vertical"
	}
	return "horizontal"
}

func countNewTiles(board *SolverBoard, m rawMove) int {
	n := 0
	dr, dc := 0, 1
	if m.dir == dirVertical {
		dr, dc = 1, 0
	}
	for i := range m.word {
		r := m.row + i*dr
		c := m.col + i*dc
		if !board.Cells[r][c].Filled {
			n++
		}
	}
	return n
}

func blankIndices(m rawMove) []int {
	var blanks []int
	for i, b := range m.isBlank {
		if b {
			blanks = append(blanks, i)
		}
	}
	return blanks
}
