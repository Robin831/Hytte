package wordfeud

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

type solveRequestCell struct {
	Letter  string `json:"letter"`
	IsBlank bool   `json:"is_blank"`
}

type solveRequest struct {
	Board [][]*solveRequestCell `json:"board"`
	Rack  string                `json:"rack"`
	// Sort selects the result ordering: "score" (default), "least_vowels",
	// "most_tiles", or "block".
	Sort string `json:"sort"`
	// OpponentRack holds the opponent's known tiles. Required for the "block"
	// sort, ignored otherwise.
	OpponentRack string `json:"opponent_rack"`
}

// validSortModes is the set of accepted sort values for the solve endpoint.
var validSortModes = map[string]bool{
	"":              true, // defaults to score
	SortScore:       true,
	SortLeastVowels: true,
	SortMostTiles:   true,
	SortBlock:       true,
}

// SolveHandler returns an http.HandlerFunc for POST /api/wordfeud/solve.
// Accepts board state + rack, returns ranked moves with scores.
func SolveHandler(dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)

		var req solveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		// Validate board dimensions
		if len(req.Board) != BoardSize {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "board must be 15x15"})
			return
		}
		for _, row := range req.Board {
			if len(row) != BoardSize {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "board must be 15x15"})
				return
			}
		}

		// Validate rack
		rack := strings.ToUpper(strings.TrimSpace(req.Rack))
		rackLen := utf8.RuneCountInString(rack)
		if rackLen == 0 || rackLen > 7 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rack must have 1-7 tiles"})
			return
		}
		for _, ch := range rack {
			if ch != '*' && !isWordfeudLetter(ch) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rack contains invalid characters"})
				return
			}
		}

		// Validate sort mode
		if !validSortModes[req.Sort] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sort mode"})
			return
		}

		// The block sort needs the opponent's known tiles
		oppRack := strings.ToUpper(strings.TrimSpace(req.OpponentRack))
		if req.Sort == SortBlock {
			oppLen := utf8.RuneCountInString(oppRack)
			if oppLen == 0 || oppLen > 7 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "block sort requires opponent_rack of 1-7 tiles"})
				return
			}
			for _, ch := range oppRack {
				if ch != '*' && !isWordfeudLetter(ch) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent_rack contains invalid characters"})
					return
				}
			}
		}

		// Parse board
		board := NewSolverBoard()
		for row := 0; row < BoardSize; row++ {
			for col := 0; col < BoardSize; col++ {
				cell := req.Board[row][col]
				if cell == nil || cell.Letter == "" {
					continue
				}
				runes := []rune(strings.ToUpper(cell.Letter))
				if len(runes) != 1 || !isWordfeudLetter(runes[0]) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid letter %q at row %d, col %d", cell.Letter, row, col)})
					return
				}
				board.Set(row, col, runes[0], cell.IsBlank)
			}
		}

		// Load dictionary
		trie, err := dict.Trie()
		if err != nil {
			log.Printf("wordfeud: failed to load dictionary from %q: %v", dict.path, err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "dictionary not available"})
			return
		}

		result := SolveSorted(board, rack, trie, req.Sort, oppRack)
		writeJSON(w, http.StatusOK, result)
	}
}
