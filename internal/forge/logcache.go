package forge

import (
	"sync"
	"time"
)

// parsedLogCacheEntry holds the cached parser state for a single worker log
// file. It is keyed by (workerID, resolvedLogPath) in parsedLogCache so that
// concurrent requests for the same log file serialize on the same mutex.
type parsedLogCacheEntry struct {
	// mu serializes the parse-and-update section. Different workers proceed
	// in parallel because each holds a different *parsedLogCacheEntry.
	mu sync.Mutex
	// state is the running parser state, including cumulative entries,
	// pending tool_use correlations, and the next sequence number.
	state *LogParseState
	// nextOffset is the byte offset in the log file where parsing should
	// resume on the next request.
	nextOffset int64
	// modTime / size record the file metadata observed at the last parse.
	// They are used to detect rotation/truncation, which invalidates the
	// cache and forces a re-parse from offset 0.
	modTime time.Time
	size    int64
	// lastAccess is updated on every cache hit and is consulted by the LRU
	// evictor. It is read/written under parsedLogCache.mu, never under
	// the entry's own mu.
	lastAccess time.Time
	// parseCount tracks how many times new bytes have actually been read
	// from disk for this entry. It is bumped under mu and is intended for
	// test verification of the "skip parse when nothing changed" path.
	parseCount int
}

// reset clears the entry so the next parse starts from the beginning of the
// file. The caller must hold the entry's mu.
func (e *parsedLogCacheEntry) reset() {
	e.state = NewLogParseState()
	e.nextOffset = 0
	e.modTime = time.Time{}
	e.size = 0
}

// parsedLogCache is a small bounded LRU of parsed log states. The cap is
// intentionally tiny: a Hytte instance typically tracks fewer than ten active
// workers at a time, and each entry holds a slice bounded by the rolling
// buffer in ParseWorkerLogFrom (~5000 entries).
type parsedLogCache struct {
	mu      sync.Mutex
	entries map[string]*parsedLogCacheEntry
	cap     int
}

// defaultParsedLogCacheCap is the default LRU capacity. Sized for a small
// fleet of active workers with headroom; 32 × ~2 MB worst case ≈ 64 MB.
const defaultParsedLogCacheCap = 32

// newParsedLogCache constructs an empty cache with the given capacity.
// A non-positive capacity falls back to defaultParsedLogCacheCap.
func newParsedLogCache(capacity int) *parsedLogCache {
	if capacity <= 0 {
		capacity = defaultParsedLogCacheCap
	}
	return &parsedLogCache{
		entries: make(map[string]*parsedLogCacheEntry),
		cap:     capacity,
	}
}

// getOrCreate returns the cache entry for key, creating it (and evicting the
// least-recently-accessed entry if the cache is full) when absent. The
// returned entry has its lastAccess timestamp updated.
func (c *parsedLogCache) getOrCreate(key string) *parsedLogCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.lastAccess = time.Now()
		return e
	}
	if len(c.entries) >= c.cap {
		c.evictOldestLocked()
	}
	e := &parsedLogCacheEntry{
		state:      NewLogParseState(),
		lastAccess: time.Now(),
	}
	c.entries[key] = e
	return e
}

// invalidate removes the entry for key, if any.
func (c *parsedLogCache) invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// evictOldestLocked removes the entry with the oldest lastAccess timestamp.
// The caller must hold c.mu.
func (c *parsedLogCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range c.entries {
		if first || e.lastAccess.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.lastAccess
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
