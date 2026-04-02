package wordfeud

// Tile represents a single tile on the board or in the rack.
type Tile struct {
	Letter string `json:"letter"`
	Value  int    `json:"value"`
	IsWild bool   `json:"is_wild,omitempty"`
}

// GameSummary is a compact representation of a game (active or finished).
type GameSummary struct {
	ID       int64    `json:"id"`
	Opponent string   `json:"opponent"`
	Scores   [2]int   `json:"scores"`
	IsMyTurn bool     `json:"is_my_turn"`
	LastMove MoveInfo `json:"last_move"`
	EndedAt  int64    `json:"ended_at,omitempty"` // Unix timestamp of last activity; non-zero for finished games
}

// MoveInfo describes the most recent move in a game summary.
type MoveInfo struct {
	UserID   int64  `json:"user_id"`
	MoveType string `json:"move_type"` // "move", "swap", "pass", "resign"
	Points   int    `json:"points"`
}

// Move represents a single move in a game's history.
type Move struct {
	UserID   int64  `json:"user_id"`
	MoveType string `json:"move_type"`
	Points   int    `json:"points"`
	MainWord string `json:"main_word"`
}

// Player represents a player in a game.
type Player struct {
	Username string `json:"username"`
	ID       int64  `json:"id"`
	Score    int    `json:"score"`
	IsMyTurn bool   `json:"is_my_turn,omitempty"`
}

// GameState is the full state of a single game.
type GameState struct {
	ID          int64      `json:"id"`
	Board       [15][15]*Tile `json:"board"`
	Rack        []Tile     `json:"rack"`
	Players     [2]Player  `json:"players"`
	IsMyTurn    bool       `json:"is_my_turn"`
	MoveHistory []Move     `json:"move_history"`
	IsRunning   bool       `json:"is_running"`
	BagCount    int        `json:"bag_count"`
}
