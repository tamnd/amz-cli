package amz

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The rules a field uses once its region has been found.
//
// A rule is scoped to a region, which is what makes it rung 1: "the first h1
// inside the region Amazon named title" is a structural statement, and
// "#productTitle anywhere on the page" is a guess that happens to be right today.
//
// Every constructor here returns a Rule. Nothing in this file reaches outside
// the region it is given.

// FieldRule reads one value out of a region. The second result reports whether
// anything was found, which is what separates "not on this page" from "zero".
type FieldRule func(e *Extractor, r Region) (any, bool)

// RegionText is the collapsed text of the region itself.
func RegionText() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		t := r.Text()
		return t, t != ""
	}
}

// TextOf is the first non-empty text among scoped selectors, in order.
func TextOf(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range sels {
			if t := collapseSpace(nodeText(r.Find(sel).First())); t != "" {
				return t, true
			}
		}
		return "", false
	}
}

// AttrOf is an attribute of the first match of a scoped selector.
func AttrOf(sel, name string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		v := attrOf(r.Find(sel).First(), name)
		return v, v != ""
	}
}

// LinkText is the text of the region's first link, with the wrappers Amazon
// puts around a brand name removed.
func LinkText() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		t := collapseSpace(nodeText(r.Find("a").First()))
		t = strings.TrimPrefix(t, "Visit the ")
		t = strings.TrimPrefix(t, "Brand: ")
		t = strings.TrimSuffix(t, " Store")
		return t, t != ""
	}
}

// LinkHref is the href of the region's first link, made absolute.
func LinkHref(base string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		h := absoluteURL(base, attrOf(r.Find("a").First(), "href"))
		return h, h != ""
	}
}

// Price reads the price a screen reader would be given.
//
// .a-offscreen is the accessible rendering of the price and it carries the
// currency, the thousands separators and the decimal in one string, where the
// visible spans split them across three elements for layout. Reading the visible
// spans means reassembling a number Amazon already assembled.
//
// It answers with that string and not with a number. The separator convention is
// a property of the marketplace and not of the string, so "1.299" is one
// thousand two hundred and ninety nine euros on amazon.de and one dollar
// twenty nine on amazon.com, and only the caller knows which host served the
// page. ParseMoney does the conversion with that knowledge in hand.
func Price() FieldRule { return PriceOf(".a-offscreen", ".a-price") }

// PriceOf is Price against a different set of selectors, which the chart family
// needs: a chart tile prints its price in a p13n span and never builds the
// a-price block the rest of the site uses.
func PriceOf(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range sels {
			if t := collapseSpace(nodeText(r.Find(sel).First())); t != "" {
				if v, _ := ParsePrice(t); v > 0 {
					return t, true
				}
			}
		}
		return "", false
	}
}

// PriceCurrency reads the currency token out of the same string Price reads the
// number from.
//
// This is a field of its own rather than a side effect, because the number and
// the token are one statement and splitting them is how a price ends up labelled
// with the marketplace's currency instead of its own.
func PriceCurrency() FieldRule { return PriceCurrencyOf(".a-offscreen", ".a-price") }

// PriceCurrencyOf is PriceCurrency against a different set of selectors, and
// reads the same string PriceOf reads the number from.
func PriceCurrencyOf(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range sels {
			if t := strings.TrimSpace(nodeText(r.Find(sel).First())); t != "" {
				if _, cur := ParsePrice(t); cur != "" {
					return cur, true
				}
			}
		}
		return "", false
	}
}

// PriceLabelled finds a price on a row whose label is one of the given words,
// which is how a list price is told from the price being charged.
func PriceLabelled(labels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var found string
		r.Find(".a-row, span, div").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			t := collapseSpace(nodeText(s))
			if t == "" || len(t) > 120 {
				return true
			}
			for _, label := range labels {
				if !strings.Contains(strings.ToLower(t), strings.ToLower(label)) {
					continue
				}
				if v, _ := ParsePrice(t); v > 0 {
					found = t
					return false
				}
			}
			return true
		})
		return found, found != ""
	}
}

// StrikePrice reads the struck-through price, which is the list price when no
// row is labelled.
func StrikePrice() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range []string{
			".a-text-price .a-offscreen",
			"[data-a-strike='true'] .a-offscreen",
			".a-text-strike",
		} {
			if t := collapseSpace(nodeText(r.Find(sel).First())); t != "" {
				if v, _ := ParsePrice(t); v > 0 {
					return t, true
				}
			}
		}
		return "", false
	}
}

// Rating reads a star rating out of the text Amazon wrote for a screen reader.
//
// "4.4 out of 5 stars" is a machine readable statement of intent. The width of
// the star bar is not.
func Rating() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, get := range []func() string{
			func() string { return attrOf(r.Find("[title*='out of']").First(), "title") },
			func() string { return nodeText(r.Find(".a-icon-alt").First()) },
			func() string { return attrOf(r.Find("[aria-label*='out of']").First(), "aria-label") },
		} {
			if v := parseRating(get()); v > 0 {
				return v, true
			}
		}
		return 0.0, false
	}
}

// Count reads a count out of an aria-label first and the visible text second.
//
// The label is preferred because it is the exact figure. A detail page shows
// "(21,088)" and labels it "21,088 Reviews"; a search card shows "(1.7K)" and
// labels it "1,739 ratings". Only the label says both what the number is and
// what it counts.
//
// Every match is tried rather than only the first, because the label a card
// carries above its rating count is "4.6 out of 5 stars, rating details", and
// stopping there yields a product with four ratings and a 4.6 average. countIn
// takes the rating clause out of any label that carries one and then requires a
// digit in what is left, which drops that string and keeps the chart tile's
// "4.4 out of 5 stars, 279,167 ratings".
func Count(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var fallback int64
		for _, sel := range sels {
			var exact int64
			r.Find(sel).EachWithBreak(func(_ int, s *goquery.Selection) bool {
				if v := countIn(attrOf(s, "aria-label")); v > 0 {
					exact = v
					return false
				}
				if v := countIn(nodeText(s)); v > 0 && fallback == 0 {
					fallback = v
				}
				return true
			})
			if exact > 0 {
				return exact, true
			}
		}
		if fallback > 0 {
			return fallback, true
		}
		return int64(0), false
	}
}

// ratingClauseRe is the star rating written out in words, wherever it appears.
var ratingClauseRe = regexp.MustCompile(`(?i)[\d.]+\s*out of\s*[\d.]+\s*stars?`)

// countIn reads a count out of one string, after removing the rating that may be
// sharing it.
//
// A search card labels its stars "4.6 out of 5 stars, rating details", where the
// only number is the rating, and reading it as a count yields a product with
// four ratings. A chart tile labels the same link "4.4 out of 5 stars, 279,167
// ratings", where the count is real and sits behind the rating. Cutting the
// rating clause out and requiring a digit in what is left answers both: the
// first string keeps nothing countable, the second keeps 279,167.
func countIn(s string) int64 {
	if s == "" {
		return 0
	}
	if containsFold(s, "out of") {
		s = ratingClauseRe.ReplaceAllString(s, "")
		if !strings.ContainsAny(s, "0123456789") {
			return 0
		}
	}
	return parseCount(s)
}

// ChartRank is the rank of this tile's ASIN in the list Amazon published for its
// own renderer, which is the only source that covers the whole list.
func ChartRank() FieldRule {
	return func(e *Extractor, r Region) (any, bool) {
		asin := r.Attr("data-asin")
		if asin == "" {
			return int64(0), false
		}
		it, ok := e.Doc().chart.byASIN[asin]
		if !ok || it.Rank == 0 {
			return int64(0), false
		}
		return int64(it.Rank), true
	}
}

// BadgeRank reads the rank off the printed badge, "#1".
func BadgeRank() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range []string{".zg-bdg-text", ".zg-badge-text"} {
			if v := parseInt(nodeText(r.Find(sel).First())); v > 0 {
				return v, true
			}
		}
		return int64(0), false
	}
}

// IntAttr reads a whole number out of an attribute, which is how a card's slot
// position comes from the number Amazon put on it rather than from counting the
// cards as they go by.
func IntAttr(sel, name string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		v := parseInt(attrOf(r.Find(sel).First(), name))
		return v, v > 0
	}
}

// ListItems collects the text of every list item in the region.
func ListItems(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var out []string
		for _, sel := range sels {
			r.Find(sel).Each(func(_ int, s *goquery.Selection) {
				if t := collapseSpace(nodeText(s)); t != "" {
					out = append(out, t)
				}
			})
			if len(out) > 0 {
				break
			}
		}
		return dedup(out), len(out) > 0
	}
}

// LinkChain collects the text of every link in the region, which is a
// breadcrumb.
func LinkChain() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var out []string
		r.Find("a").Each(func(_ int, s *goquery.Selection) {
			if t := collapseSpace(nodeText(s)); t != "" {
				out = append(out, t)
			}
		})
		return out, len(out) > 0
	}
}

// HrefParam reads a query parameter out of the region's first link that carries
// it, which is how a seller profile link yields a seller id.
func HrefParam(name string) FieldRule {
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `=([A-Za-z0-9._-]+)`)
	return func(_ *Extractor, r Region) (any, bool) {
		var out string
		r.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if m := re.FindStringSubmatch(attrOf(s, "href")); m != nil {
				out = m[1]
				return false
			}
			return true
		})
		return out, out != ""
	}
}

var nodeIDRe = regexp.MustCompile(`node=(\d+)`)

// NodeIDs collects the browse node ids out of the region's links.
func NodeIDs() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var out []string
		r.Find("a").Each(func(_ int, s *goquery.Selection) {
			if m := nodeIDRe.FindStringSubmatch(attrOf(s, "href")); m != nil {
				out = append(out, m[1])
			}
		})
		out = dedup(out)
		return out, len(out) > 0
	}
}

// KeyedRow reads the value beside a label in a two-column block, which is how
// the buy box states who sells a thing and who ships it.
//
// The buy box rows have a structure of their own. Each one is a
// .offer-display-feature-label holding the wording and a
// .offer-display-feature-text-message holding the value, so the value can be
// read without reading the label at all. That is the first thing tried, and it
// is why the same rule works when the label changes wording.
//
// The labels are the fallback, for the older tabular buy box and for pages that
// never got the offer display treatment. More than one is not indecision: the
// row reads "Sold by" on the United States storefront and "Shipper / Seller"
// when the page is served without a delivery address, and both are Amazon's
// wording for the same cell.
func KeyedRow(labels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return "", false
		}
		for _, sel := range []string{
			".offer-display-feature-text-message",
			".offer-display-feature-text",
			".tabular-buybox-text",
		} {
			if v := collapseSpace(nodeText(r.Find(sel).First())); v != "" {
				return v, true
			}
		}
		full := r.Text()
		if full == "" {
			return "", false
		}
		lower := strings.ToLower(full)
		for _, label := range labels {
			i := strings.Index(lower, strings.ToLower(label))
			if i < 0 {
				continue
			}
			v := collapseSpace(full[i+len(label):])
			v = strings.TrimLeft(v, ": ")
			// The label and its value are adjacent cells, so the value ends at
			// the next label. Cut at the longest run that still looks like one
			// field.
			if j := strings.Index(v, "  "); j > 0 {
				v = v[:j]
			}
			if len(v) > 120 {
				v = v[:120]
			}
			if v = strings.TrimSpace(v); v != "" {
				return v, true
			}
		}
		return "", false
	}
}

// resultBar is the line above a search grid: "33-48 of over 20,000 results for
// "mechanical keyboard"".
type resultBar struct {
	From, To int
	Total    int64
	Approx   bool
	Query    string
}

var (
	resultRangeRe = regexp.MustCompile(`([\d,]+)\s*-\s*([\d,]+)\s+of\s+(over\s+)?([\d,]+)\s+result`)
	resultTotalRe = regexp.MustCompile(`(over\s+)?([\d,]+)\s+result`)
	resultQueryRe = regexp.MustCompile(`for\s+"([^"]*)"`)
)

// readResultBar parses the counts and the echoed query out of the bar's text.
func readResultBar(s string) (resultBar, bool) {
	var b resultBar
	if m := resultRangeRe.FindStringSubmatch(s); m != nil {
		b.From = int(parseInt(m[1]))
		b.To = int(parseInt(m[2]))
		b.Approx = m[3] != ""
		b.Total = parseInt(m[4])
	} else if m := resultTotalRe.FindStringSubmatch(s); m != nil {
		b.Approx = m[1] != ""
		b.Total = parseInt(m[2])
	} else {
		return b, false
	}
	if m := resultQueryRe.FindStringSubmatch(s); m != nil {
		b.Query = m[1]
	}
	return b, true
}

// ResultBar declares one field of the result bar.
//
// Four fields with four provenance entries rather than one struct nobody can
// count, and the split earns its keep: measured on 2026-08-17 the same query
// printed "over 20,000 results" on page 1, "over 40,000" on page 8 and "over
// 10,000" once sorted by price, while the range on either side of it stayed
// exact. A record that carried one number could not say which half to trust.
func ResultBar(pick func(resultBar) any) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		b, ok := readResultBar(r.Text())
		if !ok {
			return nil, false
		}
		return pick(b), true
	}
}

// PageNumber reads one number out of a pagination strip.
//
// "last" is the highest page Amazon will navigate to, which is a cap and not a
// count: the same query stopped at page 20 unfiltered and at page 176 with a
// department filter applied. Past the cap the grid keeps serving cards and the
// result bar starts printing ranges that run backwards, so the cap is what a
// crawl should believe.
func PageNumber(which string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		items := r.Find(".s-pagination-item")
		if items.Length() == 0 {
			return int64(0), false
		}
		var out int64
		switch which {
		case "current":
			out = parseInt(nodeText(items.Filter(".s-pagination-selected").First()))
		case "next":
			s := items.Filter(".s-pagination-next").Not(".s-pagination-disabled").First()
			out = parseInt(attrOf(s, "aria-label"))
		case "last":
			out = parseInt(nodeText(items.Filter(".s-pagination-disabled").Last()))
		}
		return out, out > 0
	}
}

var asinAttrRe = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// ASINs collects every ASIN the region names, in a data-asin attribute or an
// href.
func ASINs(exclude string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var out []string
		r.Find("[data-asin], a[href]").Each(func(_ int, s *goquery.Selection) {
			if a := attrOf(s, "data-asin"); asinAttrRe.MatchString(a) {
				out = append(out, a)
				return
			}
			if a := ExtractASIN(attrOf(s, "href")); a != "" {
				out = append(out, a)
			}
		})
		out = dedup(out)
		if exclude != "" {
			kept := out[:0]
			for _, a := range out {
				if a != exclude {
					kept = append(kept, a)
				}
			}
			out = kept
		}
		return out, len(out) > 0
	}
}

// rankLineRe reads one rank line. The third group is the parenthetical Amazon
// puts after the department rank and after nothing else, "(See Top 100 in
// Electronics)", which is the only marker on the page for which of these lines
// is the overall sales rank.
var rankLineRe = regexp.MustCompile(`#([\d,]+)\s+in\s+([^(#\n]+)(\([^)]*\))?`)

// Ranks reads every Best Sellers Rank line in the region.
func Ranks() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return nil, false
		}
		var out []Rank
		r.Find("li, tr, span").Each(func(_ int, s *goquery.Selection) {
			t := nodeText(s)
			if !strings.Contains(t, "Best Sellers Rank") && !strings.Contains(t, "Bestsellers Rank") {
				return
			}
			for _, m := range rankLineRe.FindAllStringSubmatch(t, -1) {
				n := parseInt(m[1])
				cat := cleanRankCategory(m[2])
				if n == 0 || cat == "" {
					continue
				}
				// The department rank is the one Amazon offers a Top 100 link
				// for, and the subcategory lines carry no such link. That marker
				// is read here rather than assuming the first line is the
				// department, because the order Amazon prints them in has moved
				// before and a positional assumption fails silently when it does.
				out = append(out, Rank{
					Rank:     int(n),
					Category: cat,
					Overall:  strings.Contains(m[3], "Top 100"),
					Via:      r.Name(),
				})
			}
		})
		out = dedupRanks(out)
		return out, len(out) > 0
	}
}

func dedupRanks(in []Rank) []Rank {
	seen := map[string]bool{}
	out := in[:0]
	for _, r := range in {
		k := r.Category
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// starPctRe reads one bucket out of the label Amazon writes on each histogram
// bar. The label is the whole statement: the percentage and which star it
// belongs to, in one string meant for a screen reader.
var starPctRe = regexp.MustCompile(`(\d+)\s*percent of reviews have (\d)\s*stars?`)

// RatingHistogram reads the five bucket distribution.
//
// Amazon publishes percentages and not counts, and it publishes them only in the
// aria-label. The visible cell says "73%" with no indication of which star it
// belongs to, and the bar width is a rendering. The label says both, so it is the
// only honest source on the page.
//
// The buckets are indexed one star at 0 through five stars at 4, taken from the
// label rather than from the row order, because Amazon prints five star first
// and a positional read would silently reverse the whole histogram.
func RatingHistogram() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		var pct [5]int
		found := false
		r.Find("[aria-label*='percent of reviews']").Each(func(_ int, s *goquery.Selection) {
			m := starPctRe.FindStringSubmatch(attrOf(s, "aria-label"))
			if m == nil {
				return
			}
			star, _ := strconv.Atoi(m[2])
			if star < 1 || star > 5 {
				return
			}
			n, _ := strconv.Atoi(m[1])
			pct[star-1] = n
			found = true
		})
		return pct, found
	}
}

// SpecRows reads a label and value table into a map.
func SpecRows() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return nil, false
		}
		out := map[string]string{}
		r.Find("tr").Each(func(_ int, s *goquery.Selection) {
			k := collapseSpace(nodeText(s.Find("th").First()))
			v := collapseSpace(nodeText(s.Find("td").First()))
			if k != "" && v != "" {
				out[k] = v
			}
		})
		r.Find("li").Each(func(_ int, s *goquery.Selection) {
			spans := s.Find("span.a-list-item span")
			if spans.Length() < 2 {
				return
			}
			// Amazon wraps detail bullet labels in the bidirectional marks
			// U+200E and U+200F, which are invisible and would otherwise end up
			// inside the key.
			k := collapseSpace(strings.Trim(nodeText(spans.Eq(0)), " :\u200e\u200f"))
			v := collapseSpace(nodeText(spans.Eq(1)))
			if k != "" && v != "" {
				out[k] = v
			}
		})
		return out, len(out) > 0
	}
}

// Present reports whether the region contains a match, which is how a badge
// becomes a boolean.
func Present(sel string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return false, false
		}
		return r.Find(sel).Length() > 0, true
	}
}

// ContainsText reports whether the region's text carries one of these phrases.
func ContainsText(phrases ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		t := strings.ToLower(r.Text())
		if t == "" {
			return false, false
		}
		for _, p := range phrases {
			if strings.Contains(t, strings.ToLower(p)) {
				return true, true
			}
		}
		return false, true
	}
}

// The browse family's rules.
//
// A browse tile names its identity, its slot and its badge, and leaves title,
// price, rating and delivery to dcl- classes. These rules are split that way on
// purpose: the ones reading a named thing are rung 1 or rung 3, the ones reading
// a class say so in their declaration and are counted with the other guesses.

// RatingOf is Rating against a chosen set of selectors, which the browse family
// needs: a browse tile prints its rating as a bare number in
// dcl-product-rating-value and only sometimes carries the a-icon-alt sentence
// the rest of the site uses.
func RatingOf(sels ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		for _, sel := range sels {
			if v := parseRating(collapseSpace(nodeText(r.Find(sel).First()))); v > 0 {
				return v, true
			}
		}
		return 0.0, false
	}
}

// csaPosRe reads the tile's slot out of data-csa-c-pos, which is "1,12": the
// position within the shelf and the position of the shelf on the page.
var csaPosRe = regexp.MustCompile(`^(\d+)\s*,\s*(\d+)$`)

// CSAPosition is the tile's slot in its shelf, which is Amazon stating the order
// it chose rather than this parser counting the order it happened to parse.
func CSAPosition() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		m := csaPosRe.FindStringSubmatch(r.Attr("data-csa-c-pos"))
		if m == nil {
			return int64(0), false
		}
		return parseInt(m[1]), true
	}
}

// DynamicImage is the largest rendition in the variant map Amazon puts on a
// thumbnail, which is rung 2 and beats reading the src it happened to render at.
func DynamicImage() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		best, _ := dynamicImage(r.Find("[data-a-dynamic-image]").First())
		return best, best != ""
	}
}

// badgeLine is one of the two lines Amazon draws in a deal badge, by position.
//
// The badge has no class worth reading: the label is _badgeLabel_f6hz5_1 and the
// message is _badgeMessage_f6hz5_10, both hashed per build. What is stable is the
// structure Amazon named in data-component, two children in a fixed order, so
// the rule reads the order rather than the hash.
func badgeLine(r Region, i int) string {
	kids := r.Find("[role=presentation]")
	if kids.Length() <= i {
		return ""
	}
	return collapseSpace(nodeText(kids.Eq(i)))
}

// pctRe is the figure off a badge that reads "20% off".
var pctRe = regexp.MustCompile(`(\d+)%`)

// BadgePercent is the discount Amazon printed on the badge, "20% off".
func BadgePercent() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		m := pctRe.FindStringSubmatch(badgeLine(r, 0))
		if m == nil {
			return int64(0), false
		}
		return parseInt(m[1]), true
	}
}

// BadgeMessage is the badge's second line, "Limited time deal" or "Ends in".
func BadgeMessage() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		t := badgeLine(r, 1)
		return t, t != ""
	}
}

// DiscountFromPrices is the discount the two prices imply, for the tiles Amazon
// gave no badge. It is declared as an alternate so a tile carrying both keeps
// the printed figure and records the computed one only when they disagree.
//
// It rounds, and the rounding was measured rather than assumed. Truncating put a
// disagreement on every deal tile with a badge across all three captures, always
// by exactly one and always with Amazon the higher: $219.00 off $299.00 is 26.75
// percent and Amazon prints 27. Two of those were not even a real gap, $252.00
// off $280.00 being exactly ten percent and arriving as 9.999999999999998 in
// binary floating point. Rounding agrees with Amazon on every tile measured,
// which is the point of running the cross check at all.
func DiscountFromPrices() FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		now, _ := ParsePrice(e.Str("price"))
		was, _ := ParsePrice(e.Str("was_price"))
		if now <= 0 || was <= 0 || now >= was {
			return int64(0), false
		}
		return int64(math.Round((1 - now/was) * 100)), true
	}
}

// RegionPresent answers with the fact that a named region is on the page.
//
// This is for the things Amazon names and then fills in with script. The
// countdown on a lightning deal is the case in hand: badge-countdown-timer is in
// the served HTML and the clock inside it is not, so the honest reading is that
// the deal ends soon and the time is unknown.
//
// It always answers, because for a flag the absence of the region is the answer
// rather than a failure to find one. Reporting a miss here would put "no
// countdown on this tile" in the miss list of every tile that is not a lightning
// deal, which is most of them, and a miss list that fires on the common case is
// a miss list nobody reads.
func RegionPresent() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		return r.Exists(), true
	}
}

// CanonicalNode is the browse node in the canonical link.
//
// Worth reading separately from the node that was asked for. Amazon resolves a
// browse URL to whatever node it decides the URL means, and the canonical link is
// where it says which one, so a crawl comparing the two can tell a redirect from
// a straight answer.
func CanonicalNode() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		m := nodeRe.FindStringSubmatch(attrOf(r.Find("link[rel=canonical]").First(), "href"))
		if m == nil {
			return "", false
		}
		return m[1], true
	}
}

// CanonicalSlug is the readable segment of the canonical URL,
// "electronics-store" out of /electronics-store/b?ie=UTF8&node=172282.
func CanonicalSlug() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		m := canonicalRe.FindStringSubmatch(attrOf(r.Find("link[rel=canonical]").First(), "href"))
		if m == nil {
			return "", false
		}
		return m[1], true
	}
}

// CanonicalName is the slug turned back into words, "Electronics Store".
//
// This is a derivation rather than a reading and it is labelled as one. It exists
// because no heading on a browse page carries the name: the h1 is the literal
// word "Department" on one capture and empty on another, and the title element
// holds a colon separated path rather than a name.
func CanonicalName() FieldRule {
	return func(e *Extractor, r Region) (any, bool) {
		v, _ := CanonicalSlug()(e, r)
		slug, _ := v.(string)
		if slug == "" {
			return "", false
		}
		words := strings.Split(slug, "-")
		for i, w := range words {
			if w == "" {
				continue
			}
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " "), true
	}
}

// descriptionNameRe pulls the name out of Amazon's own description sentence,
// "Online shopping from a great selection at Electronics Store."
var descriptionNameRe = regexp.MustCompile(`(?i)great selection at (.+?)\.?$`)

// DescriptionName is that name, kept as a cross check on the slug.
func DescriptionName() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		m := descriptionNameRe.FindStringSubmatch(attrOf(r.Find("meta[name=description]").First(), "content"))
		if m == nil {
			return "", false
		}
		return collapseSpace(m[1]), true
	}
}

// The store and seller families' rules.
//
// These two split cleanly and the split is the finding. A stores page keeps its
// layout in the DOM and its data in payloads, so its rules mostly reach through
// the Doc into a var config object. A seller profile keeps everything in the DOM
// under named ids, so its rules are ordinary region reads. Neither has a rung 4
// rule, which is not restraint on this parser's part, it is what the two pages
// happened to publish.

// StoreWidgetString reads one string out of a named widget's content object.
func StoreWidgetString(widget, key string) FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		w, ok := storeWidget(e.Doc().Widgets(), widget)
		if !ok {
			return "", false
		}
		var m map[string]json.RawMessage
		if !w.unmarshalContent(&m) {
			return "", false
		}
		var s string
		if json.Unmarshal(m[key], &s) != nil {
			return "", false
		}
		return collapseSpace(s), s != ""
	}
}

// StorePageContext reads one key of a stores page's page context, which is the
// block every widget repeats to say which store it belongs to.
func StorePageContext(key string) FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		v := storePageContext(e.Doc().Widgets(), key)
		return v, v != ""
	}
}

// CanonicalPageID is the storefront page UUID out of the canonical URL.
//
// A brand storefront has no id in its payloads that names the page you are
// looking at. The page context holds rootPageId, which is the landing page, so
// on any of the seven sub pages it is the id of a different page. The canonical
// URL is the only place the current page names itself.
func CanonicalPageID() FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		href, _ := e.Doc().Root().Find("link[rel=canonical]").Attr("href")
		v := storePageID(href)
		return v, v != ""
	}
}

// StoreTileString reads one string key out of the first tile of a given type.
//
// Tiles are not widgets. An editorial row is one widget holding a list of tiles,
// and the tile is where the type lives, so a lookup by widget type finds the row
// and a lookup by tile type finds the thing inside it.
func StoreTileString(tile, key string) FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		for _, t := range tilesOfType(e.Doc().Widgets(), tile) {
			var m map[string]json.RawMessage
			if json.Unmarshal(t.Content, &m) != nil {
				continue
			}
			var s string
			if json.Unmarshal(m[key], &s) == nil && s != "" {
				return collapseSpace(s), true
			}
		}
		return "", false
	}
}

// AuthorBio reads the biography out of the aboutAuthor tile.
//
// The biography arrives as an array of paragraphs rather than a string, which is
// why it gets a rule instead of a key lookup. The array is joined on a blank line
// so the paragraph breaks Amazon wrote survive into the record.
func AuthorBio() FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		for _, t := range tilesOfType(e.Doc().Widgets(), "aboutAuthor") {
			var c struct {
				Biography []string `json:"biography"`
			}
			if json.Unmarshal(t.Content, &c) != nil {
				continue
			}
			var paras []string
			for _, p := range c.Biography {
				if p = collapseSpace(p); p != "" {
					paras = append(paras, p)
				}
			}
			if len(paras) > 0 {
				return strings.Join(paras, "\n\n"), true
			}
		}
		return "", false
	}
}

// RegionAttr reads an attribute off the region's own node.
func RegionAttr(name string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		v := strings.TrimSpace(r.Attr(name))
		return v, v != ""
	}
}

// RegionHref is the first link in a region, which for the seller family is how a
// section points somewhere: the storefront link is a div whose child carries the
// href rather than the div itself.
func RegionHref() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if v := strings.TrimSpace(r.Attr("href")); v != "" {
			return v, true
		}
		v := strings.TrimSpace(attrOf(r.Find("a[href]").First(), "href"))
		return v, v != ""
	}
}

// SectionBody is a seller section's text with its own heading removed.
//
// Every page-section on a seller profile opens with its title, so the raw text
// of the about section begins "About Seller Amazon Resale Get deep discounts on
// ...". Keeping the heading would put the word "About" inside the description of
// every seller on the site.
func SectionBody(drop ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return "", false
		}
		return sectionBody(r.Sel(), drop)
	}
}

// ExpandedSectionBody is SectionBody for a section Amazon ships twice.
//
// The about block is behind a "See more" expander, and both halves are in the
// served HTML: a .spp-expander-less-content clipped to roughly 500 characters
// and a .spp-expander-more-content holding the whole thing with hide-content on
// it. Reading the section whole therefore returns the opening paragraph, then
// the same paragraph again inside the rest of the text, then the words "See
// moreSee less" where the toggle sits. Measured on 2026-08-17 that turned a
// 1,443 character seller description into a 1,963 character one with its first
// 500 characters said twice.
//
// So this narrows to the full copy where the page has one. The unrated capture
// carries a .spp-expander-more-content with no clipped twin, and the rated one
// carries both, which is why narrowing rather than deduplicating is the right
// shape: the full copy is always the full copy.
func ExpandedSectionBody(drop ...string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return "", false
		}
		sel := r.Sel()
		if full := sel.Find(".spp-expander-more-content").First(); full.Length() > 0 {
			sel = full
		}
		return sectionBody(sel, drop)
	}
}

// sectionBody is a section's text without its heading and without the widgets a
// caller names.
func sectionBody(sel *goquery.Selection, drop []string) (any, bool) {
	if len(drop) > 0 {
		// Clone before removing, because a rule that edited the shared tree
		// would change what every later field on the page sees. The clone is
		// of one section rather than the document, so it is cheap.
		sel = sel.Clone()
		sel.Find(strings.Join(drop, ", ")).Remove()
	}
	head := collapseSpace(nodeText(sel.Find("h1, h2, h3, h4, .a-text-bold").First()))
	body := collapseSpace(nodeText(sel))
	if head != "" {
		body = collapseSpace(strings.TrimPrefix(body, head))
	}
	return body, body != ""
}

// LabeledValue reads a bold label's value out of the detailed seller
// information block.
//
// The block is a run of spans rather than a table: a bold "Business Name:"
// followed by its value, then a bold "Business Address:" followed by four
// separate spans holding street, city, state and postcode. So the rule walks
// forward from the label to the next bold label and joins what it passed, which
// is the only reading that gets a whole address rather than a street.
func LabeledValue(label string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return "", false
		}
		var parts []string
		collecting := false
		r.Find("span").Each(func(_ int, s *goquery.Selection) {
			t := collapseSpace(nodeText(s))
			if t == "" || s.Find("span").Length() > 0 {
				return
			}
			if bold := s.HasClass("a-text-bold"); bold {
				collecting = strings.EqualFold(strings.TrimSuffix(t, ":"), label)
				return
			}
			if collecting {
				parts = append(parts, t)
			}
		})
		v := collapseSpace(strings.Join(parts, " "))
		return v, v != ""
	}
}

// The seller feedback rules.
//
// Measured on 2026-08-17 against /sp?seller=AKI54NNZ6PH23, a third party seller
// trading as SIKAI CASE. Amazon builds this block once and fills it four times,
// one per window, and hides three of the four with a CSS class while a dropdown
// switches between them. All four are in the served HTML, so all four are read:
// the seller measured was 5.0 across 44 ratings over twelve months and 4.8
// across 6,365 over its lifetime, and a record that kept only the headline would
// have thrown away the fact that the headline rests on 44 ratings.

// sellerPeriods maps Amazon's four window ids to the names this package uses.
// The ids are not consistent with each other, which is why they are listed
// rather than derived: thirty and ninety are words, year is a word and its
// sub-nodes are 365d, and lifetime is lifetime throughout.
var sellerPeriods = []struct{ id, name string }{
	{"thirty", "30d"},
	{"ninety", "90d"},
	{"year", "12m"},
	{"lifetime", "lifetime"},
}

// sellerPeriodBlock is the sub-tree for one window inside the feedback region.
func sellerPeriodBlock(r Region, id string) *goquery.Selection {
	return r.Find("#rating-" + id)
}

// SellerPeriodRating is the star rating for one window.
func SellerPeriodRating(id string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return 0.0, false
		}
		b := sellerPeriodBlock(r, id)
		if b.Length() == 0 {
			return 0.0, false
		}
		v := parseRating(collapseSpace(nodeText(b.Find(".ratings-reviews").First())))
		return v, v > 0
	}
}

// SellerPeriodCount is how many ratings one window is drawn from.
func SellerPeriodCount(id string) FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return int64(0), false
		}
		b := sellerPeriodBlock(r, id)
		if b.Length() == 0 {
			return int64(0), false
		}
		v := parseCount(collapseSpace(nodeText(b.Find(".ratings-reviews-count").First())))
		return v, v > 0
	}
}

// SellerFeedbackPeriods is all four windows at once.
//
// A window with no rating is dropped rather than recorded as a zero, because a
// seller trading for a month has no twelve month figure and saying it is rated
// zero over twelve months would be a different and false claim.
func SellerFeedbackPeriods() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return nil, false
		}
		var out []SellerFeedback
		for _, p := range sellerPeriods {
			b := sellerPeriodBlock(r, p.id)
			if b.Length() == 0 {
				continue
			}
			f := SellerFeedback{
				Period: p.name,
				Rating: parseRating(collapseSpace(nodeText(b.Find(".ratings-reviews").First()))),
				Count:  parseCount(collapseSpace(nodeText(b.Find(".ratings-reviews-count").First()))),
			}
			if f.Rating == 0 && f.Count == 0 {
				continue
			}
			out = append(out, f)
		}
		return out, len(out) > 0
	}
}

// SellerPositivePct is the "100% positive" figure from the header summary.
//
// It is a separate statistic from the star average rather than a restatement of
// it, and the two can disagree: the measured seller is 100 percent positive over
// twelve months and 4.8 stars over its lifetime, because they count different
// ratings over different windows.
func SellerPositivePct() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return 0.0, false
		}
		m := pctRe.FindStringSubmatch(collapseSpace(nodeText(r.Sel())))
		if m == nil {
			return 0.0, false
		}
		return toFloat(m[1]), true
	}
}

// SellerHistogram is the five row star breakdown, highest first.
//
// The percentage is read from aria-valuenow on the meter rather than from the
// text beside it. Both carry the same number on the measured page, and the
// attribute is the one that will still parse when Amazon localizes the text or
// changes where it puts the percent sign.
func SellerHistogram() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return nil, false
		}
		var out []SellerStarShare
		for stars := 5; stars >= 1; stars-- {
			row := r.Find(fmt.Sprintf("#star%d", stars))
			if row.Length() == 0 {
				continue
			}
			v, ok := row.Find("[aria-valuenow]").First().Attr("aria-valuenow")
			if !ok {
				continue
			}
			out = append(out, SellerStarShare{Stars: stars, Pct: int(parseInt(v))})
		}
		return out, len(out) > 0
	}
}

// SellerReviews is the written feedback Amazon serves in the HTML.
//
// Measured on 2026-08-17 against /sp?seller=AKI54NNZ6PH23: five rows, complete
// with stars, text, buyer and date, sitting in #feedback-table. Everything past
// those five comes from an AJAX call to /sp/ajax/feedback, and the seller in
// question has 6,365 lifetime ratings, so this is emphatically a first page.
// That is what the doc comment on Seller.Reviews is for, because five reviews
// under a rating count in the thousands is the kind of number a consumer will
// otherwise read as a fact about the seller.
//
// The counts beside the table are deliberately not read. Amazon renders
// #ttr_total_ratings_count_default and #ttr_total_feedbacks_count_default as
// literal zeros and fills them from the same AJAX call, so the served page says
// "0 total ratings, 0 with feedback for 12 months" about a seller with 44
// ratings in that window. Taking them would be reading a placeholder as data.
func SellerReviews() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return nil, false
		}
		var out []SellerReview
		// Scoped to the table rather than to the section, because the section
		// also holds #feedback-row-template, the empty row Amazon clones to
		// render the AJAX pages into.
		r.Find("#feedback-table .feedback-row").Each(func(_ int, row *goquery.Selection) {
			v := SellerReview{
				Rating: parseRating(collapseSpace(row.Find(".feedback-stars .a-icon-alt").First().Text())),
				Text:   feedbackText(row.Find(".feedback-text").First()),
			}
			// "By SAMCare, LLC on August 13, 2026." is one span holding two
			// values. Cutting on the last " on " rather than the first keeps
			// buyers whose names contain the word, and there are enough
			// companies trading as something-on-something for that to matter.
			by := collapseSpace(nodeText(row.Find(".feedback-rater").First()))
			by = strings.TrimSuffix(strings.TrimPrefix(by, "By "), ".")
			if i := strings.LastIndex(by, " on "); i >= 0 {
				v.Rater, v.Date = by[:i], by[i+len(" on "):]
			} else {
				v.Rater = by
			}
			if v.Text != "" || v.Rating > 0 {
				out = append(out, v)
			}
		})
		return out, len(out) > 0
	}
}

// feedbackText is one review's words, once.
//
// A review longer than about 160 characters is served twice, as an
// .expandable-truncated-text ending in an ellipsis and an
// .expandable-expanded-text holding the whole thing, with "Read more" and "Read
// less" links between them. Reading the row whole gives the opening sentence,
// then the same sentence again inside the full text, then the words "Read more
// Read less" on the end, which is what the about block does at section scale and
// what ExpandedSectionBody exists to undo. The expanded copy is the review.
func feedbackText(row *goquery.Selection) string {
	if full := row.Find(".expandable-expanded-text").First(); full.Length() > 0 {
		return collapseSpace(nodeText(full))
	}
	return collapseSpace(nodeText(row))
}
