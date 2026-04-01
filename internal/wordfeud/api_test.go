package wordfeud

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	// The Wordfeud API expects SHA1(password + "JarJarBinks9").
	got := hashPassword("test123")
	h := sha1.Sum([]byte("test123" + wordfeudPasswordSalt))
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("hashPassword(\"test123\") = %q, want %q", got, want)
	}
}

func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wf/user/login/email/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		// Verify the password is sent as a SHA1 hash, not plaintext.
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		wantHash := hashPassword("password")
		if body["password"] != wantHash {
			t.Errorf("password not hashed: got %q, want %q", body["password"], wantHash)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"id":         12345,
				"session_id": "test-session-token",
			},
		})
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	token, err := c.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "test-session-token" {
		t.Errorf("got token %q, want %q", token, "test-session-token")
	}
}

func TestLogin_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"content": map[string]any{},
		})
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	_, err := c.Login("bad@example.com", "wrong")
	if err == nil {
		t.Fatal("expected error for failed login")
	}
}

func TestLogin_NetworkError(t *testing.T) {
	c := &Client{
		httpClient: &http.Client{Timeout: 50 * time.Millisecond},
		baseURL:    "http://127.0.0.1:1", // unreachable
	}
	_, err := c.Login("test@example.com", "password")
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

func TestGetGames_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wf/user/games/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"games": []map[string]any{
					{
						"id": 100,
						"players": []map[string]any{
							{"username": "me", "id": 1, "score": 50},
							{"username": "opponent", "id": 2, "score": 30},
						},
						"is_running":     true,
						"current_player": 0,
						"last_move": map[string]any{
							"user_id":   2,
							"move_type": "move",
							"points":    25,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	games, err := c.GetGames("session123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	if games[0].ID != 100 {
		t.Errorf("got ID %d, want 100", games[0].ID)
	}
	if games[0].Opponent != "opponent" {
		t.Errorf("got opponent %q, want %q", games[0].Opponent, "opponent")
	}
	if !games[0].IsMyTurn {
		t.Error("expected IsMyTurn to be true")
	}
	if games[0].Scores != [2]int{50, 30} {
		t.Errorf("got scores %v, want [50 30]", games[0].Scores)
	}
}

func TestGetGames_ExpiredSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	_, err := c.GetGames("expired-token")
	if err == nil {
		t.Fatal("expected error for expired session")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got: %v", err)
	}
}

func TestGetGame_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"game": map[string]any{
					"id": 200,
					"players": []map[string]any{
						{"username": "me", "id": 1, "score": 100},
						{"username": "other", "id": 2, "score": 80},
					},
					"tiles": [][]int{
						{7, 7, 8, 1, 0}, // H at center
					},
					"rack": [][]int{
						{1, 1}, // A
						{2, 3}, // B
					},
					"is_running":     true,
					"current_player": 1,
					"moves": []map[string]any{
						{"user_id": 1, "move_type": "move", "points": 12, "main_word": "HELLO"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	gs, err := c.GetGame("session123", 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gs.ID != 200 {
		t.Errorf("got ID %d, want 200", gs.ID)
	}
	if gs.IsMyTurn {
		t.Error("expected IsMyTurn to be false (current_player=1)")
	}
	if len(gs.Rack) != 2 {
		t.Errorf("got %d rack tiles, want 2", len(gs.Rack))
	}
	if gs.Rack[0].Letter != "A" {
		t.Errorf("got rack[0] letter %q, want %q", gs.Rack[0].Letter, "A")
	}
	if gs.Board[7][7] == nil {
		t.Error("expected tile at [7][7]")
	} else if gs.Board[7][7].Letter != "H" {
		t.Errorf("got board[7][7] letter %q, want %q", gs.Board[7][7].Letter, "H")
	}
	if len(gs.MoveHistory) != 1 {
		t.Fatalf("got %d moves, want 1", len(gs.MoveHistory))
	}
	if gs.MoveHistory[0].MainWord != "HELLO" {
		t.Errorf("got main_word %q, want %q", gs.MoveHistory[0].MainWord, "HELLO")
	}
}

func TestGetGame_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	_, err := c.GetGame("session123", 999)
	if err == nil {
		t.Fatal("expected error for not found game")
	}
	if !errors.Is(err, ErrGameNotFound) {
		t.Errorf("expected ErrGameNotFound, got: %v", err)
	}
}

func TestCache_ExpiryAndHit(t *testing.T) {
	cache := NewGameCache()

	gs := &GameState{ID: 42, IsRunning: true}
	cache.Set(1, 42, gs)

	// Should hit cache
	got, ok := cache.Get(1, 42)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.ID != 42 {
		t.Errorf("got ID %d, want 42", got.ID)
	}

	// Should miss for unknown key
	_, ok = cache.Get(1, 99)
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestCache_Expired(t *testing.T) {
	cache := NewGameCache()

	// Manually insert an expired entry
	cache.mu.Lock()
	cache.entries[cacheKey{userID: 1, gameID: 42}] = cacheEntry{
		state:   &GameState{ID: 42},
		expires: time.Now().Add(-1 * time.Second),
	}
	cache.mu.Unlock()

	_, ok := cache.Get(1, 42)
	if ok {
		t.Fatal("expected cache miss for expired entry")
	}
}

func TestGetGameCached_UsesCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"content": map[string]any{
				"game": map[string]any{
					"id":             300,
					"players":        []map[string]any{{"username": "a", "id": 1, "score": 0}, {"username": "b", "id": 2, "score": 0}},
					"tiles":          [][]int{},
					"rack":           [][]int{},
					"is_running":     true,
					"current_player": 0,
					"moves":          []map[string]any{},
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	cache := NewGameCache()

	// First call — should hit the API
	gs1, err := GetGameCached(c, cache, "token", 1, 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gs1.ID != 300 {
		t.Errorf("got ID %d, want 300", gs1.ID)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call — should use cache
	gs2, err := GetGameCached(c, cache, "token", 1, 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gs2.ID != 300 {
		t.Errorf("got ID %d, want 300", gs2.ID)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 API call (cached), got %d", callCount)
	}
}

func TestTileLetterFromOrdinal(t *testing.T) {
	tests := []struct {
		ordinal int
		want    string
	}{
		{0, ""},
		{1, "A"},
		{8, "H"},
		{26, "Z"},
		{27, ""},
		{-1, ""},
	}
	for _, tt := range tests {
		got := tileLetterFromOrdinal(tt.ordinal)
		if got != tt.want {
			t.Errorf("tileLetterFromOrdinal(%d) = %q, want %q", tt.ordinal, got, tt.want)
		}
	}
}

func TestCache_EvictsWhenFull(t *testing.T) {
	cache := NewGameCache()

	// Fill the cache to capacity.
	for i := int64(0); i < cacheMaxEntries; i++ {
		cache.Set(1, i, &GameState{ID: i})
	}

	// Add one more — should succeed without panic and evict one entry.
	cache.Set(1, cacheMaxEntries, &GameState{ID: cacheMaxEntries})

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	if count > cacheMaxEntries {
		t.Errorf("cache has %d entries, want at most %d", count, cacheMaxEntries)
	}

	// The newly inserted entry should be present.
	if _, ok := cache.Get(1, cacheMaxEntries); !ok {
		t.Error("expected newly inserted entry to be in cache")
	}
}

func TestCache_EvictsExpiredFirst(t *testing.T) {
	cache := NewGameCache()

	// Insert an expired entry and a valid entry.
	cache.mu.Lock()
	for i := int64(0); i < cacheMaxEntries; i++ {
		if i == 0 {
			cache.entries[cacheKey{userID: 1, gameID: i}] = cacheEntry{state: &GameState{ID: i}, expires: time.Now().Add(-time.Second)}
		} else {
			cache.entries[cacheKey{userID: 1, gameID: i}] = cacheEntry{state: &GameState{ID: i}, expires: time.Now().Add(time.Minute)}
		}
	}
	cache.mu.Unlock()

	// Insert a new entry — expired entry (id=0) should be evicted.
	cache.Set(1, cacheMaxEntries, &GameState{ID: cacheMaxEntries})

	// The expired entry should have been evicted.
	cache.mu.RLock()
	_, hasExpired := cache.entries[cacheKey{userID: 1, gameID: 0}]
	cache.mu.RUnlock()

	if hasExpired {
		t.Error("expected expired entry (id=0) to be evicted")
	}
}

func TestParseBoardTiles(t *testing.T) {
	var board [15][15]*Tile
	tiles := [][]int{
		{0, 0, 1, 1, 0}, // A at (0,0)
		{14, 14, 26, 10, 1}, // Z wildcard at (14,14)
		{-1, 0, 1, 1, 0}, // out of bounds — should be skipped
		{0},               // too short — should be skipped
	}
	parseBoardTiles(&board, tiles)

	if board[0][0] == nil {
		t.Fatal("expected tile at [0][0]")
	}
	if board[0][0].Letter != "A" {
		t.Errorf("got letter %q, want %q", board[0][0].Letter, "A")
	}
	if board[14][14] == nil {
		t.Fatal("expected tile at [14][14]")
	}
	if !board[14][14].IsWild {
		t.Error("expected wildcard at [14][14]")
	}
	if board[7][7] != nil {
		t.Error("expected nil at [7][7]")
	}
}
