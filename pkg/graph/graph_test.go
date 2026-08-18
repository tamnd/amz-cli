package graph

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

var now = at("2026-01-02T03:04:05Z")

func e(src, pred, dst string) Edge {
	return Edge{Src: src, Predicate: pred, Dst: dst, Via: "s1", ObservedAt: now}
}

// The vocabulary is closed and the spec says sixteen. If somebody adds a
// seventeenth without going back to 04_graph.md, this is where they find out.
func TestVocabularyIsSixteenAndClosed(t *testing.T) {
	got := Predicates()
	if len(got) != 16 {
		t.Fatalf("vocabulary is %d predicates, spec says 16", len(got))
	}
	seen := map[string]bool{}
	for _, p := range got {
		if p.Name == "" || p.From == "" || p.To == "" {
			t.Errorf("%+v: every predicate names both ends", p)
		}
		if seen[p.Name] {
			t.Errorf("%s: listed twice", p.Name)
		}
		seen[p.Name] = true
		if !Known(p.Name) {
			t.Errorf("%s: in the table but not in the index", p.Name)
		}
	}
	if Known("recommends") {
		t.Error("an unlisted predicate is known, so the vocabulary is not closed")
	}
}

// Predicates returns a copy. A caller that sorts the result for display must not
// be able to reorder the vocabulary for everybody else.
func TestPredicatesIsACopy(t *testing.T) {
	first := Predicates()[0].Name
	got := Predicates()
	got[0].Name = "mutated"
	if Predicates()[0].Name != first {
		t.Fatal("Predicates handed out the backing array")
	}
}

func TestInverseAndSymmetricAreConsistent(t *testing.T) {
	for _, p := range Predicates() {
		if p.Inverse == "" {
			continue
		}
		if !Known(p.Inverse) {
			t.Errorf("%s: inverse %q is not in the vocabulary", p.Name, p.Inverse)
		}
		if p.Symmetric {
			t.Errorf("%s: has both an inverse and a symmetric flag, which are two different claims", p.Name)
		}
	}
}

func TestValidRefusesWhatIsNotAnObservation(t *testing.T) {
	cases := []struct {
		name string
		edge Edge
		want string
	}{
		{"unknown predicate", Edge{Src: "a", Predicate: "recommends", Dst: "b", ObservedAt: now}, "sixteen"},
		{"no destination", Edge{Src: "a", Predicate: SoldBy, ObservedAt: now}, "two ends"},
		{"no source", Edge{Predicate: SoldBy, Dst: "b", ObservedAt: now}, "two ends"},
		{"no clock", Edge{Src: "a", Predicate: SoldBy, Dst: "b"}, "observed_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.edge.Valid()
			if err == nil {
				t.Fatalf("%+v: accepted", tc.edge)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	if err := e("a", SoldBy, "b").Valid(); err != nil {
		t.Fatalf("a complete edge was refused: %v", err)
	}
}

func TestUnknownPredicateIsMatchable(t *testing.T) {
	err := Edge{Src: "a", Predicate: "nope", Dst: "b", ObservedAt: now}.Valid()
	if !errors.Is(err, ErrUnknownPredicate) {
		t.Fatalf("errors.Is did not match ErrUnknownPredicate: %v", err)
	}
}

// The read time is deliberately outside the key, and the surface is deliberately
// inside it. Both halves of that decision get a test, because both are the kind
// of thing a later refactor quietly reverses.
func TestKeyExcludesTheClockAndIncludesTheSurface(t *testing.T) {
	a := e("p1", RelatedTo, "p2")
	b := a
	b.ObservedAt = now.Add(72 * time.Hour)
	if a.Key() != b.Key() {
		t.Error("re-reading the same page produced a second key, so the table grows per crawl forever")
	}
	c := a
	c.Via = "s4"
	if a.Key() == c.Key() {
		t.Error("two surfaces asserting the same pair collapsed to one edge, so which page said it is lost")
	}
}

func TestAddRefusesInvalidAndKeepsTheNewerRead(t *testing.T) {
	g := New()
	if err := g.Add(Edge{Src: "a", Predicate: "nope", Dst: "b", ObservedAt: now}); err == nil {
		t.Fatal("an invalid edge was stored")
	}
	if g.Len() != 0 {
		t.Fatal("a refused edge was still counted")
	}

	old := e("p1", RelatedTo, "p2")
	old.Position = 1
	newer := old
	newer.Position = 9
	newer.ObservedAt = now.Add(time.Hour)

	if err := g.Add(newer); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(old); err != nil {
		t.Fatal(err)
	}
	if g.Len() != 1 {
		t.Fatalf("same claim stored twice: %d edges", g.Len())
	}
	if got := g.Edges()[0].Position; got != 9 {
		t.Fatalf("position %d, want 9: the older read overwrote the newer one", got)
	}
}

func TestEdgesAreSortedSoTwoRunsMatch(t *testing.T) {
	g := New()
	for _, ed := range []Edge{
		e("p2", RelatedTo, "p9"),
		e("p1", SoldBy, "s1"),
		e("p1", RelatedTo, "p3"),
		e("p1", RelatedTo, "p2"),
	} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, ed := range g.Edges() {
		got = append(got, ed.Src+" "+ed.Predicate+" "+ed.Dst)
	}
	want := []string{
		"p1 related_to p2",
		"p1 related_to p3",
		"p1 sold_by s1",
		"p2 related_to p9",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("edge %d is %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAddNodeKeepsAnUnconnectedNode(t *testing.T) {
	g := New()
	g.AddNode("amz:person/anon")
	g.AddNode("")
	if len(g.Nodes()) != 1 {
		t.Fatalf("nodes %v, want the one non-empty node", g.Nodes())
	}
}

// A cycle is normal data here, not an error. If the visited set ever regresses
// this test hangs rather than fails, which is the loudest failure there is.
func TestTraverseTerminatesOnACycle(t *testing.T) {
	g := New()
	for _, ed := range []Edge{
		e("a", RelatedTo, "b"),
		e("b", RelatedTo, "c"),
		e("c", RelatedTo, "a"),
	} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	got := g.Traverse("a", Walk{Depth: 99})
	if len(got) != 3 {
		t.Fatalf("reached %d nodes, want 3 each once", len(got))
	}
	if got[0].URI != "a" || got[0].Depth != 0 || got[0].Via != nil {
		t.Fatalf("first result is %+v, want the start at depth 0 with no via", got[0])
	}
}

// Breadth first, so a node reachable two ways reports the shorter distance.
func TestTraverseReportsTheShortestPath(t *testing.T) {
	g := New()
	for _, ed := range []Edge{
		e("a", RelatedTo, "z"),
		e("a", RelatedTo, "b"),
		e("b", RelatedTo, "c"),
		e("c", RelatedTo, "z"),
	} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range g.Traverse("a", Walk{Depth: 5}) {
		if r.URI == "z" && r.Depth != 1 {
			t.Fatalf("z reported at depth %d, want 1", r.Depth)
		}
	}
}

func TestTraverseDepthZeroIsTheStartAlone(t *testing.T) {
	g := New()
	if err := g.Add(e("a", RelatedTo, "b")); err != nil {
		t.Fatal(err)
	}
	if got := g.Traverse("a", Walk{}); len(got) != 1 || got[0].URI != "a" {
		t.Fatalf("depth 0 returned %+v, want the start alone", got)
	}
}

func TestTraverseExcludesSponsoredByDefault(t *testing.T) {
	g := New()
	organic := e("a", RelatedTo, "b")
	paid := e("a", RelatedTo, "ad")
	paid.Sponsored = true
	for _, ed := range []Edge{organic, paid} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.Traverse("a", Walk{Depth: 1}); len(got) != 2 {
		t.Fatalf("reached %+v, want the start and the organic neighbour only", got)
	}
	if got := g.Traverse("a", Walk{Depth: 1, IncludeSponsored: true}); len(got) != 3 {
		t.Fatalf("reached %d nodes with --include-sponsored, want 3", len(got))
	}
}

func TestTraversePredicateFilter(t *testing.T) {
	g := New()
	for _, ed := range []Edge{e("a", RelatedTo, "b"), e("a", SoldBy, "s1")} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	got := g.Traverse("a", Walk{Depth: 1, Predicates: []string{SoldBy}})
	if len(got) != 2 || got[1].URI != "s1" {
		t.Fatalf("reached %+v, want only the seller", got)
	}
}

// Backwards traversal is opt in and only for the predicates that mean the same
// thing in reverse. Walking sold_by backwards would turn one seller into its
// whole catalogue, which is a different query than "two hops from this product".
func TestSymmetricWalksBackOnlyWhereItIsHonest(t *testing.T) {
	g := New()
	for _, ed := range []Edge{e("p1", VariantOf, "p2"), e("p3", SoldBy, "seller")} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.Traverse("p2", Walk{Depth: 1}); len(got) != 1 {
		t.Fatalf("reached %+v without --symmetric, want the start alone", got)
	}
	if got := g.Traverse("p2", Walk{Depth: 1, Symmetric: true}); len(got) != 2 {
		t.Fatalf("reached %+v with --symmetric, want p1 as well", got)
	}
	if got := g.Traverse("seller", Walk{Depth: 1, Symmetric: true}); len(got) != 1 {
		t.Fatalf("sold_by was followed backwards: %+v", got)
	}
}

// parent_of is stored once and traversed both ways, which is what keeps twelve
// colours at twelve edges instead of a hundred and thirty two.
func TestParentOfIsTraversedBothWays(t *testing.T) {
	g := New()
	for _, dst := range []string{"c1", "c2", "c3"} {
		if err := g.Add(e("parent", ParentOf, dst)); err != nil {
			t.Fatal(err)
		}
	}
	got := g.Traverse("c1", Walk{Depth: 2, Symmetric: true})
	if len(got) != 4 {
		t.Fatalf("reached %d nodes from one sibling, want the family of 4", len(got))
	}
}

func TestOutReturnsOnlyTheEdgesLeavingANode(t *testing.T) {
	g := New()
	for _, ed := range []Edge{e("a", RelatedTo, "b"), e("b", RelatedTo, "c")} {
		if err := g.Add(ed); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.Out("a"); len(got) != 1 || got[0].Dst != "b" {
		t.Fatalf("Out(a) = %+v", got)
	}
	if got := g.Out("nobody"); got != nil {
		t.Fatalf("Out on an unknown node returned %+v", got)
	}
}
