package amz

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two invariants that make the envelope worth believing.
//
// A provenance record that is only mostly complete is worse than none, because
// it invites a reader to trust the fields that carry one and say nothing about
// the fields that do not. These tests are what turn "set is the only writer" and
// "rung 4 does not grow" from comments into properties.

// provenanceMaps are the Extractor fields that must only ever be written by set.
//
// vals and prov are the pair: a value with no provenance is an unattributed
// claim, and the only way to guarantee they move together is to have one writer.
// order is here too, because it is what Fields returns and a field written
// straight into vals would be a value the record does not know it has.
var provenanceMaps = map[string]bool{"vals": true, "prov": true, "order": true}

// TestSetIsOnlyProvenanceWriter walks the package and fails on any assignment
// into the Extractor's value or provenance maps outside set.
//
// This is a test rather than a convention because the failure it prevents is
// silent. A parser that writes p.Title straight from a selector still produces a
// record, still passes every field test, and produces an envelope that says
// nothing about where the title came from. Nobody notices until the selector
// breaks and the envelope has no answer.
func TestSetIsOnlyProvenanceWriter(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range paths {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			// set is the one function allowed to do this, and NewExtractor has
			// to build the maps before there is anything to set.
			if fn.Name.Name == "set" || fn.Name.Name == "NewExtractor" {
				return false
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, lhs := range assign.Lhs {
					if f := provenanceTarget(lhs); f != "" {
						t.Errorf("%s:%d %s assigns to e.%s directly: every value and its provenance go through set",
							name, fset.Position(assign.Pos()).Line, fn.Name.Name, f)
					}
				}
				return true
			})
			return false
		})
	}
}

// provenanceTarget names the guarded field an expression assigns to, or "".
//
// Two shapes reach these maps: e.vals[k] = v through an index expression, and
// e.order = append(...) through the selector on its own. Both are assignments
// into the same state and both are caught here.
func provenanceTarget(lhs ast.Expr) string {
	switch v := lhs.(type) {
	case *ast.IndexExpr:
		return provenanceTarget(v.X)
	case *ast.SelectorExpr:
		if provenanceMaps[v.Sel.Name] {
			if id, ok := v.X.(*ast.Ident); ok && id.Name == "e" {
				return v.Sel.Name
			}
		}
	}
	return ""
}

// TestLevel4CountNotIncreasing is the ratchet on the bottom rung.
//
// A bare CSS selector is the rung this package exists to climb off. It is
// allowed, because a handful of things on amazon.com are genuinely only
// addressable that way, but every one of them is a guess that happens to be
// right today, so the count is fixed in the test and going up requires somebody
// to change this number on purpose and say why in the diff.
//
// The numbers below are what each family currently spends and each one is
// spent for a stated reason. They are a budget rather than a target: a field
// that moves from a selector to a named region should lower one of them.
func TestLevel4CountNotIncreasing(t *testing.T) {
	budget := map[Family]int{
		// parent_asin, from #landingAsin[value] and #ppd[data-asin]. The parent
		// of a variation is stated in two hidden inputs and in no named region,
		// so there is no rung to climb to.
		FamilyProduct: 1,
		// departments, from #searchDropdownBox. Every other search field is
		// anchored on data-component-type or data-cy, which is the vocabulary
		// Amazon built the result card out of, but the scope dropdown in the
		// header carries only an id and sits outside every named region on the
		// page.
		FamilySearch: 1,
		// rank, title, price and currency on a chart tile. A bestseller tile is
		// the oldest markup on the site: it names its container with
		// id="gridItemRoot" and names nothing inside it, so the four fields
		// inside are class matches on p13n and zg prefixes.
		FamilyChart: 4,
		// The deals grid. Amazon builds it from a component library that prefixes
		// its classes with dcl and publishes no data attribute per field, so
		// eleven of these are class matches by necessity. This is the largest
		// budget in the package and the one most worth spending down if Amazon
		// ever names them.
		FamilyBrowse: 11,
		// A stores page keeps its data in var config payloads, which is rung 2,
		// and its head, which is rung 3. Neither needs a selector.
		FamilyStore: 0,
		// A seller profile names all eight of its sections with id, so every
		// field on it is rung 1.
		FamilySeller: 0,
	}
	for _, fam := range Families() {
		var n int
		for _, f := range Registry(fam, "https://www.amazon.com", "B075F5X8BR") {
			if f.Level == LevelSelector {
				n++
			}
		}
		want, ok := budget[fam]
		if !ok {
			t.Errorf("family %s has no rung 4 budget: add one rather than leaving it unbounded", fam)
			continue
		}
		if n > want {
			t.Errorf("%s declares %d rung 4 fields, budget is %d: a bare selector needs a reason in the diff", fam, n, want)
		}
		if n < want {
			t.Errorf("%s declares %d rung 4 fields against a budget of %d: lower the budget so the ratchet holds", fam, n, want)
		}
	}
}

// TestEveryFieldSaysWhy asserts the registry carries a reason for every field.
//
// Why is not documentation. It is what the extraction report prints beside a
// field, and it is the difference between a reader learning that shipping_policy
// comes from page-section-shipping-policies and learning what that section is
// for. A field with no reason is a field somebody added without deciding what it
// was.
func TestEveryFieldSaysWhy(t *testing.T) {
	for _, fam := range Families() {
		for _, f := range Registry(fam, "https://www.amazon.com", "B075F5X8BR") {
			if f.Why == "" {
				t.Errorf("%s field %q has no Why", fam, f.Name)
			}
			if f.Rule == nil {
				t.Errorf("%s field %q has no Rule", fam, f.Name)
			}
			if f.Level == LevelRegion && len(f.Regions) == 0 {
				t.Errorf("%s field %q is declared at rung 1 with no region: a rung 1 field is a region plus a rule", fam, f.Name)
			}
		}
	}
}

// TestParseStaysUnder120ms is the budget on the hot path.
//
// A product page is half a megabyte and a crawl reads thousands of them, so the
// parse is the part that decides whether a run takes an hour or a day. 120 ms is
// roughly four times what the fixture measures, which leaves room for a slower
// machine and none for an accidental second pass over the document.
func TestParseStaysUnder120ms(t *testing.T) {
	c, stop := fixtureServer(t)
	defer stop()
	// Warm the cache so the measurement is the parse and not the fixture server.
	if _, err := c.FetchProduct(context.Background(), "B075F5X8BR"); err != nil {
		t.Fatal(err)
	}
	res := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := c.FetchProduct(context.Background(), "B075F5X8BR"); err != nil {
				b.Fatal(err)
			}
		}
	})
	if per := res.T / time.Duration(res.N); per > 120*time.Millisecond {
		t.Errorf("product parse takes %s, budget is 120ms", per.Round(time.Millisecond))
	}
}
