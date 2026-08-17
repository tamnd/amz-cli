package amz

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

// The named regions of an Amazon page, per page family.
//
// A CSS class like a-price-whole is a design system token and a redesign
// renames it. data-feature-name="corePrice" is a widget name, and a redesign
// keeps it because the team that owns the price block is still the team that
// owns the price block. Reading the second is rung 1 of the ladder and reading
// the first is rung 4.
//
// The complication is that the anchor vocabulary is per family. Measured
// 2026-08-17 across the five families:
//
//	page family        data-feature-name   data-cy   data-csa-c-type   data-testid
//	/dp/                             305         1                 0            0
//	/s                                 0        12                32            0
//	/gp/bestsellers/                   0         0                14            0
//	/b?node=                           0         0                84            0
//	/stores/                           0         0                38          130
//
// So Region is an interface with one implementation per family, and what they
// share is the ladder, the counting and the provenance, not the selector.
// See notes/Spec/3007/02_extraction.md section 2.

// Family is one of the five page shapes amazon.com serves.
type Family string

const (
	// FamilyProduct is /dp/ and /gp/aw/d/, anchored on data-feature-name.
	FamilyProduct Family = "product"
	// FamilySearch is /s, anchored on data-component-type then data-cy.
	FamilySearch Family = "search"
	// FamilyChart is /gp/bestsellers and its siblings, anchored on gridItemRoot.
	FamilyChart Family = "chart"
	// FamilyBrowse is /b and /deals, anchored on cel_widget_id.
	FamilyBrowse Family = "browse"
	// FamilyStore is /stores/ and author pages, anchored on data-testid for the
	// layout and on the var config payloads for the data.
	FamilyStore Family = "store"
	// FamilySeller is /sp?seller=, anchored on id.
	//
	// It is its own family because it is measurably its own page. On the
	// 2026-08-17 capture it carried no stores payload, no data-testid and no
	// cel_widget_id past the navbar, and named its eight sections with
	// id="page-section-*" instead. Filing it under FamilyStore because both are
	// reachable from a product page would have meant a store vocabulary that
	// matches nothing on a third of the pages it claims to cover.
	FamilySeller Family = "seller"
)

// Families returns every family, in the spec's order.
func Families() []Family {
	return []Family{FamilyProduct, FamilySearch, FamilyChart, FamilyBrowse, FamilyStore, FamilySeller}
}

// FamilyFor maps a URL to its page family through the Ops registry, so the two
// tables cannot disagree about what a path is.
func FamilyFor(rawURL string) Family {
	op := OpFor(rawURL)
	if op == nil {
		return FamilyProduct
	}
	switch op.Name {
	case "search":
		return FamilySearch
	case "bestsellers", "new-releases", "movers", "most-wished-for", "most-gifted":
		return FamilyChart
	case "category", "deals":
		return FamilyBrowse
	case "brand", "author":
		return FamilyStore
	case "seller":
		return FamilySeller
	default:
		return FamilyProduct
	}
}

// Region is a subtree the page named on purpose.
type Region interface {
	// Name is the anchor value, "corePrice" or "s-search-result".
	Name() string
	// Family is the vocabulary this region was found under.
	Family() Family
	// Exists reports whether the region is on the page at all. A region that is
	// present and empty and a region that has moved are different facts, and
	// only the second one is a parser problem.
	Exists() bool
	// Sub finds a nested region by this family's nesting vocabulary. Product
	// regions nest by data-feature-name, search cards by data-cy, chart items by
	// class, browse tiles by data-csa-c-type, store widgets by data-widget.
	Sub(name string) Region
	// Sel is the underlying selection, for the rules that need one.
	Sel() *goquery.Selection
	// Text is the collapsed text of the region.
	Text() string
	// Attr is an attribute of the region's own node.
	Attr(name string) string
	// Find is a scoped CSS query. Every call is rung 3 or rung 4 and the caller
	// is expected to say which.
	Find(sel string) *goquery.Selection
}

// base is what the five implementations share.
type base struct {
	name string
	sel  *goquery.Selection
}

func (b base) Name() string            { return b.name }
func (b base) Exists() bool            { return b.sel != nil && b.sel.Length() > 0 }
func (b base) Sel() *goquery.Selection { return b.sel }
func (b base) Text() string            { return collapseSpace(b.selText()) }

// Find and Attr answer for a region that is not on the page, because a rule that
// asks a missing region for a price should get "no price" and not a panic. The
// caller already knows the region is missing: Exists said so and the extractor
// recorded the miss.
func (b base) Find(sel string) *goquery.Selection { return b.scoped().Find(sel) }
func (b base) Attr(name string) string            { return attrOf(b.scoped(), name) }

func (b base) scoped() *goquery.Selection {
	if b.sel == nil {
		return emptySelection()
	}
	return b.sel
}

// emptySelection is a valid selection over no nodes, built once. goquery methods
// need a document behind them even when there is nothing to match, so this is
// what a missing region hands to a rule.
var emptySelection = sync.OnceValue(func() *goquery.Selection {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html></html>"))
	if err != nil {
		panic("amz: empty document did not parse: " + err.Error())
	}
	return doc.Find("amz-no-such-element")
})

func (b base) subBy(attr, name string) *goquery.Selection {
	return b.subMatch(attr, "=", name)
}

func (b base) subMatch(attr, op, name string) *goquery.Selection {
	if !b.Exists() {
		return nil
	}
	s := b.sel.Find("[" + attr + op + `"` + name + `"]`).First()
	if s.Length() == 0 {
		return nil
	}
	return s
}

func (b base) selText() string {
	if !b.Exists() {
		return ""
	}
	return nodeText(b.sel)
}

// featureRegion is the product family: data-feature-name, which nests.
//
// offerDisplayFeatures_desktop contains conditionInfoFeature, returnsInfoFeature
// and six others, so Region returns the outermost match and Sub searches inside
// it. Without that, a lookup for the condition block can land in the "other
// sellers" panel, which is a different offer for a different price.
type featureRegion struct{ base }

func (r featureRegion) Family() Family { return FamilyProduct }
func (r featureRegion) Sub(name string) Region {
	return featureRegion{base{name: name, sel: r.subBy("data-feature-name", name)}}
}

// cardRegion is the search family: data-component-type outside, data-cy inside.
//
// A search card carries no data-feature-name at all. What it carries is
// data-cy="title-recipe", data-cy="price-recipe" and ten more, and those are the
// names the search team gave their own slots.
type cardRegion struct{ base }

func (r cardRegion) Family() Family { return FamilySearch }
func (r cardRegion) Sub(name string) Region {
	sel := r.subBy("data-cy", name)
	if sel == nil {
		sel = r.subBy("data-component-type", name)
	}
	return cardRegion{base{name: name, sel: sel}}
}

// gridRegion is the chart family: cel_widget_id per widget, then classes the
// chart team owns inside a tile.
//
// The charts are the oldest markup on the site and their tiles never got the
// data-* treatment. Above the tile Amazon does name things, in cel_widget_id,
// which is why the widgets are rung 1 and the fields inside a tile are not.
type gridRegion struct{ base }

func (r gridRegion) Family() Family { return FamilyChart }
func (r gridRegion) Sub(name string) Region {
	sel := r.subMatch("cel_widget_id", "^=", name)
	if sel == nil && r.Exists() {
		if s := r.sel.Find("." + name).First(); s.Length() > 0 {
			sel = s
		}
	}
	return gridRegion{base{name: name, sel: sel}}
}

// csaRegion is the browse and deals family: cel_widget_id for the shelves,
// data-component for the parts inside a tile.
//
// The shelf names are rung 1 and the tile identifiers are rung 3, and the split
// is worth stating. Amazon names a browse shelf in cel_widget_id and names
// nothing inside a tile except its identity, its slot and its badge, so a tile's
// title and price come off dcl- classes and are counted as rung 4.
type csaRegion struct{ base }

func (r csaRegion) Family() Family { return FamilyBrowse }
func (r csaRegion) Sub(name string) Region {
	sel := r.subMatch("cel_widget_id", "^=", name)
	if sel == nil {
		sel = r.subBy("data-component", name)
	}
	if sel == nil {
		sel = r.subBy("data-csa-c-type", name)
	}
	return csaRegion{base{name: name, sel: sel}}
}

// widgetRegion is the stores and author family, anchored on data-testid.
//
// The earlier note here said a storefront is a JavaScript shell whose product
// grid is not in the HTML. Half of that was right and the important half was
// wrong. The grid is genuinely not in the DOM, and the author page proves it by
// rendering ProductGrid__no-matches where the books should be. But the data is
// on the page: twelve var config payloads, one per widget, and the grid's
// payload carries 70 full product records and the count 135. So this family
// reads its layout here at rung 1 and its data at rung 2, in storefront.go.
//
// data-testid is still a test hook rather than a data contract, and it is
// treated as one: it names regions and it is not asked for values.
type widgetRegion struct{ base }

func (r widgetRegion) Family() Family { return FamilyStore }
func (r widgetRegion) Sub(name string) Region {
	return widgetRegion{base{name: name, sel: r.subBy("data-testid", name)}}
}

// sellerRegion is the seller profile family, anchored on id.
//
// Anchoring on id is not something the other families can do, because a detail
// page carries 1,148 of them and most name a DOM node rather than a region. A
// seller profile carries 122, of which 17 use the two prefixes Amazon reserved
// for its sections, and regionName keeps only those. That is what makes id a
// usable anchor here and nowhere else.
type sellerRegion struct{ base }

func (r sellerRegion) Family() Family { return FamilySeller }
func (r sellerRegion) Sub(name string) Region {
	return sellerRegion{base{name: name, sel: r.subBy("id", name)}}
}

// anchors is the rung 1 attribute of each family, in the order tried.
//
// Three of these entries were wrong in a way worth recording, and two of the
// three were the same mistake: indexing a value as if it were a name.
//
// The chart family was anchored on data-client-recs-list, whose value is not a
// name but an 18 KB JSON array of the whole list, so every chart page indexed one
// region named after its own payload. It is read now, as a payload, by
// readChartList.
//
// The browse family was anchored on data-csa-c-item-id, whose value is an item
// identifier, so a browse page indexed 91 regions named
// amzn1.asin.B07B43WPVK and never named a single one twice. The identifier is
// read now as identity, by parseCSAItemID, and the shelves Amazon does name are
// anchored on cel_widget_id like the charts.
//
// The third was data-widget, listed as the store family's fallback anchor. It
// matches nothing. Not rarely: zero nodes across all six captures, storefront,
// author, seller, detail, search and browse. It was never measured, it was
// guessed at from a naming convention, and a fallback that never fires is a
// fallback nobody notices is dead. It is gone.
var anchors = map[Family][]string{
	FamilyProduct: {"data-feature-name"},
	FamilySearch:  {"data-component-type", "data-cy"},
	FamilyChart:   {"cel_widget_id"},
	FamilyBrowse:  {"cel_widget_id", "data-component"},
	FamilyStore:   {"data-testid"},
	FamilySeller:  {"id"},
}

// prefixAnchored reports whether a family's names are stored with their render
// slot stripped, so a lookup has to match by prefix rather than by equality.
func prefixAnchored(fam Family) bool { return fam == FamilyChart || fam == FamilyBrowse }

// The chart widgets and the tile anchor, by the names Amazon gives them.
const (
	// chartGrid is the ranked grid on /gp/bestsellers/<category> and its
	// siblings. It is present and empty on movers-and-shakers, which is a
	// different fact from being absent and is reported as one.
	chartGrid = "p13n-zg-list-grid-desktop"
	// chartCarousel is one row of the /gp/bestsellers landing page, which
	// carries six of them and gives each its own ranking from 1.
	chartCarousel = "p13n-zg-list-carousel-desktop"
	// chartNavTree is the category tree down the left of every chart page.
	chartNavTree = "p13n-zg-nav-tree-all"
	// chartTile is a product tile in either layout. It is not a cel_widget_id:
	// the tiles carry data-asin and nothing else, and Doc.repeated maps this
	// name onto that attribute so callers name a tile rather than a selector.
	chartTile = "chart-tile"
)

// The browse shelves and the tile anchor, by the names this parser uses.
const (
	// browseShelf is one carousel of a browse or deals page: a cel_widget_id
	// widget holding at least one item. It is not a cel_widget_id value itself,
	// because Amazon gives a shelf a different name on every page and sometimes
	// a bare UUID, so Doc.repeated maps this name onto the widgets that hold
	// items and callers name a shelf rather than a selector.
	browseShelf = "browse-shelf"
	// browseItem is one product tile, anchored on data-csa-c-item-id.
	browseItem = "browse-item"
	// browseBadge is the deal flag Amazon draws over a tile, which is the one
	// thing inside a tile it names. Its two children are the discount and the
	// deal type, in that order.
	browseBadge = "dui-badge"
	// browseTimer is the countdown on a lightning deal. It is named and it is
	// empty: the clock is drawn by script, so the DOM carries the slot and no
	// time. That is reported rather than guessed at.
	browseTimer = "badge-countdown-timer"
)

// browseItemSel is the tile anchor, the same on /b and on /deals.
const browseItemSel = "[data-csa-c-item-id]"

// The tails Amazon appends to a widget name, none of which are part of the name.
//
// A chart widget is p13n-zg-list-grid-desktop_zeitgeist-lists_2, a browse shelf
// is content-grid-card_apb-browse_5, a deals row is
// pcpo-offer_deals-events-atf-desktop_11, and a merchandised shelf is
// dossier-browse_deals-p13n-4c9ba9e1-... with a fresh UUID on every render.
//
// What follows the name is where the widget landed in this particular page, so
// keeping it would put new region names in the index on every fetch and leave the
// unread worklist listing the same shelf under a different name each time.
var (
	widgetSlotRe = regexp.MustCompile(`_[A-Za-z0-9-]+_\d+$`)
	widgetUUIDRe = regexp.MustCompile(`-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	bareUUIDRe   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// identRe is what a name looks like: letters, digits, dash, underscore, colon.
// Anything else in an anchor value means the value is not a name.
var identRe = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)

// sellerIDPrefixes are the two prefixes Amazon reserved for the named sections
// of a seller profile. Measured on the 2026-08-17 capture: 17 of the page's 122
// ids use one of them and every one of those 17 names a section.
var sellerIDPrefixes = []string{"page-section-", "seller-"}

// regionName is the name an anchor attribute contributes to the index, after the
// cleanup that attribute needs. Returning "" drops the node.
func regionName(fam Family, attr, v string) string {
	if attr == "id" && fam == FamilySeller {
		// Only the reserved prefixes are sections. The rest of a seller page's
		// ids name a column, a link or an expander, and indexing those would
		// bury eight real sections in a hundred layout nodes.
		for _, p := range sellerIDPrefixes {
			if strings.HasPrefix(v, p) {
				return v
			}
		}
		return ""
	}
	if attr == "data-testid" {
		// One data-testid on the author capture is the string "VND 365,818".
		// That is a price that reached a test hook, and indexing it would put a
		// region named after this morning's exchange rate in the report and a
		// different one there tomorrow. A test id is an identifier, so a value
		// that is not one is not a name.
		if !identRe.MatchString(v) {
			return ""
		}
		return v
	}
	if attr != "cel_widget_id" {
		return v
	}
	// Some browse shelves are named by a UUID and nothing else. A UUID is an
	// identifier for one render rather than a name for a widget, so it is not
	// indexed; those shelves are reached through browseShelf, which finds them
	// by the items they hold.
	if bareUUIDRe.MatchString(v) {
		return ""
	}
	v = widgetSlotRe.ReplaceAllString(v, "")
	return widgetUUIDRe.ReplaceAllString(v, "")
}

// Doc is a parsed page and its region index.
//
// The index is built once, in one pass, because a detail page carries 305 named
// regions and a hundred separate Find calls over a 2.2 MB DOM is the difference
// between a store rebuild that finishes and one that does not.
type Doc struct {
	fam   Family
	doc   *goquery.Document
	idx   map[string]*goquery.Selection
	order []string
	raw   []byte
	chart chartList
	// shelves is the browse family's carousels, in document order. A browse page
	// is a stack of them rather than one grid, and each one names its own list.
	shelves []*goquery.Selection
	// widgets is the store family's payloads, in document order. The DOM of a
	// stores page is layout and these are its data, so a store field that wants
	// a value reads here rather than out of the tree.
	widgets []StoreWidget
}

// Widgets returns the store payloads this page carried.
func (d *Doc) Widgets() []StoreWidget { return d.widgets }

// ParseDoc parses a page under one family's vocabulary and indexes its regions.
func ParseDoc(fam Family, body []byte) (*Doc, error) {
	doc, err := newDocument(body)
	if err != nil {
		return nil, err
	}
	d := &Doc{fam: fam, doc: doc, idx: map[string]*goquery.Selection{}, raw: body}
	for _, attr := range anchors[fam] {
		doc.Find("[" + attr + "]").Each(func(_ int, s *goquery.Selection) {
			name := regionName(fam, attr, attrOf(s, attr))
			if name == "" {
				return
			}
			// goquery walks in document order, so the first match for a name is
			// the outermost one. Later matches are nested and belong to Sub.
			if _, seen := d.idx[name]; seen {
				return
			}
			d.idx[name] = s
			d.order = append(d.order, name)
		})
	}
	// The two families whose repeating tile carries no name of its own get one
	// here, so a caller asks for a tile rather than for a selector and the tile
	// shows up in the region report like everything else.
	switch fam {
	case FamilyChart:
		d.synthetic(chartTile, chartTileSel)
		d.chart = readChartList(doc)
	case FamilyBrowse:
		d.synthetic(browseItem, browseItemSel)
		d.shelves = browseShelves(doc)
		if len(d.shelves) > 0 {
			d.idx[browseShelf] = d.shelves[0]
			d.order = append(d.order, browseShelf)
		}
	case FamilyStore:
		d.widgets = readStoreWidgets(body)
	}
	return d, nil
}

// synthetic indexes a repeating tile under a name this package chose, for the
// families where Amazon named the container and left the tile anonymous.
func (d *Doc) synthetic(name, sel string) {
	s := d.doc.Find(sel).First()
	if s.Length() == 0 {
		return
	}
	d.idx[name] = s
	d.order = append(d.order, name)
}

// browseShelves is every cel_widget_id widget holding at least one item, in
// document order and innermost first.
//
// Innermost matters because browse widgets nest: on /deals the eleven tiles sit
// inside deals-events-atf-desktop_deals-p13n-<uuid>, which sits inside the page
// container, and taking the outer one would report a single shelf of eleven
// where the page has one shelf that happens to be alone. Walking children before
// parents and skipping any widget whose items are already claimed gives the
// shelf that actually owns each tile.
func browseShelves(doc *goquery.Document) []*goquery.Selection {
	var out []*goquery.Selection
	doc.Find("[cel_widget_id]").Each(func(_ int, s *goquery.Selection) {
		if s.Find(browseItemSel).Length() == 0 {
			return
		}
		// A widget that contains another widget that holds items is a container
		// for shelves rather than a shelf.
		inner := s.Find("[cel_widget_id]").FilterFunction(func(_ int, c *goquery.Selection) bool {
			return c.Find(browseItemSel).Length() > 0
		})
		if inner.Length() > 0 {
			return
		}
		out = append(out, s)
	})
	return out
}

// Shelves returns the browse shelves, each a widget Amazon named that holds at
// least one item.
func (d *Doc) Shelves() []Region {
	out := make([]Region, 0, len(d.shelves))
	for _, s := range d.shelves {
		out = append(out, d.wrap(shelfName(s), s))
	}
	return out
}

// shelfName is the widget name with its render slot stripped, or the raw
// cel_widget_id when that leaves nothing, which is the bare UUID case.
func shelfName(s *goquery.Selection) string {
	raw := attrOf(s, "cel_widget_id")
	if n := regionName(FamilyBrowse, "cel_widget_id", raw); n != "" {
		return n
	}
	return raw
}

// Family returns the vocabulary this document was indexed under.
func (d *Doc) Family() Family { return d.fam }

// Raw returns the original body, which the rung 2 payload readers scan by byte
// offset rather than through the DOM.
func (d *Doc) Raw() []byte { return d.raw }

// Selection exposes the document root for the rules that have no region to work
// in. Every such rule is rung 3 or rung 4 by construction.
func (d *Doc) Selection() *goquery.Selection { return d.doc.Selection }

// Region returns the named region, or one whose Exists reports false.
func (d *Doc) Region(name string) Region {
	return d.wrap(name, d.idx[name])
}

// Root is the whole document as a region, named for the family it belongs to.
//
// This is what a rung 3 or rung 4 field is scoped to, and naming it that way is
// the point: a field reading the whole page instead of a region Amazon named is
// exactly the thing the ladder is there to count.
func (d *Doc) Root() Region {
	return d.wrap("(page)", d.doc.Selection)
}

// Each calls fn for every occurrence of an anchor name, which is how a page of
// search cards or chart tiles is walked.
func (d *Doc) Each(name string, fn func(i int, r Region)) {
	sel := d.repeated(name)
	if sel == nil {
		return
	}
	sel.Each(func(i int, s *goquery.Selection) { fn(i, d.wrap(name, s)) })
}

// EachIn is Each scoped to one region, which is how a page of shelves of tiles
// is walked: the shelf names the list and the tiles inside it are its items.
func (d *Doc) EachIn(root Region, name string, fn func(i int, r Region)) {
	if !root.Exists() {
		return
	}
	sel := d.repeatedIn(root.Sel(), name)
	if sel == nil {
		return
	}
	sel.Each(func(i int, s *goquery.Selection) { fn(i, d.wrap(name, s)) })
}

func (d *Doc) repeated(name string) *goquery.Selection {
	return d.repeatedIn(d.doc.Selection, name)
}

func (d *Doc) repeatedIn(root *goquery.Selection, name string) *goquery.Selection {
	// The tiles this package named itself, because Amazon named the container
	// and left the tile carrying nothing but its own identity.
	switch {
	case d.fam == FamilyChart && name == chartTile:
		return root.Find(chartTileSel)
	case d.fam == FamilyBrowse && name == browseItem:
		return root.Find(browseItemSel)
	}
	var out *goquery.Selection
	for _, attr := range anchors[d.fam] {
		// Families whose widget names are indexed with the render slot stripped
		// have to be looked up by prefix, or a lookup for the chart carousel
		// misses all six the landing page ships. Every other family stores the
		// name Amazon wrote and matches it exactly.
		op := "="
		if prefixAnchored(d.fam) {
			op = "^="
		}
		s := root.Find("[" + attr + op + `"` + name + `"]`)
		if s.Length() > 0 {
			out = s
			break
		}
	}
	return out
}

func (d *Doc) wrap(name string, sel *goquery.Selection) Region {
	b := base{name: name, sel: sel}
	switch d.fam {
	case FamilySearch:
		return cardRegion{b}
	case FamilyChart:
		return gridRegion{b}
	case FamilyBrowse:
		return csaRegion{b}
	case FamilyStore:
		return widgetRegion{b}
	case FamilySeller:
		return sellerRegion{b}
	default:
		return featureRegion{b}
	}
}

// RegionNames returns every anchor name on the page, in document order.
func (d *Doc) RegionNames() []string { return d.order }

// SortedRegionNames returns the anchor names sorted, for a stable report.
func (d *Doc) SortedRegionNames() []string {
	out := append([]string(nil), d.order...)
	sort.Strings(out)
	return out
}

// AgentMap is the JSON blob Amazon publishes for agents, recorded verbatim.
//
// It is an interface contract rather than data, and today it is unreliable:
// pageType reads "product-listing" on a /dp/ detail page, on /b?node= and on
// /deals alike. So amz stores it, exposes it, and decides nothing with it. If it
// starts being accurate a later version can promote it, and recording it now
// makes that a data question rather than an archaeology project.
// See notes/Spec/3007/02_extraction.md section 7.
func (d *Doc) AgentMap() []byte {
	var out []byte
	d.doc.Find(`script[type="application/json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := strings.TrimSpace(s.Text())
		if !strings.Contains(txt, "AgentInterfaceMap") {
			return true
		}
		out = []byte(txt)
		return false
	})
	if out == nil {
		// The blob is stripped from the DOM along with every other script, so
		// fall back to the raw body. This is the one place amz reads the source
		// text of a script tag it deliberately removed.
		if i := bytes.Index(d.raw, []byte("AgentInterfaceMap")); i >= 0 {
			if start := bytes.LastIndexByte(d.raw[:i], '{'); start >= 0 {
				if end := bytes.IndexByte(d.raw[i:], '<'); end > 0 {
					seg := bytes.TrimSpace(d.raw[start : i+end])
					if j := bytes.LastIndexByte(seg, '}'); j >= 0 {
						out = seg[:j+1]
					}
				}
			}
		}
	}
	return out
}
