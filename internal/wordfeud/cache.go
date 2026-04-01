package wordfeud

import (
	"sync"
	"time"
)

const cacheTTL = 1 * time.Minute

type cacheEntry struct {
	state   *GameState
	expires time.Time
}

// GameCache is a simple in-memory cache for game state responses.
type GameCache struct {
	mu      sync.RWMutex
	entries map[int64]cacheEntry
}

// NewGameCache returns a new empty cache.
func NewGameCache() *GameCache {
	return &GameCache{
		entries: make(map[int64]cacheEntry),
	}
}

// Get returns the cached game state if it exists and hasn't expired.
func (c *GameCache) Get(gameID int64) (*GameState, bool) {
	c.mu.RLock()
	entry, ok := c.entries[gameID]
	c.mu.RUnlock()

	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.state, true
}

// Set stores a game state in the cache with a 1-minute TTL.
func (c *GameCache) Set(gameID int64, state *GameState) {
	c.mu.Lock()
	c.entries[gameID] = cacheEntry{
		state:   state,
		expires: time.Now().Add(cacheTTL),
	}
	c.mu.Unlock()
}

// GetGameCached returns a cached game state or fetches from the API.
func GetGameCached(client *Client, cache *GameCache, sessionToken string, gameID int64) (*GameState, error) {
	if gs, ok := cache.Get(gameID); ok {
		return gs, nil
	}

	gs, err := client.GetGame(sessionToken, gameID)
	if err != nil {
		return nil, err
	}

	cache.Set(gameID, gs)
	return gs, nil
}
