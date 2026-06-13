package amz

import "regexp"

// asinPattern matches a 10-char ASIN following any of the known URL markers.
var asinPattern = regexp.MustCompile(`(?:/dp/|/gp/product/|/product-reviews/|/ask/questions/asin/|/ask/|/gp/aw/d/)([A-Z0-9]{10})`)

// bareASIN matches a standalone 10-char ASIN (e.g. a CLI argument).
var bareASIN = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// ExtractASIN pulls the 10-character ASIN out of any amazon product URL or a
// bare ASIN argument. It returns "" when no ASIN is present.
func ExtractASIN(s string) string {
	if bareASIN.MatchString(s) {
		return s
	}
	if m := asinPattern.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// IsURL reports whether s looks like an http(s) URL rather than a bare id/slug.
func IsURL(s string) bool {
	return len(s) > 7 && (s[:7] == "http://" || s[:8] == "https://")
}
