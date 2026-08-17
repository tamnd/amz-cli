package amz

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The node links in the header and the footer of amazon.com look exactly like
// the node links in the page, and eight of the twenty four on the Electronics
// capture are chrome. A record that listed Gift Cards and Shop with Points as
// categories related to Electronics would be wrong in a way that survives every
// count based check, because the count is right and the rows are junk.
func TestRelatedNodesSkipTheSiteChrome(t *testing.T) {
	body, err := readCapture(filepath.Join(capturesDir, "browse_node_child.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(Config{})
	bp, err := c.parseBrowsePage("13896617011", "https://www.amazon.com/b?node=13896617011", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(bp.Related) == 0 {
		t.Fatal("the computers capture links a dozen sibling nodes and none came through")
	}

	byName := map[string]bool{}
	for _, r := range bp.Related {
		byName[r.Name] = true
		if r.Kind != RefNode {
			t.Errorf("related node %+v is not a node reference", r)
		}
		if !r.Resolved || r.ID == "" {
			t.Errorf("related node %+v has no identifier, so nothing can follow it", r)
		}
		if !strings.HasPrefix(r.URI, "amz:") {
			t.Errorf("related node %+v carries no marketplace scoped URI", r)
		}
	}
	// Measured on the capture of 2026-08-17: these five sit in the global header
	// and footer on every page amazon.com serves.
	for _, chrome := range []string{"Gift Cards", "Sell", "Registry", "Shop with Points", "Amazon Currency Converter"} {
		if byName[chrome] {
			t.Errorf("%q is site chrome and was reported as a related category", chrome)
		}
	}
	// And these are the page's own siblings, which is what the field is for.
	for _, want := range []string{"Laptops", "Monitors", "Printers & Ink"} {
		if !byName[want] {
			t.Errorf("%q is on the page and is missing from the related nodes", want)
		}
	}
}

// Every entity record can point at itself the same way, which is what the graph
// and the RDF export are going to walk.
func TestRecordsPointAtThemselves(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	ctx := context.Background()

	p, err := c.FetchProduct(ctx, "B084DWG2VQ")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := c.FetchCategory(ctx, "172282")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.FetchSeller(ctx, "A9RATEDSELLER1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.FetchBrand(ctx, "anker")
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.FetchAuthor(ctx, "brandon-sanderson")
	if err != nil {
		t.Fatal(err)
	}

	for name, ref := range map[string]*Ref{
		"product":  p.Ref(),
		"category": cat.Ref(),
		"seller":   s.Ref(),
		"brand":    b.Ref(),
		"author":   a.Ref(),
	} {
		if ref == nil {
			t.Errorf("%s: a fetched record cannot say what it is", name)
			continue
		}
		if !ref.Resolved {
			t.Errorf("%s: %+v is unresolved, so nothing can be filed under it", name, ref)
		}
		// The marketplace is on the envelope because the fetch put it there. A
		// URI without it would collide amazon.com with amazon.co.uk, which
		// publish different prices for the same identifier.
		if !strings.HasPrefix(ref.URI, "amz:us/") {
			t.Errorf("%s: uri = %q, want it scoped to the marketplace", name, ref.URI)
		}
		if ref.Name == "" || ref.URL == "" {
			t.Errorf("%s: %+v is missing the name or the URL", name, ref)
		}
	}

	// A record nobody fetched has no marketplace to scope a URI to, and it says
	// so rather than making one up. The same ASIN is a different product at a
	// different price in every marketplace, so an unscoped URI is not a shorter
	// identifier, it is one that merges two things.
	hand := (Category{NodeID: "172282", Name: "Electronics"}).Ref()
	if hand == nil {
		t.Fatal("a record with an id and a name still points at something")
	}
	if hand.URI != "" || hand.Resolved {
		t.Errorf("a record with no marketplace resolved anyway: %+v", hand)
	}
	if hand.ID != "172282" || hand.Name != "Electronics" {
		t.Errorf("an unresolved reference keeps what it does know: %+v", hand)
	}
}
