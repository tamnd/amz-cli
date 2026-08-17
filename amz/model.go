package amz

import (
	"time"

	"github.com/tamnd/amz-cli/pkg/uri"
)

// The shared value types every record is built from.
//
// See notes/Spec/3007/03_model.md. The rule running through all of them is that
// absent has to stay absent: a pointer that is nil means the read did not find
// one, and a zero means the page said zero. Half the value of this package is in
// that distinction, and every pointer below is there to preserve it.

// The four pointer constructors.
//
// Every one of them turns a zero into a nil, which is the right conversion in
// exactly one direction: a rule that answered gives a non-zero value and a rule
// that did not answer gives the zero, because set never stores a zero in the
// first place. The reverse is not true and these must not be used on a value
// that could legitimately be zero, which is why there is no boolOrNil.
func f64OrNil(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func i64OrNil(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func intOrNil(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// boolPtr is for the booleans that were genuinely derived from something, where
// false is an answer rather than an absence.
func boolPtr(v bool) *bool { return &v }

// Date is a date as Amazon wrote it, plus the parse if one succeeded.
//
// Amazon writes "Reviewed in the United States on March 3, 2024" in sixteen
// locales. A failed parse must not become a zero time, because a zero time is a
// real date in January 1 year 1 and every consumer downstream will treat it as
// one. Keeping the raw string means a parser improvement can be applied to old
// records instead of requiring a refetch.
type Date struct {
	Raw    string     `json:"raw"`
	Parsed *time.Time `json:"parsed,omitempty"`
}

// NewDate keeps a date string, parsing it when the format is one we know.
func NewDate(raw string) *Date {
	raw = collapseSpace(raw)
	if raw == "" {
		return nil
	}
	d := &Date{Raw: raw}
	if t, ok := parseDisplayDate(raw); ok {
		d.Parsed = &t
	}
	return d
}

// dateLayouts are the formats Amazon writes in the marketplaces this tool reads.
// The list is deliberately short. A layout that has not been measured against a
// real page is a layout that will one day parse a different date correctly by
// accident, and a wrong date is worse than a raw string.
var dateLayouts = []string{
	"January 2, 2006",
	"2 January 2006",
	"Jan 2, 2006",
	"2006-01-02",
	"02/01/2006",
	"01/02/2006",
}

// dateLead is the wrapper Amazon puts around a review date. The country is
// carried on the record separately, so only the date itself is parsed here.
func parseDisplayDate(raw string) (time.Time, bool) {
	s := raw
	if i := indexAfter(s, " on "); i >= 0 {
		s = s[i:]
	}
	s = collapseSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func indexAfter(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i + len(sep)
		}
	}
	return -1
}

// Ref points at another entity: a brand, a browse node, a seller, an author, a
// product, or a person.
//
// Resolved is the field that earns this type its place. A review names its
// author and links to /gp/profile/, which robots.txt disallows, so amz has a
// real person's display name and no way to get an id for them. Dropping the name
// would lose data and inventing an id would be a lie, so the Ref carries the
// name with Resolved false and says exactly that.
//
// Resolved and URI are therefore not the same question. Resolved means the tool
// has the identifier Amazon issued for this thing. A review author has a URI,
// because the graph needs a node to hang an edge on, and that URI is a hash of
// the display name rather than an id anybody at Amazon would recognise, so
// Resolved stays false. It is the only kind where the two disagree.
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// URI is the marketplace-scoped identifier, amz:us/product/B075F5X8BR.
	URI string `json:"uri,omitempty"`
	URL string `json:"url,omitempty"`
	// Resolved is never omitempty. A Ref with resolved dropped from the JSON
	// and a Ref that is not resolved look identical to a consumer, and the whole
	// point of the field is to be seen.
	Resolved bool `json:"resolved"`
}

// Ref kinds, which are the kinds of the amz: identifier space.
//
// They are aliases rather than a second list of string literals, so that a kind
// this package uses and a kind pkg/uri knows how to build cannot drift apart.
const (
	RefProduct = uri.KindProduct
	RefBrand   = uri.KindBrand
	RefSeller  = uri.KindSeller
	RefNode    = uri.KindNode
	RefAuthor  = uri.KindAuthor
	RefPerson  = uri.KindPerson
	RefReview  = uri.KindReview
	RefDeal    = uri.KindDeal
)

// NewRef builds a reference and gives it an identifier when it can have one.
//
// The URI comes from pkg/uri and is never assembled here, which is the point.
// The same ASIN is a different product with a different price in every
// marketplace Amazon runs, so "amz:/product/B075F5X8BR" would be an identifier
// that quietly merges them, and one constructor is one place to make that
// impossible rather than eleven call sites to keep honest.
//
// The marketplace is ignored for the kinds whose ids are global, which is
// sellers, brands, authors, reviews and deals. Callers pass the client's
// marketplace to all of them because that is the marketplace the page was read
// in, and pkg/uri decides whether it belongs in the id.
//
// A reference the tool cannot identify keeps the name and the id it has and
// reports itself unresolved, which is what the review author does.
func NewRef(kind, marketplace, id, name, url string) *Ref {
	if id == "" && name == "" {
		return nil
	}
	r := &Ref{Kind: kind, ID: id, Name: name, URL: url}
	if id == "" {
		return r
	}
	u, err := uri.New(kind, marketplace, id)
	if err != nil {
		// The id could not be scoped, so the reference says so rather than
		// carrying an identifier that means a different thing in every
		// storefront. Nothing is dropped: the name, the id and the URL survive.
		return r
	}
	r.URI = u
	r.Resolved = true
	return r
}

// PersonRef builds the node for a review author, who has a name and no id.
//
// This is the one identifier in the space built from a display name, because
// /gp/profile/ is disallowed and is not fetched, so there is nothing else to
// build it from. Hashing the name is what makes the author a node the graph can
// hold an edge to instead of a loose string on a review.
//
// It is deliberately not resolved. Two reviewers who both call themselves
// "Amazon Customer" hash to one node and no amount of care fixes that, so the
// reference says up front that this identity is weaker than the others. The
// profile id Amazon sometimes puts in the review markup is kept on the review
// itself rather than used as the key, because a person who appears with a
// profile link on one review and without it on the next would otherwise become
// two people.
func PersonRef(name string) *Ref {
	n := collapseSpace(name)
	if n == "" {
		return nil
	}
	r := &Ref{Kind: RefPerson, Name: n}
	if u, err := uri.Person(n); err == nil {
		r.URI = u
	}
	return r
}

// NamedRef builds a reference to something named on the page whose identifier
// this tool cannot get. The review author is the case it exists for.
func NamedRef(kind, name string) *Ref {
	if collapseSpace(name) == "" {
		return nil
	}
	return &Ref{Kind: kind, Name: collapseSpace(name), Resolved: false}
}

// Conn is a partial collection: how many were loaded, how many exist, and
// whether that is all of them.
//
// Amazon ships eight reviews on a page that claims 4,812, one offer on a page
// that claims fourteen, and a dozen variation siblings on a listing that claims
// hundreds. A slice alone cannot say which of those it is, and a consumer who
// counts len() and reports it as a total is producing a wrong number with no
// warning attached.
type Conn struct {
	Loaded int `json:"loaded"`
	// TotalCount is what Amazon says exists, when the page states it.
	TotalCount *int64 `json:"total_count,omitempty"`
	// Complete is never omitempty, ever. A connection with complete false and a
	// connection whose complete field was dropped look identical in JSON, and
	// being seen is the entire job of this field.
	Complete bool `json:"complete"`
	// URL is where the rest of them are, when there is such a place.
	URL string `json:"url,omitempty"`
}

// NewConn builds a connection. total is nil when the page states no total, in
// which case Complete cannot be claimed and stays false.
func NewConn(loaded int, total *int64, url string) *Conn {
	c := &Conn{Loaded: loaded, TotalCount: total, URL: url}
	if total != nil {
		c.Complete = int64(loaded) >= *total
	}
	return c
}

// Distribution is the five bucket rating histogram.
//
// Amazon publishes integer percentages and not counts:
//
//	<a aria-label="73 percent of reviews have 5 stars">
//
// So the counts here are reconstructed as percent times total over one hundred,
// they are approximate, and Derived says so on every single record rather than
// in a document somebody has to find. Five integer percentages that sum to one
// hundred can hide up to 2.5 ratings per bucket at any total, and at ten million
// ratings that is 250,000 ratings of slack.
type Distribution struct {
	// Percent is indexed one star at 0 through five stars at 4. The order is
	// asserted by a test against a capture where the buckets are visibly
	// unequal, because an off by one reversal here is silent and wrong forever.
	Percent [5]int `json:"percent"`
	// Count is derived from Percent and Total, and is absent when there is no
	// total to derive it from.
	Count *[5]int64 `json:"count,omitempty"`
	// Derived is always true today and is stated anyway, because the day Amazon
	// starts publishing counts is the day this record needs to be able to say it
	// stopped guessing.
	Derived bool   `json:"derived"`
	Total   *int64 `json:"total,omitempty"`
	Via     string `json:"via,omitempty"`
}

// NewDistribution builds the histogram, deriving counts when a total is known.
func NewDistribution(pct [5]int, total *int64, via string) *Distribution {
	empty := true
	for _, p := range pct {
		if p != 0 {
			empty = false
		}
	}
	if empty {
		return nil
	}
	d := &Distribution{Percent: pct, Derived: true, Total: total, Via: via}
	if total != nil {
		var counts [5]int64
		for i, p := range pct {
			counts[i] = int64(p) * *total / 100
		}
		d.Count = &counts
	}
	return d
}

// Sum is the sum of the five percentages, which a caller can compare against one
// hundred to see how much rounding Amazon did.
func (d *Distribution) Sum() int {
	if d == nil {
		return 0
	}
	n := 0
	for _, p := range d.Percent {
		n += p
	}
	return n
}

// Mean is the average implied by the percentages, for reconciling against the
// rating Amazon prints. It returns 0 when the percentages sum to nothing.
func (d *Distribution) Mean() float64 {
	if d == nil {
		return 0
	}
	sum := d.Sum()
	if sum == 0 {
		return 0
	}
	weighted := 0
	for i, p := range d.Percent {
		weighted += (i + 1) * p
	}
	return float64(weighted) / float64(sum)
}

// Rank is one Best Sellers Rank line.
//
// A product is ranked once at department level and again in one to four
// subcategories. The department rank is what people mean by "sales rank", and it
// is flagged rather than assumed to be the first line, because the order Amazon
// prints them in is not guaranteed.
type Rank struct {
	Rank int `json:"rank"`
	// Node is the browse node the rank line links to. This is the strongest
	// category edge in the graph, because it is an identifier Amazon stated
	// rather than a name derived from a breadcrumb string.
	Node     *Ref   `json:"node,omitempty"`
	Category string `json:"category"`
	Overall  bool   `json:"overall"`
	Via      string `json:"via,omitempty"`
}

// Variation is the twister: the dimensions a listing varies along and the
// siblings the page happened to ship.
type Variation struct {
	ParentASIN string            `json:"parent_asin,omitempty"`
	Dimensions []Dimension       `json:"dimensions,omitempty"`
	Current    map[string]string `json:"current,omitempty"`
	Siblings   []Sibling         `json:"siblings,omitempty"`
	TotalCount *int              `json:"total_count,omitempty"`
	// Complete is never omitempty, for the same reason it is not on Conn. On a
	// large apparel listing TotalCount runs into the hundreds and the page ships
	// a dozen, so false is the normal case and it has to be visible.
	Complete bool   `json:"complete"`
	Via      string `json:"via,omitempty"`
}

// Dimension is one axis a listing varies along: Color, Size, Style.
type Dimension struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Sibling is one other ASIN in the same variation family.
type Sibling struct {
	ASIN      string            `json:"asin"`
	Values    map[string]string `json:"values,omitempty"`
	Image     string            `json:"image,omitempty"`
	Price     *Money            `json:"price,omitempty"`
	Available *bool             `json:"available,omitempty"`
}

// Rail is a recommendation strip on a detail page. It costs nothing, because the
// page carrying it has already been fetched.
type Rail struct {
	// Region is the name Amazon gave the strip: recommendations,
	// sims-simsContainer, desktop-dp-lpo.
	Region string `json:"region"`
	Title  string `json:"title,omitempty"`
	// Sponsored is never omitempty. An advertising rail and an organic rail are
	// different data, and a tool that mixes them is producing a dataset nobody
	// can use for anything.
	Sponsored bool   `json:"sponsored"`
	Cards     []Card `json:"cards"`
}

// Delivery is one shipping promise from the buy box: the free option and the
// fastest option, each with the window Amazon quoted.
type Delivery struct {
	// Kind is "free" or "fastest", which is the distinction the buy box draws.
	Kind string `json:"kind,omitempty"`
	// Text is the promise as written, "FREE delivery Tuesday, August 25".
	Text    string `json:"text,omitempty"`
	Date    *Date  `json:"date,omitempty"`
	Cost    *Money `json:"cost,omitempty"`
	Cutoff  string `json:"cutoff,omitempty"`
	ShipsTo string `json:"ships_to,omitempty"`
}

// Offer is the buy box: the one offer Amazon chose to show, with everything it
// said about that offer.
//
// Every price is a Money and every boolean is a pointer. A product whose buy box
// is missing entirely, because the item is unavailable or because Amazon served
// a page with no buy box at all, has a nil Offer, which is a different record
// from one with an Offer whose Price is nil. The first says there was nothing to
// read and the second says there was a buy box that quoted no price, which
// happens on listings that only carry other sellers.
type Offer struct {
	Price     *Money `json:"price,omitempty"`
	ListPrice *Money `json:"list_price,omitempty"`
	// Savings and SavingsPct are derived from the two above, not read off the
	// page. Amazon prints its own saving line and the two disagree often enough
	// that the derived pair is the honest one to publish.
	Savings    *Money `json:"savings,omitempty"`
	SavingsPct *int   `json:"savings_pct,omitempty"`
	// PerUnit is the unit price, "$0.28 / count", which is the only figure on the
	// page that compares two pack sizes honestly.
	PerUnit       *Money  `json:"per_unit,omitempty"`
	PerUnitLabel  string  `json:"per_unit_label,omitempty"`
	Coupon        *Coupon `json:"coupon,omitempty"`
	Subscribe     *Money  `json:"subscribe,omitempty"`
	BusinessPrice *Money  `json:"business_price,omitempty"`
	Condition     string  `json:"condition,omitempty"`
	// Availability is the line as written, and InStock is what this tool derived
	// from it. Both are kept because the derivation is a guess about phrasing in
	// sixteen locales and the string is not.
	Availability string     `json:"availability,omitempty"`
	InStock      *bool      `json:"in_stock,omitempty"`
	SoldBy       *Ref       `json:"sold_by,omitempty"`
	ShipsFrom    *Ref       `json:"ships_from,omitempty"`
	Prime        *bool      `json:"prime,omitempty"`
	Delivery     []Delivery `json:"delivery,omitempty"`
	Returns      string     `json:"returns,omitempty"`
	Via          string     `json:"via,omitempty"`
}

// Coupon is the clip-to-save promotion Amazon prints above the buy box.
type Coupon struct {
	// Display is the badge text, "Save 5% with coupon".
	Display string `json:"display"`
	Percent *int   `json:"percent,omitempty"`
	Amount  *Money `json:"amount,omitempty"`
	Via     string `json:"via,omitempty"`
}
