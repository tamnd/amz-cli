package amz

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Every record has to be able to answer "which responses went into this", and
// for most of v0.2 only the product could. This walks one record of each shape
// the fetchers produce and holds all of them to the same standard.
func TestEveryRecordNamesItsSources(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	ctx := context.Background()

	first := func(fetch func(emit func(Envelope) error) error) Envelope {
		var got Envelope
		var seen bool
		err := fetch(func(env Envelope) error {
			if !seen {
				got, seen = env, true
			}
			return io.EOF // one row is enough, and stopping keeps the fixture cheap
		})
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		return got
	}

	cases := map[string]Envelope{}

	p, err := c.FetchProduct(ctx, "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	cases["product"] = p.Envelope

	s, err := c.FetchSeller(ctx, "A9RATEDSELLER1")
	if err != nil {
		t.Fatal(err)
	}
	cases["seller"] = s.Envelope

	b, err := c.FetchBrand(ctx, "anker")
	if err != nil {
		t.Fatal(err)
	}
	cases["brand"] = b.Envelope

	a, err := c.FetchAuthor(ctx, "brandon-sanderson")
	if err != nil {
		t.Fatal(err)
	}
	cases["author"] = a.Envelope

	cat, err := c.FetchCategory(ctx, "172282")
	if err != nil {
		t.Fatal(err)
	}
	cases["category"] = cat.Envelope

	cases["chart entry"] = first(func(emit func(Envelope) error) error {
		return c.FetchChart(ctx, ChartBestsellers, "electronics", "", 1, func(e BestsellerEntry) error {
			return emit(e.Envelope)
		})
	})
	cases["search card"] = first(func(emit func(Envelope) error) error {
		return c.Search(ctx, "keyboard", SearchQuery{Limit: 1}, func(card Card) error {
			return emit(card.Envelope)
		})
	})
	cases["deal"] = first(func(emit func(Envelope) error) error {
		return c.FetchDeals(ctx, 1, func(d Deal) error { return emit(d.Envelope) })
	})
	cases["related card"] = first(func(emit func(Envelope) error) error {
		return c.FetchRelated(ctx, "B084DWG2VQ", 1, func(card Card) error { return emit(card.Envelope) })
	})

	for name, env := range cases {
		if len(env.Sources) == 0 {
			t.Errorf("%s: no sources, so nothing in the record says which page it was read from", name)
			continue
		}
		src := env.Sources[0]
		if src.URL == "" {
			t.Errorf("%s: a source without a URL cannot be refetched or checked", name)
		}
		if src.Bytes == 0 {
			t.Errorf("%s: a source that records no size cannot explain a crawl's bandwidth", name)
		}
		if src.RetrievedAt.IsZero() {
			t.Errorf("%s: a source with no time cannot be told apart from a cached read of last week", name)
		}
		// The surface id comes from the Ops registry rather than from a string at
		// the call site, so an unclassified one means the registry does not know
		// about a page amz is fetching.
		if src.Surface == "" {
			t.Errorf("%s: %s belongs to no surface in the registry", name, src.URL)
		}
		if !containsString(env.Surfaces, src.Surface) {
			t.Errorf("%s: surfaces %v does not include %s", name, env.Surfaces, src.Surface)
		}
		if env.RetrievedAt.IsZero() {
			t.Errorf("%s: the envelope takes its time from the newest source and has none", name)
		}
	}
}

// A record built from a page that was cached yesterday was not retrieved today,
// and a source that said otherwise would date a stale price to the moment of the
// read. This is the one fact about a cached read that a consumer cannot recover
// any other way.
func TestCachedSourceIsDatedWhenThePageWasFetched(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Delay = 0
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	c.SetBaseURL(srv.URL)
	u := c.ProductURL("B075F5X8BR")

	if _, _, err := c.GetSource(context.Background(), u, time.Hour); err != nil {
		t.Fatal(err)
	}

	// Age the cached copy. Nothing else in the test can move a modtime, so a
	// source that comes back stamped with this is reading the file and not the
	// clock.
	yesterday := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(cachePathFor(c, u), yesterday, yesterday); err != nil {
		t.Fatal(err)
	}

	_, src, err := c.GetSource(context.Background(), u, 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("the second read went to the network %d times, so it was not a cache hit", hits-1)
	}
	if !src.Cached {
		t.Error("a source that does not say it came off disk lets a stale record pass as a fresh one")
	}
	if !src.RetrievedAt.Equal(yesterday.UTC()) {
		t.Errorf("cached source is dated %s, want the write time %s", src.RetrievedAt, yesterday.UTC())
	}
	if src.Bytes == 0 {
		t.Error("a cached source still knows how big the page was")
	}
}

// A read taken under --no-robots is the single thing about a record that a
// downstream consumer most needs to be able to see without being told, so it
// goes in the record rather than only in the terminal the crawl ran in.
func TestRobotsNoteRecordsTheOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /dp/\n"))
			return
		}
		b, err := os.ReadFile("testdata/product.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Delay = 0
	cfg.CacheDir = t.TempDir()
	cfg.NoRobots = true
	c := NewClient(cfg)
	c.SetBaseURL(srv.URL)
	c.SetNotes(io.Discard)
	forceRobotsCheck(c)

	p, err := c.FetchProduct(context.Background(), "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	note := p.Envelope.Robots
	if note == nil {
		t.Fatal("a record fetched against a Disallow under an override has to carry the note")
	}
	if !note.Override {
		t.Error("override is false on a record fetched with --no-robots")
	}
	if note.Allowed {
		t.Error("allowed is true for a path robots.txt disallows")
	}
	if !strings.Contains(note.Rule, "/dp/") {
		t.Errorf("the note names the rule that was broken, got %q", note.Rule)
	}
}

// The ordinary case carries no note. A field that is present on every record and
// says nothing on almost all of them is a field people learn to skip, and the
// two cases above are the ones that have to be noticed.
func TestRobotsNoteIsAbsentOnAnUnremarkableRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /gp/profile\n"))
			return
		}
		b, err := os.ReadFile("testdata/product.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Delay = 0
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	c.SetBaseURL(srv.URL)
	forceRobotsCheck(c)

	p, err := c.FetchProduct(context.Background(), "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Envelope.Robots != nil {
		t.Errorf("a read no rule mentions carries no note, got %+v", p.Envelope.Robots)
	}
}
