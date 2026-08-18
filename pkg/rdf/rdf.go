// Package rdf writes triples as turtle or n-triples.
//
// It is a serializer and nothing else. It has no idea what a product is, which
// is the point: the mapping from amz records to schema.org lives in the amz
// package where the records are, and this package can be tested by writing
// triples and reading the bytes back.
//
// Two formats because they answer different questions. N-triples is one triple
// per line, fully expanded, no prefixes and no nesting, which makes it
// greppable, streamable and diffable, and which is why it is what the tests
// assert against. Turtle is the same data folded up with prefixes and blank
// node blocks, which is what a person reads.
//
// From notes/Spec/3007/04_graph.md section 7.
package rdf

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// The prefixes this tool emits. schema.org where it fits, which for a shopping
// site is most of it, and amzv: for what does not.
const (
	SchemaNS = "https://schema.org/"
	AmzvNS   = "https://amz-cli.tamnd.com/v#"
	XSDNS    = "http://www.w3.org/2001/XMLSchema#"
	RDFNS    = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
)

// Prefixes is the prefix table, in the order the header prints them.
var Prefixes = []struct{ Prefix, IRI string }{
	{"schema", SchemaNS},
	{"amzv", AmzvNS},
	{"xsd", XSDNS},
	{"rdf", RDFNS},
}

// Term is one position in a triple: an IRI, a literal or a blank node.
type Term interface{ term() }

// IRI is a resource identifier. The amz: URIs go in here as they are, angle
// bracketed rather than prefixed, because amz: is not a resolvable namespace and
// declaring a prefix for it would imply it is one.
type IRI string

func (IRI) term() {}

// A CURIE is a prefixed name, schema:Product. It is a separate type from IRI so
// that the writer knows not to angle bracket it, and so that n-triples can
// expand it back to a full IRI without guessing.
type CURIE string

func (CURIE) term() {}

// Literal is a value, optionally typed or language tagged.
//
// Datatype and Lang are mutually exclusive in RDF and the writer enforces that
// by preferring Datatype, because everything this tool emits with a language tag
// is prose and everything with a datatype is a measurement.
type Literal struct {
	Value    string
	Datatype string // a CURIE like xsd:dateTime, or empty
	Lang     string // "en", or empty
}

func (Literal) term() {}

// Blank is an anonymous node, which is how an offer and a rating hang off a
// product without being given identifiers Amazon never issued.
//
// The label is stable and derived from the thing it describes, not a counter, so
// two exports of the same record produce the same bytes.
type Blank string

func (Blank) term() {}

// Str is an untyped string literal.
func Str(s string) Literal { return Literal{Value: s} }

// Typed is a literal with an xsd datatype.
func Typed(value, datatype string) Literal { return Literal{Value: value, Datatype: datatype} }

// Triple is one statement.
type Triple struct {
	Subject   Term
	Predicate Term
	Object    Term
}

// Graph is an ordered set of triples.
//
// Insertion order is kept rather than sorted, because the mapping code emits a
// subject's triples together and reordering them would scatter one product's
// facts across the file. Duplicates are dropped.
type Graph struct {
	triples []Triple
	seen    map[string]bool
}

// NewGraph is an empty graph.
func NewGraph() *Graph { return &Graph{seen: make(map[string]bool)} }

// Add appends a triple, ignoring one that is already present.
//
// A nil or empty object is dropped silently. That is deliberate and it is what
// lets the mapping code read straight off optional fields without a nil check
// per line: a product with no brand emits no brand triple rather than a triple
// pointing at the empty string.
// The predicate is a CURIE rather than a Term, which is a deliberate narrowing.
// Every predicate this tool emits is a prefixed name from one of the four
// declared namespaces, and typing it that way means a caller can write
// "schema:name" as an untyped constant instead of rdf.CURIE("schema:name") on
// every one of two hundred lines.
func (g *Graph) Add(s Term, p CURIE, o Term) {
	if empty(s) || empty(p) || empty(o) {
		return
	}
	t := Triple{s, p, o}
	k := NTriple(t)
	if g.seen[k] {
		return
	}
	g.seen[k] = true
	g.triples = append(g.triples, t)
}

func empty(t Term) bool {
	switch v := t.(type) {
	case nil:
		return true
	case IRI:
		return v == ""
	case CURIE:
		return v == ""
	case Blank:
		return v == ""
	case Literal:
		return v.Value == ""
	}
	return false
}

// Len is the number of triples.
func (g *Graph) Len() int { return len(g.triples) }

// Triples returns them in insertion order.
func (g *Graph) Triples() []Triple { return append([]Triple(nil), g.triples...) }

// WriteNTriples writes one fully expanded triple per line.
func (g *Graph) WriteNTriples(w io.Writer) error {
	for _, t := range g.triples {
		if _, err := fmt.Fprintln(w, NTriple(t)); err != nil {
			return err
		}
	}
	return nil
}

// NTriple is one triple on one line, with every prefix expanded.
func NTriple(t Triple) string {
	return ntTerm(t.Subject) + " " + ntTerm(t.Predicate) + " " + ntTerm(t.Object) + " ."
}

func ntTerm(t Term) string {
	switch v := t.(type) {
	case IRI:
		return "<" + string(v) + ">"
	case CURIE:
		return "<" + Expand(string(v)) + ">"
	case Blank:
		return "_:" + string(v)
	case Literal:
		s := `"` + escape(v.Value) + `"`
		switch {
		case v.Datatype != "":
			s += "^^<" + Expand(v.Datatype) + ">"
		case v.Lang != "":
			s += "@" + v.Lang
		}
		return s
	}
	return ""
}

// Expand turns schema:Product into the full IRI. A name with no known prefix is
// returned unchanged, because it is already an IRI.
func Expand(curie string) string {
	pfx, local, ok := strings.Cut(curie, ":")
	if !ok {
		return curie
	}
	for _, p := range Prefixes {
		if p.Prefix == pfx {
			return p.IRI + local
		}
	}
	return curie
}

// WriteTurtle writes the prefix header and then the triples, grouped by subject.
//
// Blank nodes are inlined into the subject that references them, in square
// brackets, which is the whole reason turtle is readable. A blank node that
// nothing references is written at the top level, because dropping it would be
// data loss and that has never been worth a tidier file.
func (g *Graph) WriteTurtle(w io.Writer) error {
	for _, p := range Prefixes {
		if _, err := fmt.Fprintf(w, "@prefix %s: <%s> .\n", p.Prefix, p.IRI); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	// Group by subject, keeping first-seen order, and note which blank nodes
	// are referenced as objects so they can be inlined rather than repeated.
	var order []string
	bySubject := map[string][]Triple{}
	inlined := map[string]bool{}
	for _, t := range g.triples {
		k := ntTerm(t.Subject)
		if _, ok := bySubject[k]; !ok {
			order = append(order, k)
		}
		bySubject[k] = append(bySubject[k], t)
		if b, ok := t.Object.(Blank); ok {
			inlined[string(b)] = true
		}
	}

	for _, k := range order {
		if b, ok := strings.CutPrefix(k, "_:"); ok && inlined[b] {
			continue
		}
		if err := writeSubject(w, k, bySubject, inlined); err != nil {
			return err
		}
	}
	return nil
}

func writeSubject(w io.Writer, subj string, bySubject map[string][]Triple, inlined map[string]bool) error {
	if _, err := fmt.Fprintf(w, "%s\n", subj); err != nil {
		return err
	}
	ts := bySubject[subj]
	for i, t := range ts {
		end := " ;"
		if i == len(ts)-1 {
			end = " ."
		}
		obj := ttlTerm(t.Object)
		if b, ok := t.Object.(Blank); ok && inlined[string(b)] {
			obj = inlineBlank(string(b), bySubject)
		}
		if _, err := fmt.Fprintf(w, "    %s %s%s\n", ttlTerm(t.Predicate), obj, end); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func inlineBlank(label string, bySubject map[string][]Triple) string {
	ts := bySubject["_:"+label]
	if len(ts) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, ttlTerm(t.Predicate)+" "+ttlTerm(t.Object))
	}
	return "[\n        " + strings.Join(parts, " ;\n        ") + "\n    ]"
}

func ttlTerm(t Term) string {
	switch v := t.(type) {
	case IRI:
		return "<" + string(v) + ">"
	case CURIE:
		if v == "rdf:type" {
			return "a"
		}
		return string(v)
	case Blank:
		return "_:" + string(v)
	case Literal:
		s := `"` + escape(v.Value) + `"`
		switch {
		case v.Datatype != "":
			s += "^^" + v.Datatype
		case v.Lang != "":
			s += "@" + v.Lang
		}
		return s
	}
	return ""
}

// escape is the turtle and n-triples string escape. The two formats agree on
// every character that matters here, so one function serves both.
func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SortedKeys is a small helper for mapping code that walks a map and needs the
// output to be stable. It lives here because every caller of this package needs
// it and none of them should be writing it again.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
