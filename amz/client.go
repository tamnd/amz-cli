package amz

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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

	robots    *robotsStore
	noRobots  bool
	notes     io.Writer
	noteOnce  sync.Map // rule string -> struct{}, so a banner line prints once
	base      string   // overrides the marketplace origin (tests / proxies)
	noRobotsW sync.Once

	// forceRobots makes the gate apply to a non-marketplace base URL. Set only
	// by tests, via export_test.go.
	forceRobots bool

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
		hc:       &http.Client{Timeout: cfg.Timeout, Jar: jar, Transport: tr},
		mkt:      mkt,
		delay:    ClampDelayWith(cfg.Delay, cfg.NoRobots),
		retries:  cfg.Retries,
		noCache:  cfg.NoCache,
		refresh:  cfg.Refresh,
		cache:    newCacheIf(cfg.CacheDir),
		robots:   newRobotsStore(cfg.CacheDir),
		noRobots: cfg.NoRobots,
		notes:    io.Discard,
	}
}

// SetNotes is where the client writes the lines that must not be silent: the
// --no-robots banner and every rule it overrides. The CLI points this at stderr.
func (c *Client) SetNotes(w io.Writer) {
	if w != nil {
		c.notes = w
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
	// The rules check runs before the cache, not after it. A page cached
	// yesterday under a rule that has since changed is still a page amz is no
	// longer allowed to serve, and robots.txt is itself cached, so this is a map
	// lookup and not a request.
	if err := c.CheckRobots(ctx, rawURL); err != nil {
		return nil, err
	}
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

// Robots returns the parsed robots.txt for the host this client reads, fetching
// it if the cached copy is older than RobotsTTL.
func (c *Client) Robots(ctx context.Context) (*Robots, error) {
	host, err := c.host()
	if err != nil {
		return nil, err
	}
	return c.robots.get(ctx, c.BaseURL(), host, c.fetch)
}

func (c *Client) host() (string, error) {
	u, err := url.Parse(c.BaseURL())
	if err != nil {
		return "", err
	}
	return u.Host, nil
}

// CheckRobots asks the live robots.txt about a URL.
//
// It returns ErrRobotsUnavailable when the file cannot be had, and a
// *DisallowedError naming the rule when the file refuses. Under --no-robots it
// returns nil either way and prints what it is overriding.
func (c *Client) CheckRobots(ctx context.Context, rawURL string) error {
	if c.noRobots {
		c.noRobotsBanner()
	}

	// robots.txt is fetched by the raw path, so asking about it would loop.
	if strings.HasSuffix(pathAndQuery(rawURL), "/robots.txt") {
		return nil
	}
	// A fixture server or proxy is not amazon.com and has no rules to break.
	if c.base != "" && !c.forceRobots && !c.hostIsMarketplace() {
		return nil
	}

	r, err := c.Robots(ctx)
	if err != nil {
		if c.noRobots {
			c.note("amz: robots.txt is unreadable. The override is on, so amz is proceeding blind.")
			return nil
		}
		return err
	}

	allowed, rule := r.Test(rawURL)
	c.compareToExpectation(rawURL, allowed)
	if allowed {
		return nil
	}
	if c.noRobots {
		c.noteOnceFor(rule.String(), fmt.Sprintf("amz: overriding %q for %s", rule.String(), rawURL))
		return nil
	}
	return &DisallowedError{URL: rawURL, Agent: r.GroupName(), Rule: rule}
}

func (c *Client) hostIsMarketplace() bool {
	h, err := c.host()
	if err != nil {
		return false
	}
	return strings.HasSuffix(h, "amazon.com") || strings.Contains(h, "amazon.")
}

// compareToExpectation prints a note when the live file disagrees with what the
// Ops registry was told at measurement time. The live file wins; the note exists
// so the stale row gets fixed rather than trusted.
func (c *Client) compareToExpectation(rawURL string, allowed bool) {
	op := OpFor(rawURL)
	if op == nil || op.Robots == RobotsUnknown || op.Robots == RobotsNA {
		return
	}
	want := op.Robots == RobotsAllowed
	if want == allowed {
		return
	}
	c.noteOnceFor("expect:"+op.Name, fmt.Sprintf(
		"amz: robots.txt now says %q for surface %s (%s), the registry says %q as of %s; following the live file",
		allowStr(allowed), op.Name, op.ID, op.Robots, op.Since))
}

func allowStr(b bool) string {
	if b {
		return "allowed"
	}
	return "disallowed"
}

func (c *Client) note(s string) { _, _ = fmt.Fprintln(c.notes, s) }

func (c *Client) noteOnceFor(key, s string) {
	if _, loaded := c.noteOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	c.note(s)
}

// noRobotsBanner is four lines, printed once per run, before the first request.
//
// The flag is the user's call and amz honours it. It does not get to be quiet
// about it.
func (c *Client) noRobotsBanner() {
	c.noRobotsW.Do(func() {
		c.note("amz: --no-robots is set. amazon.com's robots.txt will be read, and then ignored.")
		c.note("amz: this is your decision, recorded here rather than hidden. It applies to this run only.")
		c.note("amz: the pace floor rises to " + MinDelayNoRobots.String() + " and every rule amz breaks is printed as it breaks it.")
		c.note("amz: stop now if that was not deliberate.")
	})
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
