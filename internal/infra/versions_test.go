package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

func resetVersionsCache() {
	versionsCacheInstance = versionsCache{}
	versionsGroup = singleflight.Group{}
}

// stubRunner returns a commandRunner that produces deterministic fake output
// for each known tool command, without spawning any real processes.
func stubRunner() commandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		responses := map[string]string{
			"claude": "claude 1.0.0",
			"forge":  "forge 2.0.0",
			"bd":     "bd 3.0.0",
			"go":     "go version go1.22.0 linux/amd64",
			"node":   "v20.0.0",
			"npm":    "10.0.0",
			"gh":     "gh version 2.40.0 (2024-01-01)",
			"git":    "git version 2.43.0",
			"dolt":   "dolt version 1.0.0",
		}
		if out, ok := responses[name]; ok {
			return []byte(out), nil
		}
		return nil, fmt.Errorf("stub: unknown command %q", name)
	}
}

func TestVersionsHandler_ReturnsJSON(t *testing.T) {
	resetVersionsCache()

	req := httptest.NewRequest("GET", "/api/infra/versions", nil)
	rec := httptest.NewRecorder()
	versionsHandlerWithRunner(stubRunner()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// All standard tool keys must be present regardless of whether the
	// command succeeds or fails (failures return "unavailable").
	for _, key := range []string{"claude", "forge", "bd", "go", "node", "npm", "gh", "git", "dolt"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in response", key)
		}
	}
}

func TestVersionsHandler_ForgeHeadOmittedWhenEnvUnset(t *testing.T) {
	resetVersionsCache()
	t.Setenv("FORGE_REPO_DIR", "")

	req := httptest.NewRequest("GET", "/api/infra/versions", nil)
	rec := httptest.NewRecorder()
	versionsHandlerWithRunner(stubRunner()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := result["forge_head"]; ok {
		t.Error("forge_head should not be present when FORGE_REPO_DIR is unset")
	}
}

func TestVersionsHandler_CachesResult(t *testing.T) {
	resetVersionsCache()

	handler := versionsHandlerWithRunner(stubRunner())

	req1 := httptest.NewRequest("GET", "/api/infra/versions", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	// Manually verify the cache was populated.
	versionsCacheInstance.mu.Lock()
	cached := versionsCacheInstance.data
	fetchedAt := versionsCacheInstance.fetchedAt
	versionsCacheInstance.mu.Unlock()

	if cached == nil {
		t.Fatal("expected cache to be populated after first request")
	}
	if time.Since(fetchedAt) > 5*time.Second {
		t.Error("cache timestamp looks stale immediately after population")
	}

	req2 := httptest.NewRequest("GET", "/api/infra/versions", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rec2.Code)
	}

	var result1, result2 map[string]string
	if err := json.NewDecoder(rec1.Body).Decode(&result1); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.NewDecoder(rec2.Body).Decode(&result2); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	for k, v1 := range result1 {
		v2, ok := result2[k]
		if !ok || v1 != v2 {
			t.Errorf("cache inconsistency for key %q: first=%q, second=%q", k, v1, v2)
		}
	}
}

func TestVersionsHandler_ConcurrentRequestsShareFetch(t *testing.T) {
	resetVersionsCache()

	const goroutines = 10
	handler := versionsHandlerWithRunner(stubRunner())

	var wg sync.WaitGroup
	codes := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/infra/versions", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("goroutine %d: expected 200, got %d", i, code)
		}
	}
}
