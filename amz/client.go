package amz

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

// Client is a polite, block-aware HTTP client for one marketplace.
//
// One connection, one request at a time, one honest identity. The concurrency
// that used to live here is gone: two requests in flight made --rate a lie by a
// factor of two, and Amazon rate-scores the session and not the request.
type Client struct {
	hc      *http.Client
	mkt     Marketplace
	delay   time.Duration
	retries int
	cache   *Cache
	noCache bool
	refresh bool

	base string // overrides the marketplace origin (tests / proxies)

	mu   sync.Mutex
	next time.Time
}

// NewClient builds a client from a resolved config.
func NewClient(cfg Config) *Client {
	mkt, _ := LookupMarketplace(cfg.Marketplace)

	// The jar holds the session cookie Amazon sets on the first request, so the
	// run is a coherent session. It is never loaded from disk and never written
	// to one: nothing amz reads needs a signed-in session, and a tool that can
	// borrow one will be pointed at surfaces that require one.
	jar, _ := cookiejar.New(nil)

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxConnsPerHost = 1
	tr.MaxIdleConnsPerHost = 1

	return &Client{
		hc:      &http.Client{Timeout: cfg.Timeout, Jar: jar, Transport: tr},
		mkt:     mkt,
		delay:   ClampDelay(cfg.Delay),
		retries: cfg.Retries,
		noCache: cfg.NoCache,
		refresh: cfg.Refresh,
		cache:   newCacheIf(cfg.CacheDir),
	}
}

func newCacheIf(dir string) *Cache {
	if dir == "" {
		return nil
	}
	return NewCache(dir)
}

// Marketplace returns the client's marketplace.
func (c *Client) Marketplace() Marketplace { return c.mkt }

// Delay returns the spacing the client keeps between requests, after the floor
// has been applied.
func (c *Client) Delay() time.Duration { return c.delay }

// BaseURL returns the marketplace origin, or the override when set.
func (c *Client) BaseURL() string {
	if c.base != "" {
		return c.base
	}
	return c.mkt.BaseURL()
}

// SetBaseURL overrides the marketplace origin. It exists so the fetchers can be
// pointed at a local fixture server or an outbound proxy; production code leaves
// it unset and uses the marketplace host.
func (c *Client) SetBaseURL(base string) { c.base = strings.TrimSuffix(base, "/") }

func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if now.Before(c.next) {
		time.Sleep(c.next.Sub(now))
	}
	c.next = time.Now().Add(c.delay)
}

// Get fetches a URL and returns its body, using the cache when allowed and
// detecting the bot wall. It retries transient 429/503/5xx with backoff.
func (c *Client) Get(ctx context.Context, rawURL string, ttl time.Duration) ([]byte, error) {
	if c.cache != nil && !c.noCache && !c.refresh {
		if b, ok := c.cache.Get(rawURL, ttl); ok {
			return b, nil
		}
	}
	body, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if c.cache != nil && !c.noCache {
		_ = c.cache.Put(rawURL, body)
	}
	return body, nil
}

func (c *Client) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	backoff := []time.Duration{0, 10 * time.Second, 40 * time.Second, 90 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if d := backoff[min(attempt, len(backoff)-1)]; d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		c.throttle()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header = Headers()
		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, rerr := readBody(resp)
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if DetectBlocked(body) {
			return nil, ErrBlocked
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("giving up on %s", rawURL)
	}
	return nil, lastErr
}

// readBody reads the response, decompressing when needed.
//
// Go's transport normally adds Accept-Encoding and decompresses transparently,
// but only when the caller has not set the header itself. amz sets it, because
// the header set is declared in one place and is asserted exactly, so the
// decompression is ours to do.
func readBody(resp *http.Response) ([]byte, error) {
	r := resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}
	return io.ReadAll(r)
}
