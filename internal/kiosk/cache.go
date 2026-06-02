package kiosk

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CacheTTL is the time-to-live for cached kiosk payloads. It is intentionally
// short: kiosks poll roughly every 30s, so a 20s window collapses the bursts
// of near-simultaneous polls from multiple tabs into a single upstream fetch
// while keeping the displayed data fresh. The same value is advertised to
// browsers/CDN via the Cache-Control: max-age header.
const CacheTTL = 20 * time.Second

// entry is a single cached kiosk payload with its expiry instant.
type entry struct {
	value     KioskData
	expiresAt time.Time
}

// TTLCache is a small, self-contained, mutex-guarded in-memory cache with a
// fixed TTL per entry. Entries are only evicted lazily on read after they
// expire; given the tiny key space (one key per distinct kiosk config) no
// size cap or background sweeper is needed.
type TTLCache struct {
	mu    sync.Mutex
	items map[string]entry
}

// NewTTLCache returns an empty TTLCache ready for use.
func NewTTLCache() *TTLCache {
	return &TTLCache{items: make(map[string]entry)}
}

// Get returns the cached value for key and true if present and not expired.
// Expired entries are treated as a miss (and removed).
func (c *TTLCache) Get(key string) (KioskData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return KioskData{}, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.items, key)
		return KioskData{}, false
	}
	return e.value, true
}

// Set stores val under key with an expiry of now+CacheTTL.
func (c *TTLCache) Set(key string, val KioskData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry{value: val, expiresAt: time.Now().Add(CacheTTL)}
}

// buildCacheKey constructs a deterministic cache key from the request's data
// inputs. Stop IDs are sorted so ordering differences don't produce distinct
// keys, and a stable separator scheme keeps the location and Netatmo segments
// unambiguous. Two requests with identical kiosk config produce the same key.
func buildCacheKey(stopIDs []string, lat, lon float64, hasLat, hasLon bool, location string, netatmoUserID int64) string {
	sorted := append([]string(nil), stopIDs...)
	sort.Strings(sorted)

	var b strings.Builder
	b.WriteString("stops=")
	b.WriteString(strings.Join(sorted, ","))
	b.WriteString("|loc=")
	if hasLat && hasLon {
		b.WriteString(strconv.FormatFloat(lat, 'f', -1, 64))
		b.WriteString(",")
		b.WriteString(strconv.FormatFloat(lon, 'f', -1, 64))
	} else {
		b.WriteString(location)
	}
	b.WriteString("|netatmo=")
	b.WriteString(strconv.FormatInt(netatmoUserID, 10))
	return b.String()
}
