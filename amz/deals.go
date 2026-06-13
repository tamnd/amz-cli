package amz

import (
	"context"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DealsURL builds the deals grid URL.
func (c *Client) DealsURL() string {
	return c.BaseURL() + "/deals"
}

// FetchDeals streams entries from the deals grid.
func (c *Client) FetchDeals(ctx context.Context, limit int, emit func(Deal) error) error {
	u := c.DealsURL()
	body, err := c.Get(ctx, u, time.Hour)
	if err != nil {
		return err
	}
	doc, err := newDocument(body)
	if err != nil {
		return err
	}
	count := 0
	var perr error
	doc.Find("[data-testid='deal-card'], .DealCard-module, .a-carousel-card[data-asin], div[data-asin]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		d := Deal{Currency: c.mkt.Currency, FetchedAt: time.Now().UTC()}
		if v, ok := s.Attr("data-asin"); ok {
			d.ASIN = v
		}
		link := s.Find("a[href*='/dp/'], a[href*='/deal/']").First()
		if d.ASIN == "" {
			if href, ok := link.Attr("href"); ok {
				d.ASIN = ExtractASIN(href)
			}
		}
		if href, ok := link.Attr("href"); ok {
			d.URL = absoluteURL(c.BaseURL(), href)
		}
		d.Title = collapseSpace(firstSelText(s,
			"[data-testid='deal-title']", ".DealContent-module__truncate", "img[alt]", ".a-truncate-full"))
		if d.Title == "" {
			d.Title = collapseSpace(attrSel(link, "img", "alt"))
		}
		d.DealPrice, _ = ParsePrice(s.Find(".a-price .a-offscreen, [data-testid='deal-price']").First().Text())
		d.ListPrice, _ = ParsePrice(s.Find(".a-text-price .a-offscreen, [data-testid='list-price']").First().Text())
		d.Badge = collapseSpace(s.Find("[data-testid='deal-badge'], .DealBadge-module, .a-badge-text").First().Text())
		if pctTxt := s.Find("[data-testid='deal-percent-off'], .BadgeAutomatedLabel-module").First().Text(); pctTxt != "" {
			if m := pctRe.FindStringSubmatch(pctTxt); m != nil {
				d.DiscountPct = int(parseInt(m[1]))
			}
		}
		if d.DiscountPct == 0 && d.ListPrice > 0 && d.DealPrice > 0 && d.DealPrice < d.ListPrice {
			d.DiscountPct = int((1 - d.DealPrice/d.ListPrice) * 100)
		}
		if d.ASIN == "" && d.Title == "" {
			return true
		}
		count++
		if err := emit(d); err != nil {
			perr = err
			return false
		}
		return limit <= 0 || count < limit
	})
	return perr
}

func firstSelText(s *goquery.Selection, sels ...string) string {
	for _, sel := range sels {
		if strings.Contains(sel, "[alt]") {
			if v, ok := s.Find(sel).First().Attr("alt"); ok && strings.TrimSpace(v) != "" {
				return v
			}
			continue
		}
		if t := strings.TrimSpace(s.Find(sel).First().Text()); t != "" {
			return t
		}
	}
	return ""
}

func attrSel(s *goquery.Selection, sel, name string) string {
	v, _ := s.Find(sel).First().Attr(name)
	return v
}
