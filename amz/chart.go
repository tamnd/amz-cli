package amz

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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
// (or the limit is reached). Ranks are offset by page so page two continues the
// numbering even when amazon drops the rank badges.
func (c *Client) FetchChart(ctx context.Context, kind ChartKind, category, node string, limit int, emit func(BestsellerEntry) error) error {
	count := 0
	seen := make(map[string]bool)
	maxPages := chartMaxPages
	for page := 1; page <= maxPages; page++ {
		u := c.ChartURL(kind, category, node, page)
		body, err := c.Get(ctx, u, time.Hour)
		if err != nil {
			if page > 1 {
				break
			}
			return err
		}
		entries := c.parseChart(string(kind), category, node, body, count)
		if len(entries) == 0 {
			break
		}
		fresh := 0
		for _, e := range entries {
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
		// A page that adds nothing new (amazon served a repeat or the last
		// page again) means we have walked off the end of the chart.
		if fresh == 0 {
			break
		}
	}
	return nil
}

func (c *Client) parseChart(listType, category, node string, body []byte, rankOffset int) []BestsellerEntry {
	doc, err := newDocument(body)
	if err != nil {
		return nil
	}
	// Amazon nests a .zg-grid-general-faceout inside each #gridItemRoot, so a
	// combined selector would match every item twice: once on the outer node
	// that carries the rank badge and once on the inner faceout that does not.
	// Pick the first layout that matches and iterate only that, newest grid
	// layout first, so each product yields exactly one entry.
	var items *goquery.Selection
	for _, sel := range []string{
		"#gridItemRoot",
		"li.zg-item-immersion",
		".zg-grid-general-faceout",
		".p13n-sc-uncoverable-faceout",
	} {
		if got := doc.Find(sel); got.Length() > 0 {
			items = got
			break
		}
	}
	if items == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []BestsellerEntry
	items.Each(func(_ int, s *goquery.Selection) {
		e := BestsellerEntry{
			ListType:  listType,
			Category:  category,
			NodeID:    node,
			Currency:  c.mkt.Currency,
			FetchedAt: time.Now().UTC(),
		}
		// Rank badge like "#1".
		rankTxt := s.Find(".zg-bdg-text, .zg-badge-text").First().Text()
		e.Rank = int(parseInt(strings.TrimPrefix(strings.TrimSpace(rankTxt), "#")))
		// ASIN + URL from the product link.
		link := s.Find("a.a-link-normal[href*='/dp/'], a[href*='/dp/']").First()
		if href, ok := link.Attr("href"); ok {
			e.ASIN = ExtractASIN(href)
			if e.ASIN != "" {
				e.URL = c.ProductURL(e.ASIN)
			}
		}
		e.Title = collapseSpace(s.Find("._cDEzb_p13n-sc-css-line-clamp-3_g3dy1, .p13n-sc-truncate, .p13n-sc-truncated, ._cDEzb_p13n-sc-css-line-clamp-2_EWgCb, div.zg-text-center-align + div").First().Text())
		if e.Title == "" {
			e.Title = collapseSpace(link.Text())
		}
		e.Price, _ = ParsePrice(s.Find(".a-price .a-offscreen, .p13n-sc-price").First().Text())
		e.Rating = parseRating(s.Find(".a-icon-alt").First().Text())
		e.RatingsCount = parseInt(s.Find(".a-size-small, .a-icon-row .a-size-small").First().Text())
		if e.ASIN == "" || seen[e.ASIN] {
			return
		}
		seen[e.ASIN] = true
		out = append(out, e)
	})
	// Re-rank if amazon omitted badges (rank by document order, offset by page).
	for i := range out {
		if out[i].Rank == 0 {
			out[i].Rank = rankOffset + i + 1
		}
	}
	return out
}
