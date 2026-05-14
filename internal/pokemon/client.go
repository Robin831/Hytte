package pokemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// DefaultBaseURL is the production pokemontcg.io v2 API base URL.
const DefaultBaseURL = "https://api.pokemontcg.io/v2"

// Client talks to the pokemontcg.io REST API. The zero value is unusable —
// construct via NewClient.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	// maxRetries bounds 429 retry attempts. Defaults to 3 in NewClient.
	maxRetries int
	// sleep is used in place of a context-aware timer so tests can avoid real waits.
	sleep func(ctx context.Context, d time.Duration) error
}

// NewClient returns a client preconfigured with the POKEMONTCG_API_KEY env var
// (when set) and a 30s HTTP timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    DefaultBaseURL,
		apiKey:     os.Getenv("POKEMONTCG_API_KEY"),
		maxRetries: 3,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
}

// WithBaseURL overrides the API base URL (used by tests with httptest).
func (c *Client) WithBaseURL(url string) *Client {
	c.baseURL = url
	return c
}

// WithHTTPClient overrides the underlying *http.Client.
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	c.httpClient = hc
	return c
}

// withSleep is exposed for tests to bypass real sleeps on 429 retries.
func (c *Client) withSleep(fn func(context.Context, time.Duration) error) *Client {
	c.sleep = fn
	return c
}

// doRequest issues a GET against the given absolute URL and decodes JSON into
// out. On HTTP 429 it inspects the Retry-After header (seconds), sleeps, and
// retries up to maxRetries times.
func (c *Client) doRequest(ctx context.Context, url string, out any) error {
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		if c.apiKey != "" {
			req.Header.Set("X-Api-Key", c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("http get %s: %w", url, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			if attempt == c.maxRetries {
				return fmt.Errorf("rate limited (429) after %d retries: %s", c.maxRetries, url)
			}
			if retryAfter <= 0 {
				retryAfter = time.Duration(attempt+1) * time.Second
			}
			if err := c.sleep(ctx, retryAfter); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return fmt.Errorf("pokemontcg %d: %s — %s", resp.StatusCode, url, string(body))
		}

		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			resp.Body.Close()
			return fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()
		return nil
	}
	return fmt.Errorf("unreachable: retry loop exited without return")
}

// parseRetryAfter parses the Retry-After header value as either a delta-seconds
// integer or returns zero if unparseable. We deliberately ignore the HTTP-date
// form because pokemontcg.io only emits integer seconds.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
