package amz

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// BrandURL builds a brand storefront URL from a slug or page id.
func (c *Client) BrandURL(slug string) string {
	slug = strings.TrimPrefix(slug, "/")
	if strings.HasPrefix(slug, "stores/") {
		return c.BaseURL() + "/" + slug
	}
	return c.BaseURL() + "/stores/" + slug
}

// storePageIDRe pulls the page id out of a storefront URL. It is a UUID and it
// is the identifier the nav tree uses, so it is what links a storefront's pages
// to each other.
var storePageIDRe = regexp.MustCompile(`/stores/(?:[^/]+/)?page/([0-9A-Fa-f-]{36})`)

// storeClaimed is the layout this parser knows about and does not read.
//
// A brand storefront is mostly editorial furniture: rows of images, the tiles
// inside them, the overlays on the tiles, and an intersection observer per row.
// None of it carries data, all of it is named, and naming it here keeps the
// unread worklist pointed at things that might.
var storeClaimed = map[string]bool{
	"observer": true, "image": true, "editorial-row": true,
	"editorial-tile-Overlay": true, "editorial-tile-image": true,
	"editorial-tile-image-column": true, "large-editorial-tile": true,
	"medium-editorial-tile": true, "small-editorial-tile": true,
	"editorial-video-tile": true, "text-tile-layer": true,
	"text-layer-header": true, "hero-image": true,
	"header-nav-area": true, "navigation": true, "nav-item": true,
	"breadcrumb": true, "breadcrumb-item": true, "follow-button": true,
	"search-input-container": true, "search-icon-button": true,
	"magnifying-glass-icon": true, "search-form": true, "search-form-input": true,
	"product-grid-container": true, "aboutAuthorText": true,
	"authorBioLink": true, "icon-rio-stars": true,
}

// FetchBrand fetches and normalizes a brand storefront.
//
// Measured on 2026-08-17 against /stores/Skullcandy/page/9F16B940. What a brand
// storefront is: a navigation page. It carries eleven widgets, ten of which are
// editorial rows of images and video, a nav tree naming seven sub-pages, and two
// ASINs in 508 KB of HTML.
//
// So this does not return a product list, because there is not one to return.
// The parser it replaces returned four ASINs, and two of those four were
// B07984JN3L and B0CHTVMXZJ, the Amazon Business Card and Reload Your Balance,
// which sit in the footer of every page on amazon.com. It collected every
// a[href*='/dp/'] on the page and the footer is on the page. Half of what it
// called a brand's featured products was Amazon's own credit card.
//
// What it returns instead is the nav tree, which is the thing that makes a
// storefront crawlable: seven named sub-pages with their ids, and the products
// are on those.
func (c *Client) FetchBrand(ctx context.Context, slugOrURL string) (Brand, error) {
	url := slugOrURL
	slug := slugOrURL
	if IsURL(slugOrURL) {
		slug = brandSlug(slugOrURL)
	} else {
		url = c.BrandURL(slugOrURL)
	}
	body, err := c.Get(ctx, url, 24*time.Hour)
	if err != nil {
		return Brand{}, err
	}
	return parseBrandPage(slug, url, body)
}

// parseBrandPage reads a storefront that has already been fetched.
//
// Split out from the fetch so a capture on disk goes through the same code a
// live page does. A ledger that measured a second implementation would drift
// from the parser it is supposed to be watching, which is the one failure a
// drift ledger cannot have.
func parseBrandPage(slug, url string, body []byte) (Brand, error) {
	d, err := ParseDoc(FamilyStore, body)
	if err != nil {
		return Brand{}, err
	}
	e := NewExtractor(d)
	e.RunFields(storeFields())

	b := Brand{
		Slug:         slug,
		URL:          url,
		PageID:       firstNonEmpty(e.Str("page_id"), storePageID(url)),
		Name:         e.Str("name"),
		Description:  e.Str("description"),
		BannerURL:    upgradeImage(e.Str("image")),
		CanonicalURL: e.Str("canonical_url"),
		FetchedAt:    time.Now().UTC(),
	}
	b.Nav = storeNav(d.Widgets())
	for _, w := range d.Widgets() {
		b.Widgets = append(b.Widgets, w.WidgetType)
	}

	// The featured ASINs come from the widget payloads, which is where the two
	// real ones are. Reading the DOM for them is what put the footer in the last
	// record.
	b.FeaturedASINs = storeASINs(d.Widgets())
	if len(b.FeaturedASINs) > 100 {
		b.FeaturedASINs = b.FeaturedASINs[:100]
	}
	if len(b.Nav) == 0 {
		e.miss("nav", "no Header widget on this page carried a nav tree, so the storefront's other pages are not reachable from here")
	}
	e.MarkUnread(claimedWith(storeFields(), storeClaimed))
	b.Envelope = e.Envelope()
	b.Envelope.AgentMap = d.AgentMap()
	if b.Name == "" && len(b.Nav) == 0 {
		return b, ErrNotFound
	}
	return b, nil
}

// storePageID is the UUID a storefront URL names its page with.
func storePageID(u string) string {
	if m := storePageIDRe.FindStringSubmatch(u); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// storeASINs is every ASIN the widget payloads mention, in first-seen order.
//
// This reads the payload text rather than a declared field because an editorial
// tile puts its product wherever its layout wants it, and the two on the
// measured page sit at different depths. Scanning the payload is bounded to what
// the widgets published, which is the part the DOM scan got wrong: the footer is
// not in a widget.
func storeASINs(ws []StoreWidget) []string {
	var out []string
	for _, w := range ws {
		out = append(out, payloadASINs(w.Content)...)
		out = append(out, payloadASINs(w.Tiles...)...)
	}
	return dedup(out)
}

func brandSlug(u string) string {
	u = strings.TrimSuffix(u, "/")
	if _, rest, ok := strings.Cut(u, "/stores/"); ok {
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return u
}
