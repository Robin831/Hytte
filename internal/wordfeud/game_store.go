package wordfeud

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)


// decryptField decrypts a field, logging a warning and returning an empty
// string if decryption fails (e.g. corrupted or invalid ciphertext).
func decryptField(ciphertext string) string {
	plain, err := encryption.DecryptField(ciphertext)
	if err != nil {
		log.Printf("WARNING: decrypt failed (corrupted or invalid ciphertext): %v", err)
		return ""
	}
	return plain
}

// LocalGame represents a locally tracked Wordfeud game.
type LocalGame struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Player1     string `json:"player1"`
	Player2     string `json:"player2"`
	Score1      int    `json:"score1"`
	Score2      int    `json:"score2"`
	CurrentTurn int    `json:"current_turn"` // 1 or 2
	Board       string `json:"board"`        // JSON 15x15 board
	Rack1       string `json:"rack1"`
	Rack2       string `json:"rack2"`
	Status      string `json:"status"` // "active" or "finished"
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// LocalGameSummary is a compact listing for the game list page.
type LocalGameSummary struct {
	ID          int64  `json:"id"`
	Player1     string `json:"player1"`
	Player2     string `json:"player2"`
	Score1      int    `json:"score1"`
	Score2      int    `json:"score2"`
	CurrentTurn int    `json:"current_turn"`
	Status      string `json:"status"`
	MoveCount   int    `json:"move_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// LocalMove represents a single move in a local game's history.
type LocalMove struct {
	ID           int64  `json:"id"`
	GameID       int64  `json:"game_id"`
	MoveNumber   int    `json:"move_number"`
	PlayerTurn   int    `json:"player_turn"`
	Word         string `json:"word"`
	Position     string `json:"position"`
	Direction    string `json:"direction"`
	Score        int    `json:"score"`
	MoveType     string `json:"move_type"` // "move", "swap", "pass"
	BoardBefore  string `json:"board_before"`
	Score1Before int    `json:"score1_before"`
	Score2Before int    `json:"score2_before"`
	Rack1Before  string `json:"rack1_before"`
	Rack2Before  string `json:"rack2_before"`
	CreatedAt    string `json:"created_at"`
}

// CreateLocalGame inserts a new local game for the given user.
func CreateLocalGame(db *sql.DB, userID int64, player1, player2 string) (*LocalGame, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encP1, err := encryption.EncryptField(player1)
	if err != nil {
		return nil, fmt.Errorf("encrypt player1: %w", err)
	}
	encP2, err := encryption.EncryptField(player2)
	if err != nil {
		return nil, fmt.Errorf("encrypt player2: %w", err)
	}

	// Initialize empty 15x15 board as JSON.
	emptyBoard := make([][]interface{}, 15)
	for i := range emptyBoard {
		emptyBoard[i] = make([]interface{}, 15)
	}
	boardJSON, _ := json.Marshal(emptyBoard)

	res, err := db.Exec(
		`INSERT INTO wordfeud_games (user_id, player1, player2, score1, score2, current_turn, board_json, rack1, rack2, status, created_at, updated_at)
		 VALUES (?, ?, ?, 0, 0, 1, ?, '', '', 'active', ?, ?)`,
		userID, encP1, encP2, string(boardJSON), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert game: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return &LocalGame{
		ID:          id,
		UserID:      userID,
		Player1:     player1,
		Player2:     player2,
		Score1:      0,
		Score2:      0,
		CurrentTurn: 1,
		Board:       string(boardJSON),
		Rack1:       "",
		Rack2:       "",
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ListLocalGames returns all games for a user, sorted by most recently updated.
func ListLocalGames(db *sql.DB, userID int64) ([]LocalGameSummary, error) {
	rows, err := db.Query(
		`SELECT g.id, g.player1, g.player2, g.score1, g.score2, g.current_turn, g.status, g.created_at, g.updated_at,
		        (SELECT COUNT(*) FROM wordfeud_moves m WHERE m.game_id = g.id) AS move_count
		 FROM wordfeud_games g
		 WHERE g.user_id = ?
		 ORDER BY g.updated_at DESC, g.id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	defer rows.Close()

	var games []LocalGameSummary
	for rows.Next() {
		var g LocalGameSummary
		var encP1, encP2 string
		if err := rows.Scan(&g.ID, &encP1, &encP2, &g.Score1, &g.Score2, &g.CurrentTurn, &g.Status, &g.CreatedAt, &g.UpdatedAt, &g.MoveCount); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		g.Player1 = decryptField(encP1)
		g.Player2 = decryptField(encP2)
		games = append(games, g)
	}
	return games, rows.Err()
}

// GetLocalGame returns a single game with its full state.
func GetLocalGame(db *sql.DB, userID, gameID int64) (*LocalGame, error) {
	var g LocalGame
	var encP1, encP2, encRack1, encRack2 string
	err := db.QueryRow(
		`SELECT id, user_id, player1, player2, score1, score2, current_turn, board_json, rack1, rack2, status, created_at, updated_at
		 FROM wordfeud_games WHERE id = ? AND user_id = ?`,
		gameID, userID,
	).Scan(&g.ID, &g.UserID, &encP1, &encP2, &g.Score1, &g.Score2, &g.CurrentTurn, &g.Board, &encRack1, &encRack2, &g.Status, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	g.Player1 = decryptField(encP1)
	g.Player2 = decryptField(encP2)
	g.Rack1 = decryptField(encRack1)
	g.Rack2 = decryptField(encRack2)
	return &g, nil
}

// UpdateLocalGame updates the game state (board, scores, turn, racks, status).
func UpdateLocalGame(db *sql.DB, userID int64, g *LocalGame) error {
	now := time.Now().UTC().Format(time.RFC3339)

	encP1, err := encryption.EncryptField(g.Player1)
	if err != nil {
		return fmt.Errorf("encrypt player1: %w", err)
	}
	encP2, err := encryption.EncryptField(g.Player2)
	if err != nil {
		return fmt.Errorf("encrypt player2: %w", err)
	}
	encR1, err := encryption.EncryptField(g.Rack1)
	if err != nil {
		return fmt.Errorf("encrypt rack1: %w", err)
	}
	encR2, err := encryption.EncryptField(g.Rack2)
	if err != nil {
		return fmt.Errorf("encrypt rack2: %w", err)
	}

	_, err = db.Exec(
		`UPDATE wordfeud_games SET player1=?, player2=?, score1=?, score2=?, current_turn=?, board_json=?, rack1=?, rack2=?, status=?, updated_at=?
		 WHERE id = ? AND user_id = ?`,
		encP1, encP2, g.Score1, g.Score2, g.CurrentTurn, g.Board, encR1, encR2, g.Status, now,
		g.ID, userID,
	)
	return err
}

// DeleteLocalGame removes a game and its move history (CASCADE).
func DeleteLocalGame(db *sql.DB, userID, gameID int64) error {
	res, err := db.Exec(`DELETE FROM wordfeud_games WHERE id = ? AND user_id = ?`, gameID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordMove records a move and saves a snapshot for undo.
func RecordMove(db *sql.DB, gameID int64, mv LocalMove) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Encrypt sensitive text fields in the move snapshot.
	encWord, err := encryption.EncryptField(mv.Word)
	if err != nil {
		return 0, fmt.Errorf("encrypt word: %w", err)
	}
	encBoardBefore, err := encryption.EncryptField(mv.BoardBefore)
	if err != nil {
		return 0, fmt.Errorf("encrypt board_before: %w", err)
	}
	encRack1Before, err := encryption.EncryptField(mv.Rack1Before)
	if err != nil {
		return 0, fmt.Errorf("encrypt rack1_before: %w", err)
	}
	encRack2Before, err := encryption.EncryptField(mv.Rack2Before)
	if err != nil {
		return 0, fmt.Errorf("encrypt rack2_before: %w", err)
	}

	res, err := db.Exec(
		`INSERT INTO wordfeud_moves (game_id, move_number, player_turn, word, position, direction, score, move_type, board_before, score1_before, score2_before, rack1_before, rack2_before, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gameID, mv.MoveNumber, mv.PlayerTurn, encWord, mv.Position, mv.Direction, mv.Score, mv.MoveType,
		encBoardBefore, mv.Score1Before, mv.Score2Before, encRack1Before, encRack2Before, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert move: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// RecordMoveAndUpdateGame atomically truncates the redo stack, inserts a move,
// and updates the game state (score1, score2, current_turn) in a single transaction.
// This prevents partial writes where a move is persisted but the game state is not.
func RecordMoveAndUpdateGame(dbConn *sql.DB, gameID int64, mv LocalMove, newScore1, newScore2, newTurn int) (int64, error) {
	tx, err := dbConn.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Truncate any future moves (redo stack).
	if _, err := tx.Exec(`DELETE FROM wordfeud_moves WHERE game_id = ? AND move_number > ?`, gameID, mv.MoveNumber-1); err != nil {
		return 0, fmt.Errorf("truncate redo stack: %w", err)
	}

	// Encrypt sensitive text fields.
	encWord, err := encryption.EncryptField(mv.Word)
	if err != nil {
		return 0, fmt.Errorf("encrypt word: %w", err)
	}
	encBoardBefore, err := encryption.EncryptField(mv.BoardBefore)
	if err != nil {
		return 0, fmt.Errorf("encrypt board_before: %w", err)
	}
	encRack1Before, err := encryption.EncryptField(mv.Rack1Before)
	if err != nil {
		return 0, fmt.Errorf("encrypt rack1_before: %w", err)
	}
	encRack2Before, err := encryption.EncryptField(mv.Rack2Before)
	if err != nil {
		return 0, fmt.Errorf("encrypt rack2_before: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(
		`INSERT INTO wordfeud_moves (game_id, move_number, player_turn, word, position, direction, score, move_type, board_before, score1_before, score2_before, rack1_before, rack2_before, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gameID, mv.MoveNumber, mv.PlayerTurn, encWord, mv.Position, mv.Direction, mv.Score, mv.MoveType,
		encBoardBefore, mv.Score1Before, mv.Score2Before, encRack1Before, encRack2Before, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert move: %w", err)
	}
	moveID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE wordfeud_games SET score1=?, score2=?, current_turn=?, updated_at=? WHERE id=?`,
		newScore1, newScore2, newTurn, now, gameID,
	)
	if err != nil {
		return 0, fmt.Errorf("update game state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return moveID, nil
}

// ListMoves returns all moves for a game, ordered by move number.
func ListMoves(db *sql.DB, gameID int64) ([]LocalMove, error) {
	rows, err := db.Query(
		`SELECT id, game_id, move_number, player_turn, word, position, direction, score, move_type, board_before, score1_before, score2_before, rack1_before, rack2_before, created_at
		 FROM wordfeud_moves WHERE game_id = ? ORDER BY move_number ASC`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("list moves: %w", err)
	}
	defer rows.Close()

	var moves []LocalMove
	for rows.Next() {
		var m LocalMove
		var encWord, encBoard, encRack1, encRack2 string
		if err := rows.Scan(&m.ID, &m.GameID, &m.MoveNumber, &m.PlayerTurn, &encWord, &m.Position, &m.Direction, &m.Score, &m.MoveType, &encBoard, &m.Score1Before, &m.Score2Before, &encRack1, &encRack2, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan move: %w", err)
		}
		m.Word = decryptField(encWord)
		m.BoardBefore = decryptField(encBoard)
		m.Rack1Before = decryptField(encRack1)
		m.Rack2Before = decryptField(encRack2)
		moves = append(moves, m)
	}
	return moves, rows.Err()
}

// DeleteMovesAfter removes all moves with move_number > n for undo/redo truncation.
func DeleteMovesAfter(db *sql.DB, gameID int64, afterMoveNumber int) error {
	_, err := db.Exec(
		`DELETE FROM wordfeud_moves WHERE game_id = ? AND move_number > ?`,
		gameID, afterMoveNumber,
	)
	return err
}

// UndoLastMove restores the game state to before the last move.
// Returns the undone move, or sql.ErrNoRows if no moves exist.
// Uses a transaction to ensure atomicity across the read, update, and delete.
func UndoLastMove(dbConn *sql.DB, userID, gameID int64) (*LocalMove, error) {
	tx, err := dbConn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Verify game ownership before proceeding.
	var ownerID int64
	err = tx.QueryRow(`SELECT user_id FROM wordfeud_games WHERE id = ?`, gameID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return nil, ErrGameNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerID != userID {
		return nil, ErrGameNotFound
	}

	// Get the last move.
	var m LocalMove
	var encWord, encBoard, encRack1, encRack2 string
	err = tx.QueryRow(
		`SELECT id, game_id, move_number, player_turn, word, position, direction, score, move_type, board_before, score1_before, score2_before, rack1_before, rack2_before, created_at
		 FROM wordfeud_moves WHERE game_id = ? ORDER BY move_number DESC LIMIT 1`,
		gameID,
	).Scan(&m.ID, &m.GameID, &m.MoveNumber, &m.PlayerTurn, &encWord, &m.Position, &m.Direction, &m.Score, &m.MoveType, &encBoard, &m.Score1Before, &m.Score2Before, &encRack1, &encRack2, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.Word = decryptField(encWord)
	m.BoardBefore = decryptField(encBoard)
	m.Rack1Before = decryptField(encRack1)
	m.Rack2Before = decryptField(encRack2)

	// Restore the game state from the snapshot.
	now := time.Now().UTC().Format(time.RFC3339)
	encR1, err := encryption.EncryptField(m.Rack1Before)
	if err != nil {
		return nil, fmt.Errorf("encrypt rack1: %w", err)
	}
	encR2, err := encryption.EncryptField(m.Rack2Before)
	if err != nil {
		return nil, fmt.Errorf("encrypt rack2: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE wordfeud_games SET board_json=?, score1=?, score2=?, current_turn=?, rack1=?, rack2=?, updated_at=?
		 WHERE id = ? AND user_id = ?`,
		m.BoardBefore, m.Score1Before, m.Score2Before, m.PlayerTurn, encR1, encR2, now,
		gameID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("restore game state: %w", err)
	}

	// Delete the undone move.
	_, err = tx.Exec(`DELETE FROM wordfeud_moves WHERE id = ?`, m.ID)
	if err != nil {
		return nil, fmt.Errorf("delete undone move: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit undo: %w", err)
	}

	return &m, nil
}
