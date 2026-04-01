package wordfeud

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sentinel errors for classifying upstream API failures.
var (
	ErrSessionExpired     = errors.New("wordfeud: session expired or invalid")
	ErrGameNotFound       = errors.New("wordfeud: game not found")
	ErrInvalidCredentials = errors.New("wordfeud: invalid email or password")
)

const (
	baseURL              = "https://game06.wordfeud.com/wf"
	defaultTimeout       = 10 * time.Second
	wordfeudPasswordSalt = "JarJarBinks9"
	userAgent            = "Wordfeud/3.7.2 (Hytte)"
)

// Client is the Wordfeud API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient returns a new Wordfeud API client.
// The HTTP client is configured to not follow redirects, because Go's default
// behaviour converts POST to GET on 301/302 redirects, dropping the request body.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: baseURL,
	}
}

// apiResponse is the common wrapper for Wordfeud API responses.
type apiResponse struct {
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

// hashPassword computes the SHA1 hash of the password with the Wordfeud salt.
// The Wordfeud API expects passwords as SHA1(password + "JarJarBinks9").
func hashPassword(password string) string {
	h := sha1.Sum([]byte(password + wordfeudPasswordSalt))
	return hex.EncodeToString(h[:])
}

// Login authenticates with email and password and returns a session token.
func (c *Client) Login(email, password string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"email":    email,
		"password": hashPassword(password),
	})
	if err != nil {
		return "", fmt.Errorf("wordfeud: marshal login request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/user/login/email/", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("wordfeud: create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("wordfeud: login request failed: %w", err)
	}
	defer resp.Body.Close()

	// Detect redirect responses (not followed due to CheckRedirect) and return
	// a clear error instead of trying to parse the redirect body.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		return "", fmt.Errorf("wordfeud: login endpoint returned redirect %d to %s", resp.StatusCode, loc)
	}

	body, err := readResponseBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("wordfeud: read login response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("wordfeud: login HTTP %d: %w", resp.StatusCode, ErrInvalidCredentials)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wordfeud: login returned HTTP %d: %s", resp.StatusCode, body)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("wordfeud: parse login response: %w", err)
	}
	if apiResp.Status != "success" {
		return "", fmt.Errorf("wordfeud: login failed (status=%s): %w", apiResp.Status, ErrInvalidCredentials)
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

	body, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: read games response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("wordfeud: games: %w", ErrSessionExpired)
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

	body, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: read game response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("wordfeud: game %d: %w", gameID, ErrSessionExpired)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("wordfeud: game %d: %w", gameID, ErrGameNotFound)
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
	req.Header.Set("User-Agent", userAgent)
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
	Board     [][]int `json:"tiles"` // list of placed tiles: [row, col, letter_ordinal, value, is_wildcard]
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

// maxResponseBytes is the maximum response body size we'll accept from the Wordfeud API.
const maxResponseBytes = 1 << 20 // 1 MiB

// readResponseBody reads the response body up to maxResponseBytes and returns an error
// if the response exceeds the limit (instead of silently truncating).
func readResponseBody(body io.Reader) ([]byte, error) {
	lr := &io.LimitedReader{R: body, N: maxResponseBytes + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)
	}
	return data, nil
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
