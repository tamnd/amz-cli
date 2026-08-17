package amz

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// ChartKind identifies one of amazon's ranked lists.
type ChartKind string

const (
	ChartBestsellers ChartKind = "bestsellers"
	ChartNewReleases ChartKind = "new-releases"
	ChartMovers      ChartKind = "movers-and-shakers"
	ChartWished      ChartKind = "most-wished-for"
	ChartGifted      ChartKind = "most-gifted"
)

// ChartURL builds a chart URL for a category slug or browse node.
func (c *Client) ChartURL(kind ChartKind, category, node string, page int) string {
	u := c.BaseURL() + "/gp/" + string(kind)
	switch {
	case node != "":
		u += "/" + node
	case category != "":
		u += "/" + category
	}
	if page > 1 {
		u += "?pg=" + strconv.Itoa(page)
	}
	return u
}

// chartMaxPages caps chart paging so a malformed list can't loop forever.
// Amazon charts top out at 100 items (two 50-item pages) today, but we page
// until a page comes back empty rather than assuming the count.
const chartMaxPages = 10

// FetchChart streams ranked entries from a chart, paging until a page is empty
// or the limit is reached.
//
// Ranks come from the page rather than from counting, because counting is wrong
// here. Page one covers ranks 1 to 50 while drawing 30 tiles, and page two
// starts at 51, so numbering entries as they arrive would label rank 51 as rank
// 31 and keep doing it for the length of the chart.
func (c *Client) FetchChart(ctx context.Context, kind ChartKind, category, node string, limit int, emit func(BestsellerEntry) error) error {
	count := 0
	seen := make(map[string]bool)
	for page := 1; page <= chartMaxPages; page++ {
		u := c.ChartURL(kind, category, node, page)
		body, src, err := c.GetSource(ctx, u, time.Hour)
		if err != nil {
			if page > 1 {
				break
			}
			return err
		}
		cp, err := c.parseChartPage(string(kind), category, node, u, page, body)
		if err != nil {
			return err
		}
		c.record(ctx, &cp.Envelope, src)
		for i := range cp.Entries {
			cp.Entries[i].Envelope.Inherit(cp.Envelope)
		}
		if len(cp.Entries) == 0 {
			break
		}
		fresh := 0
		for _, e := range cp.Entries {
			if seen[e.ASIN] {
				continue
			}
			seen[e.ASIN] = true
			fresh++
			count++
			if err := emit(e); err != nil {
				return err
			}
			if limit > 0 && count >= limit {
				return nil
			}
		}
		// A page that adds nothing new means Amazon served a repeat of the last
		// one, which is how a chart shorter than the cap ends.
		if fresh == 0 {
			break
		}
		// The landing page is one page of six lists and has no second page.
		if cp.Layout == "index" {
			break
		}
	}
	return nil
}

// ChartPage is one page of a chart, with what the page said about itself.
//
// The counts are separate fields because they disagree in a way that matters. On
// /gp/bestsellers/electronics page one, Listed is 50, ServerRendered is 30 and
// Rendered is 30: Amazon named fifty items and drew thirty. Page two picks up at
// rank 51, so the twenty in between exist only in the list. On the
// movers-and-shakers page Listed is 0 and Rendered is 0, and that is Amazon
// saying the list is empty rather than the parser failing to read it.
type ChartPage struct {
	ListType string `json:"list_type"`
	Category string `json:"category,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	URL      string `json:"url,omitempty"`
	Page     int    `json:"page,omitempty"`

	// Layout is "grid" for a ranked chart, "index" for the landing page that
	// carries one carousel per department, and "" when neither is on the page.
	Layout string `json:"layout,omitempty"`
	// RefTag is Amazon's own name for the list, zg_bs_g_electronics.
	RefTag string `json:"ref_tag,omitempty"`
	// Listed is how many items the page's list holds, from data-offset.
	Listed int `json:"listed,omitempty"`
	// ServerRendered is how many the server drew, from data-index-offset.
	ServerRendered int `json:"server_rendered,omitempty"`
	// Rendered is how many tiles were actually in the HTML.
	Rendered int `json:"rendered,omitempty"`

	Entries  []BestsellerEntry `json:"entries,omitempty"`
	Envelope Envelope          `json:"envelope,omitzero"`
}

// chartClaimed names the regions the chart parser reads.
//
// The tile fields declare no region of their own because every one of them is
// read inside a tile rather than against the page, so the claim has to be stated
// here or the report would list the grid the parser just walked as untouched.
// What is left over is the real worklist: the category tree that names every
// sibling chart, the list-type tabs, and the promotional banners.
var chartClaimed = map[string]bool{
	chartTile:     true,
	chartGrid:     true,
	chartCarousel: true,
}

// chartLayouts maps the widget Amazon put on the page to the name this parser
// uses for its shape.
var chartLayouts = []struct {
	widget, layout string
}{
	{chartGrid, "grid"},
	{chartCarousel, "index"},
}

func (c *Client) parseChartPage(listType, category, node, url string, page int, body []byte) (ChartPage, error) {
	d, err := ParseDoc(FamilyChart, body)
	if err != nil {
		return ChartPage{}, err
	}
	list := d.chart
	cp := ChartPage{
		ListType:       listType,
		Category:       category,
		NodeID:         node,
		URL:            url,
		Page:           page,
		RefTag:         list.RefTag,
		Listed:         list.Listed,
		ServerRendered: list.ServerRendered,
	}
	for _, l := range chartLayouts {
		if d.Region(l.widget).Exists() {
			cp.Layout = l.layout
			break
		}
	}

	fields := chartFields()
	e := NewExtractor(d)

	// The page's own four facts, recorded rather than only assigned.
	//
	// These were read off the grid container and written straight into the
	// record, so a chart came back with fifty entries and an envelope that
	// claimed nothing had been read from the page at all. The capture ledger is
	// what made that visible: every chart capture reported set=0 beside fifty
	// records. A field with no provenance is a field a caller cannot check, and
	// the rule in this package is that the envelope accounts for everything.
	if cp.Layout != "" {
		e.set("layout", cp.Layout, LevelRegion, chartGrid)
	}
	if list.Present {
		if cp.RefTag != "" {
			e.set("ref_tag", cp.RefTag, LevelAttr, "data-reftag")
		}
		// Zero is a real answer from both of these, so they are recorded
		// whenever the container that states them is on the page. An empty
		// movers grid says data-offset="0" and means it.
		e.set("listed", int64(cp.Listed), LevelAttr, "data-offset")
		e.set("server_rendered", int64(cp.ServerRendered), LevelAttr, "data-index-offset")
	}
	seen := map[string]bool{}
	d.Each(chartTile, func(_ int, r Region) {
		entry, ok := c.readTile(cp, r, fields, d)
		if !ok || seen[entry.ASIN] {
			return
		}
		seen[entry.ASIN] = true
		cp.Entries = append(cp.Entries, entry)
	})
	cp.Rendered = len(cp.Entries)

	// Everything Amazon named that no tile drew. These carry a rank and an ASIN
	// and nothing else, which is all the page states about them, and emitting
	// them is the difference between a chart of 50 and a chart of 30.
	for _, it := range list.Items {
		if seen[it.ASIN] {
			continue
		}
		cp.Entries = append(cp.Entries, BestsellerEntry{
			Marketplace:    c.mkt.Slug,
			ListType:       listType,
			Category:       category,
			NodeID:         node,
			Rank:           it.Rank,
			ASIN:           it.ASIN,
			URL:            c.ProductURL(it.ASIN),
			SalesRank:      it.SalesRank,
			PriorSalesRank: it.PriorSalesRank,
			PercentChange:  it.PercentChange,
			RankOnly:       true,
			FetchedAt:      time.Now().UTC(),
		})
	}
	sort.SliceStable(cp.Entries, func(i, j int) bool { return cp.Entries[i].Rank < cp.Entries[j].Rank })

	switch {
	case cp.Layout == "":
		e.miss("entries", "neither the "+chartGrid+" widget nor a "+chartCarousel+" is on this page")
	case cp.Layout == "grid" && !list.Present:
		e.miss("listed", "the grid carried no "+PayloadChartList+", so only the rendered tiles are known")
	case cp.Layout == "grid" && list.Listed == 0 && cp.Rendered == 0:
		// Amazon shipped the grid and then said it holds nothing. That is the
		// list being empty, not the parser missing it, and only data-offset can
		// tell the two apart.
		e.miss("entries", "the "+chartGrid+" widget is on the page with data-offset 0 and no tiles, so Amazon is publishing an empty list")
	case list.Listed > cp.Rendered:
		e.miss("rendered", fmt.Sprintf("Amazon listed %d items and rendered %d; the rest carry a rank and an ASIN only",
			list.Listed, cp.Rendered))
	}
	e.MarkUnread(chartClaimed)
	cp.Envelope = e.Envelope()
	cp.Envelope.AgentMap = d.AgentMap()
	return cp, nil
}

// readTile reads one product tile. The tile is anchored on data-asin, which is
// both the anchor and the identifier, so a tile without one is a layout cell
// rather than a product and is skipped.
func (c *Client) readTile(cp ChartPage, r Region, fields []Field, d *Doc) (BestsellerEntry, bool) {
	asin := r.Attr("data-asin")
	if !isASIN(asin) {
		return BestsellerEntry{}, false
	}
	e := NewExtractor(d)
	e.RunIn(r, fields)
	entry := BestsellerEntry{
		Marketplace:  c.mkt.Slug,
		ListType:     cp.ListType,
		Category:     cp.Category,
		NodeID:       cp.NodeID,
		Rank:         int(e.Int("rank")),
		ASIN:         asin,
		Title:        e.Str("title"),
		Price:        money(e, "price", c.mkt),
		Rating:       f64OrNil(e.Float("rating")),
		RatingsCount: i64OrNil(e.Int("ratings_count")),
		Image:        upgradeImage(e.Str("image")),
		URL:          c.ProductURL(asin),
		FetchedAt:    time.Now().UTC(),
	}
	// The landing page runs six lists side by side and each starts again at 1,
	// so a rank without the list it belongs to is meaningless. The carousel the
	// tile sits in names that list, in the heading above it.
	if cp.Layout == "index" {
		if name := carouselTitle(r); name != "" {
			entry.Category = name
		}
	}
	entry.Envelope = e.Envelope()
	return entry, true
}

// carouselTitle is the heading of the carousel a tile sits in, "Best Sellers in
// Health & Household".
func carouselTitle(r Region) string {
	if !r.Exists() {
		return ""
	}
	w := r.Sel().Closest(`[cel_widget_id^="` + chartCarousel + `"]`)
	if w.Length() == 0 {
		return ""
	}
	return collapseSpace(nodeText(w.Find("h2").First()))
}
