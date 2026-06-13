package amz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureServer serves testdata files based on the request path, mimicking the
// Amazon URL layout, so every fetcher can be exercised offline end-to-end.
func fixtureServer(t *testing.T) (*Client, func()) {
	t.Helper()
	route := func(p string) string {
		switch {
		case strings.HasPrefix(p, "/dp/"):
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
		// Pagination terminates: page 2+ of search/reviews returns an empty list.
		if pg := r.URL.Query().Get("page"); pg != "" && pg != "1" {
			w.Write([]byte("<html><body></body></html>"))
			return
		}
		if pg := r.URL.Query().Get("pageNumber"); pg != "" && pg != "1" {
			w.Write([]byte("<html><body></body></html>"))
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(b)
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
	if p.Brand != "Amazon" {
		t.Errorf("brand = %q", p.Brand)
	}
	if p.Price != 49.99 || p.Currency != "USD" {
		t.Errorf("price = %v %s", p.Price, p.Currency)
	}
	if p.ListPrice != 59.99 {
		t.Errorf("list_price = %v", p.ListPrice)
	}
	if p.Rating != 4.7 || p.RatingsCount != 284512 {
		t.Errorf("rating = %v count = %d", p.Rating, p.RatingsCount)
	}
	if p.AnsweredQs != 1204 {
		t.Errorf("answered_qs = %d", p.AnsweredQs)
	}
	if p.Availability != "In Stock" {
		t.Errorf("availability = %q", p.Availability)
	}
	if len(p.BulletPoints) != 2 {
		t.Errorf("bullets = %v", p.BulletPoints)
	}
	if p.Specs["Colour"] != "Charcoal" {
		t.Errorf("specs = %v", p.Specs)
	}
	if len(p.Images) != 2 {
		t.Errorf("images = %v", p.Images)
	}
	if strings.Join(p.CategoryPath, "/") != "Electronics/Smart Home/Speakers" {
		t.Errorf("category_path = %v", p.CategoryPath)
	}
	if p.SellerID != "ATVPDKIKX0DER" || p.SellerName != "Amazon.com" {
		t.Errorf("seller = %s %s", p.SellerID, p.SellerName)
	}
	if len(p.VariantASINs) != 2 {
		t.Errorf("variants = %v", p.VariantASINs)
	}
	if p.Rank != 3 || !strings.HasPrefix(p.RankCategory, "Electronics") {
		t.Errorf("rank = %d %q", p.Rank, p.RankCategory)
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
	if cards[0].ASIN != "B08F6PHTJ4" || cards[0].Price != 149.99 || cards[0].Rating != 4.6 {
		t.Errorf("card0 = %+v", cards[0])
	}
	if cards[0].RatingsCount != 38201 {
		t.Errorf("card0 ratings = %d", cards[0].RatingsCount)
	}
	if !cards[1].Sponsored {
		t.Errorf("card1 should be sponsored")
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
	var os []Offer
	err := c.FetchOffers(context.Background(), "B084DWG2VQ", OfferQuery{}, func(o Offer) error {
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
	if es[0].Rank != 1 || es[0].ASIN != "B08C1W5N87" || es[0].Price != 24.99 {
		t.Errorf("entry0 = %+v", es[0])
	}
	if es[2].RatingsCount != 90112 {
		t.Errorf("entry2 ratings = %d", es[2].RatingsCount)
	}
}

func TestFetchCategory(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	cat, err := c.FetchCategory(context.Background(), "172282")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Name != "Electronics" {
		t.Errorf("name = %q", cat.Name)
	}
	if len(cat.ChildNodeIDs) < 2 {
		t.Errorf("children = %v", cat.ChildNodeIDs)
	}
	if len(cat.TopASINs) != 3 {
		t.Errorf("top_asins = %v", cat.TopASINs)
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
	if len(b.FeaturedASINs) != 3 {
		t.Errorf("featured = %v", b.FeaturedASINs)
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
	if s.RatingCount != 92481 || s.PositivePct != 95 || s.NegativePct != 3 {
		t.Errorf("ratings = %d pos=%v neg=%v", s.RatingCount, s.PositivePct, s.NegativePct)
	}
}

func TestFetchAuthor(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	a, err := c.FetchAuthor(context.Background(), "stephenking")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Stephen King" {
		t.Errorf("name = %q", a.Name)
	}
	if len(a.BookASINs) != 3 {
		t.Errorf("books = %v", a.BookASINs)
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
	if ds[0].DealPrice != 24.99 || ds[0].ListPrice != 49.99 || ds[0].DiscountPct != 50 {
		t.Errorf("deal0 = %+v", ds[0])
	}
	if ds[0].Title != "Fire TV Stick 4K" {
		t.Errorf("deal0 title = %q", ds[0].Title)
	}
}
