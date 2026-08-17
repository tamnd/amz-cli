package amz

import (
	"encoding/json"
	"fmt"
	"sort"
)

// The extraction ladder and its bookkeeping.
//
// Four rungs. Every field records which one answered it, and the record carries
// that alongside the value. The point is not tidiness. When a field goes empty
// six months from now, "it came from a bare CSS selector added on 2026-08-17" is
// a diagnosis and "it is empty" is not.
//
// See notes/Spec/3007/02_extraction.md sections 1, 2 and 5.

// Level is a rung on the ladder. Lower is more stable.
type Level int

const (
	// LevelRegion is a region Amazon named, then a structural rule inside it.
	// This is Amazon's own contract with itself.
	LevelRegion Level = 1
	// LevelPayload is a JavaScript payload: ImageBlockATF, the twister data, an
	// a-state blob. This is Amazon's own data.
	LevelPayload Level = 2
	// LevelAttr is a data-* attribute or an aria-label outside a named region.
	// Semantic, and meant for machines, just not for this one.
	LevelAttr Level = 3
	// LevelSelector is a bare CSS selector. A guess about layout. Allowed,
	// counted, and reported.
	LevelSelector Level = 4
)

func (l Level) String() string {
	switch l {
	case LevelRegion:
		return "region"
	case LevelPayload:
		return "payload"
	case LevelAttr:
		return "attr"
	case LevelSelector:
		return "selector"
	default:
		return "unknown"
	}
}

// Prov is where one field came from.
type Prov struct {
	Level Level  `json:"level"`
	Via   string `json:"via"`
}

// Miss is a field that was looked for and not found, and why.
//
// A product with no coupon and a product whose coupon region moved are different
// facts. The first has the region present and empty; the second does not have
// the region. Only the second is a parser problem, and only this distinction
// makes it visible.
type Miss struct {
	Field string `json:"field"`
	Why   string `json:"why"`
}

// Disagreement is one field answered differently by two sources.
//
// Not a defect to hide. On Amazon the buy box price and the apex price genuinely
// differ when a subscription discount or a coupon applies, and a tool that
// silently picks one is throwing away the interesting part.
type Disagreement struct {
	Field string `json:"field"`
	Via   string `json:"via"`
	Value any    `json:"value"`
}

// Extractor accumulates one record's values and their provenance.
//
// set is the only writer. TestSetIsOnlyProvenanceWriter walks the AST of package
// amz and fails on any other assignment into these maps, which is how via and
// level are guaranteed to exist for every field rather than for the ones
// somebody remembered.
type Extractor struct {
	doc    *Doc
	fam    Family
	vals   map[string]any
	prov   map[string]Prov
	order  []string
	missed []Miss
	dis    []Disagreement
	unread []string
}

// NewExtractor starts a record against a parsed document.
func NewExtractor(d *Doc) *Extractor {
	return &Extractor{
		doc:  d,
		fam:  d.Family(),
		vals: map[string]any{},
		prov: map[string]Prov{},
	}
}

// set records a value and where it came from. It is the only writer of vals and
// prov, and every field in every record passes through it.
//
// A second source for a field already set does not overwrite: the first source
// is the primary, and the second is recorded as a disagreement when the values
// differ and dropped when they agree.
func (e *Extractor) set(field string, v any, level Level, via string) {
	if isZero(v) {
		return
	}
	if old, seen := e.vals[field]; seen {
		if !sameValue(old, v) {
			e.dis = append(e.dis, Disagreement{Field: field, Via: via, Value: v})
		}
		return
	}
	e.vals[field] = v
	e.prov[field] = Prov{Level: level, Via: via}
	e.order = append(e.order, field)
}

// miss records a field that was looked for and not found.
func (e *Extractor) miss(field, why string) {
	e.missed = append(e.missed, Miss{Field: field, Why: why})
}

// unreadRegion records a named region present on the page that no field claims.
func (e *Extractor) unreadRegion(name string) {
	e.unread = append(e.unread, name)
}

// Region reads a named region, recording a miss when it is absent.
func (e *Extractor) Region(field, name string) Region {
	r := e.doc.Region(name)
	if !r.Exists() {
		e.miss(field, fmt.Sprintf("%s region %q not present on this page", e.fam, name))
	}
	return r
}

// Doc returns the parsed document, for the rules that need the whole page.
func (e *Extractor) Doc() *Doc { return e.doc }

// Str returns a string field, or "".
func (e *Extractor) Str(field string) string {
	s, _ := e.vals[field].(string)
	return s
}

// Float returns a numeric field, or 0.
func (e *Extractor) Float(field string) float64 {
	switch v := e.vals[field].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

// Int returns an integer field, or 0.
func (e *Extractor) Int(field string) int64 {
	switch v := e.vals[field].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

// Bool returns a boolean field.
func (e *Extractor) Bool(field string) bool {
	b, _ := e.vals[field].(bool)
	return b
}

// Strings returns a string-slice field.
func (e *Extractor) Strings(field string) []string {
	s, _ := e.vals[field].([]string)
	return s
}

// Value returns a field of any type.
func (e *Extractor) Value(field string) (any, bool) {
	v, ok := e.vals[field]
	return v, ok
}

// Has reports whether a field was set.
func (e *Extractor) Has(field string) bool { _, ok := e.vals[field]; return ok }

// Fields returns the fields that were set, in the order they were set.
func (e *Extractor) Fields() []string { return e.order }

// Prov returns where a field came from.
func (e *Extractor) Prov(field string) (Prov, bool) {
	p, ok := e.prov[field]
	return p, ok
}

// Missed returns the fields that were looked for and not found.
func (e *Extractor) Missed() []Miss { return e.missed }

// Disagreements returns the fields two sources answered differently.
func (e *Extractor) Disagreements() []Disagreement { return e.dis }

// Unread returns the named regions present on the page that no field claims,
// which is the worklist for the next version.
func (e *Extractor) Unread() []string { return e.unread }

// LevelCounts returns how many fields each rung answered.
func (e *Extractor) LevelCounts() map[Level]int {
	out := map[Level]int{}
	for _, p := range e.prov {
		out[p.Level]++
	}
	return out
}

// Envelope is the provenance block every record carries.
type Envelope struct {
	Family      Family            `json:"family"`
	Via         map[string]string `json:"via,omitempty"`
	Levels      map[string]int    `json:"levels,omitempty"`
	Missed      []Miss            `json:"missed,omitempty"`
	Disagree    []Disagreement    `json:"disagree,omitempty"`
	Unread      []string          `json:"unread,omitempty"`
	AgentMap    json.RawMessage   `json:"agent_map,omitempty"`
	RetrievedAt string            `json:"retrieved_at,omitempty"`
}

// Envelope builds the provenance block for this record.
func (e *Extractor) Envelope() Envelope {
	env := Envelope{
		Family: e.fam,
		Via:    make(map[string]string, len(e.prov)),
		Levels: map[string]int{},
	}
	for f, p := range e.prov {
		env.Via[f] = p.Via
		env.Levels[p.Level.String()]++
	}
	env.Missed = e.missed
	env.Disagree = e.dis
	if len(e.unread) > 0 {
		env.Unread = append([]string(nil), e.unread...)
		sort.Strings(env.Unread)
	}
	return env
}

// MarkUnread walks every named region on the page and records the ones no field
// read. A region that shows up here with 4 KB of text on every product is a
// field the model is missing.
func (e *Extractor) MarkUnread(claimed map[string]bool) {
	for _, name := range e.doc.SortedRegionNames() {
		if !claimed[name] {
			e.unreadRegion(name)
		}
	}
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case int:
		return x == 0
	case int64:
		return x == 0
	case bool:
		return false // false is an answer, not an absence
	case []string:
		return len(x) == 0
	}
	return false
}

func sameValue(a, b any) bool {
	af, aok := numeric(a)
	bf, bok := numeric(b)
	if aok && bok {
		return af == bf
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}
