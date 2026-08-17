package amz

import (
	"strings"

	pasin "github.com/tamnd/amz-cli/pkg/asin"
)

// ExtractASIN pulls the 10-character ASIN out of any amazon product URL, an
// amz: product URI, or a bare id argument. It returns "" when there is none.
//
// This is a thin wrapper over pkg/asin.Parse and is kept because most of the
// package wants the id and nothing else. Anything that needs the marketplace the
// input named, which is the reason the parser exists, should call ParseID.
func ExtractASIN(s string) string {
	id, err := pasin.Parse(s)
	if err != nil {
		return ""
	}
	return id.Value
}

// isASIN reports whether a token lifted out of markup is an id.
//
// The rules call this on strings from data attributes and href fragments, where
// the failure being guarded against is a selector that matched a title slug or a
// tracking token rather than an id. So it is the strict test: ten uppercase
// alphanumerics, which is how Amazon writes every ASIN in its own HTML.
func isASIN(s string) bool { return pasin.IsASIN(s) }

// ParseID reads an id, its marketplace and its ISBN out of what a user typed.
//
// The marketplace on the result is the one the input named and is empty for a
// bare id. It is not defaulted here: a parser that returned "us" for
// B075F5X8BR would be inventing the one fact the input did not contain, and the
// caller who has a configured marketplace can tell the difference between
// "the user said uk" and "the user said nothing".
func ParseID(s string) (pasin.ID, error) { return pasin.Parse(s) }

// IsURL reports whether s looks like an http(s) URL rather than a bare id/slug.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// MarketplaceForHost returns the marketplace slug an amazon hostname belongs to,
// and "" for a host this tool does not know.
func MarketplaceForHost(host string) string { return pasin.MarketplaceForHost(host) }
