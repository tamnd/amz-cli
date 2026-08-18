package amz

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/amz-cli/pkg/asin"
	"github.com/tamnd/amz-cli/pkg/uri"
)

// TestNoUnscopedProductURI walks everything a real page produces and asserts
// that no product or browse node URI came out without a marketplace on it.
//
// This is the test notes/Spec/3007/04_graph.md section 1 asks for, and it is
// written against a fetched page rather than against the constructor because the
// constructor is already covered in pkg/uri. What is worth checking here is the
// wiring: sixty rail cards, four breadcrumbs, three rank lines and a variation
// family all build references, and any one of them could have been given the id
// without the storefront.
func TestNoUnscopedProductURI(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	ctx := context.Background()

	p, err := c.FetchProductDepth(ctx, "B084DWG2VQ", DepthFull)
	if err != nil {
		t.Fatal(err)
	}

	seen := 0
	check := func(where string, r *Ref) {
		if r == nil || r.URI == "" {
			return
		}
		seen++
		ref, err := uri.Parse(r.URI)
		if err != nil {
			t.Errorf("%s: %q does not parse: %v", where, r.URI, err)
			return
		}
		if ref.Kind != r.Kind {
			t.Errorf("%s: %q is a %s but the reference says %s", where, r.URI, ref.Kind, r.Kind)
		}
		if uri.Scoped(r.Kind) && ref.Marketplace == "" {
			t.Errorf("%s: %q has no marketplace, so it merges every storefront", where, r.URI)
		}
	}

	check("product", p.Ref())
	check("brand", p.Brand)
	if p.Offer != nil {
		check("sold_by", p.Offer.SoldBy)
	}
	for i := range p.Breadcrumb {
		check("breadcrumb", &p.Breadcrumb[i])
	}
	for i := range p.Authors {
		check("author", &p.Authors[i])
	}
	for i := range p.Ranks {
		check("rank", p.Ranks[i].Node)
	}
	for _, rail := range p.Rails {
		for _, card := range rail.Cards {
			if card.ASIN == "" {
				continue
			}
			u, err := uri.Product(p.Marketplace, card.ASIN)
			if err != nil {
				t.Errorf("rail card %s: %v", card.ASIN, err)
				continue
			}
			seen++
			if !strings.HasPrefix(u, "amz:"+p.Marketplace+"/") {
				t.Errorf("rail card %s: %q", card.ASIN, u)
			}
		}
	}
	if seen == 0 {
		t.Fatal("the fixture produced no identifiers, so this test asserted nothing")
	}

	// And the other half of the rule: a record nobody fetched has no storefront
	// to scope to, and it comes back unresolved rather than inventing one.
	hand := (Product{ASIN: "B075F5X8BR", Title: "hand built"}).Ref()
	if hand == nil {
		t.Fatal("a record with an id and a name still points at something")
	}
	if hand.URI != "" || hand.Resolved {
		t.Errorf("a product with no marketplace resolved anyway: %+v", hand)
	}
}

// A review author is a node with a URI and no resolution, which is the one place
// those two disagree. The name is hashed so the graph has something to hang an
// edge on, and the reference stays unresolved because the hash is not an id
// anybody at Amazon issued.
func TestReviewAuthorIsANodeButNotResolved(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()

	var first *Review
	err := c.FetchReviews(context.Background(), "B084DWG2VQ", ReviewQuery{Limit: 1}, func(r Review) error {
		first = &r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.Author == nil {
		t.Fatal("the fixture produced no reviewer to test")
	}
	if first.Author.Resolved {
		t.Errorf("a review author resolved: %+v", first.Author)
	}
	if !strings.HasPrefix(first.Author.URI, "amz:person/") {
		t.Errorf("author uri = %q, want an amz:person node", first.Author.URI)
	}
	if first.Author.Name == "" {
		t.Errorf("the name was dropped: %+v", first.Author)
	}
	// The same name always gives the same node, or the graph gains a person per
	// review and the edges point at nothing twice.
	again := PersonRef(first.ReviewerName)
	if again.URI != first.Author.URI {
		t.Errorf("the same reviewer hashed to %q and %q", first.Author.URI, again.URI)
	}
}

// The marketplace registry and the host table in pkg/asin are two lists of the
// same storefronts, and neither can import the other without inverting the
// dependency. So they are pinned together here: the day one gains a marketplace
// and the other does not, this fails instead of a .pl URL silently reading as US
// dollars.
func TestHostTablesAgree(t *testing.T) {
	fromParser := asin.Hosts()
	for _, m := range Marketplaces() {
		host := strings.TrimPrefix(m.Host, "www.")
		slug, ok := fromParser[host]
		if !ok {
			t.Errorf("%s (%s) is a marketplace the id parser does not know", m.Slug, m.Host)
			continue
		}
		if slug != m.Slug {
			t.Errorf("%s maps to %q in the parser and %q in the registry", host, slug, m.Slug)
		}
		delete(fromParser, host)
	}
	for host, slug := range fromParser {
		t.Errorf("the parser knows %s as %q, which is not a marketplace amz supports", host, slug)
	}
}

// The store key is (marketplace, asin), so the same id from two storefronts is
// two rows. Under the old schema, which keyed on the ASIN alone, the second
// write replaced the first and a crawl of both sites reported one product at
// whichever price arrived last.
func TestStoreKeepsMarketplacesApart(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "amz.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, mkt := range []string{"us", "uk"} {
		p := Product{
			ASIN:        "B075F5X8BR",
			Marketplace: mkt,
			Title:       "the same listing in " + mkt,
			FetchedAt:   time.Unix(0, 0).UTC(),
		}
		if err := s.PutProduct(ctx, p); err != nil {
			t.Fatalf("%s: %v", mkt, err)
		}
	}

	rows, err := s.Query(ctx, "SELECT marketplace, uri FROM product WHERE asin='B075F5X8BR' ORDER BY marketplace")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows for one ASIN in two storefronts, want 2: %v", len(rows), rows)
	}
	if got := fmt.Sprint(rows[0]["uri"]); got != "amz:uk/product/B075F5X8BR" {
		t.Errorf("uk uri = %q", got)
	}
	if got := fmt.Sprint(rows[1]["uri"]); got != "amz:us/product/B075F5X8BR" {
		t.Errorf("us uri = %q", got)
	}

	// A record that cannot say which storefront it came from is refused rather
	// than filed under the empty marketplace, where nothing would ever find it
	// and no later crawl would overwrite it.
	err = s.PutProduct(ctx, Product{ASIN: "B075F5X8BR", Title: "no storefront"})
	if !errors.Is(err, ErrUnscoped) {
		t.Errorf("err = %v, want ErrUnscoped", err)
	}
}
