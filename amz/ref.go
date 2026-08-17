package amz

// The entity records each know how to point at themselves.
//
// Every record already carries the pieces of its own identity: an id, a name and
// a URL. What it did not carry was one agreed way to hand those to something
// else. A product names its brand as a Ref and the brand record answered with a
// slug and a page id, so the two halves of the same edge were built by different
// code and only matched by luck.
//
// These methods are that one way. The claim graph and the RDF export walk
// records and ask each one what it is, rather than rebuilding a URI per edge
// from whichever fields happened to be nearby.
//
// The marketplace comes from the envelope, which the fetch stamps. A Ref built
// from a record that was never fetched, a hand-built literal in a test, has no
// marketplace to scope its URI to, and it says so by coming back unresolved
// instead of inventing "amz:/product/B075F5X8BR".

// Ref points at this product.
func (p Product) Ref() *Ref {
	return NewRef(RefProduct, p.Marketplace, p.ASIN, p.Title, p.URL)
}

// Ref points at this browse node.
//
// The canonical node wins over the one that was asked for, because Amazon
// redirects and the record filed under the requested node would be filed under a
// node this page is not.
func (c Category) Ref() *Ref {
	return NewRef(RefNode, c.Envelope.Marketplace, firstNonEmpty(c.CanonicalNode, c.NodeID), c.Name, firstNonEmpty(c.CanonicalURL, c.URL))
}

// Ref points at this seller.
func (s Seller) Ref() *Ref {
	return NewRef(RefSeller, s.Envelope.Marketplace, s.SellerID, s.Name, s.URL)
}

// Ref points at this brand storefront.
//
// The slug is the identifier and not the page id. A storefront is seven pages
// and each carries its own uuid, so a Ref keyed on the page id would give the
// same brand seven identities and the graph would hold seven brands.
func (b Brand) Ref() *Ref {
	return NewRef(RefBrand, b.Envelope.Marketplace, b.Slug, b.Name, firstNonEmpty(b.CanonicalURL, b.URL))
}

// Ref points at this author, keyed on the slug for the same reason as a brand.
func (a Author) Ref() *Ref {
	return NewRef(RefAuthor, a.Envelope.Marketplace, a.Slug, a.Name, a.URL)
}

// resolveRankNodes scopes the browse node on each rank line to a marketplace.
//
// The rule that read the lines has the id and the name and no marketplace, so it
// leaves the reference unresolved and this finishes it, the same way the
// breadcrumb is finished. A rank in a subcategory is the strongest category edge
// on a detail page, because Amazon stated the identifier rather than leaving a
// name to be matched against a browse tree.
func resolveRankNodes(ranks []Rank, mkt, base string) {
	for i := range ranks {
		n := ranks[i].Node
		if n == nil || n.Resolved {
			continue
		}
		ranks[i].Node = NewRef(RefNode, mkt, n.ID, n.Name, base+"/b?node="+n.ID)
	}
}
