package wordfeud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	baseURL        = "https://game06.wordfeud.com/wf"
	defaultTimeout = 10 * time.Second
)

// Client is the Wordfeud API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient returns a new Wordfeud API client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    baseURL,
	}
}

// apiResponse is the common wrapper for Wordfeud API responses.
type apiResponse struct {
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

// Login authenticates with email and password and returns a session token.
func (c *Client) Login(email, password string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return "", fmt.Errorf("wordfeud: marshal login request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/user/login/email", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("wordfeud: create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("wordfeud: login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("wordfeud: read login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wordfeud: login returned HTTP %d: %s", resp.StatusCode, body)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("wordfeud: parse login response: %w", err)
	}
	if apiResp.Status != "success" {
		return "", fmt.Errorf("wordfeud: login failed: status=%s", apiResp.Status)
	}

	var content struct {
		ID        int64  `json:"id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(apiResp.Content, &content); err != nil {
		return "", fmt.Errorf("wordfeud: parse login content: %w", err)
	}
	if content.SessionID == "" {
		return "", fmt.Errorf("wordfeud: login returned empty session token")
	}
	return content.SessionID, nil
}

// GetGames fetches the list of active games.
func (c *Client) GetGames(sessionToken string) ([]GameSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/user/games/", nil)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: create games request: %w", err)
	}
	c.setAuth(req, sessionToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: games request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("wordfeud: read games response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("wordfeud: session expired or invalid (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordfeud: games returned HTTP %d: %s", resp.StatusCode, body)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("wordfeud: parse games response: %w", err)
	}
	if apiResp.Status != "success" {
		return nil, fmt.Errorf("wordfeud: games failed: status=%s", apiResp.Status)
	}

	var content struct {
		Games []rawGame `json:"games"`
	}
	if err := json.Unmarshal(apiResp.Content, &content); err != nil {
		return nil, fmt.Errorf("wordfeud: parse games content: %w", err)
	}

	summaries := make([]GameSummary, 0, len(content.Games))
	for _, g := range content.Games {
		summaries = append(summaries, g.toSummary())
	}
	return summaries, nil
}

// GetGame fetches the full state for a single game.
func (c *Client) GetGame(sessionToken string, gameID int64) (*GameState, error) {
	url := fmt.Sprintf("%s/game/%d/", c.baseURL, gameID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: create game request: %w", err)
	}
	c.setAuth(req, sessionToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: game request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("wordfeud: read game response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("wordfeud: session expired or invalid (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("wordfeud: game %d not found", gameID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordfeud: game returned HTTP %d: %s", resp.StatusCode, body)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("wordfeud: parse game response: %w", err)
	}
	if apiResp.Status != "success" {
		return nil, fmt.Errorf("wordfeud: game failed: status=%s", apiResp.Status)
	}

	var content struct {
		Game rawGameDetail `json:"game"`
	}
	if err := json.Unmarshal(apiResp.Content, &content); err != nil {
		return nil, fmt.Errorf("wordfeud: parse game content: %w", err)
	}

	return content.Game.toGameState(), nil
}

func (c *Client) setAuth(req *http.Request, sessionToken string) {
	req.Header.Set("Cookie", "sessionid="+sessionToken)
	req.Header.Set("Accept", "application/json")
}

// rawGame maps the JSON structure from the Wordfeud games list endpoint.
type rawGame struct {
	ID       int64 `json:"id"`
	Players  []struct {
		Username string `json:"username"`
		ID       int64  `json:"id"`
		Score    int    `json:"score"`
		IsMyTurn bool   `json:"is_my_turn,omitempty"`
	} `json:"players"`
	IsRunning bool `json:"is_running"`
	LastMove  struct {
		UserID   int64  `json:"user_id"`
		MoveType string `json:"move_type"`
		Points   int    `json:"points"`
	} `json:"last_move"`
	CurrentPlayer int `json:"current_player"` // index into Players
}

func (g rawGame) toSummary() GameSummary {
	s := GameSummary{
		ID: g.ID,
		LastMove: MoveInfo{
			UserID:   g.LastMove.UserID,
			MoveType: g.LastMove.MoveType,
			Points:   g.LastMove.Points,
		},
	}

	// Find opponent and scores. Player 0 is "me" per the Wordfeud API convention.
	if len(g.Players) >= 2 {
		s.Scores = [2]int{g.Players[0].Score, g.Players[1].Score}
		s.Opponent = g.Players[1].Username
		s.IsMyTurn = g.CurrentPlayer == 0
	}

	return s
}

// rawGameDetail maps the JSON structure from the Wordfeud game detail endpoint.
type rawGameDetail struct {
	ID      int64 `json:"id"`
	Players []struct {
		Username string `json:"username"`
		ID       int64  `json:"id"`
		Score    int    `json:"score"`
	} `json:"players"`
	Board     [][]int `json:"tiles"` // 15x15 array: each tile is [letter_ordinal, value, is_wildcard]
	Rack      [][]int `json:"rack"`  // array of [letter_ordinal, value]
	IsRunning bool    `json:"is_running"`
	Moves     []struct {
		UserID   int64      `json:"user_id"`
		MoveType string     `json:"move_type"`
		Points   int        `json:"points"`
		MainWord string     `json:"main_word"`
	} `json:"moves"`
	CurrentPlayer int `json:"current_player"`
}

func (g rawGameDetail) toGameState() *GameState {
	gs := &GameState{
		ID:        g.ID,
		IsRunning: g.IsRunning,
		IsMyTurn:  g.CurrentPlayer == 0,
	}

	// Players
	for i := 0; i < 2 && i < len(g.Players); i++ {
		gs.Players[i] = Player{
			Username: g.Players[i].Username,
			ID:       g.Players[i].ID,
			Score:    g.Players[i].Score,
		}
	}

	// Board: the Wordfeud API returns placed tiles as [row, col, letter, value, wildcard].
	parseBoardTiles(&gs.Board, g.Board)

	// Rack
	gs.Rack = make([]Tile, 0, len(g.Rack))
	for _, r := range g.Rack {
		if len(r) >= 2 {
			t := Tile{
				Letter: tileLetterFromOrdinal(r[0]),
				Value:  r[1],
			}
			if len(r) >= 3 && r[2] == 1 {
				t.IsWild = true
			}
			gs.Rack = append(gs.Rack, t)
		}
	}

	// Moves
	gs.MoveHistory = make([]Move, 0, len(g.Moves))
	for _, m := range g.Moves {
		gs.MoveHistory = append(gs.MoveHistory, Move{
			UserID:   m.UserID,
			MoveType: m.MoveType,
			Points:   m.Points,
			MainWord: m.MainWord,
		})
	}

	return gs
}

// tileLetterFromOrdinal converts a Wordfeud letter ordinal to a string.
// Ordinal 0 = blank/wildcard, 1-26 = A-Z (language-dependent but broadly consistent).
func tileLetterFromOrdinal(ordinal int) string {
	if ordinal <= 0 || ordinal > 26 {
		return ""
	}
	return string(rune('A' + ordinal - 1))
}

// parseBoardTiles populates the board from the Wordfeud tiles array.
// The tiles array from the API contains placed tiles as [row, col, letter_ordinal, value, is_wildcard].
func parseBoardTiles(board *[15][15]*Tile, tiles [][]int) {
	for _, t := range tiles {
		if len(t) < 4 {
			continue
		}
		row, col := t[0], t[1]
		if row < 0 || row >= 15 || col < 0 || col >= 15 {
			continue
		}
		tile := &Tile{
			Letter: tileLetterFromOrdinal(t[2]),
			Value:  t[3],
		}
		if len(t) >= 5 && t[4] == 1 {
			tile.IsWild = true
		}
		board[row][col] = tile
	}
}
