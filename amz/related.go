package amz

import (
	"context"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchRelated streams recommendation cards (similar items, "frequently bought
// together", sponsored rails) found on a product detail page.
func (c *Client) FetchRelated(ctx context.Context, asin string, limit int, emit func(Card) error) error {
	u := c.ProductURL(asin)
	body, src, err := c.GetSource(ctx, u, 12*time.Hour)
	if err != nil {
		return err
	}
	doc, err := newDocument(body)
	if err != nil {
		return err
	}
	// These cards are read straight off the detail page rather than through an
	// extractor, so there is no page record to hang the response on. The rows
	// still have to say where they came from, so the envelope is built here.
	page := Envelope{Family: FamilyProduct}
	c.record(ctx, &page, src)
	seen := map[string]bool{asin: true}
	count := 0
	var perr error
	emitCard := func(card Card) bool {
		if card.ASIN == "" || seen[card.ASIN] {
			return true
		}
		seen[card.ASIN] = true
		count++
		if err := emit(card); err != nil {
			perr = err
			return false
		}
		return limit <= 0 || count < limit
	}
	doc.Find("li.a-carousel-card, .a-carousel-card, div[data-asin].sponsored-products-truncator-truncated, ol[data-acp-path] li").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		card := Card{Kind: relatedKind(s)}
		if v, ok := s.Attr("data-asin"); ok {
			card.ASIN = v
		}
		link := s.Find("a[href*='/dp/']").First()
		if card.ASIN == "" {
			if href, ok := link.Attr("href"); ok {
				card.ASIN = ExtractASIN(href)
			}
		}
		if card.ASIN != "" {
			card.URL = c.ProductURL(card.ASIN)
		}
		card.Title = collapseSpace(firstSelText(s, ".p13n-sc-truncate", ".a-truncate-full", "img[alt]"))
		card.Price = ParseMoney(s.Find(".a-price .a-offscreen, .p13n-sc-price").First().Text(), c.mkt, ".a-price .a-offscreen")
		card.Rating = f64OrNil(parseRating(s.Find(".a-icon-alt").First().Text()))
		card.RatingsCount = i64OrNil(parseInt(s.Find(".a-size-small").First().Text()))
		card.Image = upgradeImage(attrSel(s, "img", "src"))
		card.Envelope.Inherit(page)
		return emitCard(card)
	})
	return perr
}

func relatedKind(s *goquery.Selection) string {
	if s.Find(".s-sponsored-label-text").Length() > 0 {
		return "sponsored"
	}
	return "related"
}
