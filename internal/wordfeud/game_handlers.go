package wordfeud

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ListLocalGamesHandler returns all local games for the authenticated user.
// GET /api/wordfeud/local-games
func ListLocalGamesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		games, err := ListLocalGames(db, user.ID)
		if err != nil {
			log.Printf("Failed to list local games for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list games"})
			return
		}
		if games == nil {
			games = []LocalGameSummary{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"games": games})
	}
}

// CreateLocalGameHandler creates a new local game.
// POST /api/wordfeud/local-games
func CreateLocalGameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var body struct {
			Player1 string `json:"player1"`
			Player2 string `json:"player2"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if body.Player1 == "" || body.Player2 == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player1 and player2 are required"})
			return
		}
		if len(body.Player1) > 50 || len(body.Player2) > 50 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player names must be 50 characters or fewer"})
			return
		}

		game, err := CreateLocalGame(db, user.ID, body.Player1, body.Player2)
		if err != nil {
			log.Printf("Failed to create local game for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create game"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"game": game})
	}
}

// GetLocalGameHandler returns a single local game with full state and move history.
// GET /api/wordfeud/local-games/{id}
func GetLocalGameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		game, err := GetLocalGame(db, user.ID, gameID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
			return
		}
		if err != nil {
			log.Printf("Failed to get local game %d for user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get game"})
			return
		}

		moves, err := ListMoves(db, gameID)
		if err != nil {
			log.Printf("Failed to list moves for game %d: %v", gameID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list moves"})
			return
		}
		if moves == nil {
			moves = []LocalMove{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"game": game, "moves": moves})
	}
}

// UpdateLocalGameHandler updates the game state (board, scores, turn, racks, status).
// PUT /api/wordfeud/local-games/{id}
func UpdateLocalGameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		// Verify ownership.
		existing, err := GetLocalGame(db, user.ID, gameID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
			return
		}
		if err != nil {
			log.Printf("Failed to get local game %d for user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get game"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64 KiB for board JSON
		var body struct {
			Board       *string `json:"board"`
			Score1      *int    `json:"score1"`
			Score2      *int    `json:"score2"`
			CurrentTurn *int    `json:"current_turn"`
			Rack1       *string `json:"rack1"`
			Rack2       *string `json:"rack2"`
			Status      *string `json:"status"`
			Player1     *string `json:"player1"`
			Player2     *string `json:"player2"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if body.Board != nil {
			existing.Board = *body.Board
		}
		if body.Score1 != nil {
			existing.Score1 = *body.Score1
		}
		if body.Score2 != nil {
			existing.Score2 = *body.Score2
		}
		if body.CurrentTurn != nil {
			if *body.CurrentTurn != 1 && *body.CurrentTurn != 2 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current_turn must be 1 or 2"})
				return
			}
			existing.CurrentTurn = *body.CurrentTurn
		}
		if body.Rack1 != nil {
			existing.Rack1 = *body.Rack1
		}
		if body.Rack2 != nil {
			existing.Rack2 = *body.Rack2
		}
		if body.Status != nil {
			if *body.Status != "active" && *body.Status != "finished" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be 'active' or 'finished'"})
				return
			}
			existing.Status = *body.Status
		}
		if body.Player1 != nil {
			trimmed := strings.TrimSpace(*body.Player1)
			if trimmed == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player1 must not be empty"})
				return
			}
			if len(trimmed) > 50 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player names must be 50 characters or fewer"})
				return
			}
			existing.Player1 = trimmed
		}
		if body.Player2 != nil {
			trimmed := strings.TrimSpace(*body.Player2)
			if trimmed == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player2 must not be empty"})
				return
			}
			if len(trimmed) > 50 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player names must be 50 characters or fewer"})
				return
			}
			existing.Player2 = trimmed
		}

		if err := UpdateLocalGame(db, user.ID, existing); err != nil {
			log.Printf("Failed to update local game %d for user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update game"})
			return
		}

		// Refresh updated_at so the response reflects what was written to the DB.
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		writeJSON(w, http.StatusOK, map[string]any{"game": existing})
	}
}

// DeleteLocalGameHandler deletes a local game and its move history.
// DELETE /api/wordfeud/local-games/{id}
func DeleteLocalGameHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		if err := DeleteLocalGame(db, user.ID, gameID); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
			return
		} else if err != nil {
			log.Printf("Failed to delete local game %d for user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete game"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// RecordMoveHandler records a new move for a local game.
// POST /api/wordfeud/local-games/{id}/moves
func RecordMoveHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		// Verify ownership and capture current game state.
		game, err := GetLocalGame(db, user.ID, gameID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
			return
		}
		if err != nil {
			log.Printf("Failed to get local game %d for user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get game"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var body struct {
			PlayerTurn  int    `json:"player_turn"`
			Word        string `json:"word"`
			Position    string `json:"position"`
			Direction   string `json:"direction"`
			Score       int    `json:"score"`
			MoveType    string `json:"move_type"`
			BoardBefore string `json:"board_before"`
			Rack1Before string `json:"rack1_before"`
			Rack2Before string `json:"rack2_before"`
			NewScore1   int    `json:"new_score1"`
			NewScore2   int    `json:"new_score2"`
			NewTurn     int    `json:"new_turn"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if body.MoveType == "" {
			body.MoveType = "move"
		}
		switch body.MoveType {
		case "move", "pass", "swap":
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "move_type must be one of: move, pass, swap"})
			return
		}
		if body.PlayerTurn != 1 && body.PlayerTurn != 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player_turn must be 1 or 2"})
			return
		}
		if body.PlayerTurn != game.CurrentTurn {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "player_turn does not match current game state"})
			return
		}
		if body.NewTurn != 1 && body.NewTurn != 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new_turn must be 1 or 2"})
			return
		}

		// Derive the next move number from the DB to prevent client-supplied values
		// from corrupting history or truncating the redo stack unexpectedly.
		var maxMoveNum int
		if err := db.QueryRow(`SELECT COALESCE(MAX(move_number), 0) FROM wordfeud_moves WHERE game_id = ?`, gameID).Scan(&maxMoveNum); err != nil {
			log.Printf("Failed to get max move number for game %d: %v", gameID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record move"})
			return
		}

		mv := LocalMove{
			GameID:       gameID,
			MoveNumber:   maxMoveNum + 1,
			PlayerTurn:   body.PlayerTurn,
			Word:         body.Word,
			Position:     body.Position,
			Direction:    body.Direction,
			Score:        body.Score,
			MoveType:     body.MoveType,
			BoardBefore:  body.BoardBefore,
			Score1Before: game.Score1,
			Score2Before: game.Score2,
			Rack1Before:  body.Rack1Before,
			Rack2Before:  body.Rack2Before,
		}

		// Record the move and update game state atomically.
		moveID, err := RecordMoveAndUpdateGame(db, gameID, mv, body.NewScore1, body.NewScore2, body.NewTurn)
		if err != nil {
			log.Printf("Failed to record move for game %d: %v", gameID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record move"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"move_id": moveID})
	}
}

// UndoMoveHandler undoes the last move for a local game.
// POST /api/wordfeud/local-games/{id}/undo
func UndoMoveHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
			return
		}

		mv, err := UndoLastMove(db, user.ID, gameID)
		if err == ErrGameNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
			return
		}
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no moves to undo"})
			return
		}
		if err != nil {
			log.Printf("Failed to undo move for game %d user %d: %v", gameID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to undo move"})
			return
		}

		// Return the updated game state.
		game, err := GetLocalGame(db, user.ID, gameID)
		if err != nil {
			log.Printf("Failed to get game after undo %d: %v", gameID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get game after undo"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"game": game, "undone_move": mv})
	}
}
