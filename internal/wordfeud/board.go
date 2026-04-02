package wordfeud

// BoardSize is the standard Wordfeud board dimension (15x15).
const BoardSize = 15

// Bonus square types.
const (
	bonusNone   = 0
	bonusDL     = 1 // Double Letter
	bonusTL     = 2 // Triple Letter
	bonusDW     = 3 // Double Word
	bonusTW     = 4 // Triple Word
	bonusCenter = 5 // Center star (acts as Double Word)
)

// StandardLayout is the standard Wordfeud board multiplier layout.
// Matches the frontend BOARD_LAYOUT in WordfeudBoard.tsx.
var StandardLayout = [BoardSize][BoardSize]int{
	{4, 0, 0, 1, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 4},
	{0, 3, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 3, 0},
	{0, 0, 3, 0, 0, 0, 1, 0, 1, 0, 0, 0, 3, 0, 0},
	{1, 0, 0, 3, 0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 1},
	{0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0},
	{0, 2, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 2, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0, 0, 1, 0, 0},
	{4, 0, 0, 1, 0, 0, 0, 5, 0, 0, 0, 1, 0, 0, 4},
	{0, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0, 0, 1, 0, 0},
	{0, 2, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 2, 0},
	{0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0},
	{1, 0, 0, 3, 0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 1},
	{0, 0, 3, 0, 0, 0, 1, 0, 1, 0, 0, 0, 3, 0, 0},
	{0, 3, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 3, 0},
	{4, 0, 0, 1, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 4},
}

// SolverCell represents a single cell on the solver board.
type SolverCell struct {
	Letter  rune
	IsBlank bool
	Filled  bool
}

// SolverBoard holds the board state and multiplier layout for the solver.
type SolverBoard struct {
	Cells  [BoardSize][BoardSize]SolverCell
	Layout [BoardSize][BoardSize]int
}

// NewSolverBoard creates a board with the standard multiplier layout.
func NewSolverBoard() *SolverBoard {
	return &SolverBoard{Layout: StandardLayout}
}

// Set places a tile on the board.
func (b *SolverBoard) Set(row, col int, letter rune, isBlank bool) {
	b.Cells[row][col] = SolverCell{Letter: letter, IsBlank: isBlank, Filled: true}
}

// IsEmpty returns true if no tiles have been placed.
func (b *SolverBoard) IsEmpty() bool {
	for r := 0; r < BoardSize; r++ {
		for c := 0; c < BoardSize; c++ {
			if b.Cells[r][c].Filled {
				return false
			}
		}
	}
	return true
}

// Transpose returns a new board with rows and columns swapped.
func (b *SolverBoard) Transpose() *SolverBoard {
	t := &SolverBoard{}
	for r := 0; r < BoardSize; r++ {
		for c := 0; c < BoardSize; c++ {
			t.Cells[c][r] = b.Cells[r][c]
			t.Layout[c][r] = b.Layout[r][c]
		}
	}
	return t
}

// allLetters lists all valid Wordfeud letters (Norwegian).
var allLetters = []rune{
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'Æ', 'Ø', 'Å',
}
