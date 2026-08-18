// Package graph is the edge vocabulary and the traversal over it.
//
// Amazon is the densest graph in this family of tools. One product page names a
// brand, a seller, a fulfiller, a parent ASIN, six siblings, four browse nodes,
// three sales ranks, eight review authors and sixty related products, and almost
// every one of those relationships is temporary. A seller wins the buy box for
// an afternoon. A rail is regenerated per request. A rank is true for the hour
// it was read.
//
// So an edge here is not a pair of nodes. It is a claim: this surface, at this
// time, said these two things are related this way. Src, Predicate and Dst say
// what was claimed, Via says which page said it, and ObservedAt says when. All
// four are in the key, because a product's rank line and a browse page's link
// both assert in_node and only one of them names a node id, and collapsing those
// two into one edge throws away which of them to believe.
//
// The vocabulary is sixteen predicates and it is closed. A predicate that is not
// in this file cannot be written, because an open vocabulary means every
// consumer has to handle a relationship it has never seen, and by the third one
// nobody does.
//
// From notes/Spec/3007/04_graph.md section 4.
package graph

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// The sixteen predicates.
//
// The names are snake case and read as a sentence from source to destination:
// product sold_by seller, review written_by person. That reads badly for exactly
// one of them, sells_under, which goes seller to brand and is an inference
// rather than something Amazon stated.
const (
	SoldBy      = "sold_by"
	FulfilledBy = "fulfilled_by"
	MadeBy      = "made_by"
	AuthoredBy  = "authored_by"
	InNode      = "in_node"
	RankedIn    = "ranked_in"
	ChartedIn   = "charted_in"
	ChildOf     = "child_of"
	VariantOf   = "variant_of"
	ParentOf    = "parent_of"
	RelatedTo   = "related_to"
	BundledWith = "bundled_with"
	ReviewedIn  = "reviewed_in"
	WrittenBy   = "written_by"
	SellsUnder  = "sells_under"
	FoundBy     = "found_by"
)

// Predicate describes one relationship: what it connects, what it carries, and
// how a traversal should treat it.
type Predicate struct {
	// Name is the wire name, one of the constants above.
	Name string
	// From and To are the kinds of node at each end, as pkg/uri names them.
	// "seller-or-amazon" is the one honest exception: a fulfiller is a merchant
	// id most of the time and the literal string Amazon the rest of the time,
	// and pretending otherwise would mean inventing a merchant id for Amazon.
	From, To string
	// Carries is the properties this edge is expected to have, in prose. It is
	// documentation that `amz graph --predicates` prints, so it stays true.
	Carries string
	// Inverse is the predicate that says the same thing backwards, when there is
	// one. Only parent_of and variant_of have one, and only parent_of is stored:
	// see Symmetric.
	Inverse string
	// Symmetric says this relationship reads the same in both directions, so a
	// traversal may follow it either way without a second stored edge.
	Symmetric bool
	// Inferred says amz worked this edge out rather than reading it off a page.
	// Only sells_under is inferred today, and the RDF export marks it.
	Inferred bool
	// Ranked says the edge carries a position or a rank, which means the same
	// pair of nodes legitimately appears at different values over time.
	Ranked bool
}

// predicates is the closed vocabulary, in the order 04_graph.md lists it.
var predicates = []Predicate{
	{Name: SoldBy, From: "product", To: "seller", Carries: "offer condition, price, retrieved_at"},
	{Name: FulfilledBy, From: "product", To: "seller-or-amazon", Carries: "retrieved_at"},
	{Name: MadeBy, From: "product", To: "brand"},
	{Name: AuthoredBy, From: "product", To: "author", Carries: "role, for books"},
	{Name: InNode, From: "product", To: "node", Carries: "whether it came from a rank line or a breadcrumb"},
	{Name: RankedIn, From: "product", To: "node", Carries: "rank, retrieved_at", Ranked: true},
	{Name: ChartedIn, From: "product", To: "chart", Carries: "rank, page, retrieved_at", Ranked: true},
	{Name: ChildOf, From: "node", To: "node"},
	{Name: VariantOf, From: "product", To: "product", Carries: "the dimension values that differ", Symmetric: true},
	{Name: ParentOf, From: "product", To: "product", Carries: "the inverse of variant_of, stored once and traversed both ways", Inverse: VariantOf},
	{Name: RelatedTo, From: "product", To: "product", Carries: "rail region, sponsored flag, position", Ranked: true},
	{Name: BundledWith, From: "product", To: "product"},
	{Name: ReviewedIn, From: "review", To: "product"},
	{Name: WrittenBy, From: "review", To: "person"},
	{Name: SellsUnder, From: "seller", To: "brand", Carries: "inferred, and marked inferred", Inferred: true},
	{Name: FoundBy, From: "product", To: "search", Carries: "position, page, retrieved_at", Ranked: true},
}

var byName = func() map[string]Predicate {
	m := make(map[string]Predicate, len(predicates))
	for _, p := range predicates {
		m[p.Name] = p
	}
	return m
}()

// ErrUnknownPredicate is a predicate outside the closed vocabulary.
var ErrUnknownPredicate = errors.New("not one of the sixteen predicates")

// Predicates returns the vocabulary, in spec order.
func Predicates() []Predicate { return append([]Predicate(nil), predicates...) }

// Lookup returns the description of one predicate.
func Lookup(name string) (Predicate, bool) { p, ok := byName[name]; return p, ok }

// Known reports whether name is in the vocabulary.
func Known(name string) bool { _, ok := byName[name]; return ok }

// Edge is one claim in the graph.
//
// Sponsored is not omitempty and never will be. An advertised relationship and
// an organic one are different data, and a consumer that cannot tell them apart
// is holding a corrupted dataset without knowing it. The zero value being the
// honest default is not a reason to hide it: a JSON reader that sees no
// sponsored field cannot tell "organic" from "this tool did not check".
type Edge struct {
	Src       string `json:"src"`
	Predicate string `json:"predicate"`
	Dst       string `json:"dst"`
	// Via is the surface id that asserted this edge, "s1" or "s5". It is part
	// of the key: which page said a thing is part of the claim.
	Via string `json:"via,omitempty"`
	// Sponsored marks a paid placement. Default traversal excludes these.
	Sponsored bool `json:"sponsored"`
	// Position is the slot this edge was seen in, for the ranked predicates:
	// the rail card index, the search position, the chart rank.
	Position int `json:"position,omitempty"`
	// Props is everything else the edge carries, which differs per predicate.
	// It is a map rather than sixteen optional fields because the alternative
	// is a struct where fourteen fields are nil on every row.
	Props map[string]any `json:"props,omitempty"`
	// ObservedAt is when the page that asserted this was read. An edge without
	// one is not a fact, for the same reason a price without one is not.
	ObservedAt time.Time `json:"observed_at"`
}

// Valid reports what is wrong with an edge, or nil.
func (e Edge) Valid() error {
	if !Known(e.Predicate) {
		return fmt.Errorf("%q: %w", e.Predicate, ErrUnknownPredicate)
	}
	if e.Src == "" || e.Dst == "" {
		return fmt.Errorf("%s: an edge needs two ends", e.Predicate)
	}
	if e.ObservedAt.IsZero() {
		return fmt.Errorf("%s %s -> %s: no observed_at, which makes it an assertion rather than an observation", e.Predicate, e.Src, e.Dst)
	}
	return nil
}

// Key is the identity of this claim: the triple plus the surface that made it.
//
// The read time is deliberately not in the key. Re-reading the same page an hour
// later restates the same claim, and a key that included the clock would grow
// one row per crawl per rail card forever. What changes over time and matters is
// price and rank, and those are their own append only tables.
func (e Edge) Key() string {
	return e.Src + "\x00" + e.Predicate + "\x00" + e.Dst + "\x00" + e.Via
}

// Graph is a set of edges with the nodes they touch, deduplicated by Key.
//
// Newest wins on a repeat, because the later read is the current state of a
// relationship that is allowed to change.
type Graph struct {
	edges map[string]Edge
	nodes map[string]bool
}

// New is an empty graph.
func New() *Graph {
	return &Graph{edges: make(map[string]Edge), nodes: make(map[string]bool)}
}

// Add records one edge and both its endpoints, and reports whether it was valid.
//
// An invalid edge is refused rather than stored and flagged, because the graph
// is written once and read many times, and a consumer should not have to filter.
func (g *Graph) Add(e Edge) error {
	if err := e.Valid(); err != nil {
		return err
	}
	k := e.Key()
	if old, ok := g.edges[k]; ok && old.ObservedAt.After(e.ObservedAt) {
		return nil
	}
	g.edges[k] = e
	g.nodes[e.Src] = true
	g.nodes[e.Dst] = true
	return nil
}

// AddNode records a node with no edge, which is how a dangling amz:person or a
// product that was crawled and turned out to relate to nothing stays in the
// export.
func (g *Graph) AddNode(uri string) {
	if uri != "" {
		g.nodes[uri] = true
	}
}

// Len is the number of distinct edges.
func (g *Graph) Len() int { return len(g.edges) }

// Edges returns every edge, sorted so that two runs over the same data produce
// byte identical output. An export nobody can diff is an export nobody trusts.
func (g *Graph) Edges() []Edge {
	out := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Src != out[j].Src {
			return out[i].Src < out[j].Src
		}
		if out[i].Predicate != out[j].Predicate {
			return out[i].Predicate < out[j].Predicate
		}
		if out[i].Dst != out[j].Dst {
			return out[i].Dst < out[j].Dst
		}
		return out[i].Via < out[j].Via
	})
	return out
}

// Nodes returns every node URI the graph touches, sorted.
func (g *Graph) Nodes() []string {
	out := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Out returns the edges leaving one node.
func (g *Graph) Out(src string) []Edge {
	var out []Edge
	for _, e := range g.Edges() {
		if e.Src == src {
			out = append(out, e)
		}
	}
	return out
}

// Walk is the options for a traversal.
//
// Cycles are normal here and are not an error. variant_of is symmetric in
// practice, related_to frequently points back, and a browse node tree has cross
// links that make it a DAG at best. So traversal is visited-set based and depth
// limited, and Depth defaults to 1 because the second hop off a product page is
// already sixty times the first.
type Walk struct {
	// Depth is how many hops from the start. Zero means the start node alone.
	Depth int
	// IncludeSponsored puts paid placements back in. A "customers also bought"
	// graph polluted with advertising is not a graph of anything, so the default
	// is off.
	IncludeSponsored bool
	// Predicates limits the walk to these. Empty means all of them.
	Predicates []string
	// Symmetric follows symmetric and inverse edges backwards as well, which is
	// what makes a variant family reachable from any of its members rather than
	// only from the one the crawl happened to start at.
	Symmetric bool
}

// Result is one node reached by a traversal, with how it was reached.
type Result struct {
	URI string `json:"uri"`
	// Depth is the number of hops from the start, so 0 is the start itself.
	Depth int `json:"depth"`
	// Via is the edge that first reached this node, absent for the start.
	Via *Edge `json:"via,omitempty"`
}

// Traverse walks out from start and returns every node reached, in the order it
// was reached, breadth first.
//
// Breadth first rather than depth first so that Depth on each result is the
// shortest path and not whichever path the recursion happened to take. A node
// two hops away that is also reachable in five should say two.
func (g *Graph) Traverse(start string, w Walk) []Result {
	allow := map[string]bool{}
	for _, p := range w.Predicates {
		allow[p] = true
	}
	want := func(e Edge) bool {
		if !w.IncludeSponsored && e.Sponsored {
			return false
		}
		return len(allow) == 0 || allow[e.Predicate]
	}

	seen := map[string]bool{start: true}
	out := []Result{{URI: start}}
	frontier := []string{start}

	for d := 1; d <= w.Depth && len(frontier) > 0; d++ {
		var next []string
		for _, node := range frontier {
			for _, e := range g.Edges() {
				dst, ok := step(e, node, w)
				if !ok || !want(e) || seen[dst] {
					continue
				}
				seen[dst] = true
				out = append(out, Result{URI: dst, Depth: d, Via: &e})
				next = append(next, dst)
			}
		}
		frontier = next
	}
	return out
}

// step says where one edge takes you from a node, if anywhere.
//
// Backwards traversal is opt in and limited to the predicates that mean the same
// thing in reverse. Following sold_by backwards would turn one seller into every
// product it lists, which is a legitimate query and is not what "two hops from
// this product" should mean.
func step(e Edge, from string, w Walk) (string, bool) {
	if e.Src == from {
		return e.Dst, true
	}
	if !w.Symmetric || e.Dst != from {
		return "", false
	}
	p, ok := Lookup(e.Predicate)
	if !ok || (!p.Symmetric && p.Inverse == "") {
		return "", false
	}
	return e.Src, true
}
