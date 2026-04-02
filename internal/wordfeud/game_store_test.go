package wordfeud

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCreateAndListLocalGames(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	// Create two games.
	g1, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game 1: %v", err)
	}
	if g1.Player1 != "Alice" || g1.Player2 != "Bob" {
		t.Errorf("got players %q/%q, want Alice/Bob", g1.Player1, g1.Player2)
	}
	if g1.Status != "active" {
		t.Errorf("got status %q, want active", g1.Status)
	}

	g2, err := CreateLocalGame(database, user.ID, "Carol", "Dave")
	if err != nil {
		t.Fatalf("create game 2: %v", err)
	}

	// List games.
	games, err := ListLocalGames(database, user.ID)
	if err != nil {
		t.Fatalf("list games: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games, want 2", len(games))
	}
	// Most recently updated first.
	if games[0].ID != g2.ID {
		t.Errorf("first game ID=%d, want %d", games[0].ID, g2.ID)
	}
}

func TestGetLocalGame(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	got, err := GetLocalGame(database, user.ID, g.ID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if got.Player1 != "Alice" || got.Player2 != "Bob" {
		t.Errorf("got players %q/%q, want Alice/Bob", got.Player1, got.Player2)
	}

	// Wrong user should not find the game.
	_, err = GetLocalGame(database, 9999, g.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestUpdateLocalGame(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	g.Score1 = 42
	g.Score2 = 35
	g.CurrentTurn = 2
	g.Status = "finished"
	if err := UpdateLocalGame(database, user.ID, g); err != nil {
		t.Fatalf("update game: %v", err)
	}

	got, err := GetLocalGame(database, user.ID, g.ID)
	if err != nil {
		t.Fatalf("get game after update: %v", err)
	}
	if got.Score1 != 42 || got.Score2 != 35 {
		t.Errorf("scores: got %d/%d, want 42/35", got.Score1, got.Score2)
	}
	if got.CurrentTurn != 2 {
		t.Errorf("turn: got %d, want 2", got.CurrentTurn)
	}
	if got.Status != "finished" {
		t.Errorf("status: got %q, want finished", got.Status)
	}
}

func TestDeleteLocalGame(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	if err := DeleteLocalGame(database, user.ID, g.ID); err != nil {
		t.Fatalf("delete game: %v", err)
	}

	_, err = GetLocalGame(database, user.ID, g.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}

	// Deleting again should return ErrNoRows.
	if err := DeleteLocalGame(database, user.ID, g.ID); err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for double delete, got %v", err)
	}
}

func TestRecordAndListMoves(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	mv := LocalMove{
		GameID:       g.ID,
		MoveNumber:   1,
		PlayerTurn:   1,
		Word:         "HUND",
		Position:     "H8",
		Direction:    "across",
		Score:        12,
		MoveType:     "move",
		BoardBefore:  g.Board,
		Score1Before: 0,
		Score2Before: 0,
		Rack1Before:  "",
		Rack2Before:  "",
	}

	moveID, err := RecordMove(database, g.ID, mv)
	if err != nil {
		t.Fatalf("record move: %v", err)
	}
	if moveID == 0 {
		t.Error("move ID should not be 0")
	}

	moves, err := ListMoves(database, g.ID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != 1 {
		t.Fatalf("got %d moves, want 1", len(moves))
	}
	if moves[0].Word != "HUND" || moves[0].Score != 12 {
		t.Errorf("move: got word=%q score=%d, want HUND/12", moves[0].Word, moves[0].Score)
	}
}

func TestUndoLastMove(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	// Record a move.
	mv := LocalMove{
		GameID:       g.ID,
		MoveNumber:   1,
		PlayerTurn:   1,
		Word:         "HUND",
		Position:     "H8",
		Direction:    "across",
		Score:        12,
		MoveType:     "move",
		BoardBefore:  g.Board,
		Score1Before: 0,
		Score2Before: 0,
	}
	_, err = RecordMove(database, g.ID, mv)
	if err != nil {
		t.Fatalf("record move: %v", err)
	}

	// Update game state to reflect the move.
	g.Score1 = 12
	g.CurrentTurn = 2
	if err := UpdateLocalGame(database, user.ID, g); err != nil {
		t.Fatalf("update game: %v", err)
	}

	// Undo the move.
	undone, err := UndoLastMove(database, user.ID, g.ID)
	if err != nil {
		t.Fatalf("undo move: %v", err)
	}
	if undone.Word != "HUND" {
		t.Errorf("undone word: got %q, want HUND", undone.Word)
	}

	// Game state should be restored.
	got, err := GetLocalGame(database, user.ID, g.ID)
	if err != nil {
		t.Fatalf("get game after undo: %v", err)
	}
	if got.Score1 != 0 || got.CurrentTurn != 1 {
		t.Errorf("after undo: score1=%d turn=%d, want 0/1", got.Score1, got.CurrentTurn)
	}

	// No more moves to undo.
	_, err = UndoLastMove(database, user.ID, g.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for undo on empty history, got %v", err)
	}
}

func TestDeleteMovesAfter(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	// Record 3 moves.
	for i := 1; i <= 3; i++ {
		mv := LocalMove{
			GameID:     g.ID,
			MoveNumber: i,
			PlayerTurn: ((i - 1) % 2) + 1,
			Word:       "WORD",
			Score:      10,
			MoveType:   "move",
		}
		if _, err := RecordMove(database, g.ID, mv); err != nil {
			t.Fatalf("record move %d: %v", i, err)
		}
	}

	// Delete moves after move 1 (simulating redo truncation).
	if err := DeleteMovesAfter(database, g.ID, 1); err != nil {
		t.Fatalf("delete moves after 1: %v", err)
	}

	moves, err := ListMoves(database, g.ID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != 1 {
		t.Fatalf("got %d moves, want 1", len(moves))
	}
}

// Handler tests

func TestListLocalGamesHandler(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	// Create a game.
	_, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	handler := ListLocalGamesHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/local-games", nil)
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Games []LocalGameSummary `json:"games"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Games) != 1 {
		t.Errorf("got %d games, want 1", len(resp.Games))
	}
}

func TestCreateLocalGameHandler(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	handler := CreateLocalGameHandler(database)
	body := `{"player1":"Alice","player2":"Bob"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/local-games", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("got status %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestCreateLocalGameHandler_MissingPlayers(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	handler := CreateLocalGameHandler(database)
	body := `{"player1":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/local-games", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, user)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetLocalGameHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	r := chi.NewRouter()
	r.Get("/api/wordfeud/local-games/{id}", func(w http.ResponseWriter, req *http.Request) {
		req = requestWithUser(req, user)
		GetLocalGameHandler(database).ServeHTTP(w, req)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/local-games/9999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUndoMoveHandler(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)

	g, err := CreateLocalGame(database, user.ID, "Alice", "Bob")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	// Record a move.
	mv := LocalMove{
		GameID:       g.ID,
		MoveNumber:   1,
		PlayerTurn:   1,
		Word:         "TEST",
		Score:        8,
		MoveType:     "move",
		BoardBefore:  g.Board,
		Score1Before: 0,
		Score2Before: 0,
	}
	_, err = RecordMove(database, g.ID, mv)
	if err != nil {
		t.Fatalf("record move: %v", err)
	}

	// Update game to reflect move.
	g.Score1 = 8
	g.CurrentTurn = 2
	if err := UpdateLocalGame(database, user.ID, g); err != nil {
		t.Fatalf("update game: %v", err)
	}

	// Set up Chi router for URL params.
	r := chi.NewRouter()
	r.Post("/api/wordfeud/local-games/{id}/undo", func(w http.ResponseWriter, req *http.Request) {
		req = requestWithUser(req, user)
		UndoMoveHandler(database).ServeHTTP(w, req)
	})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/wordfeud/local-games/%d/undo", g.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Game     LocalGame `json:"game"`
		UndoneMv LocalMove `json:"undone_move"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Game.Score1 != 0 {
		t.Errorf("expected score1=0 after undo, got %d", resp.Game.Score1)
	}
	if resp.UndoneMv.Word != mv.Word || resp.UndoneMv.Score != mv.Score {
		t.Errorf("unexpected undone move: got word=%q score=%d, want word=%q score=%d",
			resp.UndoneMv.Word, resp.UndoneMv.Score, mv.Word, mv.Score)
	}
}
