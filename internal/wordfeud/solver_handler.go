package wordfeud

import (
	"encoding/json"
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
}

// SolveHandler returns an http.HandlerFunc for POST /api/wordfeud/solve.
// Accepts board state + rack, returns ranked moves with scores.
func SolveHandler(dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
					continue
				}
				board.Set(row, col, runes[0], cell.IsBlank)
			}
		}

		// Load dictionary
		trie, err := dict.Trie()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "dictionary not available"})
			return
		}

		result := Solve(board, rack, trie)
		writeJSON(w, http.StatusOK, result)
	}
}
