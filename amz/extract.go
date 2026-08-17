package amz

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
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
//
// Have and Total carry the partial case: eight reviews on a page that says
// 4,812. Fix names the command that would get the rest, when one exists, because
// a report of a gap that does not say how to close it is only half useful.
type Miss struct {
	Field    string   `json:"field"`
	Why      string   `json:"why"`
	Have     int      `json:"have,omitempty"`
	Total    int64    `json:"total,omitempty"`
	Surfaces []string `json:"surfaces,omitempty"`
	Fix      string   `json:"fix,omitempty"`
}

// Disagreement is one field answered differently by two sources.
//
// Not a defect to hide. On Amazon the buy box price and the apex price genuinely
// differ when a subscription discount or a coupon applies, and a tool that
// silently picks one is throwing away the interesting part.
//
// Both halves are recorded: the value the record kept with the source that gave
// it, and the value that lost with the source it came from. A disagreement that
// only names the loser cannot be checked without refetching the page.
type Disagreement struct {
	Field    string `json:"field"`
	Via      string `json:"via"`
	Value    any    `json:"value"`
	Alt      string `json:"alt"`
	AltValue any    `json:"alt_value"`
}

// Source is one HTTP response that went into a record.
//
// A product record built from the detail page alone and one built from the
// detail page plus a seller page are different reads, and a consumer deciding
// whether to trust a field needs to know which surfaces were actually fetched
// rather than which ones the command name implies.
type Source struct {
	Surface     string    `json:"surface,omitempty"`
	URL         string    `json:"url"`
	Status      int       `json:"status,omitempty"`
	Bytes       int       `json:"bytes,omitempty"`
	Cached      bool      `json:"cached"`
	RetrievedAt time.Time `json:"retrieved_at,omitzero"`
}

// RobotsNote records that a robots.txt rule was involved in this read. It is
// present only when one was, because a note on every record saying nothing
// happened is noise that trains people to skip the field.
type RobotsNote struct {
	Allowed bool   `json:"allowed"`
	Rule    string `json:"rule,omitempty"`
	// Override is true when --no-robots was used, which is the one thing about a
	// record that a downstream consumer most needs to be able to see.
	Override bool `json:"override"`
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
			// Both halves, named. The record kept old from the source already in
			// prov, and via disagreed with v. Recording only the loser meant a
			// reader had to refetch the page to find out what was kept.
			e.dis = append(e.dis, Disagreement{
				Field:    field,
				Via:      e.prov[field].Via,
				Value:    old,
				Alt:      via,
				AltValue: v,
			})
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

// missSurface records a field whose data lives on a surface this fetch did not
// read, naming the surfaces so the entry is checkable rather than a shrug.
//
// This is the second of the four states and the one a consumer is most likely to
// mistake for the first. A record with no reviews because the corpus is behind a
// sign-in and a record with no reviews because the product has none are the same
// JSON, and the only thing that tells them apart is this entry.
func (e *Extractor) missSurface(field, why string, surfaces []string, fix string) {
	e.missed = append(e.missed, Miss{Field: field, Why: why, Surfaces: surfaces, Fix: fix})
}

// missPartial records a field that was found and is incomplete: eight reviews on
// a page that says 4,812, a dozen variation siblings on a listing that claims
// hundreds.
//
// This is the third of the four states of a missing field and the one most
// likely to be read as the first. A consumer who counts len() on a partial slice
// and publishes it as a total has produced a wrong number with nothing to warn
// them, so the entry carries both figures and the command that would close the
// gap when there is one.
func (e *Extractor) missPartial(field string, have int, total int64, why, fix string) {
	e.missed = append(e.missed, Miss{Field: field, Why: why, Have: have, Total: total, Fix: fix})
}

// liveMisses drops the entries for fields that another source went on to fill.
//
// A book has no corePrice region and puts its price in the books team's own buy
// box, so the first price rule records a miss and the second rule answers. Both
// facts are true and only one of them is useful: the record has a price, and a
// missed entry naming a field that is right there in the record teaches a reader
// to ignore the whole list.
//
// A partial is never dropped. It exists precisely because the field is set and
// the value is short of the total, which is the one case where a field being
// present and a field being incomplete are both worth saying.
func (e *Extractor) liveMisses() []Miss {
	var out []Miss
	for _, m := range e.missed {
		if _, ok := e.vals[m.Field]; ok && m.Have == 0 && m.Total == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
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
//
// The consumer facing rule this exists to support is one sentence: if a field is
// absent and there is no Missed entry naming it, the tool looked and there was
// nothing there. Everything else in here is what makes that sentence checkable.
type Envelope struct {
	Family      Family `json:"family"`
	Marketplace string `json:"marketplace,omitempty"`
	// Surfaces names which of the surfaces in Ops were read, and Sources is the
	// responses themselves.
	Surfaces []string `json:"surfaces,omitempty"`
	Sources  []Source `json:"sources,omitempty"`
	// Depth is which of quick, meta, full and deep produced this record. A field
	// that is absent from a quick read and present in a full one is not a parser
	// that broke between two crawls, and this is the field that says so.
	Depth string `json:"depth,omitempty"`
	// Via maps a field to the region, payload or selector that produced it, and
	// Level maps it to the rung that was. Levels is the same information counted,
	// which is what the ladder report reads.
	Via      map[string]string `json:"via,omitempty"`
	Level    map[string]int    `json:"level,omitempty"`
	Levels   map[string]int    `json:"levels,omitempty"`
	Missed   []Miss            `json:"missed,omitempty"`
	Disagree []Disagreement    `json:"disagree,omitempty"`
	Unread   []string          `json:"unread,omitempty"`
	Robots   *RobotsNote       `json:"robots,omitempty"`
	AgentMap json.RawMessage   `json:"agent_map,omitempty"`
	// Extra carries payloads the page shipped that no field reads yet, verbatim.
	Extra       map[string]json.RawMessage `json:"extra,omitempty"`
	RetrievedAt time.Time                  `json:"retrieved_at,omitzero"`
}

// Envelope builds the provenance block for this record.
func (e *Extractor) Envelope() Envelope {
	env := Envelope{
		Family: e.fam,
		Via:    make(map[string]string, len(e.prov)),
		Level:  make(map[string]int, len(e.prov)),
		Levels: map[string]int{},
	}
	for f, p := range e.prov {
		env.Via[f] = p.Via
		env.Level[f] = int(p.Level)
		env.Levels[p.Level.String()]++
	}
	env.Missed = e.liveMisses()
	env.Disagree = e.dis
	if len(e.unread) > 0 {
		env.Unread = append([]string(nil), e.unread...)
		sort.Strings(env.Unread)
	}
	return env
}

// AddSource records a response that went into this record.
func (env *Envelope) AddSource(s Source) {
	env.Sources = append(env.Sources, s)
	if s.Surface != "" && !containsString(env.Surfaces, s.Surface) {
		env.Surfaces = append(env.Surfaces, s.Surface)
	}
	if env.RetrievedAt.IsZero() || s.RetrievedAt.After(env.RetrievedAt) {
		env.RetrievedAt = s.RetrievedAt
	}
}

// Inherit copies a page's sources and robots note onto a row that was read out
// of that same response.
//
// A search result and a chart entry are emitted one per line and are what a
// consumer actually holds, so "which response is this from" has to be on the row
// and not only on the page record that produced it and was never printed.
func (env *Envelope) Inherit(parent Envelope) {
	for _, s := range parent.Sources {
		env.AddSource(s)
	}
	if env.Robots == nil {
		env.Robots = parent.Robots
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
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
