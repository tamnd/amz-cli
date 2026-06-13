package amz

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// newDocument parses an HTML body and strips the nodes whose text content would
// otherwise pollute a field: inline scripts and styles concatenate into
// goquery's .Text(), so a block like #availability that carries an AOD loader
// script would leak JavaScript into the value. JSON-LD scripts are kept because
// the product parser reads structured data out of them.
func newDocument(body []byte) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	doc.Find(`script:not([type="application/ld+json"]), style, noscript`).Remove()
	return doc, nil
}

var (
	numRe      = regexp.MustCompile(`[\d.,]+`)
	intRe      = regexp.MustCompile(`[\d,]+`)
	ratingRe   = regexp.MustCompile(`([\d.]+)\s*out of`)
	currencyRe = regexp.MustCompile(`[\$£€¥]|USD|GBP|EUR|JPY|CAD|INR|AUD`)
)

var currencyByGlyph = map[string]string{
	"$": "USD", "£": "GBP", "€": "EUR", "¥": "JPY",
}

// ParsePrice extracts a numeric price and a best-effort currency code from a
// display string like "$1,299.00" or "1.299,00 €".
func ParsePrice(s string) (float64, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}
	cur := ""
	if m := currencyRe.FindString(s); m != "" {
		if code, ok := currencyByGlyph[m]; ok {
			cur = code
		} else {
			cur = m
		}
	}
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

// text returns the trimmed text of the first match of sel, or "".
func text(doc *goquery.Document, sel string) string {
	return strings.TrimSpace(doc.Find(sel).First().Text())
}

// firstNonEmptyText tries each selector in order and returns the first non-empty text.
func firstNonEmptyText(doc *goquery.Document, sels ...string) string {
	for _, sel := range sels {
		if t := text(doc, sel); t != "" {
			return t
		}
	}
	return ""
}

// attr returns the attribute value of the first match of sel.
func attr(doc *goquery.Document, sel, name string) string {
	v, _ := doc.Find(sel).First().Attr(name)
	return strings.TrimSpace(v)
}

// attrOf returns a trimmed attribute value of a single selection.
func attrOf(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.TrimSpace(v)
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
