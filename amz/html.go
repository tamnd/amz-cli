package amz

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// newDocument parses an HTML body and strips the nodes whose text content would
// otherwise pollute a field: inline scripts and styles concatenate into
// goquery's .Text(), so a block like #availability that carries an AOD loader
// script leaks JavaScript into the value.
//
// Every script goes, including the ones amz reads. The rung 2 payloads are
// found by byte offset in the raw body before any DOM work, per
// notes/Spec/3007/02_extraction.md section 9, so a light read that only needs a
// title and a price never touches ImageBlockATF and no region's text is
// polluted by an a-state blob that happens to sit inside it.
func newDocument(body []byte) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	doc.Find(`script, style, noscript`).Remove()
	return doc, nil
}

var (
	// numRe and intRe both have to start on a digit. Matching a run of digits
	// and separators alone means "Go to next page, page 2" matches the comma
	// after the first "page", which strips to the empty string and parses as
	// zero, so a pagination strip that plainly says 2 reads as no next page.
	numRe    = regexp.MustCompile(`\d[\d.,]*|\.\d+`)
	intRe    = regexp.MustCompile(`\d[\d,]*`)
	ratingRe = regexp.MustCompile(`([\d.]+)\s*out of`)
	// glyphRe is the symbol form of a currency.
	glyphRe = regexp.MustCompile(`[\$£€¥₹₩₫₺₪₱฿]`)
	// isoRe is the code form. amazon.com does not only quote dollars: fetched
	// without a delivery address from outside the United States it prices in the
	// caller's currency, and a capture taken on 2026-08-17 came back reading
	// "VND208,927". Listing the codes we happen to have seen is how the next
	// unlisted one becomes a price with no currency on it, so this matches any
	// three-letter code that is not part of a longer word.
	isoRe = regexp.MustCompile(`(^|[^A-Za-z])([A-Z]{3})([^A-Za-z]|$)`)
)

var currencyByGlyph = map[string]string{
	"$": "USD", "£": "GBP", "€": "EUR", "¥": "JPY",
	"₹": "INR", "₩": "KRW", "₫": "VND", "₺": "TRY", "₪": "ILS", "₱": "PHP", "฿": "THB",
}

// isoIn reads an explicit three-letter currency code out of a string, and only
// that. It is the half of currencyIn that cannot be wrong: a glyph needs a
// marketplace to disambiguate it and a code does not.
func isoIn(s string) string {
	if m := isoRe.FindStringSubmatch(s); m != nil {
		return m[2]
	}
	return ""
}

// currencyIn reads the currency out of a price string, preferring an explicit
// three-letter code over a glyph because "$" is ambiguous across half a dozen
// marketplaces and "CAD" is not.
func currencyIn(s string) string {
	if m := isoRe.FindStringSubmatch(s); m != nil {
		return m[2]
	}
	if m := glyphRe.FindString(s); m != "" {
		if code, ok := currencyByGlyph[m]; ok {
			return code
		}
		return m
	}
	return ""
}

// ParsePrice extracts a numeric price and a best-effort currency code from a
// display string like "$1,299.00" or "1.299,00 €".
func ParsePrice(s string) (float64, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}
	cur := currencyIn(s)
	num := numRe.FindString(s)
	if num == "" {
		return 0, cur
	}
	// Handle European "1.299,00" vs US "1,299.00": if both separators present,
	// the rightmost is the decimal separator.
	lastDot := strings.LastIndex(num, ".")
	lastComma := strings.LastIndex(num, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot { // European
			num = strings.ReplaceAll(num, ".", "")
			num = strings.Replace(num, ",", ".", 1)
		} else { // US
			num = strings.ReplaceAll(num, ",", "")
		}
	case lastComma >= 0:
		// Ambiguous single comma: treat as thousands if 3 trailing digits, else decimal.
		if len(num)-lastComma-1 == 3 {
			num = strings.ReplaceAll(num, ",", "")
		} else {
			num = strings.Replace(num, ",", ".", 1)
		}
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, cur
	}
	return v, cur
}

// abbrevRe is Amazon's shortened count: "(1.7K)" on a search card, "(2.3M)" on a
// chart tile.
var abbrevRe = regexp.MustCompile(`([\d.,]+)\s*([KMB])\b`)

// parseCount reads a count that may be written out in full or abbreviated.
//
// A search card prints "(1.7K)" and labels the same link "1,739 ratings".
// Reading the visible text with parseInt gives 1, which is not a rounding error
// but a number three orders of magnitude out, so the abbreviation is expanded
// rather than truncated. The exact figure is still preferred wherever the page
// states one.
func parseCount(s string) int64 {
	if m := abbrevRe.FindStringSubmatch(s); m != nil {
		v := toFloat(m[1])
		switch m[2] {
		case "K":
			v *= 1e3
		case "M":
			v *= 1e6
		case "B":
			v *= 1e9
		}
		return int64(v)
	}
	return parseInt(s)
}

// parseInt pulls the first integer (with thousands separators) out of a string.
func parseInt(s string) int64 {
	m := intRe.FindString(s)
	if m == "" {
		return 0
	}
	m = strings.ReplaceAll(m, ",", "")
	v, _ := strconv.ParseInt(m, 10, 64)
	return v
}

// toFloat reads a decimal number out of a string that may carry thousands
// separators, and returns 0 when there is no number in it.
func toFloat(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	m := numRe.FindString(s)
	if m == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(m, 64)
	return v
}

// round2 rounds to two decimals, the precision Amazon prices carry.
func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// parseRating pulls "4.5" out of "4.5 out of 5 stars".
func parseRating(s string) float64 {
	if m := ratingRe.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		return v
	}
	// fall back to a leading float
	if m := regexp.MustCompile(`^[\d.]+`).FindString(strings.TrimSpace(s)); m != "" {
		v, _ := strconv.ParseFloat(m, 64)
		return v
	}
	return 0
}

// The three whole-document helpers that used to live here are gone: text,
// firstNonEmptyText and attr. Every one of them took a CSS selector and ran it
// against the entire page, which is rung 4 with no region and no record of where
// the answer came from, and the store, seller and author parsers were their last
// callers. Rewiring those three onto the region model left nothing behind.
//
// They are worth naming rather than deleting silently, because the shape they
// encouraged is the one this package is built against. A selector against the
// whole document will happily find a match in the footer and report it as the
// page's content, which is exactly what it did.

// attrOf returns a trimmed attribute value of a single selection.
func attrOf(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.TrimSpace(v)
}

// blockTags names the elements whose text does not run into the next one.
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "br": true,
	"dd": true, "div": true, "dl": true, "dt": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true, "hr": true, "li": true, "main": true,
	"nav": true, "ol": true, "option": true, "p": true, "pre": true, "section": true,
	"table": true, "tbody": true, "td": true, "th": true, "thead": true, "tr": true, "ul": true,
}

// nodeText is the text of a selection with a space wherever the markup put a
// line break.
//
// goquery joins text across element boundaries with nothing in between, so a
// delivery block laid out as two rows reads back as
// "VND 946,576 deliveryShips to Vietnam". That is not a cosmetic problem. The
// run-together form has no boundary left for anything downstream to split on,
// and "deliveryShips" is a word that appears nowhere on the page.
func nodeText(s *goquery.Selection) string {
	var b strings.Builder
	for _, n := range s.Nodes {
		writeNodeText(&b, n)
	}
	return b.String()
}

func writeNodeText(b *strings.Builder, n *html.Node) {
	switch {
	case n.Type == html.TextNode:
		b.WriteString(n.Data)
		return
	case n.Type != html.ElementNode:
	case blockTags[n.Data]:
		b.WriteByte(' ')
		defer b.WriteByte(' ')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeNodeText(b, c)
	}
}

// collapseSpace squeezes runs of whitespace into single spaces.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// absoluteURL resolves a possibly-relative href against the marketplace base.
func absoluteURL(base, href string) string {
	href = strings.TrimSpace(href)
	switch {
	case href == "":
		return ""
	case strings.HasPrefix(href, "http://"), strings.HasPrefix(href, "https://"):
		return href
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/"):
		return base + href
	default:
		return base + "/" + href
	}
}

// firstNonEmpty is the first argument that is not empty, which is how a field
// with a marketplace default is filled without an if for every one.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstSelText is the first non-empty text among scoped selectors, treating an
// [alt] selector as a request for that attribute.
//
// This is the shape of the parsers that predate the field registry. The families
// that have been rewired declare a rule instead; this remains for the ones that
// have not, and the count of its callers is the size of what is left to do.
func firstSelText(s *goquery.Selection, sels ...string) string {
	for _, sel := range sels {
		if strings.Contains(sel, "[alt]") {
			if v, ok := s.Find(sel).First().Attr("alt"); ok && strings.TrimSpace(v) != "" {
				return v
			}
			continue
		}
		if t := strings.TrimSpace(s.Find(sel).First().Text()); t != "" {
			return t
		}
	}
	return ""
}

func attrSel(s *goquery.Selection, sel, name string) string {
	v, _ := s.Find(sel).First().Attr(name)
	return v
}
