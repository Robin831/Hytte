package wordfeud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Playing moves through Wordfeud's (unofficial) API, the same one Hytte
// reads games from:
//
//	POST /game/{id}/move/   {"move": [[x, y, "L", blank]…], "ruleset": n, "words": ["MAIN"]}
//	POST /game/{id}/pass/
//	POST /game/{id}/swap/   {"tiles": ["A", "B", ""]}   ("" is a blank)
//	POST /game/{id}/resign/
//
// x is the column and y the row — the same [x, y] order the API uses for
// the tiles it returns (which Hytte reads into Board[x][y] and renders as
// row y, column x).

// PlacedTile is a tile the player puts on the board, in the board's row /
// column as Hytte renders it.
type PlacedTile struct {
	Row    int    `json:"row"`
	Col    int    `json:"col"`
	Letter string `json:"letter"`
	Blank  bool   `json:"blank"`
}

// MoveResult is what Wordfeud reports after a move.
type MoveResult struct {
	Points   int    `json:"points"`
	MainWord string `json:"main_word"`
}

// PlaceTiles plays a word.
func (c *Client) PlaceTiles(sessionToken string, gameID int64, ruleset int, tiles []PlacedTile, words []string) (*MoveResult, error) {
	move := make([][]any, len(tiles))
	for i, t := range tiles {
		move[i] = []any{t.Col, t.Row, t.Letter, t.Blank}
	}
	content, err := c.post(sessionToken, fmt.Sprintf("/game/%d/move/", gameID), map[string]any{
		"move":    move,
		"ruleset": ruleset,
		"words":   words,
	})
	if err != nil {
		return nil, err
	}
	var res MoveResult
	_ = json.Unmarshal(content, &res) // the reply shape varies; points are a bonus
	return &res, nil
}

// Pass passes the turn.
func (c *Client) Pass(sessionToken string, gameID int64) error {
	_, err := c.post(sessionToken, fmt.Sprintf("/game/%d/pass/", gameID), nil)
	return err
}

// Swap exchanges rack tiles with the bag ("" for a blank).
func (c *Client) Swap(sessionToken string, gameID int64, letters []string) error {
	_, err := c.post(sessionToken, fmt.Sprintf("/game/%d/swap/", gameID), map[string]any{"tiles": letters})
	return err
}

// Resign gives the game up.
func (c *Client) Resign(sessionToken string, gameID int64) error {
	_, err := c.post(sessionToken, fmt.Sprintf("/game/%d/resign/", gameID), nil)
	return err
}

// --- checking a placement -------------------------------------------------

// PreviewWord is one word a placement forms.
type PreviewWord struct {
	Word  string `json:"word"`
	Valid bool   `json:"valid"`
}

// MovePreview describes a placement before it's played. Problem is a code
// for why it can't be played as laid out ("" when it can):
// no_tiles, too_many_tiles, off_board, occupied, duplicate, not_in_line,
// gap, not_connected, first_move_center, too_short, bad_letter, not_in_rack.
type MovePreview struct {
	Problem  string        `json:"problem"`
	MainWord string        `json:"main_word"`
	Words    []PreviewWord `json:"words"`
	Score    int           `json:"score"`
	AllValid bool          `json:"all_valid"`
}

// boardFromGame turns a game into the solver's board, rows and columns as
// rendered (Board[x][y] holds row y, column x).
func boardFromGame(gs *GameState) *SolverBoard {
	b := NewSolverBoard()
	for x := 0; x < BoardSize; x++ {
		for y := 0; y < BoardSize; y++ {
			if t := gs.Board[x][y]; t != nil && t.Letter != "" {
				b.Set(y, x, []rune(strings.ToUpper(t.Letter))[0], t.IsWild)
			}
		}
	}
	return b
}

// rackCounts counts the rack's letters; blanks under "*".
func rackCounts(gs *GameState) map[string]int {
	n := map[string]int{}
	for _, t := range gs.Rack {
		if t.IsWild || t.Letter == "" {
			n["*"]++
		} else {
			n[strings.ToUpper(t.Letter)]++
		}
	}
	return n
}

func validLetter(s string) bool {
	r := []rune(s)
	if len(r) != 1 {
		return false
	}
	_, ok := LetterValue[r[0]]
	return ok
}

// Preview checks a placement against the game: shape, rack, the words it
// forms (checked against trie when given) and its score.
func Preview(gs *GameState, tiles []PlacedTile, trie *Trie) MovePreview {
	p := MovePreview{Words: []PreviewWord{}}
	if len(tiles) == 0 {
		p.Problem = "no_tiles"
		return p
	}
	if len(tiles) > maxRackSize {
		p.Problem = "too_many_tiles"
		return p
	}
	board := boardFromGame(gs)
	seen := map[[2]int]bool{}
	need := map[string]int{}
	for i := range tiles {
		t := &tiles[i]
		t.Letter = strings.ToUpper(strings.TrimSpace(t.Letter))
		if t.Row < 0 || t.Row >= BoardSize || t.Col < 0 || t.Col >= BoardSize {
			p.Problem = "off_board"
			return p
		}
		if !validLetter(t.Letter) {
			p.Problem = "bad_letter"
			return p
		}
		if board.Cells[t.Row][t.Col].Filled {
			p.Problem = "occupied"
			return p
		}
		if seen[[2]int{t.Row, t.Col}] {
			p.Problem = "duplicate"
			return p
		}
		seen[[2]int{t.Row, t.Col}] = true
		if t.Blank {
			need["*"]++
		} else {
			need[t.Letter]++
		}
	}
	have := rackCounts(gs)
	for l, n := range need {
		if have[l] < n {
			p.Problem = "not_in_rack"
			return p
		}
	}

	// All in one row or one column.
	sameRow, sameCol := true, true
	for _, t := range tiles[1:] {
		sameRow = sameRow && t.Row == tiles[0].Row
		sameCol = sameCol && t.Col == tiles[0].Col
	}
	if !sameRow && !sameCol {
		p.Problem = "not_in_line"
		return p
	}

	firstMove := board.IsEmpty()
	after := *board
	for _, t := range tiles {
		after.Set(t.Row, t.Col, []rune(t.Letter)[0], t.Blank)
	}

	// The main direction: the line the tiles share; for a single tile, the
	// direction in which it forms a word (horizontal if both).
	dir := dirHorizontal
	if len(tiles) == 1 {
		if runLength(&after, tiles[0].Row, tiles[0].Col, dirHorizontal) < 2 && runLength(&after, tiles[0].Row, tiles[0].Col, dirVertical) >= 2 {
			dir = dirVertical
		}
	} else if sameCol {
		dir = dirVertical
	}

	// The main word must cover every new tile with no gaps.
	startR, startC, word, blanks := wordThrough(&after, board, tiles[0].Row, tiles[0].Col, dir)
	for _, t := range tiles {
		if !onLine(startR, startC, len(word), dir, t.Row, t.Col) {
			p.Problem = "gap"
			return p
		}
	}
	if len(word) < 2 {
		p.Problem = "too_short"
		return p
	}

	if firstMove {
		if !seen[[2]int{7, 7}] {
			p.Problem = "first_move_center"
			return p
		}
	} else if !touchesExisting(board, tiles) {
		p.Problem = "not_connected"
		return p
	}

	p.MainWord = string(word)
	p.Words = append(p.Words, PreviewWord{Word: p.MainWord})
	cross := dirVertical
	if dir == dirVertical {
		cross = dirHorizontal
	}
	for _, t := range tiles {
		if runLength(&after, t.Row, t.Col, cross) >= 2 {
			_, _, w, _ := wordThrough(&after, board, t.Row, t.Col, cross)
			p.Words = append(p.Words, PreviewWord{Word: string(w)})
		}
	}
	p.AllValid = true
	for i := range p.Words {
		p.Words[i].Valid = trie == nil || trie.Contains(p.Words[i].Word)
		p.AllValid = p.AllValid && p.Words[i].Valid
	}
	p.Score = scoreMove(board, rawMove{word: word, isBlank: blanks, row: startR, col: startC, dir: dir})
	return p
}

func step(dir int) (int, int) {
	if dir == dirVertical {
		return 1, 0
	}
	return 0, 1
}

// runLength is the length of the run of filled cells through (r, c).
func runLength(b *SolverBoard, r, c, dir int) int {
	dr, dc := step(dir)
	n := 1
	for i := 1; ; i++ {
		rr, cc := r-i*dr, c-i*dc
		if rr < 0 || cc < 0 || !b.Cells[rr][cc].Filled {
			break
		}
		n++
	}
	for i := 1; ; i++ {
		rr, cc := r+i*dr, c+i*dc
		if rr >= BoardSize || cc >= BoardSize || !b.Cells[rr][cc].Filled {
			break
		}
		n++
	}
	return n
}

// wordThrough returns where the run through (r, c) starts, its letters and
// which of them are blanks newly placed (existing tiles score from before).
func wordThrough(after, before *SolverBoard, r, c, dir int) (int, int, []rune, []bool) {
	dr, dc := step(dir)
	for r-dr >= 0 && c-dc >= 0 && after.Cells[r-dr][c-dc].Filled {
		r, c = r-dr, c-dc
	}
	sr, sc := r, c
	var word []rune
	var blanks []bool
	for r < BoardSize && c < BoardSize && after.Cells[r][c].Filled {
		word = append(word, after.Cells[r][c].Letter)
		blanks = append(blanks, !before.Cells[r][c].Filled && after.Cells[r][c].IsBlank)
		r, c = r+dr, c+dc
	}
	return sr, sc, word, blanks
}

func onLine(sr, sc, n, dir, r, c int) bool {
	if dir == dirVertical {
		return c == sc && r >= sr && r < sr+n
	}
	return r == sr && c >= sc && c < sc+n
}

func touchesExisting(board *SolverBoard, tiles []PlacedTile) bool {
	for _, t := range tiles {
		for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			r, c := t.Row+d[0], t.Col+d[1]
			if r >= 0 && r < BoardSize && c >= 0 && c < BoardSize && board.Cells[r][c].Filled {
				return true
			}
		}
	}
	return false
}
