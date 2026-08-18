package rdf

import (
	"bytes"
	"strings"
	"testing"
)

func write(t *testing.T, g *Graph, turtle bool) string {
	t.Helper()
	var b bytes.Buffer
	var err error
	if turtle {
		err = g.WriteTurtle(&b)
	} else {
		err = g.WriteNTriples(&b)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestNTriplesExpandsEveryPrefix(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("amz:us/product/B01"), "rdf:type", CURIE("schema:Product"))
	g.Add(IRI("amz:us/product/B01"), "schema:name", Str("A thing"))

	got := write(t, g, false)
	want := []string{
		"<amz:us/product/B01> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://schema.org/Product> .",
		`<amz:us/product/B01> <https://schema.org/name> "A thing" .`,
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("n-triples missing:\n%s\ngot:\n%s", w, got)
		}
	}
	if strings.Contains(got, "schema:") {
		t.Error("n-triples left a prefix unexpanded, so it is not fully expanded after all")
	}
}

// amz: is not a resolvable namespace, so it is written as a bare IRI rather than
// declared as a prefix. Declaring it would tell a consumer it could dereference.
func TestAmzURIsAreNotAPrefix(t *testing.T) {
	for _, p := range Prefixes {
		if p.Prefix == "amz" {
			t.Fatal("amz: was declared as a prefix, which claims it resolves")
		}
	}
	g := NewGraph()
	g.Add(IRI("amz:us/product/B01"), "schema:name", Str("x"))
	if got := write(t, g, true); !strings.Contains(got, "<amz:us/product/B01>") {
		t.Fatalf("turtle did not angle bracket the amz URI:\n%s", got)
	}
}

func TestTurtleHeaderDeclaresEveryPrefix(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "schema:name", Str("y"))
	got := write(t, g, true)
	for _, p := range Prefixes {
		line := "@prefix " + p.Prefix + ": <" + p.IRI + "> ."
		if !strings.Contains(got, line) {
			t.Errorf("turtle header missing %q", line)
		}
	}
}

func TestTurtleWritesRdfTypeAsA(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "rdf:type", CURIE("schema:Product"))
	got := write(t, g, true)
	if !strings.Contains(got, "    a schema:Product .") {
		t.Fatalf("turtle did not shorten rdf:type to a:\n%s", got)
	}
}

func TestDuplicatesAreDropped(t *testing.T) {
	g := NewGraph()
	for range 3 {
		g.Add(IRI("x"), "schema:name", Str("y"))
	}
	if g.Len() != 1 {
		t.Fatalf("%d triples, want 1", g.Len())
	}
}

// An empty term is dropped rather than written, which is what lets the mapping
// code read straight off optional fields without a nil check on every line.
func TestEmptyTermsAreDropped(t *testing.T) {
	g := NewGraph()
	g.Add(IRI(""), "schema:name", Str("y"))
	g.Add(IRI("x"), "", Str("y"))
	g.Add(IRI("x"), "schema:name", Str(""))
	g.Add(IRI("x"), "schema:name", nil)
	g.Add(IRI("x"), "schema:brand", Blank(""))
	if g.Len() != 0 {
		t.Fatalf("%d triples written from empty terms:\n%s", g.Len(), write(t, g, false))
	}
}

func TestLiteralDatatypeAndLang(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "schema:price", Typed("19.99", "xsd:decimal"))
	g.Add(IRI("x"), "schema:name", Literal{Value: "Hello", Lang: "en"})

	nt := write(t, g, false)
	if !strings.Contains(nt, `"19.99"^^<http://www.w3.org/2001/XMLSchema#decimal>`) {
		t.Errorf("n-triples did not expand the datatype:\n%s", nt)
	}
	if !strings.Contains(nt, `"Hello"@en`) {
		t.Errorf("n-triples dropped the language tag:\n%s", nt)
	}
	ttl := write(t, g, true)
	if !strings.Contains(ttl, `"19.99"^^xsd:decimal`) {
		t.Errorf("turtle expanded a datatype it should have left prefixed:\n%s", ttl)
	}
}

// Datatype and language are mutually exclusive in RDF. The writer prefers the
// datatype rather than emitting both and producing a file no parser accepts.
func TestDatatypeWinsOverLang(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "schema:p", Literal{Value: "v", Datatype: "xsd:string", Lang: "en"})
	got := write(t, g, false)
	if strings.Contains(got, "@en") {
		t.Fatalf("both a datatype and a language tag were written:\n%s", got)
	}
}

func TestEscaping(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "schema:name", Str("a \"quoted\" line\nwith a \\ and a\ttab"))
	got := write(t, g, false)
	want := `"a \"quoted\" line\nwith a \\ and a\ttab"`
	if !strings.Contains(got, want) {
		t.Fatalf("escaping is wrong:\ngot  %s\nwant %s", got, want)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatal("a literal newline reached the output, so one triple became two lines")
	}
}

// A blank node is inlined into the subject that references it, which is the
// whole reason turtle is readable.
func TestTurtleInlinesBlankNodes(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("amz:us/product/B01"), "schema:offers", Blank("offer-B01"))
	g.Add(Blank("offer-B01"), "rdf:type", CURIE("schema:Offer"))
	g.Add(Blank("offer-B01"), "schema:price", Typed("19.99", "xsd:decimal"))

	got := write(t, g, true)
	if !strings.Contains(got, "schema:offers [") {
		t.Fatalf("the offer was not inlined:\n%s", got)
	}
	if strings.Contains(got, "\n_:offer-B01") {
		t.Fatalf("the offer was inlined and then repeated at the top level:\n%s", got)
	}
	if !strings.Contains(got, "schema:price \"19.99\"^^xsd:decimal") {
		t.Fatalf("the inlined block lost a property:\n%s", got)
	}
}

// A blank node nothing references is still written. Dropping it would be data
// loss and a tidier file has never been worth that.
func TestTurtleKeepsAnUnreferencedBlankNode(t *testing.T) {
	g := NewGraph()
	g.Add(Blank("orphan"), "schema:price", Typed("1.00", "xsd:decimal"))
	got := write(t, g, true)
	if !strings.Contains(got, "_:orphan") {
		t.Fatalf("an unreferenced blank node was dropped:\n%s", got)
	}
}

// A blank label is derived from the thing it describes, not a counter, so two
// exports of the same records are byte identical and therefore diffable.
func TestOutputIsStableAcrossRuns(t *testing.T) {
	build := func() *Graph {
		g := NewGraph()
		g.Add(IRI("amz:us/product/B01"), "schema:offers", Blank("offer-B01"))
		g.Add(Blank("offer-B01"), "schema:price", Typed("19.99", "xsd:decimal"))
		g.Add(IRI("amz:us/product/B02"), "schema:name", Str("Second"))
		return g
	}
	if a, b := write(t, build(), true), write(t, build(), true); a != b {
		t.Fatalf("two exports of the same data differ:\n%s\n---\n%s", a, b)
	}
}

func TestTurtleGroupsBySubject(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("a"), "schema:name", Str("one"))
	g.Add(IRI("b"), "schema:name", Str("two"))
	g.Add(IRI("a"), "schema:sku", Str("three"))

	got := write(t, g, true)
	if strings.Count(got, "<a>\n") != 1 {
		t.Fatalf("subject a was written twice:\n%s", got)
	}
	block := got[strings.Index(got, "<a>"):]
	block = block[:strings.Index(block, "<b>")]
	if !strings.Contains(block, "schema:sku") {
		t.Fatalf("a's second triple did not join its block:\n%s", got)
	}
	if !strings.Contains(block, "schema:name \"one\" ;") {
		t.Fatalf("the non-final triple in a block is not semicolon terminated:\n%s", block)
	}
}

func TestExpandLeavesAnUnknownPrefixAlone(t *testing.T) {
	cases := map[string]string{
		"schema:Product":     "https://schema.org/Product",
		"amzv:soldBy":        "https://amz-cli.tamnd.com/v#soldBy",
		"xsd:date":           "http://www.w3.org/2001/XMLSchema#date",
		"amz:us/product/B01": "amz:us/product/B01",
		"nocolon":            "nocolon",
	}
	for in, want := range cases {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTriplesIsACopy(t *testing.T) {
	g := NewGraph()
	g.Add(IRI("x"), "schema:name", Str("y"))
	got := g.Triples()
	got[0].Subject = IRI("mutated")
	if g.Triples()[0].Subject != IRI("x") {
		t.Fatal("Triples handed out the backing array")
	}
}

func TestSortedKeys(t *testing.T) {
	got := SortedKeys(map[string]int{"c": 1, "a": 2, "b": 3})
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("SortedKeys = %v", got)
	}
}
