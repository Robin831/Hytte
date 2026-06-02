package kiosk

import (
	"testing"
	"time"
)

func TestTTLCache_HitWithinTTL(t *testing.T) {
	c := NewTTLCache()
	want := KioskData{FetchedAt: time.Now()}
	c.Set("k", want)

	got, ok := c.Get("k")
	if !ok {
		t.Fatal("expected cache hit within TTL")
	}
	if !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("expected cached FetchedAt %v, got %v", want.FetchedAt, got.FetchedAt)
	}
}

func TestTTLCache_MissForUnknownKey(t *testing.T) {
	c := NewTTLCache()
	if _, ok := c.Get("nope"); ok {
		t.Error("expected miss for unknown key")
	}
}

func TestTTLCache_ExpiredEntryIsMiss(t *testing.T) {
	c := NewTTLCache()
	// Insert an already-expired entry directly to avoid sleeping for CacheTTL.
	c.mu.Lock()
	c.items["k"] = entry{value: KioskData{}, expiresAt: time.Now().Add(-time.Second)}
	c.mu.Unlock()

	if _, ok := c.Get("k"); ok {
		t.Error("expected expired entry to be treated as a miss")
	}
	// Expired entry should have been evicted on read.
	c.mu.Lock()
	_, present := c.items["k"]
	c.mu.Unlock()
	if present {
		t.Error("expected expired entry to be removed on read")
	}
}

func TestBuildCacheKey_IdenticalInputsMatch(t *testing.T) {
	a := buildCacheKey([]string{"A", "B"}, 0, 0, false, "Bergen", 7)
	// Stop ID order must not matter (keys are sorted).
	b := buildCacheKey([]string{"B", "A"}, 0, 0, false, "Bergen", 7)
	if a != b {
		t.Errorf("expected identical keys regardless of stop order: %q vs %q", a, b)
	}
}

func TestBuildCacheKey_DistinctInputsDiffer(t *testing.T) {
	base := buildCacheKey([]string{"A"}, 0, 0, false, "Bergen", 1)

	cases := map[string]string{
		"different stop_ids":  buildCacheKey([]string{"A", "C"}, 0, 0, false, "Bergen", 1),
		"different location":  buildCacheKey([]string{"A"}, 0, 0, false, "Oslo", 1),
		"different netatmo":   buildCacheKey([]string{"A"}, 0, 0, false, "Bergen", 2),
		"lat/lon vs location": buildCacheKey([]string{"A"}, 59.9, 10.7, true, "Bergen", 1),
	}
	for name, key := range cases {
		if key == base {
			t.Errorf("%s: expected distinct cache key, got same as base %q", name, base)
		}
	}
}
