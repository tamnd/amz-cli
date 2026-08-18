package amz

import (
	"strconv"
	"time"

	"github.com/tamnd/amz-cli/pkg/graph"
	"github.com/tamnd/amz-cli/pkg/rdf"
	"github.com/tamnd/amz-cli/pkg/uri"
)

// The mapping from records to triples.
//
// schema.org where it fits, which for a shopping site is most of it, and amzv:
// for what does not. The choices worth arguing about are in 04_graph.md section
// 7 and repeated at the line that implements each of them, because a consumer
// reading the output will want to know why a field is shaped the way it is and
// they will be reading the triples, not this file.
//
// pkg/rdf knows nothing about Amazon and this file knows nothing about turtle
// syntax. That split is what makes both halves testable: the serializer is
// checked by writing terms and reading bytes, and this file is checked by
// asserting triples.

// ExportOpts is what an export was asked for.
type ExportOpts struct {
	// WithText carries schema:description and schema:reviewBody. It is off by
	// default for the reason in 00_overview.md section 5: a local store of
	// prices is a person's own measurements, and a local store of Amazon's
	// prose is a copy of Amazon's prose.
	WithText bool
	// IncludeSponsored keeps the paid placements. They get their own predicate
	// either way, so this is about size rather than about honesty.
	IncludeSponsored bool
}

// edgePredicate maps each of the sixteen edges to the term it is exported as.
//
// Two entries for related_to, and that is the point. An organic recommendation
// is schema:isRelatedTo and a paid one is amzv:sponsoredPlacement, never both
// and never the same one, so a downstream consumer cannot mix them by accident
// even if it ignores every property on the edge.
var edgePredicate = map[string]rdf.CURIE{
	graph.SoldBy:      "amzv:soldBy",
	graph.FulfilledBy: "amzv:fulfilledBy",
	graph.MadeBy:      "schema:brand",
	graph.AuthoredBy:  "schema:author",
	graph.InNode:      "amzv:browseNode",
	graph.RankedIn:    "amzv:rankedIn",
	graph.ChartedIn:   "amzv:chartedIn",
	graph.ChildOf:     "schema:isPartOf",
	graph.VariantOf:   "schema:isVariantOf",
	graph.ParentOf:    "amzv:hasVariant",
	graph.RelatedTo:   "schema:isRelatedTo",
	graph.BundledWith: "amzv:bundledWith",
	graph.ReviewedIn:  "schema:itemReviewed",
	graph.WrittenBy:   "schema:author",
	graph.SellsUnder:  "amzv:sellsUnder",
	graph.FoundBy:     "amzv:foundBy",
}

// SponsoredPredicate is what a paid placement is exported as. It is a constant
// rather than a map entry because it is the answer to a question the map cannot
// express: one predicate, two terms, chosen by a property.
const SponsoredPredicate = rdf.CURIE("amzv:sponsoredPlacement")

// EdgeTerm is the predicate one edge exports as.
func EdgeTerm(e Edge) (rdf.CURIE, bool) {
	if e.Predicate == graph.RelatedTo && e.Sponsored {
		return SponsoredPredicate, true
	}
	p, ok := edgePredicate[e.Predicate]
	return p, ok
}

// ProductTriples writes one product into the graph.
func ProductTriples(g *rdf.Graph, p Product, opt ExportOpts) {
	u, err := uri.Product(p.Marketplace, p.ASIN)
	if err != nil {
		return
	}
	s := rdf.IRI(u)
	g.Add(s, "rdf:type", rdf.CURIE("schema:Product"))
	g.Add(s, "schema:name", rdf.Str(p.Title))
	g.Add(s, "schema:sku", rdf.Str(p.ASIN))
	g.Add(s, "schema:url", rdf.Str(p.URL))
	g.Add(s, "schema:gtin13", rdf.Str(firstNonEmpty(p.EAN, p.ISBN13)))
	g.Add(s, "schema:gtin12", rdf.Str(p.UPC))
	g.Add(s, "schema:isbn", rdf.Str(firstNonEmpty(p.ISBN13, p.ISBN10)))
	g.Add(s, "schema:model", rdf.Str(p.ModelNumber))
	g.Add(s, "schema:manufacturer", rdf.Str(p.Manufacturer))
	if p.Brand != nil && p.Brand.Resolved {
		g.Add(s, "schema:brand", rdf.IRI(p.Brand.URI))
	} else if p.Brand != nil {
		g.Add(s, "schema:brand", rdf.Str(p.Brand.Name))
	}
	for _, b := range p.Bullets {
		g.Add(s, "amzv:feature", rdf.Str(b))
	}
	for _, im := range p.Images {
		g.Add(s, "schema:image", rdf.Str(im.URL()))
	}
	for _, k := range rdf.SortedKeys(p.Details) {
		// One blank node per detail row rather than an invented predicate per
		// key. Amazon's detail table is open ended and localised, so minting
		// amzv:<whatever the page said> would put page text into the schema.
		bn := rdf.Blank("detail-" + uri.Hash(p.ASIN+k))
		g.Add(s, "amzv:detail", bn)
		g.Add(bn, "schema:name", rdf.Str(k))
		g.Add(bn, "schema:value", rdf.Str(p.Details[k]))
	}

	// Text is opt in, per 00_overview.md section 5.
	if opt.WithText {
		g.Add(s, "schema:description", rdf.Str(p.Description))
	}

	if p.Rating != nil {
		bn := rdf.Blank("rating-" + p.ASIN)
		g.Add(s, "schema:aggregateRating", bn)
		g.Add(bn, "rdf:type", rdf.CURIE("schema:AggregateRating"))
		g.Add(bn, "schema:ratingValue", rdf.Str(strconv.FormatFloat(*p.Rating, 'f', -1, 64)))
		if p.RatingsCount != nil {
			g.Add(bn, "schema:ratingCount", rdf.Typed(strconv.FormatInt(*p.RatingsCount, 10), "xsd:integer"))
		}
		if p.ReviewsCount != nil {
			g.Add(bn, "schema:reviewCount", rdf.Typed(strconv.FormatInt(*p.ReviewsCount, 10), "xsd:integer"))
		}
	}

	// The histogram is one measurement and it goes out as one value. Five
	// triples would let somebody query the five star bucket without the total,
	// which is how a reconstructed number gets quoted as a count.
	if d := p.Distribution; d != nil {
		pct := make([]byte, 0, 16)
		for i, v := range d.Percent {
			if i > 0 {
				pct = append(pct, ',')
			}
			pct = strconv.AppendInt(pct, int64(v), 10)
		}
		g.Add(s, "amzv:ratingDistribution", rdf.Str(string(pct)))
		// Always exported, on every record that has a histogram, because the
		// counts are reconstructed from integer percentages per 03_model.md
		// section 5 and a consumer must be able to see that. A field that is
		// only present when it is false is a field nobody checks for.
		g.Add(s, "amzv:distributionDerived", rdf.Typed(strconv.FormatBool(d.Derived), "xsd:boolean"))
	}

	if p.Offer != nil {
		offerTriples(g, s, "offer-"+p.ASIN, p.Offer, p.FetchedAt)
	}
	for _, r := range p.Ranks {
		bn := rdf.Blank("rank-" + p.ASIN + "-" + strconv.Itoa(r.Rank))
		g.Add(s, "amzv:salesRank", bn)
		g.Add(bn, "amzv:rank", rdf.Typed(strconv.Itoa(r.Rank), "xsd:integer"))
		g.Add(bn, "schema:name", rdf.Str(r.Category))
		if r.Node != nil && r.Node.Resolved {
			g.Add(bn, "amzv:node", rdf.IRI(r.Node.URI))
		}
	}
	if !p.FetchedAt.IsZero() {
		g.Add(s, "amzv:retrievedAt", rdfStamp(p.FetchedAt))
	}
	for _, r := range p.ReviewSample {
		ReviewTriples(g, r, opt)
	}
}

// offerTriples writes one offer as a blank node hanging off its product.
//
// amzv:retrievedAt is on every one of them, without exception and without a
// flag. A price without a timestamp is not a fact: Amazon's prices move within
// the day, the buy box changes hands, and an offer exported bare says "this is
// what it costs" when what was measured is "this is what it cost when I looked".
func offerTriples(g *rdf.Graph, subject rdf.Term, label string, o *Offer, at time.Time) {
	bn := rdf.Blank(label)
	g.Add(subject, "schema:offers", bn)
	g.Add(bn, "rdf:type", rdf.CURIE("schema:Offer"))
	if o.Price != nil {
		g.Add(bn, "schema:price", rdf.Str(o.Price.Display))
		g.Add(bn, "schema:priceCurrency", rdf.Str(o.Price.Currency))
	}
	if o.ListPrice != nil {
		g.Add(bn, "amzv:listPrice", rdf.Str(o.ListPrice.Display))
	}
	if o.InStock != nil {
		// schema.org spells availability as an enumeration member, so this is
		// one of two IRIs and not the sentence Amazon printed. The sentence is
		// kept beside it, because the derivation is a guess about phrasing in
		// sixteen locales and the string is not.
		v := rdf.CURIE("schema:OutOfStock")
		if *o.InStock {
			v = "schema:InStock"
		}
		g.Add(bn, "schema:availability", v)
	}
	g.Add(bn, "amzv:availabilityText", rdf.Str(o.Availability))
	g.Add(bn, "schema:itemCondition", rdf.Str(o.Condition))
	if o.SoldBy != nil && o.SoldBy.Resolved {
		g.Add(bn, "schema:seller", rdf.IRI(o.SoldBy.URI))
	} else if o.SoldBy != nil {
		g.Add(bn, "amzv:sellerName", rdf.Str(o.SoldBy.Name))
	}
	if o.ShipsFrom != nil {
		g.Add(bn, "amzv:fulfilledBy", rdf.Str(o.ShipsFrom.Name))
	}
	if !at.IsZero() {
		g.Add(bn, "amzv:retrievedAt", rdfStamp(at))
	}
}

// ReviewTriples writes one review, and its author as a node that says it cannot
// be resolved.
func ReviewTriples(g *rdf.Graph, r Review, opt ExportOpts) {
	ru, err := uri.New(uri.KindReview, "", r.ReviewID)
	if err != nil {
		return
	}
	s := rdf.IRI(ru)
	g.Add(s, "rdf:type", rdf.CURIE("schema:Review"))
	g.Add(s, "schema:name", rdf.Str(r.Title))
	if opt.WithText {
		g.Add(s, "schema:reviewBody", rdf.Str(r.Text))
	}
	if r.Rating > 0 {
		bn := rdf.Blank("reviewrating-" + r.ReviewID)
		g.Add(s, "schema:reviewRating", bn)
		g.Add(bn, "rdf:type", rdf.CURIE("schema:Rating"))
		g.Add(bn, "schema:ratingValue", rdf.Typed(strconv.Itoa(r.Rating), "xsd:integer"))
	}
	if d := r.Date; d != nil {
		// The parse when there was one, as a real xsd:date, and the line as
		// Amazon wrote it either way. A failed parse must not become a zero
		// time, because a zero time is a real date in January of year 1 and
		// everything downstream will treat it as one.
		if d.Parsed != nil {
			g.Add(s, "schema:datePublished", rdf.Typed(d.Parsed.UTC().Format("2006-01-02"), "xsd:date"))
		}
		g.Add(s, "amzv:datePublishedText", rdf.Str(d.Raw))
	}
	g.Add(s, "amzv:verifiedPurchase", rdf.Typed(strconv.FormatBool(r.VerifiedPurchase), "xsd:boolean"))
	if pu, err := uri.Product(r.Marketplace, r.ASIN); err == nil {
		g.Add(s, "schema:itemReviewed", rdf.IRI(pu))
	}
	author := r.Author
	if author == nil {
		author = PersonRef(r.ReviewerName)
	}
	if author != nil && author.URI != "" {
		a := rdf.IRI(author.URI)
		g.Add(s, "schema:author", a)
		g.Add(a, "rdf:type", rdf.CURIE("schema:Person"))
		g.Add(a, "schema:name", rdf.Str(author.Name))
		// The node is exported saying it is unresolved, because /gp/profile/ is
		// disallowed and this identity is a hash of a display name. Two people
		// called "Amazon Customer" are one node here and a consumer has to be
		// able to see that from the data rather than from the documentation.
		g.Add(a, "amzv:resolved", rdf.Typed("false", "xsd:boolean"))
	}
	if !r.FetchedAt.IsZero() {
		g.Add(s, "amzv:retrievedAt", rdfStamp(r.FetchedAt))
	}
}

// EdgeTriples writes one graph edge, with the surface and the read time on it.
//
// A sponsored edge that the export was not asked to include is dropped here
// rather than filtered by the caller, so that every path out of the tool obeys
// the same default.
func EdgeTriples(g *rdf.Graph, e Edge, opt ExportOpts) {
	if e.Sponsored && !opt.IncludeSponsored {
		return
	}
	p, ok := EdgeTerm(e)
	if !ok {
		return
	}
	g.Add(rdf.IRI(e.Src), p, rdf.IRI(e.Dst))
}

// NodeTriples types the nodes that have no record of their own: a seller amz
// has only seen named on a listing, a browse node named in a breadcrumb, a
// chart, a search.
//
// Without this an export is a pile of edges pointing at IRIs with no type, which
// is valid RDF and useless to load into anything.
func NodeTriples(g *rdf.Graph, node string) {
	r, err := uri.Parse(node)
	if err != nil {
		return
	}
	s := rdf.IRI(node)
	switch r.Kind {
	case uri.KindProduct:
		g.Add(s, "rdf:type", rdf.CURIE("schema:Product"))
	case uri.KindNode:
		g.Add(s, "rdf:type", rdf.CURIE("schema:CategoryCode"))
	case uri.KindSeller, uri.KindBrand:
		g.Add(s, "rdf:type", rdf.CURIE("schema:Organization"))
	case uri.KindAuthor, uri.KindPerson:
		g.Add(s, "rdf:type", rdf.CURIE("schema:Person"))
	case uri.KindReview:
		g.Add(s, "rdf:type", rdf.CURIE("schema:Review"))
	case uri.KindChart:
		g.Add(s, "rdf:type", rdf.CURIE("schema:ItemList"))
		g.Add(s, "amzv:chartKind", rdf.Str(r.ChartKind))
	case uri.KindSearch:
		g.Add(s, "rdf:type", rdf.CURIE("schema:SearchAction"))
	case uri.KindDeal:
		g.Add(s, "rdf:type", rdf.CURIE("schema:Offer"))
	}
	if r.Marketplace != "" {
		g.Add(s, "amzv:marketplace", rdf.Str(r.Marketplace))
	}
}

// rdfStamp is an xsd:dateTime literal in UTC.
//
// UTC and not the local zone, because an export that carries whichever timezone
// the machine happened to be in is an export that cannot be merged with another
// one without a lookup table of who ran what where.
func rdfStamp(t time.Time) rdf.Literal {
	return rdf.Typed(t.UTC().Format(time.RFC3339), "xsd:dateTime")
}
