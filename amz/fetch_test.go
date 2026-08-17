package amz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureServer serves testdata files based on the request path, mimicking the
// Amazon URL layout, so every fetcher can be exercised offline end-to-end.
func fixtureServer(t *testing.T) (*Client, func()) {
	t.Helper()
	route := func(p string) string {
		switch {
		case strings.HasPrefix(p, "/dp/"), strings.HasPrefix(p, "/gp/aw/d/"):
			// The mobile rendering is a different surface and the same family, so
			// the fixture is shared and the tests assert on which surface the
			// envelope says it read rather than on the body being different.
			return "product.html"
		case strings.HasPrefix(p, "/product-reviews/"):
			return "reviews.html"
		case strings.HasPrefix(p, "/ask/"):
			return "qa.html"
		case strings.HasPrefix(p, "/gp/offer-listing/"):
			return "offers.html"
		case strings.HasPrefix(p, "/gp/bestsellers"), strings.HasPrefix(p, "/gp/new-releases"),
			strings.HasPrefix(p, "/gp/movers-and-shakers"), strings.HasPrefix(p, "/gp/most-wished-for"),
			strings.HasPrefix(p, "/gp/most-gifted"):
			return "bestsellers.html"
		case strings.HasPrefix(p, "/stores/"):
			return "brand.html"
		case strings.HasPrefix(p, "/sp"):
			return "seller.html"
		case strings.HasPrefix(p, "/author/"):
			return "author.html"
		case strings.HasPrefix(p, "/deals"):
			return "deals.html"
		case strings.HasPrefix(p, "/b"):
			return "category.html"
		case p == "/s" || strings.HasPrefix(p, "/s?") || strings.HasPrefix(p, "/s/"):
			return "search.html"
		}
		return ""
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := route(r.URL.Path)
		if name == "" {
			http.NotFound(w, r)
			return
		}
		// Two seller fixtures, because there are two kinds of seller profile and
		// the difference between them is the point. An Amazon owned seller
		// publishes no feedback at all, a third party seller publishes four
		// rating windows, a star histogram and a page of written feedback, and
		// only having both keeps "not rated" apart from "rated zero".
		if name == "seller.html" && r.URL.Query().Get("seller") == "A9RATEDSELLER1" {
			name = "seller_rated.html"
		}
		// Pagination terminates: page 2+ of search/reviews returns an empty list.
		if pg := r.URL.Query().Get("page"); pg != "" && pg != "1" {
			_, _ = w.Write([]byte("<html><body></body></html>"))
			return
		}
		if pg := r.URL.Query().Get("pageNumber"); pg != "" && pg != "1" {
			_, _ = w.Write([]byte("<html><body></body></html>"))
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(b)
	}))
	cfg := DefaultConfig()
	cfg.Delay = 0
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	c.SetBaseURL(srv.URL)
	return c, srv.Close
}

func TestFetchProduct(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	p, err := c.FetchProduct(context.Background(), "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Echo Dot (4th Gen) | Smart speaker with Alexa | Charcoal" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Brand == nil || p.Brand.Name != "Amazon" {
		t.Errorf("brand = %+v", p.Brand)
	}
	if p.Offer == nil {
		t.Fatal("no buy box")
	}
	o := p.Offer
	if o.Price.Float() != 49.99 || o.Price.Cur() != "USD" {
		t.Errorf("price = %v %s", o.Price.Float(), o.Price.Cur())
	}
	if o.ListPrice.Float() != 59.99 {
		t.Errorf("list_price = %v", o.ListPrice.Float())
	}
	if o.Savings.Float() != 10.00 || o.SavingsPct == nil || *o.SavingsPct != 17 {
		t.Errorf("savings = %v pct = %v", o.Savings.Float(), o.SavingsPct)
	}
	if o.Coupon == nil || !strings.Contains(o.Coupon.Display, "$5.00") {
		t.Errorf("coupon = %+v", o.Coupon)
	}
	if o.Availability != "In Stock" {
		t.Errorf("availability = %q", o.Availability)
	}
	if o.InStock == nil || !*o.InStock {
		t.Errorf("in_stock = %v", o.InStock)
	}
	if o.ShipsFrom == nil || o.ShipsFrom.Name != "Amazon.com" {
		t.Errorf("ships_from = %+v", o.ShipsFrom)
	}
	if o.SoldBy == nil || o.SoldBy.ID != "ATVPDKIKX0DER" || o.SoldBy.Name != "Amazon.com" {
		t.Errorf("sold_by = %+v", o.SoldBy)
	}
	if o.SoldBy != nil && !o.SoldBy.Resolved {
		t.Error("a seller with a merchant id should be resolved")
	}
	if p.Rating == nil || *p.Rating != 4.7 || p.RatingsCount == nil || *p.RatingsCount != 284512 {
		t.Errorf("rating = %v count = %v", p.Rating, p.RatingsCount)
	}
	if p.Questions == nil || p.Questions.TotalCount == nil || *p.Questions.TotalCount != 1204 {
		t.Errorf("questions = %+v", p.Questions)
	}
	if p.Questions != nil && p.Questions.Complete {
		t.Error("no question was loaded, so the connection cannot be complete")
	}
	// The histogram is 73/15/6/2/4 from five stars down, and it is stored one
	// star first. A positional read of the rows would have reversed it and
	// nothing else in the record would have noticed.
	if p.Distribution == nil {
		t.Fatal("no rating histogram")
	}
	if got := p.Distribution.Percent; got != [5]int{4, 2, 6, 15, 73} {
		t.Errorf("distribution = %v", got)
	}
	if !p.Distribution.Derived {
		t.Error("counts are reconstructed from percentages and must say so")
	}
	if p.Distribution.Count == nil || p.Distribution.Count[4] != 207693 {
		t.Errorf("counts = %v", p.Distribution.Count)
	}
	if s := p.Distribution.Sum(); s != 100 {
		t.Errorf("percentages sum to %d", s)
	}
	// The mean the histogram implies and the rating Amazon prints agree to
	// within the rounding five integer percentages can hide.
	if m := p.Distribution.Mean(); m < 4.4 || m > 4.7 {
		t.Errorf("mean = %v, printed rating is 4.7", m)
	}
	if len(p.Bullets) != 2 {
		t.Errorf("bullets = %v", p.Bullets)
	}
	if p.Details["Colour"] != "Charcoal" {
		t.Errorf("details = %v", p.Details)
	}
	// The hero's many size variants collapse to one master; the alt rail adds a
	// second distinct photo; the tracking pixel is dropped.
	if len(p.ImageURLs) != 2 {
		t.Errorf("images = %v", p.ImageURLs)
	}
	for _, img := range p.ImageURLs {
		if strings.Contains(img, "._SL") || strings.Contains(img, "._SS") || strings.Contains(img, "._AC") {
			t.Errorf("image not canonicalized: %q", img)
		}
	}
	if len(p.Videos) != 1 || !strings.HasSuffix(p.Videos[0].URL, "echo-dot-demo.mp4") {
		t.Errorf("videos = %v", p.Videos)
	}
	if !strings.Contains(p.BoughtPastMonth, "bought in past month") {
		t.Errorf("bought_past_month = %q", p.BoughtPastMonth)
	}
	if len(p.Breadcrumb) != 3 || p.Breadcrumb[0].Name != "Electronics" || p.Breadcrumb[2].Name != "Speakers" {
		t.Errorf("breadcrumb = %+v", p.Breadcrumb)
	}
	if p.Variation == nil || len(p.Variation.Siblings) != 2 {
		t.Errorf("variation = %+v", p.Variation)
	}
	// Two ranks: #3 overall in Electronics and #1 in Smart Speakers. Only the
	// first carries a Top 100 link, which is what marks it as the department.
	if len(p.Ranks) != 2 || p.Ranks[0].Rank != 3 || !strings.HasPrefix(p.Ranks[0].Category, "Electronics") {
		t.Errorf("ranks = %+v", p.Ranks)
	}
	if len(p.Ranks) == 2 && (p.Ranks[1].Rank != 1 || p.Ranks[1].Category != "Smart Speakers") {
		t.Errorf("ranks = %+v", p.Ranks)
	}
	if len(p.Ranks) == 2 && (!p.Ranks[0].Overall || p.Ranks[1].Overall) {
		t.Errorf("overall flags = %v %v", p.Ranks[0].Overall, p.Ranks[1].Overall)
	}
}

// TestFlatProductMatchesTheOldShape pins the --flat projection against the
// figures the v0.2.1 record carried for this fixture. This is the compatibility
// promise the flag exists to make, and it is worth a test of its own because the
// nested record is what everything else is now asserted against, so a projection
// that quietly stopped filling a column would otherwise go unnoticed.
func TestFlatProductMatchesTheOldShape(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	p, err := c.FetchProduct(context.Background(), "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	f := p.Flat()
	if f.Brand != "Amazon" || f.Price != 49.99 || f.Currency != "USD" || f.ListPrice != 59.99 {
		t.Errorf("flat price block = %+v", f)
	}
	if f.Rating != 4.7 || f.RatingsCount != 284512 || f.AnsweredQs != 1204 {
		t.Errorf("flat review block = %v %v %v", f.Rating, f.RatingsCount, f.AnsweredQs)
	}
	if f.Availability != "In Stock" || !f.InStock {
		t.Errorf("flat availability = %q %v", f.Availability, f.InStock)
	}
	if f.SellerID != "ATVPDKIKX0DER" || f.SellerName != "Amazon.com" || f.ShipsFrom != "Amazon.com" {
		t.Errorf("flat seller block = %+v", f)
	}
	if strings.Join(f.CategoryPath, "/") != "Electronics/Smart Home/Speakers" {
		t.Errorf("flat category_path = %v", f.CategoryPath)
	}
	if len(f.VariantASINs) != 2 || len(f.BulletPoints) != 2 || len(f.Images) != 2 {
		t.Errorf("flat slices = %v %v %v", f.VariantASINs, f.BulletPoints, f.Images)
	}
	if f.Specs["Colour"] != "Charcoal" {
		t.Errorf("flat specs = %v", f.Specs)
	}
	if f.Rank != 3 || !strings.HasPrefix(f.RankCategory, "Electronics") || len(f.Ranks) != 2 {
		t.Errorf("flat ranks = %d %q %+v", f.Rank, f.RankCategory, f.Ranks)
	}
	// The envelope is not a projection. It travels whole, so a flat record can
	// still say which region every column came from.
	if len(f.Envelope.Via) != len(p.Envelope.Via) {
		t.Errorf("flat envelope has %d fields, nested has %d", len(f.Envelope.Via), len(p.Envelope.Via))
	}
}

func TestSearch(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var cards []Card
	err := c.Search(context.Background(), "kindle", SearchQuery{Limit: 10}, func(card Card) error {
		cards = append(cards, card)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("got %d cards", len(cards))
	}
	if cards[0].ASIN != "B0D14N2QZF" || cards[0].Price.Float() != 79.99 ||
		cards[0].ListPrice.Float() != 99.99 || cards[0].Rating == nil || *cards[0].Rating != 4.6 {
		t.Errorf("card0 = %+v", cards[0])
	}
	// The card prints "(1.7K)" and labels the same link "1,739 ratings". The
	// label wins, and reading the text instead would have given 1.
	if cards[0].RatingsCount == nil || *cards[0].RatingsCount != 1739 {
		t.Errorf("card0 ratings = %d", cards[0].RatingsCount)
	}
	if cards[0].BoughtPastMonth != "3K+ bought in past month" || cards[0].Delivery == "" {
		t.Errorf("card0 social/delivery = %q %q", cards[0].BoughtPastMonth, cards[0].Delivery)
	}
	if cards[0].Position != 1 || cards[1].Position != 2 {
		t.Errorf("positions = %d %d", cards[0].Position, cards[1].Position)
	}
	// Every value on the card came from a slot the search team named, so nothing
	// on it should be sitting on rung 4.
	if n := cards[0].Envelope.Levels["selector"]; n != 0 {
		t.Errorf("card0 has %d rung 4 fields: %v", n, cards[0].Envelope.Via)
	}
	if !cards[1].Sponsored || cards[1].Badge != "Best Seller" {
		t.Errorf("card1 = %+v", cards[1])
	}
	if cards[0].Sponsored {
		t.Errorf("card0 should not be sponsored")
	}
}

func TestSearchPage(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	body, err := c.Get(context.Background(), c.SearchURL("mechanical keyboard", SearchQuery{}, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := c.parseSearchPage("mechanical keyboard", "", 1, body)
	if err != nil {
		t.Fatal(err)
	}
	if sp.QueryEcho != "mechanical keyboard" {
		t.Errorf("query echo = %q", sp.QueryEcho)
	}
	if sp.From != 1 || sp.To != 3 || sp.Total != 20000 || !sp.Approx {
		t.Errorf("range = %d-%d of %d approx=%v", sp.From, sp.To, sp.Total, sp.Approx)
	}
	// The strip lives under s-search-results, because the region Amazon named
	// s-pagination ships empty. The two-pass region resolution is what finds it.
	if sp.CurrentPage != 1 || sp.NextPage != 2 || sp.LastPage != 20 {
		t.Errorf("pages = current %d next %d last %d", sp.CurrentPage, sp.NextPage, sp.LastPage)
	}
	if via := sp.Envelope.Via["page_last"]; via != "s-search-results" {
		t.Errorf("page_last came from %q", via)
	}
	if sp.Exhausted() {
		t.Errorf("page 1 reports exhausted")
	}
}

func TestFetchReviews(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var rs []Review
	err := c.FetchReviews(context.Background(), "B084DWG2VQ", ReviewQuery{Limit: 2}, func(r Review) error {
		rs = append(rs, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d reviews", len(rs))
	}
	if rs[0].Rating != 5 || rs[0].Title != "Phenomenal value" || !rs[0].VerifiedPurchase {
		t.Errorf("review0 = %+v", rs[0])
	}
	if rs[0].HelpfulVotes != 142 || rs[0].Country != "United States" {
		t.Errorf("review0 votes/country = %d %q", rs[0].HelpfulVotes, rs[0].Country)
	}
	if rs[0].VariantAttrs["colour"] != "Charcoal" {
		t.Errorf("review0 variant = %v", rs[0].VariantAttrs)
	}
}

func TestFetchQA(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var qs []QA
	err := c.FetchQA(context.Background(), "B084DWG2VQ", func(q QA) error {
		qs = append(qs, q)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("got %d qa", len(qs))
	}
	if qs[0].Question != "Does it work with Spotify?" {
		t.Errorf("q0 = %q", qs[0].Question)
	}
	if !strings.Contains(qs[0].Answer, "Spotify over Bluetooth") {
		t.Errorf("a0 = %q", qs[0].Answer)
	}
}

func TestFetchOffers(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var os []OfferListing
	err := c.FetchOffers(context.Background(), "B084DWG2VQ", OfferQuery{}, func(o OfferListing) error {
		os = append(os, o)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(os) != 2 {
		t.Fatalf("got %d offers", len(os))
	}
	if os[0].Price != 49.99 || !os[0].IsBuyBox || os[0].SellerID != "ATVPDKIKX0DER" {
		t.Errorf("offer0 = %+v", os[0])
	}
	if os[1].Price != 41.50 || !strings.Contains(os[1].Condition, "Used") {
		t.Errorf("offer1 = %+v", os[1])
	}
}

func TestFetchChart(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var es []BestsellerEntry
	err := c.FetchChart(context.Background(), ChartBestsellers, "electronics", "", 3, func(e BestsellerEntry) error {
		es = append(es, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 {
		t.Fatalf("got %d entries", len(es))
	}
	if es[0].Rank != 1 || es[0].ASIN != "B08C1W5N87" || es[0].Price.Float() != 24.99 {
		t.Errorf("entry0 = %+v", es[0])
	}
	if es[0].Title != "Fire TV Stick 4K streaming device" || es[0].Price.Cur() != "USD" {
		t.Errorf("entry0 title/currency = %q %q", es[0].Title, es[0].Price.Cur())
	}
	// The count shares its aria-label with the rating: "4.8 out of 5 stars,
	// 90,112 ratings". Reading the label without cutting the rating out of it
	// gives 4, which is a plausible number and a wrong one.
	if deref(es[2].RatingsCount) != 90112 || deref(es[2].Rating) != 4.8 {
		t.Errorf("entry2 rating/count = %v %d", es[2].Rating, es[2].RatingsCount)
	}

	// Unlimited paging must not duplicate items even though the fixture server
	// replays the same page on every request.
	var all []BestsellerEntry
	if err := c.FetchChart(context.Background(), ChartBestsellers, "electronics", "", 0, func(e BestsellerEntry) error {
		all = append(all, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Five, not three. The grid renders three tiles and the list Amazon
	// publishes for its own renderer names five, so ranks 4 and 5 arrive with an
	// ASIN and a rank and nothing else rather than not arriving.
	if len(all) != 5 {
		t.Fatalf("unlimited got %d entries, want 5", len(all))
	}
	asins := map[string]int{}
	for _, e := range all {
		asins[e.ASIN]++
	}
	for a, n := range asins {
		if n != 1 {
			t.Errorf("asin %s appeared %d times", a, n)
		}
	}
	for i, e := range all {
		if e.Rank != i+1 {
			t.Errorf("entry %d has rank %d", i, e.Rank)
		}
		if want := i >= 3; e.RankOnly != want {
			t.Errorf("entry %d rank_only = %v, want %v", i, e.RankOnly, want)
		}
	}
	if all[3].ASIN != "B0CHX3QBCH" || all[3].Title != "" {
		t.Errorf("entry3 = %+v", all[3])
	}
}

// TestChartPageCounts pins the three counts apart, because they disagree on
// every live chart and a single number could not say how.
func TestChartPageCounts(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	body, err := os.ReadFile("testdata/bestsellers.html")
	if err != nil {
		t.Fatal(err)
	}
	cp, err := c.parseChartPage("bestsellers", "electronics", "", "", 1, body)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Layout != "grid" {
		t.Errorf("layout = %q, want grid", cp.Layout)
	}
	if cp.RefTag != "zg_bs_g_electronics" {
		t.Errorf("ref tag = %q", cp.RefTag)
	}
	if cp.Listed != 5 || cp.ServerRendered != 3 || cp.Rendered != 3 {
		t.Errorf("listed/server/rendered = %d/%d/%d, want 5/3/3", cp.Listed, cp.ServerRendered, cp.Rendered)
	}
	// The gap is reported rather than smoothed over.
	var got string
	for _, m := range cp.Envelope.Missed {
		if m.Field == "rendered" {
			got = m.Why
		}
	}
	if !strings.Contains(got, "listed 5 items and rendered 3") {
		t.Errorf("missed rendered = %q", got)
	}
	// The rank came from the payload and the badge agreed, so nothing is filed
	// as a disagreement.
	e0 := cp.Entries[0]
	if via := e0.Envelope.Via["rank"]; via != PayloadChartList {
		t.Errorf("rank via = %q, want %q", via, PayloadChartList)
	}
	if len(e0.Envelope.Disagree) != 0 {
		t.Errorf("entry0 disagreements = %v", e0.Envelope.Disagree)
	}
	// The category tree names every sibling chart and nothing reads it yet, so
	// it has to show up on the worklist.
	var sawTree bool
	for _, u := range cp.Envelope.Unread {
		if u == chartNavTree {
			sawTree = true
		}
	}
	if !sawTree {
		t.Errorf("unread = %v, want it to include %s", cp.Envelope.Unread, chartNavTree)
	}
}

// TestChartIndexKeepsItsListsApart covers /gp/bestsellers with no category,
// which runs several rankings side by side. Every tile is rank 1 of a different
// list, and only the carousel it sits in says which.
func TestChartIndexKeepsItsListsApart(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	body, err := os.ReadFile("testdata/bestsellers_index.html")
	if err != nil {
		t.Fatal(err)
	}
	cp, err := c.parseChartPage("bestsellers", "", "", "", 1, body)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Layout != "index" {
		t.Fatalf("layout = %q, want index", cp.Layout)
	}
	if len(cp.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(cp.Entries))
	}
	want := map[string]string{
		"B079VP6DH5": "Best Sellers in Health & Household",
		"B08JHCVHTY": "Best Sellers in Electronics",
	}
	for _, e := range cp.Entries {
		if e.Rank != 1 {
			t.Errorf("%s rank = %d, want 1", e.ASIN, e.Rank)
		}
		if got := want[e.ASIN]; e.Category != got {
			t.Errorf("%s category = %q, want %q", e.ASIN, e.Category, got)
		}
	}
	// No list payload here, and that is the page being a directory rather than
	// a chart, so it must not be reported as a missing payload.
	for _, m := range cp.Envelope.Missed {
		if m.Field == "listed" {
			t.Errorf("index layout reported a payload miss: %q", m.Why)
		}
	}
}

func TestFetchCategory(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	cat, err := c.FetchCategory(context.Background(), "172282")
	if err != nil {
		t.Fatal(err)
	}
	// The name comes from the canonical slug, because no heading on the page
	// carries it: the h1 is the word "Department" and the title is a path.
	if cat.Name != "Electronics Store" || cat.Slug != "electronics-store" {
		t.Errorf("name = %q slug = %q", cat.Name, cat.Slug)
	}
	if cat.CanonicalNode != "172282" {
		t.Errorf("canonical node = %q", cat.CanonicalNode)
	}
	// The page's own node is excluded from the links it makes to other nodes.
	for _, r := range cat.Related {
		if r.ID == "172282" {
			t.Errorf("related nodes include the page's own node: %v", cat.Related)
		}
		// A related node is a reference and not a bare id, so it carries the
		// word Amazon used for it and a URL that goes there.
		if r.Kind != RefNode || !r.Resolved || r.URL == "" {
			t.Errorf("related node is not a usable reference: %+v", r)
		}
	}
	if len(cat.Related) != 2 {
		t.Errorf("related = %v", cat.Related)
	}
	if len(cat.TopASINs) != 3 || cat.ItemCount != 3 {
		t.Errorf("top_asins = %v count = %d", cat.TopASINs, cat.ItemCount)
	}
	// A browse page is a stack of shelves and each one names its own list, so
	// the shelves survive into the record rather than being flattened away.
	if len(cat.Shelves) != 2 {
		t.Fatalf("shelves = %+v", cat.Shelves)
	}
	if cat.Shelves[0].Title != "Shop OtterBox" || len(cat.Shelves[0].ASINs) != 2 {
		t.Errorf("shelf 0 = %+v", cat.Shelves[0])
	}
	if cat.Shelves[1].Title != "Tech deals for you" || len(cat.Shelves[1].ASINs) != 1 {
		t.Errorf("shelf 1 = %+v", cat.Shelves[1])
	}
	// One shelf is named by a bare UUID, which is an identifier for this render
	// rather than a name, so it must not enter the region index and turn the
	// unread worklist into a different list on every fetch.
	for _, n := range cat.Envelope.Unread {
		if bareUUIDRe.MatchString(n) {
			t.Errorf("a bare UUID reached the region index: %q", n)
		}
	}
}

// TestBrowseTileShapeFollowsItsIdentifier pins the finding that made the browse
// rewire possible: data-csa-c-item-id says which fields a tile carries.
//
// A plain amzn1.asin identifier means a product tile with a rating and a
// delivery estimate. The compound amzn1.asin:amzn1.deal form means a promotion,
// with a badge and a struck through price and no rating at all. On the live
// capture this held for all 59 tiles, so a deal tile with no rating is Amazon
// not publishing one rather than this parser failing to read it.
func TestBrowseTileShapeFollowsItsIdentifier(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	body := readFixture(t, "category.html")
	bp, err := c.parseBrowsePage("172282", "https://www.amazon.com/b?node=172282", body)
	if err != nil {
		t.Fatal(err)
	}
	var plain, deals int
	for _, sh := range bp.Shelves {
		for _, it := range sh.Items {
			if it.DealID == "" {
				plain++
				if it.Rating == nil || it.Delivery == "" {
					t.Errorf("plain tile %s has no rating or delivery: %+v", it.ASIN, it)
				}
				if it.DealType != "" {
					t.Errorf("plain tile %s carries a deal badge: %q", it.ASIN, it.DealType)
				}
				continue
			}
			deals++
			if it.Rating != nil || it.Delivery != "" {
				t.Errorf("deal tile %s carries a rating or delivery: %+v", it.ASIN, it)
			}
			if it.DealType == "" || it.DiscountPct == 0 {
				t.Errorf("deal tile %s has no badge: %+v", it.ASIN, it)
			}
		}
	}
	if plain != 2 || deals != 1 {
		t.Errorf("plain = %d deals = %d", plain, deals)
	}
}

// TestBrowseComputesTheDiscountItWasNotGiven checks the alternate source for
// discount_pct: a plain tile carries both prices and no badge, and the two
// prices imply the figure Amazon did not print.
func TestBrowseComputesTheDiscountItWasNotGiven(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	body := readFixture(t, "category.html")
	bp, err := c.parseBrowsePage("172282", "https://www.amazon.com/b?node=172282", body)
	if err != nil {
		t.Fatal(err)
	}
	it := bp.Shelves[0].Items[0]
	if it.ASIN != "B07B43WPVK" {
		t.Fatalf("first item = %q", it.ASIN)
	}
	// $1,398.00 against a typical price of $1,698.00 is 17.67 percent off, and
	// the tile has no badge to say so. It rounds to 18 rather than truncating to
	// 17 because rounding is what Amazon does with the figure when it does print
	// one, measured across every badged tile on all three captures.
	if it.DiscountPct != 18 {
		t.Errorf("discount = %d, want 18", it.DiscountPct)
	}
	if it.Envelope.Via["discount_pct"] != "price and was_price" {
		t.Errorf("discount provenance = %q", it.Envelope.Via["discount_pct"])
	}
	// The label travels with the number: a typical price is a computed average
	// and calling it a list price would be a different claim.
	if it.WasPriceLabel != "Typical" || it.WasPrice.Float() != 1698 {
		t.Errorf("was price = %v %q", it.WasPrice, it.WasPriceLabel)
	}
	// The image comes from the variant map rather than the src, so the 480 pixel
	// rendition wins over the 240 pixel one that was actually drawn.
	if it.Envelope.Via["image"] != "data-a-dynamic-image" || !strings.Contains(it.Image, "711KuxSzmqL") {
		t.Errorf("image = %q via %q", it.Image, it.Envelope.Via["image"])
	}
}

func TestFetchBrand(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	b, err := c.FetchBrand(context.Background(), "Anker")
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "Anker" {
		t.Errorf("name = %q", b.Name)
	}
	if b.PageID != "9F16B940-F912-43FE-888C-5BB1B86337A9" {
		t.Errorf("page id = %q, want the id in the canonical URL rather than the root page's", b.PageID)
	}
	// A brand storefront is a navigation page, so the nav tree is the record.
	if len(b.Nav) != 3 {
		t.Fatalf("nav = %+v, want the landing page and its two children", b.Nav)
	}
	if b.Nav[0].Title != "Anker" || b.Nav[0].Level != 1 || len(b.Nav[0].Children) != 2 {
		t.Errorf("nav[0] = %+v", b.Nav[0])
	}
	if b.Nav[1].Title != "Chargers" || b.Nav[1].Parent != b.Nav[0].PageID {
		t.Errorf("nav[1] = %+v, want a level 2 page pointing at the landing page", b.Nav[1])
	}
	if len(b.FeaturedASINs) != 2 {
		t.Errorf("featured = %v, want the two the widgets name", b.FeaturedASINs)
	}
}

// TestStorePagesDoNotReportTheFooterAsContent is the regression for the worst
// bug in the parser this replaces.
//
// Every page on amazon.com carries links to the Amazon Business Card and to
// Reload Your Balance in its footer, both of which are /dp/ URLs. The old brand
// and author parsers collected every a[href*='/dp/'] in the document, so both of
// those arrived as products. On the live captures they were two of the brand
// page's four ASINs and two of the author page's three, which means most of what
// a bibliography said was wrong and looked exactly like the part that was right.
func TestStorePagesDoNotReportTheFooterAsContent(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	b, err := c.FetchBrand(context.Background(), "Anker")
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.FetchAuthor(context.Background(), "stephenking")
	if err != nil {
		t.Fatal(err)
	}
	for _, footer := range []string{"B07984JN3L", "B0CHTVMXZJ"} {
		for _, x := range b.FeaturedASINs {
			if x == footer {
				t.Errorf("brand featured %s, which is in the footer of every page on the site", footer)
			}
		}
		for _, x := range a.BookASINs {
			if x == footer {
				t.Errorf("author bibliography lists %s, which is in the footer of every page on the site", footer)
			}
		}
	}
}

func TestFetchSeller(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	s, err := c.FetchSeller(context.Background(), "A1XYZSELLER22")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Anker Direct" {
		t.Errorf("name = %q", s.Name)
	}
	// The legal entity is not the display name, and it is the one that says who
	// is actually selling.
	if s.BusinessName != "Anker Innovations Limited" {
		t.Errorf("business name = %q", s.BusinessName)
	}
	// The address is four separate spans under one bold label, so a rule that
	// took the next span after the label would return the street and stop.
	if s.BusinessAddress != "Room 1318-19, Hollywood Plaza Kowloon HK" {
		t.Errorf("business address = %q, want the whole address rather than the first line", s.BusinessAddress)
	}
	if !strings.HasPrefix(s.About, "Anker Direct is the official store") {
		t.Errorf("about = %q, want the seller's own text with the section heading removed", s.About)
	}
	// Amazon puts the contact form inside the about section, so an unfiltered
	// read ended every seller description on the site with "Ask a question".
	if strings.Contains(s.About, "Ask a question") {
		t.Errorf("about = %q, want the contact widget dropped", s.About)
	}
	if !strings.Contains(s.StorefrontURL, "me=A1XYZSELLER22") {
		t.Errorf("storefront = %q, want the link to the seller's own listings", s.StorefrontURL)
	}
	if s.ShippingPolicy == "" || s.ReturnPolicy == "" {
		t.Errorf("policies = %q / %q", s.ShippingPolicy, s.ReturnPolicy)
	}
}

// TestSellerWithNoFeedbackReportsAMissRatherThanZero is the seller half of the
// same lesson the browse family taught.
//
// This seller publishes no feedback rating. The old parser scanned the document
// text for a count, found none, and left the field at zero, so a seller Amazon
// does not rate and a seller rated zero produced identical records. Declaring
// the field and recording the miss keeps them apart.
func TestSellerWithNoFeedbackReportsAMissRatherThanZero(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	s, err := c.FetchSeller(context.Background(), "A1XYZSELLER22")
	if err != nil {
		t.Fatal(err)
	}
	if s.Rating != 0 || s.RatingCount != 0 || len(s.Feedback) != 0 || len(s.RatingHistogram) != 0 {
		t.Fatalf("rating = %v count = %d, but the fixture publishes no feedback", s.Rating, s.RatingCount)
	}
	var sawRating, sawCount bool
	for _, m := range s.Envelope.Missed {
		switch m.Field {
		case "rating":
			sawRating = true
		case "ratings_count":
			sawCount = true
		}
		if m.Field == "rating" && m.Why == "" {
			t.Error("a miss with no reason is the silence this envelope exists to break")
		}
	}
	if !sawRating || !sawCount {
		t.Errorf("missed = %+v, want both feedback fields named", s.Envelope.Missed)
	}
}

// TestRatedSellerKeepsEveryWindowItPublishes covers the other kind of seller
// profile, the one that carries feedback.
//
// The four windows are the reason this is worth a test of its own. Amazon builds
// the block once and fills it four times, hiding three of the four behind a
// dropdown, and all four are in the served HTML. A parser that took the visible
// one would report a seller as 5.0 and say nothing about the 6,365 lifetime
// ratings that figure is a recent slice of.
func TestRatedSellerKeepsEveryWindowItPublishes(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	s, err := c.FetchSeller(context.Background(), "A9RATEDSELLER1")
	if err != nil {
		t.Fatal(err)
	}
	// The header figures are the twelve month window, which is the one a buyer
	// is shown, rather than the thirty day one that happens to be visible.
	if s.Rating != 5 || s.RatingCount != 44 || s.PositivePct != 100 {
		t.Errorf("rating = %v count = %d positive = %v, want the twelve month window", s.Rating, s.RatingCount, s.PositivePct)
	}
	want := []SellerFeedback{
		{Period: "30d", Rating: 5, Count: 4},
		{Period: "90d", Rating: 5, Count: 11},
		{Period: "12m", Rating: 5, Count: 44},
		{Period: "lifetime", Rating: 4.8, Count: 6365},
	}
	if !reflect.DeepEqual(s.Feedback, want) {
		t.Errorf("feedback = %+v, want %+v", s.Feedback, want)
	}
	// The histogram is what stops a 4.8 with a two percent tail of one star
	// ratings from reading like a clean 4.8.
	wantHist := []SellerStarShare{{5, 87}, {4, 8}, {3, 1}, {2, 1}, {1, 2}}
	if !reflect.DeepEqual(s.RatingHistogram, wantHist) {
		t.Errorf("histogram = %+v, want %+v", s.RatingHistogram, wantHist)
	}
	// Three reviews, not four. The fourth .feedback-row in the section is
	// #feedback-row-template, the empty row Amazon clones the AJAX pages into.
	if len(s.Reviews) != 3 {
		t.Fatalf("reviews = %d, want the three real rows and not the template", len(s.Reviews))
	}
	if r := s.Reviews[0]; r.Rating != 5 || r.Text != "Excellent" || r.Rater != "Amazon Customer" || r.Date != "July 30, 2026" {
		t.Errorf("review[0] = %+v", r)
	}
	// A long review is served clipped and whole. Reading the row rather than the
	// expanded copy gives the opening sentence, then the whole thing, then the
	// words "Read more Read less".
	if r := s.Reviews[1]; strings.Contains(r.Text, "Read more") || strings.Contains(r.Text, "Thanks,...") {
		t.Errorf("review[1] text = %q, want the expanded copy only", r.Text)
	}
	// "By SAMCare, LLC on July 6, 2026." is one span holding a buyer and a date,
	// and the buyer's name has a comma in it.
	if r := s.Reviews[2]; r.Rater != "SAMCare, LLC" || r.Date != "July 6, 2026" || r.Rating != 4 {
		t.Errorf("review[2] = %+v", r)
	}
	// The about block is served twice, clipped and whole. Reading the section
	// returned the opening sentence, the whole thing, and "See moreSee less".
	if strings.Contains(s.About, "See more") || strings.Contains(s.About, "Ask a question") {
		t.Errorf("about = %q, want the expanded copy with the contact widget dropped", s.About)
	}
	if !strings.Contains(s.About, "tailored products") {
		t.Errorf("about = %q, want the full copy rather than the clipped one", s.About)
	}
	// A third party seller's legal entity is genuinely not its display name.
	if s.BusinessName != "Hangzhou Hang Kai Technology Co.,Ltd" {
		t.Errorf("business_name = %q", s.BusinessName)
	}
	if len(s.Envelope.Unread) != 0 {
		t.Errorf("unread = %v, want every named section either read or claimed", s.Envelope.Unread)
	}
	for _, m := range s.Envelope.Missed {
		t.Errorf("missed = %+v, but this fixture publishes every declared field", m)
	}
}

func TestFetchAuthor(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	a, err := c.FetchAuthor(context.Background(), "stephenking")
	if err != nil {
		t.Fatal(err)
	}
	// The payload name, not the og:title, which carries the marketing tail
	// "Stephen King: books, biography, latest update".
	if a.Name != "Stephen King" {
		t.Errorf("name = %q", a.Name)
	}
	if a.PageID != "B000AQ0KWU" {
		t.Errorf("page id = %q", a.PageID)
	}
	if !strings.HasSuffix(a.AboutURL, "/stores/author/B000AQ0KWU/about") {
		t.Errorf("about url = %q", a.AboutURL)
	}
	// The bio is an array of paragraphs in the payload and a clipped single
	// paragraph in the DOM, and the payload one is the whole thing.
	if !strings.Contains(a.Bio, "Bangor, Maine") {
		t.Errorf("bio = %q, want the payload's paragraphs rather than the clipped DOM copy", a.Bio)
	}
	// The DOM says "We couldn't find anything matching these filters" where the
	// books should be. The payload says there are 64 of them and ships 2.
	if len(a.Books) != 2 {
		t.Fatalf("books = %d, want the records the grid payload shipped", len(a.Books))
	}
	if a.TotalBooks != 64 {
		t.Errorf("total = %d, want what Amazon says the bibliography holds", a.TotalBooks)
	}
	if len(a.BookASINs) != 3 {
		t.Errorf("asins = %v, want the grid's full ASIN list rather than only the shipped page", a.BookASINs)
	}
	if len(a.SortOptions) != 2 || a.SortOptions[0] != "author-sidecar-rank" {
		t.Errorf("sorts = %v", a.SortOptions)
	}
	if len(a.Languages) != 2 {
		t.Errorf("languages = %v", a.Languages)
	}
}

// TestAuthorGridReadsTheOfferThatPublishesAPrice covers the two things that made
// the grid payload hard to read.
//
// The payload is hypermedia: a sub-resource is an object when the page has it
// and a URL string when the page does not. Across 79 offers on the live capture
// the price was an object 67 times and a string 12 times, and a struct typed to
// the object form failed the whole 562 KB decode on the first string. Then, of
// the products that survived, 9 of 70 had every field by reference on the first
// offer and inline on a later one, so reading offer zero and stopping threw away
// a real price. The first book below is that shape and the second is the one
// with no price on the page at all.
func TestAuthorGridReadsTheOfferThatPublishesAPrice(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	a, err := c.FetchAuthor(context.Background(), "stephenking")
	if err != nil {
		t.Fatal(err)
	}
	first := a.Books[0]
	if first.Price != 9.99 || first.Currency != "USD" {
		t.Errorf("price = %v %s, want the second offer's inline price", first.Price, first.Currency)
	}
	if first.Merchant != "Random House LLC" || first.Availability == "" {
		t.Errorf("merchant = %q availability = %q", first.Merchant, first.Availability)
	}
	if first.Title != "The Shining" || first.Binding != "Kindle Edition" {
		t.Errorf("title = %q binding = %q", first.Title, first.Binding)
	}
	if first.Rating != 4.7 || first.RatingsCount != 98213 {
		t.Errorf("rating = %v (%d)", first.Rating, first.RatingsCount)
	}
	if first.Series != "The Shining" || first.SeriesPosition != 1 || first.BestSellerRank != 3 {
		t.Errorf("series = %+v rank = %d", first, first.BestSellerRank)
	}
	if len(first.Editions) != 2 || len(first.Contributors) != 1 {
		t.Errorf("editions = %v contributors = %v", first.Editions, first.Contributors)
	}
	// The contributor link arrives as an HTML entity inside a JSON string inside
	// a script tag, and nothing else on the way here unescapes it.
	if strings.Contains(first.Contributors[0].URL, "&amp;") {
		t.Errorf("contributor url = %q, still HTML escaped", first.Contributors[0].URL)
	}
	// The hi-res rendition, not the thumbnail Amazon lists beside it.
	if !strings.Contains(first.Image, "hires") {
		t.Errorf("image = %q", first.Image)
	}

	second := a.Books[1]
	if second.Price != 0 {
		t.Errorf("price = %v, but every offer for this book states its price by reference", second.Price)
	}
	// A book whose price is a URL still has a range, and that range is the
	// honest answer rather than a zero standing in for one.
	if second.MinPrice != 12.75 || second.MaxPrice != 39 || second.OfferCount != 11 {
		t.Errorf("offer summary = %v to %v across %d", second.MinPrice, second.MaxPrice, second.OfferCount)
	}
	// Only the low resolution rendition is published for this one.
	if !strings.Contains(second.Image, "lores") {
		t.Errorf("image = %q", second.Image)
	}
}

func TestFetchDeals(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	var ds []Deal
	err := c.FetchDeals(context.Background(), 10, func(d Deal) error {
		ds = append(ds, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 {
		t.Fatalf("got %d deals", len(ds))
	}
	if ds[0].ASIN != "B004UBUJZG" || ds[0].DealID != "1e7a7bea" {
		t.Errorf("deal0 identity = %q %q", ds[0].ASIN, ds[0].DealID)
	}
	if ds[0].DealPrice.Float() != 239.98 || ds[0].ListPrice.Float() != 299.99 || ds[0].DiscountPct != 20 {
		t.Errorf("deal0 prices = %+v", ds[0])
	}
	if ds[0].ListLabel != "List" || ds[0].Badge != "Limited time deal" {
		t.Errorf("deal0 labels = %q %q", ds[0].ListLabel, ds[0].Badge)
	}
	if ds[0].Title != "TP-Link 48 Port Gigabit Ethernet Switch" {
		t.Errorf("deal0 title = %q", ds[0].Title)
	}
	// A deal tile's title is in dcl-product-label where a plain tile uses
	// dcl-product-title, and both have to land in the same field.
	if ds[0].Shelf != "Save on big purchases" || ds[0].Position != 1 {
		t.Errorf("deal0 placement = %q %d", ds[0].Shelf, ds[0].Position)
	}
	// The countdown is named in the DOM and empty, so the flag is set and no
	// time is claimed. The other tile has no countdown and the flag is false,
	// which must not be reported as a field this parser failed to read.
	if !ds[1].EndsSoon || ds[0].EndsSoon {
		t.Errorf("ends_soon = %v %v", ds[0].EndsSoon, ds[1].EndsSoon)
	}
	for _, m := range ds[0].Envelope.Missed {
		if m.Field == "ends_soon" {
			t.Errorf("a tile with no countdown reported a miss: %q", m.Why)
		}
	}
}

// TestDealsPageHasNoDataTestIDOrDataASIN pins the defect that made this rewire
// necessary. The parser this replaced looked for [data-testid='deal-card'] and
// div[data-asin], and /deals carries neither, so it matched nothing and returned
// an empty list with no error.
func TestDealsPageHasNoDataTestIDOrDataASIN(t *testing.T) {
	body := readFixture(t, "deals.html")
	doc, err := newDocument(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, sel := range []string{"[data-testid]", "[data-asin]", ".DealCard-module"} {
		if n := doc.Find(sel).Length(); n != 0 {
			t.Errorf("%s matched %d nodes; the live capture had none", sel, n)
		}
	}
	if n := doc.Find(browseItemSel).Length(); n != 2 {
		t.Errorf("%s matched %d tiles, want 2", browseItemSel, n)
	}
}

// readFixture loads a capture miniature from testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
