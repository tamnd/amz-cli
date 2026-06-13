package amz

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SearchQuery holds the refinements for a catalog search.
type SearchQuery struct {
	Sort       string // relevance|price-asc|price-desc|review|newest
	MinPrice   int
	MaxPrice   int
	MinRating  int
	Prime      bool
	Brand      string
	Department string
	StartPage  int
	Limit      int
}

var sortMap = map[string]string{
	"relevance":  "relevanceblender",
	"price-asc":  "price-asc-rank",
	"price-desc": "price-desc-rank",
	"review":     "review-rank",
	"newest":     "date-desc-rank",
}

// SearchURL builds the /s URL for a query and page.
func (c *Client) SearchURL(query string, q SearchQuery, page int) string {
	v := url.Values{}
	v.Set("k", query)
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if s, ok := sortMap[q.Sort]; ok {
		v.Set("s", s)
	}
	var rh []string
	if q.MinPrice > 0 || q.MaxPrice > 0 {
		rh = append(rh, "p_36:"+strconv.Itoa(q.MinPrice*100)+"-"+priceHi(q.MaxPrice))
	}
	if q.MinRating > 0 && q.MinRating <= 4 {
		// p_72 review-rating refinements: 1247-... ; use the documented "4 stars & up" style id.
		ids := map[int]string{4: "1248882011", 3: "1248883011", 2: "1248884011", 1: "1248885011"}
		if id, ok := ids[q.MinRating]; ok {
			rh = append(rh, "p_72:"+id)
		}
	}
	if q.Prime {
		rh = append(rh, "p_85:2470955011")
	}
	if len(rh) > 0 {
		v.Set("rh", strings.Join(rh, ","))
	}
	if q.Department != "" {
		v.Set("i", q.Department)
	}
	return c.BaseURL() + "/s?" + v.Encode()
}

func priceHi(hi int) string {
	if hi <= 0 {
		return ""
	}
	return strconv.Itoa(hi * 100)
}

// Search streams result cards for a query, paging until Limit is reached.
func (c *Client) Search(ctx context.Context, query string, q SearchQuery, emit func(Card) error) error {
	page := q.StartPage
	if page < 1 {
		page = 1
	}
	count := 0
	for {
		u := c.SearchURL(query, q, page)
		body, err := c.Get(ctx, u, time.Hour)
		if err != nil {
			return err
		}
		cards, err := c.parseSearch(body)
		if err != nil {
			return err
		}
		if len(cards) == 0 {
			break
		}
		for _, card := range cards {
			count++
			card.Position = count
			if err := emit(card); err != nil {
				return err
			}
			if q.Limit > 0 && count >= q.Limit {
				return nil
			}
		}
		page++
		if page > 20 { // safety: amazon caps search at ~20 pages
			break
		}
	}
	return nil
}

func (c *Client) parseSearch(body []byte) ([]Card, error) {
	doc, err := newDocument(body)
	if err != nil {
		return nil, err
	}
	var cards []Card
	doc.Find(`div[data-component-type="s-search-result"][data-asin]`).Each(func(_ int, s *goquery.Selection) {
		asin, _ := s.Attr("data-asin")
		if !bareASIN.MatchString(asin) {
			return
		}
		card := Card{ASIN: asin, Currency: c.mkt.Currency, URL: c.ProductURL(asin)}
		card.Title = collapseSpace(s.Find("h2 span, h2 a span, .a-size-medium.a-color-base.a-text-normal").First().Text())
		card.Price, _ = ParsePrice(s.Find(".a-price:not(.a-text-price) .a-offscreen").First().Text())
		if card.Price == 0 {
			card.Price, _ = ParsePrice(s.Find(".a-price .a-offscreen").First().Text())
		}
		// The struck-through list/was price carries a-text-price.
		card.ListPrice, _ = ParsePrice(s.Find(".a-price.a-text-price .a-offscreen, .a-text-price[data-a-strike='true'] .a-offscreen").First().Text())
		card.Rating = parseRating(s.Find(".a-icon-alt").First().Text())
		card.RatingsCount = parseInt(s.Find(".a-size-base.s-underline-text, .a-size-base.puis-normal-weight-text").First().Text())
		card.Image = upgradeImage(attrOf(s.Find("img.s-image").First(), "src"))
		if s.Find(".puis-sponsored-label-text, .s-sponsored-label-text").Length() > 0 {
			card.Sponsored = true
		}
		if s.Find(".a-icon-prime, [aria-label='Amazon Prime']").Length() > 0 {
			card.Prime = true
		}
		card.Badge = collapseSpace(s.Find(".a-badge-text, .puis-badge-text").First().Text())
		// "N+ bought in past month" social-proof line.
		s.Find("span").EachWithBreak(func(_ int, sp *goquery.Selection) bool {
			t := collapseSpace(sp.Text())
			if strings.Contains(strings.ToLower(t), "bought in past month") {
				card.BoughtPastMonth = t
				return false
			}
			return true
		})
		card.Kind = "search"
		cards = append(cards, card)
	})
	return cards, nil
}
