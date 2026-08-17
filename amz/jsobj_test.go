package amz

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The cases below are cut from captures taken on 2026-08-17. Every one of them
// is something encoding/json refuses, which is the whole reason this scanner
// exists: amazon.com publishes no JSON-LD, no __NEXT_DATA__ and no Apollo cache,
// so the only machine-shaped data on a detail page is an argument to a
// JavaScript call and it is written as JavaScript.

func TestParseJSObjectReadsWhatJSONRefuses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]string // key to the JSON the scanner should produce
	}{
		{
			name: "single quoted keys and values",
			src:  `{'asin' : 'B07ZPKN6YR', 'isMediaBlock': true}`,
			want: map[string]string{"asin": `"B07ZPKN6YR"`, "isMediaBlock": `true`},
		},
		{
			name: "bare keys",
			src:  `{asin: "B07ZPKN6YR", price: 99.99}`,
			want: map[string]string{"asin": `"B07ZPKN6YR"`, "price": `99.99`},
		},
		{
			// Amazon writes these. encoding/json calls it a syntax error and
			// gives up on the whole payload.
			name: "trailing comma",
			src:  `{"a": 1, "b": 2,}`,
			want: map[string]string{"a": `1`, "b": `2`},
		},
		{
			name: "line comments between keys",
			src: `{
				// the main image, at the size the widget publishes
				"hiRes": "https://m.media-amazon.com/images/I/x.jpg",
				/* and the thumb */
				"thumb": "https://m.media-amazon.com/images/I/y.jpg"
			}`,
			want: map[string]string{
				"hiRes": `"https://m.media-amazon.com/images/I/x.jpg"`,
				"thumb": `"https://m.media-amazon.com/images/I/y.jpg"`,
			},
		},
		{
			// The inner values are strict JSON. Only the outer shell is
			// JavaScript, which is what lets the rest of amz use encoding/json
			// against everything this returns.
			name: "strict JSON nested inside a JS shell",
			src:  `{'colorImages': { 'initial': [{"hiRes":"a","variant":"MAIN"}] }}`,
			want: map[string]string{"colorImages": `{"initial":[{"hiRes":"a","variant":"MAIN"}]}`},
		},
		{
			name: "escapes inside a single quoted string",
			src:  `{'title': 'Anker\'s 737 Power Bank™'}`,
			want: map[string]string{"title": `"Anker's 737 Power Bank™"`},
		},
		{
			name: "negative and exponent numbers",
			src:  `{"a": -1, "b": 1.5e3}`,
			want: map[string]string{"a": `-1`, "b": `1.5e3`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := ParseJSObject(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range tt.want {
				raw, ok := o.Get(k)
				if !ok {
					t.Errorf("key %q missing, got keys %v", k, o.Keys)
					continue
				}
				if string(raw) != want {
					t.Errorf("%s = %s, want %s", k, raw, want)
				}
				if !json.Valid(raw) {
					t.Errorf("%s = %s, which is not valid JSON", k, raw)
				}
			}
		})
	}
}

// TestKeyOrderIsAsWritten covers the reason Keys exists.
//
// Sorting the keys would make a diff of two captures unreadable, because a field
// Amazon moved and a field Amazon added would look the same. The order as
// written is a fact about the page.
func TestKeyOrderIsAsWritten(t *testing.T) {
	o, err := ParseJSObject(`{'z': 1, 'a': 2, 'm': 3}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"z", "a", "m"}
	if strings.Join(o.Keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", o.Keys, want)
	}
}

// TestExpressionsAreRecordedNotRun is the line between a scanner and an engine.
//
// Amazon writes `'state': A.state('foo')` and `'onClick': function(e){...}`. This
// package reads literals and records the key of anything else. It never
// evaluates, and it must never guess: reading the head of an expression as if it
// were the value is a quiet lie, which is worse than an honest gap.
func TestExpressionsAreRecordedNotRun(t *testing.T) {
	// ajaxUrlParams is the real one. Amazon writes it as two string literals
	// joined with +, and taking the first half would produce a URL that looks
	// right and is missing its landing ASIN.
	src := `{
		'asin': 'B07ZPKN6YR',
		'ajaxUrlParams': "&productTypeDefinition=x" + "&landingAsin=B07ZPKN6YR",
		'onClick': function(e){ return e; },
		'state': A.state('imageBlock'),
		'width': 500
	}`
	o, err := ParseJSObject(src)
	if err != nil {
		t.Fatal(err)
	}
	// The literals on either side of the expressions are still read, which is
	// the point: one key this scanner cannot follow does not cost the payload.
	if o.String("asin") != "B07ZPKN6YR" || o.Int("width") != 500 {
		t.Errorf("asin = %q width = %d, want the literals around the expressions", o.String("asin"), o.Int("width"))
	}
	for _, k := range []string{"ajaxUrlParams", "onClick", "state"} {
		if _, ok := o.Get(k); ok {
			t.Errorf("key %q has a value: an expression must be recorded, not evaluated", k)
		}
		if !contains(o.Opaque, k) {
			t.Errorf("key %q is neither a value nor opaque, so it vanished", k)
		}
	}
	// And every opaque key is still in Keys, because a key the page wrote is a
	// fact about the page whether or not this scanner could read its value.
	for _, k := range []string{"ajaxUrlParams", "onClick", "state"} {
		if !contains(o.Keys, k) {
			t.Errorf("opaque key %q is missing from Keys", k)
		}
	}
}

// TestArrayHolesBecomeNull keeps the indexes true.
//
// An expression inside an array cannot be dropped without shifting every element
// after it, and the image block addresses its images by position.
func TestArrayHolesBecomeNull(t *testing.T) {
	o, err := ParseJSObject(`{'images': [1, f(), 3]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := o.Get("images")
	if string(raw) != `[1,null,3]` {
		t.Errorf("images = %s, want [1,null,3]", raw)
	}
}

// TestOpaqueElementStopsAtItsOwnBracket bounds the damage one unreadable
// element can do.
//
// Skipping an expression means scanning to the separator after it, and the
// closing bracket of the array the expression sits in is such a separator. The
// skip used to consume it and keep going, so an opaque last element ate the
// bracket, then the comma after the array, then the key after that, then the
// brace closing the object. One element this scanner could not read cost every
// field that followed it. The blast radius has to be the element.
func TestOpaqueElementStopsAtItsOwnBracket(t *testing.T) {
	o, err := ParseJSObject(`{'images': [1, x y], 'asin': 'B07ZPKN6YR'}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := o.Get("images")
	if string(raw) != `[1,null]` {
		t.Errorf("images = %s, want [1,null]", raw)
	}
	if got := o.String("asin"); got != "B07ZPKN6YR" {
		t.Errorf("asin = %q, want the key after the array to survive it", got)
	}
}

// TestFindJSObjectSkipsTheCallbackBrace is the anchor rule.
//
// Amazon writes P.when('A').register("ImageBlockATF", function(A){ var data = {
// ... } }), so the first brace after the anchor opens the callback and the
// second opens the payload. Hardcoding "the second brace" would break on the
// next wrapper, so each brace in a short window is offered and the first that
// yields keys wins.
func TestFindJSObjectSkipsTheCallbackBrace(t *testing.T) {
	src := `P.when('A').register("ImageBlockATF", function(A){
		var data = { 'colorImages': { 'initial': [{"hiRes":"https://m.media-amazon.com/images/I/x.jpg"}] } };
		return data;
	});`
	o, err := FindJSObject(src, "ImageBlockATF")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Get("colorImages"); !ok {
		t.Errorf("keys = %v, want the payload rather than the callback body", o.Keys)
	}
}

// TestFindJSObjectWindowStopsARunawaySearch is the bound that keeps a wrong
// answer from being worse than a missing one.
//
// The anchor appears in nav strings and comments as well as in the registration.
// Without the window, a mention 200 KB above the payload would drag the search
// into a different widget's data and return it under this widget's name.
func TestFindJSObjectWindowStopsARunawaySearch(t *testing.T) {
	src := `<!-- ImageBlockATF is registered further down -->` +
		strings.Repeat("x", payloadWindow+1) +
		`{'colorImages': {'initial': []}}`
	if _, err := FindJSObject(src, "ImageBlockATF"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("err = %v, want ErrNoObject rather than a payload from %d bytes away", err, payloadWindow)
	}
}

// TestFindJSObjectsReadsEveryWidget is the store family's shape.
//
// A detail page writes ImageBlockATF once. A storefront writes `var config = `
// once per widget, twelve times on the measured author page, with the widget's
// identity inside the object rather than beside it. Taking the first and
// stopping would read a storefront's header and miss its product grid.
func TestFindJSObjectsReadsEveryWidget(t *testing.T) {
	src := `<script>var config = {"widgetType":"Header","content":{}};</script>
	<script>var config = {"widgetType":"EditorialRow","tiles":[]};</script>
	<script>var config = {"widgetType":"ProductGrid","content":{"totalCount":135}};</script>`
	objs := FindJSObjects(src, "var config = ")
	if len(objs) != 3 {
		t.Fatalf("found %d payloads, want 3", len(objs))
	}
	want := []string{"Header", "EditorialRow", "ProductGrid"}
	for i, o := range objs {
		if got := o.String("widgetType"); got != want[i] {
			t.Errorf("payload %d is %q, want %q in document order", i, got, want[i])
		}
	}
}

// TestOneUnreadableWidgetDoesNotCostTheOthers is why FindJSObjects skips rather
// than fails.
//
// One widget writing something this scanner cannot follow is not a reason to
// discard the other eleven, and on a page where the grid is the record it is the
// difference between seventy books and none.
//
// A skip has to be a skip, though, which is what this asserts and what it caught
// when it was written: the search used to run the full window past a failed
// occurrence, walk into the next widget's object and return it, so a page with
// one unreadable widget produced the widget after it twice. Bounding each
// occurrence at the next one fixed it.
func TestOneUnreadableWidgetDoesNotCostTheOthers(t *testing.T) {
	src := `var config = ;
	var config = {"widgetType":"ProductGrid","content":{"totalCount":135}};`
	objs := FindJSObjects(src, "var config = ")
	if len(objs) != 1 {
		t.Fatalf("found %d payloads, want the one that parses", len(objs))
	}
	if objs[0].String("widgetType") != "ProductGrid" {
		t.Errorf("widgetType = %q", objs[0].String("widgetType"))
	}
}

// TestMissingAnchorSaysSo separates "the payload is not on this page" from "the
// payload is here and unreadable", because only the second is a parser problem.
func TestMissingAnchorSaysSo(t *testing.T) {
	_, err := FindJSObject(`<html><body>nothing here</body></html>`, "ImageBlockATF")
	if !errors.Is(err, ErrNoObject) {
		t.Fatalf("err = %v, want ErrNoObject", err)
	}
	if !strings.Contains(err.Error(), "not present") {
		t.Errorf("message = %q, want it to say the anchor was absent rather than unparseable", err)
	}
}

// FuzzParseJSObject asserts the scanner terminates and never panics.
//
// It reads attacker-shaped input in the sense that matters here: whatever Amazon
// ships next. Unbalanced braces, unterminated strings and truncated payloads all
// arrive eventually, and the only acceptable outcomes are an object or an error.
// The corpus below is every shape the measured pages use.
func FuzzParseJSObject(f *testing.F) {
	seeds := []string{
		`{}`,
		`{'a':1}`,
		`{a:1,}`,
		`{"a":{"b":[1,2,3]}}`,
		`{'a': 'it\'s'}`,
		`{'a': f(), 'b': 2}`,
		`{'a': "x" + "y"}`,
		`{ // comment
			'a': 1 }`,
		`{/* comment */ 'a': 1}`,
		`{'a': [1, f(), 3]}`,
		`{'a':`,
		`{'a': 'unterminated`,
		`{'a': 1`,
		`{{{{{{{{`,
		`{'a': 1e999}`,
		`{'A': 1}`,
		`[1,2,3]`,
		``,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		o, err := ParseJSObject(src)
		if err != nil {
			return
		}
		// A successful parse has to be usable, which means every value it
		// produced is valid JSON. A scanner that returns half-encoded bytes
		// would fail somewhere far away from here.
		for _, k := range o.Keys {
			raw, ok := o.Values[k]
			if !ok {
				continue
			}
			if !json.Valid(raw) {
				t.Fatalf("key %q parsed to %q, which is not valid JSON", k, raw)
			}
		}
	})
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
