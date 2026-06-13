package amz

import (
	"bytes"
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

// FetchChart streams ranked entries from a chart, across its (usually two) pages.
func (c *Client) FetchChart(ctx context.Context, kind ChartKind, category, node string, limit int, emit func(BestsellerEntry) error) error {
	count := 0
	for page := 1; page <= 2; page++ {
		u := c.ChartURL(kind, category, node, page)
		body, err := c.Get(ctx, u, time.Hour)
		if err != nil {
			if page > 1 {
				break
			}
			return err
		}
		entries := c.parseChart(string(kind), category, node, body)
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			count++
			if err := emit(e); err != nil {
				return err
			}
			if limit > 0 && count >= limit {
				return nil
			}
		}
	}
	return nil
}

func (c *Client) parseChart(listType, category, node string, body []byte) []BestsellerEntry {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var out []BestsellerEntry
	doc.Find("#gridItemRoot, .zg-grid-general-faceout, li.zg-item-immersion, .p13n-sc-uncoverable-faceout").Each(func(_ int, s *goquery.Selection) {
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
		if e.ASIN == "" {
			return
		}
		out = append(out, e)
	})
	// Re-rank if amazon omitted badges (rank by document order, offset by page).
	for i := range out {
		if out[i].Rank == 0 {
			out[i].Rank = i + 1
		}
	}
	return out
}
