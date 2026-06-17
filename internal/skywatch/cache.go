package skywatch

import (
	"sync"
	"time"
)

// ttlCache is a tiny in-memory cache that stores serialized response bytes
// keyed by a string, expiring entries after a fixed TTL. It is safe for
// concurrent use. There is no eviction policy beyond TTL expiry — the cache is
// bounded by the small number of distinct coordinate/date keys in practice.
type ttlCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]ttlEntry
}

type ttlEntry struct {
	data    []byte
	expires time.Time
}

// newTTLCache creates a cache whose entries live for the given TTL.
func newTTLCache(ttl time.Duration) *ttlCache {
	return &ttlCache{
		ttl:     ttl,
		entries: make(map[string]ttlEntry),
	}
}

// get returns the cached bytes for key. The second return value is false if the
// key is missing or the entry has expired; expired entries are dropped lazily.
func (c *ttlCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		c.mu.Lock()
		// Re-check under the write lock in case another goroutine refreshed it.
		if cur, ok := c.entries[key]; ok && time.Now().After(cur.expires) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return nil, false
	}
	return e.data, true
}

// set stores data under key with the cache's TTL.
func (c *ttlCache) set(key string, data []byte) {
	c.mu.Lock()
	c.entries[key] = ttlEntry{data: data, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
