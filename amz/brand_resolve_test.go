package amz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The pages a brand resolve walks, written small on purpose.
//
// The shared fixture server answers every /stores/ path with the same
// storefront, which is the one thing this test cannot have: the whole behaviour
// under test starts with /stores/<name> answering 404, the way amazon.com
// answers it. So these are the four pages of the walk and nothing else.
const (
	brandStorePath = "/stores/Anker/page/D24FDA17-DECF-46BB-AF47-AF4647D2B1F8"

	// One organic card, in the markup a live search page uses.
	brandSearchPage = `<html><body>
<div class="s-main-slot s-result-list" data-component-type="s-search-results">
  <div data-component-type="s-search-result" data-asin="B0PROBE001" data-index="1">
    <span class="a-declarative" data-csa-c-type="item" data-csa-c-pos="1">
      <div class="puis-card-container s-card-container" data-cy="asin-faceout-container">
        <div data-cy="title-recipe">
          <a class="a-link-normal" href="/dp/B0PROBE001">
            <h2><span>Anker USB C to USB C Cable</span></h2>
          </a>
        </div>
      </div>
    </span>
  </div>
</div>
</body></html>`

	// A premium byline: an empty bylineInfo, then the brand as a logo anchor
	// with no text, then the same brand as a link that has some. Both anchors
	// carry the storefront, and the query on them is Amazon's note about which
	// product the reader came from.
	brandProductPage = `<html><head><title>Anker USB C to USB C Cable</title></head><body>
<div id="title_feature_div" class="celwidget" data-feature-name="title">
  <h1 id="title"><span id="productTitle">Anker USB C to USB C Cable, 60W</span></h1>
</div>
<div id="bylineInfo_feature_div" class="celwidget" data-feature-name="bylineInfo">
  <!-- No content: Premium non-fashion products -->
</div>
<div id="premiumBylineInfo_feature_div" class="celwidget" data-feature-name="premiumBylineInfo">
  <a id="brandLogoBylineLink" href="` + brandStorePath + `?lp_asin=B0PROBE001&amp;ref_=ast_bln">
    <img alt="Anker" src="https://m.media-amazon.com/images/I/61WTcHAAaWL.jpg" title="Visit the Anker Store"/>
  </a>
  <a id="visitStoreDesktopUrl" href="` + brandStorePath + `?lp_asin=B0PROBE001&amp;ref_=ast_bln">Visit the Anker Store</a>
</div>
</body></html>`

	brandStorePage = `<html><head>
<link rel="canonical" href="https://www.amazon.com` + brandStorePath + `"/>
<meta property="og:title" content="Anker"/>
<meta name="description" content="Shop Anker on Amazon"/>
</head><body></body></html>`
)

// brandResolveServer answers the four pages of a resolve, and 404s the short
// storefront path the way amazon.com does.
func brandResolveServer(t *testing.T) (*Client, *int) {
	t.Helper()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		p := r.URL.Path
		switch {
		case p == brandStorePath:
			_, _ = w.Write([]byte(brandStorePage))
		case strings.HasPrefix(p, "/stores/"):
			http.NotFound(w, r)
		case strings.HasPrefix(p, "/dp/"):
			_, _ = w.Write([]byte(brandProductPage))
		case p == "/s":
			_, _ = w.Write([]byte(brandSearchPage))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := DefaultConfig()
	cfg.Delay = 0
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	// The floor is a promise to amazon.com and not to a httptest server two
	// hops from this process. A resolve is four requests, and paying the 1s
	// clamp for each of them buys the suite twelve seconds and no coverage.
	c.delay = 0
	c.SetBaseURL(srv.URL)
	return c, &hits
}

// A bare brand name is the thing every user types, and until this it was the one
// thing that could not work: /stores/anker is a 404 and the uuid that makes the
// URL real is only ever stated by a product byline.
func TestBrandNameResolvesThroughAProductByline(t *testing.T) {
	c, _ := brandResolveServer(t)
	b, err := c.FetchBrand(context.Background(), "anker")
	if err != nil {
		t.Fatalf("brand anker: %v", err)
	}
	if b.Name != "Anker" {
		t.Errorf("name = %q, want Anker", b.Name)
	}
	if b.PageID != "D24FDA17-DECF-46BB-AF47-AF4647D2B1F8" {
		t.Errorf("page_id = %q", b.PageID)
	}
	// The lp_asin the byline hung on the link is which product the resolve
	// happened to open, and it has no business in the storefront's own URL.
	if strings.Contains(b.URL, "?") {
		t.Errorf("url kept the byline's query: %s", b.URL)
	}
	if !strings.HasSuffix(b.URL, brandStorePath) {
		t.Errorf("url = %q, want it to end in %s", b.URL, brandStorePath)
	}
}

// A slug that already names a page is one request, not four. The resolve is
// there for the case that cannot work otherwise, and a caller who did the
// lookup themselves should not pay for it again.
func TestBrandPageSlugSkipsTheResolve(t *testing.T) {
	c, hits := brandResolveServer(t)
	if _, err := c.FetchBrand(context.Background(), strings.TrimPrefix(brandStorePath, "/")); err != nil {
		t.Fatalf("brand by page slug: %v", err)
	}
	if *hits != 1 {
		t.Errorf("fetched %d pages, want 1", *hits)
	}
}

// A name that has no storefront gets an error that says what was tried, and it
// is a not found rather than something a script has to guess at.
func TestBrandWithNoStorefrontSaysWhatItLookedAt(t *testing.T) {
	c, _ := brandResolveServer(t)
	_, err := c.FetchBrand(context.Background(), "belkin")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	for _, want := range []string{"/stores/<name>/page/<uuid>", "belkin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// The byline on a premium page is a logo anchor with no text followed by a link
// that has some. Reading the first anchor and stopping is what made brand null
// on every premium listing on the site.
func TestPremiumBylineYieldsABrand(t *testing.T) {
	c, _ := brandResolveServer(t)
	p, err := c.FetchProduct(context.Background(), "B0PROBE001")
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	if p.Brand == nil {
		t.Fatal("brand is nil on a page whose premiumBylineInfo names one")
	}
	if p.Brand.Name != "Anker" {
		t.Errorf("brand name = %q, want Anker", p.Brand.Name)
	}
	if !strings.Contains(p.Brand.URL, brandStorePath) {
		t.Errorf("brand url = %q, want the storefront the byline links", p.Brand.URL)
	}
}

// foldBrand decides whether the byline is the brand that was asked for, so what
// it treats as the same word is worth stating rather than leaving to a reader of
// the loop.
func TestFoldBrandMatchesSpellingsAndNotNeighbours(t *testing.T) {
	same := [][2]string{
		{"anker", "Anker"},
		{"skull candy", "Skullcandy"},
		{"L'Oreal", "loreal"},
		{"AmazonBasics", "amazon basics"},
	}
	for _, p := range same {
		if foldBrand(p[0]) != foldBrand(p[1]) {
			t.Errorf("%q and %q fold apart", p[0], p[1])
		}
	}
	differ := [][2]string{
		{"sony", "sony pictures"},
		{"anker", "ankermake"},
	}
	for _, p := range differ {
		if foldBrand(p[0]) == foldBrand(p[1]) {
			t.Errorf("%q and %q fold together", p[0], p[1])
		}
	}
}
