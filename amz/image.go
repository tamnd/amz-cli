package amz

import (
	"regexp"
	"strings"
)

// Amazon serves the same photo at many sizes by inserting a modifier segment
// into the filename: ".../images/I/71abcID._AC_SX466_.jpg" is a 466px-wide crop
// of ".../images/I/71abcID.jpg". The modifier is an all-uppercase run of size
// and crop codes (SL, SX, SY, AC, US, UF, QL, CR, FMjpg, ...) wedged between the
// image id and the extension. Stripping it yields the full-resolution master and
// collapses every thumbnail of one picture to a single canonical URL.
var (
	imgModifierRe = regexp.MustCompile(`^_?[A-Z][A-Z0-9]*(?:[,_][A-Z0-9]+)*_?$`)
	amazonImgHost = regexp.MustCompile(`(?:^|\.)(?:media-amazon\.com|ssl-images-amazon\.com|images-amazon\.com)$`)
)

// upgradeImage canonicalizes an Amazon image URL to its full-resolution master.
// Non-Amazon image hosts are returned unchanged (minus any query string). Junk
// placeholders (data URIs, tracking pixels, sprites) return "".
func upgradeImage(u string) string {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "data:") {
		return ""
	}
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	// Drop the query/fragment: Amazon size params also ride here.
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	low := strings.ToLower(u)
	if strings.Contains(low, "pixel") || strings.Contains(low, "transparent") ||
		strings.Contains(low, "/sprites/") || strings.HasSuffix(low, ".svg") ||
		strings.Contains(low, "play-icon") || strings.Contains(low, "play-button") ||
		strings.Contains(low, "overlay") {
		// Video gallery thumbnails carry a composited play badge ("...PKdp-
		// play-icon-overlay__.jpg"); they are not product photos, and the video
		// itself is captured separately, so drop them.
		return ""
	}
	slash := strings.LastIndex(u, "/")
	if slash < 0 {
		return u
	}
	host := hostOf(u)
	if !amazonImgHost.MatchString(host) {
		return u
	}
	name := u[slash+1:]
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return u
	}
	ext := parts[len(parts)-1]
	keep := []string{parts[0]} // the image id is always the first segment
	for _, seg := range parts[1 : len(parts)-1] {
		if !imgModifierRe.MatchString(seg) {
			keep = append(keep, seg) // not a size code; preserve it
		}
	}
	filename := strings.Join(keep, ".") + "." + ext
	// Amazon serves the same /images/I/<id> from several CDN hosts; pin one so
	// identical photos collapse to a single URL regardless of which host a page
	// happened to reference.
	path := u[strings.Index(u, host)+len(host) : slash+1]
	return "https://m.media-amazon.com" + path + filename
}

// hostOf returns the lowercased host of an absolute URL, or "".
func hostOf(u string) string {
	rest := u
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.Index(rest, ":"); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

// normImages upgrades every URL to full resolution, drops junk, and dedups by
// canonical URL while preserving first-seen order.
func normImages(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		u := upgradeImage(s)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
