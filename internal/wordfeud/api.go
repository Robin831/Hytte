package wordfeud

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	userAgent            = "Wordfeud/4.0.0 (Android; 14; Pixel 8)"
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

// maxLoginRedirects is the maximum number of POST redirects Login will follow.
const maxLoginRedirects = 3

// isWordfeudHost reports whether host is wordfeud.com or a subdomain.
// Used to validate redirect targets before resending credentials.
func isWordfeudHost(host string) bool {
	// Strip port if present (e.g. "game06.wordfeud.com:443").
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "wordfeud.com" || strings.HasSuffix(host, ".wordfeud.com")
}

// Login authenticates with email and password and returns a session token.
// It manually follows up to maxLoginRedirects POST redirects within the
// wordfeud.com domain so that load-balancer redirects succeed without the
// POST-to-GET conversion that Go's default redirect handling performs.
func (c *Client) Login(email, password string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"email":    email,
		"password": hashPassword(password),
	})
	if err != nil {
		return "", fmt.Errorf("wordfeud: marshal login request: %w", err)
	}

	loginURL := c.baseURL + "/user/login/email/"
	var resp *http.Response
	for attempt := 0; attempt <= maxLoginRedirects; attempt++ {
		req, err := http.NewRequest(http.MethodPost, loginURL, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("wordfeud: create login request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("wordfeud: login request failed: %w", err)
		}

		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			break // not a redirect — proceed to parse
		}

		loc := resp.Header.Get("Location")
		resp.Body.Close()

		if attempt == maxLoginRedirects {
			return "", fmt.Errorf("wordfeud: login exceeded %d redirects (last redirect to: %s)", maxLoginRedirects, loc)
		}

		// Resolve the Location header against the current URL so that both
		// absolute ("https://game07.wordfeud.com/wf/...") and relative
		// ("/wf/user/login/email/") redirects work correctly.
		base, _ := url.Parse(loginURL)
		parsed, locErr := url.Parse(loc)
		if locErr != nil {
			return "", fmt.Errorf("wordfeud: login redirect to invalid URL: %s", loc)
		}
		resolved := base.ResolveReference(parsed)
		if !isWordfeudHost(resolved.Host) {
			return "", fmt.Errorf("wordfeud: login redirect to disallowed host: %s", resolved.Host)
		}
		loginURL = resolved.String()
	}
	defer resp.Body.Close()

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

	// The session token may be in the Set-Cookie header or the JSON body.
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "sessionid" && cookie.Value != "" {
			return cookie.Value, nil
		}
	}

	// Fall back to reading session_id from the JSON content body.
	var loginContent struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(apiResp.Content, &loginContent); err == nil && loginContent.SessionID != "" {
		return loginContent.SessionID, nil
	}

	return "", fmt.Errorf("wordfeud: login succeeded but no session token in response")
}

// GamesResult holds the active and finished game lists.
type GamesResult struct {
	Active   []GameSummary
	Finished []GameSummary
}

// GetGames fetches the list of games, split into active and finished.
// The Wordfeud API requires POST for this endpoint. Sending GET causes the
// server to respond with a redirect, which our client treats as an error via
// checkRedirect. Using POST matches the upstream API contract.
func (c *Client) GetGames(sessionToken string) (*GamesResult, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/user/games/", nil)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: create games request: %w", err)
	}
	c.setAuth(req, sessionToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: games request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := checkRedirect(resp); err != nil {
		return nil, err
	}

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

	result := &GamesResult{
		Active:   make([]GameSummary, 0, len(content.Games)),
		Finished: make([]GameSummary, 0),
	}
	for _, g := range content.Games {
		if g.IsRunning {
			result.Active = append(result.Active, g.toSummary())
		} else {
			result.Finished = append(result.Finished, g.toSummary())
		}
	}
	return result, nil
}

// GetGame fetches the full state for a single game.
// The official Wordfeud API expects this endpoint to be called with POST.
// Using GET here causes the server to respond with a redirect, which our
// client treats as an error via checkRedirect. Keep POST to match the
// upstream API contract and avoid spurious redirect failures.
func (c *Client) GetGame(sessionToken string, gameID int64) (*GameState, error) {
	url := fmt.Sprintf("%s/game/%d/", c.baseURL, gameID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: create game request: %w", err)
	}
	c.setAuth(req, sessionToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: game request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := checkRedirect(resp); err != nil {
		return nil, err
	}

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
		IsLocal  bool   `json:"is_local"`
	} `json:"players"`
	IsRunning bool `json:"is_running"`
	LastMove  struct {
		UserID   int64  `json:"user_id"`
		MoveType string `json:"move_type"`
		Points   int    `json:"points"`
	} `json:"last_move"`
	CurrentPlayer int   `json:"current_player"` // index into Players
	Updated       int64 `json:"updated"`         // Unix timestamp of last activity
}

func (g rawGame) toSummary() GameSummary {
	s := GameSummary{
		ID: g.ID,
		LastMove: MoveInfo{
			UserID:   g.LastMove.UserID,
			MoveType: g.LastMove.MoveType,
			Points:   g.LastMove.Points,
		},
		EndedAt: g.Updated,
	}

	// Use is_local flag to identify which player is me vs opponent.
	// The player index is NOT consistent — luremus can be at index 0 or 1.
	if len(g.Players) >= 2 {
		me, opp := 0, 1
		if g.Players[1].IsLocal {
			me, opp = 1, 0
		}
		s.MyUsername = g.Players[me].Username
		s.Opponent = g.Players[opp].Username
		s.Scores = [2]int{g.Players[me].Score, g.Players[opp].Score}
		s.IsMyTurn = g.CurrentPlayer == me
	}

	return s
}

// rawGameDetail maps the JSON structure from the Wordfeud game detail endpoint.
type rawGameDetail struct {
	ID      int64 `json:"id"`
	Players []struct {
		Username string            `json:"username"`
		ID       int64             `json:"id"`
		Score    int               `json:"score"`
		IsLocal  bool              `json:"is_local"`
		Rack     []json.RawMessage `json:"rack"` // per-player rack (some API versions)
	} `json:"players"`
	Rack          []json.RawMessage `json:"rack"`     // game-level rack: each element is [letter_id, count]
	Tiles         []json.RawMessage `json:"tiles"`    // each: [row, col, letter_id, value, is_wildcard] — mixed types
	BoardID       int               `json:"board"`    // board layout ID (integer, not the grid)
	BagCount      int               `json:"bag_count"`
	IsRunning     bool              `json:"is_running"`
	Moves         []struct {
		UserID   int64  `json:"user_id"`
		MoveType string `json:"move_type"`
		Points   int    `json:"points"`
		MainWord string `json:"main_word"`
	} `json:"moves"`
	CurrentPlayer int `json:"current_player"`
}

// letterFromID converts a Wordfeud numeric letter ID (1-based) to the letter string.
// ID 0 means blank. Returns "" for unknown IDs.
func letterFromID(id int) string {
	if id == 0 {
		return ""
	}
	if id >= 1 && id <= len(NorwegianTiles) {
		return NorwegianTiles[id-1].Letter
	}
	return ""
}

// parseLetter tries to interpret a json.RawMessage as either a string letter
// or an integer letter ID, returning the letter string.
func parseLetter(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return letterFromID(n)
	}
	return ""
}

// parseRackEntries converts raw JSON rack entries into Tile slices.
// Each entry can be a string letter, an integer letter ID, or a [letter_id, count] array.
func parseRackEntries(entries []json.RawMessage) []Tile {
	tiles := make([]Tile, 0, len(entries))
	for _, raw := range entries {
		letter := parseLetter(raw)
		if letter != "" {
			tiles = append(tiles, Tile{
				Letter: letter,
				Value:  letterPoints(letter),
			})
			continue
		}
		// Try parsing as [letter_id, count] array
		var arr []int
		if json.Unmarshal(raw, &arr) == nil && len(arr) >= 1 {
			letterID := arr[0]
			// Default to a single tile if count is omitted; handle non-positive counts defensively.
			count := 1
			if len(arr) >= 2 {
				if arr[1] <= 0 {
					// Skip entries with non-positive counts.
					continue
				}
				count = arr[1]
			}

			if letterID == 0 {
				// Blank/wild tiles
				for i := 0; i < count; i++ {
					tiles = append(tiles, Tile{
						Letter: "",
						Value:  0,
						IsWild: true,
					})
				}
				continue
			}

			l := letterFromID(letterID)
			if l != "" {
				for i := 0; i < count; i++ {
					tiles = append(tiles, Tile{
						Letter: l,
						Value:  letterPoints(l),
					})
				}
			}
		}
	}
	return tiles
}

// letterPoints returns the Wordfeud point value for a letter string.
func letterPoints(letter string) int {
	for _, r := range letter {
		if v, ok := LetterValue[r]; ok {
			return v
		}
	}
	return 0
}

// parseBoolOrInt interprets a JSON value as a boolean.
// Handles both JSON booleans (true/false) and integers (non-zero = true).
func parseBoolOrInt(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n != 0
	}
	return false
}

func (g rawGameDetail) toGameState() *GameState {
	// Determine the local player index.
	// Primary: use is_local flag. Fallback: infer from the player with rack data
	// (only the local player has rack data in the Wordfeud API).
	meIdx := 0
	foundLocal := false
	for i, p := range g.Players {
		if p.IsLocal {
			meIdx = i
			foundLocal = true
			break
		}
	}
	if !foundLocal {
		for i, p := range g.Players {
			if len(p.Rack) > 0 {
				meIdx = i
				break
			}
		}
	}

	oppIdx := 1 - meIdx

	gs := &GameState{
		ID:        g.ID,
		IsRunning: g.IsRunning,
		IsMyTurn:  g.CurrentPlayer == meIdx,
		BagCount:  g.BagCount,
	}

	// Normalize players so gs.Players[0] is always the local player and
	// gs.Players[1] is always the opponent. The frontend highlights players[0]
	// when is_my_turn is true, so this must be consistent.
	if len(g.Players) >= 2 {
		gs.Players[0] = Player{
			Username: g.Players[meIdx].Username,
			ID:       g.Players[meIdx].ID,
			Score:    g.Players[meIdx].Score,
		}
		gs.Players[1] = Player{
			Username: g.Players[oppIdx].Username,
			ID:       g.Players[oppIdx].ID,
			Score:    g.Players[oppIdx].Score,
		}
		// Per-player rack — only the local player has rack data
		if len(g.Players[meIdx].Rack) > 0 {
			gs.Rack = parseRackEntries(g.Players[meIdx].Rack)
		}
	}

	// Game-level rack (most API versions put rack at the game level)
	if len(g.Rack) > 0 {
		gs.Rack = parseRackEntries(g.Rack)
	}

	// Board tiles: each raw tile is [row, col, letter_id_or_string, value, is_wildcard]
	for _, raw := range g.Tiles {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil || len(arr) < 3 {
			continue
		}
		var row, col int
		var isWild bool
		if err := json.Unmarshal(arr[0], &row); err != nil {
			continue
		}
		if err := json.Unmarshal(arr[1], &col); err != nil {
			continue
		}
		letter := parseLetter(arr[2])
		if len(arr) >= 5 {
			isWild = parseBoolOrInt(arr[4])
		} else if len(arr) >= 4 {
			isWild = parseBoolOrInt(arr[3])
		}
		if row >= 0 && row < 15 && col >= 0 && col < 15 && letter != "" {
			gs.Board[row][col] = &Tile{
				Letter: letter,
				Value:  letterPoints(letter),
				IsWild: isWild,
			}
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

// checkRedirect returns a descriptive error when the no-redirect HTTP client
// receives a 3xx response, preventing silent JSON-parse failures on redirect bodies.
func checkRedirect(resp *http.Response) error {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("wordfeud: unexpected redirect %d to %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	return nil
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
