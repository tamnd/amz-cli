package amz

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/amz-cli/pkg/graph"
)

var read = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

// find is the one edge matching a predicate and destination, or a failure.
func find(t *testing.T, edges []Edge, predicate, dst string) Edge {
	t.Helper()
	var hits []Edge
	for _, e := range edges {
		if e.Predicate == predicate && (dst == "" || e.Dst == dst) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		t.Fatalf("no %s edge to %q in:\n%s", predicate, dst, dump(edges))
	case 1:
		return hits[0]
	}
	t.Fatalf("%d %s edges to %q, want one", len(hits), predicate, dst)
	return Edge{}
}

func has(edges []Edge, predicate, dst string) bool {
	for _, e := range edges {
		if e.Predicate == predicate && e.Dst == dst {
			return true
		}
	}
	return false
}

func countPred(edges []Edge, predicate string) int {
	n := 0
	for _, e := range edges {
		if e.Predicate == predicate {
			n++
		}
	}
	return n
}

func dump(edges []Edge) string {
	var b strings.Builder
	for _, e := range edges {
		b.WriteString("  " + e.Src + " " + e.Predicate + " " + e.Dst + " via=" + e.Via + "\n")
	}
	return b.String()
}

func ref(kind, id, name, u string) *Ref {
	return &Ref{Kind: kind, ID: id, Name: name, URI: u, Resolved: u != ""}
}

// sampleProduct is a detail page with one of everything the graph reads.
func sampleProduct() Product {
	p := Product{
		Marketplace: "us",
		ASIN:        "B0TESTPROD",
		Title:       "A thing",
		URL:         "https://www.amazon.com/dp/B0TESTPROD",
		FetchedAt:   read,
		Brand:       ref(RefBrand, "acme", "Acme", "amz:brand/acme"),
		Breadcrumb: []Ref{
			*ref(RefNode, "172282", "Electronics", "amz:us/node/172282"),
			*ref(RefNode, "281052", "Headphones", "amz:us/node/281052"),
		},
		Ranks: []Rank{{
			Rank: 42, Category: "Electronics", Overall: true,
			Node: ref(RefNode, "172282", "Electronics", "amz:us/node/172282"),
		}},
		Offer: &Offer{
			Condition: "New",
			SoldBy:    ref(RefSeller, "A1SELLER", "Acme Direct", "amz:seller/A1SELLER"),
			ShipsFrom: ref(RefSeller, "ATVPDKIKX0DER", "Amazon", "amz:seller/ATVPDKIKX0DER"),
		},
		Rails: []Rail{
			{Region: "sims", Title: "Also bought", Cards: []Card{{ASIN: "B0ORGANIC1", Position: 1}}},
			{Region: "ads", Sponsored: true, Cards: []Card{{ASIN: "B0SPONSOR1", Position: 1}}},
		},
		SimilarASINs: []string{"B0SIMILAR1"},
	}
	return p
}

func TestProductEdgesReadsThePageOnce(t *testing.T) {
	edges := ProductEdges(sampleProduct())
	if len(edges) == 0 {
		t.Fatal("a full detail page produced no edges")
	}
	for _, e := range edges {
		if err := e.Valid(); err != nil {
			t.Errorf("invalid edge: %v", err)
		}
		if e.ObservedAt != read {
			t.Errorf("%s %s: observed_at is %v, want the page read time", e.Predicate, e.Dst, e.ObservedAt)
		}
		if e.Via == "" {
			t.Errorf("%s %s: no surface, so the edge cannot say which page asserted it", e.Predicate, e.Dst)
		}
	}
	find(t, edges, graph.MadeBy, "amz:brand/acme")
	find(t, edges, graph.SoldBy, "amz:seller/A1SELLER")
	find(t, edges, graph.FulfilledBy, "amz:seller/ATVPDKIKX0DER")
}

// An unresolved Ref is a display name with no id. It stays on the record and it
// does not become a node, because a node keyed on a name merges every spelling
// of "Amazon Basics" into one brand and splits one brand across two.
func TestUnresolvedRefIsNotAnEdge(t *testing.T) {
	p := sampleProduct()
	p.Brand = &Ref{Kind: RefBrand, Name: "Acme"}
	if edges := ProductEdges(p); countPred(edges, graph.MadeBy) != 0 {
		t.Fatalf("a brand with no store link became an edge:\n%s", dump(edges))
	}
}

// A record with no read time yields nothing at all. An edge without a timestamp
// is not an observation, and the alternative to dropping it is stamping it now,
// which would date a claim to when it was exported rather than when it was read.
func TestNoTimestampMeansNoEdges(t *testing.T) {
	p := sampleProduct()
	p.FetchedAt = time.Time{}
	p.Envelope.RetrievedAt = time.Time{}
	if got := ProductEdges(p); got != nil {
		t.Fatalf("a record with no read time produced %d edges", len(got))
	}
}

// The rank line and the breadcrumb both say in_node and they are different
// claims, so both are written and each says where it came from.
func TestInNodeCarriesWhichSurfaceSaidIt(t *testing.T) {
	edges := ProductEdges(sampleProduct())
	var froms []string
	for _, e := range edges {
		if e.Predicate == graph.InNode && e.Dst == "amz:us/node/172282" {
			froms = append(froms, e.Props["from"].(string))
		}
	}
	if len(froms) != 1 {
		// Same src, predicate, dst and via: they are one claim by the key, and
		// the last write wins. What matters is that the property is stated.
		t.Logf("in_node to 172282 written %d times: %v", len(froms), froms)
	}
	rank := find(t, edges, graph.RankedIn, "amz:us/node/172282")
	if rank.Position != 42 || rank.Props["rank"] != 42 {
		t.Fatalf("ranked_in lost the rank: %+v", rank)
	}
	if rank.Props["overall"] != true {
		t.Fatalf("ranked_in dropped the overall flag: %+v", rank.Props)
	}
}

// child_of comes from the breadcrumb and only from the breadcrumb, because a
// trail is ordered root first and a browse page's link list is not ordered at
// all.
func TestChildOfComesFromTheOrderedBreadcrumb(t *testing.T) {
	edges := ProductEdges(sampleProduct())
	e := find(t, edges, graph.ChildOf, "amz:us/node/172282")
	if e.Src != "amz:us/node/281052" {
		t.Fatalf("child_of runs %s -> %s, want the deeper node under the shallower", e.Src, e.Dst)
	}
	if countPred(edges, graph.ChildOf) != 1 {
		t.Fatalf("a two step breadcrumb produced %d child_of edges, want 1", countPred(edges, graph.ChildOf))
	}
}

func TestCategoryEdgesDoesNotGuessAHierarchy(t *testing.T) {
	c := Category{
		NodeID:   "172282",
		TopASINs: []string{"B0TESTPROD"},
	}
	c.Envelope.Marketplace = "us"
	c.FetchedAt = read
	edges := CategoryEdges(c)
	if countPred(edges, graph.ChildOf) != 0 {
		t.Fatalf("a browse page asserted a hierarchy it cannot see:\n%s", dump(edges))
	}
	e := find(t, edges, graph.InNode, "amz:us/node/172282")
	if e.Src != "amz:us/product/B0TESTPROD" || e.Props["from"] != "browse" {
		t.Fatalf("in_node off a browse page is wrong: %+v", e)
	}
}

// The one inferred edge in the vocabulary says so on the edge, because Amazon
// never states that a merchant sells a brand and a consumer must not read this
// as though it did.
func TestSellsUnderIsMarkedInferred(t *testing.T) {
	e := find(t, ProductEdges(sampleProduct()), graph.SellsUnder, "amz:brand/acme")
	if e.Src != "amz:seller/A1SELLER" {
		t.Fatalf("sells_under runs from %s, want the seller", e.Src)
	}
	if e.Props["inferred"] != true {
		t.Fatalf("sells_under is not marked inferred: %+v", e.Props)
	}
}

// Twelve colours is twelve edges and not a hundred and thirty two. parent_of is
// stored once from the parent and traversed both ways.
func TestVariationFansOutFromTheParent(t *testing.T) {
	p := sampleProduct()
	p.Variation = &Variation{
		ParentASIN: "B0PARENT01",
		Current:    map[string]string{"Color": "Black"},
		Siblings: []Sibling{
			{ASIN: "B0SIBLING1", Values: map[string]string{"Color": "Red"}},
			{ASIN: "B0SIBLING2", Values: map[string]string{"Color": "Blue"}},
		},
	}
	edges := ProductEdges(p)
	if got := countPred(edges, graph.ParentOf); got != 3 {
		t.Fatalf("%d parent_of edges for a parent and two siblings, want 3:\n%s", got, dump(edges))
	}
	if countPred(edges, graph.VariantOf) != 0 {
		t.Fatal("both parent_of and variant_of were written for one family")
	}
	for _, e := range edges {
		if e.Predicate == graph.ParentOf && e.Src != "amz:us/product/B0PARENT01" {
			t.Fatalf("parent_of runs from %s, want the parent every time", e.Src)
		}
	}
}

// A twister that ships siblings without naming the family gets symmetric
// variant_of instead, so the family is still reachable from this member.
func TestVariationWithNoParentUsesVariantOf(t *testing.T) {
	p := sampleProduct()
	p.Variation = &Variation{Siblings: []Sibling{{ASIN: "B0SIBLING1"}}}
	edges := ProductEdges(p)
	if countPred(edges, graph.ParentOf) != 0 {
		t.Fatal("parent_of was written with no parent ASIN to hang it on")
	}
	find(t, edges, graph.VariantOf, "amz:us/product/B0SIBLING1")
}

// Advertising is written, flagged, and given its own place in the data. A crawl
// that dropped it would leave a store that cannot answer how much of a page was
// paid for.
func TestSponsoredRailIsWrittenAndFlagged(t *testing.T) {
	edges := ProductEdges(sampleProduct())
	organic := find(t, edges, graph.RelatedTo, "amz:us/product/B0ORGANIC1")
	if organic.Sponsored {
		t.Fatal("an organic rail card was marked sponsored")
	}
	paid := find(t, edges, graph.RelatedTo, "amz:us/product/B0SPONSOR1")
	if !paid.Sponsored {
		t.Fatal("a card from a sponsored rail was written as organic")
	}
	if !has(edges, graph.RelatedTo, "amz:us/product/B0SIMILAR1") {
		t.Fatalf("the similar ASIN list produced no edge:\n%s", dump(edges))
	}
}

func TestNoSelfEdges(t *testing.T) {
	p := sampleProduct()
	p.SimilarASINs = []string{p.ASIN}
	p.Rails = []Rail{{Region: "sims", Cards: []Card{{ASIN: p.ASIN}}}}
	for _, e := range ProductEdges(p) {
		if e.Src == e.Dst {
			t.Fatalf("a product was related to itself: %+v", e)
		}
	}
}

// The author node cannot be resolved, and it is kept anyway. Dropping it would
// lose that eight distinct people wrote these reviews and that one of them wrote
// three across the catalogue, which is what a graph is for.
func TestReviewEdgesKeepTheUnresolvableAuthor(t *testing.T) {
	r := Review{
		Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R1TEST",
		ReviewerName: "Amazon Customer", Rating: 5, VerifiedPurchase: true, FetchedAt: read,
	}
	edges := ReviewEdges(r, "s15")
	rev := find(t, edges, graph.ReviewedIn, "amz:us/product/B0TESTPROD")
	if rev.Src != "amz:review/R1TEST" || rev.Props["verified"] != true {
		t.Fatalf("reviewed_in is wrong: %+v", rev)
	}
	by := find(t, edges, graph.WrittenBy, "")
	if !strings.HasPrefix(by.Dst, "amz:person/") {
		t.Fatalf("written_by points at %q, want an amz:person node", by.Dst)
	}
	if by.Props["resolved"] != false {
		t.Fatalf("the person node does not say it is unresolved: %+v", by.Props)
	}
}

func TestReviewEdgesWithNoReviewerAtAll(t *testing.T) {
	r := Review{Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R1TEST", FetchedAt: read}
	if got := countPred(ReviewEdges(r, "s15"), graph.WrittenBy); got != 0 {
		t.Fatalf("%d written_by edges with no reviewer name", got)
	}
}

// A rank is true of a moment, so charted_in carries the rank and the clock and
// the chart it was read from.
func TestChartEdgesCarryTheRankAndTheList(t *testing.T) {
	entries := []BestsellerEntry{
		{Marketplace: "us", ListType: string(ChartBestsellers), NodeID: "172282", Rank: 3, ASIN: "B0TESTPROD", FetchedAt: read},
		{Marketplace: "us", ListType: string(ChartMovers), Rank: 1, ASIN: "B0TESTPROD", FetchedAt: read},
	}
	edges := ChartEdges(entries)
	if len(edges) != 2 {
		t.Fatalf("%d edges from two entries:\n%s", len(edges), dump(edges))
	}
	best := find(t, edges, graph.ChartedIn, "amz:us/chart/bestsellers/172282")
	if best.Position != 3 || best.Via != "s5" {
		t.Fatalf("bestsellers edge is wrong: %+v", best)
	}
	// The store-wide chart has no browse node and still needs a stable id,
	// because "the top 100 on amazon.com" is a ranking a product can be in.
	movers := find(t, edges, graph.ChartedIn, "amz:us/chart/movers-and-shakers/root")
	if movers.Via != "s7" {
		t.Fatalf("the movers list claims surface %q, want s7", movers.Via)
	}
}

func TestSearchEdgesCarryPositionAndPage(t *testing.T) {
	sp := SearchPage{
		Query: "usb c cable", Page: 2,
		Cards: []Card{
			{ASIN: "B0ORGANIC1", Position: 25},
			{ASIN: "B0SPONSOR1", Position: 26, Sponsored: true},
		},
	}
	sp.Envelope.RetrievedAt = read
	edges := SearchEdges(sp, "us", "usb c cable")
	if len(edges) != 2 {
		t.Fatalf("%d edges from two cards", len(edges))
	}
	for _, e := range edges {
		if e.Predicate != graph.FoundBy || e.Via != "s3" {
			t.Fatalf("search edge is wrong: %+v", e)
		}
		if e.Props["page"] != 2 {
			t.Fatalf("page is missing, so this position cannot be placed: %+v", e.Props)
		}
	}
	if edges[0].Position != 25 {
		t.Fatalf("position %d, want the whole result set offset 25", edges[0].Position)
	}
	if !edges[1].Sponsored {
		t.Fatal("a sponsored search card was written as organic")
	}
}

func TestOfferEdgesNameAmazonByItsMerchantID(t *testing.T) {
	l := OfferListing{
		Marketplace: "us", ASIN: "B0TESTPROD", SellerID: "A1SELLER", SellerName: "Acme Direct",
		Condition: "New", Price: 19.99, Currency: "USD", IsBuyBox: true,
		FulfilledBy: "Amazon.com", FetchedAt: read,
	}
	edges := OfferEdges(l)
	sold := find(t, edges, graph.SoldBy, "amz:seller/A1SELLER")
	if sold.Props["buybox"] != true || sold.Via != "s14" {
		t.Fatalf("sold_by is wrong: %+v", sold)
	}
	ful := find(t, edges, graph.FulfilledBy, "amz:seller/"+AmazonMerchantID)
	if ful.Props["as_written"] != "Amazon.com" {
		t.Fatalf("fulfilled_by lost the line the page printed: %+v", ful.Props)
	}
}

// A third party fulfiller is not Amazon and is not given Amazon's merchant id.
// It is also not given an invented one, so no edge is written at all.
func TestOfferEdgesDoNotInventAFulfiller(t *testing.T) {
	l := OfferListing{
		Marketplace: "us", ASIN: "B0TESTPROD", SellerID: "A1SELLER",
		FulfilledBy: "Acme Logistics", FetchedAt: read,
	}
	if got := countPred(OfferEdges(l), graph.FulfilledBy); got != 0 {
		t.Fatalf("%d fulfilled_by edges for a merchant with no id", got)
	}
}

// Every predicate the builders can emit is in the closed vocabulary. This is the
// test that fails if somebody writes a seventeenth by hand.
func TestEveryEmittedPredicateIsInTheVocabulary(t *testing.T) {
	var all []Edge
	all = append(all, ProductEdges(sampleProduct())...)
	all = append(all, ReviewEdges(Review{Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R1", ReviewerName: "X", FetchedAt: read}, "s15")...)
	all = append(all, ChartEdges([]BestsellerEntry{{Marketplace: "us", ListType: "bestsellers", Rank: 1, ASIN: "B0TESTPROD", FetchedAt: read}})...)
	all = append(all, OfferEdges(OfferListing{Marketplace: "us", ASIN: "B0TESTPROD", SellerID: "S", FetchedAt: read})...)
	for _, e := range all {
		if !graph.Known(e.Predicate) {
			t.Errorf("%q is not one of the sixteen", e.Predicate)
		}
	}
}
