package amz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The search milestone's tests.
//
// Two things are being defended here. The first is that a refinement amz sends
// is one the page offered and one the page came back saying it applied, because
// Amazon answers an rh term it does not recognise with an unfiltered 200 and a
// full grid. The second is that the walk stops, on all four of the ways Amazon
// signals the end, and says which one it hit.

// capturePage parses one golden capture as a search page.
func capturePage(t *testing.T, name string, page int) SearchPage {
	t.Helper()
	body, err := readCapture(filepath.Join(capturesDir, name+".html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := readSidecar(filepath.Join(capturesDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(DefaultConfig())
	sp, err := c.parseSearchPage("mechanical keyboard", meta.URL, page, body)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// The refinement vocabulary is read, not remembered. Six codes are global enough
// to name in Go and every other code amz sends came off the page it is about to
// filter, so this asserts the table stays small. It is not a style rule: the one
// star id v0.2.1 did hardcode was wrong, and the search it produced was silently
// unfiltered.
func TestOnlySixGlobalRefinementCodes(t *testing.T) {
	if len(globalRefinements) != 6 {
		t.Errorf("globalRefinements has %d entries, want 6: a code that varies by query belongs on the page and not in the binary", len(globalRefinements))
	}
	for _, want := range []string{"p_72", "p_123", "p_6", "p_36", "p_85", "p_76"} {
		if _, ok := globalRefinements[want]; !ok {
			t.Errorf("%s is not in globalRefinements", want)
		}
	}
}

// The sidebar of the first-page capture is where every id in this package comes
// from, so a change in how it is read shows up here before it shows up in a
// filtered search that returns everything.
func TestRefinementsReadOffThePage(t *testing.T) {
	sp := capturePage(t, "search_page1", 1)
	if len(sp.Refinements) < 20 {
		t.Fatalf("the first page capture carries %d refinement groups, want at least 20", len(sp.Refinements))
	}
	byCode := map[string]RefineGroup{}
	values := 0
	for _, g := range sp.Refinements {
		byCode[g.Code] = g
		values += len(g.Values)
		if g.Label == "" {
			t.Errorf("group %s has no label, so nothing can be printed for it", g.Code)
		}
		for _, v := range g.Values {
			if v.ID == "" || v.Label == "" {
				t.Errorf("group %s has a value with id %q and label %q", g.Code, v.ID, v.Label)
			}
		}
	}
	if values < 150 {
		t.Errorf("the capture carries %d refinement values, want at least 150", values)
	}

	// Four stars and up. v0.2.1 sent 1248882011 for this and the page offers
	// 1248879011, which is the whole reason the ids are read rather than typed.
	stars, ok := byCode["p_72"]
	if !ok {
		t.Fatal("the capture offers no p_72 group")
	}
	var four string
	for _, v := range stars.Values {
		if strings.HasPrefix(v.Label, "4 Stars") {
			four = v.ID
		}
	}
	if four != "1248879011" {
		t.Errorf("4 stars and up is %q on this capture, and v0.2.1 sent 1248882011", four)
	}

	// A per-query code, which is the case the whole design exists for. There is
	// no meaning to this group outside a keyboard search.
	found := false
	for code := range byCode {
		if strings.HasPrefix(code, "p_n_") && globalRefinements[code] == "" {
			found = true
		}
	}
	if !found {
		t.Error("the capture offers no query-specific group, so this test is not testing what it says")
	}
}

// What came back filtered has to say so, because a page that ignored the filter
// looks exactly like a page that honoured it apart from this.
func TestAppliedRefinementsReadFromTheFilteredPage(t *testing.T) {
	sp := capturePage(t, "search_filtered", 1)
	got := map[string]string{}
	for _, r := range sp.Applied {
		got[r.Group] = strings.Join(r.Values, ",")
	}
	// The capture was taken with i=electronics and rh=n:172282,p_72:1248879011.
	if got["p_72"] != "1248879011" {
		t.Errorf("the star filter came back as %q", got["p_72"])
	}
	// The department is the awkward one. Amazon has nothing to link a node to
	// except itself, so the applied node is bold text with no anchor and no
	// remove label, and reading only the aria-label reports it as unapplied.
	if got["n"] != "172282" {
		t.Errorf("the applied department came back as %q, and the capture was fetched with n:172282", got["n"])
	}

	// The unfiltered capture of the same query must not claim anything is
	// applied, or the check above would pass on every page.
	if plain := capturePage(t, "search_page1", 1); len(plain.Applied) != 0 {
		t.Errorf("the unfiltered capture reports %d applied refinements", len(plain.Applied))
	}
}

// The scope of a code is derived from its shape and never from a list, because
// the list would have to grow every time Amazon invents a facet.
func TestRefineScope(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"p_123", ScopeGlobal},
		{"p_72", ScopeGlobal},
		{"n", ScopeNode},
		{"p_n_feature_thirteen_browse-bin", ScopeFeature},
		{"p_n_g-1003532609111", ScopeAttribute},
		{"p_n_condition-type", ScopeNamed},
	} {
		if got := RefineScope(tc.code); got != tc.want {
			t.Errorf("RefineScope(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

// A refinement the query does not offer is refused before a request is made
// with it, because the request would succeed. Amazon drops the unknown term and
// serves the unfiltered result set with a 200, so "no error" is not evidence.
func TestUnofferedRefinementIsRefused(t *testing.T) {
	c, done := fixtureServer(t)
	defer done()

	_, err := c.ResolveRefinements(context.Background(), "usb c cable",
		SearchQuery{Brand: "a brand that does not exist"})
	if err == nil {
		t.Fatal("resolving an unoffered brand has to fail, because sending it would return an unfiltered page that looks filtered")
	}
	if !errors.Is(err, ErrRefinementUnoffered) {
		t.Errorf("error is %v, want ErrRefinementUnoffered", err)
	}
	// The message has to carry what the query does offer. A refusal that does
	// not say what would have worked costs the caller a second command.
	if !strings.Contains(err.Error(), "brand") {
		t.Errorf("the refusal does not name the filter it was about: %v", err)
	}
}

// The URI is the identity of a search and two spellings of the same search are
// one node. Ordering, case and Amazon's own tracking parameters are all noise.
func TestSearchURINormalisation(t *testing.T) {
	c := NewClient(DefaultConfig())
	a, err := c.SearchURI("Mechanical  Keyboard", SearchQuery{
		Refine: []Refinement{
			{Group: "p_123", Values: []string{"213704", "111070"}},
			{Group: "p_72", Values: []string{"1248879011"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.SearchURI("mechanical keyboard", SearchQuery{
		Refine: []Refinement{
			{Group: "p_72", Values: []string{"1248879011"}},
			{Group: "p_123", Values: []string{"111070", "213704"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("the same search asked two ways is two nodes:\n  %s\n  %s", a, b)
	}

	// A refinement changes the identity. A filtered search and an unfiltered one
	// are different questions with different answers and must not collide.
	plain, err := c.SearchURI("mechanical keyboard", SearchQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if plain == a {
		t.Error("a refined search has the same uri as the unrefined one, so a store would overwrite one with the other")
	}

	// The page number is not part of the identity. Page three of a search is the
	// same search, and neither is anything Amazon hangs off its own links: qid
	// is a timestamp and ds is a signed token, so a URL keeping either would
	// give one search a new identity on every fetch.
	plainURL := NormalizeSearchURL("https://www.amazon.com/s?k=kindle")
	for _, raw := range []string{
		"https://www.amazon.com/s?k=kindle&page=3",
		"https://www.amazon.com/s?k=kindle&qid=1755400000&ref=sr_pg_3",
		"https://www.amazon.com/s?ref=nb_sb_noss&k=kindle&crid=ABC&sprefix=kin",
		"https://www.amazon.com/s?k=kindle&rnid=2941120011&ds=v1%3Asigned&dc&xpid=5f-5XCozrYRSz",
		"https://www.amazon.com/s?k=Kindle&_encoding=UTF8&sr=8-1&th=1&psc=1",
	} {
		if got := NormalizeSearchURL(raw); got != plainURL {
			t.Errorf("NormalizeSearchURL(%q) = %q, which keeps something that is not part of the query", raw, got)
		}
	}

	// The five that are part of the query survive, or the normalisation would
	// merge a filtered search into the plain one.
	for _, raw := range []string{
		"https://www.amazon.com/s?k=kindle&i=electronics",
		"https://www.amazon.com/s?k=kindle&s=price-asc-rank",
		"https://www.amazon.com/s?k=kindle&low-price=50",
		"https://www.amazon.com/s?k=kindle&high-price=150",
		"https://www.amazon.com/s?k=kindle&rh=p_72%3A1248879011",
	} {
		if got := NormalizeSearchURL(raw); got == plainURL {
			t.Errorf("NormalizeSearchURL(%q) dropped a parameter that changes the answer", raw)
		}
	}

	// Amazon's own department links write one group twice. Merging them is what
	// keeps rh=n:172282,n:281407 from being a second identity for a search that
	// already has one.
	a2 := NormalizeSearchURL("https://www.amazon.com/s?k=kindle&rh=n%3A172282%2Cn%3A281407")
	b2 := NormalizeSearchURL("https://www.amazon.com/s?k=kindle&rh=n%3A281407%7C172282")
	if a2 != b2 {
		t.Errorf("a repeated rh group is a second identity:\n  %s\n  %s", a2, b2)
	}
}

// The estimate is Amazon's claim about how many things match, not an offer to
// serve them. Nothing may divide it by a page size to decide how far to walk,
// because that is the arithmetic that produces a run of 2,500 requests against
// a corpus that stops handing over results after 20 pages.
func TestEstimateNeverUsedInArithmetic(t *testing.T) {
	sp := capturePage(t, "search_page1", 1)
	if sp.Total < 1000 {
		t.Fatalf("the capture states a total of %d, so this test cannot show the gap", sp.Total)
	}
	c, done := fixtureServer(t)
	defer done()

	sum, err := c.SearchWalk(context.Background(), "usb c cable", SearchQuery{}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pages > searchPageCap {
		t.Errorf("the walk read %d pages, and the ceiling is %d", sum.Pages, searchPageCap)
	}
}

// Twenty is the last page that exists. Asking for 21 gets six filler cards and
// a range that runs backwards, so a walk that keeps going produces rows that
// look like results and are not.
func TestNeverRequestsPastTwenty(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		if p == "" {
			p = "1"
		}
		pages = append(pages, p)
		w.Header().Set("Content-Type", "text/html")
		// Every page claims 176 pages and a full grid, which is what a broad
		// query does. Only the cap stops this.
		_, _ = fmt.Fprint(w, endlessSearchHTML(p))
	}))
	defer srv.Close()

	c := fixtureClient(t, srv.URL)
	sum, err := c.SearchWalk(context.Background(), "kindle", SearchQuery{}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != searchPageCap {
		t.Errorf("the walk made %d requests against a page cap of %d: %v", len(pages), searchPageCap, pages)
	}
	if !sum.Ceiling.Hit {
		t.Error("stopping at the cap is Amazon's limit and not the caller's, so the summary has to say the ceiling was hit")
	}
	if sum.Ceiling.Advertised != 176 {
		t.Errorf("advertised last page = %d, want the 176 the strip claims", sum.Ceiling.Advertised)
	}
	if sum.Ceiling.Reason == "" {
		t.Error("a ceiling with no reason on it tells a reader nothing about which of the four stops it was")
	}
}

// --max-pages lowers the ceiling and cannot raise it, and the difference between
// the two shows up in the summary. A run cut short by the caller is not a run
// that ran out of results.
func TestMaxPagesLowersAndNeverRaisesTheCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		if p == "" {
			p = "1"
		}
		_, _ = fmt.Fprint(w, endlessSearchHTML(p))
	}))
	defer srv.Close()
	c := fixtureClient(t, srv.URL)

	low, err := c.SearchWalk(context.Background(), "kindle", SearchQuery{MaxPages: 3}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if low.Pages != 3 {
		t.Errorf("--max-pages 3 read %d pages", low.Pages)
	}
	if low.Ceiling.Hit {
		t.Error("the caller's own limit is not Amazon's ceiling, and reporting it as one would hide the difference")
	}

	high, err := c.SearchWalk(context.Background(), "kindle", SearchQuery{MaxPages: 500}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if high.Pages != searchPageCap {
		t.Errorf("--max-pages 500 read %d pages, and %d is all there are", high.Pages, searchPageCap)
	}
	if !high.Ceiling.Hit {
		t.Error("a walk stopped by the page cap has hit the ceiling")
	}
}

// The strip states the last page and the walk believes it. A query with three
// pages of results does not get twenty requests spent on it.
func TestStopsAtAdvertisedLastPage(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		p := r.URL.Query().Get("page")
		if p == "" {
			p = "1"
		}
		_, _ = fmt.Fprint(w, shortSearchHTML(p, 3))
	}))
	defer srv.Close()
	c := fixtureClient(t, srv.URL)

	sum, err := c.SearchWalk(context.Background(), "kindle", SearchQuery{}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || sum.Pages != 3 {
		t.Errorf("a three page result set took %d requests over %d pages", n, sum.Pages)
	}
	if sum.Ceiling.Hit {
		t.Error("reaching the end of the results is not hitting the ceiling")
	}
}

// The range end freezing is Amazon serving the last page again under a new page
// number. A walk that does not notice emits the same sixteen cards forever.
func TestStopsWhenRangeEndFreezes(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		// Every page reports the same range, which is what a repeated last page
		// looks like from the outside.
		_, _ = fmt.Fprint(w, frozenSearchHTML())
	}))
	defer srv.Close()
	c := fixtureClient(t, srv.URL)

	sum, err := c.SearchWalk(context.Background(), "kindle", SearchQuery{}, func(SearchPage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n > 2 {
		t.Errorf("the same page was fetched %d times before the walk noticed", n)
	}
	if !sum.Ceiling.Hit || !strings.Contains(sum.Ceiling.Reason, "range") {
		t.Errorf("ceiling = %+v, want a reason naming the frozen range", sum.Ceiling)
	}
}

// "321-306 of over 306" is the page saying it has gone past its own end. It is
// terminal and the six cards on it are not results.
func TestInvertedRangeIsTerminal(t *testing.T) {
	sp := capturePage(t, "search_deep", 21)
	if !sp.Inverted() {
		t.Fatalf("the past-the-ceiling capture reports %d-%d, which does not run backwards", sp.From, sp.To)
	}
	if sp.To != searchResultCeiling {
		t.Errorf("the capture's range ends at %d and the measured ceiling is %d", sp.To, searchResultCeiling)
	}
	// The pagination strip is gone on that page, so there is nothing to follow
	// even if the range were believed.
	if sp.NextPage != 0 {
		t.Errorf("the past-the-ceiling page offers a next page of %d", sp.NextPage)
	}
}

// One card carries its ASIN in several attributes on nested elements. Reading
// them all and keeping each hit is how a sixteen result page becomes forty.
func TestNoDuplicateResultsFromNestedAsinAttrs(t *testing.T) {
	for _, name := range []string{"search_page1", "search_filtered", "search_sorted"} {
		sp := capturePage(t, name, 1)
		seen := map[string]int{}
		for _, card := range sp.Cards {
			if card.ASIN == "" {
				t.Errorf("%s: a card with no asin, which is a card that cannot be joined to anything", name)
				continue
			}
			seen[card.ASIN]++
		}
		for asin, n := range seen {
			if n > 1 {
				t.Errorf("%s: %s appears %d times on one page", name, asin, n)
			}
		}
		if len(sp.Cards) == 0 {
			t.Errorf("%s: no cards", name)
		}
	}
}

// Position is the organic rank. An advertisement occupies a slot in the grid and
// no place in the ranking, so numbering it would shift every result below it.
func TestSponsoredCardsExcludedFromPositions(t *testing.T) {
	ads := 0
	// The filtered and sorted captures carry the advertising and the first page
	// capture does not, so all three are read. A test that only looked at the
	// clean page would pass with the ad handling deleted.
	for _, name := range []string{"search_page1", "search_filtered", "search_sorted"} {
		sp := capturePage(t, name, 1)
		last := 0
		for _, card := range sp.Cards {
			if card.Sponsored {
				ads++
				if card.Position != 0 {
					t.Errorf("%s: sponsored %s carries position %d", name, card.ASIN, card.Position)
				}
				continue
			}
			if card.Position != last+1 {
				t.Errorf("%s: organic %s is at position %d, and the one before it was %d", name, card.ASIN, card.Position, last)
			}
			last = card.Position
		}
		if last == 0 {
			t.Errorf("%s: no organic card carried a position", name)
		}
		// The count is on the page record too, because a caller filtering the ads
		// out of the rows needs to know how many were there.
		if sp.SponsoredCount != countSponsored(sp.Cards) {
			t.Errorf("%s: sponsored_count = %d, cards say %d", name, sp.SponsoredCount, countSponsored(sp.Cards))
		}
	}
	if ads == 0 {
		t.Error("no capture carried an advertisement, so nothing here tested the case it is named for")
	}
}

// A page that says it is showing 1-16 hands over 22 cards, because the ads are
// in the grid and outside the count. Neither number is wrong and a walk that
// treats one as the other loses results or invents them.
func TestCardCountMayExceedRangeWidth(t *testing.T) {
	wider := 0
	for _, name := range []string{"search_page1", "search_filtered", "search_sorted"} {
		sp := capturePage(t, name, 1)
		width := sp.To - sp.From + 1
		if len(sp.Cards) > width {
			wider++
		}
		// The two numbers reconcile exactly, and that is the claim. The extra
		// cards are the advertising, so once the ads are taken off the grid what
		// is left is precisely the range the page printed. A mismatch means one
		// of the two readings is wrong and there is no way to tell which from
		// the record alone.
		if organic := len(sp.Cards) - sp.SponsoredCount; organic != width {
			t.Errorf("%s: %d cards less %d sponsored is %d, and the page printed %d-%d",
				name, len(sp.Cards), sp.SponsoredCount, organic, sp.From, sp.To)
		}
		if sp.PageSize != width {
			t.Errorf("%s: page_size %d against a printed range of %d", name, sp.PageSize, width)
		}
	}
	if wider == 0 {
		t.Error("no capture carries more cards than its range, so this test is not testing what it says")
	}
}

// A partition is priced before it runs. --dry-run on a broad query has to state
// the request count, because the difference between 8 cells and 164 is the
// difference between a minute and an afternoon.
func TestPartitionPlanIsPricedBeforeItRuns(t *testing.T) {
	sp := capturePage(t, "search_page1", 1)
	plan, err := planFrom("mechanical keyboard", sp.Refinements, "", 3_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Cells < 2 {
		t.Fatalf("a partition into %d cells is not a partition", plan.Cells)
	}
	if plan.WorstCase != plan.Cells*searchPageCap {
		t.Errorf("worst case = %d, want cells times the page cap", plan.WorstCase)
	}
	if plan.WorstCaseTime <= 0 {
		t.Error("a plan with no wall clock on it does not let anyone decide against running it")
	}
	if len(plan.Alternatives) == 0 {
		t.Error("a caller told the default is 164 searches needs to be shown the smaller groups")
	}
	// Sorted smallest first, so the cheapest alternative is the one read first.
	for i := 1; i < len(plan.Alternatives); i++ {
		if plan.Alternatives[i-1].Values > plan.Alternatives[i].Values {
			t.Errorf("alternatives are not smallest first at %d", i)
			break
		}
	}

	// The browse node group nests, so splitting on it would run a category and
	// its children as separate searches and count everything in both.
	if _, err := planFrom("mechanical keyboard", sp.Refinements, "n", 0); err == nil {
		t.Error("partitioning on the node group has to be refused, because its values overlap")
	}
}

// fixtureClient points a client at a hand written server. The captures cover
// what a real page looks like and these servers cover the shapes a walk has to
// survive, which no single capture can: twenty identical pages, a result set
// that ends at three, and the last page served over and over.
func fixtureClient(t *testing.T, base string) *Client {
	t.Helper()
	cfg := DefaultConfig()
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	c.SetBaseURL(base)
	// The politeness floor is deliberately not settable through Config, because
	// nobody outside this package should be able to turn it off. These tests walk
	// twenty pages against a local httptest server, which is not amazon.com and
	// has no pace to keep, so the field is cleared here and nowhere else.
	c.delay = 0
	return c
}

// endlessSearchHTML is a page that always claims 176 more pages, which is what
// a broad query does right up to page 20.
func endlessSearchHTML(page string) string {
	n := atoiSafe(page)
	from, to := (n-1)*16+1, n*16
	return searchShell(from, to, 2816, n, 176)
}

// shortSearchHTML is a result set that genuinely ends.
func shortSearchHTML(page string, last int) string {
	n := atoiSafe(page)
	from, to := (n-1)*16+1, n*16
	return searchShell(from, to, 40, n, last)
}

// frozenSearchHTML is the last page served again under a new number.
func frozenSearchHTML() string { return searchShell(17, 32, 40, 2, 20) }

func searchShell(from, to, total, page, last int) string {
	var b strings.Builder
	b.WriteString(`<html><body><span data-component-type="s-result-info-bar">`)
	fmt.Fprintf(&b, `<div><h2><span>%d-%d of over %d results</span></h2></div></span>`, from, to, total)
	// The grid and the strip both hang off s-search-results, which is the region
	// the page fields are anchored on. Amazon ships an s-pagination region too
	// and leaves it empty, which is why the strip is in here and not in that.
	b.WriteString(`<div data-component-type="s-search-results">`)
	b.WriteString(`<div class="s-main-slot s-result-list">`)
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, `<div data-component-type="s-search-result" data-asin="B%09d" data-index="%d">`, i, i)
		fmt.Fprintf(&b, `<h2 aria-label="Item %d"><span>Item %d</span></h2>`, i, i)
		b.WriteString(`<a class="a-link-normal" href="/dp/B000000001"></a></div>`)
	}
	// The strip is written the way Amazon writes it: the current page is a
	// selected span, the last reachable page is a disabled span holding a bare
	// number, and next is an anchor whose aria-label names the page it goes to.
	// A shape that only this file understands would test only this file.
	b.WriteString(`</div><div class="s-pagination-container"><span class="s-pagination-strip">`)
	if page > 1 {
		fmt.Fprintf(&b, `<a class="s-pagination-item s-pagination-previous" aria-label="Go to previous page, page %d" href="/s?k=kindle&page=%d">Previous</a>`, page-1, page-1)
	}
	fmt.Fprintf(&b, `<span class="s-pagination-item s-pagination-selected" aria-label="Page %d">%d</span>`, page, page)
	if page < last {
		fmt.Fprintf(&b, `<a class="s-pagination-item s-pagination-button" aria-label="Go to page %d" href="/s?k=kindle&page=%d">%d</a>`, page+1, page+1, page+1)
		fmt.Fprintf(&b, `<span class="s-pagination-item s-pagination-ellipsis" aria-disabled="true">...</span>`)
		fmt.Fprintf(&b, `<span class="s-pagination-item s-pagination-disabled" aria-disabled="true">%d</span>`, last)
		fmt.Fprintf(&b, `<a class="s-pagination-item s-pagination-next" aria-label="Go to next page, page %d" href="/s?k=kindle&page=%d">Next</a>`, page+1, page+1)
	} else {
		b.WriteString(`<span class="s-pagination-item s-pagination-next s-pagination-disabled" aria-disabled="true">Next</span>`)
	}
	b.WriteString(`</span></div></div></body></html>`)
	return b.String()
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 1
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 1
	}
	return n
}
