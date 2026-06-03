package news

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	cacheTTL     = 10 * time.Minute
	fetchTimeout = 12 * time.Second
	maxFeedSize  = 5 << 20 // 5 MB
	userAgent    = "Hytte/1.0 (+https://robinedvardsmith.com)"
)

type cachedFeed struct {
	articles []Article
	expires  time.Time
}

// Service fetches and caches RSS feeds. Safe for concurrent use.
type Service struct {
	client *http.Client

	mu    sync.RWMutex
	cache map[string]cachedFeed // keyed by feed URL

	group singleflight.Group

	// scoring tracks users with an in-flight background scoring job so we never
	// run two at once for the same user.
	scoring sync.Map // userID(int64) -> struct{}
}

// NewService creates a news service with production defaults.
func NewService() *Service {
	return &Service{
		client: &http.Client{Timeout: fetchTimeout},
		cache:  make(map[string]cachedFeed),
	}
}

// tryStartScoring marks a user's background scoring job as started. It returns
// false if one is already running for that user.
func (s *Service) tryStartScoring(userID int64) bool {
	_, loaded := s.scoring.LoadOrStore(userID, struct{}{})
	return !loaded
}

func (s *Service) finishScoring(userID int64) {
	s.scoring.Delete(userID)
}

// fetchSource returns articles for one source, using the cache when warm and
// collapsing concurrent fetches of the same feed via singleflight.
func (s *Service) fetchSource(ctx context.Context, src Source) ([]Article, error) {
	s.mu.RLock()
	if c, ok := s.cache[src.FeedURL]; ok && time.Now().Before(c.expires) {
		s.mu.RUnlock()
		return c.articles, nil
	}
	s.mu.RUnlock()

	v, err, _ := s.group.Do(src.FeedURL, func() (any, error) {
		// Re-check the cache inside the singleflight in case another caller
		// just populated it.
		s.mu.RLock()
		if c, ok := s.cache[src.FeedURL]; ok && time.Now().Before(c.expires) {
			s.mu.RUnlock()
			return c.articles, nil
		}
		s.mu.RUnlock()

		articles, err := s.download(ctx, src)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.cache[src.FeedURL] = cachedFeed{articles: articles, expires: time.Now().Add(cacheTTL)}
		s.mu.Unlock()
		return articles, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Article), nil
}

func (s *Service) download(ctx context.Context, src Source) ([]Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.FeedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed %s: status %d", src.Key, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedSize))
	if err != nil {
		return nil, err
	}
	return parseFeed(src, body)
}

// FetchAll fetches all enabled sources concurrently and returns a merged,
// newest-first article list. Individual feed failures are logged and skipped so
// one broken source never blanks the page.
func (s *Service) FetchAll(ctx context.Context, sources []Source) []Article {
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		all []Article
	)
	for _, src := range sources {
		if !src.Enabled || src.FeedURL == "" {
			continue
		}
		wg.Add(1)
		go func(src Source) {
			defer wg.Done()
			articles, err := s.fetchSource(ctx, src)
			if err != nil {
				log.Printf("news: fetch %s failed: %v", src.Key, err)
				return
			}
			mu.Lock()
			all = append(all, articles...)
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	sort.SliceStable(all, func(i, j int) bool {
		return all[i].PublishedAt.After(all[j].PublishedAt)
	})
	return all
}
