package amz

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The browse family, and what a browse page turned out to be.
//
// Measured on 2026-08-17 against /deals, /b?node=172282 and one more browse node.
// Three things came out of that measurement, and all three contradict the shape
// the previous parser assumed.
//
// First, a browse page is not a grid. It is a stack of independent carousels,
// each a cel_widget_id widget with its own heading and its own items: four
// shelves holding 22, 12, 13 and 12 items on the electronics node, six holding
// 26, 18, 11, 12, 12 and 12 on the other, one holding 11 on /deals. Flattening
// them into one list throws away the only thing that says what any item is doing
// on the page, which is the shelf that put it there.
//
// Second, there is no pagination. Not one pagination control on any of the three
// captures. A browse node is a merchandised landing page rather than a catalogue,
// so anything that wants the whole of a category has to go to /s?rh=n:<node> or
// to the chart. That is a fact about Amazon rather than a limit of this parser
// and is reported as one.
//
// Third, the identifier says which shape a tile is. data-csa-c-item-id is either
// amzn1.asin.B07B43WPVK or amzn1.asin.B004UBUJZG:amzn1.deal.1e7a7bea, and the
// second form predicts the markup exactly: on the electronics node all 12 tiles
// carrying a deal id had a deal badge and no rating, and all 47 without one had a
// rating and a delivery estimate and no badge. So a deal tile with no rating is
// Amazon not publishing one, not this parser failing to find it, and the
// identifier is what lets those be told apart.
//
// See notes/Spec/3007/02_extraction.md section 10.

// csaItemRe splits the compound identifier on a browse tile.
//
// amzn1.asin.B004UBUJZG:amzn1.deal.1e7a7bea is two identifiers in one attribute:
// the product and the offer that put it on this page. Both are kept, because an
// ASIN identifies what to fetch next and a deal id identifies the promotion that
// will be gone tomorrow.
var csaItemRe = regexp.MustCompile(`^amzn1\.asin\.([A-Z0-9]{10})(?::amzn1\.deal\.([0-9a-z]+))?$`)

// parseCSAItemID splits a browse tile's identifier into its ASIN and its deal id.
// A value in any other shape yields neither, which is how a non product tile is
// told from a product one.
func parseCSAItemID(v string) (asin, deal string) {
	m := csaItemRe.FindStringSubmatch(v)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// BrowsePage is one /b or /deals page: what Amazon says it is, and its shelves.
type BrowsePage struct {
	// NodeID is the browse node asked for, and CanonicalNode is the one Amazon
	// says it served. They are separate fields because they can differ: the
	// canonical link is where Amazon states which node the URL resolved to, and
	// a crawl that files the result under the requested node when Amazon
	// redirected is filing it under the wrong category.
	NodeID        string `json:"node_id,omitempty"`
	CanonicalNode string `json:"canonical_node,omitempty"`
	// Slug is the human readable segment of the canonical URL,
	// "electronics-store", which is the closest thing a browse page has to a
	// machine readable name for itself.
	Slug string `json:"slug,omitempty"`
	// Name is the page's title. On a browse node the h1 is either the word
	// "Department" or empty, so this comes from the canonical slug or the meta
	// description rather than from a heading.
	Name         string  `json:"name,omitempty"`
	CanonicalURL string  `json:"canonical_url,omitempty"`
	URL          string  `json:"url,omitempty"`
	Related      []Ref   `json:"related,omitempty"`
	Shelves      []Shelf `json:"shelves,omitempty"`
	// Items is how many tiles the page held in total, across every shelf.
	Items    int      `json:"items,omitempty"`
	Envelope Envelope `json:"envelope,omitzero"`
}

// Shelf is one carousel of a browse page, named by its own heading.
type Shelf struct {
	// Widget is the cel_widget_id Amazon gave this shelf, with the render slot
	// stripped. Some shelves are named by a bare UUID that changes on every
	// fetch, and those keep the UUID so the report does not claim a stable name
	// the page does not have.
	Widget string `json:"widget,omitempty"`
	// Title is the heading above the shelf, "Save on PC devices". It is the only
	// statement the page makes about why these items are together.
	Title string       `json:"title,omitempty"`
	Items []BrowseItem `json:"items,omitempty"`
}

// BrowseItem is one product tile on a browse or deals page.
type BrowseItem struct {
	ASIN string `json:"asin"`
	// DealID is amzn1.deal.<id> when the tile is a promotion, empty when it is a
	// plain product. Its presence is what says which fields to expect.
	DealID string `json:"deal_id,omitempty"`
	Title  string `json:"title,omitempty"`
	URL    string `json:"url,omitempty"`
	Image  string `json:"image,omitempty"`

	Price *Money `json:"price,omitempty"`
	// WasPrice is the struck through price, and WasPriceLabel is what Amazon
	// called it. The label matters: across the three captures it read "List:",
	// "List Price:" and "Typical:", and a typical price is a computed average
	// rather than a manufacturer's list price. Recording the number without the
	// label would turn three different claims into one.
	WasPrice      *Money `json:"was_price,omitempty"`
	WasPriceLabel string `json:"was_price_label,omitempty"`
	DiscountPct   int    `json:"discount_pct,omitempty"`
	// DealType is the badge message, "Limited time deal" or "Ends in".
	DealType string `json:"deal_type,omitempty"`
	// EndsSoon reports that Amazon drew a countdown on this tile. The clock
	// itself is filled in by script and the served HTML carries an empty slot,
	// so the flag is what the page states and the time is not available.
	EndsSoon bool `json:"ends_soon,omitempty"`

	Rating       *float64 `json:"rating,omitempty"`
	RatingsCount *int64   `json:"ratings_count,omitempty"`
	Delivery     string   `json:"delivery,omitempty"`

	// Shelf and Position are where the tile sat. data-csa-c-pos is "1,12", the
	// slot within the shelf and the slot of the shelf on the page, which is
	// Amazon's own record of the ranking it chose.
	Shelf    string `json:"shelf,omitempty"`
	Position int    `json:"position,omitempty"`

	Envelope Envelope `json:"envelope,omitzero"`
}

// browseClaimed names the regions the browse parser reads.
//
// Tile fields declare no region of their own because every one of them runs
// inside a tile rather than against the page, so the claim is stated here or the
// report lists the shelves the parser just walked as untouched.
var browseClaimed = map[string]bool{
	browseShelf: true,
	browseItem:  true,
	browseBadge: true,
	browseTimer: true,
}

// canonicalRe pulls the slug and the node out of the canonical link, which on a
// browse page reads /electronics-store/b?ie=UTF8&node=172282.
var canonicalRe = regexp.MustCompile(`amazon\.[a-z.]+/([^/?]+)/b\?`)

func (c *Client) parseBrowsePage(node, url string, body []byte) (BrowsePage, error) {
	d, err := ParseDoc(FamilyBrowse, body)
	if err != nil {
		return BrowsePage{}, err
	}
	bp := BrowsePage{NodeID: node, URL: url}
	e := NewExtractor(d)
	// RunFields rather than Run: the unread worklist is marked once at the end,
	// after the shelves have been walked, or every shelf this parser just read
	// would be reported as a region nothing touched.
	e.RunFields(browsePageFields())
	bp.CanonicalURL = e.Str("canonical_url")
	bp.CanonicalNode = e.Str("canonical_node")
	bp.Slug = e.Str("slug")
	bp.Name = e.Str("name")
	bp.Related = relatedNodes(d, firstNonEmpty(bp.CanonicalNode, node), c.mkt.Slug, c.BaseURL())

	// The shelves this page happens to carry are claimed by name as they are
	// read, on top of the synthetic names, because a shelf widget is called
	// dossier-browse_deals-p13n on one page and a bare UUID on the next and
	// there is no fixed list to declare up front.
	claimed := map[string]bool{}
	for k, v := range browseClaimed {
		claimed[k] = v
	}

	fields := browseFields()
	for _, sh := range d.Shelves() {
		claimed[sh.Name()] = true
		s := Shelf{Widget: sh.Name(), Title: shelfTitle(sh)}
		d.EachIn(sh, browseItem, func(_ int, r Region) {
			if it, ok := c.readBrowseItem(r, s.Title, fields, d); ok {
				s.Items = append(s.Items, it)
			}
		})
		if len(s.Items) == 0 {
			continue
		}
		bp.Items += len(s.Items)
		bp.Shelves = append(bp.Shelves, s)
	}

	switch {
	case len(d.Shelves()) == 0:
		e.miss("shelves", "no cel_widget_id widget on this page holds a "+browseItemSel+" tile, so the page published no items")
	case bp.Items == 0:
		e.miss("items", "every shelf on this page held tiles whose data-csa-c-item-id was not an amzn1.asin identifier")
	}
	e.MarkUnread(claimed)
	bp.Envelope = e.Envelope()
	bp.Envelope.AgentMap = d.AgentMap()
	return bp, nil
}

// shelfTitle is the heading Amazon wrote above a shelf.
//
// It is the first heading inside the widget rather than the nearest one above it,
// because the widget that carries the heading and the widget that carries the
// items are the same widget on all three captures, and the nearest heading above
// belongs to whatever shelf came before.
func shelfTitle(r Region) string {
	if !r.Exists() {
		return ""
	}
	var out string
	r.Find("h1, h2, h3").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if t := collapseSpace(nodeText(s)); t != "" {
			out = t
			return false
		}
		return true
	})
	return out
}

func (c *Client) readBrowseItem(r Region, shelf string, fields []Field, d *Doc) (BrowseItem, bool) {
	asin, deal := parseCSAItemID(r.Attr("data-csa-c-item-id"))
	if asin == "" {
		return BrowseItem{}, false
	}
	e := NewExtractor(d)
	e.RunIn(r, fields)
	it := BrowseItem{
		ASIN:          asin,
		DealID:        deal,
		Title:         e.Str("title"),
		URL:           absoluteURL(c.BaseURL(), e.Str("href")),
		Image:         upgradeImage(e.Str("image")),
		Price:         money(e, "price", c.mkt),
		WasPrice:      money(e, "was_price", c.mkt),
		WasPriceLabel: strings.TrimSuffix(e.Str("was_price_label"), ":"),
		DiscountPct:   int(e.Int("discount_pct")),
		DealType:      e.Str("deal_type"),
		EndsSoon:      e.Bool("ends_soon"),
		Rating:        f64OrNil(e.Float("rating")),
		RatingsCount:  i64OrNil(e.Int("ratings_count")),
		Delivery:      e.Str("delivery"),
		Shelf:         shelf,
		Position:      int(e.Int("position")),
	}
	it.Envelope = e.Envelope()
	return it, true
}

// dynamicImage is the image variant map Amazon puts on every browse thumbnail.
//
// data-a-dynamic-image is a JSON object of URL to dimensions, which is the same
// attribute the detail page gallery uses. Reading it is rung 2 and gives every
// rendition Amazon has rather than the one it happened to render at, so a 240
// pixel thumbnail in the HTML yields the 480 pixel original as well.
//
// The dimensions are double encoded. The value is the string "[240, 220]" and
// not the array [240, 220], so it takes two passes to get two numbers out, and a
// single pass silently yields no dimensions at all and picks a rendition by
// alphabetical accident.
func dynamicImage(s *goquery.Selection) (best string, all []string) {
	raw := attrOf(s, "data-a-dynamic-image")
	if raw == "" {
		return "", nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", nil
	}
	type variant struct {
		url  string
		area int
	}
	vs := make([]variant, 0, len(m))
	for u, dim := range m {
		vs = append(vs, variant{u, imageArea(dim)})
	}
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].area != vs[j].area {
			return vs[i].area > vs[j].area
		}
		return vs[i].url < vs[j].url
	})
	for _, v := range vs {
		all = append(all, v.url)
	}
	if len(all) > 0 {
		best = all[0]
	}
	return best, all
}

// imageArea is the pixel area of one variant, through both layers of encoding.
func imageArea(dim json.RawMessage) int {
	var wh []int
	if err := json.Unmarshal(dim, &wh); err != nil {
		var s string
		if json.Unmarshal(dim, &s) != nil || json.Unmarshal([]byte(s), &wh) != nil {
			return 0
		}
	}
	if len(wh) != 2 {
		return 0
	}
	return wh[0] * wh[1]
}

// siteChrome is the header, the footer and the shop-by-department menu, which
// every page on amazon.com carries and none of them is about.
//
// It matters here because the node links in the chrome look exactly like the
// node links in the page. Measured on the two browse captures of 2026-08-17, a
// page for Electronics linked to twenty four nodes and eight of them were Gift
// Cards, Sell, Registry, Shop with Points and the rest of the footer. Reporting
// those as related categories is not a small inaccuracy: it says the Computers
// node sits beside the Amazon Currency Converter.
const siteChrome = "#navFooter, #nav-belt, #nav-main, #navbar, #nav-subnav, #rhf, .nav-footer, footer"

// relatedNodes is every other browse node this page links to, with the words
// Amazon used for them.
//
// A browse page links to its siblings and its children and gives no marker for
// which is which, so these are related nodes rather than a tree. They are Refs
// rather than bare ids because the anchor text is the node's name and throwing
// it away meant a consumer holding "565108" had to spend a request to find out
// it means Laptops.
func relatedNodes(d *Doc, self, mkt, base string) []Ref {
	var out []Ref
	seen := map[string]bool{self: true}
	d.Selection().Find("a[href*='node=']").Each(func(_ int, s *goquery.Selection) {
		m := nodeRe.FindStringSubmatch(attrOf(s, "href"))
		if m == nil || seen[m[1]] {
			return
		}
		if s.Closest(siteChrome).Length() > 0 {
			return
		}
		seen[m[1]] = true
		if r := NewRef(RefNode, mkt, m[1], collapseSpace(s.Text()), base+"/b?node="+m[1]); r != nil {
			out = append(out, *r)
		}
	})
	return out
}
