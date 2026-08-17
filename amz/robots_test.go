package amz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixture is amazon.com's real robots.txt, fetched 2026-08-17, 436 lines and
// 7,887 bytes. The rules asserted below are lines in that file, not inventions.
func liveRobots(t *testing.T) *Robots {
	t.Helper()
	b, err := os.ReadFile("testdata/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	return ParseRobots("www.amazon.com", b, time.Now())
}

// TestRobotsShape pins what the file is, so a refetch that changes its shape is
// visible rather than silent.
func TestRobotsShape(t *testing.T) {
	r := liveRobots(t)
	// 101 User-agent lines, 100 distinct groups: ClaudeBot is listed twice, and
	// its two blocks are one group, not two.
	if got := len(r.Groups()); got != 100 {
		t.Errorf("groups = %d, want 100", got)
	}
	if got := strings.Count(r.Raw, "laudeBot"); got != 2 {
		t.Errorf("ClaudeBot appears %d times, want the duplicate this test is about", got)
	}
	if got := r.GroupName(); got != "*" {
		t.Errorf("amz falls under group %q, want the fallback", got)
	}
	if got := len(r.Rules()); got != 135 {
		t.Errorf("the * group has %d rules, want 135", got)
	}
}

// TestCaseInsensitiveDirectives is not pedantry. amazon.com writes
// `User-Agent: PerplexityBot` on line 148 and `User-agent:` on the other hundred
// groups. A case-sensitive parser folds PerplexityBot's `Disallow: /` into the
// preceding group, and every request after that is refused for the wrong reason.
func TestCaseInsensitiveDirectives(t *testing.T) {
	r := liveRobots(t)
	if _, ok := r.groups["perplexitybot"]; !ok {
		t.Fatal("the User-Agent: PerplexityBot group was not parsed")
	}
	if allowed, _ := r.TestAgent("perplexitybot", "https://www.amazon.com/dp/B075F5X8BR"); allowed {
		t.Error("PerplexityBot is Disallow: / and should be refused")
	}
	if allowed, _ := r.Test("https://www.amazon.com/dp/B075F5X8BR"); !allowed {
		t.Error("amz-cli falls under * and /dp/ is allowed there")
	}
}

// TestRobotsQueryMatching is the finding that makes the matcher's shape
// non-obvious: five browse nodes are refused by a pattern that can only ever
// match inside a query string.
func TestRobotsQueryMatching(t *testing.T) {
	r := liveRobots(t)
	for _, node := range []string{"7454917011", "7454927011", "7454939011", "7454898011", "9052533011"} {
		u := "https://www.amazon.com/b?node=" + node
		if allowed, rule := r.Test(u); allowed {
			t.Errorf("%s should be disallowed by /b?*node=%s, matched %q", u, node, rule)
		}
	}
	if allowed, _ := r.Test("https://www.amazon.com/b?node=172282"); !allowed {
		t.Error("/b?node=172282 (Electronics) is allowed")
	}
	// The same node without the query is a different URL and is allowed.
	if allowed, _ := r.Test("https://www.amazon.com/b"); !allowed {
		t.Error("/b with no query is allowed")
	}
}

// TestLongestMatchWins covers the two rules that only work if the matcher scores
// by pattern length rather than by file order.
func TestLongestMatchWins(t *testing.T) {
	r := liveRobots(t)
	cases := []struct {
		url     string
		allowed bool
		why     string
	}{
		// Disallow: /gp/offer-listing/ with Allow: /gp/offer-listing/B000 under it.
		{"https://www.amazon.com/gp/offer-listing/B075F5X8BR", false, "offer listing is disallowed"},
		{"https://www.amazon.com/gp/offer-listing/B000123456", true, "except ASINs beginning B000"},
		{"https://www.amazon.com/gp/offer-listing/9000123456", true, "and ASINs beginning 9000"},
		// Disallow: /gp/aag with Allow: /gp/aag/main?*seller=ABVFEJU8LS620 under it.
		{"https://www.amazon.com/gp/aag/main?seller=A1234567890", false, "seller profiles under /gp/aag are disallowed"},
		{"https://www.amazon.com/gp/aag/main?seller=ABVFEJU8LS620", true, "one seller is permitted by name"},
		// A rule aimed at exactly one ASIN.
		{"https://www.amazon.com/product-reviews/B0069IY63Y", false, "one ASIN's reviews are disallowed by name"},
		{"https://www.amazon.com/product-reviews/B075F5X8BR", true, "every other ASIN's are not"},
	}
	for _, c := range cases {
		got, rule := r.Test(c.url)
		if got != c.allowed {
			t.Errorf("%s: allowed=%v, want %v (%s); matched %q", c.url, got, c.allowed, c.why, rule)
		}
	}
}

// TestDollarAnchoring covers the six `$` rules. Amazon uses them; Goodreads did
// not, which is why the matcher for 3008 could get away without one.
func TestDollarAnchoring(t *testing.T) {
	r := liveRobots(t)
	cases := []struct {
		url     string
		allowed bool
	}{
		{"https://www.amazon.com/slp/s", false},           // Disallow: /slp/s$
		{"https://www.amazon.com/slp/something", true},    // the $ stops it matching
		{"https://www.amazon.com/slp/keyboards/b", false}, // Disallow: /slp/*/b$
		{"https://www.amazon.com/slp/keyboards/bx", true},
		{"https://www.amazon.com/-/en", true}, // Allow: /-/en$ over Disallow: /-/
		{"https://www.amazon.com/-/en/dp/B075F5X8BR", false},
	}
	for _, c := range cases {
		if got, rule := r.Test(c.url); got != c.allowed {
			t.Errorf("%s: allowed=%v, want %v; matched %q", c.url, got, c.allowed, rule)
		}
	}
}

// TestEmptyDisallowAllowsAll covers the RFC rule that an empty Disallow value
// grants everything. amazon.com's file has none of these today, so the fixture is
// synthetic and says so.
func TestEmptyDisallowAllowsAll(t *testing.T) {
	r := ParseRobots("example.test", []byte("User-agent: *\nDisallow:\n"), time.Now())
	if allowed, rule := r.Test("https://example.test/anything"); !allowed {
		t.Errorf("empty Disallow should allow everything, matched %q", rule)
	}
	r2 := ParseRobots("example.test", []byte("User-agent: *\nDisallow: /private\nDisallow:\n"), time.Now())
	if allowed, _ := r2.Test("https://example.test/private/x"); allowed {
		t.Error("an empty Disallow does not cancel a real one")
	}
}

// TestAgentGroupSelection asserts amz does not route around a rule by renaming
// itself. If amazon.com ever names amz-cli, the named group applies.
func TestAgentGroupSelection(t *testing.T) {
	body := "User-agent: *\nDisallow: /private\n\nUser-agent: amz-cli\nDisallow: /\n"
	r := ParseRobots("example.test", []byte(body), time.Now())
	if got := r.GroupName(); got != "amz-cli" {
		t.Fatalf("group = %q, want the named group to win over *", got)
	}
	if allowed, _ := r.Test("https://example.test/dp/B075F5X8BR"); allowed {
		t.Error("a named Disallow: / means amz reads nothing, not that it falls back to *")
	}
}

// TestUnfetchableIsExit8 is the two-line mistake this whole file exists to
// prevent: a 500 on robots.txt does not mean "allowed".
func TestUnfetchableIsExit8(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("<html>the page amz must not reach</html>"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, false)
	_, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0)
	if !errors.Is(err, ErrRobotsUnavailable) {
		t.Fatalf("err = %v, want ErrRobotsUnavailable", err)
	}
	if hits > 4 {
		t.Errorf("%d requests: the page must not be attempted after robots.txt fails", hits)
	}
}

// TestDisallowedIsRefused checks the gate is in the fetch path and not just in
// the parser, and that the error names the rule.
func TestDisallowedIsRefused(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nDisallow: /gp/offer-listing/\n")
	c := newTestClient(t, srv.URL, false)

	_, err := c.Get(context.Background(), srv.URL+"/gp/offer-listing/B075F5X8BR", 0)
	if !errors.Is(err, ErrDisallowed) {
		t.Fatalf("err = %v, want ErrDisallowed", err)
	}
	var de *DisallowedError
	if !errors.As(err, &de) {
		t.Fatal("the error should carry the rule that refused it")
	}
	if de.Rule.Pattern != "/gp/offer-listing/" {
		t.Errorf("rule = %q, want the pattern that matched", de.Rule)
	}
	if !strings.Contains(err.Error(), "--no-robots") {
		t.Error("the message should say what the override is")
	}

	if _, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0); err != nil {
		t.Fatalf("an allowed page should still be fetched: %v", err)
	}
}

// TestNoRobotsOverrides asserts the flag works and is loud about it.
func TestNoRobotsOverrides(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nDisallow: /gp/offer-listing/\n")
	c := newTestClient(t, srv.URL, true)
	var notes strings.Builder
	c.SetNotes(&notes)

	if _, err := c.Get(context.Background(), srv.URL+"/gp/offer-listing/B075F5X8BR", 0); err != nil {
		t.Fatalf("--no-robots should read it anyway: %v", err)
	}
	out := notes.String()
	for _, want := range []string{"--no-robots is set", "Disallow: /gp/offer-listing/", "stop now"} {
		if !strings.Contains(out, want) {
			t.Errorf("the notes are missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "--no-robots is set"); n != 1 {
		t.Errorf("the banner printed %d times, want once per run", n)
	}
}

// TestNoRobotsRaisesTheFloor: reading what a site asked you not to read is a
// thing to do slowly.
func TestNoRobotsRaisesTheFloor(t *testing.T) {
	if got := ClampDelayWith(time.Second, true); got != MinDelayNoRobots {
		t.Errorf("floor under --no-robots = %s, want %s", got, MinDelayNoRobots)
	}
	if got := ClampDelayWith(30*time.Second, true); got != 30*time.Second {
		t.Errorf("--rate can still raise the floor, got %s", got)
	}
	if got := ClampDelayWith(time.Second, false); got != time.Second {
		t.Errorf("the normal floor is unchanged, got %s", got)
	}
}

// TestRobotsCachedOnDisk asserts the file is fetched once and reused, so the
// gate costs one request per host per day and not one per page.
func TestRobotsCachedOnDisk(t *testing.T) {
	var robotsHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			robotsHits++
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /nope\n"))
			return
		}
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		c := NewClient(Config{Delay: MinDelay, Timeout: 5 * time.Second, CacheDir: dir, NoCache: true})
		c.SetBaseURL(srv.URL)
		forceRobotsCheck(c)
		if _, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0); err != nil {
			t.Fatal(err)
		}
	}
	if robotsHits != 1 {
		t.Errorf("robots.txt fetched %d times across two processes, want 1 within the TTL", robotsHits)
	}
}

// TestOpsClassifiesEverySurface asserts the registry recognises the URLs amz's
// own builders produce. An unclassified fetch is a fetch nobody has measured.
func TestOpsClassifiesEverySurface(t *testing.T) {
	c := NewClient(Config{Marketplace: "us"})
	cases := map[string]string{
		c.ProductURL("B075F5X8BR"):                         "product",
		c.SearchURL("keyboard", SearchQuery{}, 1):          "search",
		c.CategoryURL("172282"):                            "category",
		c.ChartURL(ChartBestsellers, "electronics", "", 1): "bestsellers",
		c.DealsURL():                                "deals",
		c.OffersURL("B075F5X8BR"):                   "offers",
		c.QAURL("B075F5X8BR"):                       "qa",
		c.SellerURL("A1234567890"):                  "seller",
		c.ReviewURL("B075F5X8BR", ReviewQuery{}, 1): "reviews-full",
	}
	for u, want := range cases {
		op := OpFor(u)
		if op == nil {
			t.Errorf("%s: no surface claims this URL", u)
			continue
		}
		if op.Name != want {
			t.Errorf("%s: classified as %q, want %q", u, op.Name, want)
		}
	}
	// The registry's own tiebreak: an author store is not a brand store.
	if op := OpFor("https://www.amazon.com/stores/author/B000AP9A6K"); op == nil || op.Name != "author" {
		t.Errorf("author page classified as %v, want author", op)
	}
}

// TestRegistryExpectationsMatchLiveFile is the check that keeps the compiled
// robots column honest. It is the disagreement note, run as a test.
func TestRegistryExpectationsMatchLiveFile(t *testing.T) {
	r := liveRobots(t)
	samples := map[string]string{
		"product":      "https://www.amazon.com/dp/B075F5X8BR",
		"search":       "https://www.amazon.com/s?k=keyboard",
		"bestsellers":  "https://www.amazon.com/gp/bestsellers/electronics/",
		"deals":        "https://www.amazon.com/deals",
		"seller":       "https://www.amazon.com/sp?seller=A1234567890",
		"offers":       "https://www.amazon.com/gp/offer-listing/B075F5X8BR",
		"reviews-full": "https://www.amazon.com/product-reviews/B075F5X8BR",
		"qa":           "https://www.amazon.com/ask/questions/asin/B075F5X8BR",
	}
	for name, u := range samples {
		op := OpNamed(name)
		if op == nil {
			t.Fatalf("no surface named %q", name)
		}
		allowed, rule := r.Test(u)
		want := op.Robots == RobotsAllowed
		if allowed != want {
			t.Errorf("surface %s (%s): registry says %s as of %s, the file says %s (%q). Fix the registry, not the file.",
				op.ID, name, op.Robots, op.Since, allowStr(allowed), rule)
		}
	}
}

func robotsServer(t *testing.T, robots string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte(robots))
			return
		}
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestClient points a client at a fixture server and tells it to treat that
// server as if it were amazon.com, so the gate is exercised rather than skipped.
func newTestClient(t *testing.T, base string, noRobots bool) *Client {
	t.Helper()
	c := NewClient(Config{
		Delay:    MinDelay,
		Timeout:  5 * time.Second,
		CacheDir: t.TempDir(),
		NoCache:  true,
		NoRobots: noRobots,
	})
	c.SetBaseURL(base)
	forceRobotsCheck(c)
	return c
}
