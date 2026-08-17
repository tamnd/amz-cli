package amz

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A reader for the object literals Amazon inlines into <script> tags.
//
// amazon.com publishes no JSON-LD, no __NEXT_DATA__ and no Apollo cache, so the
// only machine-shaped data on a detail page is the argument to a few JavaScript
// registrations. Those arguments are object literals rather than JSON:
//
//	var data = {
//	    'isMediaBlock': true,
//	    'asin' : 'B07ZPKN6YR',
//	    'colorImages': { 'initial': [{"hiRes":"...","variant":"MAIN"}, ...] },
//	};
//
// Single quoted keys, single quoted strings, trailing commas and comments.
// encoding/json refuses all four, and the inner values are strict JSON, so the
// outer shell is the only part that needs a tolerant reader.
//
// This is a scanner and not a JavaScript engine. It reads literals, it records
// the source text of anything that is not a literal, and it evaluates nothing.
// See notes/Spec/3007/02_extraction.md section 3.

// ErrNoObject means the anchor was not on the page, or no object literal
// followed it.
var ErrNoObject = errors.New("no object literal found")

// JSObject is one parsed literal. Values are re-encoded as strict JSON so the
// rest of amz can use encoding/json against them.
type JSObject struct {
	// Keys is the key order as written, which is worth keeping: a diff of two
	// captures reads better when the fields have not been sorted.
	Keys []string
	// Values maps a key to its value, re-encoded as JSON.
	Values map[string]json.RawMessage
	// Opaque names the keys whose value was not a literal, an expression such as
	// `A.state('foo')` or a function. The key is recorded, the source is not run.
	Opaque []string
}

// Get returns the raw JSON for a key.
func (o *JSObject) Get(key string) (json.RawMessage, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.Values[key]
	return v, ok
}

// String returns a string-valued key.
func (o *JSObject) String(key string) string {
	raw, ok := o.Get(key)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// Int returns a number-valued key.
func (o *JSObject) Int(key string) int {
	raw, ok := o.Get(key)
	if !ok {
		return 0
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return 0
	}
	return int(n)
}

// Unmarshal decodes one key into v.
func (o *JSObject) Unmarshal(key string, v any) error {
	raw, ok := o.Get(key)
	if !ok {
		return fmt.Errorf("%w: key %q", ErrNoObject, key)
	}
	return json.Unmarshal(raw, v)
}

const (
	// candidateBraces is how many opening braces after an anchor are tried
	// before moving on. Measured 2026-08-17: ImageBlockATF needs two, because
	// the anchor is followed by `function(A){` and the payload is the
	// `var data = {` inside it. The twister needs two for the same reason. Eight
	// is room for a wrapper Amazon has not written yet.
	candidateBraces = 8
	// payloadWindow is how far past an anchor a candidate brace may sit. The
	// measured distance is under 100 bytes in both payloads. The bound is what
	// stops a mention of the anchor in a comment or a nav string from dragging
	// the search a quarter of a megabyte down the page and into a different
	// widget's data, which is a wrong answer rather than a missing one.
	payloadWindow = 4 << 10
	// anchorOccurrences is how many mentions of the anchor are tried. The
	// payload is not always the first one.
	anchorOccurrences = 8
)

// FindJSObject finds the object literal Amazon registered under anchor.
//
// The anchor is the registration name Amazon gave the payload, which is the same
// kind of contract as a data-feature-name: `ImageBlockATF` is the image widget's
// own name for its own data, and a redesign that renames it is a redesign that
// rewrote the widget.
//
// The brace that follows the anchor is not the payload. Amazon writes
// `P.when('A').register("ImageBlockATF", function(A){ var data = { ... } })`, so
// the first brace opens the callback and the second opens the data. Rather than
// hardcode which one, each brace within a short window is offered to the parser
// and the first that yields an object with keys wins. A callback body fails on
// its first statement, which is cheap.
func FindJSObject(src, anchor string) (*JSObject, error) {
	var firstErr error
	found := false
	for occ, from := 0, 0; occ < anchorOccurrences && from < len(src); occ++ {
		k := strings.Index(src[from:], anchor)
		if k < 0 {
			break
		}
		found = true
		start := from + k + len(anchor)
		from = start
		window := src[start:min(start+payloadWindow, len(src))]
		for n, at := 0, 0; n < candidateBraces; n++ {
			j := strings.IndexByte(window[at:], '{')
			if j < 0 {
				break
			}
			at += j
			// The object may run well past the window; only its opening brace
			// has to be inside it.
			obj, err := ParseJSObject(src[start+at:])
			if err == nil && len(obj.Keys) > 0 {
				return obj, nil
			}
			if firstErr == nil && err != nil {
				firstErr = err
			}
			at++
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: anchor %q not present", ErrNoObject, anchor)
	}
	if firstErr != nil {
		return nil, fmt.Errorf("%w: no object literal within %d bytes of %q: %v",
			ErrNoObject, payloadWindow, anchor, firstErr)
	}
	return nil, fmt.Errorf("%w: nothing opens after %q", ErrNoObject, anchor)
}

// FindJSObjects finds every object literal registered under anchor, in document
// order, and is the many-payloads counterpart to FindJSObject.
//
// It exists because the store family repeats one anchor rather than naming each
// payload. A detail page writes ImageBlockATF once and a storefront writes
// `var config = ` twelve times, once per widget, with the widget's identity
// inside the object rather than beside it. Taking the first and stopping would
// read a brand storefront's hero and miss its ten editorial rows.
//
// The occurrence cap that FindJSObject applies does not apply here, because
// there the cap bounds a search for one answer and here every occurrence is an
// answer. An occurrence that yields no object is skipped rather than failing the
// whole read, since one widget writing something this scanner cannot follow is
// not a reason to discard the other eleven.
//
// Each occurrence searches only as far as the next one. That extra bound is what
// keeps a skip honest: without it, an occurrence whose own payload will not
// parse walks forward into the next widget's object and returns it, so a page
// with one unreadable widget yields the widget after it twice and a caller
// counting products counts a grid it has already counted. Only the opening brace
// has to sit before the next occurrence; the object itself may run past it.
func FindJSObjects(src, anchor string) []*JSObject {
	var out []*JSObject
	for from := 0; from < len(src); {
		k := strings.Index(src[from:], anchor)
		if k < 0 {
			break
		}
		start := from + k + len(anchor)
		from = start
		end := min(start+payloadWindow, len(src))
		if next := strings.Index(src[start:], anchor); next >= 0 {
			end = min(end, start+next)
		}
		window := src[start:end]
		for n, at := 0, 0; n < candidateBraces; n++ {
			j := strings.IndexByte(window[at:], '{')
			if j < 0 {
				break
			}
			at += j
			if obj, err := ParseJSObject(src[start+at:]); err == nil && len(obj.Keys) > 0 {
				out = append(out, obj)
				break
			}
			at++
		}
	}
	return out
}

// ParseJSObject parses an object literal starting at the leading brace of src.
// Trailing text after the closing brace is ignored, so the caller can pass the
// rest of the document.
func ParseJSObject(src string) (*JSObject, error) {
	p := &jsScanner{src: src}
	p.space()
	if p.peek() != '{' {
		return nil, fmt.Errorf("%w: %q does not open an object", ErrNoObject, p.snippet())
	}
	return p.object()
}

type jsScanner struct {
	src string
	i   int
}

func (p *jsScanner) peek() byte {
	if p.i >= len(p.src) {
		return 0
	}
	return p.src[p.i]
}

func (p *jsScanner) snippet() string {
	end := min(p.i+24, len(p.src))
	return p.src[p.i:end]
}

// space skips whitespace and both comment forms. Amazon's inlined blocks carry
// // comments between keys often enough that a parser without this fails on
// roughly one detail page in five.
func (p *jsScanner) space() {
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == ' ', c == '\t', c == '\n', c == '\r':
			p.i++
		case c == '/' && p.i+1 < len(p.src) && p.src[p.i+1] == '/':
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
		case c == '/' && p.i+1 < len(p.src) && p.src[p.i+1] == '*':
			end := strings.Index(p.src[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.src)
				return
			}
			p.i += 2 + end + 2
		default:
			return
		}
	}
}

func (p *jsScanner) object() (*JSObject, error) {
	o := &JSObject{Values: map[string]json.RawMessage{}}
	p.i++ // past '{'
	for {
		p.space()
		switch p.peek() {
		case 0:
			return nil, fmt.Errorf("%w: object is not closed", ErrNoObject)
		case '}':
			p.i++
			return o, nil
		case ',':
			p.i++ // a trailing or doubled comma
			continue
		}
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		p.space()
		if p.peek() != ':' {
			return nil, fmt.Errorf("%w: key %q is not followed by a colon", ErrNoObject, key)
		}
		p.i++
		p.space()
		raw, opaque := p.value()
		// A literal that is not followed by a separator was only the head of an
		// expression. Amazon writes "&productTypeDefinition=..." + "&landingAsin=B0..."
		// for ajaxUrlParams, and reading the first half as the value would be a
		// quiet lie. The key is recorded as opaque and the rest is skipped, which
		// keeps the object parsing instead of failing on the key after it.
		if !opaque {
			p.space()
			if c := p.peek(); c != ',' && c != '}' && c != 0 {
				p.skipExpression()
				raw, opaque = nil, true
			}
		}
		if _, dup := o.Values[key]; !dup {
			o.Keys = append(o.Keys, key)
		}
		if opaque {
			o.Opaque = append(o.Opaque, key)
			continue
		}
		o.Values[key] = raw
	}
}

// key reads a quoted or bare key.
func (p *jsScanner) key() (string, error) {
	switch c := p.peek(); c {
	case '\'', '"':
		return p.stringLit()
	case 0:
		return "", fmt.Errorf("%w: object is not closed", ErrNoObject)
	}
	start := p.i
	for p.i < len(p.src) && (isIdent(p.src[p.i])) {
		p.i++
	}
	if p.i == start {
		return "", fmt.Errorf("%w: %q is not a key", ErrNoObject, p.snippet())
	}
	return p.src[start:p.i], nil
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// value reads one value and re-encodes it as JSON. The second result reports
// that the value was an expression rather than a literal, in which case it has
// been skipped over and nothing was evaluated.
func (p *jsScanner) value() (json.RawMessage, bool) {
	switch c := p.peek(); {
	case c == '{':
		nested, err := p.object()
		if err != nil {
			return nil, true
		}
		return nested.encode(), false
	case c == '[':
		return p.array()
	case c == '\'' || c == '"':
		s, err := p.stringLit()
		if err != nil {
			return nil, true
		}
		b, _ := json.Marshal(s)
		return b, false
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	for _, lit := range []string{"true", "false", "null"} {
		if strings.HasPrefix(p.src[p.i:], lit) && !isIdent(byteAt(p.src, p.i+len(lit))) {
			p.i += len(lit)
			return json.RawMessage(lit), false
		}
	}
	p.skipExpression()
	return nil, true
}

func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

func (p *jsScanner) array() (json.RawMessage, bool) {
	p.i++ // past '['
	var out []json.RawMessage
	for {
		p.space()
		switch p.peek() {
		case 0:
			return nil, true
		case ']':
			p.i++
			var b strings.Builder
			b.WriteByte('[')
			for i, e := range out {
				if i > 0 {
					b.WriteByte(',')
				}
				b.Write(e)
			}
			b.WriteByte(']')
			return json.RawMessage(b.String()), false
		case ',':
			p.i++
			continue
		}
		at := p.i
		raw, opaque := p.value()
		if opaque {
			// A hole in an array cannot be dropped without shifting every index
			// after it, so it becomes null and the element count stays true.
			raw = json.RawMessage("null")
		}
		// An element that consumed nothing means the array is not closed and the
		// scanner cannot get past what it is looking at, so the array ends here
		// rather than appending a null for the same byte forever. Found by
		// FuzzParseJSObject on {"":{"":[\x87}}, where the byte is skipped and
		// the } after it belongs to the enclosing object: value() correctly
		// declines to consume it and the loop had no way to notice.
		if p.i == at {
			return nil, true
		}
		out = append(out, raw)
	}
}

func (p *jsScanner) number() (json.RawMessage, bool) {
	start := p.i
	if p.peek() == '-' {
		p.i++
	}
	for p.i < len(p.src) {
		c := p.src[p.i]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			p.i++
			continue
		}
		break
	}
	lit := p.src[start:p.i]
	// JavaScript numbers that JSON does not spell the same way: 00, .5, 1., +1
	// and 0x10 are all literals a page may write and all things encoding/json
	// rejects. Passing the source text straight through produced bytes that
	// looked like a value and failed at the far end of the program, in a
	// json.Unmarshal inside whichever field happened to read it, which is the
	// worst place to learn about it. Found by FuzzParseJSObject on {'':00}.
	//
	// A literal JSON already accepts is kept exactly as written, because the
	// text Amazon wrote is the fact and reformatting 1.50 into 1.5 would lose
	// how the page said it. Anything else is normalized through the value it
	// denotes, which is what the JavaScript engine on the page would have done
	// with it, and a literal that denotes no finite number is opaque.
	if json.Valid([]byte(lit)) {
		return json.RawMessage(lit), false
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return nil, true
	}
	return json.RawMessage(strconv.FormatFloat(f, 'g', -1, 64)), false
}

// stringLit reads a single or double quoted string and unescapes it.
func (p *jsScanner) stringLit() (string, error) {
	quote := p.src[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch c {
		case quote:
			p.i++
			return b.String(), nil
		case '\\':
			p.i++
			if p.i >= len(p.src) {
				return "", fmt.Errorf("%w: string is not closed", ErrNoObject)
			}
			switch e := p.src[p.i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if p.i+4 < len(p.src) {
					if n, err := strconv.ParseUint(p.src[p.i+1:p.i+5], 16, 32); err == nil {
						var buf [4]byte
						b.Write(buf[:utf8.EncodeRune(buf[:], rune(n))])
						p.i += 4
					}
				}
			default:
				b.WriteByte(e)
			}
			p.i++
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", fmt.Errorf("%w: string is not closed", ErrNoObject)
}

// skipExpression walks past a value that is not a literal, respecting nesting
// and strings, and stops at the comma or brace that ends it.
func (p *jsScanner) skipExpression() {
	depth := 0
	for p.i < len(p.src) {
		switch c := p.src[p.i]; c {
		case '(', '[', '{':
			depth++
			p.i++
		case ')', ']', '}':
			// A closer at depth 0 belongs to whatever contains this expression,
			// so it ends the skip and is left for that caller to read. Consuming
			// it used to let an opaque last element eat the bracket closing its
			// own array and carry on scanning into the rest of the object.
			if depth == 0 {
				return
			}
			depth--
			p.i++
		case ',':
			if depth == 0 {
				return
			}
			p.i++
		case '\'', '"':
			if _, err := p.stringLit(); err != nil {
				p.i = len(p.src)
				return
			}
		case '/':
			if p.i+1 < len(p.src) && (p.src[p.i+1] == '/' || p.src[p.i+1] == '*') {
				p.space()
				continue
			}
			p.i++
		default:
			p.i++
		}
	}
}

func (o *JSObject) encode() json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, k := range o.Keys {
		v, ok := o.Values[k]
		if !ok {
			continue // an opaque key carries no value to encode
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}
