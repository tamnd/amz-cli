package amz

import (
	"encoding/json"

	"github.com/PuerkitoBio/goquery"
)

// The list Amazon hands its own client renderer, and why reading it is not
// optional.
//
// Measured on 2026-08-17 against /gp/bestsellers/electronics: the grid renders
// 30 tiles, ranks 1 through 30. Page two of the same chart renders ranks 51
// through 80. Ranks 31 through 50 are on neither page. They are in the grid
// container's data-client-recs-list attribute, an 18 KB JSON array naming all
// 50 items of page one with a render.zg.rank apiece, which the page's own
// JavaScript reads to fill the rest of the grid as the reader scrolls.
//
// So a parser that reads tiles alone loses 40 percent of every chart, silently,
// and would keep losing it while every test passed. That is exactly the failure
// the ladder exists to prevent: the tiles are rung 4 markup and the list is
// rung 2 data, and the rung 2 source is both more complete and more stable.
//
// The container states the shape too. data-offset is how many items the list
// holds, 50 on a bestsellers page and 0 on movers-and-shakers. data-index-offset
// is how many the server rendered before handing over, 30 everywhere seen. Both
// are recorded so a short chart can be told from a broken one.
//
// See notes/Spec/3007/02_extraction.md section 9.

// PayloadChartList is the attribute the chart list arrives in.
const PayloadChartList = "data-client-recs-list"

// chartTileSel is the tile anchor, which is the same in both chart layouts: a
// div carrying data-asin, holding the rank badge and the faceout.
const chartTileSel = "[data-asin]"

// ChartListItem is one item of the list, as Amazon states it.
type ChartListItem struct {
	ASIN string `json:"asin"`
	Rank int    `json:"rank"`
	// SalesRank, PriorSalesRank and PercentChange are the movers-and-shakers
	// numbers. They are declared because Amazon declares them, and they were
	// empty on every capture taken on 2026-08-17, including the movers page,
	// whose list was empty entirely.
	SalesRank      int     `json:"sales_rank,omitempty"`
	PriorSalesRank int     `json:"prior_sales_rank,omitempty"`
	PercentChange  float64 `json:"percent_change,omitempty"`
}

// chartList is the parsed list plus what the container said about it.
type chartList struct {
	Items []ChartListItem
	// Listed is data-offset, the item count Amazon put on the grid. It is zero
	// on a movers page that has no list rather than on a page that failed to
	// parse, and the two are reported differently.
	Listed int
	// ServerRendered is data-index-offset, the count the server drew before
	// leaving the rest to script.
	ServerRendered int
	// RefTag is data-reftag, which names the list and its category in one
	// token: zg_bs_g_electronics, zg_bsnr_g_electronics, zg_bsms_g_electronics.
	RefTag string
	// Present reports whether the grid container was on the page at all.
	Present bool
	// byASIN is the same items keyed for the tile reader, built once because
	// every tile on the page looks its own rank up.
	byASIN map[string]ChartListItem
}

// recsEntry is the wire shape. Every value in metadataMap is a string, ranks
// included, so the numbers are parsed rather than typed.
type recsEntry struct {
	ID          string            `json:"id"`
	MetadataMap map[string]string `json:"metadataMap"`
}

func readChartList(doc *goquery.Document) chartList {
	var out chartList
	s := doc.Find("[" + PayloadChartList + "]").First()
	if s.Length() == 0 {
		return out
	}
	out.Present = true
	out.Listed = int(parseInt(attrOf(s, "data-offset")))
	out.ServerRendered = int(parseInt(attrOf(s, "data-index-offset")))
	out.RefTag = attrOf(s, "data-reftag")

	var raw []recsEntry
	if err := json.Unmarshal([]byte(attrOf(s, PayloadChartList)), &raw); err != nil {
		return out
	}
	out.byASIN = make(map[string]ChartListItem, len(raw))
	for _, e := range raw {
		if !isASIN(e.ID) {
			continue
		}
		it := ChartListItem{
			ASIN:           e.ID,
			Rank:           int(parseInt(e.MetadataMap["render.zg.rank"])),
			SalesRank:      int(parseInt(e.MetadataMap["render.zg.bsms.currentSalesRank"])),
			PriorSalesRank: int(parseInt(e.MetadataMap["render.zg.bsms.twentyFourHourOldSalesRank"])),
			PercentChange:  toFloat(e.MetadataMap["render.zg.bsms.percentageChange"]),
		}
		out.Items = append(out.Items, it)
		out.byASIN[it.ASIN] = it
	}
	return out
}

// ChartList returns the list this page published for its own renderer.
func (d *Doc) ChartList() []ChartListItem { return d.chart.Items }
