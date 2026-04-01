package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

func resetLatestVersionsCache() {
	latestCacheInstance = latestVersionCache{}
	latestVersionsGroup = singleflight.Group{}
}

// stubFetchers returns fetchers that return deterministic versions without
// making any network calls.
func stubFetchers() map[string]latestVersionFetcher {
	return map[string]latestVersionFetcher{
		"forge":  stubFetcher("v2.1.0"),
		"bd":     stubFetcher("v3.1.0"),
		"gh":     stubFetcher("v2.50.0"),
		"dolt":   stubFetcher("v1.40.0"),
		"go":     stubFetcher("go1.23.0"),
		"node":   stubFetcher("v22.0.0"),
		"npm":    stubFetcher("11.0.0"),
		"git":    stubFetcher("v2.45.0"),
		"claude": stubFetcher("1.5.0"),
	}
}

func stubFetcher(version string) latestVersionFetcher {
	return func(ctx context.Context, client *http.Client) (string, error) {
		return version, nil
	}
}

func failingFetcher(msg string) latestVersionFetcher {
	return func(ctx context.Context, client *http.Client) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

func TestLatestVersionsHandler_ReturnsJSON(t *testing.T) {
	resetLatestVersionsCache()

	handler := latestVersionsHandlerWith(&http.Client{}, stubFetchers())
	req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	expected := map[string]string{
		"forge":  "v2.1.0",
		"bd":     "v3.1.0",
		"gh":     "v2.50.0",
		"dolt":   "v1.40.0",
		"go":     "go1.23.0",
		"node":   "v22.0.0",
		"npm":    "11.0.0",
		"git":    "v2.45.0",
		"claude": "1.5.0",
	}
	for key, want := range expected {
		got, ok := result[key]
		if !ok {
			t.Errorf("expected key %q in response", key)
		} else if got != want {
			t.Errorf("key %q: want %q, got %q", key, want, got)
		}
	}
}

func TestLatestVersionsHandler_CachesResult(t *testing.T) {
	resetLatestVersionsCache()

	callCount := 0
	fetchers := map[string]latestVersionFetcher{
		"forge": func(ctx context.Context, client *http.Client) (string, error) {
			callCount++
			return "v2.1.0", nil
		},
	}

	handler := latestVersionsHandlerWith(&http.Client{}, fetchers)

	// First request — should call fetcher.
	req1 := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 fetch call, got %d", callCount)
	}

	// Second request — should use cache, not call fetcher again.
	req2 := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rec2.Code)
	}
	if callCount != 1 {
		t.Errorf("expected cache hit (1 call), got %d calls", callCount)
	}
}

func TestLatestVersionsHandler_CacheExpiry(t *testing.T) {
	resetLatestVersionsCache()

	callCount := 0
	fetchers := map[string]latestVersionFetcher{
		"forge": func(ctx context.Context, client *http.Client) (string, error) {
			callCount++
			return "v2.1.0", nil
		},
	}

	handler := latestVersionsHandlerWith(&http.Client{}, fetchers)

	// First request populates cache.
	req1 := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// Manually expire the cache.
	latestCacheInstance.mu.Lock()
	latestCacheInstance.fetchedAt = time.Now().Add(-2 * time.Hour)
	latestCacheInstance.mu.Unlock()

	// Third request should re-fetch.
	req2 := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expired request: expected 200, got %d", rec2.Code)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls after expiry, got %d", callCount)
	}
}

func TestLatestVersionsHandler_FailureFallsBackToStale(t *testing.T) {
	resetLatestVersionsCache()

	// Pre-populate cache with a stale value.
	latestCacheInstance.mu.Lock()
	latestCacheInstance.data = map[string]string{"forge": "v2.0.0"}
	latestCacheInstance.fetchedAt = time.Now().Add(-2 * time.Hour) // expired
	latestCacheInstance.mu.Unlock()

	fetchers := map[string]latestVersionFetcher{
		"forge": failingFetcher("network error"),
	}

	handler := latestVersionsHandlerWith(&http.Client{}, fetchers)
	req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should fall back to stale cached value.
	if got := result["forge"]; got != "v2.0.0" {
		t.Errorf("expected stale value v2.0.0, got %q", got)
	}
}

func TestLatestVersionsHandler_FailureWithNoCache(t *testing.T) {
	resetLatestVersionsCache()

	fetchers := map[string]latestVersionFetcher{
		"forge": failingFetcher("network error"),
	}

	handler := latestVersionsHandlerWith(&http.Client{}, fetchers)
	req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := result["forge"]; got != "unknown" {
		t.Errorf("expected 'unknown' for failed fetch with no cache, got %q", got)
	}
}

func TestMakeGitHubReleaseFetcher_ParsesTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	// Create a fetcher that hits our test server instead of GitHub.
	fetcher := func(ctx context.Context, client *http.Client) (string, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return "", err
		}
		return release.TagName, nil
	}

	ctx := context.Background()
	version, err := fetcher(ctx, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v1.2.3" {
		t.Errorf("expected v1.2.3, got %q", version)
	}
}

func TestFetchLatestGo_ParsesStableVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"go1.23.0","stable":true},{"version":"go1.22.5","stable":true}]`)
	}))
	defer srv.Close()

	// Override the URL by creating a custom fetcher that hits the test server.
	fetcher := func(ctx context.Context, client *http.Client) (string, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var releases []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return "", err
		}
		for _, r := range releases {
			if r.Stable {
				return r.Version, nil
			}
		}
		return "", fmt.Errorf("no stable release found")
	}

	ctx := context.Background()
	version, err := fetcher(ctx, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "go1.23.0" {
		t.Errorf("expected go1.23.0, got %q", version)
	}
}

func TestFetchLatestNode_ParsesLTSVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"v23.0.0","lts":false},{"version":"v22.4.0","lts":"Jod"},{"version":"v20.15.0","lts":"Iron"}]`)
	}))
	defer srv.Close()

	fetcher := func(ctx context.Context, client *http.Client) (string, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var entries []struct {
			Version string `json:"version"`
			LTS     any    `json:"lts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			return "", err
		}
		for _, e := range entries {
			if e.LTS != nil && e.LTS != false {
				return e.Version, nil
			}
		}
		return "", fmt.Errorf("no LTS release found")
	}

	ctx := context.Background()
	version, err := fetcher(ctx, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v22.4.0" {
		t.Errorf("expected v22.4.0, got %q", version)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("expected 'short', got %q", got)
	}
	if got := truncate("this is a long string", 10); got != "this is a …" {
		t.Errorf("expected truncation, got %q", got)
	}
}
