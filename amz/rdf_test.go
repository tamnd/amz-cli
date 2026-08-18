package amz

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/amz-cli/pkg/graph"
	"github.com/tamnd/amz-cli/pkg/rdf"
)

// nt renders a graph as n-triples, which is what these tests assert against.
// One triple per line, fully expanded, no prefixes and no nesting, so a missing
// statement is a missing line rather than a shape somebody has to read.
func nt(t *testing.T, g *rdf.Graph) string {
	t.Helper()
	var b bytes.Buffer
	if err := g.WriteNTriples(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func mustHave(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("missing triple containing:\n  %s\ngot:\n%s", want, got)
	}
}

func mustNotHave(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("output contains %q and should not:\n%s", want, got)
	}
}

func exportProduct(t *testing.T, p Product, opt ExportOpts) string {
	t.Helper()
	g := rdf.NewGraph()
	ProductTriples(g, p, opt)
	return nt(t, g)
}

// Every one of the sixteen has a term, so no edge is silently dropped on export.
func TestEverySixteenPredicateMapsToATerm(t *testing.T) {
	for _, p := range graph.Predicates() {
		term, ok := EdgeTerm(Edge{Predicate: p.Name})
		if !ok || term == "" {
			t.Errorf("%s has no RDF term, so it would vanish from an export", p.Name)
		}
	}
	if _, ok := EdgeTerm(Edge{Predicate: "invented"}); ok {
		t.Error("a predicate outside the vocabulary was given a term")
	}
}

// An organic recommendation and a paid one are different data, and they get
// different predicates so a consumer that ignores every property still cannot
// mix them.
func TestSponsoredAndOrganicNeverShareAPredicate(t *testing.T) {
	organic, _ := EdgeTerm(Edge{Predicate: graph.RelatedTo})
	paid, _ := EdgeTerm(Edge{Predicate: graph.RelatedTo, Sponsored: true})
	if organic == paid {
		t.Fatalf("both export as %s", organic)
	}
	if paid != SponsoredPredicate {
		t.Fatalf("a paid placement exports as %s, want %s", paid, SponsoredPredicate)
	}
	for _, term := range edgePredicate {
		if term == SponsoredPredicate {
			t.Fatal("an organic predicate is mapped to the sponsored term")
		}
	}
}

func TestEdgeTriplesRespectTheSponsoredDefault(t *testing.T) {
	e := Edge{Src: "amz:us/product/B01", Predicate: graph.RelatedTo, Dst: "amz:us/product/B02",
		Sponsored: true, ObservedAt: read}

	g := rdf.NewGraph()
	EdgeTriples(g, e, ExportOpts{})
	if g.Len() != 0 {
		t.Fatalf("a paid placement was exported without --include-sponsored:\n%s", nt(t, g))
	}

	g = rdf.NewGraph()
	EdgeTriples(g, e, ExportOpts{IncludeSponsored: true})
	mustHave(t, nt(t, g), "<https://amz-cli.tamnd.com/v#sponsoredPlacement>")
}

// A price without a timestamp is not a fact. amzv:retrievedAt is on every offer,
// without exception and without a flag.
func TestEveryOfferCarriesRetrievedAt(t *testing.T) {
	p := sampleProduct()
	p.Offer = &Offer{Price: &Money{Display: "$19.99", Currency: "USD", Value: 19.99}, Condition: "New"}
	for _, opt := range []ExportOpts{{}, {WithText: true}, {IncludeSponsored: true}} {
		got := exportProduct(t, p, opt)
		mustHave(t, got, "<https://amz-cli.tamnd.com/v#retrievedAt> \"2026-08-17T12:00:00Z\"^^<http://www.w3.org/2001/XMLSchema#dateTime>")
		mustHave(t, got, `<https://schema.org/price> "$19.99"`)
	}
}

// The bucket counts are reconstructed from integer percentages, and a consumer
// has to be able to see that. A field only present when it is false is a field
// nobody checks for, so it is always written.
func TestDistributionIsAlwaysMarkedDerived(t *testing.T) {
	p := sampleProduct()
	total := int64(1200)
	p.Distribution = NewDistribution([5]int{2, 3, 5, 20, 70}, &total, "s1")
	got := exportProduct(t, p, ExportOpts{})
	mustHave(t, got, "<https://amz-cli.tamnd.com/v#distributionDerived>")
	// One value and not five triples, so nobody can query the five star bucket
	// without the total and quote a reconstructed number as a count.
	mustHave(t, got, `<https://amz-cli.tamnd.com/v#ratingDistribution> "2,3,5,20,70"`)
	if strings.Count(got, "ratingDistribution") != 1 {
		t.Fatalf("the histogram went out as more than one statement:\n%s", got)
	}
}

func TestNoHistogramMeansNoDerivedFlag(t *testing.T) {
	p := sampleProduct()
	p.Distribution = nil
	mustNotHave(t, exportProduct(t, p, ExportOpts{}), "distributionDerived")
}

// Text is opt in, because a local store of prices is a person's own measurements
// and a local store of Amazon's prose is a copy of Amazon's prose.
func TestTextIsAbsentWithoutWithText(t *testing.T) {
	p := sampleProduct()
	p.Description = "Three paragraphs of marketing copy."
	p.ReviewSample = []Review{{
		Marketplace: "us", ASIN: p.ASIN, ReviewID: "R1TEST", Title: "Good",
		Text: "It arrived on a Tuesday.", Rating: 5, FetchedAt: read,
	}}

	off := exportProduct(t, p, ExportOpts{})
	mustNotHave(t, off, "Three paragraphs")
	mustNotHave(t, off, "arrived on a Tuesday")
	// The record is still there, only its prose is not.
	mustHave(t, off, `<https://schema.org/name> "Good"`)

	on := exportProduct(t, p, ExportOpts{WithText: true})
	mustHave(t, on, `<https://schema.org/description> "Three paragraphs of marketing copy."`)
	mustHave(t, on, `<https://schema.org/reviewBody> "It arrived on a Tuesday."`)
}

// The author node is kept and it says it cannot be resolved, because this
// identity is a hash of a display name and two people called "Amazon Customer"
// are one node here.
func TestDanglingPersonNodeIsKeptAndSaysSo(t *testing.T) {
	g := rdf.NewGraph()
	ReviewTriples(g, Review{
		Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R1TEST",
		ReviewerName: "Amazon Customer", Rating: 4, FetchedAt: read,
	}, ExportOpts{})
	got := nt(t, g)
	mustHave(t, got, "<amz:person/")
	mustHave(t, got, "<https://schema.org/Person>")
	mustHave(t, got, `<https://amz-cli.tamnd.com/v#resolved> "false"^^<http://www.w3.org/2001/XMLSchema#boolean>`)
	mustHave(t, got, `<https://schema.org/name> "Amazon Customer"`)
}

// A failed date parse must not become a zero time, because a zero time is a real
// date in January of year 1 and everything downstream treats it as one.
func TestReviewDateKeepsTheLineAmazonWrote(t *testing.T) {
	parsed := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	g := rdf.NewGraph()
	ReviewTriples(g, Review{
		Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R1", FetchedAt: read,
		Date: &Date{Raw: "Reviewed in the United States on March 14, 2025", Parsed: &parsed},
	}, ExportOpts{})
	got := nt(t, g)
	mustHave(t, got, `<https://schema.org/datePublished> "2025-03-14"^^<http://www.w3.org/2001/XMLSchema#date>`)
	mustHave(t, got, "<https://amz-cli.tamnd.com/v#datePublishedText>")

	g = rdf.NewGraph()
	ReviewTriples(g, Review{
		Marketplace: "us", ASIN: "B0TESTPROD", ReviewID: "R2", FetchedAt: read,
		Date: &Date{Raw: "on a day the parser did not recognise"},
	}, ExportOpts{})
	unparsed := nt(t, g)
	mustNotHave(t, unparsed, "datePublished>")
	mustNotHave(t, unparsed, "0001-01-01")
	mustHave(t, unparsed, "datePublishedText")
}

func TestProductCoreFields(t *testing.T) {
	p := sampleProduct()
	p.UPC = "012345678905"
	p.ModelNumber = "ACM-1"
	rating := 4.5
	ratings := int64(1200)
	p.Rating, p.RatingsCount = &rating, &ratings
	p.Details = map[string]string{"Batteries": "2 AA", "Colour": "Black"}

	got := exportProduct(t, p, ExportOpts{})
	mustHave(t, got, "<amz:us/product/B0TESTPROD> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://schema.org/Product>")
	mustHave(t, got, `<https://schema.org/sku> "B0TESTPROD"`)
	mustHave(t, got, `<https://schema.org/gtin12> "012345678905"`)
	mustHave(t, got, `<https://schema.org/model> "ACM-1"`)
	mustHave(t, got, "<https://schema.org/brand> <amz:brand/acme>")
	mustHave(t, got, `<https://schema.org/ratingValue> "4.5"`)
	mustHave(t, got, `<https://schema.org/ratingCount> "1200"^^<http://www.w3.org/2001/XMLSchema#integer>`)
	// A detail row is a blank node, not an invented predicate. Amazon's detail
	// table is open ended and localised, so amzv:<whatever the page said> would
	// put page text into the schema.
	mustHave(t, got, "<https://amz-cli.tamnd.com/v#detail>")
	mustHave(t, got, `<https://schema.org/value> "2 AA"`)
	mustNotHave(t, got, "v#Batteries")
}

// An unresolved brand keeps its name as a literal rather than becoming an IRI
// built from a display name.
func TestUnresolvedBrandStaysALiteral(t *testing.T) {
	p := sampleProduct()
	p.Brand = &Ref{Kind: RefBrand, Name: "Acme"}
	got := exportProduct(t, p, ExportOpts{})
	mustHave(t, got, `<https://schema.org/brand> "Acme"`)
	mustNotHave(t, got, "<amz:brand/")
}

// Availability is a schema.org enumeration member and the sentence Amazon
// printed is kept beside it, because the derivation is a guess about phrasing in
// sixteen locales and the string is not.
func TestAvailabilityKeepsBothTheTermAndTheSentence(t *testing.T) {
	in := true
	p := sampleProduct()
	p.Offer = &Offer{InStock: &in, Availability: "In Stock. Usually ships within 24 hours."}
	got := exportProduct(t, p, ExportOpts{})
	mustHave(t, got, "<https://schema.org/availability> <https://schema.org/InStock>")
	mustHave(t, got, "<https://amz-cli.tamnd.com/v#availabilityText>")

	out := false
	p.Offer = &Offer{InStock: &out, Availability: "Currently unavailable."}
	mustHave(t, exportProduct(t, p, ExportOpts{}), "<https://schema.org/availability> <https://schema.org/OutOfStock>")
}

// An export is byte identical across runs, because a blank node label is derived
// from the thing it describes rather than from a counter. An export nobody can
// diff is an export nobody trusts.
func TestExportIsStableAcrossRuns(t *testing.T) {
	p := sampleProduct()
	p.Details = map[string]string{"z": "1", "a": "2", "m": "3"}
	if a, b := exportProduct(t, p, ExportOpts{}), exportProduct(t, p, ExportOpts{}); a != b {
		t.Fatalf("two exports of the same product differ:\n%s\n---\n%s", a, b)
	}
}

// Without this an export is a pile of edges pointing at IRIs with no type, which
// is valid RDF and useless to load into anything.
func TestNodeTriplesTypeTheBareNodes(t *testing.T) {
	cases := map[string]string{
		"amz:us/product/B0TESTPROD":       "<https://schema.org/Product>",
		"amz:us/node/172282":              "<https://schema.org/CategoryCode>",
		"amz:seller/A1SELLER":             "<https://schema.org/Organization>",
		"amz:brand/acme":                  "<https://schema.org/Organization>",
		"amz:person/abc123":               "<https://schema.org/Person>",
		"amz:review/R1TEST":               "<https://schema.org/Review>",
		"amz:us/chart/bestsellers/172282": "<https://schema.org/ItemList>",
		"amz:us/search/abc123":            "<https://schema.org/SearchAction>",
	}
	for node, want := range cases {
		g := rdf.NewGraph()
		NodeTriples(g, node)
		got := nt(t, g)
		mustHave(t, got, want)
		if strings.HasPrefix(node, "amz:us/") {
			mustHave(t, got, `<https://amz-cli.tamnd.com/v#marketplace> "us"`)
		}
	}

	g := rdf.NewGraph()
	NodeTriples(g, "not an amz uri")
	if g.Len() != 0 {
		t.Fatalf("an unparseable node was typed anyway:\n%s", nt(t, g))
	}
}

// A chart node says which of the five rankings it is, because bestsellers and
// movers over the same browse node are two different orderings.
func TestChartNodeNamesItsKind(t *testing.T) {
	g := rdf.NewGraph()
	NodeTriples(g, "amz:us/chart/bestsellers/172282")
	mustHave(t, nt(t, g), `<https://amz-cli.tamnd.com/v#chartKind> "bestsellers"`)
}
