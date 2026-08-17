// Package asin reads Amazon product identifiers out of whatever a person typed.
//
// There is one parser here and every command uses it, because an id is the one
// thing in this tool that must mean the same thing everywhere. A bare ASIN, a
// share link with forty characters of tracking on it, a title slug URL, an ISBN
// with dashes in it and an amz: URI are all the same product being named five
// ways, and a tool that accepted four of the five would be answering "not found"
// to a question it understood.
//
// The other half of the job is the marketplace. An ASIN is unique inside one
// storefront and nowhere else: B075F5X8BR on amazon.com and B075F5X8BR on
// amazon.co.uk are two listings with different prices, different sellers and
// occasionally different products. So a URL that names a storefront carries that
// storefront out of the parser, and the caller is expected to prefer it over
// whatever default was configured.
package asin

import (
	"errors"
	"maps"
	"net/url"
	"regexp"
	"strings"
)

// Kind is what an identifier turned out to be.
type Kind string

const (
	// KindASIN is Amazon's own ten character identifier.
	KindASIN Kind = "asin"
	// KindISBN10 is a ten digit book number whose check digit verifies. For a
	// print book Amazon uses it as the ASIN, so an ISBN-10 is both.
	KindISBN10 Kind = "isbn10"
)

// ID is one identified product.
type ID struct {
	// Value is the identifier as Amazon writes it: ten characters, uppercase,
	// no dashes.
	Value string
	// Kind says whether the value verified as an ISBN-10 as well as an ASIN.
	Kind Kind
	// Marketplace is the storefront the input named, and is empty when the input
	// was a bare id that could belong to any of them. It is never guessed.
	Marketplace string
	// Host is the hostname a URL input carried, kept even when it is not a
	// storefront this tool knows. An error that can say "shop.amazon.example is
	// not a marketplace amz knows" is worth the field.
	Host string
	// ISBN13 is filled for an ISBN-10, computed rather than copied.
	ISBN13 string
	// Raw is what was parsed, kept so an error message can quote the input
	// rather than the cleaned up version of it.
	Raw string
}

// ErrNotAnID is returned when nothing in the input is an Amazon identifier.
var ErrNotAnID = errors.New("not an ASIN, an ISBN or an amazon product URL")

// asinChars is the ten character shape. Amazon's own ids start B0 and books use
// the ISBN, which is nine digits and a check character, so the shape is
// deliberately loose and the check digit does the narrowing.
var asinChars = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// pathMarkers are the URL paths that put an id in the next segment. Every one of
// these is a page amz reads or a page a person will paste from.
var pathMarkers = regexp.MustCompile(`(?:^|/)(?:dp|gp/product|gp/aw/d|product-reviews|gp/offer-listing|ask/questions/asin|gp/customer-reviews)/([A-Za-z0-9]{10})(?:[/?#]|$)`)

// uriPattern matches an amz: product URI, which is the shape this tool prints.
// Anything else in that space is not a product and is not this parser's job.
var uriPattern = regexp.MustCompile(`^amz:([a-z]{2})/product/([A-Za-z0-9]{10})$`)

// Normalize strips the punctuation people paste with an id and uppercases it.
//
// ISBNs are written with dashes and spaces and copied out of citations with
// both. The dashes carry no information: they group the registrant and the
// publisher and the position of the groups varies by country, so removing them
// loses nothing that the number itself does not already say.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '-' || r == ' ' || r == ' ' || r == '_':
			continue
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsASIN reports whether s is an ASIN written the way Amazon writes one: ten
// uppercase alphanumerics and nothing else.
//
// It does not normalize first, and IsISBN10 below does, which is a difference
// worth explaining. This function's caller is usually checking a string pulled
// out of markup, and there a lowercase ten character token is a selector that
// picked up a URL slug rather than an id somebody typed casually. Being strict
// is what stops those reaching a record. Parse normalizes before it gets here,
// so a person typing at a shell still gets the lenient behaviour.
//
// This is a shape test and not an existence test. Every ISBN-10 is also this
// shape, which is not a flaw: for a print book the two are the same string.
func IsASIN(s string) bool { return asinChars.MatchString(s) }

// IsISBN10 reports whether s is a ten digit book number whose check digit is
// right. Dashes and spaces are stripped first, because an ISBN is a thing people
// write on paper and copy out of citations with the grouping intact.
//
// The check digit is computed and never assumed. A ten character alphanumeric
// string that looks like an ISBN is common, because every B0 ASIN is one, and an
// export that carries a made up ISBN-13 for a pair of earbuds is worse than one
// that carries no ISBN at all.
func IsISBN10(s string) bool {
	n := Normalize(s)
	if len(n) != 10 {
		return false
	}
	sum := 0
	for i, r := range n {
		var d int
		switch {
		case r >= '0' && r <= '9':
			d = int(r - '0')
		case r == 'X' && i == 9:
			// X is ten, and only in the check position, because that is the only
			// place a base eleven digit can appear.
			d = 10
		default:
			return false
		}
		sum += (10 - i) * d
	}
	return sum%11 == 0
}

// IsISBN13 reports whether s is a thirteen digit book number whose check digit
// is right.
func IsISBN13(s string) bool {
	n := Normalize(s)
	if len(n) != 13 {
		return false
	}
	sum := 0
	for i, r := range n {
		if r < '0' || r > '9' {
			return false
		}
		d := int(r - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return sum%10 == 0
}

// ISBN13From10 converts a verified ISBN-10 to its ISBN-13, and returns "" for
// anything that is not one.
//
// The conversion is a prefix and a different check digit, not a reformatting:
// the ISBN-10 check digit is base eleven over a weighted sum and the ISBN-13 one
// is base ten over a different weighting, so the last character is recomputed
// from scratch. Refusing to convert an unverified input is the point of the
// function, because the failure mode is a plausible looking number that no
// bookseller has.
func ISBN13From10(s string) string {
	if !IsISBN10(s) {
		return ""
	}
	body := "978" + Normalize(s)[:9]
	sum := 0
	for i, r := range body {
		d := int(r - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return body + string(rune('0'+(10-sum%10)%10))
}

// Parse reads an identifier out of a bare id, an amazon URL or an amz: URI.
//
// A URL is not required to be well formed beyond having a path, because the
// thing people paste is a share link and the interesting part is the segment
// after /dp/. Query strings are ignored entirely: ref, qid, keywords, th and psc
// are Amazon's own tracking and none of them changes which product is named.
func Parse(s string) (ID, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return ID{}, ErrNotAnID
	}

	if m := uriPattern.FindStringSubmatch(raw); m != nil {
		return newID(m[2], m[1], raw)
	}

	if looksLikeURL(raw) {
		u, err := url.Parse(raw)
		if err != nil {
			return ID{}, ErrNotAnID
		}
		m := pathMarkers.FindStringSubmatch(u.Path)
		if m == nil {
			return ID{}, ErrNotAnID
		}
		id, err := newID(m[1], MarketplaceForHost(u.Host), raw)
		if err != nil {
			return id, err
		}
		id.Host = hostname(u.Host)
		return id, nil
	}

	// A bare id names no storefront, so the marketplace stays empty and the
	// caller's configured one applies. Saying "us" here would be inventing the
	// one fact the input did not contain.
	if n := Normalize(raw); asinChars.MatchString(n) {
		return newID(n, "", raw)
	}
	return ID{}, ErrNotAnID
}

// newID fills in what the identifier turned out to be.
func newID(value, marketplace, raw string) (ID, error) {
	v := Normalize(value)
	if !asinChars.MatchString(v) {
		return ID{}, ErrNotAnID
	}
	id := ID{Value: v, Kind: KindASIN, Marketplace: marketplace, Raw: raw}
	if IsISBN10(v) {
		id.Kind = KindISBN10
		id.ISBN13 = ISBN13From10(v)
	}
	return id, nil
}

// looksLikeURL reports whether the input is meant to be a URL. A bare id never
// contains a slash and a URL always does, which is the whole test.
func looksLikeURL(s string) bool {
	return strings.Contains(s, "/") || strings.HasPrefix(s, "http")
}

// hosts maps every amazon domain to its marketplace slug.
//
// This lives here rather than in the client because parsing an id out of a URL
// is where the storefront is decided, and a parser that returned the id without
// the storefront would be handing back half an identity.
var hosts = map[string]string{
	"amazon.com":    "us",
	"amazon.co.uk":  "uk",
	"amazon.de":     "de",
	"amazon.fr":     "fr",
	"amazon.co.jp":  "jp",
	"amazon.ca":     "ca",
	"amazon.in":     "in",
	"amazon.it":     "it",
	"amazon.es":     "es",
	"amazon.com.mx": "mx",
	"amazon.com.br": "br",
	"amazon.com.au": "au",
	"amazon.nl":     "nl",
	"amazon.se":     "se",
	"amazon.sg":     "sg",
	"amazon.ae":     "ae",
}

// MarketplaceForHost returns the marketplace slug a hostname belongs to, and ""
// for a host this tool does not know.
//
// An unknown amazon-looking host returns nothing rather than a guess, because
// the guess would be a storefront whose currency and number format the record
// would then be parsed with, and a price read as dollars when it was written as
// euros is wrong in a way nothing downstream can detect.
func MarketplaceForHost(host string) string { return hosts[hostname(host)] }

// Hosts returns a copy of the host table.
//
// This exists so the amz package, which keeps the richer marketplace registry,
// can assert in a test that the two tables name the same storefronts. Neither
// can import the other's list without inverting the dependency, so the next best
// thing is a test that fails the day one of them gains a marketplace and the
// other does not.
func Hosts() map[string]string {
	out := make(map[string]string, len(hosts))
	maps.Copy(out, hosts)
	return out
}

// hostname reduces a URL host to the bare domain the table is keyed on. The
// port, the www prefix and the old smile prefix are all decoration.
func hostname(host string) string {
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimPrefix(h, "smile.")
	return h
}
