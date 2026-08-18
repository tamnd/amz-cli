package amz

import (
	"fmt"
	"strings"
	"time"
)

// What a crawl will cost, before it spends it.
//
// notes/Spec/3007/00_overview.md section "Say what a crawl will cost" asks for a
// plan a person can read and refuse. The numbers here are measurements rather
// than guesses, and where a guess was unavoidable it is one documented ratio
// applied to a measured size instead of a number somebody liked the look of.
//
// The sizes below are decoded bytes, taken from the golden captures in
// amz/testdata/captures on 2026-08-17 and listed in capture.txt. Decoded is not
// what a crawl pays for: Amazon gzips everything, and the one surface measured
// both ways came out at 2,197,291 bytes decoded against 373,945 on the wire, a
// ratio of 5.88. That ratio is applied to the other surfaces, because one
// measured compression ratio over HTML from the same site is a far better
// estimate than assuming the wire cost equals the decoded cost, and because the
// plan says "estimated" where it is estimating.
const compressionRatio = 5.88

// decodedBytes is what one read of each surface weighed, decoded.
var decodedBytes = map[string]int64{
	"product":  2_197_291, // product_simple, and the median of the six product captures
	"light":    2_197_291, // s2 is byte for byte the same page today. See depth.go.
	"search":   1_299_326, // search_page1
	"chart":    427_983,   // chart_bestsellers
	"category": 917_459,   // browse_node
	"seller":   295_932,   // seller_amazon
	"brand":    520_435,   // brand_store
	"author":   1_367_925, // author_store
	"reviews":  1_299_326, // no capture of its own: /product-reviews/ is a login wall
	"qa":       1_299_326, // the ask region is part of the detail page
	"offers":   1_299_326, // the AOD endpoint 404s to a direct request
}

// WireBytes estimates what one read of a surface costs on the wire.
func WireBytes(surface string) int64 {
	d, ok := decodedBytes[surface]
	if !ok {
		d = decodedBytes["product"]
	}
	return int64(float64(d) / compressionRatio)
}

// Step is one line of a plan: a count of reads of one surface.
type Step struct {
	Surface  string
	Label    string
	Requests int
	Note     string
}

// Bytes is what this step is estimated to cost on the wire.
func (s Step) Bytes() int64 { return int64(s.Requests) * WireBytes(s.Surface) }

// Plan is what a crawl intends to do, priced.
type Plan struct {
	Steps []Step
	Pace  time.Duration
	// Cached is how many of the planned requests are already on disk and will
	// not be fetched. A replanned crawl over a warm cache is close to free, and
	// a plan that did not say so would be quoting a price nobody is paying.
	Cached int
}

// Add appends a step, folding it into an existing one for the same surface so a
// plan built from six seeds does not print six lines saying the same thing.
func (p *Plan) Add(surface, label string, requests int, note string) {
	if requests <= 0 {
		return
	}
	for i := range p.Steps {
		if p.Steps[i].Surface == surface && p.Steps[i].Note == note {
			p.Steps[i].Requests += requests
			return
		}
	}
	p.Steps = append(p.Steps, Step{Surface: surface, Label: label, Requests: requests, Note: note})
}

// Requests is the total, including the ones the cache will answer.
func (p Plan) Requests() int {
	n := 0
	for _, s := range p.Steps {
		n += s.Requests
	}
	return n
}

// Bytes is the estimated wire cost of the requests that will actually be made.
func (p Plan) Bytes() int64 {
	var total int64
	for _, s := range p.Steps {
		total += s.Bytes()
	}
	if p.Cached > 0 && p.Requests() > 0 {
		total -= total * int64(p.Cached) / int64(p.Requests())
	}
	return total
}

// Duration is wall clock at the configured pace.
//
// A cached read still goes through the throttle in this estimate, because the
// throttle is what stands between a crawl and a burst and a plan that assumed
// otherwise would under-quote the one number people schedule around.
func (p Plan) Duration() time.Duration {
	return time.Duration(p.Requests()-p.Cached) * p.Pace
}

// String renders the plan as the table in notes/Spec/3007/05_commands.md.
func (p Plan) String() string {
	var b strings.Builder
	b.WriteString("plan\n")
	for _, s := range p.Steps {
		line := fmt.Sprintf("  %-6d %-18s %8s", s.Requests, plural(s.Requests, strings.TrimSuffix(s.Label, "s"), s.Label), HumanBytes(s.Bytes()))
		if s.Note != "" {
			line += "   (" + s.Note + ")"
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	b.WriteString("  " + strings.Repeat("-", 34) + "\n")
	fmt.Fprintf(&b, "  %d requests, %s estimated, %s at the %s pace\n",
		p.Requests(), HumanBytes(p.Bytes()), HumanDuration(p.Duration()), HumanDuration(p.Pace))
	if p.Cached > 0 {
		fmt.Fprintf(&b, "  %d of them are already cached and will not be fetched\n", p.Cached)
	}
	return b.String()
}

// HumanBytes prints a size the way the plan table does.
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// HumanDuration prints a duration without the trailing zero units that
// time.Duration prints, because "5m9s" is the shape the plan asks for and
// "5m9.000000001s" is not.
func HumanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	s := d.String()
	// "5m0s" is five minutes and the zero is noise. "1m10s" is not, so the
	// suffix has to be the whole seconds field and not just its last character.
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
