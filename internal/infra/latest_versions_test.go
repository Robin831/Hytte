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

func resetLatestVersionsCache() {
	latestCacheInstance = latestVersionCache{}
	latestVersionsGroup = singleflight.Group{}
}

// stubFetcher returns a fetcher that returns a fixed version.
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

func TestLatestVersionsHandler_ReturnsJSON(t *testing.T) {
	resetLatestVersionsCache()

	handler := latestVersionsHandlerWith(&http.Client{}, stubFetchers())
	req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result []latestVersionEntry
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

	if len(result) != len(expected) {
		t.Fatalf("expected %d entries, got %d", len(expected), len(result))
	}
	for _, entry := range result {
		want, ok := expected[entry.Name]
		if !ok {
			t.Errorf("unexpected key %q in response", entry.Name)
		} else if entry.Version != want {
			t.Errorf("key %q: want %q, got %q", entry.Name, want, entry.Version)
		}
	}
}

func TestLatestVersionsHandler_SortedOutput(t *testing.T) {
	resetLatestVersionsCache()

	handler := latestVersionsHandlerWith(&http.Client{}, stubFetchers())
	req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var result []latestVersionEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify entries are sorted alphabetically by name.
	for i := 1; i < len(result); i++ {
		if result[i].Name < result[i-1].Name {
			t.Errorf("entries not sorted: %q comes after %q", result[i].Name, result[i-1].Name)
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

	// Next request should re-fetch.
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

	var result []latestVersionEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should fall back to stale cached value.
	if len(result) != 1 || result[0].Name != "forge" || result[0].Version != "v2.0.0" {
		t.Errorf("expected stale value v2.0.0 for forge, got %+v", result)
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

	var result []latestVersionEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) != 1 || result[0].Version != "unknown" {
		t.Errorf("expected 'unknown' for failed fetch with no cache, got %+v", result)
	}
}

// TestLatestVersionsHandler_ConcurrentRequestsShareFetch verifies that
// multiple simultaneous cache-miss requests only trigger one upstream fetch.
func TestLatestVersionsHandler_ConcurrentRequestsShareFetch(t *testing.T) {
	resetLatestVersionsCache()

	var fetchCount int
	var mu sync.Mutex
	ready := make(chan struct{})

	fetchers := map[string]latestVersionFetcher{
		"forge": func(ctx context.Context, client *http.Client) (string, error) {
			// Block until all goroutines are ready, then count the call.
			<-ready
			mu.Lock()
			fetchCount++
			mu.Unlock()
			return "v2.1.0", nil
		},
	}

	handler := latestVersionsHandlerWith(&http.Client{}, fetchers)

	const n = 10
	results := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/api/infra/latest-versions", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}

	// Release all goroutines at once to create a concurrent cache miss.
	close(ready)

	for i := 0; i < n; i++ {
		code := <-results
		if code != http.StatusOK {
			t.Errorf("goroutine %d: expected 200, got %d", i, code)
		}
	}

	mu.Lock()
	got := fetchCount
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected exactly 1 upstream fetch for %d concurrent requests, got %d", n, got)
	}
}

// TestMakeGitHubReleaseFetcher tests the actual makeGitHubReleaseFetcher by
// pointing it at a test HTTP server that mimics the GitHub releases API.
func TestMakeGitHubReleaseFetcher_ParsesTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected Accept header application/vnd.github+json, got %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	// Build a fetcher that targets the test server via a custom transport.
	fetcher := makeGitHubReleaseFetcher("testowner", "testrepo")

	// Create a client whose transport rewrites the URL to point at our test server.
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	ctx := context.Background()
	version, err := fetcher(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v1.2.3" {
		t.Errorf("expected v1.2.3, got %q", version)
	}
}

func TestMakeGitHubReleaseFetcher_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"rate limit exceeded"}`)
	}))
	defer srv.Close()

	fetcher := makeGitHubReleaseFetcher("testowner", "testrepo")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetcher(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

// TestFetchLatestGo tests the actual fetchLatestGo function via a test server.
func TestFetchLatestGo_ParsesStableVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"go1.23.0","stable":true},{"version":"go1.22.5","stable":true}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetchLatestGo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "go1.23.0" {
		t.Errorf("expected go1.23.0, got %q", version)
	}
}

func TestFetchLatestGo_NoStableRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"go1.24rc1","stable":false}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetchLatestGo(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for no stable release")
	}
}

// TestFetchLatestNode tests the actual fetchLatestNode function via a test server.
func TestFetchLatestNode_ParsesLTSVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"v23.0.0","lts":false},{"version":"v22.4.0","lts":"Jod"},{"version":"v20.15.0","lts":"Iron"}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetchLatestNode(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v22.4.0" {
		t.Errorf("expected v22.4.0, got %q", version)
	}
}

func TestFetchLatestNode_NoLTSRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version":"v23.0.0","lts":false}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetchLatestNode(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for no LTS release")
	}
}

// TestFetchLatestNpm tests the actual fetchLatestNpm function via a test server.
func TestFetchLatestNpm_ParsesVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"11.0.0","name":"npm"}`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetchLatestNpm(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "11.0.0" {
		t.Errorf("expected 11.0.0, got %q", version)
	}
}

// TestMakeGitHubRepoIDReleaseFetcher tests fetching releases by repository ID.
func TestMakeGitHubRepoIDReleaseFetcher_ParsesTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected Accept header application/vnd.github+json, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.8.0"})
	}))
	defer srv.Close()

	fetcher := makeGitHubRepoIDReleaseFetcher(1074561042)
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetcher(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v0.8.0" {
		t.Errorf("expected v0.8.0, got %q", version)
	}
}

func TestMakeGitHubRepoIDReleaseFetcher_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	fetcher := makeGitHubRepoIDReleaseFetcher(1074561042)
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetcher(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

// TestFetchLatestGitTag tests the tag-based fetcher for git/git.
func TestFetchLatestGitTag_FindsStableTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"v2.46.0-rc0"},{"name":"v2.45.2"},{"name":"v2.45.1"},{"name":"v2.45.0"}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetchLatestGitTag(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v2.45.2" {
		t.Errorf("expected v2.45.2, got %q", version)
	}
}

func TestFetchLatestGitTag_FiltersRCTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"v2.46.0-rc2"},{"name":"v2.46.0-rc1"},{"name":"v2.46.0-rc0"}]`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetchLatestGitTag(context.Background(), client)
	if err == nil {
		t.Fatal("expected error when no stable tags exist")
	}
}

// TestFetchLatestClaude tests the npm-registry-based Claude version fetcher.
func TestFetchLatestClaude_ParsesVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"1.0.33","name":"@anthropic-ai/claude-code"}`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	version, err := fetchLatestClaude(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.0.33" {
		t.Errorf("expected 1.0.33, got %q", version)
	}
}

func TestFetchLatestClaude_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	_, err := fetchLatestClaude(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
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

func TestSortedVersions(t *testing.T) {
	input := map[string]string{
		"npm":  "11.0.0",
		"go":   "go1.23.0",
		"bd":   "v3.1.0",
		"node": "v22.0.0",
	}
	result := sortedVersions(input)
	if len(result) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(result))
	}
	expectedOrder := []string{"bd", "go", "node", "npm"}
	for i, want := range expectedOrder {
		if result[i].Name != want {
			t.Errorf("position %d: expected %q, got %q", i, want, result[i].Name)
		}
	}
}

func TestCopyMap(t *testing.T) {
	orig := map[string]string{"a": "1", "b": "2"}
	cp := copyMap(orig)

	// Modify original, copy should be unaffected.
	orig["a"] = "changed"
	if cp["a"] != "1" {
		t.Errorf("copy was affected by original mutation: got %q", cp["a"])
	}
}

// roundTripFunc is a helper that implements http.RoundTripper via a function,
// allowing tests to redirect requests to a test server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
