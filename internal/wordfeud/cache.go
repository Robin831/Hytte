package wordfeud

import (
	"sync"
	"time"
)

const (
	cacheTTL        = 1 * time.Minute
	cacheMaxEntries = 256
)

type cacheEntry struct {
	state   *GameState
	expires time.Time
}

// GameCache is a bounded in-memory cache for game state responses.
// Entries expire after cacheTTL. The cache holds at most cacheMaxEntries;
// when full, expired entries are evicted first, then the oldest entry.
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
// If the cache is at capacity, expired entries are purged first;
// if still full, the oldest entry is evicted.
func (c *GameCache) Set(gameID int64, state *GameState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If updating an existing key, just overwrite.
	if _, exists := c.entries[gameID]; !exists && len(c.entries) >= cacheMaxEntries {
		c.evictLocked()
	}

	c.entries[gameID] = cacheEntry{
		state:   state,
		expires: time.Now().Add(cacheTTL),
	}
}

// evictLocked purges expired entries. If the cache is still at capacity after
// purging, it removes the oldest entry. Must be called with c.mu held.
func (c *GameCache) evictLocked() {
	now := time.Now()

	// First pass: remove all expired entries.
	for id, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, id)
		}
	}

	// If still at capacity, evict the entry closest to expiry (oldest).
	if len(c.entries) >= cacheMaxEntries {
		var oldestID int64
		var oldestTime time.Time
		first := true
		for id, e := range c.entries {
			if first || e.expires.Before(oldestTime) {
				oldestID = id
				oldestTime = e.expires
				first = false
			}
		}
		if !first {
			delete(c.entries, oldestID)
		}
	}
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
