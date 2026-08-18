package amz

import (
	"strings"
	"time"

	"github.com/tamnd/amz-cli/pkg/graph"
	"github.com/tamnd/amz-cli/pkg/uri"
)

// Edges out of the records, into the sixteen predicate vocabulary.
//
// Nothing here fetches anything. Every edge in this file is read off a record
// that has already been parsed, which is the whole economics of the graph: a
// detail page that was fetched for its price also names a brand, a seller, a
// fulfiller, a parent ASIN, six siblings, four browse nodes, three ranks, eight
// review authors and sixty related products, and all of those are free.
//
// Two rules run through all of it.
//
// An edge is only written when both ends have an identifier. A brand named in
// the byline with no store link is a real fact and it stays on the record as an
// unresolved Ref, but it is not an edge, because an edge to a node keyed on a
// display name would merge every "Amazon Basics" spelling into one brand and
// split one brand across two spellings, in the same graph, silently. The one
// exception is amz:person, which has no identifier by construction and is
// covered in section 5 of 04_graph.md.
//
// Every edge carries the surface that asserted it and the time it was read. A
// relationship on Amazon is usually temporary, so an edge without a timestamp is
// not an observation, it is a rumour.

// Edge is the graph edge type, defined once in pkg/graph and aliased here.
//
// It is an alias rather than a second struct because the store writes these and
// the traversal reads them, and two structs with the same fields would be one
// refactor away from disagreeing about which of them is authoritative.
type Edge = graph.Edge

// ProductEdges is every edge a product detail page asserts.
//
// This is the densest function in the package and it is one page's worth of
// free data.
func ProductEdges(p Product) []Edge {
	src, ok := uri.Product(p.Marketplace, p.ASIN)
	if ok != nil || p.ASIN == "" {
		return nil
	}
	at := p.FetchedAt
	if at.IsZero() {
		at = p.Envelope.RetrievedAt
	}
	if at.IsZero() {
		return nil
	}
	via := opIDFor(p.URL)
	b := &edgeBuilder{src: src, at: at, via: via}

	b.to(graph.MadeBy, p.Brand, nil)
	// The vocabulary says authored_by carries a role, and the detail page does
	// not publish one: the byline reads "Suzanne Collins" with no statement of
	// whether that is the author, the illustrator or the narrator. So the
	// property is absent rather than guessed at, and the day Amazon states it
	// there is a place for it to go.
	for i := range p.Authors {
		b.to(graph.AuthoredBy, &p.Authors[i], nil)
	}

	if p.Offer != nil {
		b.to(graph.SoldBy, p.Offer.SoldBy, offerProps(p.Offer))
		b.to(graph.FulfilledBy, p.Offer.ShipsFrom, nil)
		// A seller with a brand storefront on the same page is the one inferred
		// edge in the vocabulary, and it is marked inferred rather than being
		// left to look like something Amazon stated. Amazon never says "this
		// merchant sells this brand": what it says is that this listing has both,
		// which is weaker and is usually right.
		if p.Offer.SoldBy != nil && p.Offer.SoldBy.Resolved && p.Brand != nil && p.Brand.Resolved {
			b.edge(Edge{Src: p.Offer.SoldBy.URI, Predicate: graph.SellsUnder, Dst: p.Brand.URI,
				Props: map[string]any{"inferred": true, "from_asin": p.ASIN}})
		}
	}

	// Two predicates over browse nodes, and the difference between them is the
	// point. A breadcrumb says where the page sits, a rank line says where the
	// product competes, and the rank line is the stronger claim because Amazon
	// stated a node id rather than a name in a trail.
	// The breadcrumb is also the only place child_of comes from. A /b page links
	// its children and its siblings with identical markup and states no parent
	// at all, so a browse page cannot say which of its links go up. The trail on
	// a detail page can: it runs root first, so each entry is the parent of the
	// next one.
	for i, c := range p.Breadcrumb {
		b.to(graph.InNode, &c, map[string]any{"from": "breadcrumb"})
		if i == 0 || !c.Resolved || !p.Breadcrumb[i-1].Resolved {
			continue
		}
		b.edge(Edge{Src: c.URI, Predicate: graph.ChildOf, Dst: p.Breadcrumb[i-1].URI,
			Props: map[string]any{"from": "breadcrumb", "name": c.Name}})
	}
	for _, r := range p.Ranks {
		b.to(graph.InNode, r.Node, map[string]any{"from": "rank"})
		if r.Node != nil && r.Node.Resolved {
			b.edge(Edge{Src: src, Predicate: graph.RankedIn, Dst: r.Node.URI, Position: r.Rank,
				Props: map[string]any{"rank": r.Rank, "category": r.Category, "overall": r.Overall}})
		}
	}

	// The variation family. parent_of is stored once and traversed both ways,
	// which is why the siblings hang off the parent and not off each other: a
	// listing with twelve colours would otherwise be 132 edges instead of 12.
	if v := p.Variation; v != nil {
		parent := v.ParentASIN
		if parent == "" {
			parent = p.ParentASIN
		}
		if pu, err := uri.Product(p.Marketplace, parent); err == nil && parent != p.ASIN {
			b.edge(Edge{Src: pu, Predicate: graph.ParentOf, Dst: src, Props: map[string]any{"values": v.Current}})
			for _, s := range v.Siblings {
				su, err := uri.Product(p.Marketplace, s.ASIN)
				if err != nil || s.ASIN == parent {
					continue
				}
				b.edge(Edge{Src: pu, Predicate: graph.ParentOf, Dst: su, Props: map[string]any{"values": s.Values}})
			}
		} else {
			// No parent ASIN, which happens on listings whose twister ships the
			// siblings without naming the family. variant_of is symmetric, so
			// one edge per sibling from this product is enough to reach them.
			for _, s := range v.Siblings {
				su, err := uri.Product(p.Marketplace, s.ASIN)
				if err != nil || s.ASIN == p.ASIN {
					continue
				}
				b.edge(Edge{Src: src, Predicate: graph.VariantOf, Dst: su, Props: map[string]any{"values": s.Values}})
			}
		}
	}

	// The rails. Sponsored is carried on the edge rather than filtered here,
	// because a crawl that dropped the ads would leave a dataset that cannot
	// answer "how much of this page was advertising", and that is a question
	// worth being able to ask.
	for _, rail := range p.Rails {
		for _, card := range rail.Cards {
			du, err := uri.Product(p.Marketplace, card.ASIN)
			if err != nil || card.ASIN == p.ASIN {
				continue
			}
			b.edge(Edge{Src: src, Predicate: graph.RelatedTo, Dst: du,
				Sponsored: rail.Sponsored || card.Sponsored, Position: card.Position,
				Props: map[string]any{"region": rail.Region, "title": rail.Title}})
		}
	}
	for _, a := range p.SimilarASINs {
		du, err := uri.Product(p.Marketplace, a)
		if err != nil || a == p.ASIN {
			continue
		}
		b.edge(Edge{Src: src, Predicate: graph.RelatedTo, Dst: du, Props: map[string]any{"region": "similar"}})
	}

	for _, r := range p.ReviewSample {
		b.append(ReviewEdges(r, via)...)
	}
	return b.out
}

// ReviewEdges is the two edges a review asserts: what it is about and who wrote
// it.
//
// written_by is the one edge in the vocabulary whose destination can never be
// resolved, because /gp/profile/ is disallowed and amz does not fetch it. The
// node is kept anyway. Dropping it loses the fact that eight distinct people
// wrote these reviews and that one of them wrote three across the catalogue,
// which is exactly what a graph is for.
func ReviewEdges(r Review, via string) []Edge {
	at := r.FetchedAt
	if at.IsZero() {
		return nil
	}
	ru, err := uri.New(uri.KindReview, "", r.ReviewID)
	if err != nil {
		return nil
	}
	b := &edgeBuilder{src: ru, at: at, via: via}
	if pu, err := uri.Product(r.Marketplace, r.ASIN); err == nil {
		b.edge(Edge{Src: ru, Predicate: graph.ReviewedIn, Dst: pu,
			Props: map[string]any{"rating": r.Rating, "verified": r.VerifiedPurchase}})
	}
	author := r.Author
	if author == nil {
		author = PersonRef(r.ReviewerName)
	}
	if author != nil && author.URI != "" {
		b.edge(Edge{Src: ru, Predicate: graph.WrittenBy, Dst: author.URI,
			Props: map[string]any{"name": author.Name, "resolved": false}})
	}
	return b.out
}

// CategoryEdges is what a browse page can honestly assert, which is less than it
// looks like.
//
// It asserts in_node for every product it merchandised, and nothing at all for
// the nodes it links to. A /b page links its children and its siblings with
// identical markup and states no parent, so "this node links that node" is the
// only true reading, and there is no predicate for that on purpose. Writing
// child_of off a Related list would build a hierarchy out of a guess, and the
// hierarchy would be wrong in whichever direction the guess went. The real
// child_of edges come from a product's breadcrumb, which is ordered.
func CategoryEdges(c Category) []Edge {
	mkt := c.Envelope.Marketplace
	node := firstNonEmpty(c.CanonicalNode, c.NodeID)
	src, err := uri.Node(mkt, node)
	if err != nil {
		return nil
	}
	at := c.FetchedAt
	if at.IsZero() {
		at = c.Envelope.RetrievedAt
	}
	if at.IsZero() {
		return nil
	}
	b := &edgeBuilder{src: src, at: at, via: opIDFor(firstNonEmpty(c.CanonicalURL, c.URL))}
	for _, a := range c.TopASINs {
		if pu, err := uri.Product(mkt, a); err == nil {
			b.edge(Edge{Src: pu, Predicate: graph.InNode, Dst: src, Props: map[string]any{"from": "browse"}})
		}
	}
	return b.out
}

// ChartEdges is charted_in, which is an edge and not a field.
//
// This is the single most useful thing the store does that a one shot command
// cannot. A product that was number 3 in Electronics last Tuesday still was, and
// a field called rank overwrites that fact every time the crawler runs. The edge
// carries the rank and the read time, and the store's chart_entry table is
// append only underneath it.
func ChartEdges(entries []BestsellerEntry) []Edge {
	var out []Edge
	for _, e := range entries {
		src, err := uri.Product(e.Marketplace, e.ASIN)
		if err != nil {
			continue
		}
		dst, err := uri.Chart(e.Marketplace, e.ListType, chartNodeID(e.NodeID))
		if err != nil {
			continue
		}
		at := e.FetchedAt
		if at.IsZero() {
			continue
		}
		out = append(out, Edge{Src: src, Predicate: graph.ChartedIn, Dst: dst,
			Via: chartOp(e.ListType), Position: e.Rank, ObservedAt: at,
			Props: map[string]any{"rank": e.Rank, "category": e.Category, "list": e.ListType}})
	}
	return out
}

// chartNodeID is the node half of a chart URI.
//
// The store-wide chart has no browse node, and it still needs a stable id
// because "the top 100 on amazon.com" is a real ranking that a product can be
// charted in. "root" is that id, and it is a reserved word rather than a node
// number because Amazon has no node that means the whole store.
func chartNodeID(id string) string {
	if id == "" {
		return "root"
	}
	return id
}

// chartOp maps a chart kind to the surface that serves it, so an edge off the
// movers list does not claim it came from the bestsellers page.
func chartOp(list string) string {
	switch ChartKind(list) {
	case ChartBestsellers:
		return "s5"
	case ChartNewReleases:
		return "s6"
	case ChartMovers:
		return "s7"
	case ChartWished:
		return "s8"
	case ChartGifted:
		return "s9"
	}
	return ""
}

// SearchEdges is found_by: this query returned this product, at this position,
// on this page, at this time.
//
// Position and page are both carried because they are different facts. Position
// is where in the whole result set the product sat, page is which request found
// it, and a search whose page size changes from sixteen to twenty-four when a
// department is applied makes the second underivable from the first.
// The key is passed in rather than derived here, because normalisation is
// specified once in 07_search.md section 3 and implemented once in SearchKey.
// Recomputing it from the page would mean a second implementation reading the
// echoed query and the applied refinements, and the day the two disagreed the
// store would hold the same search under two ids.
func SearchEdges(sp SearchPage, marketplace, key string) []Edge {
	dst, err := uri.Search(marketplace, key)
	if err != nil {
		return nil
	}
	at := sp.Envelope.RetrievedAt
	if at.IsZero() {
		return nil
	}
	var out []Edge
	for _, card := range sp.Cards {
		src, err := uri.Product(marketplace, card.ASIN)
		if err != nil {
			continue
		}
		out = append(out, Edge{Src: src, Predicate: graph.FoundBy, Dst: dst, Via: "s3",
			Sponsored: card.Sponsored, Position: card.Position, ObservedAt: at,
			Props: map[string]any{"query": sp.Query, "page": sp.Page}})
	}
	return out
}

// OfferEdges is sold_by and fulfilled_by off one row of the offer listing page.
//
// fulfilled_by points at a seller when the fulfiller is a merchant and at the
// literal amz:seller/ATVPDKIKX0DER when it is Amazon, because that is Amazon's
// own merchant id and using it keeps the destination a real node instead of a
// string that means "Amazon, probably".
func OfferEdges(l OfferListing) []Edge {
	src, err := uri.Product(l.Marketplace, l.ASIN)
	if err != nil || l.FetchedAt.IsZero() {
		return nil
	}
	b := &edgeBuilder{src: src, at: l.FetchedAt, via: "s14"}
	if su, err := uri.Seller(l.SellerID); err == nil {
		b.edge(Edge{Src: src, Predicate: graph.SoldBy, Dst: su,
			Props: map[string]any{"condition": l.Condition, "price": l.Price,
				"currency": l.Currency, "buybox": l.IsBuyBox, "seller_name": l.SellerName}})
	}
	if f := fulfillerURI(l.FulfilledBy); f != "" {
		b.edge(Edge{Src: src, Predicate: graph.FulfilledBy, Dst: f,
			Props: map[string]any{"as_written": l.FulfilledBy}})
	}
	return b.out
}

// AmazonMerchantID is Amazon's own merchant id, which is the same string in
// every storefront and is the destination of a fulfilled_by edge whenever the
// page says Amazon rather than naming a third party.
const AmazonMerchantID = "ATVPDKIKX0DER"

func fulfillerURI(s string) string {
	switch {
	case s == "":
		return ""
	case strings.EqualFold(s, "amazon"), containsFold(s, "amazon.com"), containsFold(s, "fulfilled by amazon"):
		u, _ := uri.Seller(AmazonMerchantID)
		return u
	}
	return ""
}

// edgeBuilder fills in the src, the surface and the read time so that no
// individual edge above has to remember all three. Forgetting the timestamp is
// the failure this exists to prevent: Edge.Valid rejects it, but at run time and
// one edge at a time, which is a worse place to find out.
type edgeBuilder struct {
	src string
	at  time.Time
	via string
	out []Edge
}

func (b *edgeBuilder) edge(e Edge) {
	if e.Src == "" {
		e.Src = b.src
	}
	if e.Via == "" {
		e.Via = b.via
	}
	if e.ObservedAt.IsZero() {
		e.ObservedAt = b.at
	}
	if e.Dst == "" || e.Src == e.Dst {
		return
	}
	b.out = append(b.out, e)
}

// to writes an edge to a Ref, and does nothing when the Ref has no identifier.
//
// A Ref that is not resolved is a name with no id, and a graph node keyed on a
// display name is worse than no node at all. The name is not lost: it is still
// on the record.
func (b *edgeBuilder) to(predicate string, r *Ref, props map[string]any) {
	if r == nil || !r.Resolved || r.URI == "" {
		return
	}
	if props == nil {
		props = map[string]any{}
	}
	if r.Name != "" {
		props["name"] = r.Name
	}
	b.edge(Edge{Predicate: predicate, Dst: r.URI, Props: props})
}

func (b *edgeBuilder) append(es ...Edge) { b.out = append(b.out, es...) }

func offerProps(o *Offer) map[string]any {
	m := map[string]any{}
	if o.Price != nil {
		m["price"] = o.Price.Amount
		m["currency"] = o.Price.Currency
	}
	if o.Condition != "" {
		m["condition"] = o.Condition
	}
	if o.Prime != nil {
		m["prime"] = *o.Prime
	}
	return m
}

// opIDFor is the surface that served a URL, as an id, or "" when the URL is not
// one this tool recognises.
func opIDFor(rawURL string) string {
	if op := OpFor(rawURL); op != nil {
		return op.ID
	}
	return ""
}
