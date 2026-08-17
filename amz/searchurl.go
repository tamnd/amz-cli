package amz

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Refinement is one rh term: a group code and the values selected in it.
//
// Values within one group are OR. Two Refinements are AND. That is the whole
// grammar of rh, measured on 2026-08-17: two brands in one group returned 476
// results where either alone returned fewer, and a brand plus a star rating
// returned 85 where the brand alone returned 96.
type Refinement struct {
	Group  string   `json:"group"`
	Label  string   `json:"label,omitempty"`
	Values []string `json:"values"`
	// ValueLabels is the human reading of Values, in the same order, and is
	// filled in once the sidebar has been read. A record whose refinements are
	// six numeric ids and no words cannot be reviewed by the person who asked
	// for it.
	ValueLabels []string `json:"value_labels,omitempty"`
}

// Term is the rh spelling of one refinement: p_123:213704|111070.
func (r Refinement) Term() string {
	vals := append([]string(nil), r.Values...)
	sort.Strings(vals)
	return r.Group + ":" + strings.Join(vals, "|")
}

// SearchQuery holds everything that turns a query string into a URL.
//
// The name-shaped fields (Brand, Seller, Stars, Condition) are requests rather
// than refinements. They carry no id, they cannot be composed into rh, and they
// are resolved against the sidebar of the page that comes back. The Refine slice
// is the resolved form and is the only thing SearchURL reads.
type SearchQuery struct {
	// Department is the i= search alias, read from the page's own dropdown.
	Department string
	// Sort is either an alias this package defines or a raw Amazon sort value.
	Sort string
	// Refine is the resolved rh, one entry per group.
	Refine []Refinement

	// The name-shaped requests, resolved against the sidebar before the walk.
	Brand     string
	Seller    string
	Stars     int
	Condition string

	// MinPrice and MaxPrice are in major units, the way a person types them.
	// They become p_36 in minor units, because p_36 composes into rh and
	// low-price and high-price are separate parameters that some paths drop.
	MinPrice float64
	MaxPrice float64

	StartPage int
	Limit     int
	// MaxPages can only lower the ceiling. See searchPageCap.
	MaxPages int
}

// sortAliases are the short names amz accepts, mapped to the values Amazon's own
// dropdown carries.
//
// The map is a convenience and not a vocabulary. A value that is not an alias is
// passed through untouched, and every search records the six options the page
// offered, so a sort Amazon renames is visible in the record rather than
// silently ignored.
var sortAliases = map[string]string{
	"relevance":    "relevanceblender",
	"featured":     "relevanceblender",
	"price-asc":    "price-asc-rank",
	"price-desc":   "price-desc-rank",
	"review":       "review-rank",
	"newest":       "date-desc-rank",
	"bestselling":  "exact-aware-popularity-rank",
	"best-sellers": "exact-aware-popularity-rank",
}

// SortValue turns an alias into the value Amazon's dropdown uses, and leaves
// anything else alone.
func SortValue(s string) string {
	if v, ok := sortAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v
	}
	return strings.TrimSpace(s)
}

// SortAliases is the alias table, for the help text and for `amz why`.
func SortAliases() map[string]string {
	out := make(map[string]string, len(sortAliases))
	for k, v := range sortAliases {
		out[k] = v
	}
	return out
}

// PriceRefinement is the p_36 term for a price range in major units.
//
// The bounds go in the marketplace's minor unit, so $50 to $150 is 5000-15000
// and ¥5000 to ¥15000 is 5000-15000 as well, because yen has no minor unit at
// all. An open end is written as an empty side, which is what Amazon's own links
// do.
func PriceRefinement(m Marketplace, lo, hi float64) (Refinement, bool) {
	if lo <= 0 && hi <= 0 {
		return Refinement{}, false
	}
	scale := math.Pow10(m.Minor)
	minor := func(v float64) string {
		if v <= 0 {
			return ""
		}
		return strconv.FormatInt(int64(v*scale+0.5), 10)
	}
	return Refinement{
		Group:  "p_36",
		Label:  "Price",
		Values: []string{minor(lo) + "-" + minor(hi)},
	}, true
}

// refinements is every rh term this query will send, price included, sorted by
// group code so the same search always produces the same URL.
func (q SearchQuery) refinements(m Marketplace) []Refinement {
	out := append([]Refinement(nil), q.Refine...)
	if p, ok := PriceRefinement(m, q.MinPrice, q.MaxPrice); ok {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Group < out[j].Group })
	return out
}

// RH is the composed rh parameter, empty when nothing is refined.
func (q SearchQuery) RH(m Marketplace) string {
	terms := q.refinements(m)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = t.Term()
	}
	return strings.Join(parts, ",")
}

// NeedsResolve reports whether this query names something the tool has to look
// up before it can build a URL.
func (q SearchQuery) NeedsResolve() bool {
	return q.Brand != "" || q.Seller != "" || q.Stars > 0 || q.Condition != ""
}

// SearchURL builds the /s URL for a query and page.
//
// Nothing Amazon adds to its own refinement links is sent: no qid, no ref, no
// rnid, no ds, no dc. A URL with none of them was fetched on 2026-08-17 and
// returned the same 85 results as the one Amazon generated, so they are
// tracking rather than routing and this tool has no business carrying them.
func (c *Client) SearchURL(query string, q SearchQuery, page int) string {
	v := url.Values{}
	v.Set("k", query)
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if s := SortValue(q.Sort); s != "" {
		v.Set("s", s)
	}
	if rh := q.RH(c.mkt); rh != "" {
		v.Set("rh", rh)
	}
	if q.Department != "" {
		v.Set("i", q.Department)
	}
	return c.BaseURL() + "/s?" + v.Encode()
}

// NormalizeSearchURL is the canonical form of a search URL.
//
// It exists once, here, because amz:us/search/<sha1> is built from its output
// and a second implementation would eventually disagree with this one. The
// symptom of that disagreement is a store holding the same search twice under
// two ids, which nothing in the store can detect and no consumer can repair.
//
// The rules, from notes/Spec/3007/07_search.md section 1:
//
//  1. lowercase the query, collapse whitespace, trim
//  2. sort the values within each rh group and sort the groups by code
//  3. keep k, i, s, low-price, high-price and rh, and nothing else
//  4. drop page, because page 3 of a search is the same search
//
// Rule 3 is an allowlist rather than a list of things to strip, because the
// parameters Amazon hangs off its own links are not a fixed set and a blocklist
// is wrong the first time it invents another one. What it drops today includes
// qid, which is a timestamp, and ds, which is a signed token, so a stored URL
// carrying either identifies the session that fetched it rather than the search.
// TestSearchURINormalisation names the ones measured in the wild.
func NormalizeSearchURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(raw))
	}
	in := u.Query()
	out := url.Values{}

	if k := in.Get("k"); k != "" {
		out.Set("k", NormalizeQuery(k))
	}
	for _, name := range []string{"i", "s", "low-price", "high-price"} {
		if v := strings.TrimSpace(in.Get(name)); v != "" {
			out.Set(name, v)
		}
	}
	if rh := in.Get("rh"); rh != "" {
		if v := normalizeRH(rh); v != "" {
			out.Set("rh", v)
		}
	}
	return out.Encode()
}

// NormalizeQuery is the query half of the rule, which is also what an unrefined
// search hashes to.
func NormalizeQuery(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}

// normalizeRH sorts an rh parameter into its canonical order.
//
// Groups repeated across the string are merged rather than kept twice: Amazon's
// own department links produce rh=n:172282,n:281407, which is one group with two
// values written as two groups, and leaving it as written would give the same
// search two identifiers.
func normalizeRH(rh string) string {
	byGroup := map[string][]string{}
	var order []string
	for _, term := range strings.Split(rh, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		code, values, ok := strings.Cut(term, ":")
		if !ok {
			continue
		}
		if _, seen := byGroup[code]; !seen {
			order = append(order, code)
		}
		byGroup[code] = append(byGroup[code], strings.Split(values, "|")...)
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, code := range order {
		vals := dedup(byGroup[code])
		sort.Strings(vals)
		parts = append(parts, code+":"+strings.Join(vals, "|"))
	}
	return strings.Join(parts, ",")
}

// SearchKey is the string the search URI is hashed from: the normalised query
// and the normalised refinements, and nothing about the page or the session.
func SearchKey(query string, q SearchQuery, m Marketplace) string {
	parts := []string{NormalizeQuery(query)}
	if q.Department != "" {
		parts = append(parts, "i="+q.Department)
	}
	if s := SortValue(q.Sort); s != "" {
		parts = append(parts, "s="+s)
	}
	if rh := normalizeRH(q.RH(m)); rh != "" {
		parts = append(parts, "rh="+rh)
	}
	return strings.Join(parts, "&")
}

// ErrRefinementIgnored is a refinement Amazon did not apply.
//
// This is an error rather than a warning because of what the alternative looks
// like. Amazon does not reject an rh group it does not recognise; it drops the
// term and serves the unfiltered result set with a 200. A tool that shrugged at
// that would hand back ten thousand unfiltered rows labelled as a filtered
// search, and nothing downstream could tell.
var ErrRefinementIgnored = errors.New("amazon did not apply the refinement")

// ErrRefinementUnoffered is a refinement the query does not offer at all.
var ErrRefinementUnoffered = errors.New("the query does not offer that refinement")

// ParseRefineFlag reads one --refine argument: p_123=213704,111070.
//
// The separator is = rather than : because a colon is what rh uses between the
// group and its values, and accepting both spellings in the flag would mean
// guessing which half of p_36:5000-15000 was the group.
func ParseRefineFlag(s string) (Refinement, error) {
	code, values, ok := strings.Cut(strings.TrimSpace(s), "=")
	if !ok {
		return Refinement{}, fmt.Errorf("--refine %q: expected <group>=<value>[,<value>], as in p_123=213704", s)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return Refinement{}, fmt.Errorf("--refine %q: no group code before the =", s)
	}
	var vals []string
	for _, v := range strings.Split(values, ",") {
		if v = strings.TrimSpace(v); v != "" {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return Refinement{}, fmt.Errorf("--refine %q: no values after the =", s)
	}
	return Refinement{Group: code, Values: vals}, nil
}

// ParsePriceFlag reads --price 50-150, 50- or -150.
func ParsePriceFlag(s string) (lo, hi float64, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("--price %q: expected <lo>-<hi>, and either side may be left empty", s)
	}
	parse := func(v string) (float64, error) {
		if v = strings.TrimSpace(v); v == "" {
			return 0, nil
		}
		return strconv.ParseFloat(v, 64)
	}
	if lo, err = parse(a); err != nil {
		return 0, 0, fmt.Errorf("--price %q: %q is not a number", s, a)
	}
	if hi, err = parse(b); err != nil {
		return 0, 0, fmt.Errorf("--price %q: %q is not a number", s, b)
	}
	if lo > 0 && hi > 0 && lo > hi {
		return 0, 0, fmt.Errorf("--price %q: the low bound is above the high one", s)
	}
	return lo, hi, nil
}
