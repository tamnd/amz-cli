package amz

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"time"
)

// userAgents is a small pool of realistic desktop browser UAs, rotated per request.
var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:124.0) Gecko/20100101 Firefox/124.0",
}

// Client is a polite, block-aware HTTP client for one marketplace.
type Client struct {
	hc      *http.Client
	mkt     Marketplace
	delay   time.Duration
	retries int
	cache   *Cache
	noCache bool
	refresh bool

	base string // overrides the marketplace origin (tests / proxies)

	mu      sync.Mutex
	next    time.Time
	uaIndex int
}

// NewClient builds a client from a resolved config.
func NewClient(cfg Config) *Client {
	mkt, _ := LookupMarketplace(cfg.Marketplace)
	jar, _ := cookiejar.New(nil)
	c := &Client{
		hc:      &http.Client{Timeout: cfg.Timeout, Jar: jar},
		mkt:     mkt,
		delay:   cfg.Delay,
		retries: cfg.Retries,
		noCache: cfg.NoCache,
		refresh: cfg.Refresh,
	}
	if cfg.CacheDir != "" {
		c.cache = NewCache(cfg.CacheDir)
	}
	if cfg.Cookies != "" {
		_ = c.loadCookies(cfg.Cookies)
	}
	return c
}

// Marketplace returns the client's marketplace.
func (c *Client) Marketplace() Marketplace { return c.mkt }

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

func (c *Client) ua() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ua := userAgents[c.uaIndex%len(userAgents)]
	c.uaIndex++
	return ua
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
		c.setHeaders(req)
		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
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

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.ua())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", c.mkt.Language)
	req.Header.Set("Referer", c.mkt.BaseURL()+"/")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
}

// loadCookies reads a Netscape cookies.txt file into the jar for the marketplace host.
func (c *Client) loadCookies(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cookies []*http.Cookie
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Netscape format: domain \t flag \t path \t secure \t expiry \t name \t value
		fields := strings.Split(line, "\t")
		if len(fields) >= 7 {
			cookies = append(cookies, &http.Cookie{Name: fields[5], Value: fields[6]})
			continue
		}
		// header form: "name=value; name2=value2"
		for _, part := range strings.Split(line, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 {
				cookies = append(cookies, &http.Cookie{Name: kv[0], Value: kv[1]})
			}
		}
	}
	if u, err := requestURL(c.mkt.BaseURL()); err == nil {
		c.hc.Jar.SetCookies(u, cookies)
	}
	return sc.Err()
}
