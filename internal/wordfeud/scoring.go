package wordfeud

// LetterValue returns the Wordfeud point value for a Norwegian letter.
// Blanks (used as any letter) score 0 and are not looked up here.
var LetterValue = map[rune]int{
	'A': 1, 'B': 4, 'C': 10, 'D': 1, 'E': 1,
	'F': 4, 'G': 3, 'H': 3, 'I': 2, 'J': 8,
	'K': 3, 'L': 2, 'M': 3, 'N': 1, 'O': 3,
	'P': 4, 'Q': 10, 'R': 1, 'S': 1, 'T': 1,
	'U': 4, 'V': 4, 'W': 8, 'X': 8, 'Y': 6,
	'Z': 10, 'Æ': 8, 'Ø': 5, 'Å': 4,
}

// TileInfo describes a tile type in the Norwegian Wordfeud bag.
type TileInfo struct {
	Letter string `json:"letter"`
	Value  int    `json:"value"`
	Count  int    `json:"count"`
}

// NorwegianTiles is the full Norwegian Wordfeud tile distribution (104 tiles total).
var NorwegianTiles = []TileInfo{
	{Letter: "A", Value: 1, Count: 7},
	{Letter: "B", Value: 4, Count: 3},
	{Letter: "C", Value: 10, Count: 1},
	{Letter: "D", Value: 1, Count: 5},
	{Letter: "E", Value: 1, Count: 9},
	{Letter: "F", Value: 4, Count: 4},
	{Letter: "G", Value: 3, Count: 4},
	{Letter: "H", Value: 3, Count: 3},
	{Letter: "I", Value: 2, Count: 5},
	{Letter: "J", Value: 8, Count: 2},
	{Letter: "K", Value: 3, Count: 4},
	{Letter: "L", Value: 2, Count: 5},
	{Letter: "M", Value: 3, Count: 3},
	{Letter: "N", Value: 1, Count: 7},
	{Letter: "O", Value: 3, Count: 4},
	{Letter: "P", Value: 4, Count: 2},
	{Letter: "Q", Value: 10, Count: 1},
	{Letter: "R", Value: 1, Count: 6},
	{Letter: "S", Value: 1, Count: 6},
	{Letter: "T", Value: 1, Count: 6},
	{Letter: "U", Value: 4, Count: 3},
	{Letter: "V", Value: 4, Count: 3},
	{Letter: "W", Value: 8, Count: 1},
	{Letter: "X", Value: 8, Count: 1},
	{Letter: "Y", Value: 6, Count: 1},
	{Letter: "Z", Value: 10, Count: 1},
	{Letter: "Æ", Value: 8, Count: 1},
	{Letter: "Ø", Value: 5, Count: 2},
	{Letter: "Å", Value: 4, Count: 2},
	{Letter: "*", Value: 0, Count: 2}, // blanks
}

// ScoreWord returns the base point value of a word (no board multipliers).
// Blank tiles score 0 and are indicated by their indices in blankPositions.
// The blankPositions set contains 0-based positions in the word that are blanks.
func ScoreWord(word string, blankPositions map[int]bool) int {
	total := 0
	i := 0
	for _, r := range word {
		if blankPositions != nil && blankPositions[i] {
			// Blank tile scores 0.
			i++
			continue
		}
		if v, ok := LetterValue[r]; ok {
			total += v
		}
		i++
	}
	return total
}

// ScoreWordSimple returns the base points for a word assuming no blanks are used.
func ScoreWordSimple(word string) int {
	total := 0
	for _, r := range word {
		if v, ok := LetterValue[r]; ok {
			total += v
		}
	}
	return total
}
